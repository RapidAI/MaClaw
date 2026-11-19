package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/gitforge"
)

func TestRepoAuthSurvivesARestartAndLeavesOtherSecretsOut(t *testing.T) {
	dir := t.TempDir()
	prev := desktopRepoAuthDir
	desktopRepoAuthDir = dir
	bot := "bot_repo_keep"
	const token = "ghp_testtokenvalue1234567890"
	t.Cleanup(func() {
		desktopRepoAuthDir = prev
		desktopForgetRepoAuth("ten", bot)
	})
	desktopNoteRepoTenant(bot, "ten")
	desktopPrepareUserTurn(bot, "", map[string]string{
		"bot_secret.GITHUB_TOKEN": token,
		"bot_secret.SSH_PASSWORD": "s3cret-value",
	})
	desktopSetRepoGrant(bot, gitforge.GitHub, false, map[string]bool{"acme/app": true})
	raw, err := os.ReadFile(filepath.Join(dir, "ten", bot+".json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, token) || strings.Contains(text, "s3cret-value") || !strings.Contains(text, "acme/app") {
		t.Fatalf("auth file leaked or dropped a value: %s", text)
	}
	desktopForgetRepoAuth("ten", bot)
	desktopEnsureRepoAuth("ten", bot)
	if desktopRepoTokenOf(bot, gitforge.GitHub) != token {
		t.Fatal("token was not restored")
	}
	scope := agentruntime.Scope{TenantID: "ten", UserID: desktop.BotDesktopUser("alice", bot)}
	allowed, err := gitforge.Parse("acme/app", gitforge.GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	denied, err := gitforge.Parse("secret/app", gitforge.GitHub, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := desktopRepoAdmit(scope, allowed); err != nil {
		t.Fatal(err)
	}
	if err := desktopRepoAdmit(scope, denied); err == nil {
		t.Fatal("a repository outside the saved choice was allowed")
	}
}

func TestRepoAuthRejectsAPathOutsideTheDirectory(t *testing.T) {
	dir := t.TempDir()
	prev := desktopRepoAuthDir
	desktopRepoAuthDir = dir
	t.Cleanup(func() { desktopRepoAuthDir = prev })
	if _, err := repoAuthPath("../evil", "bot_repo_keep"); err == nil {
		t.Fatal("tenant escaped the auth directory")
	}
	if _, err := repoAuthPath("ten", ".."); err == nil {
		t.Fatal("bot id escaped the auth directory")
	}
}
