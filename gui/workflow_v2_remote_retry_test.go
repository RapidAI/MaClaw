package main

import (
	"strings"
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestFailedTaskItemsFromResults(t *testing.T) {
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "A"},
		{Index: 2, Title: "B"},
		{Index: 3, Title: "C"},
	}
	results := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskPassed},
		{TaskIndex: 2, Status: v2.TaskFailed, Error: "boom"},
		{TaskIndex: 3, Status: v2.TaskSkipped, Error: "cancelled"},
	}
	failed := failedTaskItemsFromResults(tasks, results)
	if len(failed) != 1 || failed[0].Index != 2 {
		t.Fatalf("failed = %#v, want only T2", failed)
	}
}

func TestMergeTaskRunResultsByIndex(t *testing.T) {
	base := []v2.TaskRunResult{
		{TaskIndex: 1, Status: v2.TaskPassed},
		{TaskIndex: 2, Status: v2.TaskFailed, Error: "old"},
	}
	updates := []v2.TaskRunResult{
		{TaskIndex: 2, Status: v2.TaskPassed, Summary: "fixed"},
	}
	merged := mergeTaskRunResultsByIndex(base, updates)
	if len(merged) != 2 {
		t.Fatalf("len=%d", len(merged))
	}
	if merged[1].Status != v2.TaskPassed || merged[1].Summary != "fixed" {
		t.Fatalf("merged T2 = %#v", merged[1])
	}
}

func TestFormatTaskRunResultsReport(t *testing.T) {
	previous, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { setAgentViewLang(previous) })
	setAgentViewLang("zh-Hans")
	report := formatTaskRunResultsReport([]v2.TaskRunResult{
		{TaskIndex: 1, Title: "ok", Status: v2.TaskPassed},
		{TaskIndex: 2, Title: "bad", Status: v2.TaskFailed, Error: "err"},
	})
	if strings.Contains(report, "## ") || strings.Contains(report, "执行报告") {
		t.Fatalf("report should be engineer prose, got %s", report)
	}
	if !strings.Contains(report, "bad") || !strings.Contains(report, "err") {
		t.Fatalf("missing failed step: %s", report)
	}
	setAgentViewLang("en")
	enReport := formatTaskRunResultsReport([]v2.TaskRunResult{
		{TaskIndex: 1, Title: "ok", Status: v2.TaskPassed},
	})
	if strings.Contains(enReport, "## ") || !strings.Contains(enReport, "Finished ok.") {
		t.Fatalf("en report = %s", enReport)
	}
	cancelled := formatTaskRunResultsReportEx([]v2.TaskRunResult{
		{TaskIndex: 1, Title: "x", Status: v2.TaskSkipped, Error: "cancelled"},
	}, true)
	if strings.Contains(cancelled, "## ") || !strings.Contains(cancelled, "Stopped") || !strings.Contains(cancelled, "x") {
		t.Fatalf("cancelled report = %s", cancelled)
	}
}
