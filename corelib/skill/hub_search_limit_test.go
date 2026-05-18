package skill

import (
	"strings"
	"testing"
)

func TestReadBoundedHubJSONReturnsExplicitLimitError(t *testing.T) {
	_, err := readBoundedHubJSON(strings.NewReader(strings.Repeat("x", 33)), 32)
	if err == nil {
		t.Fatal("expected limit error")
	}
	if !strings.Contains(err.Error(), "hub response exceeds 32 bytes") {
		t.Fatalf("error = %v, want explicit limit error", err)
	}
}

func TestReadBoundedHubJSONAllowsExactLimit(t *testing.T) {
	body := strings.Repeat("x", 32)
	data, err := readBoundedHubJSON(strings.NewReader(body), 32)
	if err != nil {
		t.Fatalf("readBoundedHubJSON: %v", err)
	}
	if string(data) != body {
		t.Fatalf("data = %q, want %q", string(data), body)
	}
}
