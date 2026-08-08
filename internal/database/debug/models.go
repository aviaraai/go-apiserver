// Package debug owns the developer-facing record of what the identification
// model actually did: registrations it rejected, and every search it answered.
//
// It is deliberately separate from the animal package. These tables are read by
// the internal dashboard, not by the mobile app, and they hold no foreign keys
// into the user-facing schema — see the migration for why.
package debug

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Decision values. The first three come from the search decision engine; a
// FAILED row is one where the model rejected the images outright.
const (
	DecisionMatch   = "MATCH"
	DecisionReview  = "REVIEW"
	DecisionUnknown = "UNKNOWN"
	DecisionFailed  = "FAILED"
)

// Verification states for a MATCH, settled by a human on the dashboard.
const (
	VerifiedYes = "yes"
	VerifiedNo  = "no"
	VerifiedNot = "not_verified"
)

// ValidVerification reports whether s is a verification state the API accepts.
func ValidVerification(s string) bool {
	return s == VerifiedYes || s == VerifiedNo || s == VerifiedNot
}

// JSONMap is a free-form object in a jsonb column. Value renders it as text and
// every query casts the parameter explicitly, because the driver would
// otherwise send a []byte as bytea.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonb: %w", err)
	}
	return string(b), nil
}

func (m *JSONMap) Scan(src any) error {
	raw, err := jsonBytes(src)
	if err != nil || raw == nil {
		return err
	}
	return json.Unmarshal(raw, m)
}

// JSONStrings is a string array in a jsonb column, used for image keys.
type JSONStrings []string

func (s JSONStrings) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonb: %w", err)
	}
	return string(b), nil
}

func (s *JSONStrings) Scan(src any) error {
	raw, err := jsonBytes(src)
	if err != nil || raw == nil {
		return err
	}
	return json.Unmarshal(raw, s)
}

func jsonBytes(src any) ([]byte, error) {
	switch v := src.(type) {
	case nil:
		return nil, nil
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("scan jsonb: unsupported source type %T", src)
	}
}

// DeviceColumns is the client context every record carries. The fields are
// pointers so a header the app never sent is stored as NULL rather than as an
// empty string — "old client did not report this" and "client reported nothing"
// are different findings when triaging a spike.
type DeviceColumns struct {
	AppVersion         *string `db:"app_version"`
	OSVersion          *string `db:"os_version"`
	DeviceModel        *string `db:"device_model"`
	DeviceManufacturer *string `db:"device_manufacturer"`
}

// CreateRegistrationFailure is a registration the model refused, kept with the
// images that caused it so the rejection can be reproduced.
type CreateRegistrationFailure struct {
	DeviceColumns
	ErrorCode      string      `db:"error_code"`
	ImageKeys      JSONStrings `db:"image_keys"`
	Detail         JSONMap     `db:"detail"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail string      `db:"created_by_email"`
}

// CreateSearchRecord is one search attempt of any outcome. GodhaarID is set
// only for a MATCH, ErrorCode only for a FAILED, and Score for everything the
// model actually scored.
type CreateSearchRecord struct {
	DeviceColumns
	Decision       string      `db:"decision"`
	GodhaarID      *string     `db:"godhaar_id"`
	Score          *float64    `db:"score"`
	ErrorCode      *string     `db:"error_code"`
	ImageKeys      JSONStrings `db:"image_keys"`
	Detail         JSONMap     `db:"detail"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail string      `db:"created_by_email"`
}

// RegistrationFailureRow is one failure in full, for the detail view.
type RegistrationFailureRow struct {
	DeviceColumns
	RegistrationID string      `db:"registration_id"`
	ErrorCode      string      `db:"error_code"`
	ImageKeys      JSONStrings `db:"image_keys"`
	Detail         JSONMap     `db:"detail"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail *string     `db:"created_by_email"`
	CreatedAt      time.Time   `db:"created_at"`
}

// RegistrationFailureListRow is the same record reduced to what a listing
// needs: every field the dashboard filters or sorts on, plus the image keys the
// card thumbnail comes from. The detail jsonb is left behind — it is the
// largest column and nothing on the listing reads it.
type RegistrationFailureListRow struct {
	DeviceColumns
	RegistrationID string      `db:"registration_id"`
	ErrorCode      string      `db:"error_code"`
	ImageKeys      JSONStrings `db:"image_keys"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail *string     `db:"created_by_email"`
	CreatedAt      time.Time   `db:"created_at"`
}

// SearchRecordRow is one search in full, for the detail view.
//
// It carries no columns from the matched animal. What the dashboard needs about
// that animal is its identity and its photos, and both are read separately by
// MatchedAnimalImages — joining the animals table here would drag in breed, age
// and location, none of which say anything about how the model performed.
type SearchRecordRow struct {
	DeviceColumns
	SearchID       string      `db:"search_id"`
	Decision       string      `db:"decision"`
	GodhaarID      *string     `db:"godhaar_id"`
	Score          *float64    `db:"score"`
	ErrorCode      *string     `db:"error_code"`
	Verified       string      `db:"verified"`
	ImageKeys      JSONStrings `db:"image_keys"`
	Detail         JSONMap     `db:"detail"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail *string     `db:"created_by_email"`
	CreatedAt      time.Time   `db:"created_at"`
}

// SearchRecordListRow is the listing form. godhaar_id, decision and verified
// all live on the record itself, so a listing needs no join at all.
type SearchRecordListRow struct {
	DeviceColumns
	SearchID       string      `db:"search_id"`
	Decision       string      `db:"decision"`
	GodhaarID      *string     `db:"godhaar_id"`
	Score          *float64    `db:"score"`
	ErrorCode      *string     `db:"error_code"`
	Verified       string      `db:"verified"`
	ImageKeys      JSONStrings `db:"image_keys"`
	CreatedBy      string      `db:"created_by"`
	CreatedByEmail *string     `db:"created_by_email"`
	CreatedAt      time.Time   `db:"created_at"`
}

// AnimalImage is one stored photo of a registered animal, carrying the slot it
// was taken for. Unlike the debug images — whose slot has to be recovered from
// the object key — these are labelled by the images table itself.
type AnimalImage struct {
	ImageType string `db:"image_type"`
	Sequence  int    `db:"sequence"`
	ImageKey  string `db:"image_key"`
}
