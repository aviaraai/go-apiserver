package inference

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Class is the coarse category every inference failure falls into. It answers
// two questions at once: how much the caller may tell the end user, and who is
// expected to act on the failure.
type Class int

const (
	// ClassTransport — we never got a usable answer out of the inference
	// server. The request never left, the connection failed, or the server
	// itself errored. Nothing the end user did caused it and nothing they can
	// do fixes it, so they get a generic "try again" while we get the detail.
	ClassTransport Class = iota

	// ClassContract — the inference server answered, but the answer does not
	// fit the agreed contract: an unparseable body, a status we do not handle,
	// a required field that was missing, or an error code this build has never
	// heard of. That means the two services are out of step, which is a deploy
	// problem and not a user problem.
	//
	// This class exists specifically so that such failures are never reported
	// as image complaints. Doing so tells farmers to retake perfectly good
	// photos while the real fault stays invisible.
	ClassContract

	// ClassDomain — the inference server did its job and returned a verdict we
	// asked it for: a duplicate animal, an unusable photo. This is the only
	// class whose meaning is safe, and useful, to explain to the end user.
	ClassDomain
)

func (c Class) String() string {
	switch c {
	case ClassTransport:
		return "transport"
	case ClassContract:
		return "contract"
	case ClassDomain:
		return "domain"
	default:
		return "unknown"
	}
}

// Code identifies a specific failure. Codes are the contract between this
// service and the inference server, and they double as the error_code recorded
// in animal_identification_failures, so the same vocabulary spans the API
// response, the logs, and the failure dashboard.
type Code string

const (
	// Domain codes. The inference server emits these verbatim in the
	// "error_code" field of its error envelope (see decodeErrorBody). They
	// mirror schema.ErrorCode in the inference server — the two lists are one
	// contract and must be changed together.
	//
	// A code the server sends but this build does not know degrades to
	// ClassContract rather than being guessed at. That is the safe direction:
	// an unrecognised verdict surfaces as a service fault we get paged about,
	// instead of an invented instruction the farmer cannot act on. It also
	// means new codes can be deployed on either side in either order.
	CodeDuplicateAnimal  Code = "DUPLICATE_ANIMAL"
	CodeNoAnimalDetected Code = "NO_ANIMAL_DETECTED"

	// Single-image quality failures, split by cause because each one implies
	// different advice to whoever is holding the phone.
	CodeImageTooBlurry   Code = "IMAGE_TOO_BLURRY"
	CodeImageBadExposure Code = "IMAGE_BAD_EXPOSURE"
	CodeImageTooSmall    Code = "IMAGE_TOO_SMALL"
	CodeImageUnreadable  Code = "IMAGE_UNREADABLE"

	// Cross-image disagreement: each photo is fine on its own but together
	// they cannot describe one animal.
	CodeBodyColorInconsistent   Code = "BODY_COLOR_INCONSISTENT"
	CodeMuzzleColorInconsistent Code = "MUZZLE_COLOR_INCONSISTENT"

	// Umbrella the server uses when a set of images failed for more than one
	// distinct reason; the per-image codes survive in the detail.
	CodePoorImageQuality Code = "POOR_IMAGE_QUALITY"

	// Client-side codes. These are produced here, never by the inference
	// server, so that every failure carries a code worth grouping by.
	CodeUnavailable Code = "INFERENCE_UNAVAILABLE"
	CodeInternal    Code = "INFERENCE_INTERNAL"
	CodeContract    Code = "INFERENCE_CONTRACT"
)

// domainCodes is the allowlist of codes the inference server may assert. Any
// other value in an error envelope is treated as a contract violation.
var domainCodes = map[Code]bool{
	CodeDuplicateAnimal:         true,
	CodeNoAnimalDetected:        true,
	CodeImageTooBlurry:          true,
	CodeImageBadExposure:        true,
	CodeImageTooSmall:           true,
	CodeImageUnreadable:         true,
	CodeBodyColorInconsistent:   true,
	CodeMuzzleColorInconsistent: true,
	CodePoorImageQuality:        true,
}

// DuplicateDetail is the payload of a DUPLICATE_ANIMAL rejection.
//
// MatchedFaissID is the reason this is parsed rather than just logged: the
// inference server knows only FAISS ids, and we are the only side that can turn
// one back into a godhaar_id — which is what the user actually needs in order
// to go and look at the registration they are duplicating.
type DuplicateDetail struct {
	MatchedFaissID int64   `json:"matched_faiss_id"`
	TopScore       float64 `json:"top_score"`
	BodyColor      string  `json:"body_color"`
	MuzzleColor    string  `json:"muzzle_color"`
}

// ImageFailure is one photo's rejection, so the app can point at the image to
// retake instead of making the user guess which of the five was bad.
type ImageFailure struct {
	Slot      string `json:"slot"`
	Stage     string `json:"stage"`
	ErrorCode Code   `json:"error_code"`
	Reason    string `json:"reason"`
}

// ImageQualityDetail is the payload of any image-quality rejection.
type ImageQualityDetail struct {
	Message  string         `json:"message"`
	Failures []ImageFailure `json:"failures"`
}

// Class sentinels, for callers that want errors.Is over the whole category
// rather than a switch on Code.
var (
	ErrTransport = errors.New("inference service unavailable")
	ErrContract  = errors.New("inference response violated the expected contract")
	ErrDomain    = errors.New("inference rejected the request")
)

// Error is the only error type the client returns. Splitting the internal
// detail (RawError) from the classification means a caller cannot leak upstream
// hostnames, ports, stack traces or free-form server prose to an end user by
// accident — there is no field holding a message intended for display.
type Error struct {
	Class Class
	Code  Code

	// StatusCode is the upstream HTTP status, or 0 when no response was ever
	// received. It distinguishes "could not reach inference" from "inference
	// answered with a failure", which is the first thing worth knowing.
	StatusCode int

	// Detail is the structured detail the inference server supplied, kept as
	// received so it can be recorded whole against the failure row. It is
	// never rendered into a user-facing message.
	Detail map[string]any

	// Duplicate is set when Code is CodeDuplicateAnimal and the payload parsed.
	// Nil means the verdict stands but the matched animal could not be
	// identified from it — the rejection is still correct, we just cannot say
	// what it collides with.
	Duplicate *DuplicateDetail

	// Quality is set for image-quality rejections that carried a per-image
	// breakdown, so a caller can tell the user which photo to retake.
	Quality *ImageQualityDetail

	// RawError carries the full internal detail, including response bodies.
	// Logged, never sent to a client.
	RawError error
}

// Error deliberately reports only the classification. Everything sensitive
// lives in RawError, so even a caller that mistakenly hands this string to an
// HTTP client cannot spill upstream internals.
func (e *Error) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("inference %s failure (%s)", e.Class, e.Code)
	}
	return fmt.Sprintf("inference %s failure (%s, upstream status %d)", e.Class, e.Code, e.StatusCode)
}

func (e *Error) Unwrap() error {
	switch e.Class {
	case ClassDomain:
		return ErrDomain
	case ClassContract:
		return ErrContract
	default:
		return ErrTransport
	}
}

// Classify returns err as an *Error. The client only ever returns *Error, so
// the fallback exists to keep callers total: an error arriving from anywhere
// else is assumed to be a transport fault rather than something safe to show.
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var infErr *Error
	if errors.As(err, &infErr) {
		return infErr
	}
	return transportError(CodeUnavailable, 0, err)
}

func transportError(code Code, status int, raw error) *Error {
	return &Error{Class: ClassTransport, Code: code, StatusCode: status, RawError: raw}
}

func contractError(status int, raw error) *Error {
	return &Error{Class: ClassContract, Code: CodeContract, StatusCode: status, RawError: raw}
}

func domainError(code Code, status int, detail map[string]any, raw error) *Error {
	return &Error{Class: ClassDomain, Code: code, StatusCode: status, Detail: detail, RawError: raw}
}

// errorEnvelope is the failure shape the inference server is expected to return
// for every 4xx: a machine-readable code plus optional structured detail.
type errorEnvelope struct {
	ErrorCode Code            `json:"error_code"`
	Detail    json.RawMessage `json:"detail"`
}

// decodeErrorBody turns a non-2xx response into a classified error.
//
// Keying off error_code rather than the HTTP status is what keeps FastAPI's own
// request-validation failures from being mistaken for image-quality verdicts.
// Both arrive as 422, but only a deliberate domain rejection carries an
// error_code, so a validation failure — a renamed form field, a missing file —
// lands in ClassContract where it belongs.
func decodeErrorBody(status int, rawBody []byte) *Error {
	// 5xx is the inference server failing at its own job. There is no verdict
	// to read out of it, whatever the body says.
	if status >= http.StatusInternalServerError {
		return transportError(CodeInternal, status, fmt.Errorf("inference server error, body: %s", rawBody))
	}

	var env errorEnvelope
	if err := json.Unmarshal(rawBody, &env); err != nil {
		return contractError(status, fmt.Errorf("unparseable error body: %w, body: %s", err, rawBody))
	}
	if env.ErrorCode == "" {
		return contractError(status, fmt.Errorf("error body carried no error_code, body: %s", rawBody))
	}
	if !domainCodes[env.ErrorCode] {
		return contractError(status, fmt.Errorf("unrecognised error_code %q, body: %s", env.ErrorCode, rawBody))
	}

	// Detail is advisory. A shape we cannot read is not worth failing the
	// classification over — the verdict itself is already established, and
	// discarding it because an extra field moved would turn a correct rejection
	// into a service error.
	var detail map[string]any
	if len(env.Detail) > 0 {
		if err := json.Unmarshal(env.Detail, &detail); err != nil {
			detail = map[string]any{"unparsed_detail": string(env.Detail)}
		}
	}

	e := domainError(env.ErrorCode, status, detail,
		fmt.Errorf("inference rejected request: %s, body: %s", env.ErrorCode, rawBody))
	decodeTypedDetail(e, env.Detail)
	return e
}

// decodeTypedDetail parses the detail into whichever concrete shape the code
// implies. Failures are deliberately silent: the typed view is a convenience
// over Detail, which is already recorded verbatim, so a payload that has
// drifted degrades the response rather than rejecting a sound verdict.
func decodeTypedDetail(e *Error, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}

	switch e.Code {
	case CodeDuplicateAnimal:
		var d DuplicateDetail
		// A zero id is as useless as no id at all, and treating it as valid
		// would produce a lookup miss further up instead of an obvious gap.
		if err := json.Unmarshal(raw, &d); err == nil && d.MatchedFaissID != 0 {
			e.Duplicate = &d
		}
	case CodeNoAnimalDetected, CodeImageTooBlurry, CodeImageBadExposure,
		CodeImageTooSmall, CodeImageUnreadable, CodePoorImageQuality:
		var q ImageQualityDetail
		if err := json.Unmarshal(raw, &q); err == nil && len(q.Failures) > 0 {
			e.Quality = &q
		}
	}
}
