// Package upstream explains a failed call to a service Hub depends on, such as
// MaClawSrv or a docker desktop service. It keeps the HTTP status and a short
// reason from the response body, so an admin sees why the request was rejected
// instead of a generic failure.
package upstream

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// StatusError is a response that was not 2xx. Base is the sentinel error of
// the calling package, so errors.Is still matches that package's failure.
type StatusError struct {
	Base   error
	Status int
	Detail string
}

// NewStatusError builds a StatusError from a failed response, keeping a short
// reason read from the body.
func NewStatusError(base error, status int, payload []byte) *StatusError {
	return &StatusError{Base: base, Status: status, Detail: Detail(payload)}
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%v: status %d", e.Base, e.Status)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

func (e *StatusError) Unwrap() error { return e.Base }

// Message turns a failed upstream call into text an admin can act on.
func Message(err error, base string) string {
	if err == nil {
		return base
	}
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		hint := ""
		switch statusErr.Status {
		case http.StatusUnauthorized:
			hint = "; the access token was rejected, re-save it"
		case http.StatusForbidden:
			hint = "; the token lacks permission for this operation"
		case http.StatusNotFound:
			hint = "; endpoint not found, check the service URL"
		}
		msg := fmt.Sprintf("%s (HTTP %d%s)", base, statusErr.Status, hint)
		if statusErr.Detail != "" {
			msg += ": " + statusErr.Detail
		}
		return msg
	}
	// Network, DNS, TLS or decode failure: keep the underlying reason.
	return base + ": " + oneLine(err.Error(), 300)
}

// Detail picks a short, single-line reason out of an error response body,
// e.g. {"error":"unauthorized"} -> "unauthorized".
func Detail(payload []byte) string {
	body := bytes.TrimSpace(payload)
	if len(body) == 0 {
		return ""
	}
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Error) != "" {
		return oneLine(parsed.Error, 200)
	}
	return oneLine(string(body), 200)
}

// oneLine collapses whitespace and caps the text. Truncation is rune-safe, and
// the scan is capped first because an error body can be megabytes.
func oneLine(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max*4 {
		runes = runes[:max*4]
	}
	flat := strings.Join(strings.Fields(string(runes)), " ")
	runes = []rune(flat)
	if len(runes) > max {
		return string(runes[:max]) + "..."
	}
	return flat
}
