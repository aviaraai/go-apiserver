package animal

type FarmerIDRequest struct {
	PublicID *string `query:"public_id"`
}

type AnimalRequest struct {
	GodhaarID string `param:"godhaar_id"`
}

type RegisterAnimalRequest struct {
	PublicID         *string  `form:"public_id"`
	Type             string   `form:"animal_type"`
	Gender           string   `form:"gender"`
	Breed            string   `form:"breed"`
	Age              int      `form:"age"`
	Cost             *float64 `form:"cost"`
	InsurancePremium *float64 `form:"insurance_premium"`
	TagID            *string  `form:"tag_id,omitempty"`
	State            string   `form:"state"`
	District         string   `form:"district"`
	Mandal           string   `form:"mandal"`
	Village          string   `form:"village"`
	Latitude         float64  `form:"latitude"`
	Longitude        float64  `form:"longitude"`
	HealthRemarks    *string  `form:"health_remarks"`
}

type SearchAnimalByTagIDRequest struct {
	TagID string `param:"tag_id"`
}

type SearchAnimalRequest struct {
	Latitude  float64 `form:"latitude"`
	Longitude float64 `form:"longitude"`
	TagID     *string `form:"tag_id"`
}

type AnimalResponse struct {
	GodhaarID        string   `json:"godhaar_id"`
	PublicID         *string  `json:"public_id"`
	Type             string   `json:"animal_type"`
	Gender           string   `json:"gender"`
	Breed            string   `json:"breed"`
	Age              int      `json:"age"`
	Cost             *float64 `json:"cost"`
	InsurancePremium *float64 `json:"insurance_premium"`
	TagID            *string  `json:"tag_id"`
	State            string   `json:"state"`
	District         string   `json:"district"`
	Mandal           string   `json:"mandal"`
	Village          string   `json:"village"`
	HealthRemarks    *string  `json:"health_remarks"`
	ImageURL         string   `json:"image_url"`
}

type RegisterResponse struct {
	GodhaarID string `json:"godhaar_id"`
}

type SearchResponse struct {
	GodhaarID *string `json:"godhaar_id"`
	Decision  string  `json:"decision"`
	Score     float64 `json:"score"`

	// InferenceDecision is inference_server's own verdict, translated into
	// godhaar_ids. ADDITIVE: the three fields above keep their exact previous
	// meaning and are still produced by this service's own engine, so an app
	// that has never heard of this field parses the response unchanged.
	//
	// Present only when the inference server returned a decision (nil on an
	// older inference build). See InferenceDecisionView for the per-outcome
	// shape.
	InferenceDecision *InferenceDecisionView `json:"inference_decision,omitempty"`

	// MuzzlePhotoCount is how many muzzle photos this search was run on. The
	// decision thresholds were calibrated at 3 (62.1% match rate); at 1 the
	// measured rate is 45.8% and false accepts rise from 0 to 6 per 203. A
	// client sending fewer than 3 is operating outside the calibration, and
	// this field is how it finds out.
	MuzzlePhotoCount int `json:"muzzle_photo_count"`

	// CalibrationWarning is set when MuzzlePhotoCount < 3. Advisory only — the
	// search is still answered.
	CalibrationWarning *string `json:"calibration_warning,omitempty"`
}

// InferenceDecisionView is the decision as the app sees it, after FAISS ids
// have been resolved to godhaar_ids.
//
//	MATCH   -> godhaar_id set, candidates null
//	REVIEW  -> candidates holds 1..3 ids best-first, godhaar_id null
//	UNKNOWN -> both null
//
// `candidates` is null rather than [] for MATCH and UNKNOWN, so "no list" and
// "an empty list" can never be confused by a client that checks for presence.
type InferenceDecisionView struct {
	Decision  string  `json:"decision"`
	Reason    string  `json:"reason"`
	Score     float64 `json:"score"`
	Gap       float64 `json:"gap"`
	GodhaarID *string `json:"godhaar_id"`
	// Candidates is nil unless Decision is REVIEW.
	Candidates []string `json:"candidates"`
	// Degraded is true when translation could not resolve every FAISS id the
	// inference server named — the verdict here is the conservative fallback,
	// not what inference concluded. See translate.go.
	Degraded bool `json:"degraded"`
}
