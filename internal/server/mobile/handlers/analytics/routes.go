package analytics

import (
	"context"
	"go-api-server/internal/database/analytics"
	"go-api-server/internal/middleware"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Repository interface {
	UserAnalytics(context.Context, string) (*analytics.UserAnalytics, error)
}

type Handler struct {
	DB Repository
}

func toAnalyticsResponse(analytics *analytics.UserAnalytics) *AnalyticsResponse {
	return &AnalyticsResponse{
		TotalFarmers: analytics.TotalFarmers,
		TotalAnimals: analytics.TotalAnimals,
		Assigned: AnimalGroup{
			Total: analytics.AssignedMale + analytics.AssignedFemale,
			Gender: GenderDistribution{
				Male:   analytics.AssignedMale,
				Female: analytics.AssignedFemale,
			},
		},
		Unassigned: AnimalGroup{
			Total: analytics.UnassignedMale + analytics.UnassignedFemale,
			Gender: GenderDistribution{
				Male:   analytics.UnassignedMale,
				Female: analytics.UnassignedFemale,
			},
		},
	}
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	analyticsGroup := g.Group("/analytics")
	analyticsGroup.GET("", h.analytics)
}

func (h *Handler) analytics(c echo.Context) error {
	ctx := c.Request().Context()
	userID := middleware.UserIDFromContext(c)

	userAnalytics, err := h.DB.UserAnalytics(ctx, userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch user analytics").SetInternal(err)
	}
	return c.JSON(http.StatusOK, toAnalyticsResponse(userAnalytics))
}
