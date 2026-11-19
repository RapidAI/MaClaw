package desktopd

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnrollFromEnvRegistersOnceAndHidesTheCode(t *testing.T) {
	const code = "ONCE-CODE-99"
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/edge/enroll" {
			hits++
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"node_id":"nodeenv1","node_secret":"secsecsecsecsecsecsecsecsecsec12","tenant_id":"tenant-a"}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	link := &HubLink{
		StatePath: filepath.Join(dir, "hub_link.json"),
		Key:       "primary-token-value-0123456789",
		Keys:      newAdminServer(nil, "primary-token-value-0123456789", dir, nil, nil, nil),
		Client:    srv.Client(),
		Self:      "127.0.0.1:9",
	}
	t.Setenv("DESKTOPD_HUB_URL", srv.URL)
	t.Setenv("DESKTOPD_HUB_ENROLL_CODE", code)
	link.EnrollFromEnv(context.Background())
	if hits != 1 {
		t.Fatalf("enroll calls = %d", hits)
	}
	if link.NodeID() != "nodeenv1" {
		t.Fatalf("node = %q", link.NodeID())
	}
	raw, err := os.ReadFile(link.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), code) {
		t.Fatal("enrollment code was stored")
	}
	link.EnrollFromEnv(context.Background())
	if hits != 1 {
		t.Fatalf("second start called enroll again: %d", hits)
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	failed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad code "+code, http.StatusBadRequest)
	}))
	defer failed.Close()
	fresh := &HubLink{
		StatePath: filepath.Join(t.TempDir(), "hub_link.json"),
		Key:       "primary-token-value-0123456789",
		Client:    failed.Client(),
		Self:      "127.0.0.1:9",
	}
	t.Setenv("DESKTOPD_HUB_URL", failed.URL)
	fresh.EnrollFromEnv(context.Background())
	if strings.Contains(buf.String(), code) {
		t.Fatalf("code leaked into the log: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "<redacted>") {
		t.Fatalf("failure was not recorded: %s", buf.String())
	}
}

func TestEnrollFromEnvSkipsWithoutBothValues(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()
	link := &HubLink{Client: srv.Client(), Self: "127.0.0.1:9"}
	t.Setenv("DESKTOPD_HUB_URL", srv.URL)
	t.Setenv("DESKTOPD_HUB_ENROLL_CODE", "")
	link.EnrollFromEnv(context.Background())
	if hits != 0 {
		t.Fatalf("enroll calls = %d", hits)
	}
}
