package main

import (
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
	next := desktopPrepareUserTurn(botID, "已在本机保存 SSH_PASSWORD。", nil)
	if !strings.Contains(next, "继续原来的任务") || !strings.Contains(next, "训练分类器") || strings.Contains(next, "s3cret-value") {
		t.Fatalf("resume: %s", next)
	}
	scope := agentruntime.Scope{UserID: desktop.BotDesktopUser("alice", botID)}
	cmd := desktopExportSecrets("echo hi", desktopSecretsForScope(scope))
	if !strings.Contains(cmd, "export SSH_PASSWORD=") || strings.Contains(cmd, "echo hi") == false {
		t.Fatalf("export: %s", cmd)
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
