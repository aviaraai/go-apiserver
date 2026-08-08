package farmer

type CreateFarmer struct {
	PublicID       string  `db:"public_id"`
	Name           string  `db:"name"`
	Type           string  `db:"farmer_type"`
	Relation       *string `db:"relation"`
	RelationName   *string `db:"relation_name"`
	PhoneNumber    string  `db:"phone_number"`
	State          string  `db:"state"`
	District       string  `db:"district"`
	Mandal         string  `db:"mandal"`
	Village        string  `db:"village"`
	PhotoKey       string  `db:"photo_key"`
	Latitude       float64 `db:"latitude"`
	Longitude      float64 `db:"longitude"`
	CreatedBy      string  `db:"created_by"`
	CreatedByEmail string  `db:"created_by_email"`
	UpdatedBy      string  `db:"updated_by"`
	UpdatedByEmail string  `db:"updated_by_email"`
}

type Farmer struct {
	PublicID     string  `db:"public_id"`
	Name         string  `db:"name"`
	Type         string  `db:"farmer_type"`
	Relation     *string `db:"relation"`
	RelationName *string `db:"relation_name"`
	PhoneNumber  string  `db:"phone_number"`
	State        string  `db:"state"`
	District     string  `db:"district"`
	Mandal       string  `db:"mandal"`
	Village      string  `db:"village"`
	PhotoKey     string  `db:"photo_key"`
}
