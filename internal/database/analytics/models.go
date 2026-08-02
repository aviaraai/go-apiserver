package analytics

type UserAnalytics struct {
	TotalFarmers     int `db:"total_farmers"`
	TotalAnimals     int `db:"total_animals"`
	AssignedMale     int `db:"assigned_male"`
	AssignedFemale   int `db:"assigned_female"`
	UnassignedMale   int `db:"unassigned_male"`
	UnassignedFemale int `db:"unassigned_female"`
}

type AdminAnalytics struct {
	UserEmail       string `db:"user_email"`
	TotalFarmers    int    `db:"total_farmers"`
	TotalAnimals    int    `db:"total_animals"`
	TotalAssigned   int    `db:"total_assigned"`
	TotalUnassigned int    `db:"total_unassigned"`
}

type AdminTotalAnalytics struct {
	TotalFarmers int `db:"total_farmers"`
	TotalAnimals int `db:"total_animals"`
}

type LegacyAnalytics struct {
	State       string `db:"state"`
	District    string `db:"district"`
	Mandal      string `db:"mandal"`
	FarmerCount int    `db:"farmer_count"`
	AnimalCount int    `db:"animal_count"`
}
