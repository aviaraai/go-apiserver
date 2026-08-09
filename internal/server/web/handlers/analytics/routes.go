package analytics

import (
	"context"
	"go-api-server/internal/database/analytics"
	"go-api-server/internal/middleware"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type Repository interface {
	AdminAnalytics(context.Context, *string, *string, *string, *string, *time.Time, *time.Time) ([]analytics.AdminAnalytics, error)
	AdminTotalAnalytics(context.Context) (*analytics.AdminTotalAnalytics, error)
	LegacyAnalytics(context.Context, *string, *string, *string) ([]analytics.LegacyAnalytics, error)
}

type Handler struct {
	DB Repository
}

func toAnalyticsResponse(analytics analytics.AdminAnalytics) AnalyticsResponse {
	return AnalyticsResponse{
		UserEmail:       analytics.UserEmail,
		TotalFarmers:    analytics.TotalFarmers,
		TotalAnimals:    analytics.TotalAnimals,
		TotalAssigned:   analytics.TotalAssigned,
		TotalUnassigned: analytics.TotalUnassigned,
	}
}

func toLegacyAnalyticsResponse(legacyAnalytics *analytics.LegacyAnalytics) LegacyAnalyticsResponse {
	return LegacyAnalyticsResponse{
		State:       legacyAnalytics.State,
		District:    legacyAnalytics.District,
		Mandal:      legacyAnalytics.Mandal,
		FarmerCount: legacyAnalytics.FarmerCount,
		AnimalCount: legacyAnalytics.AnimalCount,
	}
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	analyticsGroup := g.Group("/analytics", middleware.RequireRole("admin"))
	analyticsGroup.GET("", h.analytics)
	analyticsGroup.GET("/totals", h.analyticsTotal)
	analyticsGroup.GET("/legacy", h.legacyAnalytics)
}

func (h *Handler) analytics(c echo.Context) error {
	ctx := c.Request().Context()

	var req AnalyticsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing data").SetInternal(err)
	}

	var from *time.Time
	if req.FromStr != nil {
		t, err := time.Parse(time.RFC3339, *req.FromStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid from timestamp")
		}
		from = &t
	}

	var to *time.Time
	if req.ToStr != nil {
		t, err := time.Parse(time.RFC3339, *req.ToStr)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid to timestamp")
		}
		to = &t
	}

	adminAnalytics, err := h.DB.AdminAnalytics(ctx, req.State, req.District, req.Mandal, req.Breed, from, to)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch admin analytics").SetInternal(err)
	}
	adminAnalyticsRes := make([]AnalyticsResponse, len(adminAnalytics))
	for i := range adminAnalytics {
		adminAnalyticsRes[i] = toAnalyticsResponse(adminAnalytics[i])
	}
	return c.JSON(http.StatusOK, adminAnalyticsRes)
}

func (h *Handler) analyticsTotal(c echo.Context) error {
	ctx := c.Request().Context()

	adminTotalAnalytics, err := h.DB.AdminTotalAnalytics(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch admin analytics").SetInternal(err)
	}
	adminTotalAnalyticsRes := &TotalAnalyticsResponse{
		TotalFarmers: adminTotalAnalytics.TotalFarmers,
		TotalAnimals: adminTotalAnalytics.TotalAnimals,
	}
	return c.JSON(http.StatusOK, adminTotalAnalyticsRes)
}

func (h *Handler) legacyAnalytics(c echo.Context) error {
	ctx := c.Request().Context()

	var req LegacyAnalyticsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing data").SetInternal(err)
	}

	legacyAnalytics, err := h.DB.LegacyAnalytics(ctx, req.State, req.District, req.Mandal)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch legacy analytics").SetInternal(err)
	}
	legacyAnalyticsRes := make([]LegacyAnalyticsResponse, len(legacyAnalytics))
	for i := range legacyAnalytics {
		legacyAnalyticsRes[i] = toLegacyAnalyticsResponse(&legacyAnalytics[i])
	}
	return c.JSON(http.StatusOK, legacyAnalyticsRes)
}
