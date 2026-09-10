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

// ── LightGlue top-K re-ranking (promotes LightGlue from a demote-only veto
//    on the single top-1 candidate to a signal across all embedding
//    candidates the inference server returned) ─────────────────────────────
//
// Gated on scripts/calibrate_decision_thresholds.py's sibling gate,
// inference_server's scratch_rerank_eval.py — see that script for the
// measured numbers this shipped on (baseline top-1 vs. re-ranked top-1 vs.
// the oracle top-K ceiling, at K in 3/5/10/20). rrfC and the RRF shape must
// match pipeline/rerank.py's RRF_C verbatim; the two are not shared code
// (Go and Python), only shared math.
const rrfC = 60

// lightglueEvidence is one candidate's LightGlue re-rank evidence. MatchRatio
// is nil when that candidate had no cached crop to compare against (see
// MUZZLE_CROP_CACHE_DIR's coverage gap) — rerankByLightglue's graceful-
// degradation contract (below) treats that exactly like inference_server's
// pipeline/rerank.py: a missing rank contributes nothing to the RRF sum
// rather than the worst possible rank.
type lightglueEvidence struct {
	NumMatches *int
	MatchRatio *float64
}

// rerankByLightglue combines each candidate's RAW-embedding-score rank
// (byRawScore must already be sorted by Score descending — the same
// ordering decideOnRawScores produces, deliberately NOT the attribute-
// adjusted order: LightGlue evidence is combined with the embedding signal
// only, so the two independent, bounded nudges (attribute agreement here,
// LightGlue there) never compound on the same ranking pass) with its
// LightGlue match_ratio rank via reciprocal rank fusion, and returns a new
// slice in the combined order. A candidate with no LightGlue evidence
// keeps its embedding-derived position relative to other evidence-less
// candidates (see lightglueEvidence's doc comment) rather than being
// pushed to the bottom.
func rerankByLightglue(byRawScore []rankedAnimal, evidence map[string]lightglueEvidence) []rankedAnimal {
	if len(byRawScore) < 2 {
		return byRawScore
	}

	type withRatio struct {
		idx   int
		ratio float64
		count int
	}
	var withEvidence []withRatio
	for i, r := range byRawScore {
		ev, ok := evidence[r.GodhaarID]
		if !ok || ev.MatchRatio == nil {
			continue
		}
		count := 0
		if ev.NumMatches != nil {
			count = *ev.NumMatches
		}
		withEvidence = append(withEvidence, withRatio{idx: i, ratio: *ev.MatchRatio, count: count})
	}
	sort.Slice(withEvidence, func(i, j int) bool {
		if withEvidence[i].ratio != withEvidence[j].ratio {
			return withEvidence[i].ratio > withEvidence[j].ratio
		}
		return withEvidence[i].count > withEvidence[j].count
	})
	lightglueRank := make(map[int]int, len(withEvidence)) // byRawScore index -> 1-indexed lightglue rank
	for rank, w := range withEvidence {
		lightglueRank[w.idx] = rank + 1
	}

	type combo struct {
		idx   int
		score float64
	}
	combos := make([]combo, len(byRawScore))
	for i := range byRawScore {
		s := 1.0 / float64(rrfC+i+1)
		if lgRank, ok := lightglueRank[i]; ok {
			s += 1.0 / float64(rrfC+lgRank)
		}
		combos[i] = combo{idx: i, score: s}
	}
	sort.SliceStable(combos, func(i, j int) bool { return combos[i].score > combos[j].score })

	out := make([]rankedAnimal, len(byRawScore))
	for i, c := range combos {
		out[i] = byRawScore[c.idx]
	}
	return out
}

// lightglueRankIndex 1-indexes byRawScore's entries by LightGlue match_ratio
// descending (ties by NumMatches descending) — the same ordering
// rerankByLightglue computes internally, exposed here so callers that only
// need the ranks (e.g. debug.go's persistence) don't have to re-derive the
// combined slice just to read them back off.
func lightglueRankIndex(byRawScore []rankedAnimal, evidence map[string]lightglueEvidence) map[string]int {
	type withRatio struct {
		gid   string
		ratio float64
		count int
	}
	var withEvidence []withRatio
	for _, r := range byRawScore {
		ev, ok := evidence[r.GodhaarID]
		if !ok || ev.MatchRatio == nil {
			continue
		}
		count := 0
		if ev.NumMatches != nil {
			count = *ev.NumMatches
		}
		withEvidence = append(withEvidence, withRatio{gid: r.GodhaarID, ratio: *ev.MatchRatio, count: count})
	}
	sort.Slice(withEvidence, func(i, j int) bool {
		if withEvidence[i].ratio != withEvidence[j].ratio {
			return withEvidence[i].ratio > withEvidence[j].ratio
		}
		return withEvidence[i].count > withEvidence[j].count
	})
	out := make(map[string]int, len(withEvidence))
	for rank, w := range withEvidence {
		out[w.gid] = rank + 1
	}
	return out
}

// combinedRankIndex 1-indexes byRawScore's entries by the SAME reciprocal-
// rank-fusion score rerankByLightglue sorts on, for the same
// persistence-only reason lightglueRankIndex exists.
func combinedRankIndex(byRawScore []rankedAnimal, evidence map[string]lightglueEvidence) map[string]int {
	reranked := rerankByLightglue(byRawScore, evidence)
	out := make(map[string]int, len(reranked))
	for i, r := range reranked {
		out[r.GodhaarID] = i + 1
	}
	return out
}

// buildLightglueCandidateEvidence assembles the full per-candidate picture
// (debug.go's LightglueCandidateEvidence) for persistence — every top-K
// candidate's embedding rank, its own LightGlue evidence (nil fields when it
// had no cached crop), and its rank under both orderings. Never touches the
// actual decision, purely a diagnostic record so a past search's re-rank
// behaviour can be reconstructed later without re-running inference.
func buildLightglueCandidateEvidence(
	byRawScore []rankedAnimal,
	evidence map[string]lightglueEvidence,
	faissIDByGodhaarID map[string]int64,
) []lightglueCandidateEvidenceView {
	lgRank := lightglueRankIndex(byRawScore, evidence)
	combinedRank := combinedRankIndex(byRawScore, evidence)

	out := make([]lightglueCandidateEvidenceView, 0, len(byRawScore))
	for i, r := range byRawScore {
		ev := evidence[r.GodhaarID]
		var lightglueRank *int
		if lr, ok := lgRank[r.GodhaarID]; ok {
			v := lr
			lightglueRank = &v
		}
		out = append(out, lightglueCandidateEvidenceView{
			GodhaarID:           r.GodhaarID,
			FaissID:             faissIDByGodhaarID[r.GodhaarID],
			EmbeddingScore:      r.Score,
			EmbeddingRank:       i + 1,
			LightglueNumMatches: ev.NumMatches,
			LightglueMatchRatio: ev.MatchRatio,
			LightglueRank:       lightglueRank,
			CombinedRank:        combinedRank[r.GodhaarID],
		})
	}
	return out
}

// lightglueCandidateEvidenceView mirrors debugdb.LightglueCandidateEvidence
// field-for-field. Kept as a distinct type here (rather than importing the
// debugdb package into this file) so decision.go stays free of a persistence
// dependency — routes.go, which already imports debugdb, converts between
// the two with a one-line loop.
type lightglueCandidateEvidenceView struct {
	GodhaarID           string
	FaissID             int64
	EmbeddingScore      float64
	EmbeddingRank       int
	LightglueNumMatches *int
	LightglueMatchRatio *float64
	LightglueRank       *int
	CombinedRank        int
}

// applyLightglueRerank may replace decide()'s chosen candidate with a
// different one from the top-K embedding matches when LightGlue's
// independent keypoint evidence favors it more than the raw embedding
// ranking alone did.
//
// SAFETY INVARIANT (the two options considered were "require the promoted
// candidate to independently clear matchThreshold on its own raw score" and
// "only re-rank within the REVIEW band" — this applies BOTH, deliberately
// stacking them rather than picking one):
//
//  1. An already-MATCH verdict is NEVER revisited. Re-ranking only ever runs
//     when v.Decision is REVIEW or UNKNOWN — exactly the two bands where the
//     raw-score-only pipeline was not confident enough to name an animal
//     hands-free. This alone guarantees the auto-MATCH false-accept rate
//     cannot rise: nothing here can change which candidate an already-
//     confident MATCH names, or turn a MATCH into something else either.
//  2. A candidate promoted by re-ranking can only reach a decision as
//     confident as classify() grants its OWN raw embedding score and its OWN
//     gap over whatever re-ranking put in second place — the exact same
//     matchThreshold/gapThreshold bar every other MATCH in this file clears.
//     LightGlue evidence changes WHICH candidate is being measured against
//     that bar; it never lowers the bar itself, and it never contributes a
//     score of its own to Adjusted/Score.
//
// TestLightglueRerankSafety pins both properties by sweeping random inputs,
// the same style TestAttributesCannotConfidentlyReorder and
// TestLightglueCanOnlyDemote already use for their own safety proofs.
func applyLightglueRerank(v verdict, byRawScore []rankedAnimal, evidence map[string]lightglueEvidence) verdict {
	if v.Decision == "MATCH" {
		return v
	}
	if len(byRawScore) < 2 || len(evidence) == 0 {
		return v
	}

	reranked := rerankByLightglue(byRawScore, evidence)
	newTop := reranked[0]
	if v.GodhaarID != nil && newTop.GodhaarID == *v.GodhaarID {
		return v // rerank agreed with the existing pick — nothing changes
	}

	newSecond := newTop.Score
	if len(reranked) > 1 {
		newSecond = reranked[1].Score
	}
	newGap := newTop.Score - newSecond
	newDecision, newReason := classify(newTop.Score, newGap)

	if confidence(newDecision) <= confidence(v.Decision) {
		// The rerank's pick isn't even as confident as what decide() already
		// had — keep the original rather than swap to a same-or-worse verdict
		// for a different animal.
		return v
	}

	id := newTop.GodhaarID
	return verdict{
		GodhaarID: &id,
		// No attribute term is applied to a LightGlue-promoted candidate —
		// AdjustedScore intentionally mirrors Score rather than inventing an
		// agreement value this function never computed. Not applying one is
		// strictly more conservative than applying one that could only ever
		// lower confidence anyway.
		Score:         round6(newTop.Score),
		AdjustedScore: round6(newTop.Score),
		Gap:           round6(newGap),
		Agreement:     0,
		Decision:      newDecision,
		Reason:        newReason + "_lightglue_reranked",
	}
}
