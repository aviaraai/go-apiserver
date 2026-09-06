package animal

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	debugdb "go-api-server/internal/database/debug"
	"go-api-server/internal/imaging"
	"go-api-server/internal/inference"
	"go-api-server/internal/middleware"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

// Recording is telemetry, so nothing here fails a request. If a capture fails,
// the registration was still a duplicate and the search still found what it
// found; reporting a storage outage as though the identification itself had
// failed would be actively misleading.
//
// These functions return their problems instead of logging them. Every log line
// in this service is emitted from customHTTPErrorHandler, or from the single
// site in search() — a capture error travels back to whichever of those is
// about to run, so the signal survives without a second logger appearing here.

// captureRegistrationFailure records a registration the model refused.
//
// Only model verdicts are recorded. A transport or contract fault says nothing
// about the animal or the photos, so keeping the images would add noise to the
// dashboard and cost storage during exactly the outages when it is least
// affordable.
func (h *Handler) captureRegistrationFailure(c echo.Context, infErr *inference.Error, front, muzzle []*imageFile, matchedGodhaarID string) error {
	if infErr.Class != inference.ClassDomain {
		return nil
	}

	ctx := c.Request().Context()
	keys, uploadErr := h.uploadDebugImages(ctx, failureUploadTasks(front, muzzle))

	detail := inferenceDetail(infErr)
	// The raw payload only carries a FAISS id, which means nothing on the
	// dashboard. Recording the resolved animal here saves every reader having
	// to work out that mapping — and it cannot be recovered later, since it
	// depends on which candidates were nearby at the time.
	if matchedGodhaarID != "" {
		detail[debugdb.DetailKeyMatchedGodhaarID] = matchedGodhaarID
	}

	record := debugdb.CreateRegistrationFailure{
		DeviceColumns:  deviceColumns(c),
		ErrorCode:      string(infErr.Code),
		ImageKeys:      keys,
		Detail:         detail,
		CreatedBy:      middleware.UserIDFromContext(c),
		CreatedByEmail: middleware.EmailFromContext(c),
	}
	return errors.Join(uploadErr, h.DebugDB.RecordRegistrationFailure(ctx, record))
}

// captureSearch records a search that produced a verdict — including the ones
// that found nothing, and the ones that found something.
//
// Successes are kept because a search that confidently returns the wrong animal
// is the failure mode worth catching, and from the server's side it is
// indistinguishable from a correct one. Only a human looking at the photos next
// to the matched animal can tell them apart, which is what the verified column
// on these rows is for.
func (h *Handler) captureSearch(c echo.Context, v verdict, front *imageFile, muzzle []*imageFile) error {
	ctx := c.Request().Context()
	keys, uploadErr := h.uploadDebugImages(ctx, failureUploadTasks(
		[]*imageFile{front}, muzzle))

	score := v.Score
	record := debugdb.CreateSearchRecord{
		DeviceColumns: deviceColumns(c),
		Decision:      v.Decision,
		Score:         &score,
		ImageKeys:     keys,
		Detail: debugdb.JSONMap{
			"reason":         v.Reason,
			"adjusted_score": v.AdjustedScore,
			"gap":            v.Gap,
			"agreement":      v.Agreement,
			"top_k":          searchTopK,
			"radius_km":      searchRadiusKm,
		},
		CreatedBy:      middleware.UserIDFromContext(c),
		CreatedByEmail: middleware.EmailFromContext(c),
	}
	// The table only accepts a godhaar_id on a MATCH: a REVIEW is explicitly
	// not a claim about which animal this is, and promoting it to the column
	// would make it verifiable as though it were.
	//
	// The near miss still goes into detail, because adjusted_score, gap and
	// agreement all describe that one candidate and say nothing without it.
	if v.Decision == debugdb.DecisionMatch {
		record.GodhaarID = v.GodhaarID
	} else if v.GodhaarID != nil {
		record.Detail["top_candidate"] = *v.GodhaarID
	}

	return errors.Join(uploadErr, h.DebugDB.RecordSearch(ctx, record))
}

// captureSearchFailure records a search the model refused to answer. Code and
// server faults are skipped for the same reason as on the registration path.
func (h *Handler) captureSearchFailure(c echo.Context, infErr *inference.Error, front *imageFile, muzzle []*imageFile) error {
	if infErr.Class != inference.ClassDomain {
		return nil
	}

	ctx := c.Request().Context()
	keys, uploadErr := h.uploadDebugImages(ctx, failureUploadTasks(
		[]*imageFile{front}, muzzle))

	code := string(infErr.Code)
	record := debugdb.CreateSearchRecord{
		DeviceColumns:  deviceColumns(c),
		Decision:       debugdb.DecisionFailed,
		ErrorCode:      &code,
		ImageKeys:      keys,
		Detail:         inferenceDetail(infErr),
		CreatedBy:      middleware.UserIDFromContext(c),
		CreatedByEmail: middleware.EmailFromContext(c),
	}
	return errors.Join(uploadErr, h.DebugDB.RecordSearch(ctx, record))
}

func deviceColumns(c echo.Context) debugdb.DeviceColumns {
	device, ok := middleware.DeviceInfoFromContext(c)
	if !ok {
		return debugdb.DeviceColumns{}
	}
	return debugdb.DeviceColumns{
		AppVersion:         nullIfEmpty(device.AppVersion),
		OSVersion:          nullIfEmpty(device.OSVersion),
		DeviceModel:        nullIfEmpty(device.Model),
		DeviceManufacturer: nullIfEmpty(device.Manufacturer),
	}
}

// inferenceDetail assembles the jsonb payload for a model rejection. These
// tables are developer-only, so this carries the internal error text verbatim:
// it is the single most useful thing when working out why a batch of
// registrations started failing, and this is deliberately the one place it is
// kept.
func inferenceDetail(infErr *inference.Error) debugdb.JSONMap {
	detail := debugdb.JSONMap{
		"class":           infErr.Class.String(),
		"upstream_status": infErr.StatusCode,
	}
	if infErr.RawError != nil {
		detail["internal_error"] = infErr.RawError.Error()
	}
	if len(infErr.Detail) > 0 {
		detail["inference"] = infErr.Detail
	}
	return detail
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// uploadDebugImages writes the attempt's images under a fresh folder and
// returns the keys written. On failure it returns no keys and the reason: a row
// with no images is still worth far more than no row at all, so an upload
// problem downgrades the record rather than abandoning it.
//
// Only the keys are returned, not the folder. The extension depends on each
// image's detected content type, so keys cannot be rebuilt from a folder — and
// the folder is just their common prefix, so storing both would only create two
// things that can disagree.
func (h *Handler) uploadDebugImages(ctx context.Context, tasks []uploadTask) (debugdb.JSONStrings, error) {
	uuidV7, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate debug folder id: %w", err)
	}
	// The UUIDv7 keeps the folder listing roughly time-ordered, matching how
	// the records are read.
	folder := fmt.Sprintf("debug/%s", uuidV7.String())

	keys := make([]string, len(tasks))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(7)

	for i := range tasks {
		g.Go(func() error {
			ext, err := imaging.ExtensionForContentType(tasks[i].img.validated)
			if err != nil {
				return fmt.Errorf("%s: %w", tasks[i].imageType, err)
			}
			// Key format: debug/{uuid}/{image_type}{image_sequence}{image_ext}
			key := fmt.Sprintf("%s/%s%d%s", folder, tasks[i].imageType, tasks[i].sequence, ext)
			if err := h.Storage.Upload(gctx, bytes.NewReader(tasks[i].img.data), key, tasks[i].img.validated); err != nil {
				return fmt.Errorf("upload %s: %w", tasks[i].imageType, err)
			}
			keys[i] = key
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("upload debug images: %w", err)
	}
	return keys, nil
}

// failureUploadTasks lists the images worth keeping: the ones the inference
// server actually looked at. Side and certificate images play no part in
// identification, so they are left out.
func failureUploadTasks(front, muzzle []*imageFile) []uploadTask {
	tasks := make([]uploadTask, 0, len(front)+len(muzzle))
	for i, f := range front {
		tasks = append(tasks, uploadTask{imageType: "front", sequence: i + 1, img: f})
	}
	for i, m := range muzzle {
		tasks = append(tasks, uploadTask{imageType: "muzzle", sequence: i + 1, img: m})
	}
	return tasks
}
