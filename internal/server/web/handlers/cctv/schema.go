package cctv

import "time"

type GoshalaResponse struct {
	PublicID  string  `json:"public_id"`
	Name      string  `json:"name"`
	Village   string  `json:"village"`
	Mandal    string  `json:"mandal"`
	District  string  `json:"district"`
	State     string  `json:"state"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	PhotoURL  string  `json:"photo_url"`
}

// GoshalaPublicID needs BOTH tags: POST /cctv/analyse arrives as a JSON body
// (Echo's JSON binder reads only the `json` tag, ignoring `form`), while
// POST /cctv/analyse/upload arrives as multipart (Echo's form binder is the
// reverse). Dropping either tag silently breaks binding for exactly one of
// the two endpoints -- there was no `json` tag here before, which meant the
// JSON path never actually populated this field.
type AnalyseRequest struct {
	GoshalaPublicID string `json:"goshala_public_id" form:"goshala_public_id"`
}

// AnalysisResponse is one completed analysis.
//
// Both counts are returned because they answer different questions and neither
// supersedes the other: total_animals is the peak seen in a single frame,
// total_clear_animals is how many distinct animals were tracked. Picking one
// here would be picking one for every camera and every situation.
type AnalysisResponse struct {
	RequestID         int64          `json:"request_id"`
	Status            string         `json:"status"`
	Goshala           GoshalaSummary `json:"goshala"`
	TotalAnimals      *int           `json:"total_animals"`
	TotalClearAnimals *int           `json:"total_clear_animals"`
	AnnotatedVideoURL *string        `json:"annotated_video_url"`
	SourceVideoURL    *string        `json:"source_video_url"`
	ErrorCode         *string        `json:"error_code"`
	RequestedBy       string         `json:"requested_by_email"`
	RequestedAt       time.Time      `json:"requested_at"`
	CompletedAt       *time.Time     `json:"completed_at"`
}

type GoshalaSummary struct {
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
	Village  string `json:"village"`
	Mandal   string `json:"mandal"`
	District string `json:"district"`
	State    string `json:"state"`
}

type HistoryRequest struct {
	GoshalaPublicID *string `query:"goshala_public_id"`
}
