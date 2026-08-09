package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Candidate struct {
	FaissID     int64   `json:"faiss_id"`
	BodyColor   string  `json:"body_color"`
	MuzzleColor string  `json:"muzzle_color"`
	HornShape   *string `json:"horn_shape"`
	TagID       *string `json:"tag_id"`
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

// registerEmbeddingCount is the number of muzzle embeddings a successful
// registration must yield — one per muzzle image. The caller writes exactly
// this many embedding rows, so a different count is a contract violation and
// is rejected here rather than half-applied downstream.
const registerEmbeddingCount = 3

type Client interface {
	Register(ctx context.Context, front, muzzle []ImagePayload, candidates []Candidate, tagID *string) (*RegisterResponse, error)
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

// Register and Search follow the same three steps: build the body, exchange it
// with the server, then check the success payload actually says what we need.
// Only the middle step can produce a domain verdict — everything the first and
// third step can go wrong with is our own fault, and is classified as such.

func (c *HTTPClient) Register(ctx context.Context, front, muzzle []ImagePayload, candidates []Candidate, tagID *string) (*RegisterResponse, error) {
	body, contentType, err := buildRegisterBody(front, muzzle, candidates, tagID)
	if err != nil {
		return nil, transportError(CodeUnavailable, 0, err)
	}

	var out RegisterResponse
	status, err := c.exchange(ctx, "/register", body, contentType, &out)
	if err != nil {
		return nil, err
	}

	if err := validateRegisterResponse(&out); err != nil {
		return nil, contractError(status, err)
	}
	return &out, nil
}

func (c *HTTPClient) Search(ctx context.Context, front, muzzle ImagePayload, candidates []Candidate, topK int) (*SearchResponse, error) {
	body, contentType, err := buildSearchBody(front, muzzle, candidates, topK)
	if err != nil {
		return nil, transportError(CodeUnavailable, 0, err)
	}

	var out SearchResponse
	status, err := c.exchange(ctx, "/search", body, contentType, &out)
	if err != nil {
		return nil, err
	}

	if err := validateSearchResponse(&out); err != nil {
		return nil, contractError(status, err)
	}
	return &out, nil
}

// exchange sends a prepared body and decodes a success payload into out. Every
// error it returns is already classified, and every one of them is safe to hand
// to a caller: the failing URL, the dial error and the response body all stay
// inside Error.RawError.
func (c *HTTPClient) exchange(ctx context.Context, path string, body *bytes.Buffer, contentType string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return 0, transportError(CodeUnavailable, 0, fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, transportError(CodeUnavailable, 0, fmt.Errorf("inference request: %w", err))
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, transportError(CodeUnavailable, resp.StatusCode, fmt.Errorf("read inference response: %w", err))
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return resp.StatusCode, decodeErrorBody(resp.StatusCode, rawBody)
	}

	if err := json.Unmarshal(rawBody, out); err != nil {
		return resp.StatusCode, contractError(resp.StatusCode, fmt.Errorf("decode inference response: %w, body: %s", err, rawBody))
	}
	return resp.StatusCode, nil
}

// validateRegisterResponse rejects a success body that decoded cleanly but does
// not carry what registration needs. An empty JSON object unmarshals without
// error, so without these checks a malformed 200 would reach the database as
// blank colours and no embeddings.
func validateRegisterResponse(r *RegisterResponse) error {
	if len(r.EmbeddingIDs) != registerEmbeddingCount {
		return fmt.Errorf("expected %d embedding ids, got %d", registerEmbeddingCount, len(r.EmbeddingIDs))
	}
	for i, id := range r.EmbeddingIDs {
		if id == 0 {
			return fmt.Errorf("embedding id %d is zero", i)
		}
	}
	if r.ExtractedColors.Body.Label == "" || r.ExtractedColors.Muzzle.Label == "" {
		return fmt.Errorf("missing extracted colour labels (body=%q muzzle=%q)",
			r.ExtractedColors.Body.Label, r.ExtractedColors.Muzzle.Label)
	}
	return nil
}

// validateSearchResponse checks the parts of a search result that must always
// be present. An empty TopMatches is deliberately allowed — "nothing scored
// well enough" is a real answer — but the colour labels are extracted on every
// query, so their absence means the body was not a real search result. Without
// that check a malformed 200 would be indistinguishable from a genuine no-match
// and would quietly register as an UNKNOWN verdict.
func validateSearchResponse(r *SearchResponse) error {
	if r.QueryColors.Body.Label == "" || r.QueryColors.Muzzle.Label == "" {
		return fmt.Errorf("missing query colour labels (body=%q muzzle=%q)",
			r.QueryColors.Body.Label, r.QueryColors.Muzzle.Label)
	}
	for i, m := range r.TopMatches {
		if m.FaissID == 0 {
			return fmt.Errorf("match %d has zero faiss_id", i)
		}
		// Scores are cosine similarities over normalised embeddings, so they
		// live in [-1, 1]. Anything outside that is not a score we can reason
		// about, and letting it through would be dangerous rather than merely
		// wrong: a single absurd value sails past every threshold in the
		// decision engine and produces a confident MATCH.
		if math.IsNaN(m.Score) || m.Score < -1 || m.Score > 1 {
			return fmt.Errorf("match %d has out-of-range score %v", i, m.Score)
		}
	}
	return nil
}

func buildRegisterBody(front, muzzle []ImagePayload, candidates []Candidate, tagID *string) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// cost is Optional[str] on the inference side: omitting the field entirely is
	// how "not priced" is expressed, so a nil cost writes nothing rather than an
	// empty string. The integer formatting matches Candidate.Cost above, so the
	// query and the existing records the model compares it against are written
	// the same way.
	if tagID != nil {
		if err := writer.WriteField("tag_no", *tagID); err != nil {
			return nil, "", fmt.Errorf("write cost field: %w", err)
		}
	}
	if err := writeCandidates(writer, candidates); err != nil {
		return nil, "", err
	}
	if err := writeImageFields(writer, "front_images", front); err != nil {
		return nil, "", err
	}
	if err := writeImageFields(writer, "muzzle_images", muzzle); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}

func buildSearchBody(front, muzzle ImagePayload, candidates []Candidate, topK int) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if err := writeCandidates(writer, candidates); err != nil {
		return nil, "", err
	}

	// Field names here must match the inference /search signature exactly:
	// muzzle (File), front (File), top_k (Form), candidates (Form).
	// A mismatch produces a FastAPI request-validation 422, which classifies as
	// a contract violation rather than an image complaint.
	if err := writeImageFields(writer, "muzzle", []ImagePayload{muzzle}); err != nil {
		return nil, "", err
	}
	if err := writeImageFields(writer, "front", []ImagePayload{front}); err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("top_k", strconv.Itoa(topK)); err != nil {
		return nil, "", fmt.Errorf("write top_k field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}

func writeCandidates(w *multipart.Writer, candidates []Candidate) error {
	candidatesJSON, err := json.Marshal(candidates)
	if err != nil {
		return fmt.Errorf("marshal candidates: %w", err)
	}
	if err := w.WriteField("candidates", string(candidatesJSON)); err != nil {
		return fmt.Errorf("write candidates field: %w", err)
	}
	return nil
}

// quoteEscaper mirrors the escaping used by mime/multipart's CreateFormFile:
// only backslash, double-quote, CR, and LF are escaped. Using fmt's %q instead
// would escape non-ASCII runes to \uXXXX/\xXX, mangling unicode filenames.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"", "\r", "%0D", "\n", "%0A")

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
