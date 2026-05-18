package agentservice

import (
	"strings"
	"testing"
)

func TestReadBoundedJSONResponseReturnsExplicitLimitError(t *testing.T) {
	_, err := readBoundedJSONResponse(strings.NewReader(strings.Repeat("x", 33)), 32)
	if err == nil {
		t.Fatal("expected limit error")
	}
	if !strings.Contains(err.Error(), "response exceeds 32 bytes") {
		t.Fatalf("error = %v, want explicit limit error", err)
	}
}

func TestReadBoundedJSONResponseAllowsExactLimit(t *testing.T) {
	body := strings.Repeat("x", 32)
	data, err := readBoundedJSONResponse(strings.NewReader(body), 32)
	if err != nil {
		t.Fatalf("readBoundedJSONResponse: %v", err)
	}
	if string(data) != body {
		t.Fatalf("data = %q, want %q", string(data), body)
	}
}
