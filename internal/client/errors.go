package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// APIError represents a non-2xx response from the Rabbit backend.
type APIError struct {
	Status     int
	Method     string
	Path       string
	Code       string
	Message    string
	RawBody    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, e.Message)
	}
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.Status, http.StatusText(e.Status))
}

// IsNotFound reports whether the error is a 404 from the API.
func IsNotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.Status == http.StatusNotFound
}

// IsConflict reports whether the error is a 409 from the API.
func IsConflict(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.Status == http.StatusConflict
}

func newAPIError(resp *http.Response) *APIError {
	body, _ := io.ReadAll(resp.Body)
	out := &APIError{
		Status:  resp.StatusCode,
		Method:  resp.Request.Method,
		Path:    resp.Request.URL.Path,
		RawBody: string(body),
	}
	// Spring's default error JSON: {"timestamp":..,"status":..,"error":"...","message":"...","path":".."}
	var parsed struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		out.Code = parsed.Error
		out.Message = parsed.Message
	}
	return out
}
