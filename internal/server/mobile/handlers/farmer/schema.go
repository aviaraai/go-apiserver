package farmer

type AddFarmerRequest struct {
	Name         string  `form:"name"`
	Type         string  `form:"farmer_type"`
	Relation     *string `form:"relation"`
	RelationName *string `form:"relation_name"`
	PhoneNumber  string  `form:"phone_number"`
	State        string  `form:"state"`
	District     string  `form:"district"`
	Mandal       string  `form:"mandal"`
	Village      string  `form:"village"`
	Latitude     float64 `form:"latitude"`
	Longitude    float64 `form:"longitude"`
}

type FarmerRequest struct {
	PublicID string `param:"public_id"`
}

type FarmerQueryRequest struct {
	PhoneNumber *string `query:"phone_number"`
}

type FarmerResponse struct {
	PublicID     string  `json:"public_id"`
	Name         string  `json:"name"`
	Type         string  `json:"farmer_type"`
	Relation     *string `json:"relation"`
	RelationName *string `json:"relation_name"`
	PhoneNumber  string  `json:"phone_number"`
	State        string  `json:"state"`
	District     string  `json:"district"`
	Mandal       string  `json:"mandal"`
	Village      string  `json:"village"`
}

type FarmerPhotoResponse struct {
	PhotoURL string `json:"photo_url"`
}
