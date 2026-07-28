package analytics

type Analytics struct {
	TotalFarmers     int `db:"total_farmers"`
	TotalAnimals     int `db:"total_animals"`
	AssignedMale     int `db:"assigned_male"`
	AssignedFemale   int `db:"assigned_female"`
	UnassignedMale   int `db:"unassigned_male"`
	UnassignedFemale int `db:"unassigned_female"`
}
