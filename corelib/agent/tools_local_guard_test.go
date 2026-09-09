package agent

import (
	"strings"
	"testing"
)

// TestToolBashWithContext_RejectsShellBrowserAutomation 保证 corelib 侧的 bash
// 入口与 gui / guiapp / corelib.tool 另外三处入口一致，拦截 shell 驱动的浏览器自动化。
//
// 这条防线的作用是防止 Agent 通过 playwright/puppeteer/selenium/CDP 建立第二个
// 浏览器控制面，绕过受管 browser 工具的登录态与 cookie 管理。历史上
// corelib/agent 入口漏掉了 RejectShellBrowserAutomationCommand，导致同一条命令
// 在 GUI 被拦、在 corelib 直连链路却能执行。
//
// 所有用例都传入一个不存在的 working_dir：被拦截的命令在守卫处直接返回，
// 未被拦截的命令则因 chdir 失败而在 cmd.Start() 处返回 "[错误] 命令启动失败"，
// 从而既验证了"未被拦截"，又保证测试不会真的执行任何命令。
func TestToolBashWithContext_RejectsShellBrowserAutomation(t *testing.T) {
	const nonexistentDir = "/nonexistent-maclaw-bash-guard-test-dir"

	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "playwright cli", command: "playwright screenshot example.com", want: true},
		{name: "npx playwright", command: "npx playwright test", want: true},
		{name: "python -m playwright", command: "python -m playwright install", want: true},
		{name: "puppeteer cli", command: "puppeteer screenshot", want: true},
		{name: "selenium cli", command: "selenium run", want: true},
		{name: "cdp 截图脚本", command: "python post.py --screenshot", want: true},
		{name: "playwright 脚本路径", command: "./run-playwright.js", want: true},

		// 必须放行：安装依赖与文本检索不应被拦截
		{name: "安装依赖放行", command: "npm install playwright", want: false},
		{name: "echo 标记放行", command: "echo connect_over_cdp", want: false},
		{name: "普通命令放行", command: "echo hello", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := ToolBashWithContext(nil, map[string]interface{}{
				"command":     tt.command,
				"working_dir": nonexistentDir,
			}, nil)

			got := strings.HasPrefix(out, "[system rejected]")
			if got != tt.want {
				t.Errorf("rejected=%v want=%v (out=%q)", got, tt.want, out)
			}
			if !tt.want && !strings.HasPrefix(out, "[错误]") {
				t.Errorf("未被拦截的命令应因无效工作目录而启动失败，实际输出: %q", out)
			}
		})
	}
}
