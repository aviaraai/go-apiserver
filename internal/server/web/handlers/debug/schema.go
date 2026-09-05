package debug

import "time"

// DeviceResponse is the client context a record was captured from. Fields stay
// pointers so the dashboard can tell "the app never sent this" from "the app
// sent an empty value".
type DeviceResponse struct {
	AppVersion         *string `json:"app_version"`
	OSVersion          *string `json:"os_version"`
	DeviceModel        *string `json:"device_model"`
	DeviceManufacturer *string `json:"device_manufacturer"`
}

// ImageResponse is one stored photo, labelled with the slot it was taken for so
// the dashboard can lay the set out rather than printing an anonymous list.
//
// Slot is one of: front, muzzle, left, right. Anything unrecognised arrives as
// "unknown" with sequence 0 — a photo whose slot cannot be read is still worth
// showing.
type ImageResponse struct {
	Slot     string `json:"slot"`
	Sequence int    `json:"sequence"`
	URL      string `json:"url"`
}

// MatchedAnimalResponse is the already-registered animal a record resolved to:
// the one a search landed on, or the one a registration was rejected as a
// duplicate of.
//
// It carries the identity and the photos and nothing else. Breed, age, owner
// and location say nothing about whether the model was right — the photos next
// to the submitted ones are what settles that, and they are the whole reason
// this object exists.
//
// Which slots arrive depends on the record: a search shows front, muzzle, left
// and right, a duplicate registration shows front and muzzle, because those are
// what each decision was actually made on. Read Slot rather than assuming a
// count. Every URL is presigned for direct download.
type MatchedAnimalResponse struct {
	GodhaarID string          `json:"godhaar_id"`
	Images    []ImageResponse `json:"images"`

	// Deleted marks a record whose godhaar_id no longer resolves, so the
	// dashboard can show the id without implying the animal is still there.
	// Images is empty in that case.
	Deleted bool `json:"deleted"`
}

// ── Registration failures ────────────────────────────────────────────────────

// RegistrationFailureListItem is one card in the listing.
//
// ErrorCode is the only filterable dimension besides the timestamp, and it is a
// closed set: registrations are captured only for model verdicts, so the value
// is always one of the inference domain codes (DUPLICATE_ANIMAL,
// NO_ANIMAL_DETECTED, IMAGE_TOO_BLURRY, IMAGE_BAD_EXPOSURE, IMAGE_TOO_SMALL,
// IMAGE_UNREADABLE, BODY_COLOR_INCONSISTENT, MUZZLE_COLOR_INCONSISTENT,
// POOR_IMAGE_QUALITY). It is safe to switch on.
type RegistrationFailureListItem struct {
	RegistrationID string `json:"registration_id"`
	ErrorCode      string `json:"error_code"`

	// ThumbnailURL is the first captured photo, for the card. Null when the
	// record kept no images — the upload can fail without sinking the capture.
	ThumbnailURL *string `json:"thumbnail_url"`

	Device         DeviceResponse `json:"device"`
	CreatedByEmail *string        `json:"created_by_email"`
	CreatedAt      time.Time      `json:"created_at"`
}

// RegistrationFailureDetailResponse is one failure in full: every photo the
// model was given, the animal it was rejected in favour of, and the raw verdict
// payload behind the code.
type RegistrationFailureDetailResponse struct {
	RegistrationID string `json:"registration_id"`
	ErrorCode      string `json:"error_code"`

	// Images are the photos the registrant submitted, all of them: two front
	// and three muzzle, whatever the model was actually shown.
	Images []ImageResponse `json:"images"`

	// Matched is the already-registered animal a DUPLICATE_ANIMAL rejection
	// pointed at, with its own front and muzzle photos, so the two sets can be
	// put side by side. Null for every other error code — nothing else is a
	// claim about which animal this is — and null on a duplicate whose matched
	// FAISS id could not be resolved back to an animal.
	Matched *MatchedAnimalResponse `json:"matched_animal"`

	Detail         map[string]any `json:"detail"`
	Device         DeviceResponse `json:"device"`
	CreatedByEmail *string        `json:"created_by_email"`
	CreatedAt      time.Time      `json:"created_at"`
}

// ── Searches ─────────────────────────────────────────────────────────────────

// SearchListItem is one card in the search listing.
//
// Decision and Verified are both closed sets enforced by database CHECK
// constraints, so the dashboard can filter on them directly:
//
//	Decision: MATCH   — the model named an animal
//	          REVIEW  — a candidate scored close, but not close enough to claim
//	          UNKNOWN — nothing near enough to compare against, or no candidate scored
//	          FAILED  — the model refused the photos; ErrorCode says why
//
//	Verified: yes | no | not_verified
//
// Verified is always present but only ever moves off "not_verified" on a MATCH:
// there is no claim to confirm on any other decision. A dashboard's "unreviewed
// matches" view is decision=MATCH and verified=not_verified.
type SearchListItem struct {
	SearchID string `json:"search_id"`
	Decision string `json:"decision"`
	Verified string `json:"verified"`

	// Score is the model's own similarity score for the top candidate. Null on
	// a FAILED search, where nothing was ever scored.
	Score *float64 `json:"score"`

	// ErrorCode is set only when Decision is FAILED, and is one of the same
	// inference domain codes the registration listing uses.
	ErrorCode *string `json:"error_code"`

	// GodhaarID is set only when Decision is MATCH.
	GodhaarID *string `json:"godhaar_id"`

	// ThumbnailURL is the searched photo, for the card. Null when the record
	// kept no images.
	ThumbnailURL *string `json:"thumbnail_url"`

	Device         DeviceResponse `json:"device"`
	CreatedByEmail *string        `json:"created_by_email"`
	CreatedAt      time.Time      `json:"created_at"`
}

// SearchDetailResponse is one search in full: the photos that were searched,
// the animal it landed on, and the decision engine's own working.
type SearchDetailResponse struct {
	SearchID  string   `json:"search_id"`
	Decision  string   `json:"decision"`
	Verified  string   `json:"verified"`
	Score     *float64 `json:"score"`
	ErrorCode *string  `json:"error_code"`

	// Images are the photos the searcher submitted: one front, one muzzle.
	Images []ImageResponse `json:"images"`

	// Matched is null for every decision except MATCH.
	Matched *MatchedAnimalResponse `json:"matched_animal"`

	// Detail carries the decision engine's working — adjusted_score, gap,
	// agreement, reason — or the model's rejection payload on a FAILED search.
	Detail map[string]any `json:"detail"`

	Device         DeviceResponse `json:"device"`
	CreatedByEmail *string        `json:"created_by_email"`
	CreatedAt      time.Time      `json:"created_at"`
}

type VerifyRequest struct {
	SearchID string `param:"search_id"`
	Verified string `json:"verified" form:"verified"`
}

// ── Muzzle embeddings (LightGlue cache backfill) ─────────────────────────────

// MuzzleEmbeddingItem is one registered muzzle embedding, with a presigned
// URL to the image it was computed from. Sole consumer:
// scripts/backfill_muzzle_cache.py, run on inference_server's deployed host
// to fill in LightGlue crop/feature cache entries for animals registered
// before that cache existed (see debugdb.MuzzleEmbeddingRow for why this
// join has to happen here, not on the inference_server side).
type MuzzleEmbeddingItem struct {
	FaissID   int64  `json:"faiss_id"`
	GodhaarID string `json:"godhaar_id"`
	Sequence  int    `json:"sequence"`
	ImageURL  string `json:"image_url"`
}
