package analytics

type GenderDistribution struct {
	Male   int `json:"male"`
	Female int `json:"female"`
}

type AnimalGroup struct {
	Total  int                `json:"total"`
	Gender GenderDistribution `json:"gender_distribution"`
}

type AnalyticsResponse struct {
	TotalFarmers int         `json:"total_farmers"`
	TotalAnimals int         `json:"total_animals"`
	Assigned     AnimalGroup `json:"assigned"`
	Unassigned   AnimalGroup `json:"unassigned"`
}
