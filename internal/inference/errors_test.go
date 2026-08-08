package inference

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDecodeErrorBodyClassification(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantClass Class
		wantCode  Code
	}{
		{
			name:      "domain rejection carries an error code",
			status:    422,
			body:      `{"error_code":"POOR_IMAGE_QUALITY","detail":{"sharpness":0.12}}`,
			wantClass: ClassDomain,
			wantCode:  CodePoorImageQuality,
		},
		{
			name:      "duplicate is a domain verdict",
			status:    409,
			body:      `{"error_code":"DUPLICATE_ANIMAL","detail":{"faiss_id":42}}`,
			wantClass: ClassDomain,
			wantCode:  CodeDuplicateAnimal,
		},
		{
			// The case that motivated the whole taxonomy: FastAPI answers a
			// renamed or missing form field with a 422 that looks exactly like
			// an image rejection unless you check for error_code. Reporting
			// this as poor image quality would tell farmers to retake usable
			// photos while a deploy mismatch stayed invisible.
			name:      "fastapi request validation is a contract fault, not an image complaint",
			status:    422,
			body:      `{"detail":[{"loc":["body","front"],"msg":"field required","type":"value_error.missing"}]}`,
			wantClass: ClassContract,
			wantCode:  CodeContract,
		},
		{
			name:      "unknown error code degrades to contract",
			status:    422,
			body:      `{"error_code":"SOMETHING_NEW","detail":{}}`,
			wantClass: ClassContract,
			wantCode:  CodeContract,
		},
		{
			name:      "non-json body is a contract fault",
			status:    404,
			body:      `<html>404 Not Found</html>`,
			wantClass: ClassContract,
			wantCode:  CodeContract,
		},
		{
			name:      "upstream 500 is transport",
			status:    500,
			body:      `{"error_code":"POOR_IMAGE_QUALITY"}`,
			wantClass: ClassTransport,
			wantCode:  CodeInternal,
		},
		{
			name:      "bad gateway from a proxy is transport",
			status:    502,
			body:      `upstream connect error`,
			wantClass: ClassTransport,
			wantCode:  CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeErrorBody(tt.status, []byte(tt.body))
			if got.Class != tt.wantClass {
				t.Errorf("class = %v, want %v", got.Class, tt.wantClass)
			}
			if got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tt.wantCode)
			}
			if got.StatusCode != tt.status {
				t.Errorf("status = %d, want %d", got.StatusCode, tt.status)
			}
		})
	}
}

// The bodies below are copied verbatim from the inference server's own
// serialisation — schema.py's DuplicateDetail / ImageQualityDetail rendered
// through errors.py's domain_error_handler — so this is a real contract check
// rather than a restatement of what this package already believes.
func TestDecodeTypedDetail(t *testing.T) {
	t.Run("duplicate carries the matched animal", func(t *testing.T) {
		body := `{"error_code":"DUPLICATE_ANIMAL","detail":{"matched_faiss_id":42,` +
			`"top_score":0.97,"body_color":"BLACK","muzzle_color":"PINK"}}`

		got := decodeErrorBody(409, []byte(body))
		if got.Class != ClassDomain || got.Code != CodeDuplicateAnimal {
			t.Fatalf("class/code = %v/%q, want domain/%s", got.Class, got.Code, CodeDuplicateAnimal)
		}
		if got.Duplicate == nil {
			t.Fatal("Duplicate not parsed — the godhaar_id lookup depends on it")
		}
		if got.Duplicate.MatchedFaissID != 42 {
			t.Errorf("MatchedFaissID = %d, want 42", got.Duplicate.MatchedFaissID)
		}
		if got.Duplicate.TopScore != 0.97 {
			t.Errorf("TopScore = %v, want 0.97", got.Duplicate.TopScore)
		}
		// The raw detail must survive alongside the typed view: it is what
		// gets recorded against the failure row.
		if got.Detail["matched_faiss_id"] == nil {
			t.Error("raw Detail should still hold the payload for the failure record")
		}
	})

	t.Run("mixed image failures keep their per-photo codes", func(t *testing.T) {
		body := `{"error_code":"POOR_IMAGE_QUALITY","detail":{"message":"muzzle_1: bad_quality blur=12.30",` +
			`"failures":[{"slot":"muzzle_1","stage":"quality","error_code":"IMAGE_TOO_BLURRY",` +
			`"reason":"bad_quality blur=12.30"},{"slot":"muzzle_3","stage":"detection",` +
			`"error_code":"NO_ANIMAL_DETECTED","reason":"RECAPTURE_NO_DETECTION"}]}}`

		got := decodeErrorBody(422, []byte(body))
		if got.Code != CodePoorImageQuality {
			t.Fatalf("code = %q, want %s", got.Code, CodePoorImageQuality)
		}
		if got.Quality == nil || len(got.Quality.Failures) != 2 {
			t.Fatal("per-image failures not parsed")
		}
		if got.Quality.Failures[0].Slot != "muzzle_1" ||
			got.Quality.Failures[0].ErrorCode != CodeImageTooBlurry {
			t.Errorf("failure[0] = %+v, want muzzle_1/IMAGE_TOO_BLURRY", got.Quality.Failures[0])
		}
		if got.Quality.Failures[1].ErrorCode != CodeNoAnimalDetected {
			t.Errorf("failure[1] code = %q, want %s", got.Quality.Failures[1].ErrorCode, CodeNoAnimalDetected)
		}
	})

	t.Run("colour inconsistency carries both slots and what each one read", func(t *testing.T) {
		body := `{"error_code":"BODY_COLOR_INCONSISTENT","detail":{` +
			`"message":"body_color_inconsistent: front images disagree and neither is decisive (BLACK(0.42) vs WHITE(0.39))",` +
			`"failures":[{"slot":"front_1","stage":"color","error_code":"BODY_COLOR_INCONSISTENT","reason":"read BLACK at 0.42"},` +
			`{"slot":"front_2","stage":"color","error_code":"BODY_COLOR_INCONSISTENT","reason":"read WHITE at 0.39"}],` +
			`"readings":[{"slot":"front_1","label":"BLACK","confidence":0.42},` +
			`{"slot":"front_2","label":"WHITE","confidence":0.39}]}}`

		got := decodeErrorBody(422, []byte(body))
		if got.Class != ClassDomain || got.Code != CodeBodyColorInconsistent {
			t.Fatalf("class/code = %v/%q, want domain/%s", got.Class, got.Code, CodeBodyColorInconsistent)
		}
		// Both photos are named because neither one alone is the defect.
		if got.Quality == nil || len(got.Quality.Failures) != 2 {
			t.Fatalf("per-photo failures not parsed: %+v", got.Quality)
		}
		// The readings are what make the verdict actionable — see ColorReading.
		if len(got.Quality.Readings) != 2 {
			t.Fatalf("readings not parsed: %+v", got.Quality.Readings)
		}
		if got.Quality.Readings[0].Label != "BLACK" || got.Quality.Readings[0].Confidence != 0.42 {
			t.Errorf("readings[0] = %+v, want front_1/BLACK/0.42", got.Quality.Readings[0])
		}
		if got.Quality.Readings[1].Slot != "front_2" || got.Quality.Readings[1].Label != "WHITE" {
			t.Errorf("readings[1] = %+v, want front_2/WHITE", got.Quality.Readings[1])
		}
	})

	// Several animals in frame needs the opposite instruction to "no animal
	// found", so it must survive as its own code rather than being folded in.
	t.Run("multi cattle keeps its own code", func(t *testing.T) {
		body := `{"error_code":"MULTI_CATTLE","detail":{"message":"muzzle_2: RECAPTURE_MULTI_CATTLE",` +
			`"failures":[{"slot":"muzzle_2","stage":"detection","error_code":"MULTI_CATTLE",` +
			`"reason":"RECAPTURE_MULTI_CATTLE"}],"readings":[]}}`

		got := decodeErrorBody(422, []byte(body))
		if got.Class != ClassDomain || got.Code != CodeMultiCattle {
			t.Fatalf("class/code = %v/%q, want domain/%s", got.Class, got.Code, CodeMultiCattle)
		}
		if got.Quality == nil || got.Quality.Failures[0].Slot != "muzzle_2" {
			t.Fatalf("per-photo failure not parsed: %+v", got.Quality)
		}
		if len(got.Quality.Readings) != 0 {
			t.Errorf("readings should be empty for a non-colour verdict, got %+v", got.Quality.Readings)
		}
	})

	// A payload that has drifted must not turn a sound verdict into a service
	// error — the rejection stands, only the typed convenience is lost.
	t.Run("unusable detail degrades without losing the verdict", func(t *testing.T) {
		for _, body := range []string{
			`{"error_code":"DUPLICATE_ANIMAL","detail":{"matched_faiss_id":0,"top_score":0.9}}`,
			`{"error_code":"DUPLICATE_ANIMAL","detail":{"renamed_field":42}}`,
			`{"error_code":"DUPLICATE_ANIMAL","detail":"a bare string"}`,
		} {
			got := decodeErrorBody(409, []byte(body))
			if got.Class != ClassDomain || got.Code != CodeDuplicateAnimal {
				t.Errorf("body %s: class/code = %v/%q, want the verdict preserved",
					body, got.Class, got.Code)
			}
			if got.Duplicate != nil {
				t.Errorf("body %s: Duplicate = %+v, want nil", body, got.Duplicate)
			}
		}
	})
}

// The Error string is what leaks if a caller ever hands it to an HTTP client,
// so it must stay free of upstream bodies and prose by construction.
func TestErrorStringExcludesUpstreamDetail(t *testing.T) {
	secrets := []string{
		`{"detail":"internal traceback at /srv/app/main.py"}`,
		`Post "http://inference.internal:8000/register": dial tcp 10.1.2.3:8000: connect: connection refused`,
	}

	for _, secret := range secrets {
		errs := []*Error{
			decodeErrorBody(422, []byte(secret)),
			transportError(CodeUnavailable, 0, errors.New(secret)),
			contractError(500, errors.New(secret)),
		}
		for _, e := range errs {
			if strings.Contains(e.Error(), secret) {
				t.Errorf("Error() leaked upstream detail: %q", e.Error())
			}
			if e.RawError == nil || !strings.Contains(e.RawError.Error(), secret) {
				t.Errorf("RawError should retain the detail for logging, got %v", e.RawError)
			}
		}
	}
}

func TestErrorUnwrapsToClassSentinel(t *testing.T) {
	domain := decodeErrorBody(409, []byte(`{"error_code":"DUPLICATE_ANIMAL"}`))
	if !errors.Is(domain, ErrDomain) {
		t.Error("domain error should match ErrDomain")
	}
	if errors.Is(domain, ErrContract) || errors.Is(domain, ErrTransport) {
		t.Error("domain error matched the wrong sentinel")
	}

	contract := decodeErrorBody(422, []byte(`{"detail":[]}`))
	if !errors.Is(contract, ErrContract) {
		t.Error("contract error should match ErrContract")
	}
}

// Classify must be total: anything that is not already an *Error is assumed to
// be a transport fault rather than something safe to show a user.
func TestClassifyCoercesForeignErrors(t *testing.T) {
	got := Classify(errors.New("something unexpected"))
	if got.Class != ClassTransport || got.Code != CodeUnavailable {
		t.Errorf("got class %v code %q, want transport/%s", got.Class, got.Code, CodeUnavailable)
	}
	if Classify(nil) != nil {
		t.Error("Classify(nil) should be nil")
	}
}

// An empty JSON object unmarshals without error, so validation is the only
// thing standing between a malformed 200 and the database.
func TestValidateRegisterResponse(t *testing.T) {
	valid := `{"embedding_ids":[1,2,3],"extracted_colors":{"body":{"label":"brown"},"muzzle":{"label":"black"}}}`

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"valid", valid, false},
		{"empty object", `{}`, true},
		{"too few embeddings", `{"embedding_ids":[1,2],"extracted_colors":{"body":{"label":"brown"},"muzzle":{"label":"black"}}}`, true},
		{"zero embedding id", `{"embedding_ids":[1,0,3],"extracted_colors":{"body":{"label":"brown"},"muzzle":{"label":"black"}}}`, true},
		{"missing colour label", `{"embedding_ids":[1,2,3],"extracted_colors":{"body":{"label":""},"muzzle":{"label":"black"}}}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r RegisterResponse
			if err := json.Unmarshal([]byte(tt.body), &r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := validateRegisterResponse(&r); (err != nil) != tt.wantErr {
				t.Errorf("validateRegisterResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSearchResponse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "match found",
			body: `{"query_colors":{"body":{"label":"brown"},"muzzle":{"label":"black"}},"top_matches":[{"faiss_id":7,"score":0.9}]}`,
		},
		{
			// A real "nothing scored well enough" answer, which must not be
			// mistaken for a broken response.
			name: "no matches is a legitimate answer",
			body: `{"query_colors":{"body":{"label":"brown"},"muzzle":{"label":"black"}},"top_matches":[]}`,
		},
		{
			// Without this check an empty body would decode cleanly and be
			// reported as an UNKNOWN verdict, indistinguishable from a genuine
			// no-match.
			name:    "empty object",
			body:    `{}`,
			wantErr: true,
		},
		{
			name:    "zero faiss id",
			body:    `{"query_colors":{"body":{"label":"brown"},"muzzle":{"label":"black"}},"top_matches":[{"faiss_id":0,"score":0.9}]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r SearchResponse
			if err := json.Unmarshal([]byte(tt.body), &r); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if err := validateSearchResponse(&r); (err != nil) != tt.wantErr {
				t.Errorf("validateSearchResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
