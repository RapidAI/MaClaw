package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestInlineSecretIsRedactedAndALaterAnswerResumesTheGoal(t *testing.T) {
	botID := "bot_turn_secret"
	desktopBotTurns.Delete(botID)
	original := "用 ssh 连家里的机器，端口 22，用户 mini，密码：s3cret-value，然后训练分类器"
	shown := desktopPrepareUserTurn(botID, original, nil)
	if strings.Contains(shown, "s3cret-value") {
		t.Fatalf("secret stayed in the task: %s", shown)
	}
	if !strings.Contains(shown, "SSH_PASSWORD") || !strings.Contains(shown, "环境变量") {
		t.Fatalf("redacted task: %s", shown)
	}
	desktopMarkWaiting(botID)
	if again := desktopPrepareUserTurn(botID, "已在本机保存 SSH_PASSWORD。", nil); strings.Contains(again, "继续原来的任务") {
		t.Fatalf("the sentence alone resumed without a carried value: %s", again)
	}
	desktopPrepareUserTurn(botID, original, nil)
	desktopMarkWaiting(botID)
	meta := map[string]string{"bot_secret_name": "SSH_PASSWORD", "bot_secret_value": "s3cret-value", "bot_secret.HUB_TOKEN": "hub-value"}
	next := desktopPrepareUserTurn(botID, "已在本机保存 SSH_PASSWORD。", meta)
	if len(meta) != 1 || meta["bot_secret_name"] != "SSH_PASSWORD" {
		t.Fatalf("secret values stayed in the stored metadata: %v", meta)
	}
	if !strings.Contains(next, "继续原来的任务") || !strings.Contains(next, "训练分类器") || strings.Contains(next, "s3cret-value") {
		t.Fatalf("resume: %s", next)
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("alice", botID)}
	var written, ran string
	prevFile, prevBash := desktopRemoteFile, desktopRemoteBash
	desktopRemoteFile = func(_ context.Context, _, _, action, filePath, content, _, _ string) (string, error) {
		if action == "write" {
			written = filePath + "\n" + content
		}
		return "ok", nil
	}
	desktopRemoteBash = func(_ context.Context, _, _, command string) (string, error) {
		ran = command
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteFile, desktopRemoteBash = prevFile, prevBash })
	if _, err := desktopContainerBash(context.Background(), scope, "echo hi"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ran, "s3cret-value") || strings.Contains(ran, "hub-value") || !strings.Contains(ran, "echo hi") || !strings.Contains(ran, "rm -f") {
		t.Fatalf("command line %q", ran)
	}
	if !strings.Contains(written, "s3cret-value") || !strings.Contains(written, ".maclaw-secret-") {
		t.Fatalf("env file %q", written)
	}
	if got := desktopScrubSecrets("value s3cret-value", desktopSecretsForScope(scope)); strings.Contains(got, "s3cret-value") {
		t.Fatalf("scrub: %s", got)
	}
}

func TestBashCannotListAnotherBotsWork(t *testing.T) {
	scope := agentruntime.Scope{UserID: desktop.BotDesktopUser("alice", "bot_mine")}
	if err := desktopBashConfined(scope, "ls /home/desktop/work/bot_other"); err == nil {
		t.Fatal("other bot work dir was allowed")
	}
	if err := desktopBashConfined(scope, "ls ~/素材"); err == nil {
		t.Fatal("listing every bot's materials was allowed")
	}
	if err := desktopBashConfined(scope, "cat ~/.git-credentials"); err == nil {
		t.Fatal("git credentials were allowed")
	}
	if err := desktopBashConfined(scope, "ls /home/desktop/work/bot_mine"); err != nil {
		t.Fatalf("own folder refused: %v", err)
	}
}

func TestRedactedTextIsNotCapturedAgain(t *testing.T) {
	botID := "bot_turn_twice"
	desktopBotTurns.Delete(botID)
	once := desktopPrepareUserTurn(botID, "ssh mini@host 密码是 hunter22", nil)
	twice := desktopPrepareUserTurn(botID, once, nil)
	scope := agentruntime.Scope{UserID: desktop.BotDesktopUser("alice", botID)}
	if got := desktopSecretsForScope(scope)["SSH_PASSWORD"]; got != "hunter22" || strings.Contains(twice, "hunter22") {
		t.Fatalf("value=%q text=%q", got, twice)
	}
	if ask := desktopPrepareUserTurn(botID, "密码是什么？", nil); ask != "密码是什么？" {
		t.Fatalf("a question was redacted: %q", ask)
	}
}

func TestBashConfinementCoversEverySpellingAndKeepsOwnFolders(t *testing.T) {
	scope := agentruntime.Scope{UserID: desktop.BotDesktopUser("alice", "bot_mine")}
	for _, cmd := range []string{"ls ~/work/bot_other", "cat $HOME/素材/bot_other/x", "ls ${HOME}/work", "ls /home/desktop/work/", "find ~/素材 -name x"} {
		if err := desktopBashConfined(scope, cmd); err == nil {
			t.Fatalf("allowed %q", cmd)
		}
	}
	for _, cmd := range []string{"cd /home/desktop/work/bot_mine/run_1 && python train.py", "ls ~/素材/bot_mine/runs", "ls /home/desktop/workspace", "echo hi"} {
		if err := desktopBashConfined(scope, cmd); err != nil {
			t.Fatalf("refused %q: %v", cmd, err)
		}
	}
}
