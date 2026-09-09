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

// Decision thresholds.
//
// Re-calibrated 2026-09-10 for the fusion+PCA-whitening embedding upgrade
// (inference_server: DINOv2@518 + ImageNet ResNet50@384 + ResNet50@448,
// concatenated, PCA-whitened to 256-d — see inference_server/CLAUDE.md and
// pipeline/fusion_encoder.py). The 0.86/0.72/0.02 values below this comment's
// previous revision were calibrated against the OLD plain-DINOv2 embedding
// space and are meaningless against the new one: whitening decorrelates and
// flattens the score distribution, so genuine-match cosine scores that used
// to cluster around 0.85+ now median around 0.48. Applying the old
// matchThreshold=0.86 to the new distribution measured a true-accept rate of
// just 3.0% (6/203) — not a recalibration, a near-total loss of function.
//
// Re-derived via inference_server/scripts/calibrate_decision_thresholds.py
// (a permanent tool, not a scratch script — scratch_multiphoto_search_eval.py
// --calibrate's RealEmbedder is hardcoded to the old crop_cattle+DINOv2 path
// and, per that repo's rule, scratch_* scripts are frozen evidence and are
// never edited; the new script imports its reusable leave-one-out/
// calibration functions unchanged and feeds them fusion-encoder embeddings
// instead), against the real 224-animal Uttarakhand UKDE* dataset with
// full-photo (no YOLO crop) embedding, median-of-up-to-3-photo aggregation.
//
// 12 of those 224 animals (6 tag-pairs) were excluded before calibrating:
// their muzzle1/2/3.jpg files are byte-identical (md5-verified) to another
// animal's tag — a dataset duplicate-registration labeling defect, not an
// embedding failure — confirmed because the FIRST calibration pass showed
// several "false accept" pairs scoring EXACTLY 1.0000 median similarity
// (e.g. UKDEGR152028 <-> UKDEGR927838), which is only possible for
// byte-identical input images. Calibrating against them would have tuned
// the thresholds around a data-quality bug that cannot occur in production.
//
// On the remaining 203 animals: matchThreshold=0.22/gapThreshold=0.08 is the
// highest-true-accept point on the true-accept/false-accept Pareto frontier
// with ZERO false accepts (a confident WRONG match reaching a farmer, the
// failure mode this decision engine is built to avoid) — true_accept=79.8%
// (162/203), false_accept=0.0% (0/203), a large improvement over the old
// embedding space's last measured full-stack strict recall (23.6%, 38/161,
// see inference_server/CLAUDE.md). reviewThreshold=0.16 is the gap=0 score
// floor maximizing true_accept-false_accept (true_accept=89.2%,
// false_accept=4.4%) — false accepts at REVIEW are tolerable since a human
// confirms/rejects before anything is stored, unlike MATCH.
//
// Re-run inference_server/scripts/calibrate_decision_thresholds.py against a
// fresh dataset before moving these again — this is a different embedding
// space than the one the 0.86/0.72/0.02 values were tuned for, so nothing
// about that prior calibration's numbers transfers.
const (
	matchThreshold  = 0.22
	reviewThreshold = 0.16
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
//   - 2*attributeWeight < gapThreshold (0.01 < 0.08). The largest gap the
//     attribute term can manufacture between two identically-scored candidates
//     is 0.01, so attributes alone can never produce the clear gap a MATCH
//     requires. A MATCH always rests on a real separation in embedding scores.
//
//   - The same relation means that whenever attributes reorder the top two
//     candidates, their adjusted gap is necessarily below gapThreshold, so the
//     result can only be REVIEW or UNKNOWN. Attributes can never hand a
//     confident identification to a different animal than the embeddings
//     picked. TestAttributesCannotConfidentlyReorder proves this over random
//     inputs.
//
// Unchanged (0.005) by the 2026-09-10 fusion-encoder recalibration above:
// gapThreshold moved from 0.02 to 0.08, which only widens this invariant's
// margin (2*0.005=0.01 < 0.08, vs. the previous 0.01 < 0.02) — there was no
// need to move attributeWeight to keep it safe, and no measurement was taken
// to justify raising it just because there's now headroom. Re-derive
// deliberately, from real data, if that's ever wanted — don't raise it
// simply because the invariant would still hold.
const attributeWeight = 0.005

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

// lightglueDisagreementZone is the only inference_server zone value this
// engine acts on. "likely_same" is calibrated just as cleanly as
// "likely_different" (see client.go's SearchResponse doc) — it is left
// unconsumed by choice, not because the signal is weaker: this engine only
// ever lets outside evidence LOWER a verdict (attributeWeight already
// follows the same rule for colour/horn agreement), and "likely_same" would
// only ever matter as a promotion. "ambiguous" is a real "no opinion" and is
// correctly excluded either way. Both are treated as no-ops here, same as
// LightglueChecked == false.
const lightglueDisagreementZone = "likely_different"

// applyLightglueDisagreement demotes a verdict by exactly one step when
// LightGlue's independent keypoint check disagrees with the embedding-based
// decision. Applied AFTER decide() has already produced its verdict from the
// score and colour/horn attribute logic — this is a second, independent
// signal layered on top, not folded into rankCandidates/decide, so every
// existing call site and test is untouched by its addition.
//
// Safety property, the LightGlue analogue of attributeWeight's gap bound:
// this function can only ever move a verdict to a LOWER confidence() rung
// (MATCH=2 -> REVIEW=1 -> UNKNOWN=0), never higher, and a nil/non-disagreeing
// zone is always a no-op. TestLightglueCanOnlyDemote sweeps every
// (Decision, zone) pair and proves confidence never increases.
//
// GodhaarID is deliberately left as-is even on a demotion to UNKNOWN:
// responseGodhaarID() already drops it from the outward response once
// Decision == UNKNOWN, and clearing it here too would just be a second place
// for that same rule to (mis)apply.
func applyLightglueDisagreement(v verdict, lightglueZone *string) verdict {
	if lightglueZone == nil || *lightglueZone != lightglueDisagreementZone {
		return v
	}

	switch v.Decision {
	case "MATCH":
		v.Decision = "REVIEW"
	case "REVIEW":
		v.Decision = "UNKNOWN"
	default: // already UNKNOWN — nothing lower to demote to
		return v
	}
	v.Reason += "_lightglue_demoted"
	return v
}
