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

type AnalyseRequest struct {
	GoshalaPublicID string `json:"goshala_public_id" form:"goshala_public_id"`
}

// AnalysisResponse is one completed analysis.
//
// Both counts are returned because they answer different questions and neither
// supersedes the other: cattle_in_view is the peak seen in a single frame,
// cattle_observed is how many distinct animals were tracked. Picking one here
// would be picking one for every camera and every situation.
type AnalysisResponse struct {
	RequestID int64          `json:"request_id"`
	Status    string         `json:"status"`
	Goshala   GoshalaSummary `json:"goshala"`

	CattleInView   *int `json:"cattle_in_view"`
	CattleObserved *int `json:"cattle_observed"`

	// AnnotatedVideoURL is the deliverable: the clip with bounding boxes and
	// per-animal IDs drawn on it. SourceVideoURL is the untouched camera clip,
	// returned alongside so the dashboard can offer a before/after comparison.
	// Both are presigned and short-lived — see the frontend contract.
	AnnotatedVideoURL *string `json:"annotated_video_url"`
	SourceVideoURL    *string `json:"source_video_url"`

	ErrorCode *string `json:"error_code"`

	RequestedBy string     `json:"requested_by_email"`
	RequestedAt time.Time  `json:"requested_at"`
	CompletedAt *time.Time `json:"completed_at"`
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
