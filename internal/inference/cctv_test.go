package inference

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCCTVServer stands in for the inference server's /cctv endpoints. It
// reports "processing" for the first pollsBeforeDone status calls, so the
// polling loop is actually exercised rather than short-circuited.
type fakeCCTVServer struct {
	pollsBeforeDone int32
	failWith        string
	resultBody      string

	polls    atomic.Int32
	deleted  atomic.Bool
	uploaded atomic.Int64
}

func (f *fakeCCTVServer) start(t *testing.T) *HTTPCCTVClient {
	t.Helper()

	mux := http.NewServeMux()

	mux.HandleFunc("POST /cctv/analyze", func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		f.uploaded.Store(n)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"job_id":"abc123","status":"queued"}`)
	})

	mux.HandleFunc("GET /cctv/jobs/{id}/status", func(w http.ResponseWriter, r *http.Request) {
		n := f.polls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case f.failWith != "":
			fmt.Fprintf(w, `{"job_id":"abc123","status":"failed","error":%q}`, f.failWith)
		case n <= f.pollsBeforeDone:
			fmt.Fprintf(w, `{"job_id":"abc123","status":"processing","progress":0.5,"frames_processed":%d}`, n*10)
		default:
			fmt.Fprint(w, `{"job_id":"abc123","status":"done","progress":1.0}`)
		}
	})

	mux.HandleFunc("GET /cctv/jobs/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := f.resultBody
		if body == "" {
			body = `{"job_id":"abc123","final_cattle_count":12,"max_cattle_in_frame":12,` +
				`"unique_tracked_cattle":17,"video_url":"/cctv/jobs/abc123/video"}`
		}
		fmt.Fprint(w, body)
	})

	mux.HandleFunc("GET /cctv/jobs/{id}/video", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		fmt.Fprint(w, "annotated-video-bytes")
	})

	mux.HandleFunc("DELETE /cctv/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.deleted.Store(true)
		fmt.Fprint(w, `{"deleted":"abc123"}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := NewHTTPCCTVClient(srv.URL)
	return client
}

// Analyse must hide the job model entirely: one call in, a finished result out.
func TestCCTVAnalyseBlocksUntilJobCompletes(t *testing.T) {
	fake := &fakeCCTVServer{pollsBeforeDone: 2}
	client := fake.start(t)

	result, err := client.Analyse(context.Background(),
		strings.NewReader("raw-video-bytes"), "camera.mp4", "GOSH-1/Somewhere")
	if err != nil {
		t.Fatalf("Analyse: %v", err)
	}

	if result.JobID != "abc123" {
		t.Errorf("JobID = %q, want abc123", result.JobID)
	}
	// The two counts must not be swapped: cattle_in_view is the peak in one
	// frame (max_cattle_in_frame), cattle_observed is the distinct tracked
	// count (unique_tracked_cattle). Getting these the wrong way round would
	// be invisible without asserting on distinct values.
	if result.CattleInView != 12 {
		t.Errorf("CattleInView = %d, want 12 (max_cattle_in_frame)", result.CattleInView)
	}
	if result.CattleObserved != 17 {
		t.Errorf("CattleObserved = %d, want 17 (unique_tracked_cattle)", result.CattleObserved)
	}

	if got := fake.polls.Load(); got < 3 {
		t.Errorf("polled %d times, expected to wait through the processing states", got)
	}
	if fake.uploaded.Load() == 0 {
		t.Error("no video body reached the server")
	}
}

// A job the pipeline rejects is a domain failure — the clip is the problem, not
// the network — and must be distinguishable from an outage.
func TestCCTVAnalyseFailedJobIsDomainError(t *testing.T) {
	fake := &fakeCCTVServer{failWith: "codec not supported"}
	client := fake.start(t)

	_, err := client.Analyse(context.Background(),
		strings.NewReader("x"), "camera.mp4", "")
	if err == nil {
		t.Fatal("expected an error")
	}

	infErr := Classify(err)
	if infErr.Class != ClassDomain || infErr.Code != CodeCCTVAnalysisFailed {
		t.Errorf("class/code = %v/%q, want domain/%s", infErr.Class, infErr.Code, CodeCCTVAnalysisFailed)
	}
	// The pipeline's own message names codecs and frame offsets, so it must
	// stay internal rather than reaching an admin.
	if strings.Contains(infErr.Error(), "codec not supported") {
		t.Errorf("Error() leaked the pipeline message: %q", infErr.Error())
	}
	if !strings.Contains(infErr.RawError.Error(), "codec not supported") {
		t.Error("RawError should retain the pipeline message for the log")
	}
}

// The inference server keeps jobs in memory, so a restart between the poll and
// the result turns a finished job into an empty body. Reporting that as zero
// cattle would be worse than failing.
func TestCCTVEmptyResultIsContractViolation(t *testing.T) {
	fake := &fakeCCTVServer{resultBody: `{}`}
	client := fake.start(t)

	_, err := client.Analyse(context.Background(), strings.NewReader("x"), "camera.mp4", "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if infErr := Classify(err); infErr.Class != ClassContract {
		t.Errorf("class = %v, want contract", infErr.Class)
	}
}

func TestCCTVFetchAnnotatedVideoAndDelete(t *testing.T) {
	fake := &fakeCCTVServer{}
	client := fake.start(t)

	body, err := client.FetchAnnotatedVideo(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("FetchAnnotatedVideo: %v", err)
	}
	defer body.Close()

	got, _ := io.ReadAll(body)
	if string(got) != "annotated-video-bytes" {
		t.Errorf("video body = %q", got)
	}

	if err := client.DeleteJob(context.Background(), "abc123"); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if !fake.deleted.Load() {
		t.Error("DeleteJob did not reach the server")
	}
}

// A cancelled request must stop the polling loop promptly rather than waiting
// out the full job deadline.
func TestCCTVAnalyseRespectsContextCancellation(t *testing.T) {
	fake := &fakeCCTVServer{pollsBeforeDone: 1_000_000}
	client := fake.start(t)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := client.Analyse(ctx, strings.NewReader("x"), "camera.mp4", "")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a cancellation error")
		}
		if !errors.Is(err, ErrTransport) {
			t.Errorf("err = %v, want a transport-class failure", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Analyse ignored context cancellation")
	}
}
