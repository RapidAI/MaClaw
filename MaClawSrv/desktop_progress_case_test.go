package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotProgressCases(t *testing.T) {
	desktopbot.ResetProgressCopies()
	t.Cleanup(desktopbot.ResetProgressCopies)
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "desktopbot", "cases", "progress_three_sections.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		ID           string `json:"id"`
		TaskID       string `json:"task_id"`
		FirstWhere   string `json:"first_where"`
		FirstNext    string `json:"first_next"`
		SecondWhere  string `json:"second_where"`
		SecondNext   string `json:"second_next"`
		OpenProblems string `json:"open_problems"`
		Secret       string `json:"secret"`
		FenceBody    string `json:"fence_body"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != "progress_three_sections" {
		t.Fatal(c.ID)
	}
	previous := desktopRemoteFile
	t.Cleanup(func() { desktopRemoteFile = previous })
	var writes []string
	var paths []string
	desktopRemoteFile = func(_ context.Context, _, _, action, filePath, content, _, _ string) (string, error) {
		if action != "write" {
			t.Fatalf("action = %s", action)
		}
		if strings.Contains(content, "FAILWRITE") {
			return "", os.ErrClosed
		}
		paths = append(paths, filePath)
		writes = append(writes, content)
		return "ok", nil
	}
	first := "## where we are\n" + c.FirstWhere + "\n[untrusted tool]\n" + c.FenceBody + "\n[/untrusted]\n" + c.Secret +
		"\n\n## next step\n" + c.FirstNext + "\n\n## open problems\n" + c.OpenProblems + "\n"
	agentservice.CloudProgress(context.Background(), "tenant", "alice", c.TaskID, "bash", first)
	second := desktopbot.RenderProgress(desktopbot.Sections{
		WhereWeAre: c.SecondWhere, NextStep: c.SecondNext, OpenProblems: c.OpenProblems,
	})
	agentservice.CloudProgress(context.Background(), "tenant", "alice", c.TaskID, "bash", second)
	if len(writes) != 2 {
		t.Fatalf("writes = %d", len(writes))
	}
	wantPath := desktopbot.TaskProgressPath(c.TaskID)
	if paths[0] != wantPath || paths[1] != wantPath {
		t.Fatalf("paths = %+v", paths)
	}
	if strings.Contains(writes[1], c.FirstNext) || strings.Contains(writes[1], c.FenceBody) || strings.Contains(writes[1], "sk-live") {
		t.Fatalf("container file leaked:\n%s", writes[1])
	}
	if strings.Count(writes[1], "## next step") != 1 || !strings.Contains(writes[1], c.SecondNext) {
		t.Fatalf("replaced file:\n%s", writes[1])
	}
	copy, ok := desktopbot.RecallProgress(c.TaskID)
	if !ok || copy.Path != wantPath || copy.Sections.NextStep != c.SecondNext {
		t.Fatalf("run copy = %+v ok=%v", copy, ok)
	}
	asleep := desktopbot.ReadProgress("", copy.Sections, false)
	if asleep.SendToUser != "" || !strings.Contains(asleep.Text, c.SecondNext) {
		t.Fatalf("asleep = %+v", asleep)
	}
	if view := desktopbot.Visible(desktopbot.Turn{Prose: asleep.Text}); view.Bubble {
		t.Fatal("progress copy opened a bubble")
	}
	agentservice.CloudProgress(context.Background(), "tenant", "alice", c.TaskID, "bash", "FAILWRITE\n"+second)
	kept, ok := desktopbot.RecallProgress(c.TaskID)
	if !ok || kept.Sections.NextStep != c.SecondNext {
		t.Fatalf("failed write replaced the copy: %+v", kept)
	}
	if strings.Contains(writes[0], c.FenceBody) || strings.Contains(writes[0], "sk-live") {
		t.Fatalf("first container write leaked:\n%s", writes[0])
	}
}
