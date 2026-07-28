package qr

type QRRequest struct {
	GodhaarID string `param:"godhaar_id"`
}

type QRGenerateResponse struct {
	GodhaarID string `json:"godhaar_id"`
	QRKey     string `json:"qr_key"`
}

type QRScanRequest struct {
	Token string `param:"qr_token"`
}

type QRScanResponse struct {
	GodhaarID string `json:"godhaar_id"`
}

type QRResponse struct {
	PreSignedURL string `json:"signed_url"`
}
