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
			Name:        "browser_connect",
			Description: "连接到浏览器的 CDP 远程调试端口。如果浏览器未启动调试模式，会返回启动指引。可选参数 addr 指定地址（默认 http://127.0.0.1:9222）。",
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
					return fmt.Sprintf("连接失败: %s", err)
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
			Name:        "browser_navigate",
			Description: "在浏览器中导航到指定 URL，等待页面加载完成。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "navigate", "浏览器", "导航", "打开", "网页", "访问"},
			Priority:    5,
			Required:    []string{"url"},
			InputSchema: map[string]interface{}{
				"url": map[string]interface{}{"type": "string", "description": "要导航到的 URL"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return err.Error()
				}
				url := strArg(args, "url", "")
				if url == "" {
					return "缺少 url 参数"
				}
				_, err = sess.Navigate(url)
				if err != nil {
					return fmt.Sprintf("导航失败: %s", err)
				}
				title, _ := sess.Eval("document.title")
				return fmt.Sprintf("已导航到: %s\n页面标题: %s", url, title)
			},
		},
		{
			Name:        "browser_click",
			Description: "点击页面上匹配 CSS 选择器的元素。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "click", "浏览器", "点击", "网页", "操作"},
			Priority:    5,
			Required:    []string{"selector"},
			InputSchema: map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return err.Error()
				}
				sel := strArg(args, "selector", "")
				if err := sess.Click(sel); err != nil {
					return fmt.Sprintf("点击失败: %s", err)
				}
				return fmt.Sprintf("已点击: %s", sel)
			},
		},
		{
			Name:        "browser_type",
			Description: "在匹配 CSS 选择器的输入框中输入文本。会先清空输入框再输入。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "input", "type", "浏览器", "输入", "填写", "网页"},
			Priority:    5,
			Required:    []string{"selector", "text"},
			InputSchema: map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
				"text":     map[string]interface{}{"type": "string", "description": "要输入的文本"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return err.Error()
				}
				sel := strArg(args, "selector", "")
				text := strArg(args, "text", "")
				if err := sess.Type(sel, text); err != nil {
					return fmt.Sprintf("输入失败: %s", err)
				}
				return fmt.Sprintf("已在 %s 中输入 %d 个字符", sel, len([]rune(text)))
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
					return err.Error()
				}
				fullPage, _ := args["full_page"].(bool)
				data, err := sess.Screenshot(fullPage)
				if err != nil {
					return fmt.Sprintf("截图失败: %s", err)
				}
				// Return as a structured response so callers can extract the image.
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
					return err.Error()
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
					return err.Error()
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
					return err.Error()
				}
				result, err := sess.Eval(strArg(args, "expression", ""))
				if err != nil {
					return fmt.Sprintf("执行失败: %s", err)
				}
				return result
			},
		},
		{
			Name:        "browser_wait",
			Description: "等待匹配 CSS 选择器的元素出现在页面上。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "wait", "浏览器", "等待", "网页"},
			Priority:    4,
			Required:    []string{"selector"},
			InputSchema: map[string]interface{}{
				"selector": map[string]interface{}{"type": "string", "description": "CSS 选择器"},
				"timeout":  map[string]interface{}{"type": "integer", "description": "超时秒数（默认 10）"},
			},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return err.Error()
				}
				sel := strArg(args, "selector", "")
				timeout := intArg(args, "timeout", 10)
				if err := sess.WaitForSelector(sel, timeout); err != nil {
					return err.Error()
				}
				return fmt.Sprintf("元素已出现: %s", sel)
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
					return err.Error()
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
					return err.Error()
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
					return err.Error()
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
					return err.Error()
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
					return err.Error()
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
					return err.Error()
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
			Name:        "browser_back",
			Description: "浏览器后退（history.back），等待页面加载完成。",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"browser", "web", "navigate", "back", "浏览器", "后退", "网页"},
			Priority:    3,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				sess, err := GetSession(addr)
				if err != nil {
					return err.Error()
				}
				if err := sess.Back(); err != nil {
					return fmt.Sprintf("后退失败: %s", err)
				}
				title, _ := sess.Eval("document.title")
				return fmt.Sprintf("已后退，当前页面: %s", title)
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
					return err.Error()
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

// ── arg helpers ──

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
