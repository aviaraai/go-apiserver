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

type RegistrationFailureResponse struct {
	ID        int64          `json:"id"`
	ErrorCode string         `json:"error_code"`
	ImageURLs []string       `json:"image_urls"`
	Detail    map[string]any `json:"detail"`
	Device    DeviceResponse `json:"device"`
	CreatedBy string         `json:"created_by"`
	CreatedAt time.Time      `json:"created_at"`
}

// MatchedAnimalResponse is the animal a search resolved to, read live from the
// animals table rather than from a copy taken at search time. It is null when
// the search matched nothing, and also when the matched animal has since been
// deleted — the search record itself survives either way.
type MatchedAnimalResponse struct {
	GodhaarID   string  `json:"godhaar_id"`
	Type        *string `json:"animal_type"`
	Breed       *string `json:"breed"`
	Gender      *string `json:"gender"`
	Age         *int    `json:"age"`
	BodyColor   *string `json:"body_color"`
	MuzzleColor *string `json:"muzzle_color"`
	HornShape   *string `json:"horn_shape"`
	Village     *string `json:"village"`
	Mandal      *string `json:"mandal"`
	District    *string `json:"district"`
	State       *string `json:"state"`
	ImageURL    *string `json:"image_url"`

	// Deleted marks a search whose godhaar_id no longer resolves, so the
	// dashboard can show the id without implying the animal is still there.
	Deleted bool `json:"deleted"`
}

type SearchRecordResponse struct {
	ID       int64    `json:"id"`
	Decision string   `json:"decision"`
	Score    *float64 `json:"score"`

	// ErrorCode is set only when decision is FAILED.
	ErrorCode *string `json:"error_code"`

	// Verified is always present but only meaningful on a MATCH; every other
	// decision is fixed at "not_verified" because there is no claim to check.
	Verified string `json:"verified"`

	Matched   *MatchedAnimalResponse `json:"matched_animal"`
	ImageURLs []string               `json:"image_urls"`
	Detail    map[string]any         `json:"detail"`
	Device    DeviceResponse         `json:"device"`
	CreatedBy string                 `json:"created_by"`
	CreatedAt time.Time              `json:"created_at"`
}

type VerifyRequest struct {
	ID       int64  `param:"id"`
	Verified string `json:"verified" form:"verified"`
}
