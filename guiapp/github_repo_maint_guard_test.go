package guiapp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGitHubRepoMaintPathJail(t *testing.T) {
	outside := t.TempDir()
	ghost := filepath.Join(outside, "repo")
	if err := os.MkdirAll(ghost, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	const marker = "MAINT-SECRET-MARKER"
	if err := os.WriteFile(secret, []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(ghost, "inside.txt")
	if err := os.WriteFile(inside, []byte("MAINT-INSIDE-MARKER"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := newRepoMaintTestApp(t, "https://203.0.113.10", false)
	t.Cleanup(func() {
		closeRepoMaintDB(filepath.Join(app.GetDataDir(), githubRepoMaintDBName))
	})
	owner := "desktop-user:" + ghost
	repoMaintActiveUsers.Store(owner, ghost)
	t.Cleanup(func() { repoMaintActiveUsers.Delete(owner) })
	h := &IMMessageHandler{app: app}

	rel := filepath.Join("..", "secret.txt")
	if _, err := h.resolveFileToolPathForOwner(rel, owner); err == nil || !strings.Contains(err.Error(), "path outside the ghost directory") {
		t.Fatalf("relative read err = %v", err)
	}
	read := h.toolReadFile(map[string]interface{}{
		"path":                           rel,
		registeredToolPolicyOwnerIDField: owner,
	})
	if strings.Contains(read, marker) || !strings.Contains(read, "path outside the ghost directory") {
		t.Fatalf("read_file escaped: %s", read)
	}
	okRead := h.toolReadFile(map[string]interface{}{
		"path":                           "inside.txt",
		registeredToolPolicyOwnerIDField: owner,
	})
	if !strings.Contains(okRead, "MAINT-INSIDE-MARKER") {
		t.Fatalf("inside read = %s", okRead)
	}

	parent := filepath.Clean(filepath.Dir(ghost))
	if cwd, active := repoMaintToolDir(h, owner, ".."); !active || filepath.Clean(cwd) == parent || (cwd != "" && !repoMaintPathInside(ghost, cwd)) {
		t.Fatalf("shell cwd = %q active=%v parent=%s", cwd, active, parent)
	}
	if got := filepath.Clean(h.resolveToolWorkDirForOwner("..", owner)); got == parent {
		t.Fatalf("resolveToolWorkDir escaped to %s", got)
	}
	bash := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        "Write-Output " + marker,
		"working_dir":                    "..",
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(bash, marker) || !strings.Contains(bash, "path outside the ghost directory") {
		t.Fatalf("bash cwd escaped: %s", bash)
	}
	absBash := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        "Get-Content " + secret,
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(absBash, marker) || !strings.Contains(absBash, "path outside the ghost directory") {
		t.Fatalf("bash absolute arg escaped: %s", absBash)
	}
	if _, err := h.executeTrustedShell(owner, "Get-Content "+secret, time.Second); err == nil || !strings.Contains(err.Error(), "path outside the ghost directory") {
		t.Fatalf("trusted shell err = %v", err)
	}
	quoted := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        `Get-Content "` + secret + `"`,
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(quoted, marker) || !strings.Contains(quoted, "path outside the ghost directory") {
		t.Fatalf("quoted absolute arg escaped: %s", quoted)
	}
	if !repoMaintPathInside(ghost, "inside.txt") {
		t.Fatal("inside relative path rejected")
	}
	if cands := repoMaintCommandAbsPaths(`cmd /c echo hi`); len(cands) != 0 {
		t.Fatalf("command switch looked like a path: %#v", cands)
	}
	if cands := repoMaintCommandAbsPaths(`git clone https://github.com/owner/repo.git`); len(cands) != 0 {
		t.Fatalf("https url looked like a path: %#v", cands)
	}
	if err := repoMaintRejectShell(h, owner, ghost, `git clone https://github.com/owner/repo.git`); err != nil {
		t.Fatalf("https url rejected: %v", err)
	}
	if err := repoMaintRejectShell(h, owner, ghost, "git clone https://github.com/owner/repo.git "+secret); err == nil || !strings.Contains(err.Error(), "path outside the ghost directory") {
		t.Fatalf("url plus outside path err = %v", err)
	}
	rooted := secret[len(filepath.VolumeName(secret)):]
	if repoMaintPathInside(ghost, rooted) {
		t.Fatal("drive-root path treated as inside the ghost")
	}
	rootRead := h.toolReadFile(map[string]interface{}{
		"path":                           rooted,
		registeredToolPolicyOwnerIDField: owner,
	})
	if strings.Contains(rootRead, marker) || !strings.Contains(rootRead, "path outside the ghost directory") {
		t.Fatalf("drive-root read escaped: %s", rootRead)
	}
	rootBash := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        "Get-Content " + rooted,
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(rootBash, marker) || !strings.Contains(rootBash, "path outside the ghost directory") {
		t.Fatalf("drive-root arg escaped: %s", rootBash)
	}
	nested := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        `powershell -NoProfile -Command "Get-Content '` + secret + `'"`,
		registeredToolPolicyOwnerIDField: owner,
	}, nil)
	if strings.Contains(nested, marker) || !strings.Contains(nested, "path outside the ghost directory") {
		t.Fatalf("quoted command body escaped: %s", nested)
	}

	link := filepath.Join(ghost, "out")
	if err := os.Symlink(outside, link); err != nil {
		out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput()
		if jerr != nil {
			t.Fatalf("symlink %v; junction %v %s", err, jerr, out)
		}
	}
	linked := h.toolReadFile(map[string]interface{}{
		"path":                           filepath.Join("out", "secret.txt"),
		registeredToolPolicyOwnerIDField: owner,
	})
	if strings.Contains(linked, marker) || !strings.Contains(linked, "path outside the ghost directory") {
		t.Fatalf("symlink read escaped: %s", linked)
	}

	cb := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		projectPath:       ghost,
		permissionOwnerID: owner,
		handler:           h,
	}}
	assertCodingMaintToolStaysInside(t, cb, secret, marker)
	linkedCoding := cb.executeToolWithOutcome("read_file", mustRepoMaintToolJSON(t, map[string]string{
		"path": filepath.Join("out", "secret.txt"),
	}))
	if strings.Contains(linkedCoding.Text, marker) || !strings.Contains(linkedCoding.Text, "path outside the ghost directory") {
		t.Fatalf("coding junction read escaped: %s", linkedCoding.Text)
	}

	// Production CodingSubAgent leaves permissionOwnerID empty and uses the loop user.
	viaLoop := &codingSubAgentCallbacks{subagent: &CodingSubAgent{
		projectPath: ghost,
		loopCtx:     &LoopContext{UserID: owner},
		handler:     h,
	}}
	assertCodingMaintToolStaysInside(t, viaLoop, secret, marker)
	body, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != marker {
		t.Fatalf("secret file changed: %s", body)
	}
}

func mustRepoMaintToolJSON(t *testing.T, args map[string]string) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertCodingMaintToolStaysInside(t *testing.T, cb *codingSubAgentCallbacks, secret, marker string) {
	t.Helper()
	rooted := secret
	if vol := filepath.VolumeName(secret); vol != "" && strings.HasPrefix(secret, vol) {
		rooted = secret[len(vol):]
	}
	calls := []struct {
		name string
		args map[string]string
	}{
		{name: "bash", args: map[string]string{"command": "Get-Content " + secret}},
		{name: "bash", args: map[string]string{"command": "Get-Content " + rooted}},
		{name: "bash", args: map[string]string{"command": `powershell -NoProfile -Command "Get-Content '` + secret + `'"`}},
		{name: "read_file", args: map[string]string{"path": secret}},
		{name: "write_file", args: map[string]string{"path": secret, "content": "PWNED"}},
		{name: "edit_file", args: map[string]string{"path": secret, "old_string": marker, "new_string": "PWNED"}},
		{name: "edit_lines", args: map[string]string{"path": secret, "operation": "replace", "start_line": "1", "end_line": "1", "content": "PWNED"}},
	}
	for _, call := range calls {
		raw, err := json.Marshal(call.args)
		if err != nil {
			t.Fatal(err)
		}
		// edit_lines numbers are JSON numbers, not strings.
		if call.name == "edit_lines" {
			raw, err = json.Marshal(map[string]any{
				"path": secret, "operation": "replace", "start_line": 1, "end_line": 1, "content": "PWNED",
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		got := cb.executeToolWithOutcome(call.name, string(raw))
		if strings.Contains(got.Text, marker) || !strings.Contains(got.Text, "path outside the ghost directory") {
			t.Fatalf("coding %s escaped: %s", call.name, got.Text)
		}
	}
}

func TestGitHubRepoMaintUserPlanGate(t *testing.T) {
	app := newRepoMaintTestApp(t, "https://203.0.113.10", false)
	app.disableBackgroundEmbeddingForTest = true
	t.Cleanup(func() {
		closeRepoMaintDB(filepath.Join(app.GetDataDir(), githubRepoMaintDBName))
	})
	project := t.TempDir()
	saved, err := app.SaveGitHubRepoMaintRule(repoMaintDraft(project))
	if err != nil {
		t.Fatal(err)
	}
	project = saved.GhostDir
	userID := projectSessionOwnerID(project)
	db, err := app.repoMaintDB()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO jobs (id, rule_id, delivery_id, event_kind, object_id, ghost_dir, status, task_project_path, session_user_id, last_error, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		"job-user-plan", saved.ID, "d-user-plan", "issues.opened", "issue:21", project, "queued", "", "", "", now, now); err != nil {
		t.Fatal(err)
	}
	job := repoMaintJobByObject(t, app, "issue:21")
	if err := app.setRepoMaintJobStatus(job, "running", project, userID, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repoMaintActiveUsers.Delete(userID) })

	handler := &IMMessageHandler{app: app}
	handler.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.PlanMode = codingPlanModeApprove
	})
	resp := handler.runCodingTemplateSubAgent(userID, "1. alpha step\n2. beta step", project, nil, nil, nil)
	if resp == nil || !repoMaintResponseHasPlanGate(resp.Actions) {
		text := ""
		if resp != nil {
			text = resp.Text
		}
		t.Fatalf("plan gate response missing: %s", text)
	}
	app.finishRepoMaintTurn(userID, false, resp)
	got := repoMaintJobByObject(t, app, "issue:21")
	if got.Status != "awaiting_user" || got.SessionUserID != userID {
		t.Fatalf("user plan gate status=%s user=%s err=%s", got.Status, got.SessionUserID, got.LastError)
	}
	if dir, ok := repoMaintActiveUsers.Load(userID); !ok || strings.TrimSpace(dir.(string)) == "" {
		t.Fatalf("directory lock released: %v", dir)
	}
	if mode := handler.getStickyCodingWorkbenchMemory(userID).PlanMode; mode != codingPlanModeApprove {
		t.Fatalf("plan mode = %s", mode)
	}
}
