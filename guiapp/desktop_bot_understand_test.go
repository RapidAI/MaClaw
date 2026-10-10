package guiapp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDesktopBotProtocolSkipsTheModel(t *testing.T) {
	called := false
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		called = true
		return "", errors.New("model should not be called")
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })

	app := &App{}
	resume := desktopBotReturnTask
	saved := "已在本机保存 SITE_PASSWORD。"
	filled := "SITE_PASSWORD 已在本地填入当前密码框。请沿用这个结果继续，不要向我索要它的内容。"
	confirm := desktopBotConfirmTask
	cases := []struct {
		text  string
		phase string
	}{
		{resume, "execute"},
		{saved, "plan"},
		{filled, "execute"},
		{confirm, "plan"},
		{confirm, "discuss"},
	}
	for _, tc := range cases {
		got, err := app.UnderstandDesktopBotTask(tc.text, tc.phase, "zh-Hans", []string{"开发个程序"}, "")
		if err != nil {
			t.Fatalf("%s %s: %v", tc.phase, tc.text, err)
		}
		if got.Told != tc.text || got.Instruction != tc.text {
			t.Fatalf("protocol changed %q into told=%q instruction=%q", tc.text, got.Told, got.Instruction)
		}
	}
	if called {
		t.Fatal("a protocol sentence was sent to the model")
	}
	if _, err := app.UnderstandDesktopBotTask("   ", "plan", "zh-Hans", nil, ""); err == nil {
		t.Fatal("empty text was understood")
	}
}

func TestDesktopBotUnderstandingUsesTheModelWords(t *testing.T) {
	const cpp = "开发个c++版的hello world程序，运行后发我运行屏幕截图"
	const told = "你要的是一个能跑起来的程序，以及它跑起来时的画面。"
	const order = "在这台云桌面的终端里写出这个程序，编译运行，再截正在运行的窗口。"
	var system, user string
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, systemPrompt, userPrompt string) (string, error) {
		system, user = systemPrompt, userPrompt
		return "好的。\n```json\n{\"told\":\"" + told + "\",\"instruction\":\"" + order + "\"}\n```", nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })

	got, err := (&App{}).UnderstandDesktopBotTask(cpp, "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Told != told || got.Instruction != order {
		t.Fatalf("model words were replaced: %#v", got)
	}
	if !strings.Contains(user, cpp) || !strings.Contains(user, "phase: plan") || !strings.Contains(user, "language: zh-Hans") {
		t.Fatalf("user prompt dropped the request: %s", user)
	}
	if strings.Contains(system, "你要当前桌面的截屏") || strings.Contains(system, "本轮只做安排") || strings.Contains(system, "C++ Hello World") {
		t.Fatal("system prompt still carries a stock sentence")
	}
	if !strings.Contains(system, "pace is direct, confirm, ask, reply, or schedule") || !strings.Contains(system, "A clear request is direct") || !strings.Contains(system, "There is no second confirmation") || !strings.Contains(system, "Small talk") || !strings.Contains(system, "even when an earlier job is unfinished") || !strings.Contains(system, "interval_minutes") {
		t.Fatal("system prompt lost the direct, confirm, ask, and reply split")
	}
	if strings.Contains(system, "When the request still needs a change") {
		t.Fatal("system prompt still holds every change for confirmation")
	}
	if got.Pace != "confirm" {
		t.Fatalf("a plan turn without pace was not held for one confirmation: %#v", got)
	}
	if strings.Contains(system, `"told":"..."`) {
		t.Fatal("system prompt still offers a placeholder object")
	}

	desktopBotUnderstandLLM = func(_ context.Context, _ *App, systemPrompt, userPrompt string) (string, error) {
		system, user = systemPrompt, userPrompt
		return `{"told":"按前面的要求去做。","instruction":"写出并运行前面要的程序，然后截运行窗口。"}`, nil
	}
	got, err = (&App{}).UnderstandDesktopBotTask(desktopBotConfirmTask, "execute", "en", []string{"开始吧", cpp}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Instruction == desktopBotConfirmTask || !strings.Contains(got.Instruction, "运行窗口") {
		t.Fatalf("confirm did not become the model's order: %#v", got)
	}
	if got.Pace != "direct" {
		t.Fatalf("an execute turn without pace did not start: %#v", got)
	}
	if !strings.Contains(user, cpp) || !strings.Contains(user, "phase: execute") || !strings.Contains(user, "language: en") || !strings.Contains(user, "开始吧") {
		t.Fatalf("confirm prompt dropped the earlier request: %s", user)
	}
}

func TestDesktopBotPaceKeepsAnExplicitChoice(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"你要处理哪一个文件？","instruction":"你要处理哪一个文件？","pace":"ask"}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	got, err := (&App{}).UnderstandDesktopBotTask("帮我处理一下", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "ask" {
		t.Fatalf("ask was dropped: %#v", got)
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"我现在去查，查到就做成 PDF 发在这里。","instruction":"检索北京天所并交付 PDF。","pace":"DIRECT"}`, nil
	}
	got, err = (&App{}).UnderstandDesktopBotTask("查询 北京天所，生成pdf", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "direct" {
		t.Fatalf("a clear request was held back: %#v", got)
	}
	if desktopBotPace("maybe", "plan") != "confirm" || desktopBotPace("", "execute") != "direct" {
		t.Fatal("an unknown pace took a side")
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"我是你在这台云桌面上的同事。","instruction":"我是你在这台云桌面上的同事。","pace":"reply"}`, nil
	}
	got, err = (&App{}).UnderstandDesktopBotTask("你是谁呀？", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "reply" || got.Told != "我是你在这台云桌面上的同事。" {
		t.Fatalf("small talk was sent onward: %#v", got)
	}
	if desktopBotPace("reply", "execute") != "direct" {
		t.Fatal("a confirmed turn was turned into chat")
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"我是你在这台云桌面上的同事。","instruction":"","pace":"reply"}`, nil
	}
	got, err = (&App{}).UnderstandDesktopBotTask("你是谁呀?", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "reply" || got.Told != "我是你在这台云桌面上的同事。" || got.Instruction != "" {
		t.Fatalf("an empty small-talk order was sent onward: %#v", got)
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"按前面的安排做。","instruction":"","persona":"这个 Bot 叫 Bot 2。","pace":"reply"}`, nil
	}
	_, err = (&App{}).UnderstandDesktopBotTask(desktopBotConfirmTask, "execute", "zh-Hans", []string{"查询 北京天所，生成pdf"}, "")
	if err == nil || !strings.Contains(err.Error(), "omitted the work order") {
		t.Fatalf("a confirmed turn with an empty order became chat: %v", err)
	}
}

func TestDesktopBotPersonaIsNotDesktopWork(t *testing.T) {
	const kept = "这个 Bot 叫码卡龙真龙，小名真龙。"
	const told = "我记下了。我是码卡龙真龙，小名真龙。"
	var system, user string
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, systemPrompt, userPrompt string) (string, error) {
		system, user = systemPrompt, userPrompt
		return `{"told":"` + told + `","instruction":"","persona":"` + kept + `","pace":"confirm"}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })

	got, err := (&App{}).UnderstandDesktopBotTask("你叫：码卡龙真龙 ，小名：真龙", "plan", "zh-Hans", nil, "旧的身份")
	if err != nil {
		t.Fatal(err)
	}
	if got.Told != told || got.Instruction != "" || got.Persona != kept {
		t.Fatalf("identity became desktop work: %#v", got)
	}
	if got.Pace != "reply" {
		t.Fatalf("an identity with no desktop work was held for confirmation: %#v", got)
	}
	if !strings.Contains(user, "persona already kept:") || !strings.Contains(user, "旧的身份") {
		t.Fatalf("kept persona was not given to the model: %s", user)
	}
	if strings.Contains(system, "你叫") || strings.Contains(system, "小名") || strings.Contains(system, "码卡龙") {
		t.Fatal("system prompt matches a persona by phrase")
	}
	if !strings.Contains(system, "not desktop work") || !strings.Contains(system, "pace is reply") {
		t.Fatal("system prompt still treats every message as desktop work")
	}
	if !strings.Contains(system, "instruction is only that work") {
		t.Fatal("system prompt does not keep desktop work when an identity message also asks for it")
	}
	if strings.Contains(system, "reply is only small talk") {
		t.Fatal("plan phase still treats an identity as outside reply")
	}

	desktopBotUnderstandLLM = func(_ context.Context, _ *App, _, userPrompt string) (string, error) {
		user = userPrompt
		return `{"told":"人设已去掉。","instruction":"","persona":"-"}`, nil
	}
	got, err = (&App{}).UnderstandDesktopBotTask("不用保留这些身份了", "plan", "zh-Hans", nil, kept)
	if err != nil {
		t.Fatal(err)
	}
	if got.Persona != "-" || got.Instruction != "" || got.Pace != "reply" {
		t.Fatalf("withdrawing an identity became work: %#v", got)
	}
	if !strings.Contains(user, kept) {
		t.Fatalf("the kept persona was not given to the model: %s", user)
	}

	const order = "用已记住的身份写一份自我介绍，交付 自述.docx。"
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"我是码卡龙真龙。自我介绍我现在写。","instruction":"` + order + `","persona":"` + kept + `","pace":"direct"}`, nil
	}
	got, err = (&App{}).UnderstandDesktopBotTask("你叫码卡龙真龙，写一份自我介绍", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Instruction != order || got.Persona != kept || got.Pace != "direct" {
		t.Fatalf("identity plus work was split wrong: %#v", got)
	}
}

func TestDesktopBotEmptyOrderIsNotAReply(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"我现在去做。","instruction":"","pace":"direct"}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	_, err := (&App{}).UnderstandDesktopBotTask("打开百度", "plan", "zh-Hans", nil, "")
	if err == nil || !strings.Contains(err.Error(), "omitted the work order") {
		t.Fatalf("a direct reading with no order became chat: %v", err)
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"确认后我按这个做。","instruction":"","pace":"confirm"}`, nil
	}
	_, err = (&App{}).UnderstandDesktopBotTask("打开百度", "plan", "zh-Hans", nil, "")
	if err == nil || !strings.Contains(err.Error(), "omitted the work order") {
		t.Fatalf("a confirm reading with no order became chat: %v", err)
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"我现在去做。","instruction":"","pace":"direct"}`, nil
	}
	_, err = (&App{}).UnderstandDesktopBotTask(desktopBotConfirmTask, "execute", "zh-Hans", []string{"打开百度"}, "")
	if err == nil || !strings.Contains(err.Error(), "omitted the work order") {
		t.Fatalf("a confirmed turn with no order became chat: %v", err)
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"我记下了。","instruction":"","persona":"这个 Bot 叫真龙。","pace":"direct"}`, nil
	}
	got, err := (&App{}).UnderstandDesktopBotTask("记下这个身份", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "reply" || got.Instruction != "" || got.Persona != "这个 Bot 叫真龙。" {
		t.Fatalf("an identity marked direct was sent onward: %#v", got)
	}
}

func TestDesktopBotUnderstandingStopsWhenTheModelFails(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return "我先截一张当前桌面。", nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	if _, err := (&App{}).UnderstandDesktopBotTask("开发个c++版的hello world程序，运行后发我运行屏幕截图", "plan", "zh-Hans", nil, ""); err == nil {
		t.Fatal("prose was accepted as an order")
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"","instruction":"去做"}`, nil
	}
	if _, err := (&App{}).UnderstandDesktopBotTask("打开百度", "plan", "zh-Hans", nil, ""); err == nil {
		t.Fatal("an empty statement was accepted")
	}

	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return "", errors.New("LLM not configured")
	}
	_, err := (&App{}).UnderstandDesktopBotTask("打开百度", "plan", "zh-Hans", nil, "")
	if err == nil || !strings.Contains(err.Error(), "LLM not configured") {
		t.Fatalf("model failure was replaced: %v", err)
	}

	long := strings.Repeat("甲", desktopBotUnderstandToldMax+20)
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"` + long + `","instruction":"去做这件事"}`, nil
	}
	got, err := (&App{}).UnderstandDesktopBotTask("看一下", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got.Told)) != desktopBotUnderstandToldMax {
		t.Fatalf("told len=%d", len([]rune(got.Told)))
	}
}

func TestDesktopBotUnderstandingKeepsTheLastRealObject(t *testing.T) {
	const told = "你要能跑起来的程序，以及它跑起来时的画面。"
	const order = "在终端里写出这个程序，编译运行，再截正在运行的窗口。"
	raw := "先看到一个 { 不完整的说明 } 然后\n" +
		`{"told":"...","instruction":"..."}` + "\n" +
		`{"told":"` + told + `","instruction":"` + order + `"}`
	got, err := parseDesktopBotUnderstanding(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Told != told || got.Instruction != order {
		t.Fatalf("kept the wrong object: %#v", got)
	}

	if _, err := parseDesktopBotUnderstanding(`{"told":"...","instruction":"..."}`); err == nil {
		t.Fatal("placeholder was accepted")
	}

	hidden := "<think>{\"told\":\"思考里的句子。\",\"instruction\":\"思考里的指令。\"}</think>"
	got, err = parseDesktopBotUnderstanding(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if got.Told != "思考里的句子。" || got.Instruction != "思考里的指令。" {
		t.Fatalf("think block hid the object: %#v", got)
	}

	buried := "<think>I start { and the quote stays open \"</think>\n{\"told\":\"外面的句子。\",\"instruction\":\"外面的指令。\"}"
	got, err = parseDesktopBotUnderstanding(buried)
	if err != nil {
		t.Fatal(err)
	}
	if got.Told != "外面的句子。" || got.Instruction != "外面的指令。" {
		t.Fatalf("unclosed think quote hid the object: %#v", got)
	}
}

func TestDesktopBotUnderstandTextPrefersParsedReasoning(t *testing.T) {
	reasoning := "<think>{\"told\":\"看懂了。\",\"instruction\":\"去截运行窗口。\"}</think>"
	chosen := desktopBotUnderstandText("先写到 { 这里", reasoning)
	got, err := parseDesktopBotUnderstanding(chosen)
	if err != nil {
		t.Fatal(err)
	}
	if got.Told != "看懂了。" || got.Instruction != "去截运行窗口。" {
		t.Fatalf("reasoning object was dropped: %#v", got)
	}

	body := `{"told":"正文。","instruction":"正文指令。"}`
	chosen = desktopBotUnderstandText(body, `{"told":"思考。","instruction":"思考指令。"}`)
	got, err = parseDesktopBotUnderstanding(chosen)
	if err != nil {
		t.Fatal(err)
	}
	if got.Told != "正文。" || got.Instruction != "正文指令。" {
		t.Fatalf("content lost to reasoning: %#v", got)
	}

	if got := desktopBotUnderstandText("不是 JSON", ""); got != "不是 JSON" {
		t.Fatalf("unparsed body was replaced: %q", got)
	}
}

func TestCallDesktopBotUnderstandNeedsAModel(t *testing.T) {
	if _, err := callDesktopBotUnderstandLLM(context.Background(), nil, "system", "user"); err == nil {
		t.Fatal("nil app reached a model")
	}
	if _, err := callDesktopBotUnderstandLLM(context.Background(), &App{}, "system", "user"); err == nil {
		t.Fatal("an app with no hub reached a model")
	}
}

func TestDesktopBotScheduleReadingKeepsTheRepeat(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"已设好，之后每分钟向你问好。","instruction":"向用户问好一次，把这句问候作为回复。","pace":"schedule","schedule":{"name":"每分钟问好","mode":"create","interval_minutes":1}}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	got, err := (&App{}).UnderstandDesktopBotTask("每分钟向我问好一次。", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "schedule" || got.Schedule == nil || got.Schedule.Mode != "create" || got.Schedule.IntervalMinutes != 1 || got.Schedule.Name != "每分钟问好" {
		t.Fatalf("repeat was not kept: %#v", got.Schedule)
	}
	if got.Schedule.Hour != nil || got.Schedule.Minute != nil {
		t.Fatalf("a repeat kept a clock: %#v", got.Schedule)
	}
	if got.Instruction == "" || got.Told == "" {
		t.Fatalf("schedule reading dropped the words: %#v", got)
	}
}

func TestDesktopBotScheduleReadingKeepsAClock(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"已设好，每个工作日 8:30 做早间简报。","instruction":"整理早间简报并发在对话里。","pace":"schedule","schedule":{"name":"早间简报","hour":8,"minute":30,"day_of_week":1}}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	got, err := (&App{}).UnderstandDesktopBotTask("每个工作日早上八点半做简报", "execute", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "schedule" || got.Schedule == nil || got.Schedule.IntervalMinutes != 0 || got.Schedule.Hour == nil || *got.Schedule.Hour != 8 || got.Schedule.Minute == nil || *got.Schedule.Minute != 30 || got.Schedule.DayOfWeek == nil || *got.Schedule.DayOfWeek != 1 {
		t.Fatalf("clock was not kept: %#v", got.Schedule)
	}
}

func TestDesktopBotScheduleReadingRejectsAMissingTime(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"我来定时。","instruction":"向用户问好。","pace":"schedule","schedule":{"name":"问好"}}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	_, err := (&App{}).UnderstandDesktopBotTask("以后问好", "plan", "zh-Hans", nil, "")
	if err == nil || !strings.Contains(err.Error(), "omitted the schedule") {
		t.Fatalf("a schedule without a time was accepted: %v", err)
	}
}

func TestDesktopBotScheduleCancelNeedsNoOrder(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"每分钟问好已停止。","instruction":"","pace":"schedule","schedule":{"mode":"cancel","name":"每分钟问好"}}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	got, err := (&App{}).UnderstandDesktopBotTask("不用再问好了", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "schedule" || got.Instruction != "" || got.Schedule == nil || got.Schedule.Mode != "cancel" || got.Schedule.Name != "每分钟问好" {
		t.Fatalf("cancel was not kept: %#v", got)
	}
}

func TestDesktopBotReplyDropsAStraySchedule(t *testing.T) {
	desktopBotUnderstandLLM = func(context.Context, *App, string, string) (string, error) {
		return `{"told":"你好。","instruction":"","pace":"reply","schedule":{"mode":"create","interval_minutes":1}}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	got, err := (&App{}).UnderstandDesktopBotTask("你好", "plan", "zh-Hans", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pace != "reply" || got.Schedule != nil {
		t.Fatalf("a reply kept a schedule: %#v", got)
	}
}
