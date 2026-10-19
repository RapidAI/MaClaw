package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotProgressCases(t *testing.T) {
	t.Run("no_transcript_copy", func(t *testing.T) {
		c := readGUIProgressCase(t, "no_transcript_copy")
		h, mem := testChatTaskHandler(t)
		mem.Save(desktopUserID, []agent.ConversationEntry{{Role: "user", Content: c.Main[0]}})
		task := &chatTaskRecord{
			ID:         "ct-new",
			SessionKey: desktopUserID,
			MemoryKey:  chatTaskMemoryOwner(desktopUserID, "ct-new"),
		}
		h.openHelperMemory(task, false)
		if got := mem.Load(task.MemoryKey); len(got) != 0 {
			t.Fatalf("new helper = %+v", got)
		}
		main := mem.Load(desktopUserID)
		if len(main) != 1 || main[0].Content != c.Main[0] {
			t.Fatalf("main = %+v", main)
		}
		mem.Save(task.MemoryKey, []agent.ConversationEntry{{Role: "user", Content: c.Own[0]}})
		h.openHelperMemory(task, true)
		own := mem.Load(task.MemoryKey)
		if len(own) != 1 || own[0].Content != c.Own[0] {
			t.Fatalf("same id = %+v", own)
		}
		main = mem.Load(desktopUserID)
		if len(main) != 1 || main[0].Content != c.Main[0] {
			t.Fatalf("main changed = %+v", main)
		}
	})

	t.Run("helper_three_line_report", func(t *testing.T) {
		c := readGUIProgressCase(t, "helper_three_line_report")
		body := c.Trace + "\n1. " + c.Lines[0] + "\n2. " + c.Lines[1] + "\n3. " + c.Lines[2] + "\n文件：" + c.Path
		result := chatTaskResultFromAgentResponse(&IMAgentResponse{Text: body}, nil)
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		if strings.Contains(result.Text, c.Trace) || !strings.Contains(result.Text, c.Lines[0]) || !strings.Contains(result.Text, c.Path) {
			t.Fatalf("main session text = %q", result.Text)
		}
		empty := chatTaskResultFromAgentResponse(&IMAgentResponse{Text: c.EmptyBody}, nil)
		if empty.Err == nil || strings.Contains(empty.Err.Error(), c.Trace) {
			t.Fatalf("empty = %+v", empty)
		}
	})

	t.Run("progress_three_sections", func(t *testing.T) {
		c := readGUIProgressCase(t, "progress_three_sections")
		root := t.TempDir()
		sections := desktopbot.Sections{WhereWeAre: c.FirstWhere, NextStep: c.FirstNext, OpenProblems: c.OpenProblems}
		if _, _, err := writeLocalTaskProgress(root, c.TaskID, true, sections); err == nil {
			t.Fatal("cloud progress was written locally")
		}
		if found := progressFilesUnder(t, root); len(found) != 0 {
			t.Fatalf("cloud bot wrote %v", found)
		}
		if _, _, err := writeLocalTaskProgress(root, c.TaskID, false, sections); err != nil {
			t.Fatal(err)
		}
		second := desktopbot.Sections{WhereWeAre: c.SecondWhere, NextStep: c.SecondNext, OpenProblems: c.OpenProblems}
		host, _, err := writeLocalTaskProgress(root, c.TaskID, false, second)
		if err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(host)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if strings.Contains(text, c.FirstNext) || strings.Count(text, "## next step") != 1 || !strings.Contains(text, c.SecondNext) {
			t.Fatalf("local file:\n%s", text)
		}
		h, _ := testChatTaskHandler(t)
		reg := h.ensureChatTasks()
		reg.mu.Lock()
		reg.tasks["ct-progress"] = &chatTaskRecord{
			ID: "ct-progress", SessionKey: desktopUserID, Title: "slides", Status: chatTaskRunning,
			WhereWeAre: c.SecondWhere, NextStep: c.SecondNext, OpenProblems: c.OpenProblems,
			CreatedAt: time.Now(), UpdatedAt: time.Now(), answerCh: make(chan string, 1),
		}
		reg.mu.Unlock()
		resp, handled := h.tryChatTaskStatusReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "进度怎么样"})
		if !handled || resp == nil || resp.SendToUser != "" || !strings.Contains(resp.Text, c.SecondNext) || !strings.Contains(resp.Text, c.SecondWhere) {
			t.Fatalf("progress answer = handled:%v resp:%+v", handled, resp)
		}
	})
}

type guiProgressCase struct {
	Main         []string `json:"main"`
	Own          []string `json:"own"`
	Trace        string   `json:"trace"`
	Lines        []string `json:"lines"`
	Path         string   `json:"path"`
	EmptyBody    string   `json:"empty_body"`
	TaskID       string   `json:"task_id"`
	FirstWhere   string   `json:"first_where"`
	FirstNext    string   `json:"first_next"`
	SecondWhere  string   `json:"second_where"`
	SecondNext   string   `json:"second_next"`
	OpenProblems string   `json:"open_problems"`
}

func readGUIProgressCase(t *testing.T, id string) guiProgressCase {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "testdata", "desktopbot", "cases", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var c guiProgressCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func progressFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if entry.Name() == desktopbot.ProgressFileName {
			found = append(found, path)
		}
		return nil
	})
	return found
}
