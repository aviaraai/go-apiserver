package cctv

import (
	"context"
	"errors"
	"net/http"

	"go-api-server/internal/cctv"
	cctvdb "go-api-server/internal/database/cctv"
	"go-api-server/internal/httperr"
	"go-api-server/internal/inference"

	"github.com/labstack/echo/v4"
)

// Failure codes this handler produces. The inference-side codes
// (CCTV_ANALYSIS_FAILED, CCTV_TIMEOUT, INFERENCE_*) pass through unchanged.
const (
	codeSourceUnavailable = "CCTV_SOURCE_UNAVAILABLE"
	codeStorageFailed     = "CCTV_STORAGE_FAILED"
	codeResultNotSaved    = "CCTV_RESULT_NOT_SAVED"

	// Upload rejections. These are the admin's to fix, so each names the
	// problem specifically enough for the dashboard to say what to do.
	codeVideoRequired          = "CCTV_VIDEO_REQUIRED"
	codeVideoTooLarge          = "CCTV_VIDEO_TOO_LARGE"
	codeVideoUnsupportedFormat = "CCTV_VIDEO_UNSUPPORTED_FORMAT"
	codeVideoUnreadable        = "CCTV_VIDEO_UNREADABLE"
)

// ErrResultNotSaved marks an analysis that ran to completion but whose result
// could not be written back.
//
// It is its own failure rather than a generic storage error because the two mean
// different things to whoever reads the history: nothing was wrong with the
// video or the model, and the annotated clip is sitting in storage — only the
// record of it is missing.
var ErrResultNotSaved = errors.New("analysis completed but the result could not be saved")

// ErrUploadUnreadable marks an uploaded clip that could not be opened for
// reading — a truncated transfer, or a spool file that went missing under us.
// It surfaces only after the request record exists, which is why it is an error
// classified here rather than a rejection returned straight from the handler.
var ErrUploadUnreadable = errors.New("uploaded video could not be read")

// failureResponse maps a failure to what the admin is told. As everywhere else
// in this service, the message is ours and the underlying error stays internal.
type failureResponse struct {
	status  int
	code    string
	message string
}

func classifyFailure(err error) failureResponse {
	if errors.Is(err, ErrResultNotSaved) {
		return failureResponse{
			http.StatusInternalServerError, codeResultNotSaved,
			"The analysis finished but could not be saved.",
		}
	}

	if errors.Is(err, ErrUploadUnreadable) {
		return failureResponse{
			http.StatusBadRequest, codeVideoUnreadable,
			"That video could not be read. Please try uploading it again.",
		}
	}

	if errors.Is(err, cctv.ErrSourceNotImplemented) {
		return failureResponse{
			http.StatusServiceUnavailable, codeSourceUnavailable,
			"Camera access is not configured yet, so no video could be pulled for this goshala.",
		}
	}

	var infErr *inference.Error
	if errors.As(err, &infErr) {
		switch {
		case infErr.Code == inference.CodeCCTVAnalysisFailed:
			return failureResponse{
				http.StatusUnprocessableEntity, string(infErr.Code),
				"The camera video could not be analysed. It may be corrupt or in an unsupported format.",
			}
		case infErr.Code == inference.CodeCCTVTimeout:
			return failureResponse{
				http.StatusGatewayTimeout, string(infErr.Code),
				"The analysis took too long and was stopped. Try a shorter clip.",
			}
		case infErr.Class == inference.ClassContract:
			return failureResponse{
				http.StatusBadGateway, string(infErr.Code),
				"We could not complete this analysis. Our team has been notified.",
			}
		default:
			return failureResponse{
				http.StatusServiceUnavailable, string(infErr.Code),
				"The analysis service is temporarily unavailable. Please try again in a few minutes.",
			}
		}
	}

	// Everything left is ours: a temp file, an upload, a presign. Nothing the
	// admin did caused it and nothing they can do fixes it.
	return failureResponse{
		http.StatusInternalServerError, codeStorageFailed,
		"Something went wrong while handling the video. Please try again.",
	}
}

// failRequest closes the record as failed and builds the response.
//
// The database write uses a cancellation-free context on purpose. The most
// likely way to reach here on a long analysis is the admin closing the tab,
// which cancels the request context — and inheriting that cancellation would
// leave the row stuck at 'running' in exactly the case the history most needs
// to show. The write is a few milliseconds against a local pool, so detaching
// it costs nothing.
func (h *Handler) failRequest(ctx context.Context, s *analysisState, cause error) error {
	resp := classifyFailure(cause)

	detail := cctvdb.JSONMap{"internal_error": cause.Error()}
	if s.jobID != "" {
		detail["inference_job_id"] = s.jobID
	}
	var infErr *inference.Error
	if errors.As(cause, &infErr) {
		detail["class"] = infErr.Class.String()
		if len(infErr.Detail) > 0 {
			detail["inference"] = infErr.Detail
		}
	}

	record := cctvdb.FailRequest{
		ID:        s.requestID,
		ErrorCode: resp.code,
		Detail:    detail,
	}
	if s.jobID != "" {
		record.InferenceJobID = &s.jobID
	}
	// A source clip that was already stored is kept and recorded even though
	// the run failed: it is the only evidence of what the model was given.
	if s.sourceKey != "" {
		record.SourceVideoKey = &s.sourceKey
	}

	writeErr := h.DB.FailRequest(context.WithoutCancel(ctx), record)

	return echo.NewHTTPError(resp.status, httperr.Response{
		Message: resp.message,
		Code:    resp.code,
		Details: map[string]any{"request_id": s.requestID},
	}).SetInternal(errors.Join(cause, writeErr, s.cleanupErr))
}

func (h *Handler) listRequests(c echo.Context) error {
	ctx := c.Request().Context()

	var req HistoryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, httperr.Response{
			Message: "Invalid query parameters.",
		}).SetInternal(err)
	}

	rows, err := h.DB.ListRequests(ctx, req.GoshalaPublicID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, httperr.Response{
			Message: "Could not load the analysis history.",
		}).SetInternal(err)
	}

	out := make([]AnalysisResponse, len(rows))
	for i, r := range rows {
		out[i] = AnalysisResponse{
			RequestID: r.ID,
			Status:    r.Status,
			Goshala: GoshalaSummary{
				PublicID: r.GoshalaPublicID, Name: r.GoshalaName,
				Village: r.GoshalaVillage, Mandal: r.GoshalaMandal,
				District: r.GoshalaDistrict, State: r.GoshalaState,
			},
			TotalAnimals:      r.TotalAnimals,
			TotalClearAnimals: r.TotalClearAnimals,
			AnnotatedVideoURL: h.presignOrNil(ctx, r.AnnotatedVideoKey),
			SourceVideoURL:    h.presignOrNil(ctx, r.SourceVideoKey),
			ErrorCode:         r.ErrorCode,
			RequestedBy:       r.RequestedByEmail,
			RequestedAt:       r.RequestedAt,
			CompletedAt:       r.CompletedAt,
		}
	}
	return c.JSON(http.StatusOK, out)
}

// presignOrNil signs a stored key, or returns nil when there is nothing to sign
// or signing failed. A history listing must render whatever it has — one
// unplayable video should not blank the whole page.
func (h *Handler) presignOrNil(ctx context.Context, key *string) *string {
	if key == nil || *key == "" {
		return nil
	}
	url, err := h.Storage.PresignedURL(ctx, *key)
	if err != nil {
		return nil
	}
	return &url
}
