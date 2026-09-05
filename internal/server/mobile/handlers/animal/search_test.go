package animal

import (
	"context"
	"testing"

	dbanimal "go-api-server/internal/database/animal"
)

// fakeRepo implements Repository with only FindFAISSCandidates overridden; the
// other methods are never called by searchCandidates, so the embedded nil
// interface is safe.
type fakeRepo struct {
	Repository
	rows []dbanimal.CandidateRow
}

func (f *fakeRepo) FindFAISSCandidates(_ context.Context, _, _, _ float64) ([]dbanimal.CandidateRow, error) {
	return f.rows, nil
}

func newSearchHandler(rows []dbanimal.CandidateRow) *Handler {
	return &Handler{DB: &fakeRepo{rows: rows}}
}

func TestSearchCandidatesRadiusTiers(t *testing.T) {
	// Candidate locations chosen so each tier is exercised: ~1.1 km (primary),
	// ~11 km (second tier), ~111 km (unbounded only).
	cases := []struct {
		name       string
		lat, lng   float64
		wantRadius float64
		wantN      int
	}{
		{"primary tier wins", 0.01, 0, 3.0, 1},
		{"second tier wins", 0.10, 0, 50.0, 1},
		{"unbounded fallback", 1.0, 0, 0.0, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := []dbanimal.CandidateRow{
				{FaissID: 1, GodhaarID: "G1", Latitude: tc.lat, Longitude: tc.lng},
			}
			h := newSearchHandler(rows)

			got, radius, err := h.searchCandidates(context.Background(), 0, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if radius != tc.wantRadius {
				t.Fatalf("radius = %v, want %v", radius, tc.wantRadius)
			}
			if len(got) != tc.wantN {
				t.Fatalf("candidates = %d, want %d", len(got), tc.wantN)
			}
		})
	}
}

func TestSearchCandidatesEmpty(t *testing.T) {
	h := newSearchHandler(nil)

	got, radius, err := h.searchCandidates(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if radius != 0 || len(got) != 0 {
		t.Fatalf("got radius=%v candidates=%d, want radius=0 candidates=0", radius, len(got))
	}
}
