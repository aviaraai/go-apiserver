package animal

import (
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

func strptr(s string) *string { return &s }

func TestAttributeAgreement(t *testing.T) {
	horned := strptr("curved")
	straight := strptr("straight")

	tests := []struct {
		name          string
		query         queryAttributes
		candidate     animalAttributes
		wantAgreement float64
		wantCompared  int
	}{
		{
			name:          "everything matches",
			query:         queryAttributes{BodyColor: "brown", MuzzleColor: "black", HornShape: horned},
			candidate:     animalAttributes{BodyColor: "brown", MuzzleColor: "black", HornShape: horned},
			wantAgreement: 1,
			wantCompared:  3,
		},
		{
			name:          "everything disagrees",
			query:         queryAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: straight},
			candidate:     animalAttributes{BodyColor: "brown", MuzzleColor: "black", HornShape: horned},
			wantAgreement: -1,
			wantCompared:  3,
		},
		{
			name:          "labels compare case and whitespace insensitively",
			query:         queryAttributes{BodyColor: "  Brown ", MuzzleColor: "BLACK"},
			candidate:     animalAttributes{BodyColor: "brown", MuzzleColor: "black"},
			wantAgreement: 1,
			wantCompared:  2,
		},
		{
			// A polled animal, or one registered before the horn classifier
			// existed, must not be penalised for the missing attribute.
			name:          "unknown horn shape is not counted either way",
			query:         queryAttributes{BodyColor: "brown", MuzzleColor: "black", HornShape: nil},
			candidate:     animalAttributes{BodyColor: "brown", MuzzleColor: "black", HornShape: horned},
			wantAgreement: 1,
			wantCompared:  2,
		},
		{
			name:          "no comparable attributes is no evidence",
			query:         queryAttributes{},
			candidate:     animalAttributes{BodyColor: "brown", MuzzleColor: "black"},
			wantAgreement: 0,
			wantCompared:  0,
		},
		{
			name:          "one of three disagrees",
			query:         queryAttributes{BodyColor: "brown", MuzzleColor: "pink", HornShape: horned},
			candidate:     animalAttributes{BodyColor: "brown", MuzzleColor: "black", HornShape: horned},
			wantAgreement: 1.0 / 3.0,
			wantCompared:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agreement, compared := attributeAgreement(tt.query, tt.candidate)
			if compared != tt.wantCompared {
				t.Errorf("compared = %d, want %d", compared, tt.wantCompared)
			}
			if round6(agreement) != round6(tt.wantAgreement) {
				t.Errorf("agreement = %v, want %v", agreement, tt.wantAgreement)
			}
		})
	}
}

// The attribute term is a tie-breaker, not a filter: it must be able to move a
// borderline case but never to overturn a decisive embedding score.
func TestAttributesCannotOverturnDecisiveScores(t *testing.T) {
	query := queryAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}
	disagrees := animalAttributes{BodyColor: "brown", MuzzleColor: "black", HornShape: strptr("curved")}

	// A confident match with every attribute disagreeing stays a match.
	ranked := rankCandidates(
		map[string]float64{"A": 0.97, "B": 0.40},
		map[string]animalAttributes{"A": disagrees, "B": disagrees},
		query,
	)
	if v := decide(ranked); v.Decision != "MATCH" {
		t.Errorf("decisive score with full disagreement = %s, want MATCH (reason %s)", v.Decision, v.Reason)
	}

	// A clearly-too-low score (well under reviewThreshold) with every
	// attribute agreeing stays unknown.
	agrees := animalAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}
	ranked = rankCandidates(
		map[string]float64{"A": reviewThreshold - 0.05},
		map[string]animalAttributes{"A": agrees},
		query,
	)
	if v := decide(ranked); v.Decision != "UNKNOWN" {
		t.Errorf("low score with full agreement = %s, want UNKNOWN", v.Decision)
	}
}

// A score sitting just above the match threshold is exactly where the
// attributes should be allowed to speak.
func TestAttributesDemoteBorderlineDecisions(t *testing.T) {
	query := queryAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}
	disagrees := animalAttributes{BodyColor: "brown", MuzzleColor: "black", HornShape: strptr("curved")}

	// A raw score just above matchThreshold is a MATCH on raw score alone;
	// full disagreement pulls it down by attributeWeight, back under
	// matchThreshold, so it becomes a REVIEW instead.
	borderline := matchThreshold + 0.003
	ranked := rankCandidates(
		map[string]float64{"A": borderline, "B": reviewThreshold - 0.05},
		map[string]animalAttributes{"A": disagrees, "B": disagrees},
		query,
	)
	v := decide(ranked)
	if v.Decision != "REVIEW" {
		t.Fatalf("borderline score with full disagreement = %s, want REVIEW", v.Decision)
	}
	if !strings.HasSuffix(v.Reason, "_attribute_shifted") {
		t.Errorf("reason = %q, want it flagged as attribute-shifted", v.Reason)
	}
	if v.Score != round6(borderline) {
		t.Errorf("Score = %v, want the model's own %v reported unmodified", v.Score, borderline)
	}
}

// Attributes may lower confidence but never raise it: a decision must never
// rest on the colour and horn classifiers alone.
func TestAttributesNeverPromote(t *testing.T) {
	agrees := animalAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}
	query := queryAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}

	// A raw score just under matchThreshold is a REVIEW. Full agreement lifts
	// it back over matchThreshold, with a clear gap — but promotion is not
	// allowed, so the decision must stay whatever the raw score alone gives.
	nearThreshold := matchThreshold - 0.003
	ranked := rankCandidates(
		map[string]float64{"A": nearThreshold, "B": 0.10},
		map[string]animalAttributes{"A": agrees, "B": agrees},
		query,
	)
	v := decide(ranked)
	if v.Decision != "REVIEW" {
		t.Errorf("decision = %s, want REVIEW: attributes must not promote", v.Decision)
	}

	// The same holds at the lower threshold.
	ranked = rankCandidates(
		map[string]float64{"A": reviewThreshold - 0.05},
		map[string]animalAttributes{"A": agrees},
		query,
	)
	if v := decide(ranked); v.Decision != "UNKNOWN" {
		t.Errorf("decision = %s, want UNKNOWN: attributes must not promote", v.Decision)
	}
}

// Attribute agreement can reorder candidates whose embedding scores are close.
func TestAttributesCanReorderCloseCandidates(t *testing.T) {
	query := queryAttributes{BodyColor: "white", MuzzleColor: "pink"}

	ranked := rankCandidates(
		map[string]float64{
			"top-by-score":     0.900,
			"top-by-attribute": 0.899,
		},
		map[string]animalAttributes{
			"top-by-score":     {BodyColor: "brown", MuzzleColor: "black"},
			"top-by-attribute": {BodyColor: "white", MuzzleColor: "pink"},
		},
		query,
	)

	if ranked[0].GodhaarID != "top-by-attribute" {
		t.Errorf("ranked[0] = %s, want the attribute-agreeing candidate on top", ranked[0].GodhaarID)
	}
	if v := decide(ranked); !strings.HasSuffix(v.Reason, "_attribute_shifted") {
		t.Errorf("reason = %q, want it flagged as attribute-shifted", v.Reason)
	}
}

// The safety property that makes reordering acceptable: because the largest gap
// the attribute term can manufacture (2*attributeWeight = 0.01) is smaller than
// gapThreshold (0.02), attributes can never hand a confident MATCH to an animal
// the embeddings did not already rank first.
//
// Swept over the full space of two-candidate cases rather than a few examples,
// since the property is what licenses the whole design.
func TestAttributesCannotConfidentlyReorder(t *testing.T) {
	attrs := []animalAttributes{
		{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}, // agrees
		{BodyColor: "brown", MuzzleColor: "black", HornShape: strptr("curved")},  // disagrees
		{BodyColor: "white", MuzzleColor: "black", HornShape: strptr("curved")},  // partial
		{}, // unknown
	}
	query := queryAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}

	var matches, reorders int
	for scoreA := 0.0; scoreA <= 1.0; scoreA += 0.005 {
		for scoreB := 0.0; scoreB <= 1.0; scoreB += 0.005 {
			for _, attrA := range attrs {
				for _, attrB := range attrs {
					ranked := rankCandidates(
						map[string]float64{"A": scoreA, "B": scoreB},
						map[string]animalAttributes{"A": attrA, "B": attrB},
						query,
					)
					v := decide(ranked)

					_, _, rawTop := decideOnRawScores(ranked)
					if ranked[0].GodhaarID != rawTop {
						reorders++
					}
					if v.Decision == "MATCH" {
						matches++
						if *v.GodhaarID != rawTop {
							t.Fatalf("attributes produced a MATCH for %s, but raw scores ranked %s first "+
								"(scoreA=%.3f scoreB=%.3f)", *v.GodhaarID, rawTop, scoreA, scoreB)
						}
					}
				}
			}
		}
	}

	// Guard against the sweep passing because it never reached the interesting
	// states: it has to have produced both MATCHes and reorderings for the
	// absence of a reordered MATCH to mean anything.
	if matches == 0 || reorders == 0 {
		t.Fatalf("sweep was vacuous: %d matches, %d reorderings", matches, reorders)
	}
	t.Logf("swept %d matches and %d attribute reorderings with no confident reorder", matches, reorders)
}

// A MATCH must always rest on a real separation in embedding scores, never on
// one the attribute term invented.
func TestMatchAlwaysHasRawSeparation(t *testing.T) {
	agrees := animalAttributes{BodyColor: "white", MuzzleColor: "pink"}
	disagrees := animalAttributes{BodyColor: "brown", MuzzleColor: "black"}
	query := queryAttributes{BodyColor: "white", MuzzleColor: "pink"}

	// Two identically-scored candidates, maximally split by attributes. The
	// adjusted gap reaches 0.01, still short of the 0.02 a MATCH needs.
	ranked := rankCandidates(
		map[string]float64{"A": 0.95, "B": 0.95},
		map[string]animalAttributes{"A": agrees, "B": disagrees},
		query,
	)
	if v := decide(ranked); v.Decision == "MATCH" {
		t.Errorf("tied scores produced a MATCH on attribute evidence alone (gap %.4f)", v.Gap)
	}

	if 2*attributeWeight >= gapThreshold {
		t.Fatalf("attributeWeight %.3f breaks the invariant 2*weight < gapThreshold %.3f",
			attributeWeight, gapThreshold)
	}
}

func TestDecideNoCandidates(t *testing.T) {
	v := decide(nil)
	if v.Decision != "UNKNOWN" || v.Reason != "no_candidates" {
		t.Errorf("decide(nil) = %s/%s, want UNKNOWN/no_candidates", v.Decision, v.Reason)
	}
	if v.GodhaarID != nil {
		t.Error("decide(nil) should not name an animal")
	}
}

// Reproduces the two real false positives found investigating the 2026-08-16
// Uttarakhand precision test: two unrelated animals scored 0.748 and 0.788
// against a local index (both land in the REVIEW band on raw score alone),
// and inference_server's LightGlue tiebreaker read "likely_different" for
// both. Before this change that reached the farmer as a plain REVIEW; this
// confirms it now correctly demotes to UNKNOWN ("not registered").
func TestLightglueDemotesRealFalsePositives(t *testing.T) {
	zone := lightglueDisagreementZone

	for _, score := range []float64{0.748, 0.788} {
		ranked := rankCandidates(
			map[string]float64{"A": score},
			map[string]animalAttributes{"A": {}},
			queryAttributes{},
		)
		v := decide(ranked)
		if v.Decision != "REVIEW" {
			t.Fatalf("score %.3f: raw decision = %s, want REVIEW (test setup is wrong, not the fix)", score, v.Decision)
		}

		demoted := applyLightglueDisagreement(v, &zone)
		if demoted.Decision != "UNKNOWN" {
			t.Errorf("score %.3f: decision after likely_different = %s, want UNKNOWN", score, demoted.Decision)
		}
		if !strings.HasSuffix(demoted.Reason, "_lightglue_demoted") {
			t.Errorf("score %.3f: reason = %q, want it flagged as lightglue-demoted", score, demoted.Reason)
		}
		// The demotion must not silently invent a different animal or score.
		if demoted.GodhaarID == nil || *demoted.GodhaarID != "A" {
			t.Errorf("score %.3f: GodhaarID changed by demotion: %v", score, demoted.GodhaarID)
		}
		if demoted.Score != v.Score {
			t.Errorf("score %.3f: Score changed by demotion: %v -> %v", score, v.Score, demoted.Score)
		}
	}
}

// The safety property, LightGlue's analogue of TestMatchAlwaysHasRawSeparation:
// this function can only move a verdict to a strictly lower confidence rung,
// or leave it unchanged. Swept over every (Decision, zone) combination rather
// than a few examples, matching TestAttributesCannotConfidentlyReorder's style.
func TestLightglueCanOnlyDemote(t *testing.T) {
	same := "likely_same"
	ambiguous := "ambiguous"
	different := "likely_different"
	unknownZone := "some_future_zone_value"

	decisions := []string{"MATCH", "REVIEW", "UNKNOWN"}
	zones := []*string{nil, &same, &ambiguous, &different, &unknownZone}

	for _, decision := range decisions {
		for _, zone := range zones {
			id := "A"
			v := verdict{Decision: decision, GodhaarID: &id, Reason: "base_reason"}
			out := applyLightglueDisagreement(v, zone)

			if confidence(out.Decision) > confidence(v.Decision) {
				t.Fatalf("decision=%s zone=%v: confidence rose (%s -> %s)",
					decision, derefLabel(zone), decision, out.Decision)
			}

			zoneVal := derefLabel(zone)
			if zoneVal != lightglueDisagreementZone {
				// Anything other than exactly "likely_different" — including
				// nil, "likely_same", "ambiguous", and an unrecognised future
				// value — must be a complete no-op.
				if out != v {
					t.Errorf("decision=%s zone=%v: expected no-op, got %+v (was %+v)",
						decision, zoneVal, out, v)
				}
			}
		}
	}
}

// nil is the ordinary case (LightGlue never ran) and must behave exactly as
// it does today: decide()'s own verdict, untouched, field for field.
func TestLightglueNilIsNoOp(t *testing.T) {
	ranked := rankCandidates(
		map[string]float64{"A": 0.90, "B": 0.30},
		map[string]animalAttributes{"A": {BodyColor: "brown"}, "B": {}},
		queryAttributes{BodyColor: "brown"},
	)
	v := decide(ranked)
	out := applyLightglueDisagreement(v, nil)
	if out != v {
		t.Errorf("nil zone changed the verdict: %+v -> %+v", v, out)
	}
}

// independentRawGap hand-derives the true nearest-rival gap for godhaarID
// from byRawScore, WITHOUT reusing any of applyLightglueRerank's or
// rerankByLightglue's own gap-computation logic — written from scratch here
// so this check cannot share the bug it exists to catch. Mirrors decide()'s
// own single-direction convention: a candidate's gap is its own raw score
// minus the next-lower entry once everything is sorted by raw score
// descending; no rival below (the singleton case) means gap 0, exactly
// like decide()'s own secondScore-defaults-to-top.Adjusted convention.
func independentRawGap(byRawScore []rankedAnimal, godhaarID string) float64 {
	sorted := make([]rankedAnimal, len(byRawScore))
	copy(sorted, byRawScore)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })

	idx := -1
	var ownScore float64
	for i, r := range sorted {
		if r.GodhaarID == godhaarID {
			idx = i
			ownScore = r.Score
			break
		}
	}
	if idx == -1 || idx == len(sorted)-1 {
		return 0.0
	}
	return ownScore - sorted[idx+1].Score
}

// Executed, real-numbers proof of a confirmed false-MATCH vulnerability:
// applyLightglueRerank used to compute Gap against reranked[1] — whoever
// LightGlue's FUSED ranking placed second — instead of against the
// promoted candidate's true nearest rival on raw embedding score. A
// candidate with no cached LightGlue crop (the common case:
// MUZZLE_CROP_CACHE_DIR only covers animals registered after that cache
// shipped) could be skipped over in the fused ranking by a candidate that
// merely had SOME LightGlue evidence, however weak — even if that
// evidence-bearing candidate was nowhere near the promoted candidate on
// raw score, and the promoted candidate's REAL closest rival (a case with
// no cached crop either) sat right next to it in raw score, un-consulted.
//
// This scenario reproduces exactly that: C (raw score 0.25, strong
// LightGlue evidence, no cached crop) has D (0.24 — a textbook near-twin,
// true gap 0.01) as its real nearest rival, but D has no cached LightGlue
// evidence. E (0.05, weak raw score, second-best LightGlue evidence) is
// what the buggy code used as "second place" instead, reporting a gap of
// 0.20 — comfortably clearing gapThreshold=0.08 — for a candidate whose
// true separation from its actual closest competitor is 0.01, exactly the
// case this whole decision engine exists to keep out of auto-MATCH.
func TestLightglueRerankGapUsesRawRival(t *testing.T) {
	scores := map[string]float64{
		"A": 0.90, // best raw score, no cached LightGlue crop
		"B": 0.85, // no evidence
		"C": 0.25, // weak raw score, but strong LightGlue evidence
		"D": 0.24, // C's TRUE nearest rival on raw score (gap 0.01) — no cached crop
		"E": 0.05, // very weak raw score, second-best LightGlue evidence
	}
	ratioC, numC := 0.90, 800
	ratioE, numE := 0.40, 300

	ranked := rankCandidates(scores, map[string]animalAttributes{}, queryAttributes{})
	v := decide(ranked)
	if v.Decision != "REVIEW" || v.GodhaarID == nil || *v.GodhaarID != "A" {
		t.Fatalf("test setup is wrong, not the fix: raw decision = %s/%v, want REVIEW/A", v.Decision, v.GodhaarID)
	}

	byRawScore := make([]rankedAnimal, len(ranked))
	copy(byRawScore, ranked)
	sort.Slice(byRawScore, func(i, j int) bool { return byRawScore[i].Score > byRawScore[j].Score })

	evidence := map[string]lightglueEvidence{
		"C": {MatchRatio: &ratioC, NumMatches: &numC},
		"E": {MatchRatio: &ratioE, NumMatches: &numE},
		// A, B, D deliberately carry no evidence — no cached crop.
	}

	// Confirm the vulnerability's mechanism is real and still fires:
	// rerankByLightglue (untouched by this fix, and correctly so — the RRF
	// combination itself was never the bug) does put C on top, exactly as
	// the exploit needs. If this ever stops being true the rest of the
	// test proves nothing, so it must hold before anything else is checked.
	reranked := rerankByLightglue(byRawScore, evidence)
	if reranked[0].GodhaarID != "C" {
		t.Fatalf("test setup is wrong, not the fix: rerankByLightglue's top = %s, want C", reranked[0].GodhaarID)
	}

	// The bug, confirmed present before this fix (git-stash-verified against
	// the pre-fix code): reranked[1] is E (score 0.05, second-best LightGlue
	// evidence) purely because D has no cached crop, giving a WRONG gap of
	// 0.25-0.05=0.20 — comfortably over gapThreshold — and a false MATCH.
	buggyGap := round6(reranked[0].Score - reranked[1].Score)
	if math.Abs(buggyGap-0.20) > 1e-9 {
		t.Fatalf("test setup is wrong, not the fix: reranked[1] gap = %v, want 0.20 (E, not D)", buggyGap)
	}

	// C's TRUE nearest rival, independently derived from raw scores alone
	// (no shared code with applyLightglueRerank/rerankByLightglue): D, gap
	// 0.01 — a textbook near-twin, nowhere close to gapThreshold=0.08.
	trueGapForC := round6(independentRawGap(byRawScore, "C"))
	if math.Abs(trueGapForC-0.01) > 1e-9 {
		t.Fatalf("test setup is wrong, not the fix: independently-derived true gap for C = %v, want 0.01", trueGapForC)
	}

	out := applyLightglueRerank(v, byRawScore, evidence)

	// The fix: whatever applyLightglueRerank actually returns, its Gap must
	// match the RETURNED candidate's true raw-score-rival gap — computed
	// completely independently here, not reused from decision.go's own
	// logic. (With the true gap for C capped at 0.01, C's own best possible
	// decision is REVIEW, confidence()==1 — no MORE confident than the
	// v.Decision=REVIEW this scenario already had, so applyLightglueRerank's
	// pre-existing "don't swap to a same-or-worse verdict" rule — untouched
	// by this fix — correctly leaves the verdict on A. That is itself part
	// of what closes this vulnerability: with the gap corrected, C can
	// never look more confident than the candidate already standing, so no
	// swap to the wrong animal happens at all, not even a safe-looking one.)
	if out.GodhaarID == nil {
		t.Fatalf("out.GodhaarID is nil")
	}
	wantGap := round6(independentRawGap(byRawScore, *out.GodhaarID))
	if out.Gap != wantGap {
		t.Errorf("Gap = %v for candidate %s, want the independently-derived true nearest-rival gap %v",
			out.Gap, *out.GodhaarID, wantGap)
	}

	if out.Decision == "MATCH" {
		t.Errorf("Decision = MATCH — C's real nearest rival (D) is only 0.01 away, exactly the "+
			"near-twin case this engine must not auto-confirm; got GodhaarID=%s score=%v gap=%v",
			*out.GodhaarID, out.Score, out.Gap)
	}
	if out.Decision != "REVIEW" && out.Decision != "UNKNOWN" {
		t.Errorf("Decision = %s, want REVIEW or UNKNOWN", out.Decision)
	}
}

// The safety property applyLightglueRerank's doc comment states: (1) an
// already-MATCH verdict is never touched, and (2) a candidate can only reach
// a MORE confident decision than the original if its OWN raw score/gap
// independently clears classify()'s bar. Swept over randomised inputs rather
// than a handful of examples, matching TestAttributesCannotConfidentlyReorder
// and TestLightglueCanOnlyDemote's own style for a safety proof.
func TestLightglueRerankSafety(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	names := []string{"A", "B", "C", "D"}

	var matchNeverTouched, promotions, noOpAgreements, sameOrLessConfident int

	for trial := 0; trial < 5000; trial++ {
		n := 2 + rng.Intn(3) // 2..4 candidates
		scores := make(map[string]float64, n)
		for i := 0; i < n; i++ {
			scores[names[i]] = rng.Float64()
		}
		ranked := rankCandidates(scores, map[string]animalAttributes{}, queryAttributes{})
		v := decide(ranked)

		byRawScore := make([]rankedAnimal, len(ranked))
		copy(byRawScore, ranked)
		sort.Slice(byRawScore, func(i, j int) bool { return byRawScore[i].Score > byRawScore[j].Score })

		evidence := make(map[string]lightglueEvidence)
		for i := 0; i < n; i++ {
			if rng.Float64() < 0.7 { // some candidates have no cached crop
				ratio := rng.Float64()
				nm := rng.Intn(900)
				evidence[names[i]] = lightglueEvidence{MatchRatio: &ratio, NumMatches: &nm}
			}
		}

		before := v
		out := applyLightglueRerank(v, byRawScore, evidence)

		if before.Decision == "MATCH" {
			matchNeverTouched++
			if out != before {
				t.Fatalf("trial %d: an already-MATCH verdict was changed: %+v -> %+v", trial, before, out)
			}
			continue
		}

		if out.GodhaarID != nil && before.GodhaarID != nil && *out.GodhaarID == *before.GodhaarID {
			noOpAgreements++
			continue
		}

		if confidence(out.Decision) > confidence(before.Decision) {
			promotions++
			// The promoted candidate's own Score/Gap must independently
			// satisfy classify() — recompute it exactly as
			// applyLightglueRerank should have, and confirm the reported
			// decision matches what classify() alone would grant it.
			wantDecision, _ := classify(out.Score, out.Gap)
			if wantDecision != out.Decision {
				t.Fatalf("trial %d: promoted decision %s does not match classify(%.4f, %.4f)=%s — "+
					"the promoted candidate did not independently clear its own bar",
					trial, out.Decision, out.Score, out.Gap, wantDecision)
			}
			// Closes the actual bug class (TestLightglueRerankGapUsesRawRival
			// pins the concrete exploit): out.Gap must match the promoted
			// candidate's TRUE nearest-rival gap on raw score, independently
			// hand-derived from byRawScore here — not merely self-consistent
			// with whatever Gap applyLightglueRerank happened to compute.
			wantGap := round6(independentRawGap(byRawScore, *out.GodhaarID))
			if out.Gap != wantGap {
				t.Fatalf("trial %d: promoted candidate %s Gap = %v, want independently-derived "+
					"true raw-score rival gap %v", trial, *out.GodhaarID, out.Gap, wantGap)
			}
		} else {
			sameOrLessConfident++
		}
	}

	if matchNeverTouched == 0 || promotions == 0 {
		t.Fatalf("sweep was vacuous: %d already-MATCH cases, %d promotions (want both > 0)",
			matchNeverTouched, promotions)
	}
	t.Logf("swept %d already-MATCH (untouched), %d promotions (independently verified), "+
		"%d no-op agreements, %d same-or-less-confident", matchNeverTouched, promotions,
		noOpAgreements, sameOrLessConfident)
}

// Ranking must not depend on map iteration order.
func TestRankingIsDeterministicOnTies(t *testing.T) {
	scores := map[string]float64{"b": 0.9, "a": 0.9, "c": 0.9}
	attrs := map[string]animalAttributes{}

	for range 20 {
		ranked := rankCandidates(scores, attrs, queryAttributes{})
		if ranked[0].GodhaarID != "a" || ranked[1].GodhaarID != "b" || ranked[2].GodhaarID != "c" {
			t.Fatalf("unstable tie ordering: %s, %s, %s",
				ranked[0].GodhaarID, ranked[1].GodhaarID, ranked[2].GodhaarID)
		}
	}
}
