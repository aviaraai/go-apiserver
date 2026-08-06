// internal/inference/client.go
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	ErrDuplicateAnimal   = errors.New("animal already registered (duplicate detected)")
	ErrPoorImageQuality  = errors.New("image quality too poor for registration")
	ErrInferenceInternal = errors.New("inference server internal error")
	ErrInferenceUpstream = errors.New("failed to process request")
)

// ResponseError wraps an inference failure together with the raw response that
// caused it. The raw body is deliberately kept out of Error() — callers log it
// at debug level, while the wrapped error stays safe to surface to a client.
// errors.Is still reaches the sentinels above through Unwrap.
type ResponseError struct {
	StatusCode int
	RawError   error
	SafeError  error
}

func (e *ResponseError) Error() string { return e.SafeError.Error() }

func (e *ResponseError) Unwrap() error { return e.SafeError }

type Candidate struct {
	FaissID     int64   `json:"faiss_id"`
	BodyColor   string  `json:"body_color"`
	MuzzleColor string  `json:"muzzle_color"`
	HornShape   *string `json:"horn_shape"`
}

type ImagePayload struct {
	Filename    string
	ContentType string
	Data        []byte
}

type RegisterResponse struct {
	Status          string          `json:"status"`
	EmbeddingIDs    []int64         `json:"embedding_ids"`
	ExtractedColors ExtractedColors `json:"extracted_colors"`
	HornShape       *string         `json:"horn_shape"`
	RegisteredAt    string          `json:"registered_at"`
}

type ExtractedColors struct {
	Body   ColorLabel `json:"body"`
	Muzzle ColorLabel `json:"muzzle"`
}

type ColorLabel struct {
	Label string `json:"label"`
}

// SearchMatch is one embedding-level match returned by the inference server's
// /search endpoint. rank/gap are computed by inference; the API server only
// needs faiss_id + score to aggregate to cattle level.
type SearchMatch struct {
	FaissID int64   `json:"faiss_id"`
	Score   float64 `json:"score"`
	Rank    int     `json:"rank"`
	Gap     float64 `json:"gap"`
}

type SearchResponse struct {
	RequestID   string          `json:"request_id"`
	QueryColors ExtractedColors `json:"query_colors"`
	HornShape   *string         `json:"horn_shape"`
	TopMatches  []SearchMatch   `json:"top_matches"`
}

type Client interface {
	Register(ctx context.Context, front, muzzle []ImagePayload, candidates []Candidate) (*RegisterResponse, error)
	Search(ctx context.Context, front, muzzle ImagePayload, candidateIDs []Candidate, topK int) (*SearchResponse, error)
}

type HTTPClient struct {
	baseURL string
	http    *http.Client
}

func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *HTTPClient) Register(ctx context.Context, front, muzzle []ImagePayload, candidates []Candidate) (*RegisterResponse, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	candidatesJSON, err := json.Marshal(candidates)
	if err != nil {
		return nil, fmt.Errorf("marshal candidates: %w", err)
	}
	if err := writer.WriteField("candidates", string(candidatesJSON)); err != nil {
		return nil, fmt.Errorf("write candidates field: %w", err)
	}

	if err := writeImageFields(writer, "front_images", front); err != nil {
		return nil, err
	}
	if err := writeImageFields(writer, "muzzle_images", muzzle); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/register", body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inference request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read inference response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		var out RegisterResponse
		if err := json.Unmarshal(rawBody, &out); err != nil {
			return nil, responseErr(resp.StatusCode, fmt.Errorf("json parsing error, body: %w, %s", err, rawBody), fmt.Errorf("decode inference response: %w", err))
		}
		return &out, nil
	case http.StatusConflict:
		return nil, responseErr(resp.StatusCode, fmt.Errorf("duplicate animal registration body: %s", rawBody), ErrDuplicateAnimal)
	case http.StatusUnprocessableEntity:
		return nil, responseErr(resp.StatusCode, fmt.Errorf("body: %s", rawBody), withDetail(ErrPoorImageQuality, rawBody))
	case http.StatusInternalServerError:
		return nil, responseErr(resp.StatusCode, fmt.Errorf("inference server body: %s", rawBody), ErrInferenceInternal)
	default:
		return nil, responseErr(resp.StatusCode, fmt.Errorf("unexpected body: %s", rawBody), ErrInferenceUpstream)
	}
}

// quoteEscaper mirrors the escaping used by mime/multipart's CreateFormFile:
// only backslash, double-quote, CR, and LF are escaped. Using fmt's %q instead
// would escape non-ASCII runes to \uXXXX/\xXX, mangling unicode filenames.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"", "\r", "%0D", "\n", "%0A")

func (c *HTTPClient) Search(ctx context.Context, front, muzzle ImagePayload, candidates []Candidate, topK int) (*SearchResponse, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	candidatesJSON, err := json.Marshal(candidates)
	if err != nil {
		return nil, fmt.Errorf("marshal candidates: %w", err)
	}
	if err := writer.WriteField("candidates", string(candidatesJSON)); err != nil {
		return nil, fmt.Errorf("write candidates field: %w", err)
	}

	// Field names here must match the inference /search signature exactly:
	// muzzle (File), front (File), top_k (Form), candidate_ids (repeated Form).
	if err := writeImageFields(writer, "muzzle", []ImagePayload{muzzle}); err != nil {
		return nil, err
	}
	if err := writeImageFields(writer, "front", []ImagePayload{front}); err != nil {
		return nil, err
	}
	if err := writer.WriteField("top_k", strconv.Itoa(topK)); err != nil {
		return nil, fmt.Errorf("write top_k field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/search", body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inference request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read inference response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var out SearchResponse
		if err := json.Unmarshal(rawBody, &out); err != nil {
			return nil, responseErr(resp.StatusCode, fmt.Errorf("json parsing error, body: %w, %s", err, rawBody), ErrInferenceUpstream)
		}
		return &out, nil
	case http.StatusUnprocessableEntity:
		return nil, responseErr(resp.StatusCode, fmt.Errorf("body: %s", rawBody), withDetail(ErrPoorImageQuality, rawBody))
	case http.StatusInternalServerError:
		return nil, responseErr(resp.StatusCode, fmt.Errorf("inference server body: %s", rawBody), ErrInferenceInternal)
	default:
		return nil, responseErr(resp.StatusCode, fmt.Errorf("unexpected body: %s", rawBody), ErrInferenceUpstream)
	}
}

func responseErr(statusCode int, rawError error, safeError error) error {
	return &ResponseError{StatusCode: statusCode, RawError: rawError, SafeError: safeError}
}

// withDetail appends the inference server's "detail" field to base when there
// is one. Everything else in the body is discarded so raw upstream payloads
// never end up in an error message. A body that is not JSON, has no detail, or
// carries a non-string detail (e.g. a validation error list) leaves base as-is.
func withDetail(base error, raw []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return base
	}
	detail, _ := payload["detail"].(string)
	if detail == "" {
		return base
	}
	return fmt.Errorf("%w: %s", base, detail)
}

func writeImageFields(w *multipart.Writer, fieldName string, images []ImagePayload) error {
	for _, img := range images {
		h := make(map[string][]string)
		h["Content-Disposition"] = []string{fmt.Sprintf(
			`form-data; name="%s"; filename="%s"`,
			quoteEscaper.Replace(fieldName), quoteEscaper.Replace(img.Filename),
		)}
		h["Content-Type"] = []string{img.ContentType}

		part, err := w.CreatePart(h)
		if err != nil {
			return fmt.Errorf("create multipart part: %w", err)
		}
		if _, err := part.Write(img.Data); err != nil {
			return fmt.Errorf("write image data: %w", err)
		}
	}
	return nil
}
