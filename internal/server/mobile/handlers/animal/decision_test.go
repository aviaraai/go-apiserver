package animal

import (
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

	// A clearly-too-low score with every attribute agreeing stays unknown.
	agrees := animalAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}
	ranked = rankCandidates(
		map[string]float64{"A": 0.50},
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

	// 0.83 is a MATCH on raw score; full disagreement pulls it to 0.80, under
	// the 0.82 threshold, so it becomes a REVIEW instead.
	ranked := rankCandidates(
		map[string]float64{"A": 0.83, "B": 0.30},
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
	if v.Score != 0.83 {
		t.Errorf("Score = %v, want the model's own 0.83 reported unmodified", v.Score)
	}
}

// Attributes may lower confidence but never raise it: a decision must never
// rest on the colour and horn classifiers alone.
func TestAttributesNeverPromote(t *testing.T) {
	agrees := animalAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}
	query := queryAttributes{BodyColor: "white", MuzzleColor: "pink", HornShape: strptr("straight")}

	// 0.80 is a REVIEW on raw score. Full agreement lifts it to 0.83, over the
	// match threshold, and the gap is wide — but promotion is not allowed.
	ranked := rankCandidates(
		map[string]float64{"A": 0.80, "B": 0.10},
		map[string]animalAttributes{"A": agrees, "B": agrees},
		query,
	)
	v := decide(ranked)
	if v.Decision != "REVIEW" {
		t.Errorf("decision = %s, want REVIEW: attributes must not promote", v.Decision)
	}

	// The same holds at the lower threshold.
	ranked = rankCandidates(
		map[string]float64{"A": 0.70},
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
			"top-by-attribute": 0.890,
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
// the attribute term can manufacture (2*attributeWeight = 0.06) is smaller than
// gapThreshold (0.08), attributes can never hand a confident MATCH to an animal
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
	// adjusted gap reaches 0.06, still short of the 0.08 a MATCH needs.
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
