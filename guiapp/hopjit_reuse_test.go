package guiapp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
)

func TestGUIHopJITReuseRunsReasonWithJSONContract(t *testing.T) {
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		if !jsonResponse {
			t.Fatal("reason step did not request JSON")
		}
		return `{"summary":"测试通过"}`, nil
	}}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: GUI Reuse
Goal: 验证 GUI Caller
Outputs:
- summary % text
## Steps
1. [reason] 生成摘要
  + → summary % text
2. [exit]
`, nil, caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["summary"] != "测试通过" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestGUIHopJITReuseRejectsMalformedModelJSON(t *testing.T) {
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		return "不是 JSON", nil
	}}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 错误输出
Goal: 非法 JSON 不得成功
Outputs:
- summary % text
## Steps
1. [reason] 生成摘要
  + → summary % text
`, nil, caller)
	if err != nil || result.Status != hopjit.NextFailed {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestGUIHopJITReuseResumeAnswersPause(t *testing.T) {
	store, err := hopjit.NewSQLitePersistence(filepath.Join(t.TempDir(), "hopjit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		t.Fatal("paused flow called LLM")
		return "", nil
	}}
	result, err := runHopSpecWithPersistence(context.Background(), store, `# Spec: GUI 暂停恢复
Goal: 等待用户输入
Outputs:
- answer % text
## Steps
1. [ask] 输入答案
  + → answer % text
2. [exit]
`, nil, caller)
	if err != nil || result.Status != hopjit.NextPaused {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result, err = resumeHopSpecWithPersistence(context.Background(), store, result.RunID, map[string]any{"answer": "继续"}, caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["answer"] != "继续" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestGUIHopJITReuseDrivesAdaptiveReplan(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("原路径失败")
		}
		if !jsonResponse {
			return "1. [reason] 替代路径\n  + → report % text", nil
		}
		return `{"report":"修复完成"}`, nil
	}}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: GUI Adaptive
Goal: 失败后重规划
Outputs:
- report % text
## Steps
1. [subtask adaptive]
  + → report % text
  1.1. [reason] 原路径
    + → report % text
2. [exit]
`, nil, caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "修复完成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestStripHopJITCodeFence(t *testing.T) {
	if got := stripHopJITCodeFence("```json\n{\"a\":1}\n```"); !strings.Contains(got, `"a":1`) {
		t.Fatalf("got=%q", got)
	}
}
