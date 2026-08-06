package animal

import (
	"math"
	"sort"
	"strings"
)

// animalAttributes are the coarse visual traits stored against a registered
// animal. They are far weaker evidence than a muzzle embedding, but they are
// independent of it, which is what makes them useful for breaking ties.
type animalAttributes struct {
	BodyColor   string
	MuzzleColor string
	HornShape   *string
}

// queryAttributes are the same traits extracted from the photos being searched.
type queryAttributes struct {
	BodyColor   string
	MuzzleColor string
	HornShape   *string
}

// rankedAnimal is one cattle-level entry: the best embedding score any of its
// embeddings achieved, and what the attribute comparison did to it.
type rankedAnimal struct {
	GodhaarID string

	// Score is the raw maximum embedding score, kept separate so a verdict can
	// still be read against the model's own output.
	Score float64

	// Adjusted is Score after attribute agreement, and is what the thresholds
	// and the ranking are applied to.
	Adjusted float64

	// Agreement is in [-1, 1]: 1 when every comparable attribute matched, -1
	// when every one disagreed, 0 when there was nothing to compare.
	Agreement float64

	// Compared counts the attributes that had a value on both sides.
	Compared int
}

// verdict is the decision engine's output.
type verdict struct {
	Decision  string
	GodhaarID *string

	// Score is the model's own similarity score for the top candidate. It is
	// what gets reported and stored, deliberately unmodified: the adjusted
	// value is an internal ranking device, and publishing it would mean
	// emitting a confidence figure the model never produced — one that can sit
	// outside the score's natural range.
	Score float64

	// AdjustedScore is Score after attribute agreement, kept for the logs and
	// the dashboard so a demotion can be explained.
	AdjustedScore float64

	Gap       float64
	Agreement float64
	Reason    string
}

// Decision thresholds — sourced from the Godhaar V1 Architecture document.
const (
	matchThreshold  = 0.82
	reviewThreshold = 0.72
	gapThreshold    = 0.08
)

// attributeWeight bounds how far attribute agreement can move a score.
//
// It is small on purpose. The colour and horn classifiers are not reliable
// enough to overrule the muzzle embedding, so this is a tie-breaker, not a
// filter. Disagreement never rejects a candidate outright either — an animal
// whose coat was recorded as brown and photographs as black in poor light is
// still the same animal.
//
// The specific value matters, and two safety properties depend on it:
//
//   - 2*attributeWeight < gapThreshold (0.06 < 0.08). The largest gap the
//     attribute term can manufacture between two identically-scored candidates
//     is 0.06, so attributes alone can never produce the clear gap a MATCH
//     requires. A MATCH always rests on a real separation in embedding scores.
//
//   - The same relation means that whenever attributes reorder the top two
//     candidates, their adjusted gap is necessarily below gapThreshold, so the
//     result can only be REVIEW or UNKNOWN. Attributes can never hand a
//     confident identification to a different animal than the embeddings
//     picked. TestAttributesCannotConfidentlyReorder proves this over random
//     inputs.
//
// Raising attributeWeight to 0.04 or above breaks both properties.
const attributeWeight = 0.03

// attributeAgreement scores how well a candidate's recorded traits match the
// ones extracted from the query, over just the traits both sides actually know.
// An attribute missing on either side is not evidence in either direction:
// horn_shape in particular is nullable for polled breeds and for animals
// registered before the classifier existed, and treating "unknown" as a
// mismatch would penalise exactly those.
func attributeAgreement(q queryAttributes, a animalAttributes) (agreement float64, compared int) {
	var matches, mismatches int

	compare := func(x, y string) {
		x, y = normalizeLabel(x), normalizeLabel(y)
		if x == "" || y == "" {
			return
		}
		compared++
		if x == y {
			matches++
		} else {
			mismatches++
		}
	}

	compare(q.BodyColor, a.BodyColor)
	compare(q.MuzzleColor, a.MuzzleColor)
	compare(derefLabel(q.HornShape), derefLabel(a.HornShape))

	if compared == 0 {
		return 0, 0
	}
	return float64(matches-mismatches) / float64(compared), compared
}

func normalizeLabel(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func derefLabel(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// rankCandidates aggregates embedding scores to cattle level, applies attribute
// agreement, and sorts by the adjusted score.
func rankCandidates(scores map[string]float64, attrs map[string]animalAttributes, q queryAttributes) []rankedAnimal {
	ranked := make([]rankedAnimal, 0, len(scores))
	for gid, score := range scores {
		agreement, compared := attributeAgreement(q, attrs[gid])
		ranked = append(ranked, rankedAnimal{
			GodhaarID: gid,
			Score:     score,
			Adjusted:  score + attributeWeight*agreement,
			Agreement: agreement,
			Compared:  compared,
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Adjusted != ranked[j].Adjusted {
			return ranked[i].Adjusted > ranked[j].Adjusted
		}
		// Ties broken by raw score, then id, so the order is deterministic
		// rather than dependent on map iteration.
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].GodhaarID < ranked[j].GodhaarID
	})
	return ranked
}

// decide maps a ranked list (sorted by adjusted score descending) to a final
// identification decision. Ported from app/utils/decision.py, extended with the
// attribute agreement term.
func decide(ranked []rankedAnimal) verdict {
	if len(ranked) == 0 {
		return verdict{Decision: "UNKNOWN", Reason: "no_candidates"}
	}

	top := ranked[0]
	// Gap between rank-1 and rank-2; 0.0 when there is only one candidate.
	secondScore := top.Adjusted
	if len(ranked) > 1 {
		secondScore = ranked[1].Adjusted
	}
	gap := top.Adjusted - secondScore

	id := top.GodhaarID
	v := verdict{
		GodhaarID:     &id,
		Score:         round6(top.Score),
		AdjustedScore: round6(top.Adjusted),
		Gap:           round6(gap),
		Agreement:     round6(top.Agreement),
	}

	// The attribute term is allowed to lower confidence but never to raise it.
	//
	// The asymmetry is deliberate. Demotion costs a human a look at a REVIEW;
	// promotion asserts an identification that rests entirely on the colour and
	// horn classifiers, in the narrow band where the embedding score alone was
	// not enough — and those are precisely the classifiers the original code
	// warned were unreliable. Taking the more cautious of the two readings
	// means agreement still earns a candidate its place at the top of the
	// ranking, without that agreement ever being the sole reason a farmer is
	// told this is a known animal.
	adjustedDecision, adjustedReason := classify(top.Adjusted, gap)
	rawDecision, rawReason, rawTop := decideOnRawScores(ranked)

	if confidence(rawDecision) < confidence(adjustedDecision) {
		v.Decision, v.Reason = rawDecision, rawReason
	} else {
		v.Decision, v.Reason = adjustedDecision, adjustedReason
	}

	// A decision the attribute term changed is worth being able to spot in the
	// logs and on the dashboard, since a miscalibrated colour or horn
	// classifier would show up here first — as a run of shifted decisions that
	// verification then marks as wrong. The comparison is against the raw
	// scores, i.e. what would have happened with no attribute term at all.
	if v.Decision != rawDecision || top.GodhaarID != rawTop {
		v.Reason += "_attribute_shifted"
	}
	return v
}

// confidence ranks decisions from least to most committal, so the engine can
// take the more cautious of two readings.
func confidence(decision string) int {
	switch decision {
	case "MATCH":
		return 2
	case "REVIEW":
		return 1
	default:
		return 0
	}
}

func classify(score, gap float64) (decision, reason string) {
	switch {
	case score >= matchThreshold && gap >= gapThreshold:
		return "MATCH", "high_score_clear_gap"
	case score >= matchThreshold:
		return "REVIEW", "high_score_ambiguous_gap"
	case score >= reviewThreshold:
		return "REVIEW", "mid_range_score"
	default:
		return "UNKNOWN", "below_review_threshold"
	}
}

// decideOnRawScores re-runs the decision over the unadjusted embedding scores:
// the answer the engine would have given with no attribute term at all. It has
// to re-sort rather than reuse the caller's ordering, because the attribute
// term can change which candidate comes top — which is precisely one of the
// shifts worth detecting.
func decideOnRawScores(ranked []rankedAnimal) (decision, reason, godhaarID string) {
	if len(ranked) == 0 {
		return "UNKNOWN", "no_candidates", ""
	}

	byRaw := make([]rankedAnimal, len(ranked))
	copy(byRaw, ranked)
	sort.Slice(byRaw, func(i, j int) bool {
		if byRaw[i].Score != byRaw[j].Score {
			return byRaw[i].Score > byRaw[j].Score
		}
		return byRaw[i].GodhaarID < byRaw[j].GodhaarID
	})

	gap := 0.0
	if len(byRaw) > 1 {
		gap = byRaw[0].Score - byRaw[1].Score
	}
	decision, reason = classify(byRaw[0].Score, gap)
	return decision, reason, byRaw[0].GodhaarID
}

func round6(x float64) float64 {
	return math.Round(x*1e6) / 1e6
}
