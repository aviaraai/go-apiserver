// Package httperr defines the body every failed API response carries.
//
// It exists as its own package so handlers and the central error handler can
// both name the type without importing each other.
package httperr

// Response is the JSON body of a failed request.
//
// Message is display copy and may be reworded or translated at any time, so
// clients must never branch on it. Code is the stable identifier to switch on.
// Details carries whatever machine-readable data the code implies — for a
// DUPLICATE_ANIMAL it is the godhaar_id of the existing registration, which is
// the whole reason a client is told about the collision at all.
type Response struct {
	Message string         `json:"message"`
	Code    string         `json:"code,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

// Error lets a Response be used as an echo.HTTPError message while still being
// a valid error value.
func (r Response) Error() string { return r.Message }
