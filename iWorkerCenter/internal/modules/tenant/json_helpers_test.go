package tenant

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDecodeLimitedJSONRejectsOversizedBody(t *testing.T) {
	var input struct {
		Name string `json:"name"`
	}
	err := decodeLimitedJSON(strings.NewReader(`{"name":"center"}`), &input, 8, false)
	if !errors.Is(err, errJSONBodyTooLarge) {
		t.Fatalf("error = %v, want errJSONBodyTooLarge", err)
	}
}

func TestDecodeLimitedJSONRejectsTrailingDocument(t *testing.T) {
	var input struct {
		Name string `json:"name"`
	}
	err := decodeLimitedJSON(strings.NewReader(`{"name":"a"} {"name":"b"}`), &input, adminJSONBodyLimit, false)
	if !errors.Is(err, errJSONTrailingData) {
		t.Fatalf("error = %v, want errJSONTrailingData", err)
	}
}

func TestDecodeLimitedJSONAllowsEmptyBodyWhenRequested(t *testing.T) {
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeLimitedJSON(strings.NewReader(`   `), &input, adminJSONBodyLimit, true); err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
}

func TestDecodeLimitedJSONRejectsEmptyBodyByDefault(t *testing.T) {
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeLimitedJSON(strings.NewReader(`   `), &input, adminJSONBodyLimit, false); !errors.Is(err, io.EOF) {
		t.Fatalf("error = %v, want EOF", err)
	}
}
