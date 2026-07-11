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
	if testing.Short() {
		t.Skip("builds an over-limit JSON body (~MaxSkillPackageDownloadBytes)")
	}
	store := skill.NewSkillStore(t.TempDir())
	h := NewSkillHandlers(store, nil)
	// Valid JSON larger than the limit so MaxBytesReader trips during Decode
	// (invalid junk fails as "invalid JSON" before the byte limit is hit).
	prefix := []byte(`{"id":"too-large","name":"Too Large","files":{"data.txt":"`)
	suffix := []byte(`"}}`)
	pad := int(maxSkillPublishJSONBytes) - len(prefix) - len(suffix) + 1
	if pad < 1 {
		pad = 1
	}
	body := make([]byte, 0, len(prefix)+pad+len(suffix))
	body = append(body, prefix...)
	body = append(body, bytes.Repeat([]byte("A"), pad)...)
	body = append(body, suffix...)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills", bytes.NewReader(body))
	h.PublishSkill(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
