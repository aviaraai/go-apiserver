package animal

import (
	"context"
	"encoding/json"
	"testing"

	"go-api-server/internal/inference"
)

func i64(v int64) *int64 { return &v }

// expectedGodhaar resolves ids the long way round — build the reverse map from
// scratch and look each one up — rather than reusing translateDecision's own
// traversal, so a bug in that traversal cannot make the expectation agree with
// it.
func expectedGodhaar(t *testing.T, m map[int64]string, ids []int64) []string {
	t.Helper()
	rev := map[int64]string{}
	for k, v := range m {
		rev[k] = v
	}
	var out []string
	for _, id := range ids {
		if g, ok := rev[id]; ok {
			out = append(out, g)
		}
	}
	return out
}

func TestTranslateDecision(t *testing.T) {
	full := map[int64]string{10: "GH-A", 20: "GH-B", 30: "GH-C"}

	tests := []struct {
		name         string
		in           *inference.InferenceDecision
		mapping      map[int64]string
		wantDecision string
		wantGodhaar  *string
		wantCands    []int64 // ids expected to survive; resolved independently
		wantUnmapped []int64
		wantDegraded bool
	}{
		{
			name:         "nil decision from an older inference build",
			in:           nil,
			mapping:      full,
			wantDecision: "", // translateDecision returns nil; asserted separately
		},
		{
			name:         "MATCH resolves to one godhaar_id",
			in:           &inference.InferenceDecision{Decision: "MATCH", Reason: "high_score_clear_gap", Score: 0.91, Gap: 0.33, FaissID: i64(20)},
			mapping:      full,
			wantDecision: "MATCH",
			wantGodhaar:  strPtr("GH-B"),
		},
		{
			name:         "REVIEW resolves all three in order",
			in:           &inference.InferenceDecision{Decision: "REVIEW", Reason: "high_score_ambiguous_gap", FaissIDs: []int64{30, 10, 20}},
			mapping:      full,
			wantDecision: "REVIEW",
			wantCands:    []int64{30, 10, 20},
		},
		{
			name:         "REVIEW with fewer than three is not padded",
			in:           &inference.InferenceDecision{Decision: "REVIEW", FaissIDs: []int64{10}},
			mapping:      full,
			wantDecision: "REVIEW",
			wantCands:    []int64{10},
		},
		{
			name:         "UNKNOWN names nothing and stays UNKNOWN",
			in:           &inference.InferenceDecision{Decision: "UNKNOWN", Reason: "below_review_threshold"},
			mapping:      full,
			wantDecision: "UNKNOWN",
		},
		{
			// The blocking case: a stale index. Demoting is the honest floor —
			// naming an animal this service cannot produce would render blank.
			name:         "MATCH on an unmapped faiss_id demotes to UNKNOWN",
			in:           &inference.InferenceDecision{Decision: "MATCH", Reason: "high_score_clear_gap", FaissID: i64(999)},
			mapping:      full,
			wantDecision: "UNKNOWN",
			wantUnmapped: []int64{999},
			wantDegraded: true,
		},
		{
			name:         "REVIEW keeps the resolvable candidates and drops the rest",
			in:           &inference.InferenceDecision{Decision: "REVIEW", FaissIDs: []int64{10, 999, 30}},
			mapping:      full,
			wantDecision: "REVIEW",
			wantCands:    []int64{10, 30},
			wantUnmapped: []int64{999},
			wantDegraded: true,
		},
		{
			name:         "REVIEW with every candidate unmapped falls to UNKNOWN",
			in:           &inference.InferenceDecision{Decision: "REVIEW", FaissIDs: []int64{998, 999}},
			mapping:      full,
			wantDecision: "UNKNOWN",
			wantUnmapped: []int64{998, 999},
			wantDegraded: true,
		},
		{
			name:         "MATCH without an id is a contract violation, fails closed",
			in:           &inference.InferenceDecision{Decision: "MATCH", FaissID: nil},
			mapping:      full,
			wantDecision: "UNKNOWN",
			wantDegraded: true,
		},
		{
			name:         "empty candidate map resolves nothing",
			in:           &inference.InferenceDecision{Decision: "MATCH", FaissID: i64(10)},
			mapping:      map[int64]string{},
			wantDecision: "UNKNOWN",
			wantUnmapped: []int64{10},
			wantDegraded: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := translateDecision(context.Background(), tc.in, tc.mapping, "req-test")
			if tc.in == nil {
				if got != nil {
					t.Fatalf("nil decision must translate to nil, got %+v", got)
				}
				return
			}
			if got.Decision != tc.wantDecision {
				t.Errorf("decision = %q, want %q", got.Decision, tc.wantDecision)
			}
			if tc.wantGodhaar == nil && got.GodhaarID != nil {
				t.Errorf("godhaar_id = %q, want nil", *got.GodhaarID)
			}
			if tc.wantGodhaar != nil && (got.GodhaarID == nil || *got.GodhaarID != *tc.wantGodhaar) {
				t.Errorf("godhaar_id = %v, want %q", got.GodhaarID, *tc.wantGodhaar)
			}
			want := expectedGodhaar(t, tc.mapping, tc.wantCands)
			if len(got.GodhaarIDs) != len(want) {
				t.Fatalf("candidates = %v, want %v", got.GodhaarIDs, want)
			}
			for i := range want {
				if got.GodhaarIDs[i] != want[i] {
					t.Errorf("candidate[%d] = %q, want %q", i, got.GodhaarIDs[i], want[i])
				}
			}
			if len(got.Unmapped) != len(tc.wantUnmapped) {
				t.Errorf("unmapped = %v, want %v", got.Unmapped, tc.wantUnmapped)
			}
			if got.Degraded != tc.wantDegraded {
				t.Errorf("degraded = %v, want %v", got.Degraded, tc.wantDegraded)
			}
			// Contract: never both a single id and a candidate list.
			if got.GodhaarID != nil && len(got.GodhaarIDs) > 0 {
				t.Errorf("both godhaar_id and candidates populated: %v / %v", *got.GodhaarID, got.GodhaarIDs)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

// TestUnchangedClientStillParses is the compatibility proof. An app built
// against the OLD three-field response must decode the new one without error
// and read identical values — that is what makes this deployable without an
// app release.
func TestUnchangedClientStillParses(t *testing.T) {
	gid := "GH-B"
	warn := "search ran on 1 muzzle photo(s)"
	newResp := SearchResponse{
		GodhaarID: &gid,
		Decision:  "REVIEW",
		Score:     0.77,
		InferenceDecision: &InferenceDecisionView{
			Decision:   "REVIEW",
			Reason:     "high_score_ambiguous_gap",
			Score:      0.77,
			Gap:        0.04,
			Candidates: []string{"GH-B", "GH-A"},
		},
		MuzzlePhotoCount:   1,
		CalibrationWarning: &warn,
	}
	raw, err := json.Marshal(newResp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Exactly the struct an un-updated app has.
	var old struct {
		GodhaarID *string `json:"godhaar_id"`
		Decision  string  `json:"decision"`
		Score     float64 `json:"score"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatalf("an unchanged client must still parse this response: %v", err)
	}
	if old.GodhaarID == nil || *old.GodhaarID != gid {
		t.Errorf("godhaar_id = %v, want %q", old.GodhaarID, gid)
	}
	if old.Decision != "REVIEW" || old.Score != 0.77 {
		t.Errorf("decision/score = %q/%v, want REVIEW/0.77", old.Decision, old.Score)
	}
}

// TestUnknownOmitsCandidatesRatherThanEmptyArray pins the null-vs-[] choice:
// a client checking for presence must not see an empty list for MATCH/UNKNOWN.
func TestUnknownOmitsCandidatesRatherThanEmptyArray(t *testing.T) {
	for _, d := range []string{"MATCH", "UNKNOWN"} {
		view := InferenceDecisionView{Decision: d}
		raw, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if m["candidates"] != nil {
			t.Errorf("%s: candidates = %v, want null", d, m["candidates"])
		}
	}
}

// TestOlderInferenceBuildDecodesAsNil: a response with no `decision` key must
// leave Decision nil, which means "inference did not decide" — never UNKNOWN.
func TestOlderInferenceBuildDecodesAsNil(t *testing.T) {
	raw := []byte(`{"request_id":"r","top_matches":[],"lightglue_checked":false}`)
	var out inference.SearchResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("older inference payload must decode: %v", err)
	}
	if out.Decision != nil {
		t.Errorf("Decision = %+v, want nil", out.Decision)
	}
	if got := translateDecision(context.Background(), out.Decision, map[int64]string{}, "r"); got != nil {
		t.Errorf("nil decision must translate to nil, got %+v", got)
	}
}
