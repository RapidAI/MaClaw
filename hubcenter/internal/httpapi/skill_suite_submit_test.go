package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"database/sql"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

func TestSubmitSkillSuitePublishesMembersAndSuite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sm, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: sm, SkillStore: skill.NewSkillStore(root), UserSvc: skillmarket.NewUserService(sm, nil)})
	body := map[string]any{"email": "suite@example.com", "suite": skill.SkillSuiteFull{SkillSuiteMeta: skill.SkillSuiteMeta{ID: "office-suite", Name: "Office Suite", Members: []skill.SkillSuiteMember{{SkillID: "pdf"}, {SkillID: "sheet"}}}, Skills: []skill.HubSkillFull{{HubSkillMeta: skill.HubSkillMeta{ID: "pdf", Name: "PDF"}}, {HubSkillMeta: skill.HubSkillMeta{ID: "sheet", Name: "Sheet"}}}}}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skill-suites/submit", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.SubmitSkillSuite(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := h.skillStore.GetSuite("office-suite"); err != nil {
		t.Fatalf("suite not persisted: %v", err)
	}
	if _, err := h.skillStore.Get("pdf"); err != nil {
		t.Fatalf("member not persisted: %v", err)
	}
}

func TestSuiteZipBytesIncludesMemberFiles(t *testing.T) {
	su := &skill.SkillSuiteFull{SkillSuiteMeta: skill.SkillSuiteMeta{ID: "demo", Name: "Demo"}, Skills: []skill.HubSkillFull{{HubSkillMeta: skill.HubSkillMeta{ID: "a", Name: "A"}, Files: map[string]string{"run.sh": "ZWNobyBoaQo="}}}}
	data, err := suiteZipBytes(su)
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range r.File {
		if f.Name == "skills/A/run.sh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("member file missing from suite zip")
	}
}
