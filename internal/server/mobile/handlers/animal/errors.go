package animal

import (
	"errors"
	"fmt"
	"net/http"

	"go-api-server/internal/httperr"
	"go-api-server/internal/inference"

	"github.com/labstack/echo/v4"
)

// userFacing is the response we are willing to give an end user for a domain
// verdict: our own status and our own wording, keyed by the inference code.
//
// The copy is written here rather than passed through from the inference
// server on purpose. Upstream prose is not translatable, changes without
// warning, cannot be switched on by the mobile app, and is an uncontrolled
// string arriving from another service. The app should branch on the code; the
// message is only a fallback for display.
type userFacing struct {
	status  int
	message string
}

var domainResponses = map[inference.Code]userFacing{
	inference.CodeDuplicateAnimal: {
		http.StatusConflict,
		"This animal is already registered. Look up the existing record instead of registering it again.",
	},
	inference.CodeNoAnimalDetected: {
		http.StatusUnprocessableEntity,
		"No animal could be found in the photos. Step back so the whole animal is in frame and take them again.",
	},
	inference.CodeImageTooBlurry: {
		http.StatusUnprocessableEntity,
		"The photos are too blurry to use. Hold the camera steady, wait for it to focus, and take them again.",
	},
	inference.CodeImageBadExposure: {
		http.StatusUnprocessableEntity,
		"The photos are too dark or too bright to use. Move out of direct sun or harsh shade and take them again.",
	},
	inference.CodeImageTooSmall: {
		http.StatusUnprocessableEntity,
		"The photos are too small to use. Check the camera quality setting in the app and take them again.",
	},
	inference.CodeImageUnreadable: {
		http.StatusUnprocessableEntity,
		"One of the photos could not be opened. Take it again.",
	},
	inference.CodeBodyColorInconsistent: {
		http.StatusUnprocessableEntity,
		"The two front photos do not agree on the animal's colour. Make sure both photos are of the same animal, in even light, and take them again.",
	},
	inference.CodeMuzzleColorInconsistent: {
		http.StatusUnprocessableEntity,
		"The muzzle photos do not agree with each other. Make sure all three are of the same muzzle, in even light, and take them again.",
	},
	inference.CodePoorImageQuality: {
		http.StatusUnprocessableEntity,
		"The photos are not clear enough to use. Take them again in good light with the animal in focus.",
	},
}

// Non-domain failures get a generic message. The distinction between them
// matters to us, not to the user: transport means the service is down or
// unreachable and retrying may work, contract means the two services disagree
// about the protocol and retrying will not help until someone deploys a fix.
const (
	transportMessage = "The animal identification service is temporarily unavailable. Please try again in a few minutes."
	contractMessage  = "We could not complete this request. Our team has been notified — please try again later."
)

// localImageResponses is the copy for photos this server rejects before
// inference ever sees them.
var localImageResponses = map[string]string{
	codeImageUnreadable:        "One of the photos could not be opened. Take it again.",
	codeImageUnsupportedFormat: "Photos must be JPEG or PNG. Take them again using the app's camera.",
	codeImageTooLarge:          "One of the photos is too large. Lower the camera resolution and take it again.",
}

// imageRejectionHTTPError renders a locally-detected bad photo. The decoder's
// own message stays in SetInternal, where customHTTPErrorHandler logs it —
// what reaches the user is our copy plus the slot to retake.
func imageRejectionHTTPError(rej *imageRejection) *echo.HTTPError {
	message, ok := localImageResponses[rej.Code]
	if !ok {
		message = "One of the photos could not be used. Take it again."
	}

	return echo.NewHTTPError(http.StatusBadRequest, httperr.Response{
		Message: message,
		Code:    rej.Code,
		Details: map[string]any{
			"failed_images": []map[string]any{{"slot": rej.Slot, "code": rej.Code}},
		},
	}).SetInternal(rej.Internal)
}

// uploadHTTPError turns any error from reading the multipart form into a
// response. Anything that is not a recognised image rejection is ours, not the
// user's, so it gets a generic message and the real error goes to the log.
func uploadHTTPError(err error) *echo.HTTPError {
	if rej, ok := errors.AsType[*imageRejection](err); ok {
		return imageRejectionHTTPError(rej)
	}
	return echo.NewHTTPError(http.StatusInternalServerError, httperr.Response{
		Message: "We could not read the uploaded photos. Please try again.",
	}).SetInternal(err)
}

// inferenceHTTPError converts any error from the inference client into the
// response the caller should return, optionally carrying machine-readable
// details the client can act on. Nothing derived from the upstream body reaches
// the message; the full detail travels in SetInternal, where
// customHTTPErrorHandler unwraps it into the log.
//
// internalExtra carries non-fatal problems that happened while handling the
// failure — a debug capture that did not write, a duplicate we could not name.
// They ride along on the error so they are logged in the one place logging
// happens, instead of each site reaching for a logger of its own.
func inferenceHTTPError(infErr *inference.Error, details map[string]any, internalExtra error) *echo.HTTPError {
	status, message := http.StatusServiceUnavailable, transportMessage

	switch infErr.Class {
	case inference.ClassContract:
		status, message = http.StatusBadGateway, contractMessage
	case inference.ClassDomain:
		if resp, ok := domainResponses[infErr.Code]; ok {
			status, message = resp.status, resp.message
		} else {
			status, message = http.StatusBadGateway, contractMessage
		}
	}

	return echo.NewHTTPError(status, httperr.Response{
		Message: message,
		Code:    string(infErr.Code),
		Details: details,
	}).SetInternal(errors.Join(infErr, internalExtra))
}

func duplicateGodhaarID(infErr *inference.Error, faissToGodhaar map[int64]string) (string, error) {
	if infErr.Code != inference.CodeDuplicateAnimal {
		return "", nil
	}
	if infErr.Duplicate == nil {
		return "", errors.New("duplicate rejection carried no usable matched_faiss_id")
	}

	godhaarID, ok := faissToGodhaar[infErr.Duplicate.MatchedFaissID]
	if !ok {
		return "", fmt.Errorf(
			"duplicate matched faiss_id %d, which was not in the candidate set we sent",
			infErr.Duplicate.MatchedFaissID)
	}
	return godhaarID, nil
}

func registerErrorDetails(infErr *inference.Error, matchedGodhaarID string) map[string]any {
	details := searchErrorDetails(infErr)

	if matchedGodhaarID == "" {
		return details
	}
	if details == nil {
		details = map[string]any{}
	}
	details["godhaar_id"] = matchedGodhaarID
	if infErr.Duplicate != nil {
		details["score"] = infErr.Duplicate.TopScore
	}
	return details
}

// searchErrorDetails carries the per-photo breakdown, so the app can mark the
// bad images for retaking instead of making the user work out which of the five
// was at fault. Nil when the rejection was not per-image.
func searchErrorDetails(infErr *inference.Error) map[string]any {
	if infErr.Quality == nil || len(infErr.Quality.Failures) == 0 {
		return nil
	}

	slots := make([]map[string]any, 0, len(infErr.Quality.Failures))
	for _, f := range infErr.Quality.Failures {
		slots = append(slots, map[string]any{
			"slot": f.Slot,
			"code": string(f.ErrorCode),
		})
	}
	return map[string]any{"failed_images": slots}
}
