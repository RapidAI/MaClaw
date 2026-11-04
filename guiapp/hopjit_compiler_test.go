package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
)

func TestRunHopTaskCompilesAndExecutesSpec(t *testing.T) {
	calls := 0
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		calls++
		if calls == 1 {
			if jsonResponse {
				t.Fatal("compiler requested JSON instead of Markdown")
			}
			return `# Spec: 任务摘要
Goal: 生成并核验摘要
Outputs:
- report % text
## Steps
1. [reason] 生成摘要
  + → report % text
2. [check] 核验摘要
  - ← report
  + → accepted % bool
  + → feedback % text
3. [exit]
`, nil
		}
		if jsonResponse && calls == 2 {
			return `{"report":"完成"}`, nil
		}
		if jsonResponse {
			return `{"accepted":true,"feedback":""}`, nil
		}
		return "", nil
	}}
	result, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "生成任务摘要", caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["report"] != "完成" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestCompiledHopSpecRejectsCommit(t *testing.T) {
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		return `# Spec: 危险规约
Goal: 不应运行
Outputs:
- report % text
## Steps
1. [commit] 提交
  + → report % text
  > ` + "```hop_python" + `
  > report = "danger"
  > ` + "```" + `
`, nil
	}}
	_, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "执行危险任务", caller)
	if err == nil || !strings.Contains(err.Error(), "commit is not allowed") {
		t.Fatalf("err=%v", err)
	}
}

func TestCompiledHopSpecRejectsMalformedDocument(t *testing.T) {
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		return "不是 HopSpec", nil
	}}
	_, err := runHopTaskWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), "任务", caller)
	if err == nil {
		t.Fatal("malformed compiler output was accepted")
	}
}
