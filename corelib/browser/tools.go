package browser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// RegisterTools registers all browser automation tools into the given registry.
func RegisterTools(registry *tool.Registry) {
	addr := "" // will use default; can be made configurable later

	tools := []tool.RegisteredTool{
		{
			Name:        "browser_session_start",
			Description: "启动或复用一个长期浏览器 agent 会话，返回 session_id。支持 allowed_domains / blocked_domains / start_url / reuse_existing。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "session", "agent", "浏览器", "会话", "网页"},
			Priority:    6,
			InputSchema: map[string]interface{}{
				"addr":                          map[string]interface{}{"type": "string", "description": "可选 CDP 地址，默认自动发现或启动"},
				"start_url":                     map[string]interface{}{"type": "string", "description": "可选初始 URL"},
				"reuse_existing":                map[string]interface{}{"type": "boolean", "description": "是否优先复用已有 browser session，默认 true"},
				"allowed_domains":               map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "允许访问的域名列表"},
				"blocked_domains":               map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "禁止访问的域名列表"},
				"allow_cross_origin_navigation": map[string]interface{}{"type": "boolean", "description": "是否允许跨域导航"},
			},
			Handler: func(args map[string]interface{}) string {
				policy := policyFromArgs(args)
				reuseExisting := boolArg(args, "reuse_existing", true)
				agentSession, err := StartAgentSession(strArg(args, "addr", addr), policy, reuseExisting)
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				if startURL := strings.TrimSpace(strArg(args, "start_url", "")); startURL != "" {
					if _, err := agentSession.Navigate(startURL); err != nil {
						return marshalBrowserResult(false, err.Error(), map[string]interface{}{"session_id": agentSession.ID})
					}
				}
				state := agentSession.State()
				return marshalBrowserResult(true, fmt.Sprintf("已启动浏览器会话 %s", agentSession.ID), map[string]interface{}{
					"session_id":  state.ID,
					"target_id":   state.TargetID,
					"url":         state.CurrentURL,
					"title":       state.CurrentTitle,
					"ready_state": state.ReadyState,
				})
			},
		},
		{
			Name:        "browser_session_stop",
			Description: "关闭指定 browser agent 会话，可选 close_browser=true 一并关闭托管浏览器。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "session", "stop", "浏览器", "关闭"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":    map[string]interface{}{"type": "string", "description": "browser session id"},
				"close_browser": map[string]interface{}{"type": "boolean", "description": "是否关闭托管浏览器"},
			},
			Handler: func(args map[string]interface{}) string {
				sessionID := strArg(args, "session_id", "")
				if err := StopAgentSession(sessionID, boolArg(args, "close_browser", false)); err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, fmt.Sprintf("已关闭浏览器会话 %s", sessionID), map[string]interface{}{"session_id": sessionID})
			},
		},
		{
			Name:        "browser_observe",
			Description: "观察当前页面，生成 snapshot、stable refs、截图和 console/network 摘要。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "observe", "snapshot", "浏览器", "观察", "网页"},
			Priority:    6,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":         map[string]interface{}{"type": "string", "description": "browser session id"},
				"include_screenshot": map[string]interface{}{"type": "boolean", "description": "是否包含截图，默认 true"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				obs, err := agentSession.Observe(boolArg(args, "include_screenshot", true))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, obs.Display, obs.Data)
			},
		},
		{
			Name:        "browser_navigate",
			Description: "在 browser session 内导航到指定 URL，成功后自动 observe。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "navigate", "session", "浏览器", "导航"},
			Priority:    6,
			Required:    []string{"session_id", "url"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
				"url":        map[string]interface{}{"type": "string", "description": "要访问的 URL"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Navigate(strArg(args, "url", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, result.Display, result.Data)
			},
		},
		{
			Name:        "browser_click",
			Description: "在 browser session 内点击元素，优先使用 ref，支持 selector 兜底。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "click", "session", "浏览器", "点击"},
			Priority:    6,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "观察返回的 ref，例如 @e1"},
				"selector":    map[string]interface{}{"type": "string", "description": "可选 CSS selector"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Click(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, result.Display, result.Data)
			},
		},
		{
			Name:        "browser_type",
			Description: "在 browser session 内输入文本，优先使用 ref，支持 selector 兜底。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "type", "input", "session", "浏览器", "输入"},
			Priority:    6,
			Required:    []string{"session_id", "text"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "观察返回的 ref，例如 @e1"},
				"selector":    map[string]interface{}{"type": "string", "description": "可选 CSS selector"},
				"text":        map[string]interface{}{"type": "string", "description": "输入文本"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Type(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""), strArg(args, "text", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, result.Display, result.Data)
			},
		},
		{
			Name:        "browser_wait",
			Description: "等待页面稳定，支持 duration_ms 或等待指定 ref/selector 出现。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "wait", "session", "浏览器", "等待"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "可选 ref"},
				"selector":    map[string]interface{}{"type": "string", "description": "可选 selector"},
				"duration_ms": map[string]interface{}{"type": "integer", "description": "等待毫秒数，默认 1000"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Wait(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""), intArg(args, "duration_ms", 1000))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, result.Display, result.Data)
			},
		},
		{
			Name:        "browser_refresh",
			Description: "刷新当前 browser session 页面，并自动 observe。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "refresh", "session", "浏览器", "刷新"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Refresh()
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, result.Display, result.Data)
			},
		},
		{
			Name:        "browser_back",
			Description: "在 browser session 内后退到上一页，并自动 observe。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "back", "session", "浏览器", "后退"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Back()
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, result.Display, result.Data)
			},
		},
		{
			Name:        "browser_extract",
			Description: "从当前页面或 snapshot 中提取文本，支持按 ref/selector 或整页摘要提取。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "extract", "text", "session", "浏览器", "提取"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "可选 ref"},
				"selector":    map[string]interface{}{"type": "string", "description": "可选 selector"},
				"query":       map[string]interface{}{"type": "string", "description": "提取目标说明"},
				"format":      map[string]interface{}{"type": "string", "description": "返回格式，默认 text"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Extract(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""), strArg(args, "query", ""), strArg(args, "format", "text"))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, result.Display, result.Data)
			},
		},
		{
			Name:        "browser_connect",
			Description: "连接到浏览器（自动发现或启动）。优先连接已有调试端口；如果没有，会自动用用户的 Chrome/Edge 默认 profile 启动（保留登录态）。可选参数 addr 手动指定 CDP 地址。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "test", "automation", "浏览器", "连接", "网页"},
			Priority:    5,
			InputSchema: map[string]interface{}{
				"addr": map[string]interface{}{"type": "string", "description": "CDP 地址，默认 http://127.0.0.1:9222"},
			},
			Handler: func(args map[string]interface{}) string {
				a := strArg(args, "addr", addr)
				sess, err := GetSession(a)
				if err != nil {
					return fmt.Sprintf("浏览器连接失败。\n\n根因:\n%s\n\n建议:\n- 优先检查 127.0.0.1:9222 是否被其他程序占用\n- 若浏览器已在运行，请确认它暴露了有效 CDP /json 端点", err)
				}
				pages, _ := sess.ListPages()
				var info []string
				for _, p := range pages {
					if p.Type == "page" {
						id := p.ID
						if len(id) > 8 {
							id = id[:8]
						}
						info = append(info, fmt.Sprintf("  [%s] %s - %s", id, p.Title, p.URL))
					}
				}
				return fmt.Sprintf("已连接到浏览器\n当前页面:\n%s", strings.Join(info, "\n"))
			},
		},
		{
			Name:        "browser_screenshot",
			Description: "截取当前页面的屏幕截图，返回 base64 编码的 PNG 图片。可选 full_page 参数截取整个页面。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "screenshot", "浏览器", "截图", "网页"},
			Priority:    5,
			InputSchema: map[string]interface{}{
				"full_page": map[string]interface{}{"type": "boolean", "description": "是否截取整个页面（默认 false，只截取可视区域）"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				fullPage, _ := args["full_page"].(bool)
				data, err := sess.Screenshot(fullPage)
				if err != nil {
					return fmt.Sprintf("截图失败: %s", err)
				}
				result, _ := json.Marshal(map[string]interface{}{
					"type":   "image",
					"format": "png",
					"base64": data,
				})
				return string(result)
			},
		},
		{
			Name:        "browser_get_text",
			Description: "获取匹配 CSS 选择器的元素的文本内容。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "text", "浏览器", "文本", "获取", "网页"},
			Priority:    5,
			Required:    []string{"selector"},
			InputSchema: map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				text, err := sess.GetText(strArg(args, "selector", ""))
				if err != nil {
					return fmt.Sprintf("获取文本失败: %s", err)
				}
				return text
			},
		},
		{
			Name:        "browser_get_html",
			Description: "获取匹配 CSS 选择器的元素的 HTML。selector 为空则返回整个页面 HTML（截断到 50KB）。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "html", "浏览器", "网页", "源码"},
			Priority:    4,
			InputSchema: map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "CSS 选择器（留空返回整页）"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				html, err := sess.GetHTML(strArg(args, "selector", ""))
				if err != nil {
					return fmt.Sprintf("获取 HTML 失败: %s", err)
				}
				return html
			},
		},
		{
			Name:        "browser_eval",
			Description: "在当前页面执行任意 JavaScript 代码并返回结果。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "javascript", "eval", "浏览器", "执行", "脚本", "网页"},
			Priority:    5,
			Required:    []string{"expression"},
			InputSchema: map[string]interface{}{
				"expression": map[string]interface{}{"type": "string", "description": "要执行的 JavaScript 表达式"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				result, err := sess.Eval(strArg(args, "expression", ""))
				if err != nil {
					return fmt.Sprintf("执行失败: %s", err)
				}
				return result
			},
		},
		{
			Name:        "browser_scroll",
			Description: "滚动页面。delta_y 正值向下滚动，负值向上。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "scroll", "浏览器", "滚动", "网页"},
			Priority:    3,
			InputSchema: map[string]interface{}{
				"delta_x": map[string]interface{}{"type": "integer", "description": "水平滚动像素（默认 0）"},
				"delta_y": map[string]interface{}{"type": "integer", "description": "垂直滚动像素（默认 500）"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				dx := intArg(args, "delta_x", 0)
				dy := intArg(args, "delta_y", 500)
				if err := sess.Scroll(dx, dy); err != nil {
					return fmt.Sprintf("滚动失败: %s", err)
				}
				return fmt.Sprintf("已滚动 dx=%d dy=%d", dx, dy)
			},
		},
		{
			Name:        "browser_select",
			Description: "在 <select> 下拉框中选择指定值。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "select", "浏览器", "选择", "下拉", "网页"},
			Priority:    3,
			Required:    []string{"selector", "value"},
			InputSchema: map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
				"value":    map[string]interface{}{"type": "string", "description": "要选择的 option value"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				if err := sess.Select(strArg(args, "selector", ""), strArg(args, "value", "")); err != nil {
					return fmt.Sprintf("选择失败: %s", err)
				}
				return "已选择"
			},
		},
		{
			Name:        "browser_list_pages",
			Description: "列出浏览器中所有打开的页面（标签页）。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "pages", "tabs", "浏览器", "标签页", "网页"},
			Priority:    4,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				pages, err := sess.ListPages()
				if err != nil {
					return fmt.Sprintf("列出页面失败: %s", err)
				}
				var lines []string
				for _, p := range pages {
					if p.Type == "page" {
						id := p.ID
						if len(id) > 8 {
							id = id[:8]
						}
						lines = append(lines, fmt.Sprintf("[%s] %s - %s", id, p.Title, p.URL))
					}
				}
				if len(lines) == 0 {
					return "没有打开的页面"
				}
				return strings.Join(lines, "\n")
			},
		},
		{
			Name:        "browser_switch_page",
			Description: "切换到指定的页面标签页（通过 target_id，可从 browser_list_pages 获取）。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "switch", "tab", "浏览器", "切换", "标签页", "网页"},
			Priority:    3,
			Required:    []string{"target_id"},
			InputSchema: map[string]interface{}{
				"target_id": map[string]interface{}{"type": "string", "description": "目标页面 ID（browser_list_pages 返回的 ID）"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				tid := strArg(args, "target_id", "")
				if err := sess.SwitchPage(tid); err != nil {
					return fmt.Sprintf("切换失败: %s", err)
				}
				return fmt.Sprintf("已切换到页面: %s", tid)
			},
		},
		{
			Name:        "browser_close",
			Description: "断开与浏览器的 CDP 连接（不会关闭浏览器本身）。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "close", "浏览器", "关闭", "断开"},
			Priority:    3,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				CloseSession()
				return "已断开浏览器连接"
			},
		},
		{
			Name:        "browser_click_at",
			Description: "CDP 级别真实鼠标点击（Input.dispatchMouseEvent）。算用户手势，能触发文件对话框、绕过反自动化检测。适合 el.click() 无效的场景。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "click", "automation", "浏览器", "点击", "真实点击", "网页"},
			Priority:    5,
			Required:    []string{"selector"},
			InputSchema: map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				sel := strArg(args, "selector", "")
				if err := sess.ClickAt(sel); err != nil {
					return fmt.Sprintf("点击失败: %s", err)
				}
				return fmt.Sprintf("已真实鼠标点击: %s", sel)
			},
		},
		{
			Name:        "browser_set_files",
			Description: "给 file input 元素设置本地文件路径，绕过文件对话框。用于自动化文件上传。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "upload", "file", "浏览器", "上传", "文件", "网页"},
			Priority:    4,
			Required:    []string{"selector", "files"},
			InputSchema: map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "file input 的 CSS 选择器，如 input[type=file]"},
				"files":    map[string]interface{}{"type": "string", "description": "本地文件路径，多个用逗号分隔"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				sel := strArg(args, "selector", "")
				filesStr := strArg(args, "files", "")
				if filesStr == "" {
					return "缺少 files 参数"
				}
				files := strings.Split(filesStr, ",")
				for i := range files {
					files[i] = strings.TrimSpace(files[i])
				}
				if err := sess.SetFiles(sel, files); err != nil {
					return fmt.Sprintf("设置文件失败: %s", err)
				}
				return fmt.Sprintf("已设置 %d 个文件到 %s", len(files), sel)
			},
		},
		{
			Name:        "browser_info",
			Description: "获取当前页面的标题、URL 和加载状态（一次调用返回所有信息）。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "info", "浏览器", "信息", "状态", "网页"},
			Priority:    5,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return sessionError(err)
				}
				info, err := sess.Info()
				if err != nil {
					return fmt.Sprintf("获取信息失败: %s", err)
				}
				result, _ := json.Marshal(info)
				return string(result)
			},
		},
	}

	for _, t := range tools {
		t.Status = tool.StatusAvailable
		t.Source = "builtin:browser"
		registry.Register(t)
	}
}

func marshalBrowserResult(ok bool, display string, data map[string]interface{}) string {
	payload := map[string]interface{}{
		"ok":      ok,
		"display": display,
	}
	if data != nil {
		payload["data"] = data
	}
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

func policyFromArgs(args map[string]interface{}) BrowserPolicy {
	return BrowserPolicy{
		AllowedDomains:             stringSliceArg(args, "allowed_domains"),
		BlockedDomains:             stringSliceArg(args, "blocked_domains"),
		AllowCrossOriginNavigation: boolArg(args, "allow_cross_origin_navigation", true),
		AllowPopup:                 boolArg(args, "allow_popup", false),
		AllowDownload:              boolArg(args, "allow_download", false),
		AllowUpload:                boolArg(args, "allow_upload", false),
		ContentBoundary:            boolArg(args, "content_boundary", true),
	}
}

// ── arg helpers ──

// sessionError returns a concise error for legacy CDP browser tool failures,
// directing the AI to reconnect via browser_connect instead of inventing workarounds.
func sessionError(err error) string {
	return fmt.Sprintf("浏览器连接失败。\n\n根因:\n%s\n\n建议:\n- 可先调用 browser_connect 查看更完整的启动诊断\n- 若你在使用 browser session 工作流，请先调用 browser_session_start", err)
}

func strArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func intArg(args map[string]interface{}, key string, fallback int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return fallback
}

func boolArg(args map[string]interface{}, key string, fallback bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}

func stringSliceArg(args map[string]interface{}, key string) []string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	items, ok := value.([]interface{})
	if !ok {
		if direct, ok := value.([]string); ok {
			return append([]string(nil), direct...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}
