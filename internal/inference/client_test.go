package inference

import (
	"mime/multipart"
	"testing"
)

// buildSearchBody's wire format is a real cross-service contract: it must
// match inference_server's /search signature exactly (muzzle_images: 1-3
// repeated File parts, front: File, top_k/candidates: Form). Parses the
// actual multipart body back out rather than just checking it builds
// without error -- a wrong field name here is a 422 in production, not a
// Go compile error, so nothing else catches it.

func TestBuildSearchBodySendsMuzzleImagesPlural(t *testing.T) {
	front := ImagePayload{Filename: "front.jpg", ContentType: "image/jpeg", Data: []byte("front-bytes")}
	muzzle := []ImagePayload{
		{Filename: "m1.jpg", ContentType: "image/jpeg", Data: []byte("m1-bytes")},
		{Filename: "m2.jpg", ContentType: "image/jpeg", Data: []byte("m2-bytes")},
		{Filename: "m3.jpg", ContentType: "image/jpeg", Data: []byte("m3-bytes")},
	}

	body, contentType, err := buildSearchBody(front, muzzle, nil, 5, nil)
	if err != nil {
		t.Fatalf("buildSearchBody: %v", err)
	}

	boundary := parseBoundary(t, contentType)
	reader := multipart.NewReader(body, boundary)
	form, err := reader.ReadForm(10 << 20)
	if err != nil {
		t.Fatalf("parse multipart body: %v", err)
	}

	muzzleParts := form.File["muzzle_images"]
	if len(muzzleParts) != 3 {
		t.Fatalf("muzzle_images: got %d parts, want 3 (one per query photo, same repeated-field "+
			"shape /register already uses for its own muzzle_images -- not the old singular 'muzzle' field)",
			len(muzzleParts))
	}

	if legacyParts := form.File["muzzle"]; len(legacyParts) != 0 {
		t.Fatalf("found %d part(s) still under the OLD singular 'muzzle' field name -- "+
			"inference_server's /search no longer accepts it", len(legacyParts))
	}

	frontParts := form.File["front"]
	if len(frontParts) != 1 {
		t.Fatalf("front: got %d parts, want exactly 1 (front stays single-photo)", len(frontParts))
	}
}

func TestBuildSearchBodyWithOneMuzzlePhotoStillWorks(t *testing.T) {
	// N=1 must keep working unchanged -- this is the exact backward-
	// compatibility case: a caller that hasn't adopted multi-photo yet.
	front := ImagePayload{Filename: "front.jpg", ContentType: "image/jpeg", Data: []byte("front-bytes")}
	muzzle := []ImagePayload{{Filename: "m1.jpg", ContentType: "image/jpeg", Data: []byte("m1-bytes")}}

	body, contentType, err := buildSearchBody(front, muzzle, nil, 5, nil)
	if err != nil {
		t.Fatalf("buildSearchBody: %v", err)
	}

	boundary := parseBoundary(t, contentType)
	reader := multipart.NewReader(body, boundary)
	form, err := reader.ReadForm(10 << 20)
	if err != nil {
		t.Fatalf("parse multipart body: %v", err)
	}

	if got := len(form.File["muzzle_images"]); got != 1 {
		t.Fatalf("muzzle_images: got %d parts, want 1", got)
	}
}

func parseBoundary(t *testing.T, contentType string) string {
	t.Helper()
	const prefix = "multipart/form-data; boundary="
	if len(contentType) <= len(prefix) || contentType[:len(prefix)] != prefix {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	return contentType[len(prefix):]
}
