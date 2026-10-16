package computeruse

import "testing"

func TestHasExplicitTrigger(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"@computer open notepad", true},
		{"用 computer use 测一下计算器", true},
		{"try computer_use mode", true},
		{"computer-use please", true},
		{"Use @computer, then click Save", true},
		{"The computer use workflow is ready", true},
		{"contact@computer.example", false},
		{"@computerized report", false},
		{"mycomputer use notes", false},
		{"computer_user profile", false},
		// Semantic desktop phrasing is NOT an explicit trigger — activation is
		// decided by the unified intent classifier (corelib/intent).
		{"打开word程序写一份简历", false},
		{"点击屏幕上的保存按钮", false},
		{"open notepad and type hello", false},
		{"", false},
	}
	for _, c := range cases {
		if got := HasExplicitTrigger(c.in); got != c.want {
			t.Errorf("HasExplicitTrigger(%q)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestRequestsDesktopAppOperation(t *testing.T) {
	yes := []string{
		"打开word程序，编写一个你（maclaw）的简历。",
		"帮我在电脑上打开Excel程序，把这一列求和",
		"打开记事本程序输入一段文字",
		"打开微信",
		"打开这个微信",
		"打开记事本",
		"启动计算器",
		"打开这个软件",
		"打开软件，软件工程课先放一放",
		"点击窗口上的确定按钮",
		"点击屏幕上的保存按钮",
		"看看屏幕上现在显示了什么",
		"在桌面上把文件拖进文件夹",
		"操作桌面软件完成设置",
		"操作界面上的保存按钮",
		"控制鼠标点击屏幕上的图标",
		"在应用窗口里输入文字",
		"把当前窗口最小化",
		"open the Word app and type a document",
		"click the OK button on the dialog",
		"type into the notepad window",
		"look at what is on the screen",
		"open the calculator app and click buttons",
		"drag a file into a folder on the desktop",
	}
	no := []string{
		"",
		"帮我写一份word简历",
		"把昨天那个文件发给我",
		"生成一份markdown，不要pdf",
		"今天天气怎么样",
		"这个软件怎么用",
		"写一个应用程序",
		"写一份操作界面说明",
		"look at the screenshot",
		"look at the screensaver",
		"type a short note about the window function",
		"关闭窗口后点击这里看说明",
		"在时间窗口里统计点击率",
		"focus on the application of this formula",
		"open the application of this formula",
		"version control application notes",
		"点击后请确定需求",
		"运行这个程序看看输出",
		"控制程序流程",
		"请点击查看详情",
		"点击这个链接",
		"鼠标垫推荐",
		"在浏览器里打开知乎发帖",
		"打开网页登录账号",
		"打开 Chrome 点购买",
		"打开chrome搜索天气",
		"打开keyword列表",
		"打开应用层协议",
		"打开软件工程作业",
		"打开程序设计课",
		"打开昨天那个关于微信的备份文件",
		"click subscribe now please. after that write the notes. the dialog design is documented in the appendix",
		"搜索一下excel函数怎么用",
		"随便聊聊",
		"继续",
		`[VS Code / ACP programming workspace]
cwd: F:\desktop\notes

User request:
生成markdown`,
	}
	for _, text := range yes {
		if !RequestsDesktopAppOperation(text) {
			t.Errorf("RequestsDesktopAppOperation(%q) = false, want true", text)
		}
	}
	for _, text := range no {
		if RequestsDesktopAppOperation(text) {
			t.Errorf("RequestsDesktopAppOperation(%q) = true, want false", text)
		}
	}
}

func TestIsComputerUseTool(t *testing.T) {
	if !IsComputerUseTool("computer_observe") {
		t.Fatal("expected computer_observe")
	}
	if !IsComputerUseTool("computer_find") {
		t.Fatal("expected computer_find")
	}
	if IsComputerUseTool("bash") {
		t.Fatal("bash is not CU")
	}
}
