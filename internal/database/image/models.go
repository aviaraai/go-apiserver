package image

type ImageRow struct {
	Type string `db:"image_type"`
	Key  string `db:"image_key"`
}
