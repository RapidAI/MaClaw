package main

import (
	"fmt"
	"strings"
)

// MergedBrowserToolName is the single tool name that replaces 27+ individual
// browser_* tool definitions in the LLM context. This reduces token density
// from ~3000-5000 tokens (27 definitions) to ~500 tokens (1 definition),
// eliminating the root cause of "Browser:" role-prefix hallucinations.
//
// Root cause analysis (#79 review):
// LLM training data contains multi-agent dialogue formats where "Browser"
// is a high-frequency agent role name ("Browser: 好的，我来操作浏览器...").
// When 27 tool definitions all prefixed with "browser_" appear in context,
// the token density of "browser" activates this training pattern, causing
// the model to switch from assistant role to "Browser agent" role.
// Other tools (ssh, bash, write_file) don't trigger this because they
// have 1 definition each and aren't agent role names in training data.
const MergedBrowserToolName = "browser"

// mergedBrowserToolDescription is the single description for the merged tool.
// It lists all available actions so the LLM knows what operations are possible.
const mergedBrowserToolDescription = `浏览器自动化工具（合并接口）。通过 action 参数选择操作：

会话管理：
- session_start: 启动/复用浏览器会话（返回 session_id）
- session_stop: 关闭浏览器会话
- connect: 连接到浏览器（自动发现或启动）
- close: 断开 CDP 连接
- info: 获取当前页面标题、URL、加载状态
- list_pages: 列出所有标签页
- switch_page: 切换标签页

页面操作：
- navigate: 导航到 URL
- click: 点击元素（ref 或 selector）
- click_at: CDP 级真实鼠标点击（绕过反自动化）
- type: 输入文本
- select: 下拉框选择
- scroll: 滚动页面
- back: 后退
- refresh: 刷新
- set_files: 文件上传

观察与提取：
- observe: 观察页面（snapshot + 截图）
- extract: 提取文本（ref/selector/整页）
- screenshot: 截取页面截图
- get_text: 获取元素文本
- get_html: 获取元素 HTML
- eval: 执行 JavaScript
- wait: 等待页面稳定
- ocr: OCR 文字识别

任务与录制：
- task_run: 执行自动化任务步骤序列
- task_status: 查询任务状态
- task_verify: 验证页面状态
- task_replay: 回放录制的操作流程
- record_start: 开始录制操作
- record_stop: 停止录制并保存
- list_flows: 列出已录制的流程

所有操作（除 connect/close/info/list_pages/list_flows）需要 session_id 参数。`

// mergedBrowserInputSchema defines the parameters for the merged browser tool.
// Only the common parameters are listed here; action-specific parameters are
// passed through to the underlying handler.
var mergedBrowserInputSchema = map[string]interface{}{
	"action":     map[string]string{"type": "string", "description": "操作类型（见工具描述中的完整列表）"},
	"session_id": map[string]string{"type": "string", "description": "浏览器会话 ID（大部分操作必填）"},
	"url":        map[string]string{"type": "string", "description": "URL（navigate 时必填）"},
	"ref":        map[string]string{"type": "string", "description": "元素引用 ID（click/type/extract/wait 时可用）"},
	"selector":   map[string]string{"type": "string", "description": "CSS 选择器（click/type/extract/wait 时可用）"},
	"text":       map[string]string{"type": "string", "description": "输入文本（type 时必填）/ 搜索文本"},
	"value":      map[string]string{"type": "string", "description": "选择值（select 时必填）"},
	"expression": map[string]string{"type": "string", "description": "JavaScript 代码（eval 时必填）"},
	"x":          map[string]string{"type": "number", "description": "X 坐标（click_at 时必填）"},
	"y":          map[string]string{"type": "number", "description": "Y 坐标（click_at 时必填）"},
	"delta_y":    map[string]string{"type": "number", "description": "滚动距离（scroll 时使用，正值向下）"},
	"target_id":  map[string]string{"type": "string", "description": "标签页 ID（switch_page 时必填）"},
	"full_page":  map[string]string{"type": "boolean", "description": "是否截取整页（screenshot 时可用）"},
	"steps":      map[string]string{"type": "array", "description": "操作步骤序列（task_run 时必填）"},
	"task_id":    map[string]string{"type": "string", "description": "任务 ID（task_status/task_replay 时使用）"},
	"flow_name":  map[string]string{"type": "string", "description": "流程名称（record_stop/task_replay 时使用）"},
	// session_start specific
	"start_url":        map[string]string{"type": "string", "description": "初始 URL（session_start 时可用）"},
	"reuse_existing":   map[string]string{"type": "boolean", "description": "是否复用已有会话（session_start 时可用，默认 true）"},
	"mode":             map[string]string{"type": "string", "description": "连接模式（session_start 时可用）：auto（默认，优先直连用户 Chrome 复用登录态）/ connect_user（仅直连）/ isolated（隔离实例）"},
	"allowed_domains":  map[string]string{"type": "array", "description": "允许访问的域名列表"},
	"blocked_domains":  map[string]string{"type": "array", "description": "禁止访问的域名列表"},
	"close_browser":    map[string]string{"type": "boolean", "description": "是否关闭浏览器（session_stop 时可用）"},
	"file_paths":       map[string]string{"type": "array", "description": "文件路径列表（set_files 时必填）"},
	"duration_ms":      map[string]string{"type": "number", "description": "等待时长毫秒（wait 时可用）"},
	"success_criteria": map[string]string{"type": "string", "description": "成功标准（task_run/task_verify 时可用）"},
}

// dispatchMergedBrowser routes a merged browser(action=...) call to the
// corresponding individual browser_* tool handler.
func dispatchMergedBrowser(registry *ToolRegistry, args map[string]interface{}) string {
	actionText, _ := args["action"].(string)
	actionText = strings.TrimSpace(actionText)
	if actionText == "" {
		return "缺少 action 参数。请指定操作类型，如 browser(action=\"navigate\", ...)"
	}

	action := normalizeBrowserToolAction(actionText)
	toolName := action.ToolName()
	if toolName == "" {
		// Try with browser_ prefix in case LLM uses the full name.
		toolName = "browser_" + actionText
		if _, exists := registry.Get(toolName); !exists {
			actions := browserSupportedActionNames()
			return fmt.Sprintf("未知 browser action: %s（支持: %s）", action, strings.Join(actions, ", "))
		}
	}

	tool, ok := registry.Get(toolName)
	if !ok || tool.Handler == nil {
		return fmt.Sprintf("browser 工具 %s 未注册或无 handler", toolName)
	}

	return tool.Handler(args)
}
