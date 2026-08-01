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
	UserID          string `db:"user_id"`
	TotalFarmers    int    `db:"total_farmers"`
	TotalAnimals    int    `db:"total_animals"`
	TotalAssigned   int    `db:"total_assigned"`
	TotalUnassigned int    `db:"total_unassigned"`
}
