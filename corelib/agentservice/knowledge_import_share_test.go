package agentservice

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type oversizedSharePackageBody struct {
	remaining int64
}

func (r *oversizedSharePackageBody) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	for i := range p {
		p[i] = ' '
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

func TestDownloadSharePackageRejectsOversizedPackage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, &oversizedSharePackageBody{remaining: maxSharePackageJSONBytes + 1})
	}))
	defer server.Close()

	_, err := downloadSharePackage(context.Background(), server.URL, "")
	if err == nil || !strings.Contains(err.Error(), "knowledge package is too large") {
		t.Fatalf("expected oversized package error, got %v", err)
	}
}
