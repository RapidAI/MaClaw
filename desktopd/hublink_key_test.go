package desktopd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHubLinkKeepsTheIntranetAddress(t *testing.T) {
	dir := t.TempDir()
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		posted, _ = body["direct_base"].(string)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"node_id":"node123","node_secret":"secsecsecsecsecsecsecsecsecsec12"}`))
	}))
	defer srv.Close()
	link := &HubLink{
		StatePath: filepath.Join(dir, "hub_link.json"),
		Key:       "primary-token-value-0123456789",
		Client:    srv.Client(),
	}
	status, err := link.EnrollWith(context.Background(), HubLinkEnroll{
		HubURL: srv.URL, Code: "CODE", DirectBase: "http://10.0.0.8:18081/extra/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if posted != "http://10.0.0.8:18081/extra" || status.DirectBase != posted {
		t.Fatalf("posted %q status %q", posted, status.DirectBase)
	}
	raw, err := os.ReadFile(link.StatePath)
	if err != nil || !strings.Contains(string(raw), `"direct_base": "http://10.0.0.8:18081/extra"`) {
		t.Fatalf("saved address missing: %v", err)
	}
	if _, err := link.EnrollWith(context.Background(), HubLinkEnroll{
		HubURL: srv.URL, Code: "CODE", DirectBase: "http://desktopd.abc.node.invalid",
	}); err == nil {
		t.Fatal("a virtual node host was accepted")
	}
	if again := link.Status().DirectBase; again != "http://10.0.0.8:18081/extra" {
		t.Fatalf("a rejected address replaced the saved one: %q", again)
	}
	if err := link.Leave(); err != nil {
		t.Fatal(err)
	}
}

func TestHubLinkSaveDirectBaseReplacesTheAddress(t *testing.T) {
	dir := t.TempDir()
	var gotBase, gotNode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/edge/enroll":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"node_id":"node123","node_secret":"secsecsecsecsecsecsecsecsecsec12"}`))
		case "/api/v1/edge/direct-base":
			if r.Header.Get("X-Node-Id") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			gotBase = body["direct_base"]
			gotNode = r.Header.Get("X-Node-Id")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	link := &HubLink{
		StatePath: filepath.Join(dir, "hub_link.json"),
		Key:       "primary-token-value-0123456789",
		Client:    srv.Client(),
	}
	if _, err := link.EnrollWith(context.Background(), HubLinkEnroll{
		HubURL: srv.URL, Code: "CODE", DirectBase: "http://10.0.0.8:18081",
	}); err != nil {
		t.Fatal(err)
	}
	saved, err := link.SaveDirectBase(context.Background(), "http://10.4.0.7:18081/")
	if err != nil || !saved {
		t.Fatalf("save saved=%v err=%v", saved, err)
	}
	if gotBase != "http://10.4.0.7:18081" || gotNode != "node123" || link.Status().DirectBase != gotBase {
		t.Fatalf("hub got %q node %q local %q", gotBase, gotNode, link.Status().DirectBase)
	}
	if _, err := link.SaveDirectBase(context.Background(), "http://desktopd.abc.node.invalid"); err == nil {
		t.Fatal("a virtual node host was saved")
	}
	if link.Status().DirectBase != "http://10.4.0.7:18081" {
		t.Fatalf("rejected address replaced the saved one: %q", link.Status().DirectBase)
	}
	if err := link.Leave(); err != nil {
		t.Fatal(err)
	}
}

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
