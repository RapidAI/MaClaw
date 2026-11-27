package cloudagent

import (
	"testing"
	"time"
)

func TestHistoryReportsErrorFlagWithoutChangingLines(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	requestID, created, err := b.Begin("c1", "task_1", "实现登录", now)
	if err != nil || !created || requestID == "" {
		t.Fatalf("begin %v %v %s", err, created, requestID)
	}
	wrote, err := b.Finish(requestID, "云端官方模型未接通", true, []string{"out/report.md"}, now.Add(time.Second))
	if err != nil || !wrote {
		t.Fatalf("finish %v %v", err, wrote)
	}
	lines, err := b.History("task_1")
	if err != nil || len(lines) != 2 || lines[0] != "user\n实现登录" || lines[1] != "assistant\n云端官方模型未接通" {
		t.Fatalf("lines %#v %v", lines, err)
	}
	_, flags, files, requestIDs, clientIDs, err := b.history("task_1")
	if err != nil || len(flags) != 2 || flags[0] || !flags[1] {
		t.Fatalf("flags %v %v", flags, err)
	}
	if len(requestIDs) != 2 || requestIDs[0] != requestID || requestIDs[1] != requestID {
		t.Fatalf("request ids %#v", requestIDs)
	}
	if len(clientIDs) != 2 || clientIDs[0] != "c1" || clientIDs[1] != "" {
		t.Fatalf("client ids %#v", clientIDs)
	}
	if len(files) != 2 || len(files[0]) != 0 || len(files[1]) != 1 || files[1][0] != "out/report.md" {
		t.Fatalf("files %v", files)
	}
	if !b.producedPaths()["out/report.md"] {
		t.Fatal("producedPaths misses the finished file")
	}
}

func TestQueuedTurnsFIFOAndDedup(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := b.Enqueue("c1", "task_1", "第一条", now); err != nil {
		t.Fatal(err)
	}
	second, err := b.Enqueue("c2", "task_1", "第二条", now.Add(time.Second))
	if err != nil || second == "" {
		t.Fatalf("enqueue second %q %v", second, err)
	}
	dup, err := b.Enqueue("c1", "task_1", "第一条", now.Add(2*time.Second))
	if err != nil || dup == "" {
		t.Fatalf("enqueue dup %q %v", dup, err)
	}
	msg, ok, err := b.PeekOldest()
	if err != nil || !ok || msg.Text != "第一条" {
		t.Fatalf("peek %#v %v %v", msg, ok, err)
	}
	msg, ok, err = b.PeekOldest()
	if err != nil || !ok || msg.Text != "第一条" {
		t.Fatalf("peek twice %#v %v %v", msg, ok, err)
	}
	if err := b.DropQueued(msg.QueueID); err != nil {
		t.Fatal(err)
	}
	msg, ok, err = b.PeekOldest()
	if err != nil || !ok || msg.Text != "第二条" || msg.QueueID != second {
		t.Fatalf("peek second %#v %v %v", msg, ok, err)
	}
	// The queue runner promotes through BeginDrain; external Begin is fenced
	// behind parked rows, but the drain itself must not fence against its head.
	if _, _, err := b.BeginDrain("c2", "task_1", "第二条", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	back, err := b.Enqueue("c2", "task_1", "第二条", now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if back == "" {
		t.Fatal("enqueue after begin returned empty id")
	}
}

func TestCancelLeavesUserLineAndTurnStatus(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, _, err := b.Begin("c1", "task_1", "实现登录", now); err != nil {
		t.Fatal(err)
	}
	if _, err := b.CancelRunning("task_1", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	lines, err := b.History("task_1")
	if err != nil || len(lines) != 1 || lines[0] != "user\n实现登录" {
		t.Fatalf("lines %#v %v", lines, err)
	}
	status, err := b.latestTurn("task_1")
	if err != nil || status != "cancelled" {
		t.Fatalf("turn %q %v", status, err)
	}
}

func TestDeleteTaskCascadesEveryRow(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	requestID, created, err := b.Begin("c1", "task_1", "实现登录", now)
	if err != nil || !created {
		t.Fatalf("begin %v %v", err, created)
	}
	if err := b.AppendTurnEvent(requestID, "task_1", "progress", "读取文件", now); err != nil {
		t.Fatal(err)
	}
	wrote, err := b.Finish(requestID, "完成", false, []string{"out/report.md"}, now.Add(time.Second))
	if err != nil || !wrote {
		t.Fatalf("finish %v %v", err, wrote)
	}
	if err := b.EnsureTask("task_2", "别的任务"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Enqueue("c2", "task_1", "排队消息", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := b.storeDecision("task_1", "fp1", "yes"); err != nil {
		t.Fatal(err)
	}
	if b.prepareAsk("task_1", "fp2") == "" {
		t.Fatal("prepareAsk returned empty nonce")
	}
	if err := b.DeleteTask("task_1"); err != nil {
		t.Fatal(err)
	}
	// task_1 的每张挂表都清空；task_2 原样保留。
	for _, table := range []string{
		"tasks", "messages", "turns", "client_messages", "queued_turns",
		"plan_state", "commit_approvals", "commit_pending", "turn_events",
	} {
		var taskRows int
		if err := b.db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE task_id = 'task_1'`).Scan(&taskRows); err != nil {
			// tasks 表主键列叫 id，其余表才有 task_id；tasks 单独查。
			if err2 := b.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = 'task_1'`).Scan(&taskRows); err2 != nil {
				t.Fatalf("count %s: %v / %v", table, err, err2)
			}
		}
		if taskRows != 0 {
			t.Fatalf("%s still holds %d rows for task_1", table, taskRows)
		}
	}
	var others int
	if err := b.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = 'task_2'`).Scan(&others); err != nil || others != 1 {
		t.Fatalf("task_2 kept %d %v", others, err)
	}
}

func TestTaskSummariesStatusAndActivity(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := b.EnsureTask("task_idle", "还没发消息"); err != nil {
		t.Fatal(err)
	}
	requestID, created, err := b.Begin("c1", "task_done", "实现登录", now)
	if err != nil || !created {
		t.Fatalf("begin %v %v", err, created)
	}
	if _, err := b.Finish(requestID, "完成", false, nil, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Begin("c2", "task_running", "另一条", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	summaries, err := b.TaskSummaries()
	if err != nil || len(summaries) != 3 {
		t.Fatalf("summaries %#v %v", summaries, err)
	}
	byID := map[string]taskSummaryRow{}
	for _, item := range summaries {
		byID[item.ID] = item
	}
	if got := byID["task_done"]; got.Status != "finished" || got.UpdatedAt == "" {
		t.Fatalf("task_done summary %#v", got)
	}
	if got := byID["task_running"]; got.Status != "running" {
		t.Fatalf("task_running summary %#v", got)
	}
	if got := byID["task_idle"]; got.Status != "" || got.UpdatedAt != "" {
		t.Fatalf("task_idle summary %#v", got)
	}
}

func TestHistoryWindowReturnsTailAndTotal(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := 0; i < 3; i++ {
		requestID, created, err := b.Begin(
			"", "task_1",
			[]string{"第一条", "第二条", "第三条"}[i],
			now.Add(time.Duration(i)*time.Second),
		)
		if err != nil || !created {
			t.Fatalf("begin %d %v %v", i, err, created)
		}
		wrote, err := b.Finish(requestID, "回复"+[]string{"一", "二", "三"}[i], false, nil, now.Add(time.Duration(i)*time.Second+time.Millisecond))
		if err != nil || !wrote {
			t.Fatalf("finish %d %v %v", i, err, wrote)
		}
	}
	// 6 行。limit=2 取末尾两行且保持时间序。
	lines, flags, files, requestIDs, clientIDs, total, err := b.historyWindow("task_1", 2)
	if err != nil || total != 6 {
		t.Fatalf("window total %d %v", total, err)
	}
	if len(lines) != 2 || lines[0] != "user\n第三条" || lines[1] != "assistant\n回复三" {
		t.Fatalf("window lines %#v", lines)
	}
	if len(flags) != 2 || flags[0] || flags[1] {
		t.Fatalf("window flags %v", flags)
	}
	if len(files) != 2 || len(requestIDs) != 2 || len(clientIDs) != 2 {
		t.Fatalf("window parallels %d/%d/%d", len(files), len(requestIDs), len(clientIDs))
	}
	// limit 覆盖全部时等价全量历史。
	all, _, _, _, _, allTotal, err := b.historyWindow("task_1", 0)
	if err != nil || allTotal != 6 || len(all) != 6 {
		t.Fatalf("full window %d/%d %v", len(all), allTotal, err)
	}
	full, err := b.History("task_1")
	if err != nil {
		t.Fatal(err)
	}
	for i := range all {
		if all[i] != full[i] {
			t.Fatalf("window full row %d differs: %q vs %q", i, all[i], full[i])
		}
	}
}

func TestTurnEventsAppendSealAndCascade(t *testing.T) {
	b, err := openBook(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	requestID, created, err := b.Begin("c1", "task_1", "实现登录", now)
	if err != nil || !created {
		t.Fatalf("begin %v %v", err, created)
	}
	if err := b.AppendTurnEvent(requestID, "task_1", "progress", "第一步", now); err != nil {
		t.Fatal(err)
	}
	if err := b.AppendTurnEvent(requestID, "task_1", "progress", "第二步", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// 思考落库两次：替换而不是叠加。
	if err := b.SealThinking(requestID, "task_1", "先想一想", now); err != nil {
		t.Fatal(err)
	}
	if err := b.SealThinking(requestID, "task_1", "再想一想", now); err != nil {
		t.Fatal(err)
	}
	events, err := b.turnEventsByTask("task_1")
	if err != nil {
		t.Fatal(err)
	}
	list := events[requestID]
	if len(list) != 3 ||
		list[0].Text != "第一步" || list[0].Kind != "progress" || list[0].Seq != 1 ||
		list[1].Text != "第二步" || list[1].Seq != 2 ||
		list[2].Text != "再想一想" || list[2].Kind != "thinking" {
		t.Fatalf("events %#v", list)
	}
	// 空 text / 空 request 是 no-op，不报错也不落行。
	if err := b.AppendTurnEvent(requestID, "task_1", "progress", "  ", now); err != nil {
		t.Fatal(err)
	}
	if err := b.AppendTurnEvent("", "task_1", "progress", "x", now); err != nil {
		t.Fatal(err)
	}
	if again, err := b.turnEventsByTask("task_1"); err != nil || len(again[requestID]) != 3 {
		t.Fatalf("events after no-ops %#v %v", again, err)
	}
	if err := b.DeleteTask("task_1"); err != nil {
		t.Fatal(err)
	}
	after, err := b.turnEventsByTask("task_1")
	if err != nil || len(after) != 0 {
		t.Fatalf("events after delete %#v %v", after, err)
	}
}
