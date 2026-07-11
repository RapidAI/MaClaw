package agentservice

import (
	"strings"
	"testing"
)

func TestReadBoundedJSONResponseReturnsExplicitLimitError(t *testing.T) {
	_, err := readBoundedJSONResponse(strings.NewReader(strings.Repeat("x", 33)), -1, 32)
	if err == nil {
		t.Fatal("expected limit error")
	}
	if !strings.Contains(err.Error(), "client download limit") {
		t.Fatalf("error = %v, want explicit limit error", err)
	}
}

func TestReadBoundedJSONResponseFailsFastOnContentLength(t *testing.T) {
	_, err := readBoundedJSONResponse(strings.NewReader("tiny"), 100, 32)
	if err == nil {
		t.Fatal("expected content-length rejection")
	}
	if !strings.Contains(err.Error(), "client download limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestReadBoundedJSONResponseAllowsExactLimit(t *testing.T) {
	body := strings.Repeat("x", 32)
	data, err := readBoundedJSONResponse(strings.NewReader(body), int64(len(body)), 32)
	if err != nil {
		t.Fatalf("readBoundedJSONResponse: %v", err)
	}
	if string(data) != body {
		t.Fatalf("data = %q, want %q", string(data), body)
	}
}
