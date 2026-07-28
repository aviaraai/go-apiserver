package qr

type CreateQR struct {
	AnimalID int64  `db:"animal_id"`
	Key      string `db:"qr_image_key"`
}

type QRRow struct {
	AnimalID int64  `db:"animal_id"`
	Key      string `db:"qr_image_key"`
}
