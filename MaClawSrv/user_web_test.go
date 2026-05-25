package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestUserWebServesEmbeddedShell(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "MaClawSrv") || !strings.Contains(w.Body.String(), "/app/app.js") {
		t.Fatalf("user shell = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/app/app.js", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	body := w.Body.String()
	for _, needle := range []string{
		"/api/v1/web/exchange",
		"const hasLaunchToken = params.has(\"launch_token\");",
		"skipToMain: \"Skip to main content\"",
		"skipToMain: \"跳到主要内容\"",
		"document.querySelector(\".skip-link\").textContent = t(\"skipToMain\")",
		"document.querySelector(\".sidebar\").setAttribute(\"aria-label\", t(\"appSections\"))",
		"document.querySelector(\".nav\").setAttribute(\"aria-label\", t(\"userViews\"))",
		"const secretURLKeys = [\"token\", \"access_token\", \"api_key\", \"api_secret\"];",
		"params.has(key) || location.hash.toLowerCase().includes(`${key}=`)",
		"state.token = hasLaunchToken || rawURLSecret ? \"\" : saved;",
		"secretURLKeys.forEach((key) => params.delete(key));",
		"Raw secrets in URLs are not accepted.",
		"/api/v1/me",
		"/api/v1/instances",
		"/api/v1/config/schema",
		"/api/v1/config/validate",
		"/api/v1/config/test",
		"/api/v1/instances/${encodeURIComponent(inst.id)}/messages",
		"state.instanceId ? (state.instances.find((x) => x.id === state.instanceId) || null) : (state.instances[0] || null)",
		"function selectedInstanceMissing()",
		"hiddenMessages: {}",
		"function panelMessageKey()",
		"function hiddenMessageSet(key = panelMessageKey())",
		"function visibleMessages(messages)",
		"Selected assistant instance was not found or is unavailable.",
		"$(\"prompt\").disabled = true;",
		"/api/v1/instances/${encodeURIComponent(inst.id)}/runs/${encodeURIComponent(run.id)}/events",
		"/api/v1/instances/${encodeURIComponent(inst.id)}/runs/${encodeURIComponent(run.id)}/cancel",
		"function resetRunState()",
		"if (state.instanceId !== b.dataset.instance) resetRunState()",
		"if (state.sessionId !== b.dataset.session) resetRunState()",
		"if (state.view !== b.dataset.view) resetRunState()",
		"resetRunState(); sessionStorage.removeItem",
		"closeRunStream();\n  state.currentRun = null;",
		"err.status = resp.status",
		"if (resp.status === 401)",
		"throw err; }\n  state.token = data.access_token",
		"throw err; }\n    const reader = resp.body.getReader()",
		"Session expired. Open this page again from VE Platform.",
		"function handleAPIError(e)",
		"async function refreshInstances()",
		"state.instances = items(out)",
		"if (!handleAPIError(e)) toast(e.message)",
		"if (!handleAPIError(e)) renderError(e)",
		"Continue waiting",
		"tool-detail",
		"function splitURLTrailingPunctuation(url)",
		"count(body, ch) > count(body, open[ch])",
		"function renderExternalLink(href, label)",
		"function restoreInlineTokens(html, tokens)",
		"function renderMarkdown(text)",
		"function renderMarkdownParagraph(lines)",
		"function renderMarkdownTable(rows)",
		"function isMarkdownTableDivider(line)",
		"state.messages = []; state.copySnippets = []; updateJumpLatestButton(false);",
		"state.messages.map(messageIdentity).filter(Boolean).forEach((id) => hidden.add(id));",
		"state.messages = visibleMessages(msgs); renderMessages();",
		"renderRunPanel(null);",
		"id=\"clearPanel\"",
		"function clearPanelContent()",
		"function upsertSession(session)",
		"function addThinkingPlaceholder(runId = \"\")",
		"function removeThinkingPlaceholders()",
		"function removeThinkingPlaceholdersAndRender()",
		"function replaceLocalMessage(localId, message)",
		"function clearContentLabel()",
		"function contentClearedLabel()",
		"function sendingLabel()",
		"function thinkingLabel()",
		"return locale === \"en\" ? \"Thinking\" : \"思考中\"",
		"function bindComposerKeys()",
		"function autoResizePrompt()",
		"function updateSendButtonState()",
		"prompt.oninput = sync",
		"prompt.onkeydown = (e) =>",
		"e.key !== \"Enter\" || e.shiftKey",
		"$(\"composer\")?.requestSubmit()",
		"if (!promptEl || !sendBtn || sendBtn.disabled) return;",
		"const optimisticId = `local-user-",
		"state.messages.push({ id: optimisticId, role: \"user\", content",
		"addThinkingPlaceholder();",
		"renderMessages(true);",
		"local_pending: true",
		"local_thinking: true",
		"removeThinkingPlaceholders();",
		"replaceLocalMessage(optimisticId, out.message)",
		"upsertSession(out.session)",
		"if (out.run?.id) { addThinkingPlaceholder(out.run.id); watchRun(out.run); }",
		"if (e.name !== \"AbortError\") { removeThinkingPlaceholders(); renderMessages();",
		"m.local_pending || m.local_thinking ? \"pending\"",
		"if (run === null) state.currentRun = null; else state.currentRun = run || state.currentRun;",
		"state.messages = state.messages.filter((m) => m.id !== optimisticId && !m.local_thinking)",
		"if (!promptEl.value.trim()) promptEl.value = content",
		"function bindMessageCopyButtons(msgs)",
		"function fallbackCopyText(value)",
		"function copyTextToClipboard(value)",
		"function copyTextImproved(text, btn)",
		"function shouldStickMessagesToBottom(el)",
		"function bindJumpLatestButton()",
		"function messageCopyButtonHTML(m, idx)",
		"m.local_thinking || !String(m.content || m.text || \"\").trim()",
		"function renderMessages(forceStick = false)",
		"function formatMessageTime(m)",
		"function messageMetaHTML(m)",
		"function parseSSEFrame(part)",
		"function splitSSEBuffer(buffer)",
		"const crlf = rest.indexOf(\"\\r\\n\\r\\n\")",
		"const split = splitSSEBuffer(buf); buf = split.rest;",
		"function parseSSEJSON(frame)",
		"function handleRunFrame(part)",
		"if (frame.event === \"error\") throw new Error(payload?.error || \"stream error\")",
		"if (frame.event === \"done\" && !payload?.snapshot?.assistant_message) removeThinkingPlaceholdersAndRender();",
		"split.frames.forEach(handleRunFrame)",
		"if (buf.trim()) handleRunFrame(buf);",
		"id=\"jumpLatest\"",
		"navigator.clipboard?.writeText",
		"catch { /* Fall back for denied clipboard permissions or insecure contexts. */ }",
		"document.execCommand(\"copy\")",
		"finally { area.remove(); }",
		"data-copy-message",
		"data-copy-code",
		"function orderedMessages()",
		"messageRoleClass(role)",
		"a.ts - b.ts",
		"<div class=\"md-content ${m.local_thinking ? \"thinking\" : \"\"}\">${renderMarkdown",
		"state.messages.push(snap.assistant_message)",
		"resp.body.getReader",
		"maclaw.user.token",
		"maclaw_llm_providers",
		"configGroups",
		"cfgTabs",
		"role=\"tabpanel\"",
		"aria-controls=\"cfg_panel_",
		"function moveConfigTab",
		"ArrowRight",
		"function setSettingsActionsDisabled",
		"setSettingsActionsDisabled(true)",
		"const validation = await api(\"/api/v1/config/validate\"",
		"try { await refreshInstances(); } catch (refreshErr)",
		"if (refreshErr.status === 401) throw refreshErr",
		"p.setAttribute(\"aria-hidden\", off ? \"true\" : \"false\")",
		"aria-hidden=\"${group.id === state.settingsTab ? \"false\" : \"true\"}\"",
		"configIssueLabel({ key })",
		"secretHint",
		"autocomplete=\"new-password\"",
		"parseConfigNumber",
		"must be a valid",
		"Number.isFinite",
		"Number.isInteger",
		"const next = Number(raw)",
		"MCP & Tools",
		"Knowledge & Memory",
		"groupAdvanced",
		"const allKeys = [...new Set",
		"groups[groups.length - 1].keys = rest.filter",
		"security_policy_mode",
		"mcp_servers",
		"ve-platform-web",
		"viewer authentication failed",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("user web missing marker %s", needle)
		}
	}
	if strings.Contains(body, "Number.parseInt") {
		t.Fatalf("user app should reject partial integer strings instead of parseInt truncation")
	}
	if strings.Contains(body, "state.messages.slice().reverse()") || strings.Contains(body, "state.messages.unshift(snap.assistant_message)") {
		t.Fatalf("user app should render chat messages oldest-to-newest and append streamed assistant messages")
	}
	if strings.Contains(body, "[.,;:!?，。；：！？)]") {
		t.Fatalf("user app should balance closing delimiters before stripping them from bare URLs")
	}
	paramsIdx := strings.Index(body, "const params = new URLSearchParams")
	localeIdx := strings.Index(body, "const requestedLocale =")
	if paramsIdx < 0 || localeIdx < 0 || paramsIdx > localeIdx {
		t.Fatalf("user app must initialize URL params before locale reads params")
	}
	if strings.Contains(body, "state.token = params.get(\"token\")") || strings.Contains(body, "params.get(\"token\") || (") {
		t.Fatalf("user app must not accept raw bearer tokens from URL query")
	}
	if strings.Contains(body, "style=") || strings.Contains(body, "\uFFFD") {
		t.Fatalf("user app asset contains CSP-hostile inline style or replacement char")
	}

	req = httptest.NewRequest(http.MethodGet, "/app/styles.css", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	css := w.Body.String()
	for _, needle := range []string{"@media (prefers-color-scheme: dark)", "@media (prefers-reduced-motion", ".skip-link", "min-height: 100dvh", ".run-panel", ".chat-toolbar", ".clear-panel-btn", ".tool-detail", ".messages-wrap", ".jump-latest", ".message-head", ".message-meta", ".message-time", ".message.pending", ".md-content.thinking", "@keyframes thinking-dots", ".copy-btn", ".sr-copy-area", ".md-content", ".md-content .md-code", ".md-content .md-code-head", ".md-content blockquote", ".md-content hr", ".md-content .md-table-wrap", ".md-content .task-list-item", ".composer textarea { min-height: 50px; max-height: 180px; resize: none; overflow: auto; }", ".cfg-group", ".cfg-group[hidden] { display: none !important; }", ".cfg-tabs", ".cfg-output", "#issues .error", ".fields { display: block; }", "width: 100%; border: 1px solid var(--line)"} {
		if !strings.Contains(css, needle) {
			t.Fatalf("user css missing marker %s", needle)
		}
	}
}

func TestUserWebRedirectsSlashlessApp(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/app?launch_token=abc&view=assistant", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusMovedPermanently || w.Result().Header.Get("Location") != "/app/?launch_token=abc&view=assistant" {
		t.Fatalf("redirect = %d location = %q", w.Code, w.Result().Header.Get("Location"))
	}
}

func TestAdminCanUpdateUserWebConfig(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(t.Context(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User", Email: "user@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	path := "/api/v1/admin/tenants/" + tenant.ID + "/users/" + user.ID + "/config"
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"app_config":{"maclaw_llm_url":"https://llm.example/v1","maclaw_llm_key":"secret-key","maclaw_llm_model":"gpt-test","memory_max_backups":7}}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin update config = %d body = %s", w.Code, w.Body.String())
	}

	var out struct {
		AppConfig corelib.AppConfig `json:"app_config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if out.AppConfig.MaclawLLMUrl != "https://llm.example/v1" || out.AppConfig.MaclawLLMModel != "gpt-test" || out.AppConfig.MemoryMaxBackups != 7 {
		t.Fatalf("admin update did not persist expected config: %#v", out.AppConfig)
	}

	req = httptest.NewRequest(http.MethodPost, path+"/validate", strings.NewReader(`{"app_config":{"maclaw_llm_url":"https://llm.example/v1","maclaw_llm_key":"secret-key","maclaw_llm_model":"gpt-test"}}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"valid":true`) {
		t.Fatalf("admin validate config = %d body = %s", w.Code, w.Body.String())
	}
}
