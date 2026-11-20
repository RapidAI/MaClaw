package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInboundMCPDurableRestartDoesNotRerun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task")
	app := &App{mcpStateDir: dir}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: path,
		Status:      mcpStatusRunning,
		RequestID:   "mcp-1",
		RunID:       "run-keep",
		Provider:    "Old",
		Model:       "old-m",
		Workspace:   dir,
		Text:        "working",
	})
	app.mcpAppendLog(path, "mcp-1", mcpLogEvent{Kind: "progress", Text: "step ok"})
	app.mcpFinishRun(path, "mcp-1", mcpStatusCompleted, "done", "", false, "", nil, nil)
	req, claimed, err := app.mcpClaimRequest("req-replay", mcpRequestKindCommand, "hello", "")
	if err != nil || !claimed || req == nil {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
	app.mcpBindRequest(req, path)
	app.mcpCompleteRequest(req, nil)

	again := &App{mcpStateDir: dir}
	view, err := again.mcpGetTask(context.Background(), path, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.RunID != "run-keep" || view.CurrentRunID != "run-keep" || view.Provider != "Old" || view.Model != "old-m" || view.Status != mcpStatusCompleted {
		t.Fatalf("reloaded %+v", view)
	}
	page, err := again.mcpReadLog(context.Background(), path, "run-keep", 0)
	if err != nil || len(page.Events) != 1 || page.Events[0].Text != "step ok" || page.RunID != "run-keep" {
		t.Fatalf("log %+v err=%v", page, err)
	}
	replay, claimed, err := again.mcpClaimRequest("req-replay", mcpRequestKindCommand, "hello", "")
	if err != nil || claimed || replay == nil {
		t.Fatalf("replay claimed=%v err=%v", claimed, err)
	}
	if _, _, err := again.mcpClaimRequest("req-replay", mcpRequestKindCommand, "other", ""); err == nil || !mcpCodeIs(err, mcpCodeRequestConflict) || !strings.Contains(err.Error(), "另一条") {
		t.Fatalf("conflict %v", err)
	}
	state, err := again.mcpGetRequest(context.Background(), "req-replay")
	if err != nil || !state.Accepted || state.State != mcpRequestStateCompleted || state.PayloadHash == "" || state.Scope == nil || state.Scope.TaskID == "" {
		t.Fatalf("request %+v err=%v", state, err)
	}
}

func TestInboundMCPConcurrentPersistKeepsLatest(t *testing.T) {
	for round := 0; round < 8; round++ {
		dir := t.TempDir()
		app := &App{mcpStateDir: dir}
		app.mcpStoreRun(&mcpRunRecord{
			ProjectPath: "p", Status: mcpStatusRunning, RequestID: "req",
			RunID: "run-1", StatusVersion: 1, Text: "start",
		})
		var wg sync.WaitGroup
		const n = 24
		wg.Add(n + 1)
		for i := 0; i < n; i++ {
			go func(i int) {
				defer wg.Done()
				app.mcpAppendLog("p", "req", mcpLogEvent{Kind: "progress", Text: fmt.Sprintf("step-%d", i)})
			}(i)
		}
		go func() {
			defer wg.Done()
			app.mcpFinishRun("p", "req", mcpStatusCompleted, "done", "", false, "", nil, nil)
		}()
		wg.Wait()
		task, err := app.mcpGetTask(context.Background(), "p", "")
		if err != nil {
			t.Fatal(err)
		}
		live, err := app.mcpReadLog(context.Background(), "p", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		next := &App{mcpStateDir: dir}
		got, err := next.mcpGetTask(context.Background(), "p", "")
		if err != nil {
			t.Fatal(err)
		}
		page, err := next.mcpReadLog(context.Background(), "p", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != task.Status || got.Text != task.Text || got.StatusVersion != task.StatusVersion || len(page.Events) != len(live.Events) {
			t.Fatalf("round %d disk status=%s text=%q ver=%d logs=%d; memory status=%s text=%q ver=%d logs=%d",
				round, got.Status, got.Text, got.StatusVersion, len(page.Events),
				task.Status, task.Text, task.StatusVersion, len(live.Events))
		}
	}
}

func TestInboundMCPProgressLogDoesNotReplaceSnapshot(t *testing.T) {
	dir := t.TempDir()
	app := &App{mcpStateDir: dir}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusRunning, RequestID: "req",
		RunID: "run-1", StatusVersion: 1, Text: "start",
	})
	snapshot := mcpRunFilePath(dir, "p")
	before, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	app.mcpAppendLog("p", "req", mcpLogEvent{Kind: "progress", Text: "step ok"})
	after, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("progress log rewrote the snapshot")
	}
	if _, err := os.Stat(mcpRunLogPath(dir, "p")); err != nil {
		t.Fatal(err)
	}
	again := &App{mcpStateDir: dir}
	page, err := again.mcpReadLog(context.Background(), "p", "run-1", 0)
	if err != nil || len(page.Events) != 1 || page.Events[0].Text != "step ok" {
		t.Fatalf("log %+v err=%v", page, err)
	}
	view, err := again.mcpGetTask(context.Background(), "p", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.StatusVersion < 2 {
		t.Fatalf("version %d", view.StatusVersion)
	}
	app.mcpFinishRun("p", "req", mcpStatusCompleted, "done", "", false, "", nil, nil)
	if _, err := os.Stat(mcpRunLogPath(dir, "p")); !os.IsNotExist(err) {
		t.Fatalf("sidecar after finish: %v", err)
	}
}

func TestInboundMCPLogSidecarFoldsBackIntoSnapshot(t *testing.T) {
	dir := t.TempDir()
	app := &App{mcpStateDir: dir}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusRunning, RequestID: "req",
		RunID: "run-1", StatusVersion: 1, Text: "start",
	})
	snapshot := mcpRunFilePath(dir, "p")
	before, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	app.mcpAppendLog("p", "req", mcpLogEvent{Kind: "progress", Text: "step ok"})
	held, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, held) {
		t.Fatal("one progress line rewrote the snapshot")
	}
	side := mcpRunLogPath(dir, "p")
	info, err := os.Stat(side)
	if err != nil {
		t.Fatal(err)
	}
	app.mcpDiskMu.Lock()
	app.mcpFoldLogSidecarLocked(dir, "p", mcpLogEvent{Seq: 1}, 1, info.Size()+1)
	app.mcpDiskMu.Unlock()
	if mid, err := os.ReadFile(snapshot); err != nil || !bytes.Equal(before, mid) {
		t.Fatal("a sidecar under the bound rewrote the snapshot")
	}
	if _, err := os.Stat(side); err != nil {
		t.Fatalf("sidecar under the bound: %v", err)
	}
	app.mcpDiskMu.Lock()
	app.mcpFoldLogSidecarLocked(dir, "p", mcpLogEvent{Seq: 1}, 1, info.Size())
	app.mcpDiskMu.Unlock()
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatalf("sidecar after fold: %v", err)
	}
	folded, err := os.ReadFile(snapshot)
	if err != nil || bytes.Equal(before, folded) || !bytes.Contains(folded, []byte("step ok")) {
		t.Fatalf("snapshot did not absorb the line: %v", err)
	}
	again := &App{mcpStateDir: dir}
	page, err := again.mcpReadLog(context.Background(), "p", "run-1", 0)
	if err != nil || len(page.Events) != 1 || page.Events[0].Text != "step ok" {
		t.Fatalf("reloaded %+v err=%v", page, err)
	}
	if err := mcpAppendLogSidecar(side, mcpLogEvent{Seq: 99, Kind: "progress", Text: "later"}, 9); err != nil {
		t.Fatal(err)
	}
	app.mcpDiskMu.Lock()
	app.mcpFoldLogSidecarLocked(dir, "p", mcpLogEvent{Seq: 99}, 9, 1)
	app.mcpDiskMu.Unlock()
	if _, err := os.Stat(side); err != nil {
		t.Fatalf("unheld row deleted the sidecar: %v", err)
	}
	if kept, err := os.ReadFile(snapshot); err != nil || !bytes.Equal(kept, folded) {
		t.Fatal("unheld row rewrote the snapshot")
	}
}

func TestInboundMCPLogSidecarSkipsAPartialTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.log.jsonl")
	body := "{\"seq\":1,\"text\":\"old\"}\n{\"seq\":2,\"text\":\"kept\",\"status_version\":4,\"at\":\"2026-11-19T00:00:00Z\"}\n{\"seq\":3,\"text\":\"torn\""
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle := mcpDurableBundle{Current: &mcpRunSnapshot{LogSeq: 1, StatusVersion: 1, Logs: []mcpLogEvent{{Seq: 1, Text: "base"}}}}
	mcpMergeLogSidecar(&bundle, path)
	if bundle.Current.LogSeq != 2 || bundle.Current.StatusVersion != 4 || len(bundle.Current.Logs) != 2 || bundle.Current.Logs[1].Text != "kept" {
		t.Fatalf("merged %+v", bundle.Current)
	}
	if !bundle.SavedAt.Equal(time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("saved at %s", bundle.SavedAt)
	}
}

func TestInboundMCPLogSidecarKeepsOutOfOrderRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.log.jsonl")
	body := "{\"seq\":4,\"text\":\"d\",\"status_version\":6,\"at\":\"2026-11-19T00:00:04Z\"}\n{\"seq\":2,\"text\":\"b\",\"status_version\":3,\"at\":\"2026-11-19T00:00:02Z\"}\n{\"seq\":3,\"text\":\"c\",\"status_version\":5,\"at\":\"2026-11-19T00:00:03Z\"}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle := mcpDurableBundle{Current: &mcpRunSnapshot{LogSeq: 1, StatusVersion: 1, Logs: []mcpLogEvent{{Seq: 1, Text: "a"}}}}
	mcpMergeLogSidecar(&bundle, path)
	if bundle.Current.LogSeq != 4 || bundle.Current.StatusVersion != 6 || len(bundle.Current.Logs) != 4 {
		t.Fatalf("merged %+v", bundle.Current)
	}
	got := []string{bundle.Current.Logs[1].Text, bundle.Current.Logs[2].Text, bundle.Current.Logs[3].Text}
	if strings.Join(got, "") != "bcd" {
		t.Fatalf("order %v", got)
	}
}

func TestInboundMCPLogSidecarSeparatesATornTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.log.jsonl")
	if err := os.WriteFile(path, []byte(`{"seq":1,"text":"torn"`), 0o600); err != nil {
		t.Fatal(err)
	}
	ev := mcpLogEvent{Seq: 2, Kind: "progress", Text: "kept", At: "2026-11-19T00:00:02Z"}
	if err := mcpAppendLogSidecar(path, ev, 4); err != nil {
		t.Fatal(err)
	}
	bundle := mcpDurableBundle{Current: &mcpRunSnapshot{}}
	mcpMergeLogSidecar(&bundle, path)
	if bundle.Current.LogSeq != 2 || bundle.Current.StatusVersion != 4 || len(bundle.Current.Logs) != 1 || bundle.Current.Logs[0].Text != "kept" {
		t.Fatalf("merged %+v", bundle.Current)
	}
}

func TestInboundMCPLogGapSeesAMiddleHole(t *testing.T) {
	app := &App{mcpRuns: map[string]*mcpRunRecord{
		"p": {ProjectPath: "p", Status: mcpStatusCompleted, RunID: "run-1", logSeq: 4, logs: []mcpLogEvent{
			{Seq: 1, RunID: "run-1", Text: "a"},
			{Seq: 2, RunID: "run-1", Text: "b"},
			{Seq: 4, RunID: "run-1", Text: "d"},
		}},
	}}
	page, err := app.mcpReadLog(context.Background(), "p", "", 0)
	if err != nil || !page.Gap || len(page.Events) != 3 || page.NextCursor != 4 || page.Events[0].Seq != 1 {
		t.Fatalf("middle %+v err=%v", page, err)
	}
	// The hole is seq 3, after seq 2. A page that stops on seq 2 has not
	// reached it yet.
	if mcpLogSeqGap(app.mcpRuns["p"].logs, 0, 2) {
		t.Fatal("hole past this page")
	}
}

func TestInboundMCPScopedLogIgnoresOtherRoundSeq(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-a", RunID: "run-a"})
	app.mcpAppendLog("p", "mcp-a", mcpLogEvent{Kind: "progress", Text: "第一轮"})
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-b", RunID: "run-b"})
	app.mcpAppendLog("p", "mcp-b", mcpLogEvent{Kind: "progress", Text: "下一轮"})
	scoped, err := app.mcpReadLog(context.Background(), "p", "run-b", 0)
	if err != nil || scoped.Gap || len(scoped.Events) != 1 || scoped.Events[0].Text != "下一轮" || scoped.Events[0].Seq != 2 {
		t.Fatalf("scoped %+v err=%v", scoped, err)
	}
}

func TestInboundMCPAgedDoneRequestCanBeClaimed(t *testing.T) {
	app := &App{}
	old := time.Now().Add(-mcpStateRetention - time.Hour)
	ready := make(chan struct{})
	app.mcpReqs = map[string]*mcpClientRequest{
		"done-id": {
			Kind: mcpRequestKindCommand, Command: "old", done: true, updated: old,
			Hash: mcpRequestHash(mcpRequestKindCommand, "old", ""), State: mcpRequestStateCompleted,
		},
		"live-id": {
			Kind: mcpRequestKindCommand, Command: "stay", done: false, updated: old,
			Hash: mcpRequestHash(mcpRequestKindCommand, "stay", ""), State: mcpRequestStateRunning, ready: ready,
		},
	}
	app.mcpReqOrder = []string{"done-id", "live-id"}
	got, err := app.mcpGetRequest(context.Background(), "done-id")
	if err != nil || got.Accepted || got.State != mcpRequestStateNotFound {
		t.Fatalf("done %+v err=%v", got, err)
	}
	req, claimed, err := app.mcpClaimRequest("done-id", mcpRequestKindCommand, "new", "")
	if err != nil || !claimed || req == nil {
		t.Fatalf("reclaim claimed=%v err=%v", claimed, err)
	}
	live, claimed, err := app.mcpClaimRequest("live-id", mcpRequestKindCommand, "stay", "")
	if err != nil || claimed || live == nil || live.State != mcpRequestStateRunning {
		t.Fatalf("live claimed=%v req=%+v err=%v", claimed, live, err)
	}
}

func TestInboundMCPAgedExpiredTaskIsGone(t *testing.T) {
	app := &App{}
	app.mcpMarkTaskExpired("old-task", time.Now().Add(-mcpStateRetention-time.Hour))
	app.mcpMarkTaskExpired("fresh-task", time.Now())
	_, err := app.mcpGetTask(context.Background(), "old-task", "")
	if err == nil || !mcpCodeIs(err, mcpCodeTaskNotFound) {
		t.Fatalf("old %v", err)
	}
	_, err = app.mcpGetTask(context.Background(), "fresh-task", "")
	if err == nil || !mcpCodeIs(err, mcpCodeTaskExpired) {
		t.Fatalf("fresh %v", err)
	}
}

func TestInboundMCPAgedTombCanBeClaimed(t *testing.T) {
	app := &App{mcpReqTombs: map[string]mcpRequestTomb{
		"old-id":   {ID: "old-id", At: time.Now().Add(-mcpStateRetention - time.Hour)},
		"fresh-id": {ID: "fresh-id", At: time.Now()},
	}}
	old, err := app.mcpGetRequest(context.Background(), "old-id")
	if err != nil || old.Accepted || old.State != mcpRequestStateNotFound {
		t.Fatalf("old %+v err=%v", old, err)
	}
	fresh, err := app.mcpGetRequest(context.Background(), "fresh-id")
	if err != nil || fresh.Accepted || fresh.State != mcpRequestStateExpired {
		t.Fatalf("fresh %+v err=%v", fresh, err)
	}
	req, claimed, err := app.mcpClaimRequest("old-id", mcpRequestKindCommand, "go", "")
	if err != nil || !claimed || req == nil {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
}

func TestInboundMCPArtifactCursorAcceptsBackslash(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	list := mcpWalkArtifacts(root, mcpArtifactWalk{Cursor: `sub\a.txt`, Limit: 10})
	if len(list.Files) != 1 || list.Files[0].Path != "sub/b.txt" {
		t.Fatalf("page %+v", list.Files)
	}
}

func TestInboundMCPStatusVersionDoesNotGoBackwards(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusCompleted, RequestID: "mcp-a", RunID: "run-a", StatusVersion: 7, Text: "old"})
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-b", RunID: "run-b", StatusVersion: 1})
	live, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil {
		t.Fatal(err)
	}
	old, err := app.mcpGetTask(context.Background(), "p", "run-a")
	if err != nil {
		t.Fatal(err)
	}
	if live.RunID != "run-b" || live.StatusVersion <= old.StatusVersion || old.StatusVersion != 7 {
		t.Fatalf("live=%d old=%d run=%s", live.StatusVersion, old.StatusVersion, live.RunID)
	}
}

func TestInboundMCPLoadedRunningIsHostWait(t *testing.T) {
	dir := t.TempDir()
	app := &App{mcpStateDir: dir}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "live", Status: mcpStatusRunning, RequestID: "mcp-1", RunID: "run-live"})
	again := &App{mcpStateDir: dir}
	view, err := again.mcpGetTask(context.Background(), "live", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != mcpStatusCancelled || view.Stop == nil || view.Stop.Class != mcpStopUnconfirmed || view.Stop.Source != "host_wait" || view.Stop.Deadline != "" {
		t.Fatalf("loaded stop %+v", view.Stop)
	}
	if !mcpAutoResumeAllowed(view) {
		t.Fatal("host wait must stay resumable")
	}
}

func TestInboundMCPRequestTombDiffersFromMissing(t *testing.T) {
	dir := t.TempDir()
	app := &App{mcpStateDir: dir}
	for i := 0; i <= mcpClientRequestLimit; i++ {
		id := fmt.Sprintf("e-%d", i)
		item, claimed, err := app.mcpClaimRequest(id, mcpRequestKindCreate, "", "same")
		if err != nil || !claimed {
			t.Fatalf("claim %s claimed=%v err=%v", id, claimed, err)
		}
		app.mcpSucceedRequest(item, "p")
	}
	oldest, err := app.mcpGetRequest(context.Background(), "e-0")
	if err != nil || oldest.Accepted || oldest.State != mcpRequestStateExpired {
		t.Fatalf("tomb %+v err=%v", oldest, err)
	}
	missing, err := app.mcpGetRequest(context.Background(), "never-issued")
	if err != nil || missing.Accepted || missing.State != mcpRequestStateNotFound {
		t.Fatalf("missing %+v err=%v", missing, err)
	}
	again := &App{mcpStateDir: dir}
	reloaded, err := again.mcpGetRequest(context.Background(), "e-0")
	if err != nil || reloaded.Accepted || reloaded.State != mcpRequestStateExpired {
		t.Fatalf("reloaded tomb %+v err=%v", reloaded, err)
	}
}

func TestInboundMCPRetentionDropsOldTombs(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-mcpStateRetention - time.Hour)
	requests := mcpRequestDisk{
		Requests: []mcpRequestSnapshot{{
			ID: "old-req", Kind: mcpRequestKindCommand, Command: "x",
			ProjectPath: "p", Done: true, UpdatedAt: old, State: mcpRequestStateCompleted,
		}},
		Tombs: []mcpRequestTomb{{ID: "old-id", At: old}, {ID: "kept-id", At: time.Now()}},
	}
	if err := mcpWriteJSON(filepath.Join(dir, "requests.json"), requests); err != nil {
		t.Fatal(err)
	}
	expired := mcpExpiredDisk{Tasks: []mcpRequestTomb{
		{ID: "gone", At: old},
		{ID: "fresh", At: time.Now()},
	}}
	if err := mcpWriteJSON(filepath.Join(dir, "expired.json"), expired); err != nil {
		t.Fatal(err)
	}
	app := &App{mcpStateDir: dir}
	for _, id := range []string{"old-id", "old-req"} {
		state, err := app.mcpGetRequest(context.Background(), id)
		if err != nil || state.Accepted || state.State != mcpRequestStateNotFound {
			t.Fatalf("%s %+v err=%v", id, state, err)
		}
	}
	kept, err := app.mcpGetRequest(context.Background(), "kept-id")
	if err != nil || kept.Accepted || kept.State != mcpRequestStateExpired {
		t.Fatalf("kept %+v err=%v", kept, err)
	}
	claimed, accepted, err := app.mcpClaimRequest("old-id", mcpRequestKindCommand, "go", "")
	if err != nil || !accepted || claimed == nil {
		t.Fatalf("reclaim claimed=%v err=%v", accepted, err)
	}
	_, err = app.mcpGetTask(context.Background(), "gone", "")
	if err == nil || !mcpCodeIs(err, mcpCodeTaskNotFound) || mcpCodeIs(err, mcpCodeTaskExpired) {
		t.Fatalf("aged task %v", err)
	}
	_, err = app.mcpGetTask(context.Background(), "fresh", "")
	if err == nil || !mcpCodeIs(err, mcpCodeTaskExpired) {
		t.Fatalf("fresh expired %v", err)
	}
}

func TestInboundMCPLegacyTaskKeepsNullRun(t *testing.T) {
	app := &App{mcpRuns: map[string]*mcpRunRecord{
		"legacy": {ProjectPath: "legacy", Status: mcpStatusIdle, Text: "旧记录"},
	}}
	view, err := app.mcpGetTask(context.Background(), "legacy", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.RunID != "" || view.CurrentRunID != "" || view.TaskID != "legacy" {
		t.Fatalf("legacy identity %+v", view)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"run_id":null`) || !strings.Contains(string(raw), `"current_run_id":null`) {
		t.Fatalf("run ids %s", raw)
	}
	page, err := app.mcpReadLog(context.Background(), "legacy", "", 2)
	if err != nil || !page.Empty || len(page.Events) != 0 || page.NextCursor != 2 {
		t.Fatalf("empty log %+v err=%v", page, err)
	}
	_, err = app.mcpGetTask(context.Background(), "legacy", "missing-round")
	if err == nil || !mcpCodeIs(err, mcpCodeRunNotFound) || !strings.Contains(err.Error(), "没有这一轮执行") {
		t.Fatalf("unknown round %v", err)
	}
	_, err = app.mcpReadLog(context.Background(), "legacy", "missing-round", 0)
	if err == nil || !mcpCodeIs(err, mcpCodeRunNotFound) || !strings.Contains(err.Error(), "没有这一轮执行") {
		t.Fatalf("unknown log round %v", err)
	}
	_, err = app.mcpGetTask(context.Background(), "missing-task", "missing-round")
	if err == nil || !mcpCodeIs(err, mcpCodeTaskNotFound) || !strings.Contains(err.Error(), "没有找到这个任务") || strings.Contains(err.Error(), "没有这一轮执行") {
		t.Fatalf("unknown task %v", err)
	}
	app.mcpMarkTaskExpired("old-task", time.Now())
	_, err = app.mcpGetTask(context.Background(), "old-task", "")
	if err == nil || !mcpCodeIs(err, mcpCodeTaskExpired) || !strings.Contains(err.Error(), "保留期") {
		t.Fatalf("expired %v", err)
	}
}

func TestInboundMCPFreshViewJSON(t *testing.T) {
	raw, err := json.Marshal(mcpTaskView{})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"task_id", "project_path", "current_run_id", "run_id", "status_version", "status", "started_at", "finished_at", "steps", "stop"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("missing %s in %s", key, raw)
		}
	}
	if doc["task_id"] != nil || doc["current_run_id"] != nil || doc["run_id"] != nil || doc["stop"] != nil || doc["status_version"].(float64) != 0 {
		t.Fatalf("fresh %s", raw)
	}
	steps, ok := doc["steps"].([]any)
	if !ok || len(steps) != 0 {
		t.Fatalf("steps %#v", doc["steps"])
	}
}

func TestInboundMCPConfirmationPhraseIsNotCompleted(t *testing.T) {
	app := &App{}
	asked := app.mcpPublishTaskView(mcpTaskView{ProjectPath: "p", Status: mcpStatusCompleted, Text: "需要确认执行计划"})
	if asked.Status != mcpStatusNeedsConfirmation || asked.EndReason != mcpStatusNeedsConfirmation {
		t.Fatalf("confirmation %+v", asked)
	}
	plain := app.mcpPublishTaskView(mcpTaskView{ProjectPath: "p", Status: mcpStatusCompleted, Text: "最后的回答"})
	if plain.Status != mcpStatusCompleted {
		t.Fatalf("plain answer %+v", plain)
	}
	blocked := app.mcpPublishTaskView(mcpTaskView{ProjectPath: "p", Status: "mystery"})
	if blocked.Status != mcpStatusBlockedUnknown || blocked.StatusRaw != "mystery" {
		t.Fatalf("unknown %+v", blocked)
	}
	if mcpDisplayStatus("", "", "最后的回答") != mcpStatusCompleted {
		t.Fatal("empty workflow still completes an ordinary answer")
	}
	if mcpDisplayStatus("", "completed", "需要确认执行计划") != mcpStatusNeedsConfirmation {
		t.Fatal("workflow completion lost the confirmation")
	}
}

func TestInboundMCPStatusVersionConflictDoesNotStart(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusCompleted, RequestID: "mcp-1", RunID: "run-1", StatusVersion: 4, Text: "done"})
	ctx := mcpContextWithOpts(context.Background(), mcpCallOpts{HasExpected: true, ExpectedVersion: 3})
	if _, err := app.mcpSendCommand(ctx, "p", "继续", "", "req-cas", false); err == nil || !mcpCodeIs(err, mcpCodeStatusConflict) || !strings.Contains(err.Error(), "状态版本已经变化") {
		t.Fatalf("send %v", err)
	}
	state, err := app.mcpGetRequest(context.Background(), "req-cas")
	if err != nil || state.Accepted || state.State != mcpRequestStateNotFound {
		t.Fatalf("request consumed %+v err=%v", state, err)
	}
	view, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil || view.Status != mcpStatusCompleted || view.StatusVersion != 4 || view.RunID != "run-1" {
		t.Fatalf("mutated %+v err=%v", view, err)
	}
	if _, err := app.mcpRetrySteps(ctx, "p", []string{"T1"}, "", "req-retry", false); err == nil || !mcpCodeIs(err, mcpCodeStatusConflict) {
		t.Fatalf("retry %v", err)
	}
	retryState, err := app.mcpGetRequest(context.Background(), "req-retry")
	if err != nil || retryState.Accepted {
		t.Fatalf("retry consumed %+v err=%v", retryState, err)
	}
	absent := mcpContextWithOpts(context.Background(), mcpCallOpts{HasExpected: true, ExpectedVersion: 0})
	if _, err := app.mcpSendCommand(absent, "nobody", "继续", "", "req-zero", false); err == nil || !mcpCodeIs(err, mcpCodeStatusConflict) {
		t.Fatalf("version 0 %v", err)
	}
}

func TestInboundMCPCommitRechecksStatusVersion(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "old",
		RunID: "run-1", StatusVersion: 5, Text: "need",
	})
	_, _, err := app.mcpCommitRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusRunning, RequestID: "new",
	}, 0, true, mcpCallOpts{HasExpected: true, ExpectedVersion: 4})
	if !mcpCodeIs(err, mcpCodeStatusConflict) {
		t.Fatalf("stale commit %v", err)
	}
	view, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil || view.Status != mcpStatusNeedsConfirmation || view.StatusVersion != 5 || view.RunID != "run-1" || view.RequestID != "old" {
		t.Fatalf("stale commit changed the run %+v err=%v", view, err)
	}
	_, _, err = app.mcpCommitRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusRunning, RequestID: "new",
	}, 0, true, mcpCallOpts{HasExpected: true, ExpectedVersion: 5})
	if err != nil {
		t.Fatal(err)
	}
	view, err = app.mcpGetTask(context.Background(), "p", "")
	if err != nil || view.Status != mcpStatusRunning || view.StatusVersion <= 5 || view.RequestID != "new" {
		t.Fatalf("matching version did not install %+v err=%v", view, err)
	}
}

func TestInboundMCPDryRunDoesNotStart(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusCompleted, RequestID: "mcp-1", RunID: "run-1", StatusVersion: 2, Workspace: t.TempDir()})
	ctx := mcpContextWithOpts(context.Background(), mcpCallOpts{DryRun: true, HasExpected: true, ExpectedVersion: 2})
	view, err := app.mcpSendCommand(ctx, "p", "继续", "", "req-dry", false)
	if err != nil || !view.DryRun || view.Status == mcpStatusRunning || !view.ResumeOK {
		t.Fatalf("dry %+v err=%v", view, err)
	}
	state, err := app.mcpGetRequest(context.Background(), "req-dry")
	if err != nil || state.Accepted {
		t.Fatalf("dry consumed %+v err=%v", state, err)
	}
	stored, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil || stored.Status != mcpStatusCompleted || stored.RunID != "run-1" {
		t.Fatalf("stored %+v err=%v", stored, err)
	}
	preview, err := app.mcpRetrySteps(ctx, "p", []string{"T1"}, "", "req-retry-dry", false)
	if err != nil || !preview.DryRun {
		t.Fatalf("retry dry %+v err=%v", preview, err)
	}
}

func TestInboundMCPUserStopBlocksAutoResume(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath:   "p",
		Status:        mcpStatusCancelled,
		RequestID:     "mcp-1",
		RunID:         "run-stop",
		StatusVersion: 2,
		Stop:          &mcpStopView{Class: mcpStopUser, Source: "user", Reason: "用户停止"},
	})
	view, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil {
		t.Fatal(err)
	}
	if mcpAutoResumeAllowed(view) {
		t.Fatal("user stop must not auto resume")
	}
	ctx := mcpContextWithOpts(context.Background(), mcpCallOpts{DryRun: true})
	dry, err := app.mcpSendCommand(ctx, "p", "继续", "", "req-stop-dry", false)
	if err != nil || dry.ResumeOK || dry.Status != mcpStatusCancelled || dry.RunID != "run-stop" {
		t.Fatalf("dry %+v err=%v", dry, err)
	}
	_, err = app.mcpSendCommand(context.Background(), "p", "__scope_approval__ mcp-contract-no-such allow_dir", "", "", false)
	if err == nil || strings.Contains(err.Error(), "状态版本已经变化") {
		t.Fatalf("explicit send %v", err)
	}
	after, err := app.mcpGetTask(context.Background(), "p", "")
	if err != nil || after.Status != mcpStatusCancelled || after.RunID != "run-stop" {
		t.Fatalf("explicit send changed the run %+v err=%v", after, err)
	}
}

func TestInboundMCPArtifactPageAndExpectedSHA(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".keep", "note.txt"), []byte("dot"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "report.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: root, Status: mcpStatusCompleted, RequestID: "mcp-1", RunID: "run-a", Workspace: root})
	visible, err := app.mcpListArtifacts(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !mcpArtifactNames(visible, "report.txt") || mcpArtifactNames(visible, ".keep/note.txt") {
		t.Fatalf("default list %+v", visible.Files)
	}
	ctx := mcpContextWithOpts(context.Background(), mcpCallOpts{IncludeHidden: true, HasLimit: true, Limit: 1})
	first, err := app.mcpListArtifacts(ctx, root)
	if err != nil || len(first.Files) != 1 || first.Files[0].Path != ".keep/note.txt" || first.NextCursor != ".keep/note.txt" || first.TruncatedReason != "page_limit" {
		t.Fatalf("first page %+v err=%v", first, err)
	}
	nextCtx := mcpContextWithOpts(context.Background(), mcpCallOpts{IncludeHidden: true, HasLimit: true, Limit: 1, ArtifactCursor: first.NextCursor})
	second, err := app.mcpListArtifacts(nextCtx, root)
	if err != nil || len(second.Files) != 1 || second.Files[0].Path != "report.txt" {
		t.Fatalf("second page %+v err=%v", second, err)
	}
	body, err := app.mcpReadArtifact(context.Background(), root, "report.txt", false)
	if err != nil || !body.Snapshot || body.Text != "hello" || body.SHA256 == "" {
		t.Fatalf("read %+v err=%v", body, err)
	}
	bad := mcpContextWithOpts(context.Background(), mcpCallOpts{HasExpectedSHA: true, ExpectedSHA: "sha256:deadbeef"})
	if _, err := app.mcpReadArtifact(bad, root, "report.txt", false); err == nil || !mcpCodeIs(err, mcpCodeArtifactStale) || !strings.Contains(err.Error(), "哈希不一致") {
		t.Fatalf("stale read %v", err)
	}
	good := mcpContextWithOpts(context.Background(), mcpCallOpts{HasExpectedSHA: true, ExpectedSHA: "SHA256:" + body.SHA256})
	again, err := app.mcpReadArtifact(good, root, "report.txt", false)
	if err != nil || again.Text != "hello" || !again.Snapshot {
		t.Fatalf("matching read %+v err=%v", again, err)
	}
	packed, err := app.mcpPackArtifacts(context.Background(), root, []string{"report.txt"})
	if err != nil || !packed.Snapshot || packed.ArchiveSHA256 == "" || packed.Content == "" {
		t.Fatalf("pack %+v err=%v", packed, err)
	}
	wrong := mcpContextWithOpts(context.Background(), mcpCallOpts{HasExpectedSHA: true, ExpectedSHA: "0000"})
	if _, err := app.mcpPackArtifacts(wrong, root, []string{"report.txt"}); err == nil || !mcpCodeIs(err, mcpCodeArtifactStale) {
		t.Fatalf("stale pack %v", err)
	}
}

func TestInboundMCPCapabilitiesLeaveProbeNull(t *testing.T) {
	info, err := (&App{}).mcpCapabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.SchemaVersion != "mcp-task-v2" || info.Commit != "" || info.ServerBuild == "" {
		t.Fatalf("identity %+v", info)
	}
	if info.CurrentModel == nil || info.CurrentModel.ProbeOK != nil || info.CurrentModel.Configured {
		t.Fatalf("probe %+v", info.CurrentModel)
	}
	if info.Persistence == nil || !info.Persistence.Durable || info.Persistence.Retention != "7d" {
		t.Fatalf("persistence %+v", info.Persistence)
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"probe_ok":null`) || !strings.Contains(string(raw), `"commit":""`) {
		t.Fatalf("wire %s", raw)
	}
}

func TestInboundMCPProtocolOptionalArgs(t *testing.T) {
	capture := &mcpOptCapture{}
	handler := &mcpHTTP{api: capture, token: func() string { return "t" }}
	_, rpcErr := handler.callTool(context.Background(), json.RawMessage(`{"name":"send_command","arguments":{"project_path":"p","command":"继续","expected_status_version":0,"dry_run":true}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	capture.mu.Lock()
	opts := capture.opts
	capture.mu.Unlock()
	if !opts.HasExpected || opts.ExpectedVersion != 0 || !opts.DryRun {
		t.Fatalf("send opts %+v", opts)
	}
	_, rpcErr = handler.callTool(context.Background(), json.RawMessage(`{"name":"list_artifacts","arguments":{"project_path":"p","cursor":12,"limit":2,"include_hidden":true,"exclude_dirs":["tmp"]}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	capture.mu.Lock()
	opts = capture.opts
	capture.mu.Unlock()
	if opts.ArtifactCursor != "12" || !opts.HasLimit || opts.Limit != 2 || !opts.IncludeHidden || len(opts.ExcludeDirs) != 1 || opts.ExcludeDirs[0] != "tmp" {
		t.Fatalf("list opts %+v", opts)
	}
	_, rpcErr = handler.callTool(context.Background(), json.RawMessage(`{"name":"read_artifact","arguments":{"project_path":"p","path":"report.txt","expected_sha256":"abc"}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	capture.mu.Lock()
	opts = capture.opts
	capture.mu.Unlock()
	if !opts.HasExpectedSHA || opts.ExpectedSHA != "abc" {
		t.Fatalf("read opts %+v", opts)
	}
	errAPI := &mcpHTTP{api: &mcpErrAPI{}, token: func() string { return "t" }}
	result, rpcErr := errAPI.callTool(context.Background(), json.RawMessage(`{"name":"get_task","arguments":{"project_path":"p","run_id":"run-x"}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	text, isError := mcpResultText(t, result)
	if !isError || !strings.Contains(text, `"code":"run_not_found"`) || !strings.Contains(text, "没有这一轮执行") || !strings.Contains(text, `"task_id":"p"`) || !strings.Contains(text, `"run_id":"run-x"`) {
		t.Fatalf("coded tool error %s", text)
	}
	for _, def := range mcpToolDefinitions() {
		if def["name"] != "send_command" {
			continue
		}
		schema := def["inputSchema"].(map[string]any)
		props := schema["properties"].(map[string]any)
		if _, ok := props["project_path"]; !ok {
			t.Fatal("dropped project_path")
		}
		if _, ok := props["command"]; !ok {
			t.Fatal("dropped command")
		}
		if _, ok := props["expected_status_version"]; !ok {
			t.Fatal("missing expected_status_version")
		}
		required := schema["required"].([]string)
		if len(required) != 2 || required[0] != "project_path" || required[1] != "command" {
			t.Fatalf("required %#v", required)
		}
	}
}

func TestInboundMCPSessionPathDoesNotDoubleBump(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-1", Workspace: `D:\repo`, RunID: "run-1"})
	before, ok := app.mcpViewFromRegistry("p")
	if !ok {
		t.Fatal("missing run")
	}
	app.mcpSetRunSessionPath("p", "mcp-1", `D:\repo`)
	same, _ := app.mcpViewFromRegistry("p")
	if same.StatusVersion != before.StatusVersion || same.SessionPath != "" {
		t.Fatalf("same directory bumped %d -> %+v", before.StatusVersion, same)
	}
	app.mcpSetRunSessionPath("p", "mcp-1", `D:\session`)
	moved, _ := app.mcpViewFromRegistry("p")
	if moved.SessionPath == "" || moved.StatusVersion != before.StatusVersion+1 {
		t.Fatalf("session %+v", moved)
	}
}

func TestInboundMCPCommandSessionStaysQueryable(t *testing.T) {
	app := &App{}
	missing := "计划引用的输入文件不存在，无法开始实施；当前计划已恢复为待确认状态：\n\n• users.noreply.github.com\n\n请补齐文件或修正计划后重试。\n\n## 计划正文\n\n" + mcpPlanConfirmHint
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-a",
		RunID: "run-a", StatusVersion: 3, Text: missing,
		Commands:         []string{"/plan approve", "/plan skip", "/plan reject"},
		ConfirmationKind: mcpConfirmKindPlan,
	})
	req, claimed, err := app.mcpClaimRequest("cmd-1", mcpRequestKindCommand, "approve", "")
	if err != nil || !claimed || req == nil {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
	app.mcpBindRequest(req, "p")
	app.mcpNoteRequestRun("p", "run-a")
	app.mcpCompleteRequest(req, nil)

	view, err := app.mcpGetTask(context.Background(), "p", "run-a")
	if err != nil || view.Status != mcpStatusNeedsConfirmation || view.RunID != "run-a" {
		t.Fatalf("session %+v err=%v", view, err)
	}
	if !strings.Contains(view.Outcome, "users.noreply.github.com") || strings.Contains(view.Outcome, "## 计划正文") || !strings.Contains(view.Outcome, "commands") {
		t.Fatalf("outcome %q", view.Outcome)
	}

	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusRunning, RequestID: "mcp-b",
		RunID: "run-b", Text: "should not leak", ConfirmationID: "mcf_live",
	})
	next, claimed, err := app.mcpClaimRequest("cmd-2", mcpRequestKindCommand, "skip", "")
	if err != nil || !claimed || next == nil {
		t.Fatalf("next claimed=%v err=%v", claimed, err)
	}
	app.mcpBindRequest(next, "p")
	app.mcpNoteRequestRun("p", "run-b")

	state, err := app.mcpGetRequest(context.Background(), "cmd-1")
	if err != nil || state.RunID != "run-a" || state.Task == nil || state.Task.RunID != "run-a" {
		t.Fatalf("request %+v err=%v", state, err)
	}
	if !strings.Contains(state.Task.Outcome, "users.noreply.github.com") || strings.Contains(state.Task.Outcome, "should not leak") {
		t.Fatalf("later round hid this session: %q", state.Task.Outcome)
	}
	live, ok := app.mcpViewFromRegistry("p")
	if !ok || live.RunID != "run-b" || live.ConfirmationID != "mcf_live" {
		t.Fatalf("old session rewrote the live round %+v", live)
	}
	replay, err := app.mcpReplayRequest(context.Background(), req)
	if err != nil || replay.RunID != "run-a" || !strings.Contains(replay.Outcome, "users.noreply.github.com") {
		t.Fatalf("replay %+v err=%v", replay, err)
	}
	receipt := app.mcpPublishTaskView(mcpTaskView{
		ProjectPath: "p", Status: mcpStatusRunning, RunID: "run-b", CurrentRunID: "run-b", Text: "上一轮回答",
	})
	if !strings.Contains(receipt.Outcome, "run-b") || !strings.Contains(receipt.Outcome, "还没结束") || strings.Contains(receipt.Outcome, "上一轮回答") {
		t.Fatalf("receipt %q", receipt.Outcome)
	}
	defs, err := json.Marshal(mcpToolDefinitions())
	if err != nil || !strings.Contains(string(defs), "会话标识") {
		t.Fatalf("schema %v", err)
	}
}

func TestInboundMCPGoneCommandSessionIsNotStillRunning(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-old",
		RunID: "run-old", Text: "计划引用的输入文件不存在",
	})
	req, claimed, err := app.mcpClaimRequest("cmd-old", mcpRequestKindCommand, "approve", "")
	if err != nil || !claimed || req == nil {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
	app.mcpBindRequest(req, "p")
	app.mcpNoteRequestRun("p", "run-old")
	app.mcpCompleteRequest(req, nil)

	failedReq, claimed, err := app.mcpClaimRequest("cmd-fail", mcpRequestKindCommand, "fail", "")
	if err != nil || !claimed || failedReq == nil {
		t.Fatalf("fail claim claimed=%v err=%v", claimed, err)
	}
	app.mcpBindRequest(failedReq, "p")
	app.mcpNoteRequestRun("p", "run-old")
	app.mcpCompleteRequest(failedReq, errors.New("这一轮启动失败"))

	for i := 0; i <= mcpRunArchiveLimit; i++ {
		app.mcpStoreRun(&mcpRunRecord{
			ProjectPath: "p", Status: mcpStatusCompleted, RequestID: fmt.Sprintf("mcp-%d", i),
			RunID: fmt.Sprintf("run-%d", i),
		})
	}
	if _, ok := app.mcpViewForRun("p", "run-old"); ok {
		t.Fatal("evicted session still present")
	}
	state, err := app.mcpGetRequest(context.Background(), "cmd-old")
	if err == nil || !mcpCodeIs(err, mcpCodeRunNotFound) || !strings.Contains(err.Error(), "没有这一轮执行") {
		t.Fatalf("gone session %+v err=%v", state, err)
	}
	failedState, err := app.mcpGetRequest(context.Background(), "cmd-fail")
	if err != nil || failedState.State != mcpRequestStateFailed || !strings.Contains(failedState.Error, "这一轮启动失败") {
		t.Fatalf("failed request %+v err=%v", failedState, err)
	}

	bare, claimed, err := app.mcpClaimRequest("cmd-bare", mcpRequestKindCommand, "bare", "")
	if err != nil || !claimed || bare == nil {
		t.Fatalf("bare claimed=%v err=%v", claimed, err)
	}
	app.mcpBindRequest(bare, "p")
	app.mcpCompleteRequest(bare, nil)
	live, err := app.mcpGetRequest(context.Background(), "cmd-bare")
	if err != nil || live.Task == nil || live.Task.RunID == "" || live.Task.RunID == "run-old" {
		t.Fatalf("unbound request %+v err=%v", live, err)
	}

	orphan, claimed, err := app.mcpClaimRequest("cmd-orphan", mcpRequestKindCommand, "orphan", "")
	if err != nil || !claimed || orphan == nil {
		t.Fatalf("orphan claimed=%v err=%v", claimed, err)
	}
	app.mcpBindRequest(orphan, "no-such")
	app.mcpCompleteRequest(orphan, nil)
	waiting, err := app.mcpGetRequest(context.Background(), "cmd-orphan")
	if err != nil || waiting.State != mcpRequestStateRunning || waiting.Task != nil || waiting.RunID != "" {
		t.Fatalf("unpublished request %+v err=%v", waiting, err)
	}
	for _, def := range mcpToolDefinitions() {
		if def["name"] != "get_request" {
			continue
		}
		desc, _ := def["description"].(string)
		if !strings.Contains(desc, "run_not_found") || !strings.Contains(desc, "没有这一轮执行") || strings.Contains(desc, "task 为空") {
			t.Fatalf("schema %s", desc)
		}
	}
}

func TestInboundMCPOlderSessionKeepsItsOwnResult(t *testing.T) {
	app := &App{}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusWaitingReview, RequestID: "mcp-old",
		RunID: "run-old", Text: "子任务结果待审阅",
	})
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusCompleted, RequestID: "mcp-new",
		RunID: "run-new", Text: "这一轮写好了",
	})
	owner := projectSessionOwnerID("p")
	app.imHandler = &IMMessageHandler{}
	app.imHandler.stickyCodingWorkbenchMemory.Store(stickyCodingMemoryKey(owner), stickyCodingWorkbenchMemory{
		PendingPlanJSON:     `{"tasks":[{"index":1}]}`,
		PendingPlanMarkdown: "## 改登录",
	})
	req, claimed, err := app.mcpClaimRequest("cmd-old", mcpRequestKindCommand, "review", "")
	if err != nil || !claimed || req == nil {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
	app.mcpBindRequest(req, "p")
	app.mcpNoteRequestRun("p", "run-old")
	app.mcpCompleteRequest(req, nil)

	live, err := app.mcpGetTask(context.Background(), "p", "run-new")
	if err != nil || live.Status != mcpStatusNeedsConfirmation || live.RunID != "run-new" || live.CurrentRunID != "run-new" {
		t.Fatalf("live %+v err=%v", live, err)
	}
	if !strings.Contains(live.Text, "## 改登录") || !strings.Contains(live.Text, "这一轮写好了") || !containsAll(live.Commands, "/plan approve", "/plan skip", "/plan reject") {
		t.Fatalf("live plan %+v", live)
	}
	liveID := live.ConfirmationID
	if !strings.HasPrefix(liveID, mcpConfirmIDPrefix) {
		t.Fatalf("live confirmation %q", liveID)
	}

	old, err := app.mcpGetTask(context.Background(), "p", "run-old")
	if err != nil || old.Status != mcpStatusWaitingReview || old.RunID != "run-old" || old.CurrentRunID != "run-new" {
		t.Fatalf("old session %+v err=%v", old, err)
	}
	if strings.Contains(old.Text, "## 改登录") || strings.Contains(old.Outcome, "## 改登录") || len(old.Commands) != 0 || old.ConfirmationID != "" {
		t.Fatalf("old session took the live plan %+v", old)
	}
	if !strings.Contains(old.Text, "子任务结果待审阅") || !strings.Contains(old.Outcome, "子任务结果待审阅") {
		t.Fatalf("old result %+v", old)
	}
	state, err := app.mcpGetRequest(context.Background(), "cmd-old")
	if err != nil || state.Task == nil || state.Task.Status != mcpStatusWaitingReview || state.Task.RunID != "run-old" || state.Task.CurrentRunID != "run-new" {
		t.Fatalf("request %+v err=%v", state, err)
	}
	if strings.Contains(state.Task.Outcome, "## 改登录") {
		t.Fatalf("request took the live plan %q", state.Task.Outcome)
	}
	again, err := app.mcpGetTask(context.Background(), "p", "run-new")
	if err != nil || again.ConfirmationID != liveID || again.Status != mcpStatusNeedsConfirmation {
		t.Fatalf("reading the old session rewrote the live card %+v err=%v", again, err)
	}
}

func TestInboundMCPOlderSessionKeepsItsConfirmationID(t *testing.T) {
	app := &App{}
	commands := mcpPlanDecisionCommands()
	fp := mcpConfirmationFingerprint(mcpConfirmKindPlan, commands)
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-old",
		RunID: "run-old", Text: "计划引用的输入文件不存在",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
		ConfirmationID: "mcf_hist", confirmFingerprint: fp,
	})
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-new",
		RunID: "run-new", Text: "当前这一轮",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
		ConfirmationID: "mcf_live", confirmFingerprint: fp,
	})
	old, err := app.mcpGetTask(context.Background(), "p", "run-old")
	if err != nil || old.Status != mcpStatusCancelled || old.ConfirmationID != "mcf_hist" || old.RunID != "run-old" || old.CurrentRunID != "run-new" {
		t.Fatalf("stored id %+v err=%v", old, err)
	}
	if !containsAll(old.Commands, "/plan approve", "/plan skip", "/plan reject") || strings.Contains(old.Text, "当前这一轮") {
		t.Fatalf("old commands %+v", old)
	}
	live, err := app.mcpGetTask(context.Background(), "p", "run-new")
	if err != nil || live.ConfirmationID != "mcf_live" || live.RunID != "run-new" {
		t.Fatalf("live id %+v err=%v", live, err)
	}
	oldAgain, err := app.mcpGetTask(context.Background(), "p", "run-old")
	if err != nil || oldAgain.ConfirmationID != "mcf_hist" {
		t.Fatalf("stored id changed %+v err=%v", oldAgain, err)
	}

	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "q", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-q-old",
		RunID: "run-q-old", Text: "还没有编号",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
	})
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "q", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-q-new",
		RunID: "run-q-new", Text: "q 的当前轮",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
		ConfirmationID: "mcf_qlive", confirmFingerprint: fp,
	})
	minted, err := app.mcpGetTask(context.Background(), "q", "run-q-old")
	if err != nil || !strings.HasPrefix(minted.ConfirmationID, mcpConfirmIDPrefix) || minted.ConfirmationID == "mcf_qlive" {
		t.Fatalf("mint %+v err=%v", minted, err)
	}
	if _, err := app.mcpGetTask(context.Background(), "q", "run-q-new"); err != nil {
		t.Fatal(err)
	}
	mintedAgain, err := app.mcpGetTask(context.Background(), "q", "run-q-old")
	if err != nil || mintedAgain.ConfirmationID != minted.ConfirmationID {
		t.Fatalf("minted id changed %q -> %+v err=%v", minted.ConfirmationID, mintedAgain, err)
	}
	qLive, err := app.mcpGetTask(context.Background(), "q", "run-q-new")
	if err != nil || qLive.ConfirmationID != "mcf_qlive" {
		t.Fatalf("q live %+v err=%v", qLive, err)
	}
}

func TestInboundMCPConfirmationIDSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task")
	app := &App{mcpStateDir: dir}
	commands := mcpPlanDecisionCommands()
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: path, Status: mcpStatusNeedsConfirmation, RequestID: "mcp-old",
		RunID: "run-old", Text: "计划引用的输入文件不存在",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
	})
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: path, Status: mcpStatusNeedsConfirmation, RequestID: "mcp-new",
		RunID: "run-new", Text: "下一轮",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
	})
	old, err := app.mcpGetTask(context.Background(), path, "run-old")
	if err != nil || old.Status != mcpStatusCancelled || old.RunID != "run-old" || old.CurrentRunID != "run-new" {
		t.Fatalf("old %+v err=%v", old, err)
	}
	live, err := app.mcpGetTask(context.Background(), path, "run-new")
	if err != nil || live.Status != mcpStatusNeedsConfirmation || live.RunID != "run-new" {
		t.Fatalf("live %+v err=%v", live, err)
	}
	if !strings.HasPrefix(old.ConfirmationID, mcpConfirmIDPrefix) || !strings.HasPrefix(live.ConfirmationID, mcpConfirmIDPrefix) || old.ConfirmationID == live.ConfirmationID {
		t.Fatalf("minted old=%q live=%q", old.ConfirmationID, live.ConfirmationID)
	}
	if !strings.Contains(old.Text, "计划引用的输入文件不存在") || strings.Contains(old.Text, "下一轮") {
		t.Fatalf("old text %q", old.Text)
	}

	again := &App{mcpStateDir: dir}
	oldAgain, err := again.mcpGetTask(context.Background(), path, "run-old")
	if err != nil || oldAgain.ConfirmationID != old.ConfirmationID || oldAgain.StatusVersion != old.StatusVersion || oldAgain.CurrentRunID != "run-new" {
		t.Fatalf("reloaded old %+v err=%v want id %q version %d", oldAgain, err, old.ConfirmationID, old.StatusVersion)
	}
	liveAgain, err := again.mcpGetTask(context.Background(), path, "run-new")
	if err != nil || liveAgain.ConfirmationID != live.ConfirmationID || liveAgain.StatusVersion != live.StatusVersion {
		t.Fatalf("reloaded live %+v err=%v want id %q version %d", liveAgain, err, live.ConfirmationID, live.StatusVersion)
	}

	used := liveAgain.ConfirmationID
	if err := again.mcpConsumeConfirmation(path, used); err != nil {
		t.Fatal(err)
	}
	restarted := &App{mcpStateDir: dir}
	liveNew, err := restarted.mcpGetTask(context.Background(), path, "run-new")
	if err != nil || !strings.HasPrefix(liveNew.ConfirmationID, mcpConfirmIDPrefix) || liveNew.ConfirmationID == used || liveNew.StatusVersion != live.StatusVersion {
		t.Fatalf("used id returned %+v err=%v used %q", liveNew, err, used)
	}
	oldKept, err := restarted.mcpGetTask(context.Background(), path, "run-old")
	if err != nil || oldKept.ConfirmationID != old.ConfirmationID {
		t.Fatalf("archive id changed %+v err=%v", oldKept, err)
	}
}

func TestInboundMCPChangedConfirmationCardRotatesID(t *testing.T) {
	app := &App{}
	commands := mcpPlanDecisionCommands()
	path := normalizeProjectSessionPath("p")
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-1",
		RunID: "run-1", Text: "待确认",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
		ConfirmationID: "mcf_saved",
	})
	// A loaded record has no fingerprint. A view whose card differs must not
	// keep the id that was stored for the previous commands.
	changed := app.mcpRuns[path].view()
	changed.Commands = append(append([]string(nil), commands...), "另一项")
	before := app.mcpRuns[path].StatusVersion
	rotated := app.mcpEnsureConfirmationID(changed)
	if !strings.HasPrefix(rotated.ConfirmationID, mcpConfirmIDPrefix) || rotated.ConfirmationID == "mcf_saved" {
		t.Fatalf("changed card kept %q", rotated.ConfirmationID)
	}
	if got := app.mcpRuns[path].StatusVersion; got != before {
		t.Fatalf("status_version %d -> %d", before, got)
	}

	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-2",
		RunID: "run-2", Text: "仍是这一张",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
		ConfirmationID: "mcf_saved",
	})
	kept := app.mcpEnsureConfirmationID(app.mcpRuns[path].view())
	if kept.ConfirmationID != "mcf_saved" {
		t.Fatalf("matching card replaced %q", kept.ConfirmationID)
	}
}

func TestInboundMCPSamePlanKeepsConfirmationIDAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	app := &App{mcpStateDir: dir}
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-1",
		RunID: "run-1", Text: "待确认",
		ConfirmationKind: mcpConfirmKindPlan, Commands: []string{"/plan approve"},
	})
	rememberMCPPendingPlan(app, "p")
	first, err := app.mcpGetTask(context.Background(), "p", "run-1")
	if err != nil || !strings.HasPrefix(first.ConfirmationID, mcpConfirmIDPrefix) || !containsAll(first.Commands, "/plan approve", "/plan skip", "/plan reject") {
		t.Fatalf("first %+v err=%v", first, err)
	}
	app.mcpRunsMu.Lock()
	stored := append([]string(nil), app.mcpRuns[normalizeProjectSessionPath("p")].Commands...)
	app.mcpRunsMu.Unlock()
	if len(stored) != 1 || stored[0] != "/plan approve" {
		t.Fatalf("record commands changed %v", stored)
	}

	again := &App{mcpStateDir: dir}
	rememberMCPPendingPlan(again, "p")
	second, err := again.mcpGetTask(context.Background(), "p", "run-1")
	if err != nil || second.ConfirmationID != first.ConfirmationID || second.StatusVersion != first.StatusVersion || !containsAll(second.Commands, "/plan approve", "/plan skip", "/plan reject") {
		t.Fatalf("restart %+v err=%v want %q version %d", second, err, first.ConfirmationID, first.StatusVersion)
	}
}

func TestInboundMCPPlanEditRotatesConfirmationID(t *testing.T) {
	app := &App{}
	commands := mcpPlanDecisionCommands()
	app.mcpStoreRun(&mcpRunRecord{
		ProjectPath: "p", Status: mcpStatusNeedsConfirmation, RequestID: "mcp-1",
		RunID: "run-1", Text: "待确认",
		ConfirmationKind: mcpConfirmKindPlan, Commands: commands,
	})
	rememberMCPPendingPlan(app, "p")
	first, err := app.mcpGetTask(context.Background(), "p", "run-1")
	if err != nil || !strings.HasPrefix(first.ConfirmationID, mcpConfirmIDPrefix) || !strings.Contains(first.Text, "改登录") {
		t.Fatalf("first %+v err=%v", first, err)
	}
	same, err := app.mcpGetTask(context.Background(), "p", "run-1")
	if err != nil || same.ConfirmationID != first.ConfirmationID || same.StatusVersion != first.StatusVersion {
		t.Fatalf("same plan %+v err=%v", same, err)
	}
	owner := projectSessionOwnerID("p")
	app.imHandler.stickyCodingWorkbenchMemory.Store(stickyCodingMemoryKey(owner), stickyCodingWorkbenchMemory{
		PendingPlanJSON:     `{"tasks":[{"index":2}]}`,
		PendingPlanMarkdown: "## 改密码",
	})
	edited, err := app.mcpGetTask(context.Background(), "p", "run-1")
	if err != nil || edited.ConfirmationID == first.ConfirmationID || !strings.HasPrefix(edited.ConfirmationID, mcpConfirmIDPrefix) || !strings.Contains(edited.Text, "改密码") || edited.StatusVersion != first.StatusVersion {
		t.Fatalf("edited %+v err=%v first %q version %d", edited, err, first.ConfirmationID, first.StatusVersion)
	}
	again, err := app.mcpGetTask(context.Background(), "p", "run-1")
	if err != nil || again.ConfirmationID != edited.ConfirmationID || again.StatusVersion != first.StatusVersion {
		t.Fatalf("edited plan unstable %+v err=%v", again, err)
	}
}

func rememberMCPPendingPlan(app *App, projectPath string) {
	owner := projectSessionOwnerID(projectPath)
	app.imHandler = &IMMessageHandler{}
	app.imHandler.stickyCodingWorkbenchMemory.Store(stickyCodingMemoryKey(owner), stickyCodingWorkbenchMemory{
		PendingPlanJSON:     `{"tasks":[{"index":1}]}`,
		PendingPlanMarkdown: "## 改登录",
	})
}

type mcpOptCapture struct {
	fakeMCPTasks
	mu   sync.Mutex
	opts mcpCallOpts
}

func (f *mcpOptCapture) remember(ctx context.Context) {
	f.mu.Lock()
	f.opts = mcpCallOptsFrom(ctx)
	f.mu.Unlock()
}

func (f *mcpOptCapture) mcpSendCommand(ctx context.Context, projectPath, command, confirmationID, clientRequestID string, wait bool) (mcpTaskView, error) {
	f.remember(ctx)
	return f.fakeMCPTasks.mcpSendCommand(ctx, projectPath, command, confirmationID, clientRequestID, wait)
}

func (f *mcpOptCapture) mcpListArtifacts(ctx context.Context, projectPath string) (mcpArtifactList, error) {
	f.remember(ctx)
	return f.fakeMCPTasks.mcpListArtifacts(ctx, projectPath)
}

func (f *mcpOptCapture) mcpReadArtifact(ctx context.Context, projectPath, relPath string, download bool) (mcpArtifactBody, error) {
	f.remember(ctx)
	return f.fakeMCPTasks.mcpReadArtifact(ctx, projectPath, relPath, download)
}

type mcpErrAPI struct{ fakeMCPTasks }

func (mcpErrAPI) mcpGetTask(ctx context.Context, projectPath, runID string) (mcpTaskView, error) {
	return mcpTaskView{}, mcpErr(mcpCodeRunNotFound, "没有这一轮执行", "p", "run-x")
}

func mcpCodeIs(err error, code string) bool {
	var coded *mcpCallError
	return errors.As(err, &coded) && coded != nil && coded.Code == code
}

func mcpArtifactNames(list mcpArtifactList, name string) bool {
	for _, file := range list.Files {
		if file.Path == name {
			return true
		}
	}
	return false
}

func mcpResultText(t *testing.T, result any) (string, bool) {
	t.Helper()
	body, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result %#v", result)
	}
	isError, _ := body["isError"].(bool)
	content, _ := body["content"].([]map[string]any)
	if len(content) == 0 {
		t.Fatalf("content %#v", body)
	}
	text, _ := content[0]["text"].(string)
	return text, isError
}
