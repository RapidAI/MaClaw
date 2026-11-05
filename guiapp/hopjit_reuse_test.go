package guiapp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestGUIHopJITResumeRejectsDifferentRunOwner(t *testing.T) {
	store := hopjit.NewMemoryPersistence()
	callerA := &guiHopJITCaller{identity: hopJITExecutionIdentity{PrincipalID: "user-a"}}
	result, err := runHopSpecWithPersistence(context.Background(), store, `# Spec: 所有者暂停
Goal: 仅所有者可回答
Outputs:
- answer % text
## Steps
1. [ask] 输入答案
  + → answer % text
`, nil, callerA)
	if err != nil || result.Status != hopjit.NextPaused || result.Snapshot.OwnerID != "user-a" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	callerB := &guiHopJITCaller{identity: hopJITExecutionIdentity{PrincipalID: "user-b"}}
	if _, err := resumeHopSpecWithPersistence(context.Background(), store, result.RunID, map[string]any{"answer": "越权"}, callerB); !errors.Is(err, hopjit.ErrRunAccessDenied) {
		t.Fatalf("cross-owner resume err=%v", err)
	}
	result, err = resumeHopSpecWithPersistence(context.Background(), store, result.RunID, map[string]any{"answer": "合法"}, callerA)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["answer"] != "合法" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestHopJITActiveRunCancellationStopsCaller(t *testing.T) {
	var runs hopJITRunState
	started := make(chan struct{}, 1)
	caller := &guiHopJITCaller{invoke: func(ctx context.Context, _ []interface{}, _ bool) (string, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return "", ctx.Err()
	}}
	type outcome struct {
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		_, err := runHopSpecWithPersistenceManaged(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 活动中止
Goal: 取消等待中的调用
Outputs:
- report % text
## Steps
1. [reason] 等待模型
  + → report % text
`, nil, caller, &runs)
		done <- outcome{err: err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("caller did not start")
	}
	runs.mu.Lock()
	var runID string
	for id := range runs.active {
		runID = id
	}
	runs.mu.Unlock()
	if runID == "" || !runs.cancel(runID) {
		t.Fatalf("active run %q was not cancellable", runID)
	}
	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("err=%v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not stop caller")
	}
	if runs.cancel(runID) {
		t.Fatal("finished run remained active")
	}
}

func TestHopJITActiveRunRejectsDuplicateDriver(t *testing.T) {
	var runs hopJITRunState
	cancel := func() {}
	unregister, err := runs.register("run-1", cancel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.register("run-1", cancel); err == nil {
		t.Fatal("duplicate active run was accepted")
	}
	unregister()
	unregisterAgain, err := runs.register("run-1", cancel)
	if err != nil {
		t.Fatal(err)
	}
	unregisterAgain()
}

func TestHopJITDuplicateResumeDoesNotMutateSnapshot(t *testing.T) {
	store := hopjit.NewMemoryPersistence()
	caller := &guiHopJITCaller{invoke: func(ctx context.Context, _ []interface{}, _ bool) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	paused, err := runHopSpecWithPersistence(context.Background(), store, `# Spec: 重复恢复
Goal: 重复恢复不得干扰活动快照
Outputs:
- report % text
## Steps
1. [ask] 回答
  + → answer % text
2. [reason] 等待模型
  + → report % text
`, nil, &guiHopJITCaller{})
	if err != nil || paused.Status != hopjit.NextPaused {
		t.Fatalf("paused=%+v err=%v", paused, err)
	}
	var runs hopJITRunState
	started := make(chan struct{}, 1)
	caller.invoke = func(ctx context.Context, _ []interface{}, _ bool) (string, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := resumeHopSpecWithPersistenceManaged(context.Background(), store, paused.RunID, map[string]any{"answer": "继续"}, caller, &runs)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first resume did not start caller")
	}
	before, err := store.LoadSnapshot(context.Background(), paused.RunID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resumeHopSpecWithPersistenceManaged(context.Background(), store, paused.RunID, nil, &guiHopJITCaller{}, &runs)
	if err == nil || !strings.Contains(err.Error(), "HOP_RUN_ALREADY_ACTIVE") {
		t.Fatalf("duplicate resume err=%v", err)
	}
	after, err := store.LoadSnapshot(context.Background(), paused.RunID)
	if err != nil || after.StateSeq != before.StateSeq {
		t.Fatalf("duplicate resume changed state seq: before=%d after=%d err=%v", before.StateSeq, after.StateSeq, err)
	}
	runs.cancel(paused.RunID)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first resume err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first resume did not stop")
	}
}

func TestHopJITRunShutdownWaitsAndRejectsNewDrivers(t *testing.T) {
	var runs hopJITRunState
	cancelled := make(chan struct{})
	unregister, err := runs.register("run-1", func() { close(cancelled) })
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		runs.shutdown()
		close(done)
	}()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel active run")
	}
	unregister()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after unregister")
	}
	if _, err := runs.register("run-2", func() {}); err == nil {
		t.Fatal("closing run state accepted a new driver")
	}
}

func TestHopJITPersistenceSharedUntilAppClose(t *testing.T) {
	var state hopJITPersistenceState
	path := filepath.Join(t.TempDir(), "hopjit.db")
	first, err := state.open(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := state.open(path)
	if err != nil || first != second {
		t.Fatalf("store was not reused: first=%p second=%p err=%v", first, second, err)
	}
	switched, err := state.open(filepath.Join(t.TempDir(), "hopjit.db"))
	if err != nil || switched == first {
		t.Fatalf("switched=%p first=%p err=%v", switched, first, err)
	}
	if _, err := first.Exists(context.Background(), "run"); err != nil {
		t.Fatalf("path switch closed the old store: %v", err)
	}
	state.close()
	if _, err := first.Exists(context.Background(), "run"); err == nil {
		t.Fatal("closed store remained usable")
	}
	if _, err := switched.Exists(context.Background(), "run"); err == nil {
		t.Fatal("closed switched store remained usable")
	}
	reopened, err := state.open(path)
	if err != nil || reopened == first {
		t.Fatalf("reopened=%p first=%p err=%v", reopened, first, err)
	}
	state.close()
}

func TestStripHopJITCodeFence(t *testing.T) {
	if got := stripHopJITCodeFence("```json\n{\"a\":1}\n```"); !strings.Contains(got, `"a":1`) {
		t.Fatalf("got=%q", got)
	}
}
