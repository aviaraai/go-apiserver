package debug

import "testing"

func strp(s string) *string { return &s }

// VerifiableGodhaarID decides which searches the dashboard offers a verify
// button for. UpdateSearchVerification's WHERE clause decides which ones the
// database will actually accept a verdict on. They are written in two different
// languages in two different files, and if they disagree the dashboard renders
// a button that answers 409 — so the table below is the shared contract, and
// the SQL is quoted next to each case it stands for.
func TestVerifiableGodhaarID(t *testing.T) {
	tests := []struct {
		name     string
		decision string
		column   *string
		detail   JSONMap
		want     *string
	}{
		{
			// SQL: decision = 'MATCH'. The claim lives in the column.
			name:     "MATCH is verifiable on its claimed id",
			decision: DecisionMatch,
			column:   strp("UKDEOT142642"),
			want:     strp("UKDEOT142642"),
		},
		{
			// SQL: decision = 'REVIEW' AND detail->>'top_candidate' IS NOT NULL.
			// This is the case the whole change exists for: a miss is only
			// measurable if a human can be asked about the candidate.
			name:     "REVIEW is verifiable on its unclaimed top candidate",
			decision: DecisionReview,
			column:   nil,
			detail:   JSONMap{DetailKeyTopCandidate: "UKDEOT142642"},
			want:     strp("UKDEOT142642"),
		},
		{
			// A REVIEW that ranked nothing has nothing to ask about. The SQL
			// refuses it via IS NOT NULL; this must refuse it too.
			name:     "REVIEW with no candidate is not verifiable",
			decision: DecisionReview,
			detail:   JSONMap{"reason": "mid_range_score"},
			want:     nil,
		},
		{
			name:     "REVIEW with an empty candidate string is not verifiable",
			decision: DecisionReview,
			detail:   JSONMap{DetailKeyTopCandidate: ""},
			want:     nil,
		},
		{
			// UNKNOWN never carries an id — asserted in CLAUDE.md and relied on
			// by the mobile client. Even if a stray detail key appeared, an
			// UNKNOWN is not a statement about any animal.
			name:     "UNKNOWN is never verifiable",
			decision: DecisionUnknown,
			detail:   JSONMap{DetailKeyTopCandidate: "UKDEOT142642"},
			want:     nil,
		},
		{
			name:     "FAILED is never verifiable",
			decision: DecisionFailed,
			detail:   JSONMap{DetailKeyTopCandidate: "UKDEOT142642"},
			want:     nil,
		},
		{
			// Defensive: a MATCH row whose column is somehow empty must not
			// silently fall back to a detail key.
			name:     "MATCH with no stored id is not verifiable",
			decision: DecisionMatch,
			column:   nil,
			detail:   JSONMap{DetailKeyTopCandidate: "UKDEOT142642"},
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VerifiableGodhaarID(tt.decision, tt.column, tt.detail)
			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("want not verifiable, got %q", *got)
			case tt.want != nil && got == nil:
				t.Fatalf("want %q, got not verifiable", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Fatalf("want %q, got %q", *tt.want, *got)
			}
		})
	}
}

// The two readers must not drift apart: TopCandidateGodhaarID is what the
// REVIEW branch above is built on, and a duplicate registration's
// matched_godhaar_id is a different key entirely.
func TestDetailKeysAreDistinct(t *testing.T) {
	if DetailKeyTopCandidate == DetailKeyMatchedGodhaarID {
		t.Fatal("a search's top candidate and a duplicate's matched animal are different facts and must not share a key")
	}

	detail := JSONMap{DetailKeyMatchedGodhaarID: "DUP-1"}
	if got := TopCandidateGodhaarID(detail); got != nil {
		t.Fatalf("a duplicate's matched id must not read as a search candidate, got %q", *got)
	}
}
