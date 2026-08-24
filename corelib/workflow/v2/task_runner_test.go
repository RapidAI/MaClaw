package v2

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mockSubAgent(status TaskRunStatus) SubAgentFunc {
	return func(ctx context.Context, task *TaskItem, config TaskRunnerConfig, onToken func(string), onProgress func(string)) *TaskRunResult {
		return &TaskRunResult{
			TaskIndex: task.Index,
			Title:     task.Title,
			Status:    status,
			Summary:   "done",
		}
	}
}

func TestTaskRunner_WaveSizePropagated(t *testing.T) {
	var maxWave int32
	var sawParallel int32
	sub := func(ctx context.Context, task *TaskItem, config TaskRunnerConfig, onToken func(string), onProgress func(string)) *TaskRunResult {
		if config.WaveSize > int(atomic.LoadInt32(&maxWave)) {
			atomic.StoreInt32(&maxWave, int32(config.WaveSize))
		}
		if config.WaveSize > 1 {
			atomic.StoreInt32(&sawParallel, 1)
		}
		time.Sleep(20 * time.Millisecond)
		return &TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskPassed}
	}
	tasks := []*TaskItem{
		{Index: 1, Title: "A"},
		{Index: 2, Title: "B"},
		{Index: 3, Title: "C", DependsOn: []int{1, 2}},
	}
	runner := NewTaskRunner(TaskRunnerConfig{ProjectPath: t.TempDir(), MaxRetries: 0, MaxParallel: 2}, sub)
	results := runner.RunAll(context.Background(), tasks, nil, nil)
	if len(results) != 3 {
		t.Fatalf("results=%d", len(results))
	}
	if atomic.LoadInt32(&sawParallel) != 1 {
		t.Fatal("expected WaveSize>1 for independent A/B wave")
	}
	if atomic.LoadInt32(&maxWave) < 2 {
		t.Fatalf("maxWave=%d", maxWave)
	}
}

func TestTaskRunner_ParallelWaveAdmissionCanFailClosedToSerial(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32
	sub := func(ctx context.Context, task *TaskItem, config TaskRunnerConfig, onToken func(string), onProgress func(string)) *TaskRunResult {
		n := atomic.AddInt32(&concurrent, 1)
		for {
			current := atomic.LoadInt32(&maxConcurrent)
			if n <= current || atomic.CompareAndSwapInt32(&maxConcurrent, current, n) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
		return &TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskPassed}
	}
	tasks := []*TaskItem{{Index: 1, Title: "A"}, {Index: 2, Title: "B"}}
	runner := NewTaskRunner(TaskRunnerConfig{
		MaxParallel:        2,
		CanRunParallelWave: func([]*TaskItem) bool { return false },
	}, sub)
	results := runner.RunAll(context.Background(), tasks, nil, nil)
	if len(results) != 2 || results[0].Status != TaskPassed || results[1].Status != TaskPassed {
		t.Fatalf("results=%+v", results)
	}
	if got := atomic.LoadInt32(&maxConcurrent); got != 1 {
		t.Fatalf("max concurrent=%d, want serial fallback", got)
	}
}

func TestTaskRunner_AllPass(t *testing.T) {
	tasks := []*TaskItem{
		{Index: 1, Title: "Init"},
		{Index: 2, Title: "Core", DependsOn: []int{1}},
		{Index: 3, Title: "UI", DependsOn: []int{1, 2}},
	}
	config := TaskRunnerConfig{ProjectPath: "d:\\test", MaxRetries: 1}
	runner := NewTaskRunner(config, mockSubAgent(TaskPassed))

	results := runner.RunAll(context.Background(), tasks, nil, nil)
	if len(results) != 3 {
		t.Fatalf("got %d results", len(results))
	}
	for _, r := range results {
		if r.Status != TaskPassed {
			t.Errorf("T%d status = %s", r.TaskIndex, r.Status)
		}
	}
}

func TestTaskRunner_DependencySkip(t *testing.T) {
	tasks := []*TaskItem{
		{Index: 1, Title: "Init"},
		{Index: 2, Title: "Core", DependsOn: []int{1}},
	}
	config := TaskRunnerConfig{ProjectPath: "d:\\test", MaxRetries: 0}
	runner := NewTaskRunner(config, mockSubAgent(TaskFailed))

	results := runner.RunAll(context.Background(), tasks, nil, nil)
	if results[0].Status != TaskFailed {
		t.Errorf("T1 status = %s, want failed", results[0].Status)
	}
	if results[1].Status != TaskSkipped {
		t.Errorf("T2 status = %s, want skipped (dep failed)", results[1].Status)
	}
}

func TestTaskRunner_Retry(t *testing.T) {
	attempts := 0
	subAgent := func(ctx context.Context, task *TaskItem, config TaskRunnerConfig, onToken func(string), onProgress func(string)) *TaskRunResult {
		attempts++
		if attempts < 3 {
			return &TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskFailed, Error: "flaky"}
		}
		return &TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskPassed}
	}

	tasks := []*TaskItem{{Index: 1, Title: "Flaky"}}
	config := TaskRunnerConfig{ProjectPath: "d:\\test", MaxRetries: 3, AllowAutomaticRetries: true}
	runner := NewTaskRunner(config, subAgent)

	results := runner.RunAll(context.Background(), tasks, nil, nil)
	if results[0].Status != TaskPassed {
		t.Errorf("status = %s, want passed after retries", results[0].Status)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestTaskRunner_DefaultDoesNotReplayFailedCodingTask(t *testing.T) {
	attempts := 0
	runner := NewTaskRunner(TaskRunnerConfig{ProjectPath: t.TempDir(), MaxRetries: 3}, func(ctx context.Context, task *TaskItem, config TaskRunnerConfig, onToken func(string), onProgress func(string)) *TaskRunResult {
		attempts++
		return &TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskFailed, Error: "provider timeout after write may have started"}
	})
	results := runner.RunAll(context.Background(), []*TaskItem{{Index: 1, Title: "write"}}, nil, nil)
	if results[0].Status != TaskFailed || attempts != 1 {
		t.Fatalf("status=%s attempts=%d, want failed after one attempt", results[0].Status, attempts)
	}
}

func TestTaskRunner_TDDSkippedRedPhaseDoesNotStartGreenPhase(t *testing.T) {
	calls := 0
	runner := NewTaskRunner(TaskRunnerConfig{ProjectPath: t.TempDir(), TDDMode: true}, func(ctx context.Context, task *TaskItem, config TaskRunnerConfig, onToken func(string), onProgress func(string)) *TaskRunResult {
		calls++
		return &TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskSkipped, Error: "waiting_child"}
	})

	results := runner.RunAll(context.Background(), []*TaskItem{{Index: 1, Title: "write"}}, nil, nil)
	if len(results) != 1 || results[0].Status != TaskSkipped {
		t.Fatalf("results=%+v, want skipped red-phase handoff", results)
	}
	if calls != 1 {
		t.Fatalf("subagent calls=%d, want only the red phase; green phase must not start", calls)
	}
}

func TestTaskRunner_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	tasks := []*TaskItem{
		{Index: 1, Title: "A"},
		{Index: 2, Title: "B"},
	}
	config := TaskRunnerConfig{ProjectPath: "d:\\test"}
	runner := NewTaskRunner(config, mockSubAgent(TaskPassed))

	results := runner.RunAll(ctx, tasks, nil, nil)
	for _, r := range results {
		if r.Status != TaskSkipped {
			t.Errorf("T%d status = %s, want skipped (cancelled)", r.TaskIndex, r.Status)
		}
	}
}

func TestTaskRunner_FinalReport(t *testing.T) {
	tasks := []*TaskItem{
		{Index: 1, Title: "Good"},
		{Index: 2, Title: "Bad"},
	}
	callCount := 0
	subAgent := func(ctx context.Context, task *TaskItem, config TaskRunnerConfig, onToken func(string), onProgress func(string)) *TaskRunResult {
		callCount++
		if callCount == 1 {
			return &TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskPassed, Summary: "works"}
		}
		return &TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: TaskFailed, Error: "compile error"}
	}
	config := TaskRunnerConfig{ProjectPath: "d:\\test", MaxRetries: 0}
	runner := NewTaskRunner(config, subAgent)
	runner.RunAll(context.Background(), tasks, nil, nil)

	report := runner.FinalReport()
	if strings.Contains(report, "## ") || strings.Contains(report, "执行报告") || strings.Contains(report, "[ERR]") {
		t.Errorf("report should be engineer prose, got:\n%s", report)
	}
	if !containsSubstr(report, "works") || !containsSubstr(report, "compile error") {
		t.Errorf("report missing task outcome:\n%s", report)
	}
}

func TestValidateWritePath(t *testing.T) {
	tests := []struct {
		target  string
		project string
		wantErr bool
	}{
		{"d:\\game\\src\\main.cpp", "d:\\game", false},
		{"d:\\game\\build\\out.exe", "d:\\game", false},
		{"d:\\other\\evil.txt", "d:\\game", true},
		{"c:\\windows\\system32\\x", "d:\\game", true},
	}
	for _, tc := range tests {
		err := ValidateWritePath(tc.target, tc.project)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateWritePath(%q, %q) err=%v, wantErr=%v", tc.target, tc.project, err, tc.wantErr)
		}
	}
}

func TestIsDangerousCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"rm -rf /", true},
		{"rm -rf /*", true},
		{"rm -rf ./build", false},
		{"format c:", true},
		{"g++ main.cpp -o main", false},
		{"cmake --build .", false},
	}
	for _, tc := range tests {
		got := IsDangerousCommand(tc.cmd)
		if got != tc.want {
			t.Errorf("IsDangerousCommand(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func containsSubstr(s, sub string) bool {
	return strings.Contains(s, sub)
}
