package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/gitforge"
)

func TestDesktopRepoConnectGivesALinkAndThenKeepsTheToken(t *testing.T) {
	desktopRepoAuthReset()
	t.Cleanup(desktopRepoAuthReset)
	prevStart, prevPoll := desktopRepoStartDevice, desktopRepoPollDevice
	t.Cleanup(func() {
		desktopRepoStartDevice = prevStart
		desktopRepoPollDevice = prevPoll
	})
	const token = "ghp_testtokenvalue1234567890"
	hold := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(hold) }) }
	t.Cleanup(release)
	desktopRepoStartDevice = func(context.Context, string, string, string) (string, string, int, error) {
		return "ABCD-EFGH", "device-secret-value", 1, nil
	}
	desktopRepoPollDevice = func(context.Context, string, string, string, int) (string, error) {
		<-hold
		return token, nil
	}
	bot := "bot_repo_auth"
	call := agentservice.RepoCall{
		TenantID: "ten", UserID: "alice", BotID: bot,
		Args: map[string]interface{}{"action": "connect", "host": "github"},
	}
	text, err := desktopRepoCall(context.Background(), call)
	if err != nil || !strings.Contains(text, "https://github.com/login/device?user_code=ABCD-EFGH") || strings.Contains(text, "device-secret-value") || strings.Contains(text, token) {
		t.Fatalf("connect: %s %v", text, err)
	}
	if !strings.Contains(text, "workflow") || !strings.Contains(text, "read:org") {
		t.Fatalf("scopes not shown: %s", text)
	}
	again, err := desktopRepoCall(context.Background(), call)
	if err != nil || again != text {
		t.Fatalf("second connect: %s %v", again, err)
	}
	release()
	deadline := time.Now().Add(2 * time.Second)
	for desktopRepoTokenOf(bot, gitforge.GitHub) != token && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if desktopRepoTokenOf(bot, gitforge.GitHub) != token || !desktopRepoMustPick(bot, gitforge.GitHub) {
		t.Fatal("token was not saved for a repository choice")
	}
}

func TestDesktopRepoGrantLimitsTheBotToChosenRepos(t *testing.T) {
	bot := "bot_repo_grant"
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": "ghp_testtokenvalue1234567890"})
	desktopMarkRepoPick(bot, gitforge.GitHub)
	text, err := desktopRepoCall(context.Background(), agentservice.RepoCall{
		UserID: "alice", BotID: bot,
		Args: map[string]interface{}{"action": "grant", "host": "github", "repos": "acme/app, other/lib"},
	})
	if err != nil || !strings.Contains(text, "acme/app") || !strings.Contains(text, "other/lib") {
		t.Fatalf("grant: %s %v", text, err)
	}
	allowed, err := gitforge.Parse("acme/app", gitforge.GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	denied, err := gitforge.Parse("secret/app", gitforge.GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	scope := agentruntime.Scope{UserID: desktop.BotDesktopUser("alice", bot)}
	if err := desktopRepoAdmit(scope, allowed); err != nil {
		t.Fatal(err)
	}
	if err := desktopRepoAdmit(scope, denied); err == nil || !strings.Contains(err.Error(), "不在这个 bot 的授权范围里") {
		t.Fatalf("denied: %v", err)
	}
}

func TestDesktopRepoCloneWaitsForARepositoryChoice(t *testing.T) {
	bot := "bot_repo_pick"
	desktopPrepareUserTurn(bot, "", map[string]string{"bot_secret.GITHUB_TOKEN": "ghp_testtokenvalue1234567890"})
	desktopMarkRepoPick(bot, gitforge.GitHub)
	prev := desktopRemoteBash
	desktopRemoteBash = func(context.Context, string, string, string) (string, error) {
		t.Fatal("clone ran before a repository was chosen")
		return "", nil
	}
	t.Cleanup(func() { desktopRemoteBash = prev })
	_, err := desktopRepoCall(context.Background(), agentservice.RepoCall{
		UserID: "alice", BotID: bot,
		Args: map[string]interface{}{"action": "clone", "host": "github", "repo": "acme/app"},
	})
	if err == nil || !strings.Contains(err.Error(), "还没有选定仓库") {
		t.Fatalf("clone: %v", err)
	}
}
