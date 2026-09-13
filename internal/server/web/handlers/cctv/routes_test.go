package cctv

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-api-server/internal/cctv"

	"github.com/labstack/echo/v4"
)

type fakeSource struct{ gotGoshalaID string }

func (f *fakeSource) FetchLatest(_ context.Context, goshalaPublicID string) (*cctv.Video, error) {
	f.gotGoshalaID = goshalaPublicID
	return &cctv.Video{
		Filename:    "camera.mp4",
		ContentType: "video/mp4",
		Body:        io.NopCloser(strings.NewReader("CAMERAVIDEO")),
	}, nil
}

func postAnalyseJSON(t *testing.T, h *Handler, goshalaPublicID string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	var body []byte
	if goshalaPublicID != "" {
		body, _ = json.Marshal(map[string]string{"goshala_public_id": goshalaPublicID})
	} else {
		body = []byte(`{}`)
	}

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e := echo.New()
	return rec, h.analyse(e.NewContext(req, rec))
}

// TestAnalyseJSONBodyBindsGoshalaID is the regression test for the binding bug:
// AnalyseRequest previously carried only a `form` tag, so a JSON POST (what the
// dashboard actually sends to this endpoint) silently bound GoshalaPublicID to
// "" and every call failed GOSHALA_REQUIRED before ever reaching the source
// fetch. This confirms the JSON path now reaches the fake source with the real
// id, and the analysis completes end-to-end.
func TestAnalyseJSONBodyBindsGoshalaID(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStore{uploaded: map[string]string{}}
	inf := &fakeInference{}
	src := &fakeSource{}
	h := &Handler{DB: repo, Storage: store, Inference: inf, Source: src}

	rec, err := postAnalyseJSON(t, h, "gsh_1")
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d err %v body %s", rec.Code, err, rec.Body)
	}

	if src.gotGoshalaID != "gsh_1" {
		t.Fatalf("source was called with goshala id %q, want %q — binding is still broken", src.gotGoshalaID, "gsh_1")
	}

	var resp AnalysisResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.RequestID != 42 {
		t.Fatalf("bad response: %+v", resp)
	}
	if inf.gotBody != "CAMERAVIDEO" {
		t.Fatalf("inference did not receive the camera video: %q", inf.gotBody)
	}
}

// TestAnalyseJSONBodyMissingGoshalaID confirms the *right* failure — a real
// GOSHALA_REQUIRED because the field was genuinely omitted — still fires, so
// the fix above didn't just make the check pass unconditionally.
func TestAnalyseJSONBodyMissingGoshalaID(t *testing.T) {
	h := &Handler{DB: &fakeRepo{}, Storage: &fakeStore{uploaded: map[string]string{}}, Inference: &fakeInference{}, Source: &fakeSource{}}

	_, err := postAnalyseJSON(t, h, "")
	if err == nil {
		t.Fatal("expected a rejection")
	}
	status, code := rejection(t, err)
	if status != http.StatusBadRequest || code != "GOSHALA_REQUIRED" {
		t.Fatalf("got %d/%s, want %d/%s", status, code, http.StatusBadRequest, "GOSHALA_REQUIRED")
	}
}

// TestAnalyseSourceUnavailable is Step 2's regression test: with
// cctv.NotImplementedSource wired in (as routes.go now does), a JSON /analyse
// call for a real goshala must answer a clean 503 CCTV_SOURCE_UNAVAILABLE,
// not attempt a fake analysis.
func TestAnalyseSourceUnavailable(t *testing.T) {
	h := &Handler{
		DB:        &fakeRepo{},
		Storage:   &fakeStore{uploaded: map[string]string{}},
		Inference: &fakeInference{},
		Source:    cctv.NotImplementedSource{},
	}

	_, err := postAnalyseJSON(t, h, "gsh_1")
	if err == nil {
		t.Fatal("expected a rejection")
	}
	status, code := rejection(t, err)
	if status != http.StatusServiceUnavailable || code != codeSourceUnavailable {
		t.Fatalf("got %d/%s, want %d/%s", status, code, http.StatusServiceUnavailable, codeSourceUnavailable)
	}
}
