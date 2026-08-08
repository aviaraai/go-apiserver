package animal

type Animal struct {
	AnimalID         int64    `db:"id"`
	GodhaarID        string   `db:"godhaar_id"`
	PublicID         *string  `db:"public_id"`
	Type             string   `db:"animal_type"`
	Gender           string   `db:"gender"`
	Breed            string   `db:"breed"`
	Age              int      `db:"age"`
	Cost             *float64 `db:"cost"`
	InsurancePremium *float64 `db:"insurance_premium"`
	TagID            *string  `db:"tag_id"`
	State            string   `db:"state"`
	District         string   `db:"district"`
	Mandal           string   `db:"mandal"`
	Village          string   `db:"village"`
	ImageKey         string   `db:"image_key"`
}

type CandidateRow struct {
	FaissID     int64   `db:"faiss_id"`
	GodhaarID   string  `db:"godhaar_id"`
	Latitude    float64 `db:"latitude"`
	Longitude   float64 `db:"longitude"`
	BodyColor   string  `db:"body_color"`
	MuzzleColor string  `db:"muzzle_color"`
	HornShape   *string `db:"horn_shape"`
	TagID       *string `db:"tag_id"`
}

type CreateAnimalTx struct {
	Animal     CreateAnimal
	Embeddings []CreateEmbedding
	Images     []CreateImage
}

type CreateAnimal struct {
	GodhaarID        string   `db:"godhaar_id"`
	FarmerID         *int64   `db:"farmer_id"`
	Type             string   `db:"animal_type"`
	Gender           string   `db:"gender"`
	Breed            string   `db:"breed"`
	Age              int      `db:"age"`
	Cost             *float64 `db:"cost"`
	InsurancePremium *float64 `db:"insurance_premium"`
	State            string   `db:"state"`
	District         string   `db:"district"`
	Mandal           string   `db:"mandal"`
	Village          string   `db:"village"`
	TagID            *string  `db:"tag_id"`
	BodyColor        string   `db:"body_color"`
	MuzzleColor      string   `db:"muzzle_color"`
	HornShape        *string  `db:"horn_shape"`
	HealthRemarks    *string  `db:"health_remarks"`
	Latitude         float64  `db:"latitude"`
	Longitude        float64  `db:"longitude"`
	CreatedBy        string   `db:"created_by"`
	CreatedByEmail   string   `db:"created_by_email"`
	UpdatedBy        string   `db:"updated_by"`
	UpdatedByEmail   string   `db:"updated_by_email"`
}

type CreateEmbedding struct {
	EmbeddingType string `db:"embedding_type"`
	Sequence      int    `db:"sequence"`
	FaissID       int64  `db:"faiss_id"`
}

type CreateImage struct {
	ImageType string `db:"image_type"`
	Sequence  int    `db:"sequence"`
	ImageKey  string `db:"image_key"`
}
