package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/huynle/brain-api/internal/types"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// If encoding fails, we can't do much — headers are already sent.
		http.Error(w, `{"error":"Internal Server Error","message":"failed to encode response"}`, http.StatusInternalServerError)
	}
}

// WriteError writes a consistent JSON error response.
func WriteError(w http.ResponseWriter, status int, errType, message string) {
	WriteJSON(w, status, types.ErrorResponse{
		Error:   errType,
		Message: message,
	})
}

// WriteValidationError writes a 400 response with field-level validation details.
func WriteValidationError(w http.ResponseWriter, details []types.ValidationDetail) {
	WriteJSON(w, http.StatusBadRequest, types.ErrorResponse{
		Error:   "Validation Error",
		Message: "Invalid request",
		Details: details,
	})
}

// fieldValidationError is implemented by service errors that reject one named
// request field, such as an automation's trigger.schedule.
type fieldValidationError interface {
	ValidationDetail() types.ValidationDetail
}

// writeFieldValidationError answers 400 with the rejected field in the
// validation details when err names one, and reports whether it did.
func writeFieldValidationError(w http.ResponseWriter, err error) bool {
	var fv fieldValidationError
	if !errors.As(err, &fv) {
		return false
	}
	WriteValidationError(w, []types.ValidationDetail{fv.ValidationDetail()})
	return true
}

// NotFoundHandler returns a 404 JSON response for unknown routes.
func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusNotFound, types.ErrorResponse{
			Error:   "Not Found",
			Message: fmt.Sprintf("Route %s %s not found", r.Method, r.URL.Path),
		})
	}
}

// MethodNotAllowedHandler returns a 405 JSON response.
func MethodNotAllowedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusMethodNotAllowed, types.ErrorResponse{
			Error:   "Method Not Allowed",
			Message: fmt.Sprintf("Method %s not allowed for %s", r.Method, r.URL.Path),
		})
	}
}

// notImplemented returns a 501 handler for routes not yet implemented.
func notImplemented(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusNotImplemented, types.ErrorResponse{
		Error:   "Not Implemented",
		Message: fmt.Sprintf("Route %s %s is not yet implemented", r.Method, r.URL.Path),
	})
}
