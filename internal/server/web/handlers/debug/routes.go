package debug

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	debugdb "go-api-server/internal/database/debug"
	"go-api-server/internal/middleware"
	"go-api-server/internal/storage"

	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

type Repository interface {
	ListRegistrationFailures(context.Context) ([]debugdb.RegistrationFailureRow, error)
	ListSearches(context.Context) ([]debugdb.SearchRecordRow, error)
	UpdateSearchVerification(context.Context, int64, string) (*debugdb.SearchRecordRow, error)
}

type Handler struct {
	DB      Repository
	Storage storage.Storage
}

// Both listings return everything, unpaginated, because the dashboard shows
// them on a single screen. That holds while these tables stay small; the
// created_at/id indexes are in place for keyset pagination when it stops being
// true.
func RegisterRoutes(g *echo.Group, h *Handler) {
	debugGroup := g.Group("/debug", middleware.RequireRole("developer"))
	debugGroup.GET("/registrations", h.listRegistrationFailures)
	debugGroup.GET("/searches", h.listSearches)
	debugGroup.PATCH("/searches/:id/verify", h.verifySearch)
}

func (h *Handler) listRegistrationFailures(c echo.Context) error {
	ctx := c.Request().Context()

	rows, err := h.DB.ListRegistrationFailures(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list registration failures").SetInternal(err)
	}

	out := make([]RegistrationFailureResponse, len(rows))
	urls, err := h.presignRows(ctx, len(rows), func(i int) []string { return rows[i].ImageKeys })
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
	}

	for i, r := range rows {
		out[i] = RegistrationFailureResponse{
			ID:        r.ID,
			ErrorCode: r.ErrorCode,
			ImageURLs: urls[i],
			Detail:    r.Detail,
			Device:    toDeviceResponse(r.DeviceColumns),
			CreatedBy: r.CreatedBy,
			CreatedAt: r.CreatedAt,
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) listSearches(c echo.Context) error {
	ctx := c.Request().Context()

	rows, err := h.DB.ListSearches(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list searches").SetInternal(err)
	}

	urls, err := h.presignRows(ctx, len(rows), func(i int) []string { return rows[i].ImageKeys })
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
	}

	out := make([]SearchRecordResponse, len(rows))
	for i := range rows {
		matched, err := h.toMatchedAnimal(ctx, &rows[i])
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
		}
		out[i] = SearchRecordResponse{
			ID:        rows[i].ID,
			Decision:  rows[i].Decision,
			Score:     rows[i].Score,
			ErrorCode: rows[i].ErrorCode,
			Verified:  rows[i].Verified,
			Matched:   matched,
			ImageURLs: urls[i],
			Detail:    rows[i].Detail,
			Device:    toDeviceResponse(rows[i].DeviceColumns),
			CreatedBy: rows[i].CreatedBy,
			CreatedAt: rows[i].CreatedAt,
		}
	}
	return c.JSON(http.StatusOK, out)
}

// verifySearch records, or revises, a developer's judgement on a match. It is
// deliberately reversible: yes can become no and back again, because this is
// exactly the kind of call that gets revisited once more examples are seen.
func (h *Handler) verifySearch(c echo.Context) error {
	ctx := c.Request().Context()

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid search record id")
	}

	var req VerifyRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body").SetInternal(err)
	}
	if !debugdb.ValidVerification(req.Verified) {
		return echo.NewHTTPError(http.StatusBadRequest, "verified must be one of: yes, no, not_verified")
	}

	row, err := h.DB.UpdateSearchVerification(ctx, id, req.Verified)
	if err != nil {
		switch {
		case errors.Is(err, debugdb.ErrRecordNotFound):
			return echo.NewHTTPError(http.StatusNotFound, "search record not found")
		case errors.Is(err, debugdb.ErrNotVerifiable):
			return echo.NewHTTPError(http.StatusConflict, "only a matched search can be verified")
		default:
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to update verification").SetInternal(err)
		}
	}

	urls, err := h.presignKeys(ctx, row.ImageKeys)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
	}
	matched, err := h.toMatchedAnimal(ctx, row)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
	}

	return c.JSON(http.StatusOK, SearchRecordResponse{
		ID:        row.ID,
		Decision:  row.Decision,
		Score:     row.Score,
		ErrorCode: row.ErrorCode,
		Verified:  row.Verified,
		Matched:   matched,
		ImageURLs: urls,
		Detail:    row.Detail,
		Device:    toDeviceResponse(row.DeviceColumns),
		CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt,
	})
}

func (h *Handler) toMatchedAnimal(ctx context.Context, r *debugdb.SearchRecordRow) (*MatchedAnimalResponse, error) {
	if r.GodhaarID == nil {
		return nil, nil
	}

	matched := &MatchedAnimalResponse{
		GodhaarID:   *r.GodhaarID,
		Type:        r.MatchedType,
		Breed:       r.MatchedBreed,
		Gender:      r.MatchedGender,
		Age:         r.MatchedAge,
		BodyColor:   r.MatchedBodyColor,
		MuzzleColor: r.MatchedMuzzleColor,
		HornShape:   r.MatchedHornShape,
		Village:     r.MatchedVillage,
		Mandal:      r.MatchedMandal,
		District:    r.MatchedDistrict,
		State:       r.MatchedState,
		// The join produced no row, so the animal this search pointed at is
		// gone. The record still stands as evidence of what the model said.
		Deleted: r.MatchedType == nil,
	}

	if r.MatchedImageKey != nil {
		url, err := h.Storage.PresignedURL(ctx, *r.MatchedImageKey)
		if err != nil {
			return nil, err
		}
		matched.ImageURL = &url
	}
	return matched, nil
}

func toDeviceResponse(d debugdb.DeviceColumns) DeviceResponse {
	return DeviceResponse{
		AppVersion:         d.AppVersion,
		OSVersion:          d.OSVersion,
		DeviceModel:        d.DeviceModel,
		DeviceManufacturer: d.DeviceManufacturer,
	}
}

// presignRows signs every row's images concurrently. Signing is a local
// operation for both storage backends, but a full listing multiplies it by the
// number of records, so it is worth not doing serially.
func (h *Handler) presignRows(ctx context.Context, n int, keysAt func(int) []string) ([][]string, error) {
	out := make([][]string, n)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i := range n {
		g.Go(func() error {
			urls, err := h.presignKeys(gctx, keysAt(i))
			if err != nil {
				return err
			}
			out[i] = urls
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handler) presignKeys(ctx context.Context, keys []string) ([]string, error) {
	urls := make([]string, 0, len(keys))
	for _, key := range keys {
		url, err := h.Storage.PresignedURL(ctx, key)
		if err != nil {
			return nil, err
		}
		urls = append(urls, url)
	}
	return urls, nil
}
