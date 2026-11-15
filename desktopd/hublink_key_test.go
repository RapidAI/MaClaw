package desktopd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestHubLinkEnrollReportsExtraKeyNotPrimary(t *testing.T) {
	dir := t.TempDir()
	const primary = "primary-token-value-0123456789"
	admin := newAdminServer(nil, primary, dir, nil, nil, nil)
	if err := admin.saveTokens([]AdminToken{{
		ID: "panel", Label: "operator", Token: "operator-token-0123456789", CreatedAt: "t",
	}}); err != nil {
		t.Fatal(err)
	}
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		posted, _ = body["desktopd_key"].(string)
		if strings.Contains(posted, primary) {
			http.Error(w, "primary token was sent", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"node_id":"node123","node_secret":"secsecsecsecsecsecsecsecsecsec12"}`))
	}))
	defer srv.Close()
	link := &HubLink{
		StatePath: filepath.Join(dir, "hub_link.json"),
		Key:       primary,
		Keys:      admin,
		Client:    srv.Client(),
		Self:      "127.0.0.1:9",
	}
	if _, err := link.EnrollWith(context.Background(), HubLinkEnroll{HubURL: srv.URL, Code: "CODE"}); err != nil {
		t.Fatal(err)
	}
	if posted == "" || posted == primary {
		t.Fatalf("posted key = %q", posted)
	}
	if !admin.tokenAccepted(posted) || !admin.tokenAccepted(primary) || !admin.tokenAccepted("operator-token-0123456789") {
		t.Fatal("hub key, primary, or panel key was not accepted")
	}
	tokens, err := admin.tokens()
	if err != nil {
		t.Fatal(err)
	}
	var hubLabel string
	for _, item := range tokens {
		if item.Token == posted {
			hubLabel = item.Label
		}
	}
	if hubLabel != "hub-node123" {
		t.Fatalf("hub key label = %q", hubLabel)
	}
	if err := link.Leave(); err != nil {
		t.Fatal(err)
	}
	if admin.tokenAccepted(posted) {
		t.Fatal("leaving the hub must drop the extra key")
	}
	if !admin.tokenAccepted(primary) || !admin.tokenAccepted("operator-token-0123456789") {
		t.Fatal("leaving the hub must keep the primary token and panel keys")
	}
}
