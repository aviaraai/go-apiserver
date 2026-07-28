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
	State            string   `form:"state"`
	District         string   `form:"district"`
	Mandal           string   `form:"mandal"`
	Village          string   `form:"village"`
	Latitude         float64  `form:"latitude"`
	Longitude        float64  `form:"longitude"`
	HealthRemarks    *string  `form:"health_remarks"`
}

type SearchAnimalRequest struct {
	Latitude  float64 `form:"latitude"`
	Longitude float64 `form:"longitude"`
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
	State            string   `json:"state"`
	District         string   `json:"district"`
	Mandal           string   `json:"mandal"`
	Village          string   `json:"village"`
	ImageURL         string   `json:"image_url"`
}

type RegisterResponse struct {
	GodhaarID string `json:"godhaar_id"`
}

type SearchResponse struct {
	GodhaarID *string `json:"godhaar_id"`
	Score     float64 `json:"score"`
}
