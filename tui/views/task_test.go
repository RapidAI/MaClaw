package views

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTaskFocusScheduled(t *testing.T) {
	m := NewTaskModel("en")
	if m.ActiveSubTab() != TaskSubRemote {
		t.Fatalf("default sub-tab = %d, want remote", m.ActiveSubTab())
	}

	m.FocusScheduled()
	if m.ActiveSubTab() != TaskSubScheduled {
		t.Fatalf("focused sub-tab = %d, want scheduled", m.ActiveSubTab())
	}
}

func TestTaskFocusTabHelpers(t *testing.T) {
	m := NewTaskModel("en")
	m.FocusBackground()
	if m.ActiveSubTab() != TaskSubBackground {
		t.Fatalf("FocusBackground sub-tab = %d, want background", m.ActiveSubTab())
	}
	m.FocusRemote()
	if m.ActiveSubTab() != TaskSubRemote {
		t.Fatalf("FocusRemote sub-tab = %d, want remote", m.ActiveSubTab())
	}
	m.FocusTab(TaskSubScheduled)
	if m.ActiveSubTab() != TaskSubScheduled {
		t.Fatalf("FocusTab sub-tab = %d, want scheduled", m.ActiveSubTab())
	}
	m.FocusTab(TaskSubCount + 1)
	if m.ActiveSubTab() != TaskSubScheduled {
		t.Fatalf("invalid FocusTab should keep current subTab, got %d", m.ActiveSubTab())
	}
}

func TestTaskRemoteHeadersFollowLanguage(t *testing.T) {
	m := NewTaskModel("zh")
	m.SetRemoteTasks([]RemoteTaskItem{{ID: "1", Host: "host", Status: "running", StartedAt: "now", Command: "echo ok"}})
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, taskLocalText("zh", "host")) || !strings.Contains(view, taskLocalText("zh", "command")) {
		t.Fatalf("remote task headers should be Chinese-localized:\n%s", view)
	}
	if !strings.Contains(view, "运行中") || strings.Contains(view, "running") {
		t.Fatalf("remote task status should be Chinese-localized:\n%s", view)
	}

	m.SetLang("en")
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "HOST") || !strings.Contains(view, "COMMAND") {
		t.Fatalf("remote task headers should be English-localized:\n%s", view)
	}
	if !strings.Contains(view, "running") {
		t.Fatalf("remote task status should be English-localized:\n%s", view)
	}
}

func TestTaskRemoteListScrollsAroundCursor(t *testing.T) {
	m := NewTaskModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	tasks := make([]RemoteTaskItem, 12)
	for i := range tasks {
		tasks[i] = RemoteTaskItem{
			ID:        fmt.Sprintf("remote-%02d", i),
			Host:      "host",
			Status:    "running",
			StartedAt: "now",
			Command:   "echo ok",
		}
	}
	m.SetRemoteTasks(tasks)
	m.remoteCursor = len(tasks) - 1

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "remote-11") {
		t.Fatalf("view should follow the selected remote task:\n%s", view)
	}
	if strings.Contains(view, "remote-00") {
		t.Fatalf("view should scroll instead of rendering from the top:\n%s", view)
	}
	if !strings.Contains(view, "more above") {
		t.Fatalf("view should show an overflow hint above:\n%s", view)
	}
}

func TestTaskViewsFitNarrowTerminal(t *testing.T) {
	m := NewTaskModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 14})
	m.SetRemoteTasks([]RemoteTaskItem{{
		ID:        "remote-task-with-a-very-long-id",
		Host:      "very-long-hostname.example.internal",
		Status:    "running",
		StartedAt: "2026-05-05T10:00:00Z",
		Command:   "a very long command with many arguments that should not overflow",
	}})
	m.SetBackgroundTasks([]BackgroundTaskItem{{
		ID:        "background-task-with-a-very-long-id",
		Name:      "a very long background task name that should fit",
		Status:    "running",
		Rounds:    "123/456",
		StartedAt: "2026-05-05T10:00:00Z",
	}})
	m.SetTasks([]ScheduleItem{{
		ID:     "schedule-task-with-a-very-long-id",
		Name:   "a very long scheduled task name",
		Status: "active",
		Time:   "every 1 hour",
		Runs:   42,
		Action: "a very long scheduled action command that should fit",
	}})

	for _, subTab := range []int{TaskSubRemote, TaskSubBackground, TaskSubScheduled} {
		m.subTab = subTab
		assertViewFitsWidth(t, stripANSIForTest(m.View()), 44)
	}
}

func TestTaskFootersOnlyAdvertiseImplementedActions(t *testing.T) {
	m := NewTaskModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 18})
	m.SetRemoteTasks([]RemoteTaskItem{{ID: "remote", Host: "host", Status: "running", Command: "echo ok"}})
	m.SetBackgroundTasks([]BackgroundTaskItem{{ID: "bg", Name: "loop", Status: "running", Rounds: "1/5"}})
	m.SetTasks([]ScheduleItem{{ID: "sch", Name: "daily", Status: "active", Time: "09:00", Runs: 1, Action: "run"}})

	m.subTab = TaskSubRemote
	view := stripANSIForTest(m.View())
	if strings.Contains(view, "Enter:details") || strings.Contains(view, "s:stop") {
		t.Fatalf("remote footer should not advertise unimplemented actions:\n%s", view)
	}

	m.subTab = TaskSubBackground
	view = stripANSIForTest(m.View())
	if strings.Contains(view, "Enter:details") || strings.Contains(view, "s:stop") {
		t.Fatalf("background footer should not advertise unimplemented actions:\n%s", view)
	}

	m.subTab = TaskSubScheduled
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "p:pause/resume") || !strings.Contains(view, "d:delete") {
		t.Fatalf("scheduled footer should keep implemented actions:\n%s", view)
	}
}

func TestTaskEmptyStatesGuideTowardTUIFlows(t *testing.T) {
	m := NewTaskModel("en")
	m.SetRemoteTasks(nil)
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Press Enter") || !strings.Contains(view, "Tools") || !strings.Contains(view, "Chat") {
		t.Fatalf("remote empty state should guide users toward TUI flows:\n%s", view)
	}

	m.subTab = TaskSubBackground
	m.SetBackgroundTasks(nil)
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "Chat") {
		t.Fatalf("background empty state should guide users toward Chat:\n%s", view)
	}

	m.subTab = TaskSubScheduled
	m.SetTasks(nil)
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "return to Chat") {
		t.Fatalf("scheduled empty state should guide users toward Chat:\n%s", view)
	}
	if strings.Contains(view, "schedule create --name") || strings.Contains(view, "CLI") {
		t.Fatalf("scheduled empty state should not route users back to CLI commands:\n%s", view)
	}

	m.SetLang("zh")
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "聊天页") || strings.Contains(view, "schedule create --name") || strings.Contains(view, "CLI") {
		t.Fatalf("Chinese scheduled empty state should stay TUI-friendly:\n%s", view)
	}
}

func TestTaskEmptyStateEnterOpensHelpfulTabs(t *testing.T) {
	m := NewTaskModel("en")
	m.SetRemoteTasks(nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("remote empty state should emit a navigation command")
	}
	if _, ok := cmd().(TaskOpenToolsMsg); !ok {
		t.Fatalf("remote empty state command = %T, want TaskOpenToolsMsg", cmd())
	}

	m.subTab = TaskSubBackground
	m.SetBackgroundTasks(nil)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("background empty state should emit a navigation command")
	}
	if _, ok := cmd().(TaskOpenChatMsg); !ok {
		t.Fatalf("background empty state command = %T, want TaskOpenChatMsg", cmd())
	}

	m.subTab = TaskSubScheduled
	m.SetTasks(nil)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("scheduled empty state should emit a navigation command")
	}
	if _, ok := cmd().(TaskOpenChatMsg); !ok {
		t.Fatalf("scheduled empty state command = %T, want TaskOpenChatMsg", cmd())
	}
}

func TestTaskCompactViewKeepsFocusedRemoteRowVisible(t *testing.T) {
	m := NewTaskModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 48, Height: 9})
	tasks := make([]RemoteTaskItem, 10)
	for i := range tasks {
		tasks[i] = RemoteTaskItem{
			ID:        fmt.Sprintf("remote-%02d", i),
			Host:      "very-long-hostname.example.internal",
			Status:    "running",
			StartedAt: "now",
			Command:   "a very long command that should fit",
		}
	}
	m.SetRemoteTasks(tasks)
	m.remoteCursor = len(tasks) - 1

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "remote-09") {
		t.Fatalf("compact view should follow the selected remote task:\n%s", view)
	}
	if strings.Contains(view, "remote-00") {
		t.Fatalf("compact view should not render from the top when cursor is near the end:\n%s", view)
	}
	if strings.Contains(view, "HOST") || strings.Contains(view, "COMMAND") {
		t.Fatalf("compact view should drop table headers in short terminals:\n%s", view)
	}
	assertViewFitsWidth(t, view, 48)
}

func TestTaskCompactEmptyStateKeepsEnterActionVisible(t *testing.T) {
	m := NewTaskModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 8})
	m.SetRemoteTasks(nil)

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "No remote tasks") || !strings.Contains(view, "Press Enter") {
		t.Fatalf("compact empty state should keep the actionable guidance visible:\n%s", view)
	}
	if strings.Contains(view, "CLI") || strings.Contains(view, "schedule create") {
		t.Fatalf("compact empty state should stay TUI-oriented:\n%s", view)
	}
	assertViewFitsWidth(t, view, 44)
}
