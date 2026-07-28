package qr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

	"go-api-server/internal/crypto"
	"go-api-server/internal/database/qr"
	"go-api-server/internal/storage"

	"github.com/labstack/echo/v4"
	"github.com/skip2/go-qrcode"
)

type Repository interface {
	AnimalIDByGodhaarID(context.Context, string) (int64, error)
	QRByAnimalID(context.Context, int64) (*qr.QRRow, error)
	Create(context.Context, *qr.CreateQR) (*qr.QRRow, error)
}

type Handler struct {
	DB      Repository
	Storage storage.Storage
	QRKey   []byte
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	g.POST("/:godhaar_id/qr/generate", h.generateQRCode)
	g.GET("/qr/:qr_token/scan", h.scanQRCode)
	g.GET("/:godhaar_id/qr", h.getQRCode)
}

func (h *Handler) generateQRCode(c echo.Context) error {
	ctx := c.Request().Context()

	var req QRRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing godhaar id")
	}

	animalID, err := h.DB.AnimalIDByGodhaarID(ctx, req.GodhaarID)
	if err != nil {
		if errors.Is(err, qr.ErrAnimalNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "animal not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	_, err = h.DB.QRByAnimalID(ctx, animalID)
	if err == nil {
		return echo.NewHTTPError(http.StatusConflict, "qr code already exists for this animal")
	}
	if !errors.Is(err, qr.ErrQRNotFound) {
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	qrContent, err := crypto.Encrypt(h.QRKey, req.GodhaarID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate QR").SetInternal(err)
	}

	png, err := qrcode.Encode(qrContent, qrcode.Medium, 512)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate QR").SetInternal(err)
	}

	key := fmt.Sprintf("qr/%s/qr.png", req.GodhaarID)
	if err := h.Storage.Upload(ctx, bytes.NewReader(png), key, "image/png"); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to upload QR").SetInternal(err)
	}

	_, err = h.DB.Create(ctx, &qr.CreateQR{AnimalID: animalID, Key: key})
	if err != nil {
		if errors.Is(err, qr.ErrQRAlreadyExists) {
			return echo.NewHTTPError(http.StatusConflict, "qr code already exists for this animal")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save QR").SetInternal(err)
	}

	return c.NoContent(http.StatusCreated)
}

func (h *Handler) scanQRCode(c echo.Context) error {
	var req QRScanRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "qr token missing")
	}

	godhaarID, err := crypto.Decrypt(h.QRKey, req.Token)
	if err != nil {
		if errors.Is(err, crypto.ErrInvalidToken) {

			return echo.NewHTTPError(http.StatusBadRequest, "invalid or tampered token")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	return c.JSON(http.StatusOK, QRScanResponse{GodhaarID: godhaarID})
}

func (h *Handler) getQRCode(c echo.Context) error {
	ctx := c.Request().Context()

	var req QRRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "godhaar id missing")
	}

	animalID, err := h.DB.AnimalIDByGodhaarID(ctx, req.GodhaarID)
	if err != nil {
		if errors.Is(err, qr.ErrAnimalNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "animal not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	qrRow, err := h.DB.QRByAnimalID(ctx, animalID)
	if err != nil {
		if errors.Is(err, qr.ErrQRNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "qr code not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error").SetInternal(err)
	}

	preSignedURL, err := h.Storage.PresignedURL(ctx, qrRow.Key)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to obtain QR").SetInternal(err)
	}

	return c.JSON(http.StatusOK, QRResponse{PreSignedURL: preSignedURL})
}
