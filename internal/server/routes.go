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
	"go-api-server/internal/server/handlers/analytics"
	"go-api-server/internal/server/handlers/animal"
	"go-api-server/internal/server/handlers/farmer"
	"go-api-server/internal/server/handlers/image"
	"go-api-server/internal/server/handlers/qr"
	"go-api-server/internal/storage"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	echoMiddleware "github.com/labstack/echo/v4/middleware"
)

type ErrorResponse struct {
	Message string `json:"message"`
}

func (s *Server) RegisterRoutes() (http.Handler, error) {
	e := echo.New()
	e.Use(echoMiddleware.RequestID())
	e.Use(echoMiddleware.Recover())

	e.HTTPErrorHandler = customHTTPErrorHandler
	e.HEAD("/", s.rootHandler, middleware.RequireAPIKeyAuth(s.cfg.AdminAPIKey))
	e.GET("/health", s.healthHandler, middleware.RequireAPIKeyAuth(s.cfg.AdminAPIKey))

	gcsStore, err := storage.NewGCSStore(s.cfg)
	if err != nil {
		return nil, fmt.Errorf("initialize gcs store: %w", err)
	}
	slog.Info("storage client initialized")
	inferenceClient := inference.NewHTTPClient(s.cfg.InferenceServerURL)
	dbHandle := s.db.DB()

	farmerHandler := &farmer.Handler{DB: farmerdb.NewRepository(dbHandle), Storage: gcsStore}
	animalHandler := &animal.Handler{DB: animaldb.NewRepository(dbHandle), Storage: gcsStore, Inference: inferenceClient}
	analyticsHandler := &analytics.Handler{DB: analyticsdb.NewRepository(dbHandle), Storage: gcsStore}
	imageHandler := &image.Handler{DB: imagedb.NewRepository(dbHandle), Storage: gcsStore}
	qrHandler := &qr.Handler{DB: qrdb.NewRepository(dbHandle), Storage: gcsStore, QRKey: s.cfg.QREncryptionKey}

	api := e.Group("/api")
	mobile := api.Group("/mobile/v1", middleware.RequireJWTAuth(s.cfg.JWTSecret, s.cfg.AdminAPIKey))

	farmer.RegisterRoutes(mobile, farmerHandler)
	animal.RegisterRoutes(mobile, animalHandler)
	analytics.RegisterRoutes(mobile, analyticsHandler)

	animalGroup := mobile.Group("/animals")

	image.RegisterRoutes(animalGroup, imageHandler)
	qr.RegisterRoutes(animalGroup, qrHandler)

	return e, nil
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
