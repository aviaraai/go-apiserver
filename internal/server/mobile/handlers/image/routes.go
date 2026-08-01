package image

import (
	"context"
	"errors"
	"go-api-server/internal/database/image"
	"go-api-server/internal/storage"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Repository interface {
	AnimalIDByGodhaarID(context.Context, string) (int64, error)
	ImagesByAnimalID(context.Context, int64) ([]image.ImageRow, error)
}

type Handler struct {
	DB      Repository
	Storage storage.Storage
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	g.GET("/:godhaar_id/images", h.images)
}

func (h *Handler) images(c echo.Context) error {
	ctx := c.Request().Context()

	var req ImageRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing godhaar id")
	}

	animalID, err := h.DB.AnimalIDByGodhaarID(ctx, req.GodhaarID)
	if err != nil {
		if errors.Is(err, image.ErrAnimalNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "animal not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	rows, err := h.DB.ImagesByAnimalID(ctx, animalID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch images").SetInternal(err)
	}

	resp := make([]ImageResponse, len(rows))
	for i := range rows {
		url, err := h.Storage.PresignedURL(ctx, rows[i].Key)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch images").SetInternal(err)
		}
		resp[i].Type = rows[i].Type
		resp[i].URL = url
	}
	return c.JSON(http.StatusOK, resp)
}
