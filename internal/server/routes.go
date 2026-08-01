package server

import (
	"errors"
	"fmt"
	analyticsdb "go-api-server/internal/database/analytics"
	animaldb "go-api-server/internal/database/animal"
	farmerdb "go-api-server/internal/database/farmer"
	imagedb "go-api-server/internal/database/image"
	qrdb "go-api-server/internal/database/qr"
	"go-api-server/internal/inference"
	"go-api-server/internal/middleware"
	"go-api-server/internal/server/mobile/handlers/analytics"
	"go-api-server/internal/server/mobile/handlers/animal"
	"go-api-server/internal/server/mobile/handlers/farmer"
	"go-api-server/internal/server/mobile/handlers/image"
	"go-api-server/internal/server/mobile/handlers/qr"
	webAnalytics "go-api-server/internal/server/web/handlers/analytics"
	"go-api-server/internal/storage"
	"log/slog"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
)

type ErrorResponse struct {
	Message string `json:"message"`
}

type dbRepositories struct {
	farmer    *farmerdb.Repository
	animal    *animaldb.Repository
	analytics *analyticsdb.Repository
	image     *imagedb.Repository
	qr        *qrdb.Repository
}

func newDbRepositories(db *sqlx.DB) *dbRepositories {
	return &dbRepositories{
		farmer:    farmerdb.NewRepository(db),
		animal:    animaldb.NewRepository(db),
		analytics: analyticsdb.NewRepository(db),
		image:     imagedb.NewRepository(db),
		qr:        qrdb.NewRepository(db),
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
	dbHandle := s.db.DB()

	dbRepo := newDbRepositories(dbHandle)

	api := e.Group("/api")
	api.HEAD("/", s.rootHandler, middleware.RequireAPIKeyAuth(s.cfg.AdminAPIKey))
	api.GET("/health", s.healthHandler, middleware.RequireAPIKeyAuth(s.cfg.AdminAPIKey))

	web := api.Group("/web/v1")
	mobile := api.Group("/mobile/v1", middleware.RequireJWTAuth(s.cfg.JWTSecret, s.cfg.AdminAPIKey))

	registerWebRoutes(web, dbRepo)
	registerMobileRoutes(mobile, dbRepo, gcsStore, inferenceClient, s.cfg.QREncryptionKey)

	return e, nil
}

func registerWebRoutes(web *echo.Group, dbRepo *dbRepositories) {
	webAnalytics.RegisterRoutes(web, &webAnalytics.Handler{DB: dbRepo.analytics})
}

func registerMobileRoutes(mobile *echo.Group, dbRepo *dbRepositories, gcsStorage storage.Storage, inferenceClient inference.Client, qrKey []byte) {
	farmer.RegisterRoutes(mobile, &farmer.Handler{DB: dbRepo.farmer, Storage: gcsStorage})
	animal.RegisterRoutes(mobile, &animal.Handler{DB: dbRepo.animal, Storage: gcsStorage, Inference: inferenceClient})
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
	message := "internal server error"

	logErr := err

	if httpError, ok := errors.AsType[*echo.HTTPError](err); ok {
		code = httpError.Code

		if msg, ok := httpError.Message.(string); ok {
			message = msg
		}

		if httpError.Internal != nil {
			logErr = httpError.Internal
			if inferErr, ok := errors.AsType[*inference.ResponseError](logErr); ok {
				logErr = inferErr.RawError
			}
		}
	}

	attrs := []slog.Attr{
		slog.String("requestID", middleware.GetRequestID(c)),
		slog.Int("status", code),
		slog.String("method", c.Request().Method),
		slog.String("path", c.Request().URL.Path),
		slog.String("userID", middleware.UserIDFromContext(c)),
		slog.String("message", message),
		slog.Any("error", logErr),
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
	c.JSON(code, ErrorResponse{Message: message})
}

func (s *Server) rootHandler(c echo.Context) error {
	return c.NoContent(http.StatusOK)
}

func (s *Server) healthHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, s.db.Health())
}
