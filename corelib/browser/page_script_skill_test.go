package browser

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestPageScriptSkillRoundTrip(t *testing.T) {
	dir := t.TempDir()
	owner := "0123456789abcdef"
	script := `await click("保存"); await type("#q", "weather")`
	if _, err := SavePageScriptSkill(dir, owner, "Save Draft", script, "https://docs.example.com/a?token=1", "script ok"); err == nil {
		t.Fatal("an uppercase name was stored")
	}
	if _, err := SavePageScriptSkill(dir, owner, "save-draft", script, "https://docs.example.com/a?token=1", "cookie=secret"); err != nil {
		t.Fatal(err)
	}
	listed, err := ListPageScriptSkills(dir, owner)
	if err != nil || !strings.Contains(listed, "save-draft") || !strings.Contains(listed, "host=docs.example.com") || strings.Contains(listed, "cookie") || strings.Contains(listed, "token") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	loaded, err := LoadPageScriptSkill(dir, owner, "save-draft")
	if err != nil || loaded.Script != script || loaded.Host != "docs.example.com" || loaded.Summary != "" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	if err := PageScriptSkillHostAllowed(loaded.Host, "https://www.docs.example.com/other"); err != nil {
		t.Fatal(err)
	}
	if host := PageScriptSkillHost("https://www.docs.example.com/a"); host != "docs.example.com" {
		t.Fatalf("host=%s", host)
	}
	if err := PageScriptSkillHostAllowed(loaded.Host, "https://mail.example.com/"); err == nil {
		t.Fatal("a skill ran on another site")
	}
	if _, err := SavePageScriptSkill(dir, owner, "save-draft", `await click("发布")`, "https://docs.example.com/b", "script ok: clicked 发布"); err != nil {
		t.Fatal(err)
	}
	updated, err := LoadPageScriptSkill(dir, owner, "save-draft")
	if err != nil || updated.Script != `await click("发布")` {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	if _, err := SavePageScriptSkill(dir, owner, "anywhere", `await click("保存")`, "about:blank", ""); err == nil {
		t.Fatal("a skill without a website was stored")
	}
	if _, err := SavePageScriptSkill(dir, "", "save-draft", script, "https://docs.example.com/a", "ok"); err == nil {
		t.Fatal("a skill without an owner was stored")
	}
	if _, err := SavePageScriptSkill(dir, "gui-user", "save-draft", script, "https://docs.example.com/a", "script ok"); err != nil {
		t.Fatal(err)
	}
	desktop, err := LoadPageScriptSkill(dir, owner, "save-draft")
	if err != nil || desktop.Script != `await click("发布")` {
		t.Fatalf("gui owner overwrote the desktop skill: %#v err=%v", desktop, err)
	}
}

func TestPageScriptOwnerNamespace(t *testing.T) {
	dir := t.TempDir()
	script := `await click("保存")`
	desktopOwner := "0123456789abcdef"
	if _, err := SavePageScriptSkill(dir, desktopOwner, "save-draft", script, "https://docs.example.com/a", "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, desktopOwner, "save-draft.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := SavePageScriptSkill(dir, "gui-user", "save-draft", script, "https://docs.example.com/b", "ok"); err != nil {
		t.Fatal(err)
	}
	canon, err := canonicalPageScriptOwner("gui-user")
	if err != nil || canon == desktopOwner || !strings.HasPrefix(canon, "h") || len(canon) != 17 {
		t.Fatalf("canon=%s err=%v", canon, err)
	}
	again, err := canonicalPageScriptOwner(canon)
	if err != nil || again != canon {
		t.Fatalf("again=%s err=%v", again, err)
	}
	if _, err := os.Stat(filepath.Join(dir, canon, "save-draft.json")); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPageScriptSkill(dir, canon, "save-draft")
	if err != nil || loaded.Script != script || loaded.Host != "docs.example.com" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	desktop, err := LoadPageScriptSkill(dir, desktopOwner, "save-draft")
	if err != nil || desktop.Host != "docs.example.com" {
		t.Fatalf("desktop skill changed: %#v err=%v", desktop, err)
	}
	if _, err := SavePageScriptSkill(dir, "..", "save-draft", script, "https://docs.example.com/a", "ok"); err == nil {
		t.Fatal("owner .. was stored")
	}
	if _, err := NormalizePageScriptSkillName("  "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty name err=%v", err)
	}
}

func TestPageScriptSkillIgnoresStrayFilesAndTampering(t *testing.T) {
	dir := t.TempDir()
	owner := "0123456789abcdef"
	ownerDir := filepath.Join(dir, owner)
	if err := os.MkdirAll(ownerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < pageScriptSkillLimit; i++ {
		// Uppercase names are not skills and must not fill the quota.
		name := filepath.Join(ownerDir, "Note-"+strings.Repeat("0", 2)+string(rune('A'+i%26))+".json")
		if i >= 26 {
			name = filepath.Join(ownerDir, "Note-extra-"+string(rune('A'+i-26))+".json")
		}
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := `await click("保存")`
	if _, err := SavePageScriptSkill(dir, owner, "save-draft", script, "https://docs.example.com/a", "ok"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ownerDir, "save-draft.json")
	tampered := []byte("{\n  \"name\": \"save-draft\",\n  \"host\": \"docs.example.com\\ninjected\",\n  \"script\": \"await click(\\\"保存\\\")\"\n}\n")
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPageScriptSkill(dir, owner, "save-draft"); err == nil {
		t.Fatal("a host with a newline was loaded")
	}
	listed, err := ListPageScriptSkills(dir, owner)
	if err != nil || strings.Contains(listed, "injected") || strings.Contains(listed, "save-draft") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	secret := []byte("{\n  \"name\": \"save-draft\",\n  \"host\": \"docs.example.com\",\n  \"script\": \"fetch(\\\"https://example.com\\\")\"\n}\n")
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPageScriptSkill(dir, owner, "save-draft"); err == nil {
		t.Fatal("a script that calls the network was loaded")
	}
}

func TestPageScriptSkillRejectsLinks(t *testing.T) {
	dir := t.TempDir()
	owner := "0123456789abcdef"
	ownerDir := filepath.Join(dir, owner)
	if err := os.MkdirAll(ownerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"name":"save-draft","host":"docs.example.com","script":"await click(\"x\")"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ownerDir, "save-draft.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Log(err)
	} else {
		if _, err := LoadPageScriptSkill(dir, owner, "save-draft"); err == nil {
			t.Fatal("a symlink was loaded")
		}
		if _, err := SavePageScriptSkill(dir, owner, "save-draft", `await click("保存")`, "https://docs.example.com/a", "ok"); err == nil {
			t.Fatal("a symlink was overwritten")
		}
	}
	store := t.TempDir()
	target := t.TempDir()
	ownerLink := filepath.Join(store, owner)
	if err := os.Symlink(target, ownerLink); err != nil {
		if err := exec.Command("cmd", "/c", "mklink", "/J", ownerLink, target).Run(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := SavePageScriptSkill(store, owner, "other-draft", `await click("保存")`, "https://docs.example.com/a", "ok"); err == nil {
		t.Fatal("a linked owner directory was used")
	}
	if _, err := os.Stat(filepath.Join(target, "other-draft.json")); err == nil {
		t.Fatal("skill was written through the owner link")
	}
}

func TestPageScriptSkillPageURLPrefersStart(t *testing.T) {
	got := pageScriptSkillPageURL(&BrowserActionResult{Data: map[string]interface{}{
		"start_url": "https://docs.example.com/a",
		"url":       "https://other.example/b",
	}})
	if got != "https://docs.example.com/a" {
		t.Fatalf("url=%s", got)
	}
	if end := pageScriptSkillPageURL(&BrowserActionResult{Data: map[string]interface{}{"url": "https://other.example/b"}}); end != "" {
		t.Fatalf("end url=%s", end)
	}
	if _, err := SavePageScriptSkill(t.TempDir(), "0123456789abcdef", "save-draft", `await click("保存")`, got, "ok"); err != nil {
		t.Fatal(err)
	}
}

func TestPageScriptSkillDoesNotPromoteDamagedBackup(t *testing.T) {
	dir := t.TempDir()
	owner := "0123456789abcdef"
	ownerDir := filepath.Join(dir, owner)
	if err := os.MkdirAll(ownerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\n  \"name\": \"save-draft\",\n  \"host\": \"docs.example.com\",\n  \"script\": \"fetch(\\\"https://example.com\\\")\"\n}\n")
	if err := os.WriteFile(filepath.Join(ownerDir, "save-draft.json.bak"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPageScriptSkill(dir, owner, "save-draft"); err == nil {
		t.Fatal("a damaged backup was loaded")
	}
	if _, err := os.Stat(filepath.Join(ownerDir, "save-draft.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestPageScriptSkillRestoresBackup(t *testing.T) {
	dir := t.TempDir()
	owner := "0123456789abcdef"
	ownerDir := filepath.Join(dir, owner)
	if err := os.MkdirAll(ownerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := `await click("保存")`
	body := []byte("{\n  \"name\": \"save-draft\",\n  \"host\": \"docs.example.com\",\n  \"script\": \"await click(\\\"保存\\\")\"\n}\n")
	if err := os.WriteFile(filepath.Join(ownerDir, "save-draft.json.bak"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPageScriptSkill(dir, owner, "save-draft")
	if err != nil || loaded.Script != script || loaded.Host != "docs.example.com" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	if _, err := os.Stat(filepath.Join(ownerDir, "save-draft.json")); err != nil {
		t.Fatal(err)
	}
	listed, err := ListPageScriptSkills(dir, owner)
	if err != nil || strings.Count(listed, "save-draft") != 1 {
		t.Fatalf("list=%q err=%v", listed, err)
	}
}

func TestRunPageScriptBatchRejectsNameBeforeSession(t *testing.T) {
	_, err := RunPageScriptBatch(nil, `await click("保存")`, "Save Draft", "0123456789abcdef", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "kebab-case") {
		t.Fatalf("err=%v", err)
	}
}

func TestPageScriptCanSaveSkipsLogin(t *testing.T) {
	if PageScriptCanSave(&BrowserActionResult{Status: "ok", Display: "page flags: login_wall"}) {
		t.Fatal("login page was stored")
	}
	if PageScriptCanSave(&BrowserActionResult{Status: "ask", Display: "captcha"}) {
		t.Fatal("captcha was stored")
	}
	if !PageScriptCanSave(&BrowserActionResult{Status: "ok", Display: "script ok: clicked 保存"}) {
		t.Fatal("ordinary script was not stored")
	}
	if PageScriptCanSave(&BrowserActionResult{Status: "ok", Data: map[string]interface{}{"page_flags": map[string]bool{"login_wall": true}}}) {
		t.Fatal("unrecognized page flags were stored")
	}
	if PageScriptCanSave(&BrowserActionResult{Status: "ok", Display: "script ok: clicked 登录", Data: map[string]interface{}{"page_flags": BrowserPageFlags{LoginQR: true}}}) {
		t.Fatal("qr login page was stored")
	}
	if PageScriptCanSave(&BrowserActionResult{Status: "ok", Display: "script ok: clicked 同意", Data: map[string]interface{}{"page_flags": BrowserPageFlags{ConsentDialog: true}}}) {
		t.Fatal("consent dialog was stored")
	}
	left := &BrowserActionResult{Data: map[string]interface{}{
		"start_url": "https://docs.example.com/a",
		"url":       "https://accounts.example/login",
	}}
	if !pageScriptSkillLeftSite(left) {
		t.Fatal("a script that left its website was reusable")
	}
	stayed := &BrowserActionResult{Data: map[string]interface{}{
		"start_url": "https://www.docs.example.com/a",
		"url":       "https://docs.example.com/b",
	}}
	if pageScriptSkillLeftSite(stayed) {
		t.Fatal("a www redirect was treated as leaving the website")
	}
	if pageScriptSkillSummary("script ok: clicked Forgot password") == "" {
		t.Fatal("a button label was dropped")
	}
	if pageScriptSkillSummary("password=secret") != "" {
		t.Fatal("a password assignment was kept")
	}
	for _, host := range []string{".", ":", "a..b", "docs.example.com/path", "example.com.", "-bad.com"} {
		if _, ok := acceptableSkillHost(host); ok {
			t.Fatalf("host %q was accepted", host)
		}
	}
	if host, ok := acceptableSkillHost("Docs.Example.com"); !ok || host != "docs.example.com" {
		t.Fatalf("host=%q ok=%v", host, ok)
	}
}

func TestBrowserScriptToolsRegistered(t *testing.T) {
	reg := tool.NewRegistry()
	RegisterTools(reg)
	for _, name := range []string{"browser_script", "browser_skill_list", "browser_skill_run"} {
		toolDef, ok := reg.Get(name)
		if !ok || toolDef == nil || toolDef.Handler == nil {
			t.Fatalf("%s not registered", name)
		}
	}
}
