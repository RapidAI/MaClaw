package v2

import (
	"context"
	"strings"
	"testing"
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
	config := TaskRunnerConfig{ProjectPath: "d:\\test", MaxRetries: 3}
	runner := NewTaskRunner(config, subAgent)

	results := runner.RunAll(context.Background(), tasks, nil, nil)
	if results[0].Status != TaskPassed {
		t.Errorf("status = %s, want passed after retries", results[0].Status)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
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
	if !containsSubstr(report, "1 通过") || !containsSubstr(report, "1 失败") {
		t.Errorf("report missing expected counts:\n%s", report)
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
