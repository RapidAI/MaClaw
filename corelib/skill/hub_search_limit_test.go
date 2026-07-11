package skill

import (
	"strings"
	"testing"
)

func TestReadBoundedHubJSONReturnsExplicitLimitError(t *testing.T) {
	_, err := readBoundedHubJSON(strings.NewReader(strings.Repeat("x", 33)), -1, 32)
	if err == nil {
		t.Fatal("expected limit error")
	}
	if !strings.Contains(err.Error(), "client download limit") {
		t.Fatalf("error = %v, want explicit limit error", err)
	}
}

func TestReadBoundedHubJSONFailsFastOnContentLength(t *testing.T) {
	err := CheckSkillPackageDownloadLimit(100, 32)
	if err == nil {
		t.Fatal("expected content-length rejection")
	}
	_, err = readBoundedHubJSON(strings.NewReader("tiny"), 100, 32)
	if err == nil {
		t.Fatal("expected content-length rejection before body read")
	}
}

func TestReadBoundedHubJSONAllowsExactLimit(t *testing.T) {
	body := strings.Repeat("x", 32)
	data, err := readBoundedHubJSON(strings.NewReader(body), int64(len(body)), 32)
	if err != nil {
		t.Fatalf("readBoundedHubJSON: %v", err)
	}
	if string(data) != body {
		t.Fatalf("data = %q, want %q", string(data), body)
	}
}
