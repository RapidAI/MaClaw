package main

import (
	"strings"
	"testing"
)

func TestRegisterBrowserToolsIncludesSessionMVP(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{}
	app.browserSessions = NewBrowserAgentManager(app)
	registerBrowserTools(registry, app)
	for _, name := range []string{
		"browser_session_start",
		"browser_session_stop",
		"browser_observe",
		"browser_navigate",
		"browser_click",
		"browser_type",
		"browser_wait",
		"browser_back",
		"browser_refresh",
		"browser_extract",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("tool %q not found", name)
		}
		if tool.Handler == nil {
			t.Fatalf("tool %q handler is nil", name)
		}
	}
}

func TestRegisterBrowserToolsPassesContinuationSchema(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{}
	app.browserSessions = NewBrowserAgentManager(app)
	registerBrowserTools(registry, app)

	tool, ok := registry.Get("browser_extract")
	if !ok || tool == nil {
		t.Fatal("browser_extract missing")
	}
	for _, field := range []string{"offset", "max_chars"} {
		entry, ok := tool.InputSchema[field]
		if !ok {
			t.Fatalf("browser_extract missing %q", field)
		}
		meta, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("schema[%q] = %#v", field, entry)
		}
		if meta["type"] != "integer" {
			t.Fatalf("schema[%q].type = %#v", field, meta["type"])
		}
	}
}

func TestHiddenBrowserToolsRequireSessionID(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{}
	app.browserSessions = NewBrowserAgentManager(app)
	registerBrowserTools(registry, app)

	tool, ok := registry.Get("browser_info")
	if !ok || tool == nil || tool.Handler == nil {
		t.Fatal("browser_info missing")
	}
	got := tool.Handler(map[string]interface{}{})
	if !strings.Contains(got, "requires session_id") {
		t.Fatalf("browser_info without session = %q, want session_id rejection", got)
	}

	if !browserToolRequiresSessionID("browser_info") {
		t.Fatal("browser_info should require session_id")
	}
	if browserToolRequiresSessionID("browser_connect") || browserToolRequiresSessionID("browser_session_start") || browserToolRequiresSessionID("browser_list_flows") {
		t.Fatal("connect/session_start/list_flows should not require session_id")
	}
}

func TestMergedBrowserConnectDispatchesToSessionStart(t *testing.T) {
	registry := NewToolRegistry()
	var received map[string]interface{}
	_ = registry.Register(RegisteredTool{
		Name: "browser_session_start",
		Handler: func(args map[string]interface{}) string {
			received = args
			return "ok"
		},
	})
	_ = registry.Register(RegisteredTool{
		Name: "browser_connect",
		Handler: func(args map[string]interface{}) string {
			return "legacy"
		},
	})

	original := map[string]interface{}{"action": "connect"}
	if got := dispatchMergedBrowser(registry, original); got != "ok" {
		t.Fatalf("dispatchMergedBrowser = %q, want ok", got)
	}
	if received["action"] != "session_start" {
		t.Fatalf("action = %#v, want session_start", received["action"])
	}
	if received["reuse_existing"] != true {
		t.Fatalf("reuse_existing = %#v, want true", received["reuse_existing"])
	}
	if received["mode"] != "persistent" {
		t.Fatalf("mode = %#v, want persistent", received["mode"])
	}
	if original["action"] != "connect" {
		t.Fatalf("original args mutated: %#v", original)
	}
}

func TestMergedBrowserPageActionsRequireSessionID(t *testing.T) {
	registry := NewToolRegistry()
	_ = registry.Register(RegisteredTool{
		Name: "browser_info",
		Handler: func(args map[string]interface{}) string {
			return "legacy-global"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{"action": "info"})
	if !strings.Contains(got, "requires session_id") {
		t.Fatalf("dispatchMergedBrowser = %q, want session_id error", got)
	}

	got = dispatchMergedBrowser(registry, map[string]interface{}{"action": "task_status", "task_id": "task-1"})
	if !strings.Contains(got, "requires session_id") {
		t.Fatalf("dispatchMergedBrowser task_status = %q, want session_id error", got)
	}
}

func TestMergedBrowserTaskVerifyMapsSuccessCriteria(t *testing.T) {
	registry := NewToolRegistry()
	var received map[string]interface{}
	_ = registry.Register(RegisteredTool{
		Name: "browser_task_verify",
		Handler: func(args map[string]interface{}) string {
			received = args
			return "ok"
		},
	})

	criteria := []interface{}{map[string]interface{}{"type": "url_contains", "pattern": "zhihu"}}
	got := dispatchMergedBrowser(registry, map[string]interface{}{
		"action":           "task_verify",
		"session_id":       "browser-session-test",
		"success_criteria": criteria,
	})
	if got != "ok" {
		t.Fatalf("dispatchMergedBrowser = %q, want ok", got)
	}
	if received["criteria"] == nil {
		t.Fatalf("criteria not mapped: %#v", received)
	}
}

func TestMergedBrowserSessionStartDefaultsToPersistent(t *testing.T) {
	registry := NewToolRegistry()
	var received map[string]interface{}
	_ = registry.Register(RegisteredTool{
		Name: "browser_session_start",
		Handler: func(args map[string]interface{}) string {
			received = args
			return "ok"
		},
	})

	original := map[string]interface{}{"action": "session_start"}
	if got := dispatchMergedBrowser(registry, original); got != "ok" {
		t.Fatalf("dispatchMergedBrowser = %q, want ok", got)
	}
	if received["mode"] != "persistent" {
		t.Fatalf("mode = %#v, want persistent", received["mode"])
	}
	if _, ok := original["mode"]; ok {
		t.Fatalf("original args mutated: %#v", original)
	}
}

func TestMergedBrowserSessionStartMapsAutoToPersistent(t *testing.T) {
	registry := NewToolRegistry()
	var received map[string]interface{}
	_ = registry.Register(RegisteredTool{
		Name: "browser_session_start",
		Handler: func(args map[string]interface{}) string {
			received = args
			return "ok"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{"action": "session_start", "mode": "auto"})
	if got != "ok" {
		t.Fatalf("dispatchMergedBrowser = %q, want ok", got)
	}
	if received["mode"] != "persistent" {
		t.Fatalf("mode = %#v, want persistent", received["mode"])
	}
}

func TestMergedBrowserSessionStopNeverKillsBrowserProcess(t *testing.T) {
	registry := NewToolRegistry()
	var received map[string]interface{}
	_ = registry.Register(RegisteredTool{
		Name: "browser_session_stop",
		Handler: func(args map[string]interface{}) string {
			received = args
			return "ok"
		},
	})

	original := map[string]interface{}{
		"action":        "session_stop",
		"session_id":    "browser-session-test",
		"close_browser": true,
	}
	if got := dispatchMergedBrowser(registry, original); got != "ok" {
		t.Fatalf("dispatchMergedBrowser = %q, want ok", got)
	}
	if received["close_browser"] != false {
		t.Fatalf("close_browser = %#v, want forced false", received["close_browser"])
	}
	if original["close_browser"] != true {
		t.Fatalf("original args mutated: %#v", original)
	}
	if _, ok := mergedBrowserInputSchema["close_browser"]; ok {
		t.Fatal("merged browser schema should not expose close_browser to the LLM")
	}
}

func TestMergedBrowserAcceptsFullBrowserActionName(t *testing.T) {
	registry := NewToolRegistry()
	var called bool
	_ = registry.Register(RegisteredTool{
		Name: "browser_info",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "ok"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{"action": "browser_info", "session_id": "browser-session-test"})
	if got != "ok" || !called {
		t.Fatalf("dispatchMergedBrowser = %q called=%v, want ok/true", got, called)
	}
}

func TestMergedBrowserRejectsRegisteredUnknownBrowserAction(t *testing.T) {
	registry := NewToolRegistry()
	_ = registry.Register(RegisteredTool{
		Name: "browser_legacy_fallback",
		Handler: func(args map[string]interface{}) string {
			return "legacy-fallback"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{
		"action":     "legacy_fallback",
		"session_id": "browser-session-test",
	})
	if strings.Contains(got, "legacy-fallback") || !strings.Contains(got, "unknown browser action") {
		t.Fatalf("dispatchMergedBrowser = %q, want registered unknown action rejected", got)
	}
}

func TestMergedBrowserObserveForcesScreenshotOff(t *testing.T) {
	registry := NewToolRegistry()
	var received map[string]interface{}
	_ = registry.Register(RegisteredTool{
		Name: "browser_observe",
		Handler: func(args map[string]interface{}) string {
			received = args
			return "ok"
		},
	})

	original := map[string]interface{}{
		"action":             "observe",
		"session_id":         "browser-session-test",
		"include_screenshot": true,
	}
	if got := dispatchMergedBrowser(registry, original); got != "ok" {
		t.Fatalf("dispatchMergedBrowser = %q, want ok", got)
	}
	if received["include_screenshot"] != false {
		t.Fatalf("include_screenshot = %#v, want forced false", received["include_screenshot"])
	}
	if original["include_screenshot"] != true {
		t.Fatalf("original args mutated: %#v", original)
	}
}

func TestMergedBrowserRejectsUnstableRecordActions(t *testing.T) {
	registry := NewToolRegistry()
	_ = registry.Register(RegisteredTool{
		Name: "browser_record_start",
		Handler: func(args map[string]interface{}) string {
			return "legacy-record"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{"action": "record_start"})
	if !strings.Contains(got, "not wired to the stable session path") {
		t.Fatalf("dispatchMergedBrowser = %q, want unstable action rejection", got)
	}
}

func TestMergedBrowserRejectsMojibakeArgs(t *testing.T) {
	registry := NewToolRegistry()
	_ = registry.Register(RegisteredTool{
		Name: "browser_click",
		Handler: func(args map[string]interface{}) string {
			return "legacy-click"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{
		"action":     "click",
		"session_id": "browser-session-test",
		"text":       "\ufffd",
	})
	if !strings.Contains(got, "mojibake") {
		t.Fatalf("dispatchMergedBrowser = %q, want mojibake rejection", got)
	}

	got = dispatchMergedBrowser(registry, map[string]interface{}{
		"action":     "click",
		"session_id": "browser-session-test",
		"text":       "\u9352\u638d\u68d4\u95ca\u2541\u5b62\u9288\u544a\u715d\u95b9\u2248\u6ad5\u7ead",
	})
	if !strings.Contains(got, "mojibake") {
		t.Fatalf("dispatchMergedBrowser = %q, want common mojibake rejection", got)
	}
}

func TestMergedBrowserRejectsEval(t *testing.T) {
	registry := NewToolRegistry()
	_ = registry.Register(RegisteredTool{
		Name: "browser_eval",
		Handler: func(args map[string]interface{}) string {
			return "legacy-eval"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{
		"action":     "eval",
		"session_id": "browser-session-test",
		"expression": "document.title",
	})
	if !strings.Contains(got, "not wired to the stable session path") {
		t.Fatalf("dispatchMergedBrowser eval = %q, want stable path rejection", got)
	}
}

func TestMergedBrowserRejectsUnstableBrowserActions(t *testing.T) {
	registry := NewToolRegistry()
	for _, name := range []string{"browser_click_at", "browser_get_text", "browser_get_html"} {
		toolName := name
		_ = registry.Register(RegisteredTool{
			Name: toolName,
			Handler: func(args map[string]interface{}) string {
				return "legacy-" + toolName
			},
		})
	}

	for _, action := range []string{"click_at", "get_text", "get_html"} {
		got := dispatchMergedBrowser(registry, map[string]interface{}{
			"action":     action,
			"session_id": "browser-session-test",
			"selector":   "button",
		})
		if !strings.Contains(got, "not wired to the stable session path") {
			t.Fatalf("dispatchMergedBrowser %s = %q, want stable path rejection", action, got)
		}
	}
}

func TestMergedBrowserSupportedActionsHideDisabledPaths(t *testing.T) {
	supported := strings.Join(browserSupportedActionNames(), ",")
	for _, disabled := range []string{"eval", "click_at", "get_text", "get_html", "screenshot", "ocr", "task_replay", "record_start", "record_stop"} {
		if strings.Contains(supported, disabled) {
			t.Fatalf("supported browser actions include disabled path %q: %s", disabled, supported)
		}
	}
	if _, ok := mergedBrowserInputSchema["expression"]; ok {
		t.Fatal("merged browser schema should not expose expression after eval was disabled")
	}
	for _, field := range []string{"x", "y"} {
		if _, ok := mergedBrowserInputSchema[field]; ok {
			t.Fatalf("merged browser schema should not expose %s", field)
		}
	}
	if _, ok := mergedBrowserInputSchema["full_page"]; ok {
		t.Fatal("merged browser schema should not expose full_page after screenshot was disabled")
	}
}

func TestMergedBrowserTypeExposesMarkdownContentFormat(t *testing.T) {
	field, ok := mergedBrowserInputSchema["content_format"]
	if !ok {
		t.Fatal("merged browser schema missing content_format")
	}
	fieldMap, ok := field.(map[string]string)
	if !ok {
		t.Fatalf("content_format schema = %#v", field)
	}
	if !strings.Contains(fieldMap["description"], "markdown") || !strings.Contains(mergedBrowserToolDescription, "content_format") {
		t.Fatalf("content_format markdown guidance missing: %#v", fieldMap)
	}
	stepsField, ok := mergedBrowserInputSchema["steps"].(map[string]string)
	if !ok || !strings.Contains(stepsField["description"], "params.content_format=markdown") {
		t.Fatalf("task_run steps markdown guidance missing: %#v", mergedBrowserInputSchema["steps"])
	}
}

func TestMergedBrowserRejectsScreenshotAction(t *testing.T) {
	registry := NewToolRegistry()
	_ = registry.Register(RegisteredTool{
		Name: "browser_screenshot",
		Handler: func(args map[string]interface{}) string {
			return "legacy-screenshot"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{
		"action":     "screenshot",
		"session_id": "browser-session-test",
	})
	if !strings.Contains(got, "not wired to the stable session path") {
		t.Fatalf("dispatchMergedBrowser screenshot = %q, want stable path rejection", got)
	}
}

func TestMergedBrowserRejectsOCRAction(t *testing.T) {
	registry := NewToolRegistry()
	_ = registry.Register(RegisteredTool{
		Name: "browser_ocr",
		Handler: func(args map[string]interface{}) string {
			return "legacy-ocr"
		},
	})

	got := dispatchMergedBrowser(registry, map[string]interface{}{
		"action":     "ocr",
		"session_id": "browser-session-test",
	})
	if !strings.Contains(got, "not wired to the stable session path") {
		t.Fatalf("dispatchMergedBrowser ocr = %q, want stable path rejection", got)
	}
}
