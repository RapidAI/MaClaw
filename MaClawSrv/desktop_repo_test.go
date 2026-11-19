package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func stubDesktopFiles(t *testing.T) {
	t.Helper()
	prev := desktopRemoteFile
	desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		return "wrote", nil
	}
	t.Cleanup(func() { desktopRemoteFile = prev })
}

func TestDesktopRepoCloneUsesTheSavedTokenWithoutPrintingIt(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	bot := "bot_repo_clone"
	stubDesktopFiles(t)
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	var command string
	prev := desktopRemoteBash
	desktopRemoteBash = func(_ context.Context, _, _ string, cmd string) (string, error) {
		command = cmd
		return "Cloning into app", nil
	}
	t.Cleanup(func() { desktopRemoteBash = prev })

	text, err := desktopRepoCall(context.Background(), agentservice.RepoCall{
		TenantID: "ten", UserID: "alice", BotID: bot, RunID: "run_repo_clone",
		Args: map[string]interface{}{"action": "clone", "host": "github", "repo": "acme/app", "ref": "main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(command, token) || strings.Contains(text, token) {
		t.Fatalf("token leaked\ncmd=%s\nout=%s", command, text)
	}
	if !strings.Contains(command, "https://github.com/acme/app.git") || !strings.Contains(command, "$GITHUB_TOKEN") || !strings.Contains(command, "x-access-token") {
		t.Fatalf("clone command: %s", command)
	}
	if !strings.Contains(text, "checkout /home/desktop/work/bot_repo_clone/") {
		t.Fatalf("checkout: %s", text)
	}
}

func TestDesktopRepoPushAsksBeforeItRuns(t *testing.T) {
	desktopbot.ResetDangerState()
	t.Cleanup(desktopbot.ResetDangerState)
	const token = "ghp_testtokenvalue1234567890"
	bot := "bot_repo_push"
	stubDesktopFiles(t)
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	pushes := 0
	pushed := ""
	prev := desktopRemoteBash
	desktopRemoteBash = func(_ context.Context, _, _ string, cmd string) (string, error) {
		if strings.Contains(cmd, token) {
			t.Fatal("push command contained the token")
		}
		switch {
		case strings.Contains(cmd, "rev-parse"):
			return "fix-login", nil
		case strings.Contains(cmd, "remote get-url"):
			return "https://github.com/acme/app.git", nil
		default:
			pushes++
			pushed = cmd
			return "pushed", nil
		}
	}
	t.Cleanup(func() { desktopRemoteBash = prev })
	call := agentservice.RepoCall{
		TenantID: "ten", UserID: "alice", BotID: bot, InstanceID: "inst_repo_push",
		Args: map[string]interface{}{"action": "push", "path": "/home/desktop/work/bot_repo_push/app", "ref": "main"},
	}
	_, err := desktopRepoCall(context.Background(), call)
	if err == nil || !strings.Contains(err.Error(), "先问这个人") || pushes != 0 {
		t.Fatalf("first push err=%v pushes=%d", err, pushes)
	}
	ask, ok := desktopbot.PeekDangerAsk(call.InstanceID)
	if !ok || !strings.Contains(ask.Text, "git push fix-login") {
		t.Fatalf("ask %+v %v", ask, ok)
	}
	call.UserText = "确认"
	text, err := desktopRepoCall(context.Background(), call)
	if err != nil || !strings.Contains(text, "pushed") || pushes != 1 || !strings.Contains(pushed, "push -u origin 'fix-login'") || strings.Contains(pushed, "origin 'main'") {
		t.Fatalf("after confirm text=%s err=%v pushes=%d script=%s", text, err, pushes, pushed)
	}
}

func TestDesktopRepoIssuesDoNotWakeABrowser(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	bot := "bot_repo_issues"
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	prev := desktopRepoHTTP
	desktopRepoHTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("auth header")
		}
		if !strings.Contains(r.URL.Path, "/repos/acme/app/issues") {
			t.Errorf("path %s", r.URL.Path)
		}
		body := `[{"number":3,"title":"login crash","html_url":"https://github.com/acme/app/issues/3","updated_at":"2026-11-15T00:00:00Z"}]`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { desktopRepoHTTP = prev })

	text, err := desktopRepoCall(context.Background(), agentservice.RepoCall{
		UserID: "alice", BotID: bot,
		Args: map[string]interface{}{"action": "issues", "host": "github", "repo": "acme/app"},
	})
	if err != nil || !strings.Contains(text, "#3 login crash") || strings.Contains(text, token) {
		t.Fatalf("issues: %s %v", text, err)
	}
}

func TestDesktopRepoPRPushesTheBranchAndOpensIt(t *testing.T) {
	desktopbot.ResetDangerState()
	t.Cleanup(desktopbot.ResetDangerState)
	const token = "ghp_testtokenvalue1234567890"
	bot := "bot_repo_pr"
	stubDesktopFiles(t)
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	var pushed bool
	prev := desktopRemoteBash
	desktopRemoteBash = func(_ context.Context, _, _ string, cmd string) (string, error) {
		if strings.Contains(cmd, token) {
			t.Fatal("command contained the token")
		}
		switch {
		case strings.Contains(cmd, "rev-parse"):
			return "fix-login", nil
		case strings.Contains(cmd, "symbolic-ref"):
			return "origin/main", nil
		case strings.Contains(cmd, "remote get-url"):
			return "https://github.com/acme/app.git", nil
		case strings.Contains(cmd, "push -u origin HEAD"):
			pushed = true
			return "pushed", nil
		default:
			t.Fatalf("unexpected command: %s", cmd)
			return "", nil
		}
	}
	t.Cleanup(func() { desktopRemoteBash = prev })
	prevHTTP := desktopRepoHTTP
	desktopRepoHTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/repos/acme/app/pulls" {
			t.Errorf("path %s", r.URL.Path)
		}
		body := `[]`
		if r.Method != http.MethodGet {
			body = `{"html_url":"https://github.com/acme/app/pull/9"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { desktopRepoHTTP = prevHTTP })
	call := agentservice.RepoCall{
		TenantID: "ten", UserID: "alice", BotID: bot, InstanceID: "inst_repo_pr",
		Args: map[string]interface{}{
			"action": "pr", "path": "/home/desktop/work/bot_repo_pr/app",
			"name": "Fix empty login", "body": "closes #3",
		},
	}
	if _, err := desktopRepoCall(context.Background(), call); err == nil || !strings.Contains(err.Error(), "先问这个人") || pushed {
		t.Fatalf("pr ran before confirm: %v pushed=%v", err, pushed)
	}
	call.UserText = "确认"
	text, err := desktopRepoCall(context.Background(), call)
	if err != nil || !pushed || !strings.Contains(text, "pull/9") || strings.Contains(text, token) {
		t.Fatalf("pr: %s %v pushed=%v", text, err, pushed)
	}
}

func TestDesktopRepoPRDoesNotPushTheBaseBranch(t *testing.T) {
	desktopbot.ResetDangerState()
	t.Cleanup(desktopbot.ResetDangerState)
	const token = "ghp_testtokenvalue1234567890"
	bot := "bot_repo_pr_base"
	stubDesktopFiles(t)
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	prev := desktopRemoteBash
	desktopRemoteBash = func(_ context.Context, _, _ string, cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "rev-parse"):
			return "main", nil
		case strings.Contains(cmd, "symbolic-ref"):
			return "origin/main", nil
		default:
			t.Fatalf("command ran: %s", cmd)
			return "", nil
		}
	}
	t.Cleanup(func() { desktopRemoteBash = prev })
	call := agentservice.RepoCall{
		TenantID: "ten", UserID: "alice", BotID: bot, InstanceID: "inst_repo_pr_base",
		Args: map[string]interface{}{
			"action": "pr", "path": "/home/desktop/work/bot_repo_pr_base/app",
			"name": "Fix", "body": "closes #3",
		},
	}
	if _, err := desktopRepoCall(context.Background(), call); err == nil || !strings.Contains(err.Error(), "先问这个人") {
		t.Fatalf("pr ran before confirm: %v", err)
	}
	call.UserText = "确认"
	_, err := desktopRepoCall(context.Background(), call)
	if err == nil || !strings.Contains(err.Error(), "base branch") || !strings.Contains(err.Error(), "nothing was pushed") {
		t.Fatalf("pr on main: %v", err)
	}
}

func TestDesktopRepoMergeAsksThenLandsTheRequest(t *testing.T) {
	desktopbot.ResetDangerState()
	t.Cleanup(desktopbot.ResetDangerState)
	const token = "ghp_testtokenvalue1234567890"
	bot := "bot_repo_merge"
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	prev := desktopRemoteBash
	desktopRemoteBash = func(context.Context, string, string, string) (string, error) {
		t.Fatal("merge ran a shell")
		return "", nil
	}
	t.Cleanup(func() { desktopRemoteBash = prev })
	var puts int
	prevHTTP := desktopRepoHTTP
	desktopRepoHTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/pulls/9" {
			body := `{"head":{"sha":"0123456789abcdef0123456789abcdef01234567"}}`
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if r.Method != http.MethodPut || r.URL.Path != "/repos/acme/app/pulls/9/merge" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"sha":"0123456789abcdef0123456789abcdef01234567"`) {
			t.Errorf("merge body %s", raw)
		}
		puts++
		body := `{"merged":true,"sha":"abc123"}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { desktopRepoHTTP = prevHTTP })
	call := agentservice.RepoCall{
		TenantID: "ten", UserID: "alice", BotID: bot, InstanceID: "inst_repo_merge",
		Args: map[string]interface{}{"action": "merge", "host": "github", "repo": "acme/app", "number": "9"},
	}
	if _, err := desktopRepoCall(context.Background(), call); err == nil || !strings.Contains(err.Error(), "先问这个人") || puts != 0 {
		t.Fatalf("merge ran before confirm: %v puts=%d", err, puts)
	}
	call.UserText = "确认"
	text, err := desktopRepoCall(context.Background(), call)
	if err != nil || puts != 1 || !strings.Contains(text, "merged #9") || strings.Contains(text, token) {
		t.Fatalf("merge: %s puts=%d %v", text, puts, err)
	}
}

func TestDesktopRepoRequestReadsTheReviewWithoutAsking(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	bot := "bot_repo_request"
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	prevHTTP := desktopRepoHTTP
	desktopRepoHTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/acme/app/pulls/9":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"number":9,"title":"Fix empty login","state":"open","mergeable":true,"mergeable_state":"clean","html_url":"https://github.com/acme/app/pull/9","head":{"ref":"fix-login"},"base":{"ref":"main"}}`)), Header: make(http.Header)}, nil
		case "/repos/acme/app/pulls/9/reviews":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"state":"APPROVED","body":"looks good","user":{"login":"octo"}}]`)), Header: make(http.Header)}, nil
		case "/repos/acme/app/pulls/9/comments":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
		default:
			t.Errorf("path %s", r.URL.Path)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
		}
	})}
	t.Cleanup(func() { desktopRepoHTTP = prevHTTP })
	text, err := desktopRepoCall(context.Background(), agentservice.RepoCall{
		UserID: "alice", BotID: bot,
		Args: map[string]interface{}{"action": "request", "host": "github", "repo": "acme/app", "number": "9"},
	})
	if err != nil || !strings.Contains(text, "mergeable yes (clean)") || !strings.Contains(text, "APPROVED") || strings.Contains(text, token) || strings.Contains(text, "先问这个人") {
		t.Fatalf("request: %s %v", text, err)
	}
}

func TestDesktopRepoSyncMergesTheDefaultBranchWithoutPushing(t *testing.T) {
	const token = "ghp_testtokenvalue1234567890"
	bot := "bot_repo_sync"
	stubDesktopFiles(t)
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	var command string
	prev := desktopRemoteBash
	desktopRemoteBash = func(_ context.Context, _, _ string, cmd string) (string, error) {
		if strings.Contains(cmd, token) {
			t.Fatal("command contained the token")
		}
		switch {
		case strings.Contains(cmd, "remote get-url"):
			return "https://github.com/acme/app.git", nil
		case strings.Contains(cmd, "symbolic-ref"):
			return "origin/main", nil
		case strings.Contains(cmd, "fetch origin"):
			command = cmd
			return "Already up to date.", nil
		default:
			t.Fatalf("unexpected command: %s", cmd)
			return "", nil
		}
	}
	t.Cleanup(func() { desktopRemoteBash = prev })
	text, err := desktopRepoCall(context.Background(), agentservice.RepoCall{
		UserID: "alice", BotID: bot,
		Args: map[string]interface{}{"action": "sync", "path": "/home/desktop/work/bot_repo_sync/app"},
	})
	if err != nil || !strings.Contains(text, "Already up to date.") || !strings.Contains(command, "fetch origin '+refs/heads/main:refs/remotes/origin/main'") || !strings.Contains(command, "merge --no-edit 'origin/main'") || strings.Contains(command, "git push") || strings.Contains(text, token) {
		t.Fatalf("sync: %s\ncmd=%s\n%v", text, command, err)
	}
}

func TestDesktopRepoReleasePinsTheCheckout(t *testing.T) {
	desktopbot.ResetDangerState()
	t.Cleanup(desktopbot.ResetDangerState)
	const token = "ghp_testtokenvalue1234567890"
	const sha = "0123456789abcdef0123456789abcdef01234567"
	bot := "bot_repo_release"
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": token})
	prev := desktopRemoteBash
	desktopRemoteBash = func(_ context.Context, _, _ string, cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "abbrev-ref"):
			return "fix-login", nil
		case strings.Contains(cmd, "rev-parse"):
			return sha, nil
		default:
			t.Fatalf("unexpected %s", cmd)
			return "", nil
		}
	}
	t.Cleanup(func() { desktopRemoteBash = prev })
	var posts int
	prevHTTP := desktopRepoHTTP
	desktopRepoHTTP = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/app/releases" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
			return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
		}
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"target_commitish":"`+sha+`"`) || strings.Contains(string(raw), `"target_commitish":"main"`) {
			t.Errorf("release body %s", raw)
		}
		posts++
		body := `{"html_url":"https://github.com/acme/app/releases/tag/v1.2.0","tag_name":"v1.2.0"}`
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { desktopRepoHTTP = prevHTTP })
	call := agentservice.RepoCall{
		TenantID: "ten", UserID: "alice", BotID: bot, InstanceID: "inst_repo_release",
		Args: map[string]interface{}{"action": "release", "host": "github", "repo": "acme/app", "tag": "v1.2.0", "path": "/home/desktop/work/bot_repo_release/app"},
	}
	if _, err := desktopRepoCall(context.Background(), call); err == nil || !strings.Contains(err.Error(), "先问这个人") || posts != 0 {
		t.Fatalf("release ran before confirm: %v posts=%d", err, posts)
	}
	ask, ok := desktopbot.PeekDangerAsk(call.InstanceID)
	if !ok || !strings.Contains(ask.Text, "fix-login") || !strings.Contains(ask.Text, sha) {
		t.Fatalf("ask %+v %v", ask, ok)
	}
	call.UserText = "确认"
	text, err := desktopRepoCall(context.Background(), call)
	if err != nil || posts != 1 || !strings.Contains(text, "released v1.2.0") || strings.Contains(text, token) {
		t.Fatalf("release: %s posts=%d %v", text, posts, err)
	}
}

func TestDesktopRepoPlanDoesNotClone(t *testing.T) {
	prev := desktopRemoteBash
	ran := false
	desktopRemoteBash = func(context.Context, string, string, string) (string, error) {
		ran = true
		return "", nil
	}
	t.Cleanup(func() { desktopRemoteBash = prev })
	_, err := desktopRepoCall(context.Background(), agentservice.RepoCall{
		Phase: "plan", UserID: "alice", BotID: "bot_repo_plan",
		Args: map[string]interface{}{"action": "clone", "repo": "acme/app"},
	})
	if err == nil || !strings.Contains(err.Error(), "只核对") || ran {
		t.Fatalf("plan clone: %v ran=%v", err, ran)
	}
	_, err = desktopRepoCall(context.Background(), agentservice.RepoCall{
		Phase: "plan", UserID: "alice", BotID: "bot_repo_plan",
		Args: map[string]interface{}{"action": "sync", "path": "/home/desktop/work/bot_repo_plan/app"},
	})
	if err == nil || !strings.Contains(err.Error(), "只核对") || ran {
		t.Fatalf("plan sync: %v ran=%v", err, ran)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
