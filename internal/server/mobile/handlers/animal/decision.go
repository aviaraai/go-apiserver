package animal

import "math"

// rankedAnimal is one cattle-level entry after aggregating embedding scores to
// the max score per animal and sorting descending.
type rankedAnimal struct {
	GodhaarID string
	Score     float64
}

// verdict is the decision engine's output. AnimalID is the internal DB foreign
// key; the handler translates it to the public godhaar_id before responding.
type verdict struct {
	Decision  string
	GodhaarID *string
	Score     float64
	Gap       float64
	Reason    string
}

// Decision thresholds — sourced from the Godhaar V1 Architecture document.
const (
	matchThreshold  = 0.82
	reviewThreshold = 0.72
	gapThreshold    = 0.08
)

// decide maps a cattle-level ranked list (sorted by score descending) to a
// final identification decision. Ported from app/utils/decision.py.
func decide(ranked []rankedAnimal) verdict {
	if len(ranked) == 0 {
		return verdict{Decision: "UNKNOWN", Score: 0, Gap: 0, Reason: "no_candidates"}
	}

	top := ranked[0]
	// Gap between rank-1 and rank-2; 0.0 when there is only one candidate.
	secondScore := top.Score
	if len(ranked) > 1 {
		secondScore = ranked[1].Score
	}
	gap := top.Score - secondScore

	id := top.GodhaarID
	v := verdict{GodhaarID: &id, Score: round6(top.Score), Gap: round6(gap)}

	switch {
	case top.Score >= matchThreshold && gap >= gapThreshold:
		v.Decision, v.Reason = "MATCH", "high_score_clear_gap"
	case top.Score >= matchThreshold:
		v.Decision, v.Reason = "REVIEW", "high_score_ambiguous_gap"
	case top.Score >= reviewThreshold:
		v.Decision, v.Reason = "REVIEW", "mid_range_score"
	default:
		v.Decision, v.Reason = "UNKNOWN", "below_review_threshold"
	}
	return v
}

func round6(x float64) float64 {
	return math.Round(x*1e6) / 1e6
}
