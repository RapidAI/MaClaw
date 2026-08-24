package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const browserRuntimePolicyOwnerIDArg = "_runtime_policy_owner_id"

// RegisterTools registers all browser automation tools into the given registry.
func RegisterTools(registry *tool.Registry) {
	addr := "" // will use default; can be made configurable later

	tools := []tool.RegisteredTool{
		{
			Name:        "browser_session_start",
			Description: "Start or reuse a stable browser agent session. Defaults to persistent managed profile, preserving login/cookies.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "session", "agent", "浏览器", "会话", "网页"},
			Priority:    6,
			InputSchema: map[string]interface{}{
				"addr":                          map[string]interface{}{"type": "string", "description": "可选 CDP 地址，默认自动发现或启动"},
				"start_url":                     map[string]interface{}{"type": "string", "description": "可选初始 URL"},
				"reuse_existing":                map[string]interface{}{"type": "boolean", "description": "是否优先复用已有 browser session，默认 true"},
				"mode":                          map[string]interface{}{"type": "string", "description": "persistent (default, preserves login/cookies) / isolated / connect_user / auto (maps to persistent)"},
				"allowed_domains":               map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "允许访问的域名列表"},
				"blocked_domains":               map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "禁止访问的域名列表"},
				"allow_cross_origin_navigation": map[string]interface{}{"type": "boolean", "description": "是否允许跨域导航"},
				"allow_popup":                   map[string]interface{}{"type": "boolean", "description": "是否允许弹出新标签，默认 false"},
				"allow_download":                map[string]interface{}{"type": "boolean", "description": "是否允许页面触发下载，默认 false"},
				"allow_upload":                  map[string]interface{}{"type": "boolean", "description": "是否允许 set_files 上传，默认 false"},
			},
			Handler: func(args map[string]interface{}) string {
				policy := policyFromArgs(args)
				reuseExisting := boolArg(args, "reuse_existing", true)
				mode := stableToolSessionMode(args)
				ownerID := runtimePolicyOwnerFromBrowserArgs(args)
				agentSession, err := StartAgentSessionForOwner(ownerID, strArg(args, "addr", addr), policy, reuseExisting, mode)
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				if err := agentSession.OpenURL(strArg(args, "start_url", "")); err != nil {
					return marshalBrowserResult(false, err.Error(), map[string]interface{}{"session_id": agentSession.ID})
				}
				state := agentSession.State()
				modeLabel := "persistent (login/cookies preserved)"
				if agentSession.Mode == SessionModeConnectUser {
					modeLabel = "connect_user (复用登录态)"
				} else if agentSession.Mode == SessionModePersistent {
					modeLabel = "persistent (login/cookies preserved)"
				} else if agentSession.Mode == SessionModeIsolated {
					modeLabel = "isolated (隔离环境)"
				}
				return marshalBrowserResult(true, fmt.Sprintf("已启动浏览器会话 %s [%s]", agentSession.ID, modeLabel), map[string]interface{}{
					"session_id":  state.ID,
					"owner_id":    state.OwnerID,
					"target_id":   state.TargetID,
					"url":         state.CurrentURL,
					"title":       state.CurrentTitle,
					"ready_state": state.ReadyState,
					"mode":        string(agentSession.Mode),
				})
			},
		},
		{
			Name:        "browser_session_stop",
			Description: "Stop a browser session handle. Persistent browser process and login/cookies are preserved; only isolated debug sessions may be closed by process cleanup.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "session", "stop", "浏览器", "关闭"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
			},
			Handler: func(args map[string]interface{}) string {
				sessionID := strArg(args, "session_id", "")
				if err := StopAgentSession(sessionID, false); err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, fmt.Sprintf("已关闭浏览器会话 %s", sessionID), map[string]interface{}{"session_id": sessionID})
			},
		},
		{
			Name:        "browser_observe",
			Description: "Observe current page and stable refs. Screenshots are disabled in the stable browser path.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "observe", "snapshot", "浏览器", "观察", "网页"},
			Priority:    6,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":         map[string]interface{}{"type": "string", "description": "browser session id"},
				"include_screenshot": map[string]interface{}{"type": "boolean", "description": "Ignored; screenshots disabled, default false"},
				"query":              map[string]interface{}{"type": "string", "description": "可选：只保留 name/text/role 匹配该词的 refs"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				obs, err := agentSession.ObserveFiltered(strArg(args, "query", ""))
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
				"expect":     map[string]interface{}{"type": "string", "description": "可选后置条件，如 url_contains:login"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Navigate(strArg(args, "url", ""))
				return marshalActionResult(agentSession, result, err, ParseExpect(strArg(args, "expect", "")))
			},
		},
		{
			Name:        "browser_click",
			Description: "Click by stable ref, selector, or visible text inside a browser session.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "click", "session", "浏览器", "点击"},
			Priority:    6,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "观察返回的 ref，例如 @e1"},
				"selector":    map[string]interface{}{"type": "string", "description": "可选 CSS selector"},
				"text":        map[string]interface{}{"type": "string", "description": "Visible text fallback when ref/selector is empty"},
				"expect":      map[string]interface{}{"type": "string", "description": "可选后置条件，如 url_contains:cart 或 text:成功"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				snapshotID := strArg(args, "snapshot_id", "")
				ref := strArg(args, "ref", "")
				selector := strArg(args, "selector", "")
				var result *BrowserActionResult
				if strings.TrimSpace(ref) == "" && strings.TrimSpace(selector) == "" && strings.TrimSpace(strArg(args, "text", "")) != "" {
					result, err = agentSession.ClickText(snapshotID, strArg(args, "text", ""))
				} else {
					result, err = agentSession.Click(snapshotID, ref, selector)
				}
				return marshalActionResult(agentSession, result, err, ParseExpect(strArg(args, "expect", "")))
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
				"content_format": map[string]interface{}{
					"type":        "string",
					"description": "plain (default) or markdown. Use markdown for rich editors/article publishing so Markdown renders as headings/lists/bold instead of raw text.",
				},
				"append": map[string]interface{}{"type": "boolean", "description": "true 时追加输入，不清空现有内容"},
				"expect": map[string]interface{}{"type": "string", "description": "可选后置条件"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.TypeContentAppend(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""), strArg(args, "text", ""), strArg(args, "content_format", ""), boolArg(args, "append", false))
				return marshalActionResult(agentSession, result, err, ParseExpect(strArg(args, "expect", "")))
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
				return marshalActionResult(agentSession, result, err, ExpectSpec{})
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
				return marshalActionResult(agentSession, result, err, ExpectSpec{})
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
				return marshalActionResult(agentSession, result, err, ExpectSpec{})
			},
		},
		{
			Name:        "browser_extract",
			Description: "从当前页面或 snapshot 中提取文本，支持按 ref/selector 或整页摘要提取。整页提取支持 offset/max_chars 续读。",
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
				"offset":      map[string]interface{}{"type": "integer", "description": "从第几个字符开始提取（用于整页续读，默认 0）"},
				"max_chars":   map[string]interface{}{"type": "integer", "description": "最多返回字符数（用于整页续读，默认 1200）"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := GetAgentSession(strArg(args, "session_id", ""))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				result, err := agentSession.Extract(
					strArg(args, "snapshot_id", ""),
					strArg(args, "ref", ""),
					strArg(args, "selector", ""),
					strArg(args, "query", ""),
					strArg(args, "format", "text"),
					intArg(args, "offset", 0),
					intArg(args, "max_chars", 1200),
				)
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				return marshalBrowserResult(true, result.Display, result.Data)
			},
		},
		{
			Name:        "browser_connect",
			Description: "Compatibility alias for browser_session_start. Starts/reuses a stable persistent browser session and returns browser-session-*.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "test", "automation", "浏览器", "连接", "网页"},
			Priority:    5,
			InputSchema: map[string]interface{}{
				"addr":           map[string]interface{}{"type": "string", "description": "Optional CDP address"},
				"start_url":      map[string]interface{}{"type": "string", "description": "Optional initial URL"},
				"reuse_existing": map[string]interface{}{"type": "boolean", "description": "Reuse existing session, default true"},
				"mode":           map[string]interface{}{"type": "string", "description": "persistent (default) / isolated / auto / connect_user"},
			},
			Handler: func(args map[string]interface{}) string {
				policy := policyFromArgs(args)
				reuseExisting := boolArg(args, "reuse_existing", true)
				ownerID := runtimePolicyOwnerFromBrowserArgs(args)
				agentSession, err := StartAgentSessionForOwner(ownerID, strArg(args, "addr", addr), policy, reuseExisting, stableToolSessionMode(args))
				if err != nil {
					return marshalBrowserResult(false, err.Error(), nil)
				}
				if err := agentSession.OpenURL(strArg(args, "start_url", "")); err != nil {
					return marshalBrowserResult(false, err.Error(), map[string]interface{}{"session_id": agentSession.ID})
				}
				state := agentSession.State()
				return marshalBrowserResult(true, fmt.Sprintf("browser session ready %s", agentSession.ID), map[string]interface{}{
					"session_id":  state.ID,
					"owner_id":    state.OwnerID,
					"target_id":   state.TargetID,
					"url":         state.CurrentURL,
					"title":       state.CurrentTitle,
					"ready_state": state.ReadyState,
					"mode":        string(agentSession.Mode),
				})
			},
		},
		{
			Name:        "browser_scroll",
			Description: "滚动页面。可带 ref 滚到元素；delta_y 正值向下滚动，负值向上。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "scroll", "浏览器", "滚动", "网页"},
			Priority:    3,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "可选 ref，滚到该元素"},
				"selector":    map[string]interface{}{"type": "string", "description": "可选 CSS selector"},
				"delta_x":     map[string]interface{}{"type": "integer", "description": "水平滚动像素（默认 0）"},
				"delta_y":     map[string]interface{}{"type": "integer", "description": "垂直滚动像素（默认 500）"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				result, err := agentSession.ScrollBy(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""), intArg(args, "delta_x", 0), intArg(args, "delta_y", 500))
				return marshalActionResult(agentSession, result, err, ExpectSpec{})
			},
		},
		{
			Name:        "browser_select",
			Description: "在 <select> 下拉框中选择指定值，优先 ref。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "select", "浏览器", "选择", "下拉", "网页"},
			Priority:    3,
			Required:    []string{"session_id", "value"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "观察返回的 ref"},
				"selector":    map[string]interface{}{"type": "string", "description": "CSS 选择器（无 ref 时使用）"},
				"value":       map[string]interface{}{"type": "string", "description": "要选择的 option value"},
				"expect":      map[string]interface{}{"type": "string", "description": "可选后置条件"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				result, err := agentSession.SelectOption(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""), strArg(args, "value", ""))
				return marshalActionResult(agentSession, result, err, ParseExpect(strArg(args, "expect", "")))
			},
		},
		{
			Name:        "browser_list_pages",
			Description: "列出浏览器中所有打开的页面（标签页）。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "pages", "tabs", "浏览器", "标签页", "网页"},
			Priority:    4,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				sess := agentSession.session
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
			Required:    []string{"session_id", "target_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
				"target_id":  map[string]interface{}{"type": "string", "description": "目标页面 ID（browser_list_pages 返回的 ID）"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				sess := agentSession.session
				tid := strArg(args, "target_id", "")
				if err := sess.SwitchPage(tid); err != nil {
					return fmt.Sprintf("切换失败: %s", err)
				}
				agentSession.TargetID = tid
				return fmt.Sprintf("已切换到页面: %s", tid)
			},
		},
		{
			Name:        "browser_close",
			Description: "断开与浏览器的 CDP 连接（不会关闭浏览器本身）。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "close", "浏览器", "关闭", "断开"},
			Priority:    3,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
			},
			Handler: func(args map[string]interface{}) string {
				sessionID := strings.TrimSpace(strArg(args, "session_id", ""))
				if sessionID == "" {
					return sessionError(fmt.Errorf("missing session_id"))
				}
				if err := StopAgentSession(sessionID, false); err != nil {
					return fmt.Sprintf("关闭浏览器会话失败: %s", err)
				}
				return fmt.Sprintf("已断开浏览器会话: %s", sessionID)
			},
		},
		{
			Name:        "browser_set_files",
			Description: "给 file input 元素设置本地文件路径，绕过文件对话框。需要 session_start 时 allow_upload=true。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "upload", "file", "浏览器", "上传", "文件", "网页"},
			Priority:    4,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "file input 的 ref"},
				"selector":    map[string]interface{}{"type": "string", "description": "file input 的 CSS 选择器，如 input[type=file]"},
				"files":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "本地文件路径"},
				"file_paths":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "files 参数别名"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				files := browserFilesArg(args)
				result, err := agentSession.SetFilesOn(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""), files)
				return marshalActionResult(agentSession, result, err, ExpectSpec{})
			},
		},
		{
			Name:        "browser_hover",
			Description: "将鼠标移到 ref/selector 上，用于展开菜单。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "hover", "menu", "浏览器", "悬停"},
			Priority:    4,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id":  map[string]interface{}{"type": "string", "description": "browser session id"},
				"snapshot_id": map[string]interface{}{"type": "string", "description": "可选 snapshot id"},
				"ref":         map[string]interface{}{"type": "string", "description": "观察返回的 ref"},
				"selector":    map[string]interface{}{"type": "string", "description": "可选 CSS selector"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				result, err := agentSession.Hover(strArg(args, "snapshot_id", ""), strArg(args, "ref", ""), strArg(args, "selector", ""))
				return marshalActionResult(agentSession, result, err, ExpectSpec{})
			},
		},
		{
			Name:        "browser_press",
			Description: "发送按键：Enter / Escape / Tab / 方向键 / 常见快捷键。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "press", "keyboard", "浏览器", "按键"},
			Priority:    4,
			Required:    []string{"session_id", "key"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
				"key":        map[string]interface{}{"type": "string", "description": "Enter, Escape, Tab, ArrowDown 等"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				result, err := agentSession.Press(strArg(args, "key", ""))
				return marshalActionResult(agentSession, result, err, ExpectSpec{})
			},
		},
		{
			Name:        "browser_dialog",
			Description: "处理当前 JavaScript alert/confirm/prompt。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "dialog", "alert", "浏览器", "对话框"},
			Priority:    4,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
				"accept":     map[string]interface{}{"type": "boolean", "description": "true 接受，false 取消，默认 true"},
				"text":       map[string]interface{}{"type": "string", "description": "prompt 输入文本"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				result, err := agentSession.HandleDialog(boolArg(args, "accept", true), strArg(args, "text", ""))
				return marshalActionResult(agentSession, result, err, ExpectSpec{})
			},
		},
		{
			Name:        "browser_info",
			Description: "获取当前页面的标题、URL 和加载状态（一次调用返回所有信息）。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "info", "浏览器", "信息", "状态", "网页"},
			Priority:    5,
			Required:    []string{"session_id"},
			InputSchema: map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string", "description": "browser session id"},
			},
			Handler: func(args map[string]interface{}) string {
				agentSession, err := requireAgentSessionFromArgs(args)
				if err != nil {
					return sessionError(err)
				}
				sess := agentSession.session
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

func runtimePolicyOwnerFromBrowserArgs(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	ownerID := strings.TrimSpace(strArg(args, browserRuntimePolicyOwnerIDArg, ""))
	delete(args, browserRuntimePolicyOwnerIDArg)
	return ownerID
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

func marshalActionResult(s *BrowserAgentSession, result *BrowserActionResult, err error, expect ExpectSpec) string {
	if err != nil {
		extra := map[string]interface{}{}
		var amb *AmbiguousElementError
		if errors.As(err, &amb) && amb != nil {
			extra["refs"] = compactElementRefs(amb.Refs)
		}
		if len(extra) == 0 {
			extra = nil
		}
		return marshalBrowserResult(false, llmSafeBrowserError(err), extra)
	}
	if s != nil {
		result = s.applyGoalClassContract(result, expect)
		result = s.applyExpect(result, expect)
	}
	if result == nil {
		return marshalBrowserResult(false, "empty browser action result", nil)
	}
	if result.Status == "ask" {
		req := result.AskUser
		if req == nil {
			req = captchaAskUserRequest("")
		}
		return agent.AskUserResultMarker(req)
	}
	if s != nil {
		s.rememberSubmitClickIfOK(result.submitRememberKey, result)
	}
	ok := result.Status == "ok" || result.Status == ""
	data := result.Data
	if !ok {
		data = compactFailureDataFromResult(result)
	}
	data = attachLastExpectLedger(s, data)
	return marshalBrowserResult(ok, result.Display, data)
}

func llmSafeBrowserError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	stalePrefix := func() string {
		if i := strings.Index(msg, " is stale"); i > 0 && (strings.HasPrefix(msg, "ref ") || strings.HasPrefix(msg, "text ")) {
			return msg[:i+len(" is stale")] + "; run observe again to get fresh refs"
		}
		return ""
	}
	switch {
	case strings.Contains(msg, "element not found"):
		if prefix := stalePrefix(); prefix != "" {
			return prefix
		}
		return "element not found; run observe again and click by ref"
	case strings.Contains(msg, "wait for selector timed out"):
		if prefix := stalePrefix(); prefix != "" {
			return prefix
		}
		return "wait timed out; run observe again"
	case strings.Contains(msg, "option not found"):
		return "option not found; use the option's visible label or value from observe"
	case strings.Contains(msg, "element occluded"):
		return "element occluded; run observe again and click by ref"
	}
	return msg
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

func requireAgentSessionFromArgs(args map[string]interface{}) (*BrowserAgentSession, error) {
	agentSession, ok, err := agentSessionFromArgs(args)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("missing session_id")
	}
	return agentSession, nil
}

func agentSessionFromArgs(args map[string]interface{}) (*BrowserAgentSession, bool, error) {
	sessionID := strings.TrimSpace(strArg(args, "session_id", ""))
	if sessionID == "" {
		return nil, false, nil
	}
	agentSession, err := GetAgentSession(sessionID)
	if err != nil {
		return nil, true, err
	}
	return agentSession, true, nil
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

func browserFilesArg(args map[string]interface{}) []string {
	for _, key := range []string{"files", "file_paths"} {
		files := stringSliceArg(args, key)
		if len(files) > 0 {
			return files
		}
		if text := strings.TrimSpace(strArg(args, key, "")); text != "" {
			parts := strings.Split(text, ",")
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					out = append(out, part)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

func stableToolSessionMode(args map[string]interface{}) SessionMode {
	mode := SessionMode(strings.ToLower(strings.TrimSpace(strArg(args, "mode", ""))))
	switch mode {
	case "", SessionModeAuto:
		return SessionModePersistent
	case SessionModePersistent, SessionModeIsolated, SessionModeConnectUser:
		return mode
	default:
		return SessionModePersistent
	}
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
