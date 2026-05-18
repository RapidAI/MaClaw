package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

func TestSkillHandlersPublishSkillAcceptsBundledSkillJSONAboveOneMB(t *testing.T) {
	store := skill.NewSkillStore(t.TempDir())
	h := NewSkillHandlers(store, nil)
	payload := skill.HubSkillFull{
		HubSkillMeta: skill.HubSkillMeta{ID: "large-json-skill", Name: "Large JSON Skill"},
		Files: map[string]string{
			"data.txt": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 900*1024))),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if len(body) <= 1<<20 {
		t.Fatalf("test payload is %d bytes, want larger than old 1MB limit", len(body))
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills", bytes.NewReader(body))
	h.PublishSkill(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := store.Get("large-json-skill"); err != nil {
		t.Fatalf("published skill not stored: %v", err)
	}
}

func TestSkillHandlersPublishSkillRejectsBodyAboveLimit(t *testing.T) {
	store := skill.NewSkillStore(t.TempDir())
	h := NewSkillHandlers(store, nil)
	body := []byte(`{"id":"too-large","name":"Too Large","files":{"data.txt":"` + strings.Repeat("a", maxSkillPublishJSONBytes) + `"}}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills", bytes.NewReader(body))
	h.PublishSkill(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
