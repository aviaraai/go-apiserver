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
	State            string   `db:"state"`
	District         string   `db:"district"`
	Mandal           string   `db:"mandal"`
	Village          string   `db:"village"`
	ImageKey         string   `db:"image_key"`
}

type CandidateRow struct {
	FaissID          int64   `db:"faiss_id"`
	GodhaarID        string  `db:"godhaar_id"`
	Latitude         float64 `db:"latitude"`
	Longitude        float64 `db:"longitude"`
	BodyColorLabel   string  `db:"body_color"`
	MuzzleColorLabel string  `db:"muzzle_color"`
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
	BodyColor        string   `db:"body_color"`
	MuzzleColor      string   `db:"muzzle_color"`
	HealthRemarks    *string  `db:"health_remarks"`
	Latitude         float64  `db:"latitude"`
	Longitude        float64  `db:"longitude"`
	CreatedBy        string   `db:"created_by"`
	UpdatedBy        string   `db:"updated_by"`
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
