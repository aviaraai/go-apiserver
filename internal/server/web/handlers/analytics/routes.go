package analytics

import (
	"context"
	"go-api-server/internal/database/analytics"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

type Repository interface {
	AdminAnalytics(context.Context, *string, *string, *string, *time.Time, *time.Time) ([]analytics.AdminAnalytics, error)
	AdminTotalAnalytics(context.Context) (*analytics.AdminTotalAnalytics, error)
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

func RegisterRoutes(g *echo.Group, h *Handler) {
	analyticsGroup := g.Group("/analytics")
	analyticsGroup.GET("", h.analytics)
	analyticsGroup.GET("/totals", h.analyticsTotal)
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

	adminAnalytics, err := h.DB.AdminAnalytics(ctx, req.State, req.District, req.Mandal, from, to)
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
