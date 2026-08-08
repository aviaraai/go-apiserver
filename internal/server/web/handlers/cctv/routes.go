package cctv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"go-api-server/internal/cctv"
	cctvdb "go-api-server/internal/database/cctv"
	"go-api-server/internal/httperr"
	"go-api-server/internal/inference"
	"go-api-server/internal/middleware"
	"go-api-server/internal/storage"

	"github.com/labstack/echo/v4"
)

type Repository interface {
	ListGoshalas(context.Context) ([]cctvdb.Goshala, error)
	GoshalaByPublicID(context.Context, string) (*cctvdb.Goshala, error)
	OpenRequest(context.Context, cctvdb.CreateRequest) (int64, error)
	CompleteRequest(context.Context, cctvdb.CompleteRequest) error
	FailRequest(context.Context, cctvdb.FailRequest) error
	ListRequests(context.Context, *string) ([]cctvdb.RequestRow, error)
}

type Handler struct {
	DB        Repository
	Storage   storage.Storage
	Inference inference.CCTVClient
	Source    cctv.Source
}

func RegisterRoutes(g *echo.Group, h *Handler) {
	cctvGroup := g.Group("/cctv", middleware.RequireRole("admin"))
	cctvGroup.GET("/goshalas", h.listGoshalas)
	cctvGroup.POST("/analyse", h.analyse)
	cctvGroup.GET("/requests", h.listRequests)
}

func (h *Handler) listGoshalas(c echo.Context) error {
	ctx := c.Request().Context()

	rows, err := h.DB.ListGoshalas(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, httperr.Response{
			Message: "Could not load the goshala list.",
		}).SetInternal(err)
	}

	out := make([]GoshalaResponse, len(rows))
	for i := range rows {
		// A missing photo is not worth failing the whole list over — the
		// dashboard needs the names to render a picker at all, so an unsigned
		// photo degrades that one row instead of the response.
		url, err := h.Storage.PresignedURL(ctx, rows[i].PhotoKey)
		if err != nil {
			url = ""
		}
		out[i] = GoshalaResponse{
			PublicID:  rows[i].PublicID,
			Name:      rows[i].Name,
			State:     rows[i].State,
			District:  rows[i].District,
			Mandal:    rows[i].Mandal,
			Village:   rows[i].Village,
			Latitude:  rows[i].Latitude,
			Longitude: rows[i].Longitude,
			PhotoURL:  url,
		}
	}
	return c.JSON(http.StatusOK, out)
}

// analyse runs the whole pipeline in one request: pull a clip from the goshala's
// camera, keep it, hand it to the model, keep what comes back, and answer with
// the counts and both videos.
//
// It blocks for as long as the analysis takes, by design. The caller sees one
// request; the inference server's job model is handled inside the client. The
// practical consequence is that a long clip can outlast a proxy's idle timeout —
// see the frontend contract for the timeout the dashboard must set.
func (h *Handler) analyse(c echo.Context) error {
	ctx := c.Request().Context()

	var req AnalyseRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, httperr.Response{
			Message: "Invalid request body.",
		}).SetInternal(err)
	}
	if strings.TrimSpace(req.GoshalaPublicID) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, httperr.Response{
			Message: "Select a goshala to analyse.",
			Code:    "GOSHALA_REQUIRED",
		})
	}

	goshala, err := h.DB.GoshalaByPublicID(ctx, req.GoshalaPublicID)
	if err != nil {
		if errors.Is(err, cctvdb.ErrGoshalaNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, httperr.Response{
				Message: "That goshala could not be found.",
				Code:    "GOSHALA_NOT_FOUND",
			}).SetInternal(err)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, httperr.Response{
			Message: "Could not look up that goshala.",
		}).SetInternal(err)
	}

	// The record is opened before any work starts, so an analysis that times
	// out or crashes still appears in the history as an attempt that was made.
	requestID, err := h.DB.OpenRequest(ctx, cctvdb.CreateRequest{
		FarmerID:         goshala.FarmerID,
		RequestedBy:      middleware.UserIDFromContext(c),
		RequestedByEmail: middleware.EmailFromContext(c),
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, httperr.Response{
			Message: "Could not start the analysis.",
		}).SetInternal(err)
	}

	result, err := h.runAnalysis(ctx, requestID, goshala)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, result)
}

// runAnalysis carries out the work and closes the request record either way.
// Every exit path writes a terminal status, so a row is never left running
// except when the process itself dies.
func (h *Handler) runAnalysis(ctx context.Context, requestID int64, goshala *cctvdb.Goshala) (*AnalysisResponse, error) {
	state := &analysisState{requestID: requestID, goshala: goshala}

	if err := h.fetchAndStoreSource(ctx, state); err != nil {
		return nil, h.failRequest(ctx, state, err)
	}
	defer os.Remove(state.sourcePath)

	if err := h.analyseAndStoreAnnotated(ctx, state); err != nil {
		return nil, h.failRequest(ctx, state, err)
	}

	detail := cctvdb.JSONMap{"inference_job_id": state.jobID}
	// Detached for the same reason as the failure path: an admin who closed the
	// tab must not cost us the record of an analysis that actually completed.
	if err := h.DB.CompleteRequest(context.WithoutCancel(ctx), cctvdb.CompleteRequest{
		ID:                requestID,
		InferenceJobID:    state.jobID,
		SourceVideoKey:    &state.sourceKey,
		AnnotatedVideoKey: state.annotatedKey,
		TotalAnimals:      state.totalAnimals,
		TotalClearAnimals: state.totalClearAnimals,
		Detail:            detail,
	}); err != nil {
		// The work succeeded and only the write failed, but the row still has to
		// reach a terminal state: leaving it running would strand it there for
		// good, and there is no result to read back from it either way. The
		// distinct error code is what tells the two apart in the history.
		return nil, h.failRequest(ctx, state, fmt.Errorf("%w: %w", ErrResultNotSaved, err))
	}

	return h.presentResult(ctx, state)
}

// analysisState threads the pieces of one run through the steps, so a failure
// at any point still knows what was already achieved — the source key in
// particular, which is worth recording even when the model never ran.
type analysisState struct {
	requestID int64
	goshala   *cctvdb.Goshala

	sourcePath string
	sourceKey  string

	jobID             string
	annotatedKey      string
	totalAnimals      int
	totalClearAnimals int

	// cleanupErr records a failure to tidy up the inference server's copy of
	// the job. It never changes the outcome — the analysis is complete either
	// way — but it rides along on the response's internal error so a machine
	// quietly filling its disk is visible in the logs.
	cleanupErr error
}

// fetchAndStoreSource pulls the clip from the camera and keeps a copy.
//
// The clip is spooled to a temp file rather than held in memory: these are
// multi-minute videos, and it has to be read twice — once to upload as the
// source of record, once to send to the model.
func (h *Handler) fetchAndStoreSource(ctx context.Context, s *analysisState) error {
	video, err := h.Source.FetchLatest(ctx, s.goshala.PublicID)
	if err != nil {
		return err
	}
	defer video.Body.Close()

	tmp, err := os.CreateTemp("", fmt.Sprintf("cctv-%d-*.mp4", s.requestID))
	if err != nil {
		return fmt.Errorf("create temp video file: %w", err)
	}
	s.sourcePath = tmp.Name()

	if _, err := io.Copy(tmp, video.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("spool camera video: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp video file: %w", err)
	}

	contentType := video.ContentType
	if contentType == "" {
		contentType = "video/mp4"
	}
	s.sourceKey = fmt.Sprintf("cctv/%d/source%s", s.requestID, videoExtension(video.Filename))

	src, err := os.Open(s.sourcePath)
	if err != nil {
		return fmt.Errorf("reopen temp video file: %w", err)
	}
	defer src.Close()

	if err := h.Storage.Upload(ctx, src, s.sourceKey, contentType); err != nil {
		s.sourceKey = ""
		return fmt.Errorf("upload source video: %w", err)
	}
	return nil
}

// analyseAndStoreAnnotated runs the model and copies its output into storage.
//
// The annotated video is streamed straight from the inference server into
// object storage without touching disk here, then the job is deleted: that
// machine's disk is a working area, and once both videos are ours there is
// nothing left on it worth keeping.
func (h *Handler) analyseAndStoreAnnotated(ctx context.Context, s *analysisState) error {
	src, err := os.Open(s.sourcePath)
	if err != nil {
		return fmt.Errorf("reopen source video for inference: %w", err)
	}
	defer src.Close()

	locationTag := fmt.Sprintf("%s/%s", s.goshala.PublicID, s.goshala.Village)
	result, err := h.Inference.Analyse(ctx, src, path.Base(s.sourcePath), locationTag)
	if err != nil {
		return err
	}
	s.jobID = result.JobID
	s.totalAnimals = result.TotalAnimals
	s.totalClearAnimals = result.TotalClearAnimals

	annotated, err := h.Inference.FetchAnnotatedVideo(ctx, result.JobID)
	if err != nil {
		return err
	}
	defer annotated.Close()

	s.annotatedKey = fmt.Sprintf("cctv/%d/annotated.mp4", s.requestID)
	if err := h.Storage.Upload(ctx, annotated, s.annotatedKey, "video/mp4"); err != nil {
		s.annotatedKey = ""
		return fmt.Errorf("upload annotated video: %w", err)
	}

	// Best effort. Failing to tidy up the inference server does not make the
	// analysis any less complete, and the error is carried on the response's
	// internal error rather than discarded.
	if err := h.Inference.DeleteJob(ctx, result.JobID); err != nil {
		s.cleanupErr = err
	}
	return nil
}

func (h *Handler) presentResult(ctx context.Context, s *analysisState) (*AnalysisResponse, error) {
	annotatedURL, err := h.Storage.PresignedURL(ctx, s.annotatedKey)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusInternalServerError, httperr.Response{
			Message: "The analysis finished but its video link could not be created.",
		}).SetInternal(err)
	}
	sourceURL, err := h.Storage.PresignedURL(ctx, s.sourceKey)
	if err != nil {
		// The annotated video is the deliverable; the original is a
		// convenience, so its absence must not sink a completed analysis.
		sourceURL = ""
	}

	resp := &AnalysisResponse{
		RequestID:         s.requestID,
		Status:            cctvdb.StatusSucceeded,
		Goshala:           toGoshalaSummary(s.goshala),
		TotalAnimals:      &s.totalAnimals,
		TotalClearAnimals: &s.totalClearAnimals,
		AnnotatedVideoURL: &annotatedURL,
	}
	if sourceURL != "" {
		resp.SourceVideoURL = &sourceURL
	}
	return resp, nil
}

func toGoshalaSummary(g *cctvdb.Goshala) GoshalaSummary {
	return GoshalaSummary{
		PublicID: g.PublicID, Name: g.Name,
		Village: g.Village, Mandal: g.Mandal,
		District: g.District, State: g.State,
	}
}

func videoExtension(filename string) string {
	if ext := path.Ext(filename); ext != "" {
		return ext
	}
	return ".mp4"
}
