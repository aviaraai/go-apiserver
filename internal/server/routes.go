package server

import (
	"errors"
	"fmt"
	"go-api-server/internal/cctv"
	analyticsdb "go-api-server/internal/database/analytics"
	animaldb "go-api-server/internal/database/animal"
	cctvdb "go-api-server/internal/database/cctv"
	debugdb "go-api-server/internal/database/debug"
	farmerdb "go-api-server/internal/database/farmer"
	imagedb "go-api-server/internal/database/image"
	qrdb "go-api-server/internal/database/qr"
	"go-api-server/internal/httperr"
	"go-api-server/internal/inference"
	"go-api-server/internal/middleware"
	"go-api-server/internal/server/mobile/handlers/analytics"
	"go-api-server/internal/server/mobile/handlers/animal"
	"go-api-server/internal/server/mobile/handlers/farmer"
	"go-api-server/internal/server/mobile/handlers/image"
	"go-api-server/internal/server/mobile/handlers/qr"
	webAnalytics "go-api-server/internal/server/web/handlers/analytics"
	webCCTV "go-api-server/internal/server/web/handlers/cctv"
	webDebug "go-api-server/internal/server/web/handlers/debug"
	"go-api-server/internal/storage"
	"log/slog"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
)

type dbRepositories struct {
	farmer    *farmerdb.Repository
	animal    *animaldb.Repository
	analytics *analyticsdb.Repository
	image     *imagedb.Repository
	qr        *qrdb.Repository
	debug     *debugdb.Repository
	cctv      *cctvdb.Repository
}

func newDbRepositories(db *sqlx.DB) *dbRepositories {
	return &dbRepositories{
		farmer:    farmerdb.NewRepository(db),
		animal:    animaldb.NewRepository(db),
		analytics: analyticsdb.NewRepository(db),
		image:     imagedb.NewRepository(db),
		qr:        qrdb.NewRepository(db),
		debug:     debugdb.NewRepository(db),
		cctv:      cctvdb.NewRepository(db),
	}
}

func (s *Server) RegisterRoutes() (http.Handler, error) {
	e := echo.New()
	e.Use(echoMiddleware.RequestID())
	e.Use(echoMiddleware.Recover())

	e.HTTPErrorHandler = customHTTPErrorHandler

	gcsStore, err := storage.NewGCSStore(s.cfg)
	if err != nil {
		return nil, fmt.Errorf("initialize gcs store: %w", err)
	}
	slog.Info("storage client initialized")
	inferenceClient := inference.NewHTTPClient(s.cfg.InferenceServerURL)
	cctvInference := inference.NewHTTPCCTVClient(s.cfg.InferenceServerURL)

	// The camera integration is not written yet. Everything downstream of the
	// fetch works; swapping this for a real Source is the only step left.
	//
	// NotImplementedSource, not NaiveImplementationSource: the dashboard
	// already has real, tested handling for a clean 503 CCTV_SOURCE_UNAVAILABLE
	// ("camera not reachable right now", retryable) — that is the honest state
	// today, not a fixed local test file standing in as every goshala's camera.
	// Swap this back to NaiveImplementationSource only for local pipeline
	// testing, never for anything the dashboard actually talks to.
	var cctvSource cctv.Source = cctv.NotImplementedSource{}

	dbHandle := s.db.DB()

	dbRepo := newDbRepositories(dbHandle)

	api := e.Group("/api")
	api.HEAD("", s.rootHandler, middleware.RequireAPIKeyAuth(s.cfg.AdminAPIKey))
	api.GET("/health", s.healthHandler, middleware.RequireAPIKeyAuth(s.cfg.AdminAPIKey))

	web := api.Group("/web/v1", middleware.RequireJWTAuth(s.cfg.JWTSecret, s.cfg.SupabaseProjectURL+"/auth/v1"))
	mobile := api.Group("/mobile/v1", middleware.RequireJWTAuth(s.cfg.JWTSecret, s.cfg.SupabaseProjectURL+"/auth/v1"))

	registerWebRoutes(web, dbRepo, gcsStore, cctvInference, cctvSource)
	registerMobileRoutes(mobile, dbRepo, gcsStore, inferenceClient, s.cfg.QREncryptionKey)

	return e, nil
}

func registerWebRoutes(
	web *echo.Group,
	dbRepo *dbRepositories,
	gcsStorage storage.Storage,
	cctvInference inference.CCTVClient,
	cctvSource cctv.Source,
) {
	webAnalytics.RegisterRoutes(web, &webAnalytics.Handler{DB: dbRepo.analytics})
	webDebug.RegisterRoutes(web, &webDebug.Handler{DB: dbRepo.debug, Storage: gcsStorage})
	webCCTV.RegisterRoutes(web, &webCCTV.Handler{
		DB:        dbRepo.cctv,
		Storage:   gcsStorage,
		Inference: cctvInference,
		Source:    cctvSource,
	})
}

func registerMobileRoutes(mobile *echo.Group, dbRepo *dbRepositories, gcsStorage storage.Storage, inferenceClient inference.Client, qrKey []byte) {
	farmer.RegisterRoutes(mobile, &farmer.Handler{DB: dbRepo.farmer, Storage: gcsStorage})
	animal.RegisterRoutes(mobile, &animal.Handler{
		DB:        dbRepo.animal,
		DebugDB:   dbRepo.debug,
		Storage:   gcsStorage,
		Inference: inferenceClient,
	})
	analytics.RegisterRoutes(mobile, &analytics.Handler{DB: dbRepo.analytics})

	animalGroup := mobile.Group("/animals")

	image.RegisterRoutes(animalGroup, &image.Handler{DB: dbRepo.image, Storage: gcsStorage})
	qr.RegisterRoutes(animalGroup, &qr.Handler{DB: dbRepo.qr, Storage: gcsStorage, QRKey: qrKey})
}

func customHTTPErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}

	code := http.StatusInternalServerError
	body := httperr.Response{Message: "internal server error"}

	logErr := err

	if httpError, ok := errors.AsType[*echo.HTTPError](err); ok {
		code = httpError.Code

		// A handler may return either a plain string message or a full
		// Response when it has machine-readable data to pass on.
		switch msg := httpError.Message.(type) {
		case httperr.Response:
			body = msg
		case string:
			body.Message = msg
		}

		if httpError.Internal != nil {
			logErr = httpError.Internal
		}
	}

	attrs := []slog.Attr{
		slog.String("requestID", middleware.GetRequestID(c)),
		slog.Int("status", code),
		slog.String("method", c.Request().Method),
		slog.String("path", c.Request().URL.Path),
		slog.String("userID", middleware.UserIDFromContext(c)),
		slog.String("message", body.Message),
		slog.Any("error", logErr),
	}

	// Inference errors keep their internal detail out of Error(), so RawError
	// is pulled out separately — this is the only place the upstream body, URL
	// and dial error are allowed to surface. It is an extra attribute rather
	// than a replacement because logErr may be several joined problems (the
	// inference failure plus, say, a debug capture that did not write), and
	// collapsing it to the upstream detail alone would drop the rest.
	if inferErr, ok := errors.AsType[*inference.Error](logErr); ok {
		if body.Code == "" {
			body.Code = string(inferErr.Code)
		}
		if inferErr.RawError != nil {
			attrs = append(attrs, slog.Any("upstreamError", inferErr.RawError))
		}
	}
	if body.Code != "" {
		attrs = append(attrs, slog.String("errorCode", body.Code))
	}

	if device, ok := middleware.DeviceInfoFromContext(c); ok {
		attrs = append(attrs,
			slog.String("manufacturer", device.Manufacturer),
			slog.String("model", device.Model),
			slog.String("os_version", device.OSVersion),
			slog.String("app_version", device.AppVersion))
	}

	switch {
	case code >= 500:
		slog.LogAttrs(c.Request().Context(), slog.LevelError, "server error", attrs...)
	default:
		slog.LogAttrs(c.Request().Context(), slog.LevelWarn, "client error", attrs...)
	}
	c.JSON(code, body)
}

func (s *Server) rootHandler(c echo.Context) error {
	return c.NoContent(http.StatusOK)
}

func (s *Server) healthHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, s.db.Health())
}
