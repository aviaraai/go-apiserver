package analytics

type AnalyticsRequest struct {
	State    *string `query:"state"`
	District *string `query:"district"`
	Mandal   *string `query:"mandal"`
	FromStr  *string `query:"from_date"`
	ToStr    *string `query:"to_date"`
}

type LegacyAnalyticsRequest struct {
	State    *string `query:"state"`
	District *string `query:"district"`
	Mandal   *string `query:"mandal"`
}

type AnalyticsResponse struct {
	UserEmail       string `json:"user_email"`
	TotalFarmers    int    `json:"total_farmers"`
	TotalAnimals    int    `json:"total_animals"`
	TotalAssigned   int    `json:"total_assigned"`
	TotalUnassigned int    `json:"total_unassigned"`
}

type TotalAnalyticsResponse struct {
	TotalFarmers int `json:"total_farmers"`
	TotalAnimals int `json:"total_animals"`
}

type LegacyAnalyticsResponse struct {
	State       string `json:"state"`
	District    string `json:"district"`
	Mandal      string `json:"mandal"`
	FarmerCount int    `json:"farmer_count"`
	AnimalCount int    `json:"animal_count"`
}
