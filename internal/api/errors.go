package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Error is a non-2xx response from the platform.
type Error struct {
	StatusCode int
	Message    string // server-provided message, possibly empty
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if hint := e.Hint(); hint != "" {
		return fmt.Sprintf("api: %d %s (%s)", e.StatusCode, msg, hint)
	}
	return fmt.Sprintf("api: %d %s", e.StatusCode, msg)
}

// Hint explains the most common failures in user terms.
func (e *Error) Hint() string {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return "the API token is missing, invalid or expired"
	case http.StatusPaymentRequired:
		return "log management is not enabled for this organization"
	case http.StatusForbidden:
		return "the API token has no access to this organization or resource"
	case http.StatusNotFound:
		return "resource not found or feature not enabled for this organization"
	case http.StatusRequestEntityTooLarge:
		return "aggregation interval too fine for the time range"
	case http.StatusTooManyRequests:
		return "rate limited, retry later"
	default:
		return ""
	}
}

// StatusCode returns the HTTP status of err if it is (or wraps) an *Error, or 0.
func StatusCode(err error) int {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

func newAPIError(resp *http.Response) *Error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBytes))
	e := &Error{StatusCode: resp.StatusCode}
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &payload) == nil && payload.Message != "" {
		e.Message = payload.Message
	} else {
		e.Message = strings.TrimSpace(string(b))
	}
	if len(e.Message) > 300 {
		e.Message = e.Message[:300] + "…"
	}
	return e
}
