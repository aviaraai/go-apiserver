package farmer

import (
	"context"
	"errors"
	"fmt"
	"go-api-server/internal/database/farmer"
	"go-api-server/internal/id"
	"go-api-server/internal/imaging"
	"go-api-server/internal/middleware"
	"go-api-server/internal/storage"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
)

type Repository interface {
	Create(context.Context, *farmer.CreateFarmer) (*farmer.Farmer, error)
	FarmersByPhoneNumber(context.Context, string) ([]farmer.Farmer, error)
	FarmersByUser(context.Context, string) ([]farmer.Farmer, error)
	Get(context.Context, string) (*farmer.Farmer, error)
}

type Handler struct {
	DB      Repository
	Storage storage.Storage
}

func toFarmerResponse(farmer *farmer.Farmer) *FarmerResponse {
	return &FarmerResponse{
		PublicID:     farmer.PublicID,
		Name:         farmer.Name,
		Type:         farmer.Type,
		Relation:     farmer.Relation,
		RelationName: farmer.RelationName,
		State:        farmer.State,
		District:     farmer.District,
		Mandal:       farmer.Mandal,
		Village:      farmer.Village,
		PhoneNumber:  farmer.PhoneNumber,
	}
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	farmerGroup := g.Group("/farmers")
	farmerGroup.POST("", h.addFarmer)
	farmerGroup.PUT("", h.updateFarmer)
	farmerGroup.GET("", h.listFarmers)
	farmerGroup.GET("/:public_id", h.getFarmer)
	farmerGroup.GET("/:public_id/photo", h.getFarmerPhoto)
}

func (h *Handler) addFarmer(c echo.Context) error {
	ctx := c.Request().Context()
	userID := middleware.UserIDFromContext(c)
	email := middleware.EmailFromContext(c)

	var req AddFarmerRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body").SetInternal(err)
	}

	file, err := c.FormFile("farmer_photo")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "farmer photo is required").SetInternal(err)
	}

	src, err := file.Open()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read uploaded photo").SetInternal(err)
	}
	defer src.Close()

	contentType, err := imaging.ValidateImage(src)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid photo")
	}
	src.Seek(0, io.SeekStart)

	ext, err := imaging.ExtensionForContentType(contentType)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "unsupported image format")
	}

	publicID, err := id.GeneratePublicID(req.State, req.District)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate farmer id").SetInternal(err)
	}

	key := fmt.Sprintf("farmer/%s/photo%s", publicID, ext)
	if err = h.Storage.Upload(ctx, src, key, file.Header.Get("Content-Type")); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to upload photo").SetInternal(err)
	}

	createFarmer := farmer.CreateFarmer{
		PublicID:       publicID,
		Name:           req.Name,
		Type:           req.Type,
		Relation:       req.Relation,
		RelationName:   req.RelationName,
		State:          req.State,
		District:       req.District,
		Mandal:         req.Mandal,
		Village:        req.Village,
		PhoneNumber:    req.PhoneNumber,
		PhotoKey:       key,
		Latitude:       req.Latitude,
		Longitude:      req.Longitude,
		CreatedBy:      userID,
		CreatedByEmail: email,
		UpdatedBy:      userID,
		UpdatedByEmail: email,
	}

	f, err := h.DB.Create(ctx, &createFarmer)
	if err != nil {
		switch {
		case errors.Is(err, farmer.ErrDuplicatePhone):
			return echo.NewHTTPError(http.StatusConflict, err.Error()).SetInternal(err)
		case errors.Is(err, farmer.ErrInvalidFarmerData):
			return echo.NewHTTPError(http.StatusBadRequest, err.Error()).SetInternal(err)
		case errors.Is(err, farmer.ErrCreateNoRowReturned):
			return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
		default:
			return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
		}
	}

	return c.JSON(http.StatusCreated, toFarmerResponse(f))
}

func (h *Handler) updateFarmer(c echo.Context) error {
	return nil
}

func (h *Handler) listFarmers(c echo.Context) error {
	ctx := c.Request().Context()
	userID := middleware.UserIDFromContext(c)

	var req FarmerQueryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	var farmers []farmer.Farmer
	var err error

	if req.PhoneNumber != nil {
		farmers, err = h.DB.FarmersByPhoneNumber(ctx, *req.PhoneNumber)
	} else {
		farmers, err = h.DB.FarmersByUser(ctx, userID)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list farmers").SetInternal(err)
	}

	farmersRes := make([]FarmerResponse, len(farmers))
	for i := range farmers {
		farmersRes[i] = *toFarmerResponse(&farmers[i])
	}
	return c.JSON(http.StatusOK, farmersRes)
}

func (h *Handler) getFarmer(c echo.Context) error {
	ctx := c.Request().Context()

	var req FarmerRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing farmer id")
	}

	f, err := h.DB.Get(ctx, req.PublicID)
	if err != nil {
		if errors.Is(err, farmer.ErrRowNotFound) {
			return echo.NewHTTPError(http.StatusBadRequest, "farmer not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	return c.JSON(http.StatusOK, toFarmerResponse(f))
}

func (h *Handler) getFarmerPhoto(c echo.Context) error {
	ctx := c.Request().Context()

	var req FarmerRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing farmer id")
	}

	f, err := h.DB.Get(ctx, req.PublicID)
	if err != nil {
		if errors.Is(err, farmer.ErrRowNotFound) {
			return echo.NewHTTPError(http.StatusBadRequest, "farmer not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	photoURL, err := h.Storage.PresignedURL(ctx, f.PhotoKey)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	return c.JSON(http.StatusOK, FarmerPhotoResponse{PhotoURL: photoURL})
}
