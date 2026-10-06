package guiapp

import (
	"context"
	"strings"
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

// TestCreateTaskUnifiedSchedulesTitlePass covers the wrapper hook: the chat
// branch schedules the pass for a raw first instruction, while the expert
// branch keeps the launcher naming convention and never reaches the LLM —
// proven with a long URL name that would otherwise pass the gate.
func TestCreateTaskUnifiedSchedulesTitlePass(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ctx = context.Background()
	instruction := "安装这个skill: https://github.com/alchaincyf/huashu-art-motion"
	title := "安装 huashu-art-motion 技能"
	calls := 0
	stubTaskTitleLLM(t, func(_ *App, got string) string {
		calls++
		if got != instruction {
			t.Fatalf("LLM stub received %q, want the first instruction", got)
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

	pi := app.memoryStore.ProjectIndex()
	deadline := time.Now().Add(2 * time.Second)
	for pi.CustomName(created.ProjectPath) != title {
		if time.Now().After(deadline) {
			t.Fatalf("auto title not applied within deadline; custom name = %q", pi.CustomName(created.ProjectPath))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls != 1 {
		t.Fatalf("LLM stub called %d times, want 1 (expert branch must be skipped)", calls)
	}
	if got := pi.CustomName(expert.ProjectPath); got != "" {
		t.Fatalf("expert task auto-renamed to %q", got)
	}
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
