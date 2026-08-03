package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

// newPublishTestAuth builds an AuthService on an in-memory skillmarket store
// and returns it together with a valid session token.
func newPublishTestAuth(t *testing.T) (*skillmarket.AuthService, string) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	authSvc := skillmarket.NewAuthService(store, nil, "")
	session, err := authSvc.CreateSessionForUser(context.Background(), "publisher-1", "publisher@example.test")
	if err != nil {
		t.Fatal(err)
	}
	return authSvc, session.Token
}

func TestSkillHandlersPublishSkillRequiresSessionToken(t *testing.T) {
	store := skill.NewSkillStore(t.TempDir())
	authSvc, token := newPublishTestAuth(t)
	h := NewSkillHandlers(store, nil, authSvc)
	body := strings.NewReader(`{"id":"auth-required","name":"Auth Required"}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skills", body)
	h.PublishSkill(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/skills", strings.NewReader(`{"id":"auth-required","name":"Auth Required"}`))
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	h.PublishSkill(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// The valid token must still authenticate (sanity check for the helper).
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/skills", strings.NewReader(`{"id":"auth-required","name":"Auth Required"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	h.PublishSkill(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid token: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSkillHandlersPublishSkillDowngradesSelfReportedTrustLevel(t *testing.T) {
	store := skill.NewSkillStore(t.TempDir())
	authSvc, token := newPublishTestAuth(t)
	h := NewSkillHandlers(store, nil, authSvc)

	publish := func(id, trustLevel string) string {
		payload, err := json.Marshal(skill.HubSkillFull{HubSkillMeta: skill.HubSkillMeta{ID: id, Name: id, TrustLevel: trustLevel}})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/skills", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		h.PublishSkill(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("publish %s: status=%d body=%s", id, rec.Code, rec.Body.String())
		}
		stored, err := store.Get(id)
		if err != nil {
			t.Fatalf("published skill %s not stored: %v", id, err)
		}
		return stored.TrustLevel
	}

	for _, elevated := range []string{"trusted", "builtin", "official", ""} {
		id := "tl-" + elevated
		if elevated == "" {
			id = "tl-empty"
		}
		if got := publish(id, elevated); got != "community" {
			t.Fatalf("self-reported trust_level %q stored as %q, want community", elevated, got)
		}
	}
	for _, kept := range []string{"community", "agent-created"} {
		if got := publish("tl-"+kept, kept); got != kept {
			t.Fatalf("trust_level %q stored as %q, want %q", kept, got, kept)
		}
	}
}

func TestSkillHandlersPublishSkillAcceptsBundledSkillJSONAboveOneMB(t *testing.T) {
	store := skill.NewSkillStore(t.TempDir())
	authSvc, token := newPublishTestAuth(t)
	h := NewSkillHandlers(store, nil, authSvc)
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
	req.Header.Set("Authorization", "Bearer "+token)
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
	authSvc, token := newPublishTestAuth(t)
	h := NewSkillHandlers(store, nil, authSvc)
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
	req.Header.Set("Authorization", "Bearer "+token)
	h.PublishSkill(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDownloadBySkillIDReturnsCurrentMixedIdentityRevision(t *testing.T) {
	store := skill.NewSkillStore(t.TempDir())
	for _, item := range []skill.HubSkillFull{
		{HubSkillMeta: skill.HubSkillMeta{ID: "legacy-v2", Name: "PDF Translator", ProductKind: "maclaw_app_skill", IsMaclawApp: true, Fingerprint: "author@example.com:PDF Translator", Version: "2", Visible: true, UpdatedAt: "2026-07-20T00:00:00Z"}},
		{HubSkillMeta: skill.HubSkillMeta{ID: "current-v3", SkillID: "paper.pdf-translator", Name: "PDF Translator", ProductKind: "maclaw_app_skill", IsMaclawApp: true, Fingerprint: "author@example.com:PDF Translator", Version: "3", Visible: true, UpdatedAt: "2026-07-21T00:00:00Z"}},
	} {
		if err := store.Publish(item); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills/by-skill-id/paper.pdf-translator/download", nil)
	req.SetPathValue("skill_id", "paper.pdf-translator")
	NewSkillHandlers(store, nil, nil).DownloadBySkillID(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got skill.HubSkillFull
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != "current-v3" {
		t.Fatalf("downloaded id=%q, want current-v3", got.ID)
	}
}
