package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func delegateTurn(runID, content string, meta map[string]string) agentruntime.TurnRequest {
	inst := agentservice.Instance{ID: "inst_dr", Metadata: map[string]string{"hub_user_id": "alice", "hub_tenant_id": "tenant-a", "hub_bot": "1"}}
	var in agentservice.ExecuteRequest
	in.Instance = inst
	in.Message.Content = content
	in.Message.Metadata = meta
	return agentruntime.TurnRequest{Scope: agentruntime.Scope{RunID: runID}, Input: in}
}

// The verbose reply seen on papers 2026-10-30 becomes one sentence that
// names who has the task and restates nothing.
func TestDelegationTurnReplyIsOneSentence(t *testing.T) {
	useTempMultibotState(t)
	desktopRemoteDelegate = func(context.Context, map[string]any) (map[string]any, error) {
		return map[string]any{"id": "note_x"}, nil
	}
	defer func() { desktopRemoteDelegate = nil }()
	req := delegateTurn("run_dr1", "让 collab-b 写一个 Python 函数，计算字符串里每个单词出现的次数（附两个测试用例），写好后把代码发给我。", nil)
	if _, err := desktopDelegate(context.Background(), req, map[string]any{"target": "collab-b", "message": "写 word_count，忽略大小写，附两个测试用例，不要发布，不要建文件"}); err != nil {
		t.Fatal(err)
	}
	model := "已交给 collab-b，我还没收到它的结果。\n\n- **任务**：写一个统计字符串中每个单词出现次数的 Python 函数（忽略大小写）\n- **约束**：不发布到任何网站，不创建文件。\n\n代码还没到，我不替它先写。"
	got := desktopReplyAfterDelegation("inst_dr", "run_dr1", "sess_1", model)
	if got != "已交给 collab-b，结果出来会直接发到这里。" {
		t.Fatalf("reply=%q", got)
	}
	for _, bad := range []string{"\n", "- ", "任务", "约束", "忽略大小写", "note_", "代码还没到"} {
		if strings.Contains(got, bad) {
			t.Fatalf("reply restates %q: %q", bad, got)
		}
	}
	if strings.Count(got, "。") != 1 {
		t.Fatalf("not one sentence: %q", got)
	}
	// Taken once: the next run of the bot keeps its own text.
	if again := desktopReplyAfterDelegation("inst_dr", "run_dr1", "", "别的回答"); again != "别的回答" {
		t.Fatalf("second take=%q", again)
	}
}

func TestDelegationReplyNamesEveryTeammateAndFollowsLanguage(t *testing.T) {
	useTempMultibotState(t)
	desktopRemoteDelegate = func(context.Context, map[string]any) (map[string]any, error) {
		return map[string]any{"id": "note_y"}, nil
	}
	defer func() { desktopRemoteDelegate = nil }()
	req := delegateTurn("run_dr2", "Have collab-a and collab-b each write a haiku.", nil)
	for _, target := range []string{"collab-a", "collab-b"} {
		if _, err := desktopDelegate(context.Background(), req, map[string]any{"target": target, "message": "write a haiku " + target}); err != nil {
			t.Fatal(err)
		}
	}
	got := desktopReplyAfterDelegation("inst_dr", "run_dr2", "", "long model text\n- requirement")
	if got != "collab-a and collab-b have it; the results will show up here." {
		t.Fatalf("reply=%q", got)
	}
	if strings.Contains(got, "\n") || strings.Contains(got, "requirement") {
		t.Fatalf("reply=%q", got)
	}
}

// A wake turn and a run that did not delegate keep the model's text.
func TestNoDelegationOrWakeKeepsModelText(t *testing.T) {
	useTempMultibotState(t)
	desktopRemoteDelegate = func(context.Context, map[string]any) (map[string]any, error) {
		return map[string]any{"id": "note_z"}, nil
	}
	defer func() { desktopRemoteDelegate = nil }()
	if got := desktopReplyAfterDelegation("inst_dr", "run_none", "", "答案是 42。"); got != "答案是 42。" {
		t.Fatalf("got %q", got)
	}
	wake := delegateTurn("run_wake", "新邮件", map[string]string{"bot_wake": "1"})
	if _, err := desktopDelegate(context.Background(), wake, map[string]any{"target": "collab-b", "message": "返工"}); err != nil {
		t.Fatal(err)
	}
	if got := desktopReplyAfterDelegation("inst_dr", "run_wake", "", "退回给 collab-b 返工了。"); got != "退回给 collab-b 返工了。" {
		t.Fatalf("wake got %q", got)
	}
}
