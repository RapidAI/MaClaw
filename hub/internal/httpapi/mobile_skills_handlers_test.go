package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMobileAgentSkillsHandlerListsSeededSkills(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-skills@example.com")
	root := t.TempDir()
	initMobileCoreAgentForTest(t, root)

	// Place a market JSON skill under hub data/skills sibling path.
	seedDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillJSON := `{"id":"seed-demo","name":"种子演示","description":"test skill","version":"1.0.0","steps":[{"action":"send_input","params":{"text":"hi"}}]}`
	if err := os.WriteFile(filepath.Join(seedDir, "seed-demo.json"), []byte(skillJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/skills", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileAgentSkillsHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// Seed path uses parent of mobile-agent; InitMobileCoreAgent(root) => mobile-agent under root,
	// parent is root, so seedDir=root/skills is scanned.
	count, _ := body["count"].(float64)
	if count < 1 {
		t.Fatalf("expected seeded skill, body=%#v", body)
	}
	skills, _ := body["skills"].([]any)
	found := false
	for _, raw := range skills {
		item, _ := raw.(map[string]any)
		if item["name"] == "种子演示" {
			found = true
			if item["type"] == "" {
				t.Fatalf("skill type empty: %#v", item)
			}
			break
		}
	}
	if !found {
		t.Fatalf("seed-demo skill missing, body=%#v", body)
	}
}

func TestMobileAgentSkillsHandlerReseed(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-skills-reseed@example.com")
	root := t.TempDir()
	initMobileCoreAgentForTest(t, root)

	// First GET seeds empty (no seed files yet) and writes marker.
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/agent/skills", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileAgentSkillsHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}

	seedDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillJSON := `{"id":"reseed-demo","name":"重播演示","description":"after reseed","version":"2.0.0","steps":[{"action":"send_input","params":{"text":"yo"}}]}`
	if err := os.WriteFile(filepath.Join(seedDir, "reseed-demo.json"), []byte(skillJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// POST reseed clears marker and picks up new seed file.
	reseedReq := httptest.NewRequest(http.MethodPost, "/api/mobile/agent/skills/reseed", nil)
	reseedReq.Header.Set("Authorization", "Bearer "+token)
	reseedRec := httptest.NewRecorder()
	MobileAgentSkillsHandler(identity).ServeHTTP(reseedRec, reseedReq)
	if reseedRec.Code != http.StatusOK {
		t.Fatalf("reseed status=%d body=%s", reseedRec.Code, reseedRec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(reseedRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	count, _ := body["count"].(float64)
	if count < 1 {
		t.Fatalf("expected skill after reseed, body=%#v", body)
	}
	skills, _ := body["skills"].([]any)
	found := false
	for _, raw := range skills {
		item, _ := raw.(map[string]any)
		if item["name"] == "重播演示" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reseed skill missing, body=%#v", body)
	}
}
