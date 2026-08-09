package cctv

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cctvdb "go-api-server/internal/database/cctv"
	"go-api-server/internal/httperr"
	"go-api-server/internal/inference"

	"github.com/labstack/echo/v4"
)

type fakeRepo struct{ completed cctvdb.CompleteRequest }

func (f *fakeRepo) ListGoshalas(context.Context) ([]cctvdb.Goshala, error) { return nil, nil }
func (f *fakeRepo) GoshalaByPublicID(_ context.Context, id string) (*cctvdb.Goshala, error) {
	if id != "gsh_1" {
		return nil, cctvdb.ErrGoshalaNotFound
	}
	return &cctvdb.Goshala{PublicID: "gsh_1", Village: "V", FarmerID: 7}, nil
}
func (f *fakeRepo) OpenRequest(context.Context, cctvdb.CreateRequest) (int64, error) { return 42, nil }
func (f *fakeRepo) CompleteRequest(_ context.Context, r cctvdb.CompleteRequest) error {
	f.completed = r
	return nil
}
func (f *fakeRepo) FailRequest(context.Context, cctvdb.FailRequest) error { return nil }
func (f *fakeRepo) ListRequests(context.Context, *string) ([]cctvdb.RequestRow, error) {
	return nil, nil
}

type fakeStore struct{ uploaded map[string]string }

func (s *fakeStore) Upload(_ context.Context, r io.Reader, key, ct string) error {
	b, _ := io.ReadAll(r)
	s.uploaded[key] = string(b) + "|" + ct
	return nil
}
func (s *fakeStore) PresignedURL(_ context.Context, key string) (string, error) {
	return "https://signed/" + key, nil
}

type fakeInference struct{ gotFilename, gotBody string }

func (f *fakeInference) Analyse(_ context.Context, v io.Reader, filename, tag string) (*inference.CCTVResult, error) {
	b, _ := io.ReadAll(v)
	f.gotBody = string(b)
	f.gotFilename = filename
	return &inference.CCTVResult{JobID: "job1", TotalAnimals: 9, TotalClearAnimals: 11}, nil
}
func (f *fakeInference) FetchAnnotatedVideo(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("ANNOTATED")), nil
}
func (f *fakeInference) DeleteJob(context.Context, string) error { return nil }

func postUpload(t *testing.T, h *Handler, goshala, filename, content string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if goshala != "" {
		w.WriteField("goshala_public_id", goshala)
	}
	if filename != "" {
		part, _ := w.CreateFormFile("video", filename)
		part.Write([]byte(content))
	}
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	req.Header.Set(echo.HeaderContentType, w.FormDataContentType())
	rec := httptest.NewRecorder()
	e := echo.New()
	return rec, h.analyseUpload(e.NewContext(req, rec))
}

// rejection pulls the status and machine-readable code out of a handler error,
// mirroring what the app's error handler puts on the wire.
func rejection(t *testing.T, err error) (int, string) {
	t.Helper()
	he, ok := err.(*echo.HTTPError)
	if !ok {
		t.Fatalf("not an HTTPError: %v", err)
	}
	body, ok := he.Message.(httperr.Response)
	if !ok {
		t.Fatalf("not an httperr.Response: %#v", he.Message)
	}
	return he.Code, body.Code
}

func TestUploadHappyPath(t *testing.T) {
	repo := &fakeRepo{}
	store := &fakeStore{uploaded: map[string]string{}}
	inf := &fakeInference{}
	h := &Handler{DB: repo, Storage: store, Inference: inf}

	rec, err := postUpload(t, h, "gsh_1", "My Clip.MP4", "VIDEOBYTES")
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d err %v body %s", rec.Code, err, rec.Body)
	}

	var resp AnalysisResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.RequestID != 42 || *resp.TotalAnimals != 9 || *resp.TotalClearAnimals != 11 {
		t.Fatalf("bad response: %+v", resp)
	}
	if *resp.AnnotatedVideoURL != "https://signed/cctv/42/annotated.mp4" {
		t.Fatalf("annotated url: %v", *resp.AnnotatedVideoURL)
	}
	if *resp.SourceVideoURL != "https://signed/cctv/42/source.mp4" {
		t.Fatalf("source url: %v", *resp.SourceVideoURL)
	}
	if got := store.uploaded["cctv/42/source.mp4"]; got != "VIDEOBYTES|video/mp4" {
		t.Fatalf("stored source: %q", got)
	}
	if got := store.uploaded["cctv/42/annotated.mp4"]; got != "ANNOTATED|video/mp4" {
		t.Fatalf("stored annotated: %q", got)
	}
	if inf.gotBody != "VIDEOBYTES" {
		t.Fatalf("inference body: %q", inf.gotBody)
	}
	if !strings.HasSuffix(inf.gotFilename, ".mp4") {
		t.Fatalf("inference filename: %q", inf.gotFilename)
	}
	if repo.completed.SourceVideoKey == nil || *repo.completed.SourceVideoKey != "cctv/42/source.mp4" {
		t.Fatalf("completed record: %+v", repo.completed)
	}
}

func TestUploadMovKeepsItsFormat(t *testing.T) {
	store := &fakeStore{uploaded: map[string]string{}}
	h := &Handler{DB: &fakeRepo{}, Storage: store, Inference: &fakeInference{}}

	if rec, err := postUpload(t, h, "gsh_1", "clip.mov", "X"); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d err %v body %s", rec.Code, err, rec.Body)
	}
	if got := store.uploaded["cctv/42/source.mov"]; got != "X|video/quicktime" {
		t.Fatalf("stored: %q / keys %v", got, store.uploaded)
	}
}

func TestUploadRejections(t *testing.T) {
	h := &Handler{DB: &fakeRepo{}, Storage: &fakeStore{uploaded: map[string]string{}}, Inference: &fakeInference{}}

	cases := []struct {
		name             string
		goshala, file, c string
		wantStatus       int
		wantCode         string
	}{
		{"no file", "gsh_1", "", "", http.StatusBadRequest, codeVideoRequired},
		{"empty file", "gsh_1", "a.mp4", "", http.StatusBadRequest, codeVideoUnreadable},
		{"bad ext", "gsh_1", "a.exe", "X", http.StatusUnsupportedMediaType, codeVideoUnsupportedFormat},
		{"no ext", "gsh_1", "clip", "X", http.StatusUnsupportedMediaType, codeVideoUnsupportedFormat},
		{"no goshala", "", "a.mp4", "X", http.StatusBadRequest, "GOSHALA_REQUIRED"},
		{"unknown goshala", "nope", "a.mp4", "X", http.StatusNotFound, "GOSHALA_NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := postUpload(t, h, tc.goshala, tc.file, tc.c)
			if err == nil {
				t.Fatal("expected a rejection")
			}
			status, code := rejection(t, err)
			if status != tc.wantStatus || code != tc.wantCode {
				t.Fatalf("got %d/%s, want %d/%s", status, code, tc.wantStatus, tc.wantCode)
			}
		})
	}
}

// A path in the filename must never reach the storage key.
func TestUploadFilenameIsNotTrusted(t *testing.T) {
	store := &fakeStore{uploaded: map[string]string{}}
	h := &Handler{DB: &fakeRepo{}, Storage: store, Inference: &fakeInference{}}

	rec, err := postUpload(t, h, "gsh_1", `C:\Users\a\..\..\evil.mp4`, "X")
	if err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d err %v body %s", rec.Code, err, rec.Body)
	}
	if _, ok := store.uploaded["cctv/42/source.mp4"]; !ok {
		t.Fatalf("unexpected keys: %v", store.uploaded)
	}
}
