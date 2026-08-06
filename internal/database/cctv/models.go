// Package cctv owns the record of CCTV analyses admins have requested.
//
// Unlike the identification debug tables, these rows carry a real foreign key
// to farmers: this is operational data about a specific goshala, not a
// developer-only capture, so it should not outlive the farmer it describes.
package cctv

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrRequestNotFound is returned when an update targets a row that is gone.
var ErrRequestNotFound = errors.New("cctv analysis request not found")

// ErrGoshalaNotFound is returned when a public id matches no farmer, or matches
// one that is not a goshala. The two are deliberately one error: telling an
// admin which of the two it was would confirm the existence of farmers they
// were not otherwise shown.
var ErrGoshalaNotFound = errors.New("goshala not found")

// Request statuses. A row starts running and ends in exactly one of the others.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

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
	switch v := src.(type) {
	case nil:
		*m = nil
		return nil
	case []byte:
		return json.Unmarshal(v, m)
	case string:
		return json.Unmarshal([]byte(v), m)
	default:
		return fmt.Errorf("scan jsonb: unsupported source type %T", src)
	}
}

// Goshala is one selectable camera site: a farmer whose type is goshala.
type Goshala struct {
	FarmerID  int64   `db:"id"`
	PublicID  string  `db:"public_id"`
	Name      string  `db:"name"`
	State     string  `db:"state"`
	District  string  `db:"district"`
	Mandal    string  `db:"mandal"`
	Village   string  `db:"village"`
	PhotoKey  string  `db:"photo_key"`
	Latitude  float64 `db:"latitude"`
	Longitude float64 `db:"longitude"`
}

// CreateRequest opens a new analysis record, before any work is attempted.
type CreateRequest struct {
	FarmerID         int64  `db:"farmer_id"`
	RequestedBy      string `db:"requested_by"`
	RequestedByEmail string `db:"requested_by_email"`
}

// CompleteRequest closes a record that produced a result.
type CompleteRequest struct {
	ID                int64   `db:"id"`
	InferenceJobID    string  `db:"inference_job_id"`
	SourceVideoKey    *string `db:"source_video_key"`
	AnnotatedVideoKey string  `db:"annotated_video_key"`
	TotalAnimals      int     `db:"total_animals"`
	TotalClearAnimals int     `db:"total_clear_animals"`
	Detail            JSONMap `db:"detail"`
}

// FailRequest closes a record that did not.
type FailRequest struct {
	ID             int64   `db:"id"`
	InferenceJobID *string `db:"inference_job_id"`
	SourceVideoKey *string `db:"source_video_key"`
	ErrorCode      string  `db:"error_code"`
	Detail         JSONMap `db:"detail"`
}

// RequestRow is one history entry, joined to the goshala it was run against.
type RequestRow struct {
	ID                int64      `db:"id"`
	Status            string     `db:"status"`
	InferenceJobID    *string    `db:"inference_job_id"`
	SourceVideoKey    *string    `db:"source_video_key"`
	AnnotatedVideoKey *string    `db:"annotated_video_key"`
	TotalAnimals      *int       `db:"total_animals"`
	TotalClearAnimals *int       `db:"total_clear_animals"`
	ErrorCode         *string    `db:"error_code"`
	Detail            JSONMap    `db:"detail"`
	RequestedBy       string     `db:"requested_by"`
	RequestedByEmail  string     `db:"requested_by_email"`
	RequestedAt       time.Time  `db:"requested_at"`
	CompletedAt       *time.Time `db:"completed_at"`

	FarmerID        int64  `db:"farmer_id"`
	GoshalaPublicID string `db:"goshala_public_id"`
	GoshalaName     string `db:"goshala_name"`
	GoshalaState    string `db:"goshala_state"`
	GoshalaDistrict string `db:"goshala_district"`
	GoshalaMandal   string `db:"goshala_mandal"`
	GoshalaVillage  string `db:"goshala_village"`
}
