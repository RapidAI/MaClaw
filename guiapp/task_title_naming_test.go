package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskTitleNeedsSummarization(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"short clean zh", "北京天气", false},
		{"short mixed", "检查 usb打印服务", false},
		{"url", "安装这个skill: https://github.com/alchaincyf/huashu-art-motion", true},
		{"www host", "部署 www.example.com 证书", true},
		{"bare http word is not a url", "修复 HTTP 接口超时问题处理流程", false},
		{"host without url", "部署 nginx.example.com 证书", true},
		{"ipv4", "连接 192.168.1.1 调试打印机", true},
		{"filename is not a host", "修复 main.go 空指针问题", false},
		{"long plain", "开发一套系统信息检测软件，包含CPU、内存、磁盘和网络的实时监控与告警", true},
		{"multiline", "第一行命令\n第二行命令", true},
	}
	for _, tc := range cases {
		if got := taskTitleNeedsSummarization(tc.in); got != tc.want {
			t.Fatalf("%s: taskTitleNeedsSummarization(%q) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestSanitizeLLMTaskTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clean passthrough", "安装 huashu-art-motion 技能", "安装 huashu-art-motion 技能"},
		{"quote plus trailing period", "「开发贪吃蛇游戏」。", "开发贪吃蛇游戏"},
		{"trailing ellipsis", "开发贪吃蛇游戏…", "开发贪吃蛇游戏"},
		{"http topic kept", "修复 HTTP 接口超时", "修复 HTTP 接口超时"},
		{"fullwidth colon flattened", "任务：部署 nginx 服务", "任务 部署 nginx 服务"},
		{"first line only", "安装技能\n补充说明", "安装技能"},
		{"url rejected", "安装 https://github.com/x/y", ""},
		{"host rejected", "部署 nginx.example.com 证书", ""},
		{"ipv4 rejected", "连接 192.168.1.1 调试", ""},
		{"filename kept", "修复 main.go 空指针", "修复 main.go 空指针"},
		{"cn secret scrubbed", "密码 abc123 部署服务", "部署服务"},
		{"en secret scrubbed", "API key: sk-123 部署", "部署"},
		{"token topic kept", "token 刷新机制", "token 刷新机制"},
		{"empty", "  ", ""},
	}
	for _, tc := range cases {
		if got := sanitizeLLMTaskTitle(tc.in); got != tc.want {
			t.Fatalf("%s: sanitizeLLMTaskTitle(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}

	capped := sanitizeLLMTaskTitle(strings.Repeat("长", 40))
	if got := len([]rune(capped)); got != autoTaskTitleMaxRunes {
		t.Fatalf("long title capped to %d runes, got %d", autoTaskTitleMaxRunes, got)
	}
	// The cap backs up to the last separator so English words survive whole.
	if got := sanitizeLLMTaskTitle("compile install and configure the whole"); got != "compile install and" {
		t.Fatalf("latin cap = %q, want %q", got, "compile install and")
	}
}

// stubTaskTitleLLM replaces the title-pass LLM entry point so tests never
// issue real requests, even on machines where the default MaClaw endpoint
// resolves.
func stubTaskTitleLLM(t *testing.T, fn func(*App, string) string) {
	t.Helper()
	prev := taskTitleLLMFn
	taskTitleLLMFn = fn
	t.Cleanup(func() { taskTitleLLMFn = prev })
}

func TestScheduleTaskTitleSummarizationAppliesGeneratedTitle(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ctx = context.Background()
	instruction := "安装这个skill: https://github.com/alchaincyf/huashu-art-motion"
	title := "安装 huashu-art-motion 技能"
	stubTaskTitleLLM(t, func(*App, string) string { return title })

	task := app.CreateTask(instruction, "")
	if task.ProjectPath == "" {
		t.Fatal("CreateTask returned an empty project path")
	}

	pi := app.memoryStore.ProjectIndex()
	deadline := time.Now().Add(2 * time.Second)
	for pi.CustomName(task.ProjectPath) != title {
		if time.Now().After(deadline) {
			t.Fatalf("auto title not applied within deadline; custom name = %q", pi.CustomName(task.ProjectPath))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := pi.GetDisplayName(task.ProjectPath); got != title {
		t.Fatalf("display name = %q, want %q", got, title)
	}
}

func TestScheduleTaskTitleSummarizationSkippedWithoutAppContext(t *testing.T) {
	app := newProjectSearchTestApp(t)
	instruction := "安装这个skill: https://github.com/alchaincyf/huashu-art-motion"
	calls := 0
	stubTaskTitleLLM(t, func(*App, string) string { calls++; return "不应该被应用" })

	task := app.CreateTask(instruction, "")
	if task.ProjectPath == "" {
		t.Fatal("CreateTask returned an empty project path")
	}
	app.scheduleTaskTitleSummarization(task.ProjectPath, instruction)
	time.Sleep(50 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("LLM stub invoked %d times without app context", calls)
	}
	if got := app.memoryStore.ProjectIndex().CustomName(task.ProjectPath); got != "" {
		t.Fatalf("custom name set without app context: %q", got)
	}
}

func TestScheduleTaskTitleSummarizationSkipsShortCleanTitle(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ctx = context.Background()
	calls := 0
	stubTaskTitleLLM(t, func(*App, string) string { calls++; return "查询北京天气" })

	task := app.CreateTask("北京天气", "")
	if task.ProjectPath == "" {
		t.Fatal("CreateTask returned an empty project path")
	}
	app.scheduleTaskTitleSummarization(task.ProjectPath, "北京天气")
	time.Sleep(50 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("LLM stub invoked %d times for a short clean title", calls)
	}
}

// TestCreateTaskUnifiedSchedulesTitlePass covers the wrapper hook: a chat
// task and an expert task both take their title from the first instruction.
// The expert's display name is not the title.
func TestCreateTaskUnifiedSchedulesTitlePass(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ctx = context.Background()
	instruction := "安装这个skill: https://github.com/alchaincyf/huashu-art-motion"
	title := "安装 huashu-art-motion 技能"
	var calls atomic.Int32
	stubTaskTitleLLM(t, func(_ *App, got string) string {
		calls.Add(1)
		if got != instruction {
			t.Errorf("LLM stub received %q, want the first instruction", got)
		}
		return title
	})

	created, err := app.CreateTaskUnified(TaskCreateOptions{Name: instruction, Mode: "chat"})
	if err != nil || created.ProjectPath == "" {
		t.Fatalf("CreateTaskUnified chat branch failed: %v %q", err, created.ProjectPath)
	}
	expert, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:       instruction,
		ExpertID:   "expert-paper",
		ExpertName: "Paper reviewer",
	})
	if err != nil || expert.ProjectPath == "" {
		t.Fatalf("CreateTaskUnified expert branch failed: %v %q", err, expert.ProjectPath)
	}
	if got := app.memoryStore.ProjectIndex().GetDisplayName(expert.ProjectPath); got == "Paper reviewer" {
		t.Fatalf("expert task title = %q, want the instruction", got)
	}

	pi := app.memoryStore.ProjectIndex()
	deadline := time.Now().Add(2 * time.Second)
	for pi.CustomName(created.ProjectPath) != title || pi.CustomName(expert.ProjectPath) != title {
		if time.Now().After(deadline) {
			t.Fatalf("auto title not applied within deadline; chat=%q expert=%q", pi.CustomName(created.ProjectPath), pi.CustomName(expert.ProjectPath))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("LLM stub called %d times, want 2 (chat and expert)", got)
	}
}

func TestAdoptExpertTaskInstructionTitleReplacesExpertLabel(t *testing.T) {
	app := newProjectSearchTestApp(t)
	created := app.CreateExpertTask("expert-paper", "Paper reviewer")
	if created.ProjectPath == "" {
		t.Fatal("CreateExpertTask returned an empty project path")
	}
	if got := created.Name; sharedExpertPlaceholderTitle(got) {
		t.Fatalf("launcher title = %q, want it not to be a shared placeholder", got)
	}
	if got := created.Preview; sharedExpertPlaceholderTitle(got) || strings.HasPrefix(got, "C:/maclaw/expert-task/") {
		t.Fatalf("launcher preview = %q, want it not to be internal metadata", got)
	}
	// Same working directory must not become the shared title either.
	dir := t.TempDir()
	first := app.insertExpertTaskRecordWithTitle("expert-paper", dir, "")
	second := app.insertExpertTaskRecordWithTitle("expert-paper", dir, "")
	for _, row := range []ProjectSearchResult{first, second} {
		if sharedExpertPlaceholderTitle(row.Name) || strings.HasPrefix(strings.ToLower(row.Name), "working directory:") {
			t.Fatalf("same-directory launcher title = %q", row.Name)
		}
	}
	app.adoptExpertTaskInstructionTitle(created.ProjectPath, "expert-paper", "写一本量子工程教材")
	if got := app.memoryStore.ProjectIndex().GetDisplayName(created.ProjectPath); got != "写一本量子工程教材" {
		t.Fatalf("display name = %q, want the instruction", got)
	}
	app.adoptExpertTaskInstructionTitle(created.ProjectPath, "expert-paper", "再写一本蒙文识字书")
	if got := app.memoryStore.ProjectIndex().GetDisplayName(created.ProjectPath); got != "写一本量子工程教材" {
		t.Fatalf("later command renamed the task to %q", got)
	}

	// A title the user already chose stays, even when it is the expert's name.
	kept := app.CreateTask("Paper reviewer", "")
	if kept.ProjectPath == "" {
		t.Fatal("CreateTask returned an empty project path")
	}
	if renamed := app.RenameTask(kept.ProjectPath, "Paper reviewer"); renamed != "Paper reviewer" {
		t.Fatalf("rename = %q", renamed)
	}
	app.adoptExpertTaskInstructionTitle(kept.ProjectPath, "expert-paper", "写一本量子工程教材")
	if got := app.memoryStore.ProjectIndex().GetDisplayName(kept.ProjectPath); got != "Paper reviewer" {
		t.Fatalf("user title overwritten: %q", got)
	}

	// An instruction that starts with the working-directory label is still a
	// title. Only a name extracted from the metadata line is replaced.
	instruction := `Working directory: D:\books 写一本教材`
	named := app.insertExpertTaskRecordWithTitle("expert-paper", "", instruction)
	if named.ProjectPath == "" {
		t.Fatal("instruction task was not created")
	}
	app.adoptExpertTaskInstructionTitle(named.ProjectPath, "expert-paper", "再补一章")
	if got := app.memoryStore.ProjectIndex().GetDisplayName(named.ProjectPath); got != instruction {
		t.Fatalf("instruction title replaced: %q", got)
	}

	// The same words extracted from the body line, with no matching heading,
	// are the old metadata title and take the next instruction.
	metaName := `Working directory: D:\work\aicoder`
	meta := app.insertExpertTaskRecordWithTitle("expert-paper", "", metaName)
	if meta.ProjectPath == "" {
		t.Fatal("metadata-shaped task was not created")
	}
	if err := os.WriteFile(filepath.Join(meta.ProjectPath, "task.md"), []byte("source: expert-task\nWorking directory: D:\\work\\aicoder\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.adoptExpertTaskInstructionTitle(meta.ProjectPath, "expert-paper", "写一本量子工程教材")
	if got := app.memoryStore.ProjectIndex().GetDisplayName(meta.ProjectPath); got != "写一本量子工程教材" {
		t.Fatalf("extracted working-directory title stayed %q", got)
	}
}

func sharedExpertPlaceholderTitle(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	return name == "Paper reviewer" ||
		name == "expert-paper" ||
		name == "Created from task management." ||
		strings.HasPrefix(lower, "source: expert-task") ||
		strings.HasPrefix(lower, "created: expert") ||
		strings.HasPrefix(lower, "ai expert task.") ||
		strings.HasPrefix(name, "C:/maclaw/expert-task/")
}

func TestApplyAutoTaskTitleRespectsUserRename(t *testing.T) {
	app := newProjectSearchTestApp(t)
	instruction := "安装这个skill: https://github.com/alchaincyf/huashu-art-motion"
	task := app.CreateTask(instruction, "")
	if task.ProjectPath == "" {
		t.Fatal("CreateTask returned an empty project path")
	}
	pi := app.memoryStore.ProjectIndex()

	title := "安装 huashu-art-motion 技能"
	app.applyAutoTaskTitle(task.ProjectPath, instruction, title)
	if got := pi.GetDisplayName(task.ProjectPath); got != title {
		t.Fatalf("display name after auto title = %q, want %q", got, title)
	}
	if got := pi.CustomName(task.ProjectPath); got != title {
		t.Fatalf("custom name after auto title = %q, want %q", got, title)
	}

	// A user rename landing between generation and apply must win.
	pi.SetCustomName(task.ProjectPath, "我的自定义名称")
	app.applyAutoTaskTitle(task.ProjectPath, instruction, title)
	if got := pi.GetDisplayName(task.ProjectPath); got != "我的自定义名称" {
		t.Fatalf("user rename overwritten by auto title: got %q", got)
	}
}
