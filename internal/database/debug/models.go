// Package debug owns the developer-facing record of what the identification
// model actually did: registrations it rejected, and every search it answered.
//
// It is deliberately separate from the animal package. These tables are read by
// the internal dashboard, not by the mobile app, and they hold no foreign keys
// into the user-facing schema — see the migration for why.
package debug

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Decision values. The first three come from the search decision engine; a
// FAILED row is one where the model rejected the images outright.
const (
	DecisionMatch   = "MATCH"
	DecisionReview  = "REVIEW"
	DecisionUnknown = "UNKNOWN"
	DecisionFailed  = "FAILED"
)

// Verification states for a MATCH, settled by a human on the dashboard.
const (
	VerifiedYes = "yes"
	VerifiedNo  = "no"
	VerifiedNot = "not_verified"
)

// ValidVerification reports whether s is a verification state the API accepts.
func ValidVerification(s string) bool {
	return s == VerifiedYes || s == VerifiedNo || s == VerifiedNot
}

// JSONMap is a free-form object in a jsonb column. Value renders it as a string
// rather than []byte, which the driver would otherwise send as bytea; Postgres
// then infers jsonb from the target column. Do not add an explicit ::jsonb cast
// to the parameter — sqlx cannot parse `:name::type` and the whole statement
// fails to compile. See TestNamedQueriesCompile.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonb: %w", err)
	}
	return string(b), nil
}

func (m *JSONMap) Scan(src any) error {
	raw, err := jsonBytes(src)
	if err != nil || raw == nil {
		return err
	}
	return json.Unmarshal(raw, m)
}

// JSONStrings is a string array in a jsonb column, used for image keys.
type JSONStrings []string

func (s JSONStrings) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonb: %w", err)
	}
	return string(b), nil
}

func (s *JSONStrings) Scan(src any) error {
	raw, err := jsonBytes(src)
	if err != nil || raw == nil {
		return err
	}
	return json.Unmarshal(raw, s)
}

// LightglueCandidateEvidence is one top-K embedding candidate's raw score
// plus its LightGlue re-rank evidence, as computed for a single search (see
// go-apiserver's rerankByLightglue and inference_server's pipeline/rerank.py).
// Stored as a JSONB array on animal_search_records so a past search's full
// per-candidate picture — not just the decided top-1 — can be pulled up
// later for recalibration. LightglueNumMatches/LightglueMatchRatio/
// LightglueRank are all nil for a candidate with no cached crop to compare
// against (see MUZZLE_CROP_CACHE_DIR's coverage gap) — that is the expected,
// common case for animals registered before the LightGlue feature cache
// shipped, not a data-quality problem to flag.
type LightglueCandidateEvidence struct {
	GodhaarID           string   `json:"godhaar_id"`
	FaissID             int64    `json:"faiss_id"`
	EmbeddingScore      float64  `json:"embedding_score"`
	EmbeddingRank       int      `json:"embedding_rank"`
	LightglueNumMatches *int     `json:"lightglue_num_matches"`
	LightglueMatchRatio *float64 `json:"lightglue_match_ratio"`
	LightglueRank       *int     `json:"lightglue_rank"`
	CombinedRank        int      `json:"combined_rank"`
}

// LightglueCandidates is a jsonb array column — nil (-> SQL NULL) when the
// reranker never ran for this search (fewer than 2 candidates, or no
// candidate had any LightGlue evidence at all).
type LightglueCandidates []LightglueCandidateEvidence

func (c LightglueCandidates) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonb: %w", err)
	}
	return string(b), nil
}

func (c *LightglueCandidates) Scan(src any) error {
	raw, err := jsonBytes(src)
	if err != nil || raw == nil {
		return err
	}
	return json.Unmarshal(raw, c)
}

func jsonBytes(src any) ([]byte, error) {
	switch v := src.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("scan jsonb: unsupported source type %T", src)
	}
}

// DeviceColumns is the client context every record carries. The fields are
// pointers so a header the app never sent is stored as NULL rather than as an
// empty string — "old client did not report this" and "client reported nothing"
// are different findings when triaging a spike.
type DeviceColumns struct {
	AppVersion         *string `db:"app_version"`
	OSVersion          *string `db:"os_version"`
	DeviceModel        *string `db:"device_model"`
	DeviceManufacturer *string `db:"device_manufacturer"`
}

// CreateRegistrationFailure is a registration the model refused, kept with the
// images that caused it so the rejection can be reproduced.
type CreateRegistrationFailure struct {
	DeviceColumns
	ErrorCode      string      `db:"error_code"`
	ImageKeys      JSONStrings `db:"image_keys"`
	Detail         JSONMap     `db:"detail"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail string      `db:"created_by_email"`
}

// CreateSearchRecord is one search attempt of any outcome. GodhaarID is set
// only for a MATCH, ErrorCode only for a FAILED, and Score for everything the
// model actually scored.
type CreateSearchRecord struct {
	DeviceColumns
	Decision       string      `db:"decision"`
	GodhaarID      *string     `db:"godhaar_id"`
	Score          *float64    `db:"score"`
	ErrorCode      *string     `db:"error_code"`
	ImageKeys      JSONStrings `db:"image_keys"`
	Detail         JSONMap     `db:"detail"`
	// LightglueCandidates is the full top-K per-candidate re-rank evidence —
	// see its type doc comment. Nil (-> SQL NULL) when the reranker never ran.
	LightglueCandidates LightglueCandidates `db:"lightglue_candidates"`
	CreatedBy           string              `db:"created_by"`
	CreatedByEmail      string              `db:"created_by_email"`
}

// RegistrationFailureRow is one failure in full, for the detail view.
type RegistrationFailureRow struct {
	DeviceColumns
	RegistrationID string      `db:"registration_id"`
	ErrorCode      string      `db:"error_code"`
	ImageKeys      JSONStrings `db:"image_keys"`
	Detail         JSONMap     `db:"detail"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail *string     `db:"created_by_email"`
	CreatedAt      time.Time   `db:"created_at"`
}

// RegistrationFailureListRow is the same record reduced to what a listing
// needs: every field the dashboard filters or sorts on, plus the image keys the
// card thumbnail comes from. The detail jsonb is left behind — it is the
// largest column and nothing on the listing reads it.
type RegistrationFailureListRow struct {
	DeviceColumns
	RegistrationID string      `db:"registration_id"`
	ErrorCode      string      `db:"error_code"`
	ImageKeys      JSONStrings `db:"image_keys"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail *string     `db:"created_by_email"`
	CreatedAt      time.Time   `db:"created_at"`
}

// SearchRecordRow is one search in full, for the detail view.
//
// It carries no columns from the matched animal. What the dashboard needs about
// that animal is its identity and its photos, and both are read separately by
// MatchedAnimalImages — joining the animals table here would drag in breed, age
// and location, none of which say anything about how the model performed.
type SearchRecordRow struct {
	DeviceColumns
	SearchID       string      `db:"search_id"`
	Decision       string      `db:"decision"`
	GodhaarID      *string     `db:"godhaar_id"`
	Score          *float64    `db:"score"`
	ErrorCode      *string     `db:"error_code"`
	Verified       string      `db:"verified"`
	ImageKeys      JSONStrings `db:"image_keys"`
	Detail         JSONMap     `db:"detail"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail *string     `db:"created_by_email"`
	CreatedAt      time.Time   `db:"created_at"`
}

// SearchRecordListRow is the listing form. godhaar_id, decision and verified
// all live on the record itself, so a listing needs no join at all.
type SearchRecordListRow struct {
	DeviceColumns
	SearchID string `db:"search_id"`
	Decision string `db:"decision"`

	// Verifiable is computed by the listing query (see verifiableSearch), not
	// stored. The dashboard cannot derive it: a REVIEW's candidate lives in
	// detail, which the listing deliberately does not carry.
	Verifiable bool `db:"verifiable"`

	GodhaarID      *string     `db:"godhaar_id"`
	Score          *float64    `db:"score"`
	ErrorCode      *string     `db:"error_code"`
	Verified       string      `db:"verified"`
	ImageKeys      JSONStrings `db:"image_keys"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail *string     `db:"created_by_email"`
	CreatedAt      time.Time   `db:"created_at"`
}

// AnimalImage is one stored photo of a registered animal, carrying the slot it
// was taken for. Unlike the debug images — whose slot has to be recovered from
// the object key — these are labelled by the images table itself.
type AnimalImage struct {
	ImageType string `db:"image_type"`
	Sequence  int    `db:"sequence"`
	ImageKey  string `db:"image_key"`
}

// MuzzleEmbeddingRow is one registered muzzle embedding and the stored image it
// was computed from — the join inference_server has no way to make itself,
// since it holds neither the animals/embeddings/images tables nor object
// storage credentials (see inference_server's pipeline/muzzle_crop_cache.py).
// For MuzzleEmbeddings, the backfill script's one purpose.
type MuzzleEmbeddingRow struct {
	FaissID   int64  `db:"faiss_id"`
	GodhaarID string `db:"godhaar_id"`
	Sequence  int    `db:"sequence"`
	ImageKey  string `db:"image_key"`
}

// Slot sets for MatchedAnimalImages. Both exclude the certificates, which play
// no part in identification.
var (
	// SearchSlots are the four slots a search compares against.
	SearchSlots = []string{"front", "muzzle", "left", "right"}

	// DuplicateSlots are the slots a registration duplicate is decided on. The
	// side photos are stored but never embedded, so putting them next to a
	// rejected registration would invite a reviewer to weigh evidence the model
	// never saw.
	DuplicateSlots = []string{"front", "muzzle"}
)

// DetailKeyMatchedGodhaarID is where a registration failure records the animal
// a duplicate rejection resolved to. It lives in the detail payload rather than
// in a column because only one error code ever sets it — see
// captureRegistrationFailure.
const DetailKeyMatchedGodhaarID = "matched_godhaar_id"

// MatchedGodhaarID reads the resolved duplicate out of a detail payload. Absent
// for every error code except DUPLICATE_ANIMAL, and absent even there when the
// matched FAISS id could not be mapped back to an animal.
func MatchedGodhaarID(detail JSONMap) *string {
	godhaarID, ok := detail[DetailKeyMatchedGodhaarID].(string)
	if !ok || godhaarID == "" {
		return nil
	}
	return &godhaarID
}

// DetailKeyTopCandidate is where a REVIEW records the animal it ranked first
// WITHOUT claiming it. Deliberately not the godhaar_id column: that column
// means "the model says this is the animal", and a REVIEW says precisely the
// opposite (see captureSearch).
const DetailKeyTopCandidate = "top_candidate"

// TopCandidateGodhaarID reads a REVIEW's unclaimed top candidate out of a
// detail payload. Absent on every other decision, and absent on a REVIEW that
// ranked nothing at all.
func TopCandidateGodhaarID(detail JSONMap) *string {
	godhaarID, ok := detail[DetailKeyTopCandidate].(string)
	if !ok || godhaarID == "" {
		return nil
	}
	return &godhaarID
}

// VerifiableGodhaarID is the animal a human is being asked to judge: the claim
// on a MATCH, the unclaimed top candidate on a REVIEW, nothing otherwise.
//
// It is the one definition of "this search named an animal", and it has to stay
// in step with UpdateSearchVerification's WHERE clause. If this accepts a row
// that the WHERE clause refuses, the dashboard renders a verify button that
// answers 409.
func VerifiableGodhaarID(decision string, godhaarID *string, detail JSONMap) *string {
	switch decision {
	case DecisionMatch:
		return godhaarID
	case DecisionReview:
		return TopCandidateGodhaarID(detail)
	default:
		return nil
	}
}
