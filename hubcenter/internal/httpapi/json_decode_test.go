package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeLimitedJSONRejectsOversizedBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"`+strings.Repeat("x", 32)+`"}`))
	rr := httptest.NewRecorder()
	var dst struct {
		Value string `json:"value"`
	}

	err := decodeLimitedJSON(rr, req, &dst, 16)
	if !errors.Is(err, errRequestBodyTooLarge) {
		t.Fatalf("decodeLimitedJSON() error = %v, want errRequestBodyTooLarge", err)
	}
}

func TestDecodeLimitedJSONAcceptsSmallBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"ok"}`))
	rr := httptest.NewRecorder()
	var dst struct {
		Value string `json:"value"`
	}

	if err := decodeLimitedJSON(rr, req, &dst, defaultJSONBodyLimit); err != nil {
		t.Fatalf("decodeLimitedJSON() error = %v", err)
	}
	if dst.Value != "ok" {
		t.Fatalf("decoded value = %q, want ok", dst.Value)
	}
}

func TestDecodeLimitedJSONRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"ok"}{"value":"extra"}`))
	rr := httptest.NewRecorder()
	var dst struct {
		Value string `json:"value"`
	}

	if err := decodeLimitedJSON(rr, req, &dst, defaultJSONBodyLimit); err == nil {
		t.Fatal("decodeLimitedJSON() error = nil, want trailing JSON error")
	}
}

func TestDecodeOptionalLimitedJSONAllowsEmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
	rr := httptest.NewRecorder()
	var dst struct {
		Value string `json:"value"`
	}

	if err := decodeOptionalLimitedJSON(rr, req, &dst, defaultJSONBodyLimit); err != nil {
		t.Fatalf("decodeOptionalLimitedJSON() error = %v", err)
	}
	if dst.Value != "" {
		t.Fatalf("decoded value = %q, want empty", dst.Value)
	}
}

func TestDecodeOptionalLimitedJSONRejectsOversizedBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"value":"`+strings.Repeat("x", 32)+`"}`))
	rr := httptest.NewRecorder()
	var dst struct {
		Value string `json:"value"`
	}

	err := decodeOptionalLimitedJSON(rr, req, &dst, 16)
	if !errors.Is(err, errRequestBodyTooLarge) {
		t.Fatalf("decodeOptionalLimitedJSON() error = %v, want errRequestBodyTooLarge", err)
	}
}
