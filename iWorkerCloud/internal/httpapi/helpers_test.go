package httpapi

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONLimitAcceptsSingleJSONDocument(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"name":"center-a"}`))
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSONLimit(req, &input, 64); err != nil {
		t.Fatalf("decodeJSONLimit returned error: %v", err)
	}
	if input.Name != "center-a" {
		t.Fatalf("expected decoded name center-a, got %q", input.Name)
	}
}

func TestDecodeJSONLimitRejectsOversizedBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"name":"center-a"}`))
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSONLimit(req, &input, 8); !errors.Is(err, errJSONBodyTooLarge) {
		t.Fatalf("expected errJSONBodyTooLarge, got %v", err)
	}
}

func TestDecodeJSONLimitRejectsTrailingJSONDocument(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"name":"center-a"} {"name":"center-b"}`))
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSONLimit(req, &input, 64); !errors.Is(err, errJSONTrailingData) {
		t.Fatalf("expected errJSONTrailingData, got %v", err)
	}
}

func TestDecodeJSONLimitRejectsMalformedJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"name":`))
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSONLimit(req, &input, 64); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}
