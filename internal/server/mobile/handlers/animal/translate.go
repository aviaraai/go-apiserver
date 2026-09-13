package animal

import (
	"context"
	"log/slog"

	"go-api-server/internal/inference"
)

// translatedDecision is inference_server's verdict after its FAISS ids have
// been resolved to godhaar_ids — the only form of it anything downstream may
// use.
type translatedDecision struct {
	Decision string
	Reason   string
	Score    float64
	Gap      float64

	// GodhaarID is set for MATCH only.
	GodhaarID *string
	// GodhaarIDs holds 1..3 candidates for REVIEW only, best first.
	GodhaarIDs []string

	// Unmapped records every FAISS id the decision referenced that this
	// service could not resolve. Non-empty means the FAISS index and the
	// database disagree — a stale index, or an animal deleted after the search
	// began. It is deliberately surfaced rather than swallowed.
	Unmapped []int64
	// Degraded is true when translation lost information: an unresolvable
	// MATCH, or a REVIEW that lost at least one candidate.
	Degraded bool
}

// translateDecision resolves a decision's FAISS ids through faissToGodhaar.
//
// WHY THIS EXISTS AT ALL. inference_server has no database connection and no
// way to know a godhaar_id; it can only ever name FAISS ids. The app speaks
// godhaar_id exclusively. An untranslated id reaching the client does not fail
// loudly — the app looks up an animal it cannot find and renders a correct
// MATCH as "not registered", which reads as a model failure and is
// indistinguishable from one in the logs. Translation is therefore not a
// formatting step, it is the step that makes the decision mean anything.
//
// UNMAPPED IDS. A FAISS id with no row in faissToGodhaar means the index holds
// a vector the database does not — a stale index that outlived a deletion, or
// a re-index that ran against different data. The chosen behaviour, per
// outcome:
//
//	MATCH, id unmapped     -> DEMOTED to UNKNOWN. Reporting "not registered"
//	                          is wrong, but naming an animal this service
//	                          cannot produce is worse: the app would show a
//	                          blank or crash on the lookup. UNKNOWN is the
//	                          honest floor, and Degraded + Unmapped carry the
//	                          real reason to the logs.
//	REVIEW, some unmapped  -> the resolvable candidates are KEPT, in order, and
//	                          the unmapped ones dropped. A REVIEW exists so a
//	                          human can choose; two real options are still
//	                          useful, and the alternative (discarding the whole
//	                          verdict over one bad row) throws away good
//	                          candidates.
//	REVIEW, all unmapped   -> UNKNOWN, same reasoning as MATCH.
//	UNKNOWN                -> unchanged; it names nothing to begin with.
//
// Every unmapped id is logged at WARN with the request id. This is an
// index/database consistency fault and should be visible without anyone
// having to reproduce a search.
func translateDecision(
	ctx context.Context,
	d *inference.InferenceDecision,
	faissToGodhaar map[int64]string,
	requestID string,
) *translatedDecision {
	if d == nil {
		return nil
	}

	out := &translatedDecision{
		Decision: d.Decision,
		Reason:   d.Reason,
		Score:    d.Score,
		Gap:      d.Gap,
	}

	switch d.Decision {
	case "MATCH":
		if d.FaissID == nil {
			// MATCH with no id is a contract violation by the inference
			// server, not an unmapped-id case. Fail closed.
			out.Decision, out.Reason, out.Degraded = "UNKNOWN", d.Reason+"_match_without_id", true
			break
		}
		gid, ok := faissToGodhaar[*d.FaissID]
		if !ok {
			out.Unmapped = append(out.Unmapped, *d.FaissID)
			out.Decision, out.Reason, out.Degraded = "UNKNOWN", d.Reason+"_unmapped_faiss_id", true
			break
		}
		out.GodhaarID = &gid

	case "REVIEW":
		for _, fid := range d.FaissIDs {
			gid, ok := faissToGodhaar[fid]
			if !ok {
				out.Unmapped = append(out.Unmapped, fid)
				continue
			}
			out.GodhaarIDs = append(out.GodhaarIDs, gid)
		}
		if len(out.Unmapped) > 0 {
			out.Degraded = true
		}
		if len(out.GodhaarIDs) == 0 {
			out.Decision, out.Reason = "UNKNOWN", d.Reason+"_all_candidates_unmapped"
		}
	}

	if len(out.Unmapped) > 0 {
		slog.LogAttrs(ctx, slog.LevelWarn, "decision referenced unmapped faiss_id(s)",
			slog.String("requestID", requestID),
			slog.String("inferenceDecision", d.Decision),
			slog.String("translatedDecision", out.Decision),
			slog.Any("unmappedFaissIDs", out.Unmapped),
			slog.Int("candidateMapSize", len(faissToGodhaar)),
			slog.String("cause", "FAISS index holds vectors the database does not — "+
				"stale index, deleted animal, or a re-index against different data"),
		)
	}
	return out
}
