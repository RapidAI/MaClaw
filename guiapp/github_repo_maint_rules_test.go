package guiapp

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

func TestGitHubRepoMaintRuleLifecycle(t *testing.T) {
	if oauth.GitHubAccountScopes != "read:user repo" {
		t.Fatalf("oauth scope = %q", oauth.GitHubAccountScopes)
	}
	for _, raw := range []string{
		"http://203.0.113.10",
		"https://127.0.0.1",
		"https://10.1.2.3",
		"https://192.168.1.5",
		"https://169.254.1.1",
		"https://[fd00::1]",
		"https://localhost",
	} {
		if _, err := parsePublicHubURL(raw); err == nil {
			t.Fatalf("%s was accepted", raw)
		}
	}
	hubURL, err := parsePublicHubURL("https://203.0.113.10/")
	if err != nil {
		t.Fatal(err)
	}
	joined, err := repoMaintJoin(hubURL, "api", "github", "repo-maint", "hooks", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joined, "//api") || !strings.HasPrefix(joined, "https://203.0.113.10/api/github/repo-maint/hooks/tok") {
		t.Fatalf("join = %s", joined)
	}

	var mu sync.Mutex
	var calls []recordedHubCall
	var failCreate, failDelete bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, recordedHubCall{
			Host: r.Host, Method: r.Method, Path: r.URL.Path,
			Auth: r.Header.Get("Authorization"), Machine: r.Header.Get("X-Machine-ID"),
			Body: append([]byte(nil), body...),
		})
		create, del := failCreate, failDelete
		mu.Unlock()
		if r.Method == http.MethodDelete && del {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/hooks") && create {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"boom"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"secret_set":true}`))
	}))
	defer srv.Close()

	dialTLS := func() *http.Client {
		client := defaultGitHubHTTPClient()
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				switch addr {
				case "api.github.com:443", "203.0.113.10:443":
					return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
				default:
					t.Errorf("dialed %s", addr)
					return nil, context.Canceled
				}
			},
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		return client
	}
	prevGit := newGitHubHTTPClient
	prevHub := newRepoMaintHubHTTPClient
	prevClone := cloneGitHubRepoForMaint
	newGitHubHTTPClient = dialTLS
	newRepoMaintHubHTTPClient = dialTLS
	var cloneCalls int
	cloneGitHubRepoForMaint = func(a *App, repoURL, dest string) error {
		cloneCalls++
		return os.ErrInvalid
	}
	prevIntake := githubRepoMaintIntakeReady
	githubRepoMaintIntakeReady = false
	t.Cleanup(func() {
		newGitHubHTTPClient = prevGit
		newRepoMaintHubHTTPClient = prevHub
		cloneGitHubRepoForMaint = prevClone
		githubRepoMaintIntakeReady = prevIntake
	})

	app := newRepoMaintTestApp(t, "https://203.0.113.10", true)
	root := t.TempDir()
	ghost := filepath.Join(root, "ghost")
	if err := os.MkdirAll(ghost, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ghost, "init"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(ghost, "remote", "add", "origin", "https://github.com/YuceJade/JJQuant-V2.git"); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing")
	saved, err := app.SaveGitHubRepoMaintRule(repoMaintDraft(missing))
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatal("save created a missing directory")
	}
	if saved.Enabled || saved.IntakeReady || saved.ID == "" {
		t.Fatalf("draft = %+v", saved)
	}
	secret, err := app.repoMaintSecret(saved.ID)
	if err != nil || secret == "" {
		t.Fatalf("secret err=%v", err)
	}
	dbPath := filepath.Join(app.GetDataDir(), githubRepoMaintDBName)
	t.Cleanup(func() { closeRepoMaintDB(dbPath) })
	for _, path := range []string{dbPath, dbPath + "-wal"} {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret stored in %s", path)
		}
	}
	listed, err := app.ListGitHubRepoMaintRules()
	if err != nil {
		t.Fatal(err)
	}
	rawList, _ := json.Marshal(listed)
	if strings.Contains(string(rawList), secret) || strings.Contains(string(rawList), "hook_secret") {
		t.Fatal("list exposed the webhook secret")
	}
	if len(calls) != 0 || cloneCalls != 0 {
		t.Fatalf("draft save called out calls=%d clones=%d", len(calls), cloneCalls)
	}

	dup := repoMaintDraft(filepath.Join(root, "other-dir"))
	dup.RepoURL = "https://github.com/YuceJade/JJQuant-V2"
	if _, err := app.SaveGitHubRepoMaintRule(dup); err == nil {
		t.Fatal("duplicate owner/repo was saved")
	}
	occupied := repoMaintDraft(missing)
	occupied.RepoURL = "https://github.com/yucejade/other-name"
	if _, err := app.SaveGitHubRepoMaintRule(occupied); err == nil {
		t.Fatal("duplicate directory was saved")
	}
	emptyCmd := repoMaintDraft(filepath.Join(root, "empty-cmd"))
	emptyCmd.RepoURL = "https://github.com/yucejade/empty-cmd"
	emptyCmd.EnabledKinds = map[string]bool{"issues.opened": true}
	emptyCmd.Commands = nil
	if _, err := app.SaveGitHubRepoMaintRule(emptyCmd); err == nil {
		t.Fatal("empty command was saved")
	}
	filePath := filepath.Join(root, "occupied.txt")
	if err := os.WriteFile(filePath, []byte("keep-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := repoMaintDraft(filePath)
	foreign.RepoURL = "https://github.com/yucejade/foreign"
	if _, err := app.SaveGitHubRepoMaintRule(foreign); err == nil {
		t.Fatal("foreign path was saved")
	}
	if raw, _ := os.ReadFile(filePath); string(raw) != "keep-file" {
		t.Fatal("foreign file was overwritten")
	}

	if err := app.SetGitHubRepoMaintEnabled(saved.ID, true, "new_only"); err == nil || len(calls) != 0 || cloneCalls != 0 {
		t.Fatalf("intake not ready err=%v calls=%d clones=%d", err, len(calls), cloneCalls)
	}
	githubRepoMaintIntakeReady = true
	app.configSnap.Store(&corelib.AppConfig{RemoteHubURL: "https://10.1.2.3", RemoteMachineID: "m1", RemoteMachineToken: "machine-token"})
	if err := app.SetGitHubRepoMaintEnabled(saved.ID, true, "new_only"); err == nil || len(calls) != 0 {
		t.Fatalf("private hub err=%v calls=%d", err, len(calls))
	}
	app.configSnap.Store(&corelib.AppConfig{RemoteHubURL: "http://203.0.113.10", RemoteMachineID: "m1", RemoteMachineToken: "machine-token"})
	if err := app.SetGitHubRepoMaintEnabled(saved.ID, true, "new_only"); err == nil || len(calls) != 0 {
		t.Fatalf("http hub err=%v calls=%d", err, len(calls))
	}
	app.setGitHubTokenCache("")
	_ = app.credentialStore.Delete(oauth.GitHubAccountProviderID)
	publishRepoMaintConfig(app, "https://203.0.113.10")
	if err := app.SetGitHubRepoMaintEnabled(saved.ID, true, "new_only"); err == nil || !strings.Contains(err.Error(), "GitHub") || len(calls) != 0 {
		t.Fatalf("missing github err=%v calls=%d", err, len(calls))
	}
	saveGitHubToken(t, app)
	app.configSnap.Store(&corelib.AppConfig{RemoteHubURL: "https://203.0.113.10"})
	if err := app.SetGitHubRepoMaintEnabled(saved.ID, true, "new_only"); err == nil || !strings.Contains(err.Error(), "Hub") || len(calls) != 0 {
		t.Fatalf("missing hub err=%v calls=%d", err, len(calls))
	}
	publishRepoMaintConfig(app, "https://203.0.113.10")
	if err := app.SetGitHubRepoMaintEnabled(saved.ID, true, ""); err == nil {
		t.Fatal("first enable without a replay choice succeeded")
	}
	if err := app.SetGitHubRepoMaintEnabled(saved.ID, true, "new_only"); err == nil || cloneCalls != 1 || len(calls) != 0 {
		t.Fatalf("clone failure err=%v clones=%d calls=%d", err, cloneCalls, len(calls))
	}

	if _, err := app.SaveGitHubRepoMaintRule(repoMaintDraft(ghost)); err == nil {
		t.Fatal("second rule on the same repo was saved")
	}
	// The draft still points at the missing directory. Point it at the checkout
	// without enabling, then turn it on.
	moved := repoMaintDraft(ghost)
	moved.ID = saved.ID
	saved, err = app.SaveGitHubRepoMaintRule(moved)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetGitHubRepoMaintEnabled(saved.ID, true, "new_only"); err != nil {
		t.Fatal(err)
	}
	if cloneCalls != 1 {
		t.Fatalf("ready directory was cloned calls=%d", cloneCalls)
	}
	mu.Lock()
	got := append([]recordedHubCall(nil), calls...)
	mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("enable calls=%d", len(got))
	}
	assertHubPut(t, got[0], true, 0, secret)
	if got[1].Method != http.MethodPost || got[1].Path != "/repos/yucejade/jjquant-v2/hooks" {
		t.Fatalf("github create = %s %s", got[1].Method, got[1].Path)
	}
	var hook map[string]any
	if err := json.Unmarshal(got[1].Body, &hook); err != nil {
		t.Fatal(err)
	}
	cfg, _ := hook["config"].(map[string]any)
	if cfg["secret"] != secret || cfg["insecure_ssl"] != "0" || hook["active"] != true {
		t.Fatalf("hook body = %s", got[1].Body)
	}
	assertHubPut(t, got[2], true, 42, secret)
	listed, err = app.ListGitHubRepoMaintRules()
	if err != nil || len(listed) != 1 || !listed[0].Enabled || !listed[0].HookReady || !listed[0].IntakeReady {
		t.Fatalf("enabled list = %+v err=%v", listed, err)
	}

	mu.Lock()
	failCreate = true
	calls = nil
	mu.Unlock()
	secondGhost := filepath.Join(root, "second")
	if err := os.MkdirAll(secondGhost, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runGit(secondGhost, "init"); err != nil || runGit(secondGhost, "remote", "add", "origin", "https://github.com/yucejade/other.git") != nil {
		t.Fatal("second checkout")
	}
	second, err := app.SaveGitHubRepoMaintRule(GitHubRepoMaintRule{
		RepoURL:      "https://github.com/yucejade/other",
		GhostDir:     secondGhost,
		Commands:     map[string]string{"push": "记一笔"},
		EnabledKinds: map[string]bool{"push": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetGitHubRepoMaintEnabled(second.ID, true, "backfill"); err == nil {
		t.Fatal("hook create failure was treated as enabled")
	}
	mu.Lock()
	failed := append([]recordedHubCall(nil), calls...)
	failCreate = false
	mu.Unlock()
	if len(failed) != 3 || !hubEnabled(t, failed[0]) || failed[1].Method != http.MethodPost || hubEnabled(t, failed[2]) {
		t.Fatalf("rollback sequence = %#v", callSummary(failed))
	}
	secondView, err := app.publicRepoMaintRule(second.ID)
	if err != nil || secondView.Enabled {
		t.Fatalf("rolled back rule enabled=%v err=%v", secondView.Enabled, err)
	}

	db, err := app.repoMaintDB()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, object, status string }{
		{"job-queued", "issue:9001", "queued"},
		{"job-blocked", "issue:9002", "blocked_workspace"},
		{"job-running", "issue:9003", "running"},
	} {
		if _, err := db.Exec(`INSERT INTO jobs (
			id, rule_id, delivery_id, event_kind, object_id, ghost_dir, status,
			task_project_path, session_user_id, last_error, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,'','','','2026-11-18T00:00:00Z','2026-11-18T00:00:00Z')`,
			row.id, saved.ID, "d-"+row.id, "issues.opened", row.object, ghost, row.status); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	calls = nil
	mu.Unlock()
	if err := app.SetGitHubRepoMaintEnabled(saved.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ object, want string }{
		{"issue:9001", "skipped_disabled"},
		{"issue:9002", "skipped_disabled"},
		{"issue:9003", "running"},
	} {
		var status string
		if err := db.QueryRow(`SELECT status FROM jobs WHERE object_id=?`, row.object).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != row.want {
			t.Fatalf("%s status = %s", row.object, status)
		}
	}
	mu.Lock()
	disabled := append([]recordedHubCall(nil), calls...)
	mu.Unlock()
	if len(disabled) != 2 {
		t.Fatalf("disable calls=%d", len(disabled))
	}
	assertHubPut(t, disabled[0], false, 42, secret)
	if disabled[1].Method != http.MethodPatch || disabled[1].Path != "/repos/yucejade/jjquant-v2/hooks/42" {
		t.Fatalf("deactivate = %s %s", disabled[1].Method, disabled[1].Path)
	}
	var patch map[string]any
	if err := json.Unmarshal(disabled[1].Body, &patch); err != nil {
		t.Fatal(err)
	}
	patchCfg, _ := patch["config"].(map[string]any)
	if patch["active"] != false || patchCfg["secret"] != secret {
		t.Fatalf("deactivate body = %s", disabled[1].Body)
	}

	mu.Lock()
	failDelete = true
	calls = nil
	mu.Unlock()
	if err := app.DeleteGitHubRepoMaintRule(saved.ID); err == nil {
		t.Fatal("hub delete failure was ignored")
	}
	pending, err := app.loadRepoMaintRule(saved.ID)
	if err != nil || !pending.DeletePending {
		t.Fatalf("delete pending = %+v err=%v", pending, err)
	}
	if _, err := app.repoMaintSecret(saved.ID); err != nil {
		t.Fatal("secret removed while delete is pending")
	}
}

func TestGitHubRepoMaintRuleLimit(t *testing.T) {
	prev := githubRepoMaintIntakeReady
	githubRepoMaintIntakeReady = false
	t.Cleanup(func() { githubRepoMaintIntakeReady = prev })
	app := newRepoMaintTestApp(t, "https://203.0.113.10", false)
	t.Cleanup(func() { closeRepoMaintDB(filepath.Join(app.GetDataDir(), githubRepoMaintDBName)) })
	root := t.TempDir()
	for i := 0; i < githubRepoMaintMaxRules; i++ {
		rule := GitHubRepoMaintRule{
			RepoURL:  "https://github.com/yucejade/repo-" + itoa(i),
			GhostDir: filepath.Join(root, itoa(i)),
		}
		if _, err := app.SaveGitHubRepoMaintRule(rule); err != nil {
			t.Fatalf("rule %d: %v", i, err)
		}
	}
	if _, err := app.SaveGitHubRepoMaintRule(GitHubRepoMaintRule{
		RepoURL:  "https://github.com/yucejade/one-more",
		GhostDir: filepath.Join(root, "more"),
	}); err == nil {
		t.Fatal("rule 21 was saved")
	}
}

type recordedHubCall struct {
	Host, Method, Path, Auth, Machine string
	Body                              []byte
}

func callSummary(calls []recordedHubCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Method+" "+c.Host+c.Path)
	}
	return out
}

func hubEnabled(t *testing.T, call recordedHubCall) bool {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(call.Body, &body); err != nil {
		t.Fatal(err)
	}
	on, _ := body["enabled"].(bool)
	return on
}

func assertHubPut(t *testing.T, call recordedHubCall, enabled bool, hookID int, secret string) {
	t.Helper()
	if call.Method != http.MethodPut || !strings.Contains(call.Path, "/api/github/repo-maint/rules/") {
		t.Fatalf("hub put = %s %s", call.Method, call.Path)
	}
	if call.Auth != "Bearer machine-token" || call.Machine != "m1" {
		t.Fatalf("hub auth %q machine %q", call.Auth, call.Machine)
	}
	var body map[string]any
	if err := json.Unmarshal(call.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["enabled"] != enabled || body["hook_secret"] != secret {
		t.Fatalf("hub body enabled=%v secret-match=%v", body["enabled"], body["hook_secret"] == secret)
	}
	switch n := body["github_hook_id"].(type) {
	case float64:
		if int(n) != hookID {
			t.Fatalf("hook id %v", n)
		}
	default:
		t.Fatalf("hook id %T", body["github_hook_id"])
	}
	if _, ok := body["hook_secret"].(string); !ok || strings.Contains(call.Path, secret) {
		t.Fatal("secret missing or placed in the URL")
	}
}

func newRepoMaintTestApp(t *testing.T, hubURL string, withGitHub bool) *App {
	t.Helper()
	app := &App{
		testHomeDir:     t.TempDir(),
		credentialStore: oauth.NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.json")),
	}
	publishRepoMaintConfig(app, hubURL)
	if withGitHub {
		saveGitHubToken(t, app)
	}
	return app
}

func publishRepoMaintConfig(app *App, hubURL string) {
	cfg := corelib.AppConfig{
		RemoteHubURL:       hubURL,
		RemoteMachineID:    "m1",
		RemoteMachineToken: "machine-token",
	}
	app.configCache = cfg
	app.configCacheValid = true
	app.configSnap.Store(&cfg)
}

func saveGitHubToken(t *testing.T, app *App) {
	t.Helper()
	if err := app.credentialStore.Modify(oauth.GitHubAccountProviderID, func(old *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{Type: "oauth", AccessToken: "ghp_test_token", Scopes: oauth.GitHubAccountScopes}, nil
	}); err != nil {
		t.Fatal(err)
	}
	app.setGitHubTokenCache("ghp_test_token")
}

func repoMaintDraft(dir string) GitHubRepoMaintRule {
	return GitHubRepoMaintRule{
		RepoURL:      "https://github.com/yucejade/jjquant-v2",
		GhostDir:     dir,
		Commands:     map[string]string{"push": "更新日志", "issues.opened": "看一下议题"},
		EnabledKinds: map[string]bool{"push": true, "issues.opened": true},
	}
}
