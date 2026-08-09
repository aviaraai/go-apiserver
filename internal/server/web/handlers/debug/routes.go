package debug

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"

	debugdb "go-api-server/internal/database/debug"
	"go-api-server/internal/middleware"
	"go-api-server/internal/storage"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

type Repository interface {
	ListRegistrationFailures(context.Context) ([]debugdb.RegistrationFailureListRow, error)
	RegistrationFailureByID(context.Context, string) (*debugdb.RegistrationFailureRow, error)
	ListSearches(context.Context) ([]debugdb.SearchRecordListRow, error)
	SearchByID(context.Context, string) (*debugdb.SearchRecordRow, error)
	MatchedAnimalImages(context.Context, string, []string) ([]debugdb.AnimalImage, bool, error)
	UpdateSearchVerification(context.Context, string, string) (*debugdb.SearchRecordRow, error)
}

type Handler struct {
	DB      Repository
	Storage storage.Storage
}

// The dashboard filters client-side, so each listing returns every row and the
// detail endpoints carry the payloads a card never shows. Both listings are
// unpaginated, which holds while these tables stay small; the created_at/id
// indexes are in place for keyset pagination when it stops being true.
func RegisterRoutes(g *echo.Group, h *Handler) {
	debugGroup := g.Group("/debug", middleware.RequireRole("developer"))
	debugGroup.GET("/registrations", h.listRegistrationFailures)
	debugGroup.GET("/registrations/:registration_id", h.getRegistrationFailure)
	debugGroup.GET("/searches", h.listSearches)
	debugGroup.GET("/searches/:search_id", h.getSearch)
	debugGroup.PATCH("/searches/:search_id/verify", h.verifySearch)
}

func (h *Handler) listRegistrationFailures(c echo.Context) error {
	ctx := c.Request().Context()

	rows, err := h.DB.ListRegistrationFailures(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list registration failures").SetInternal(err)
	}

	thumbs, err := h.presignThumbnails(ctx, len(rows), func(i int) []string { return rows[i].ImageKeys })
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
	}

	out := make([]RegistrationFailureListItem, len(rows))
	for i, r := range rows {
		out[i] = RegistrationFailureListItem{
			RegistrationID: r.RegistrationID,
			ErrorCode:      r.ErrorCode,
			ThumbnailURL:   thumbs[i],
			Device:         toDeviceResponse(r.DeviceColumns),
			CreatedByEmail: r.CreatedByEmail,
			CreatedAt:      r.CreatedAt,
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) getRegistrationFailure(c echo.Context) error {
	ctx := c.Request().Context()

	registrationID, err := pathUUID(c, "registration_id")
	if err != nil {
		return err
	}

	row, err := h.DB.RegistrationFailureByID(ctx, registrationID)
	if err != nil {
		if errors.Is(err, debugdb.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "registration failure not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load registration failure").SetInternal(err)
	}

	images, err := h.presignDebugImages(ctx, row.ImageKeys)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
	}

	// A duplicate rejection is only reviewable next to the animal it was
	// rejected in favour of: the id alone tells a reader which record to go and
	// look up, which is the work this saves them.
	matched, err := h.matchedAnimal(ctx, debugdb.MatchedGodhaarID(row.Detail), debugdb.DuplicateSlots)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load matched animal").SetInternal(err)
	}

	return c.JSON(http.StatusOK, RegistrationFailureDetailResponse{
		RegistrationID: row.RegistrationID,
		ErrorCode:      row.ErrorCode,
		Images:         images,
		Matched:        matched,
		Detail:         row.Detail,
		Device:         toDeviceResponse(row.DeviceColumns),
		CreatedByEmail: row.CreatedByEmail,
		CreatedAt:      row.CreatedAt,
	})
}

func (h *Handler) listSearches(c echo.Context) error {
	ctx := c.Request().Context()

	rows, err := h.DB.ListSearches(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list searches").SetInternal(err)
	}

	thumbs, err := h.presignThumbnails(ctx, len(rows), func(i int) []string { return rows[i].ImageKeys })
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
	}

	out := make([]SearchListItem, len(rows))
	for i, r := range rows {
		out[i] = SearchListItem{
			SearchID:       r.SearchID,
			Decision:       r.Decision,
			Verified:       r.Verified,
			Score:          r.Score,
			ErrorCode:      r.ErrorCode,
			GodhaarID:      r.GodhaarID,
			ThumbnailURL:   thumbs[i],
			Device:         toDeviceResponse(r.DeviceColumns),
			CreatedByEmail: r.CreatedByEmail,
			CreatedAt:      r.CreatedAt,
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) getSearch(c echo.Context) error {
	ctx := c.Request().Context()

	searchID, err := pathUUID(c, "search_id")
	if err != nil {
		return err
	}

	row, err := h.DB.SearchByID(ctx, searchID)
	if err != nil {
		if errors.Is(err, debugdb.ErrRecordNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "search record not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load search record").SetInternal(err)
	}

	return h.respondWithSearchDetail(c, row)
}

// verifySearch records, or revises, a developer's judgement on a match. It is
// deliberately reversible: yes can become no and back again, because this is
// exactly the kind of call that gets revisited once more examples are seen.
func (h *Handler) verifySearch(c echo.Context) error {
	ctx := c.Request().Context()

	searchID, err := pathUUID(c, "search_id")
	if err != nil {
		return err
	}

	var req VerifyRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body").SetInternal(err)
	}
	if !debugdb.ValidVerification(req.Verified) {
		return echo.NewHTTPError(http.StatusBadRequest, "verified must be one of: yes, no, not_verified")
	}

	row, err := h.DB.UpdateSearchVerification(ctx, searchID, req.Verified)
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

	// Answering with the same shape as GET /searches/:search_id means the
	// dashboard can swap the card it just acted on without a refetch.
	return h.respondWithSearchDetail(c, row)
}

func (h *Handler) respondWithSearchDetail(c echo.Context, row *debugdb.SearchRecordRow) error {
	ctx := c.Request().Context()

	images, err := h.presignDebugImages(ctx, row.ImageKeys)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to sign image urls").SetInternal(err)
	}
	matched, err := h.matchedAnimal(ctx, row.GodhaarID, debugdb.SearchSlots)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load matched animal").SetInternal(err)
	}

	return c.JSON(http.StatusOK, SearchDetailResponse{
		SearchID:       row.SearchID,
		Decision:       row.Decision,
		Verified:       row.Verified,
		Score:          row.Score,
		ErrorCode:      row.ErrorCode,
		Images:         images,
		Matched:        matched,
		Detail:         row.Detail,
		Device:         toDeviceResponse(row.DeviceColumns),
		CreatedByEmail: row.CreatedByEmail,
		CreatedAt:      row.CreatedAt,
	})
}

// matchedAnimal resolves the animal a record named, if it named one, and signs
// the given slots of its photos.
//
// Signing is serial here where the listings sign concurrently: a match is at
// most a handful of photos, against a listing's one per row over the whole
// table.
func (h *Handler) matchedAnimal(ctx context.Context, godhaarID *string, slots []string) (*MatchedAnimalResponse, error) {
	if godhaarID == nil {
		return nil, nil
	}

	stored, found, err := h.DB.MatchedAnimalImages(ctx, *godhaarID, slots)
	if err != nil {
		return nil, err
	}
	// The animal this record pointed at is gone. The record still stands as
	// evidence of what the model said, so the id is reported without it.
	if !found {
		return &MatchedAnimalResponse{
			GodhaarID: *godhaarID,
			Images:    []ImageResponse{},
			Deleted:   true,
		}, nil
	}

	images := make([]ImageResponse, len(stored))
	for i, img := range stored {
		url, err := h.Storage.PresignedURL(ctx, img.ImageKey)
		if err != nil {
			return nil, err
		}
		images[i] = ImageResponse{Slot: img.ImageType, Sequence: img.Sequence, URL: url}
	}

	return &MatchedAnimalResponse{GodhaarID: *godhaarID, Images: images}, nil
}

func toDeviceResponse(d debugdb.DeviceColumns) DeviceResponse {
	return DeviceResponse{
		AppVersion:         d.AppVersion,
		OSVersion:          d.OSVersion,
		DeviceModel:        d.DeviceModel,
		DeviceManufacturer: d.DeviceManufacturer,
	}
}

// pathUUID reads and validates a record id from the path.
//
// Validating here rather than letting the database reject it is what keeps a
// malformed id a 400 instead of a 500: an unparseable uuid reaches Postgres as
// an invalid-input error, which is indistinguishable from a real fault by the
// time it comes back.
func pathUUID(c echo.Context, name string) (string, error) {
	raw := c.Param(name)
	if _, err := uuid.Parse(raw); err != nil {
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid "+name)
	}
	return raw, nil
}

// presignThumbnails signs one image per row — the card only shows the first —
// concurrently. Signing is a local operation for both storage backends, but a
// full listing multiplies it by the number of records, so it is worth not doing
// serially.
func (h *Handler) presignThumbnails(ctx context.Context, n int, keysAt func(int) []string) ([]*string, error) {
	out := make([]*string, n)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i := range n {
		g.Go(func() error {
			keys := keysAt(i)
			if len(keys) == 0 {
				return nil
			}
			url, err := h.Storage.PresignedURL(gctx, keys[0])
			if err != nil {
				return err
			}
			out[i] = &url
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// presignDebugImages signs a record's photos and labels each with the slot it
// was captured for.
func (h *Handler) presignDebugImages(ctx context.Context, keys []string) ([]ImageResponse, error) {
	images := make([]ImageResponse, 0, len(keys))
	for _, key := range keys {
		url, err := h.Storage.PresignedURL(ctx, key)
		if err != nil {
			return nil, err
		}
		slot, sequence := slotFromKey(key)
		images = append(images, ImageResponse{Slot: slot, Sequence: sequence, URL: url})
	}
	return images, nil
}

// debugSlots are the slots a debug capture can hold. Side and certificate
// images are never captured — only what the model was actually shown.
var debugSlots = []string{"front", "muzzle", "left", "right"}

// slotFromKey recovers which photo a debug object key holds.
//
// The keys are written as debug/{uuid}/{slot}{sequence}{ext}, so the slot is
// carried by the name rather than by a column. That is worth reading back
// rather than storing twice, but it is also the kind of thing that breaks
// quietly, so an unrecognised name degrades to "unknown" instead of failing the
// request — a photo whose slot cannot be read is still worth showing.
func slotFromKey(key string) (string, int) {
	base := strings.TrimSuffix(path.Base(key), path.Ext(key))

	for _, slot := range debugSlots {
		if !strings.HasPrefix(base, slot) {
			continue
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(base, slot))
		if err != nil {
			return "unknown", 0
		}
		return slot, sequence
	}
	return "unknown", 0
}
