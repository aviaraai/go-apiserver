package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// CCTV-specific codes, produced by this client. The /cctv endpoints predate the
// error_code envelope and answer with FastAPI's plain {"detail": ...}, so
// nothing here is read off the wire — these classify what we observed.
const (
	// CodeCCTVAnalysisFailed is a job the pipeline itself reported as failed:
	// it accepted the video and could not process it. Unlike the transport
	// codes this says something about the clip, so it is worth telling the
	// admin apart from an outage.
	CodeCCTVAnalysisFailed Code = "CCTV_ANALYSIS_FAILED"

	// CodeCCTVTimeout is a job still running when our patience ran out. The
	// job may well finish afterwards; we simply stop waiting.
	CodeCCTVTimeout Code = "CCTV_TIMEOUT"
)

// CCTVResult is what one analysed video yields.
type CCTVResult struct {
	JobID string

	// CattleInView is the peak number visible in any single frame
	// (max_cattle_in_frame). Trustworthy for a fixed camera; undercounts one
	// panning across a herd.
	CattleInView int

	// CattleObserved is the number of distinct tracked animals
	// (unique_tracked_cattle). Trustworthy for a panning shot; can overcount a
	// static herd when the tracker churns IDs.
	//
	// Neither figure is a correction of the other, which is why both are
	// surfaced rather than one being picked here.
	CattleObserved int
}

// CCTVClient runs one video through the inference server's analytics pipeline.
//
// Analyse blocks until the job finishes. That hides the server's job model from
// callers on purpose: the API this feeds is itself a single blocking request, so
// spreading polling across layers would buy nothing.
type CCTVClient interface {
	Analyse(ctx context.Context, video io.Reader, filename, locationTag string) (*CCTVResult, error)
	FetchAnnotatedVideo(ctx context.Context, jobID string) (io.ReadCloser, error)
	DeleteJob(ctx context.Context, jobID string) error
}

// Polling cadence. The interval is a compromise: short enough that a 30-second
// clip is not padded by a long wait, long enough that a ten-minute job does not
// generate thousands of requests.
const (
	cctvPollInterval = 2 * time.Second
	cctvMaxWait      = 30 * time.Minute
)

type HTTPCCTVClient struct {
	baseURL string
	http    *http.Client
}

func NewHTTPCCTVClient(baseURL string) *HTTPCCTVClient {
	return &HTTPCCTVClient{
		baseURL: baseURL,
		// Per-request timeout, not per-job: uploading or downloading a video
		// is the slow part, and the waiting between polls is bounded
		// separately by cctvMaxWait.
		http: &http.Client{Timeout: 15 * time.Minute},
	}
}

type cctvJobStatus struct {
	JobID           string  `json:"job_id"`
	Status          string  `json:"status"`
	Progress        float64 `json:"progress"`
	FramesProcessed int     `json:"frames_processed"`
	TotalFrames     int     `json:"total_frames"`
	CattleSoFar     int     `json:"cattle_so_far"`
	Error           string  `json:"error"`
}

type cctvJobResult struct {
	JobID               string `json:"job_id"`
	MaxCattleInFrame    int    `json:"max_cattle_in_frame"`
	UniqueTrackedCattle int    `json:"unique_tracked_cattle"`
	FramesProcessed     int    `json:"frames_processed"`
	VideoURL            string `json:"video_url"`
}

func (c *HTTPCCTVClient) Analyse(ctx context.Context, video io.Reader, filename, locationTag string) (*CCTVResult, error) {
	jobID, err := c.startJob(ctx, video, filename, locationTag)
	if err != nil {
		return nil, err
	}
	if err := c.awaitJob(ctx, jobID); err != nil {
		return nil, err
	}
	return c.fetchResult(ctx, jobID)
}

// startJob streams the video to /cctv/analyze. The body is piped rather than
// buffered so a large clip never has to be held in memory here — it goes
// straight from the caller's reader onto the wire.
func (c *HTTPCCTVClient) startJob(ctx context.Context, video io.Reader, filename, locationTag string) (string, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		// CloseWithError on the writer end surfaces a mid-stream failure as a
		// read error on the request body, so a truncated upload fails loudly
		// instead of being sent as a short file.
		err := func() error {
			part, err := writer.CreateFormFile("video", filename)
			if err != nil {
				return fmt.Errorf("create video part: %w", err)
			}
			if _, err := io.Copy(part, video); err != nil {
				return fmt.Errorf("stream video: %w", err)
			}
			if err := writer.WriteField("preset", "fast"); err != nil {
				return fmt.Errorf("write preset field: %w", err)
			}
			if locationTag != "" {
				if err := writer.WriteField("location_tag", locationTag); err != nil {
					return fmt.Errorf("write location_tag field: %w", err)
				}
			}
			return writer.Close()
		}()
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/cctv/analyze", pr)
	if err != nil {
		return "", transportError(CodeUnavailable, 0, fmt.Errorf("build cctv analyse request: %w", err))
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	var status cctvJobStatus
	if _, err := c.exchange(req, &status); err != nil {
		return "", err
	}
	if status.JobID == "" {
		return "", contractError(http.StatusOK, fmt.Errorf("cctv analyse returned no job_id"))
	}
	return status.JobID, nil
}

// awaitJob polls until the pipeline finishes, fails, or we give up.
func (c *HTTPCCTVClient) awaitJob(ctx context.Context, jobID string) error {
	deadline := time.Now().Add(cctvMaxWait)
	ticker := time.NewTicker(cctvPollInterval)
	defer ticker.Stop()

	for {
		status, err := c.jobStatus(ctx, jobID)
		if err != nil {
			return err
		}

		switch status.Status {
		case "done":
			return nil
		case "failed":
			// The pipeline's own message can name a codec or a corrupt frame,
			// so it stays internal; the caller decides what to tell the admin.
			return &Error{
				Class:      ClassDomain,
				Code:       CodeCCTVAnalysisFailed,
				StatusCode: http.StatusOK,
				Detail:     map[string]any{"job_id": jobID, "pipeline_error": status.Error},
				RawError:   fmt.Errorf("cctv job %s failed: %s", jobID, status.Error),
			}
		case "queued", "processing":
			// keep waiting
		default:
			return contractError(http.StatusOK,
				fmt.Errorf("cctv job %s reported unknown status %q", jobID, status.Status))
		}

		if time.Now().After(deadline) {
			return &Error{
				Class:      ClassTransport,
				Code:       CodeCCTVTimeout,
				StatusCode: 0,
				Detail:     map[string]any{"job_id": jobID, "waited_seconds": cctvMaxWait.Seconds()},
				RawError: fmt.Errorf("cctv job %s still %s after %s",
					jobID, status.Status, cctvMaxWait),
			}
		}

		select {
		case <-ctx.Done():
			return transportError(CodeUnavailable, 0,
				fmt.Errorf("cctv job %s abandoned: %w", jobID, ctx.Err()))
		case <-ticker.C:
		}
	}
}

func (c *HTTPCCTVClient) jobStatus(ctx context.Context, jobID string) (*cctvJobStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/cctv/jobs/%s/status", c.baseURL, jobID), nil)
	if err != nil {
		return nil, transportError(CodeUnavailable, 0, fmt.Errorf("build cctv status request: %w", err))
	}

	var status cctvJobStatus
	if _, err := c.exchange(req, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (c *HTTPCCTVClient) fetchResult(ctx context.Context, jobID string) (*CCTVResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/cctv/jobs/%s/result", c.baseURL, jobID), nil)
	if err != nil {
		return nil, transportError(CodeUnavailable, 0, fmt.Errorf("build cctv result request: %w", err))
	}

	var out cctvJobResult
	statusCode, err := c.exchange(req, &out)
	if err != nil {
		return nil, err
	}

	// The inference server keeps its job table in memory, so a restart between
	// the poll and this call turns a finished job into a 404 handled above, or
	// into a zeroed body here. Either way the counts are the whole point of the
	// call, so an absent result is a contract violation rather than a zero.
	if out.JobID == "" {
		return nil, contractError(statusCode, fmt.Errorf("cctv result for job %s carried no job_id", jobID))
	}

	return &CCTVResult{
		JobID:          jobID,
		CattleInView:   out.MaxCattleInFrame,
		CattleObserved: out.UniqueTrackedCattle,
	}, nil
}

// FetchAnnotatedVideo opens the annotated MP4. The caller owns the stream and
// must close it; it is returned unbuffered so it can be piped straight into
// object storage without ever being held whole.
func (c *HTTPCCTVClient) FetchAnnotatedVideo(ctx context.Context, jobID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/cctv/jobs/%s/video", c.baseURL, jobID), nil)
	if err != nil {
		return nil, transportError(CodeUnavailable, 0, fmt.Errorf("build cctv video request: %w", err))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, transportError(CodeUnavailable, 0, fmt.Errorf("cctv video request: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, decodeErrorBody(resp.StatusCode,
			fmt.Appendf(nil, `{"detail":%q}`, string(body)))
	}
	return resp.Body, nil
}

// DeleteJob releases the inference server's copy of a job's output. Called once
// both videos are safely in object storage — that machine's disk is a working
// area, not a store, and leaving finished jobs on it fills it up.
func (c *HTTPCCTVClient) DeleteJob(ctx context.Context, jobID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/cctv/jobs/%s", c.baseURL, jobID), nil)
	if err != nil {
		return transportError(CodeUnavailable, 0, fmt.Errorf("build cctv delete request: %w", err))
	}
	_, err = c.exchange(req, nil)
	return err
}

// exchange performs a request and decodes a JSON success body into out, which
// may be nil when the body is not needed. Errors come back already classified,
// with every upstream detail kept inside Error.RawError.
func (c *HTTPCCTVClient) exchange(req *http.Request, out any) (int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, transportError(CodeUnavailable, 0, fmt.Errorf("cctv request %s: %w", req.URL.Path, err))
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, transportError(CodeUnavailable, resp.StatusCode,
			fmt.Errorf("read cctv response: %w", err))
	}

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, decodeErrorBody(resp.StatusCode, rawBody)
	}
	if out == nil {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(rawBody, out); err != nil {
		return resp.StatusCode, contractError(resp.StatusCode,
			fmt.Errorf("decode cctv response: %w, body: %s", err, rawBody))
	}
	return resp.StatusCode, nil
}
