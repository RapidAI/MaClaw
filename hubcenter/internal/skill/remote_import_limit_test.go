package skill

import (
	"strings"
	"testing"
)

func TestReadLimitedRemoteImportBodyReturnsExplicitLimitError(t *testing.T) {
	_, err := readLimitedRemoteImportBody(strings.NewReader(strings.Repeat("x", 33)), 32)
	if err == nil {
		t.Fatal("expected limit error")
	}
	if !strings.Contains(err.Error(), "remote import response exceeds 32 bytes") {
		t.Fatalf("error = %v, want explicit limit error", err)
	}
}

func TestReadLimitedRemoteImportBodyAllowsExactLimit(t *testing.T) {
	body := strings.Repeat("x", 32)
	data, err := readLimitedRemoteImportBody(strings.NewReader(body), 32)
	if err != nil {
		t.Fatalf("readLimitedRemoteImportBody: %v", err)
	}
	if string(data) != body {
		t.Fatalf("data = %q, want %q", string(data), body)
	}
}
