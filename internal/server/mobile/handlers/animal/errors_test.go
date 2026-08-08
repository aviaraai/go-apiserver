package animal

import (
	"testing"

	"go-api-server/internal/inference"
)

// A code that reaches the app with no copy attached is the failure this table
// exists to prevent, and it is silent: the response still has the right code,
// so nothing errors — the user just sees a blank or generic caption. The two
// tables are maintained by hand, so the invariant between them is asserted
// rather than assumed.
func TestEveryPerImageCodeHasAVerdictMessage(t *testing.T) {
	for code := range perImageMessages {
		if _, ok := domainResponses[code]; !ok {
			t.Errorf("%s has per-image copy but no top-level verdict copy — a rejection "+
				"on this code alone would fall through to the generic contract message", code)
		}
	}

	// The reverse gap is legitimate for exactly one code: a duplicate animal is
	// a verdict about the registration, not about any one photo.
	for code := range domainResponses {
		if code == inference.CodeDuplicateAnimal {
			continue
		}
		if _, ok := perImageMessages[code]; !ok {
			t.Errorf("%s is a per-photo verdict with no per-image copy", code)
		}
	}
}

func TestSearchErrorDetailsCarriesPerPhotoContext(t *testing.T) {
	infErr := &inference.Error{
		Class: inference.ClassDomain,
		Code:  inference.CodePoorImageQuality,
		Quality: &inference.ImageQualityDetail{
			Message: "muzzle_1: bad_quality blur=12.30; muzzle_2: RECAPTURE_MULTI_CATTLE",
			Failures: []inference.ImageFailure{
				{Slot: "muzzle_1", Stage: "quality", ErrorCode: inference.CodeImageTooBlurry, Reason: "bad_quality blur=12.30"},
				{Slot: "muzzle_2", Stage: "detection", ErrorCode: inference.CodeMultiCattle, Reason: "RECAPTURE_MULTI_CATTLE"},
			},
		},
	}

	details := searchErrorDetails(infErr)
	images, ok := details["failed_images"].([]map[string]any)
	if !ok || len(images) != 2 {
		t.Fatalf("failed_images = %#v", details["failed_images"])
	}

	// The umbrella code says "not clear enough" for the set; each photo must
	// still explain itself, and the two here failed for unrelated reasons.
	if images[0]["message"] != perImageMessages[inference.CodeImageTooBlurry] {
		t.Errorf("muzzle_1 message = %v, want the blur caption", images[0]["message"])
	}
	if images[1]["message"] != perImageMessages[inference.CodeMultiCattle] {
		t.Errorf("muzzle_2 message = %v, want the multi-cattle caption", images[1]["message"])
	}
	if images[0]["message"] == images[1]["message"] {
		t.Error("both photos got the same caption — the per-image codes were ignored")
	}

	if _, present := details["color_readings"]; present {
		t.Error("color_readings should be absent when the rejection was not about colour")
	}
}

func TestSearchErrorDetailsCarriesColourReadings(t *testing.T) {
	infErr := &inference.Error{
		Class: inference.ClassDomain,
		Code:  inference.CodeBodyColorInconsistent,
		Quality: &inference.ImageQualityDetail{
			Message: "front images disagree",
			Failures: []inference.ImageFailure{
				{Slot: "front_1", Stage: "color", ErrorCode: inference.CodeBodyColorInconsistent, Reason: "read BLACK at 0.42"},
				{Slot: "front_2", Stage: "color", ErrorCode: inference.CodeBodyColorInconsistent, Reason: "read WHITE at 0.39"},
			},
			Readings: []inference.ColorReading{
				{Slot: "front_1", Label: "BLACK", Confidence: 0.42},
				{Slot: "front_2", Label: "WHITE", Confidence: 0.39},
			},
		},
	}

	details := searchErrorDetails(infErr)
	readings, ok := details["color_readings"].([]map[string]any)
	if !ok || len(readings) != 2 {
		t.Fatalf("color_readings = %#v", details["color_readings"])
	}
	if readings[0]["label"] != "BLACK" || readings[1]["label"] != "WHITE" {
		t.Errorf("readings lost their labels: %#v", readings)
	}
	if readings[0]["confidence"] != 0.42 {
		t.Errorf("readings[0] confidence = %v, want 0.42", readings[0]["confidence"])
	}

	// Both photos are listed: the disagreement is the defect, so there is no
	// single photo to point at.
	images := details["failed_images"].([]map[string]any)
	if len(images) != 2 {
		t.Fatalf("want both front photos listed, got %#v", images)
	}
}

// The duplicate path is the one that carries a godhaar_id, and it is the whole
// reason the matched faiss_id is parsed at all.
func TestRegisterErrorDetailsNamesTheExistingAnimal(t *testing.T) {
	infErr := &inference.Error{
		Class:     inference.ClassDomain,
		Code:      inference.CodeDuplicateAnimal,
		Duplicate: &inference.DuplicateDetail{MatchedFaissID: 42, TopScore: 0.97, BodyColor: "BLACK", MuzzleColor: "PINK"},
	}

	details := registerErrorDetails(infErr, "GD-TG-ABC-123")
	if details["godhaar_id"] != "GD-TG-ABC-123" {
		t.Errorf("godhaar_id = %v", details["godhaar_id"])
	}
	if details["score"] != 0.97 {
		t.Errorf("score = %v, want 0.97", details["score"])
	}
}
