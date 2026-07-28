package image

type ImageRequest struct {
	GodhaarID string `param:"godhaar_id"`
}

type ImageResponse struct {
	Type string `json:"image_type"`
	URL  string `json:"image_url"`
}
