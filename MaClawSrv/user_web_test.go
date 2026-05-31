package main

import (
	"encoding/json"
	"io/fs"
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
		"function isMarkdownTableRow(line)",
		"if (isMarkdownTableDivider(line)) return false",
		"function splitFlattenedMarkdownTable(line)",
		"const rows = [header]",
		"const cellsForRow = (row) => Array.isArray(row) ? row : splitMarkdownTableRow(row)",
		"const flattenedRows = splitFlattenedMarkdownTable(line)",
		"const hasDivider = rows.length > 1 && isMarkdownTableDivider(rows[1])",
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
		"HIDDEN_CONFIG_KEYS",
		"!HIDDEN_CONFIG_KEYS.has(key)",
		"CLEARED_USER_COMPLEX_CONFIG_KEYS",
		"function stripUserComplexConfig",
		"state.config = stripUserComplexConfig(cfgResp.app_config)",
		"const next = stripUserComplexConfig(state.config)",
		"\"maclaw_llm_protocol\", \"maclaw_llm_context_length\", \"maclaw_llm_timeout_sec\", \"maclaw_llm_current_provider\", \"maclaw_llm_providers\", \"llm_prompt_cache\", \"auxiliary_llm\", \"model_routes\"",
		"keys: [\"maclaw_llm_url\", \"maclaw_llm_key\", \"maclaw_llm_model\"]",
		"llm_token_usage",
		"remote_machine_token",
		"noise_floor_calibrated",
		"floating_btn_position_set",
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
		"CONFIG_CHOICE_FIELDS",
		"GENERIC_CHOICE_FIELDS",
		"function genericChoiceOptions(key)",
		"security_policy_mode: [\"none\", \"standard\", \"relaxed\", \"strict\", \"developer\"]",
		"sandbox_mode: [\"none\", \"os\", \"docker\"]",
		"network_level: [\"none\", \"intranet\", \"allowlist\", \"full\"]",
		"default_proxy_protocol: [\"http\", \"https\", \"socks5\"]",
		"skill_purchase_mode: [\"auto\", \"free_only\"]",
		"ui_mode: [\"lite\", \"pro\"]",
		"pet_interaction_mode: [\"quiet\", \"balanced\", \"active\"]",
		"function providerChoiceOptions(key)",
		"web_search_current_provider",
		"function stringChoiceInput(key, value)",
		"genericChoiceOptions(key).length",
		"CONFIG_NUMBER_CHOICE_FIELDS",
		"subagent_concurrency: [1, 2, 3, 4]",
		"ui_zoom_factor: [0, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 2]",
		"thirdparty_gateway_port: [0, 8080, 18080, 28080, 38080]",
		"local_needle_min_confidence: [0, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95]",
		"function numberChoiceInput(key, value, type)",
		"function numberChoiceCustomMarkup(id, attrs, current, suggestions)",
		"CONFIG_ARRAY_CHOICE_FIELDS",
		"function arrayChoiceInput(key)",
		"data-type=\"array-choice\" multiple",
		"[...el.selectedOptions].map",
		"CONFIG_LINE_ARRAY_FIELDS",
		"CONFIG_LINE_ARRAY_SUGGESTION_FIELDS",
		"remote_hubcenter_urls",
		"CONFIG_STRING_LINE_FIELDS",
		"CONFIG_STRING_LINE_SUGGESTION_FIELDS",
		"COMMON_SKILL_SUGGESTIONS",
		"COMMON_ROLE_SUGGESTIONS",
		"COMMON_COMMAND_ARG_SUGGESTIONS",
		"COMMON_TEXT_FALLBACK_SUGGESTIONS",
		"COMMON_LINE_FALLBACK_SUGGESTIONS",
		"COMMON_NUMBER_FALLBACK_SUGGESTIONS",
		"ROLE_NAME_SUGGESTIONS",
		"ROLE_DESCRIPTION_SUGGESTIONS",
		"default_proxy_bypass",
		"function choiceLinesMarkup(id, attrs, currentValues, suggestions",
		"function choiceCustomMarkup(id, attrs, current, suggestions)",
		"data-list-kind=\"choice-custom\"",
		"data-choice-suggest",
		"__custom__",
		"function bindChoiceCustomControls()",
		"string-choice-custom",
		"function longTextChoiceMarkup(id, attrs, current, suggestions)",
		"data-list-kind=\"longtext-choice\"",
		"data-longtext-suggest",
		"function stringLineInput(key)",
		"string-choice-lines",
		"string-choice-lines",
		"join(\";\")",
		"network_allowlist",
		"function lineArrayInput(key)",
		"array-choice-lines",
		"data-array-suggest",
		"data-choice-lines-action=\"all\"",
		"data-choice-lines-action=\"clear\"",
		"selectAll",
		"clearSelection",
		"data-array-custom",
		"COMMON_LINE_FALLBACK_SUGGESTIONS",
		"el.value.split(/\\r?\\n/)",
		"CONFIG_OBJECT_LIST_FIELDS",
		"function objectListInput(key)",
		"data-object-list=\"${esc(key)}\"",
		"data-list-field",
		"meta.kind === \"number\"",
		"meta.kind === \"bool\"",
		"meta.kind === \"lines\"",
		"box.querySelectorAll(\"[data-list-key]\")",
		"CONFIG_OBJECT_FIELDS",
		"mis_data: {",
		"group_discussion: {",
		"availability",
		"languages",
		"allowed_roles",
		"use_cross_agent_experience",
		"capability_market_policy: {",
		"enterprise_only_install",
		"managed_deployment.retry_interval_minutes",
		"update_policy.enterprise_hub.apply_to",
		"kind: \"multi\"",
		"update_policy.hubcenter.paid_capability",
		"resource_types.skill.allowed_sources",
		"data-object-form=\"${esc(key)}\"",
		"CONFIG_OBJECT_MAP_FIELDS",
		"data-object-map=\"${esc(key)}\"",
		"function objectMapInput(key)",
		"function objectFieldValue(item, field)",
		"f.kind === \"longtext\"",
		"f.kind === \"multi\"",
		"f.kind === \"provider\"",
		"data-list-kind=\"choice-lines\"",
		"f.kind === \"number\" && f.options?.length",
		"CONFIG_JSON_STRING_OBJECT_FIELDS",
		"ve_approval_config: {",
		"acl.mode",
		"acl.departments",
		"acl.roles",
		"acl.skills",
		"acl.entities",
		"data-json-string-object=\"${esc(key)}\"",
		"function jsonStringObjectInput(key)",
		"const setObjectPath =",
		"function objectElementValue(el)",
		"CONFIG_SUGGESTION_FIELDS",
		"GENERIC_TEXT_SUGGESTIONS",
		"function userProfileSuggestions(keys)",
		"remote_email",
		"remote_mobile",
		"function genericSuggestionOptions(key)",
		"function genericLineSuggestions(key)",
		"function genericArrayInput(key)",
		"COMMON_LINE_FALLBACK_SUGGESTIONS",
		"GENERIC_OBJECT_KEY_SUGGESTIONS",
		"function shallowObject(value)",
		"function scalarLeafObject(value)",
		"function flattenObjectLeaves(value, prefix = \"\")",
		"function setPlainObjectPath(target, field, value)",
		"function plainObjectPathValue(target, field)",
		"function coercePlainObjectValue(current, value)",
		"function genericObjectInput(key)",
		"data-type=\"object-kv\"",
		"data-deep-object=\"true\"",
		"const typedValue = coercePlainObjectValue(plainObjectPathValue(current, pairKey), pairValue)",
		"raw-json-editor",
		"data-generic-object-key",
		"data-generic-object-key-custom",
		"data-generic-object-value",
		"data-generic-object-value-custom",
		"function genericNumberOptions(key)",
		"genericSuggestionOptions(f.key)",
		"genericNumberOptions(f.key)",
		"genericSuggestionOptions(key).length",
		"genericNumberOptions(key).length",
		"numberChoiceCustomMarkup(`cfg_${key}`, `data-key=\"${esc(key)}\" data-type=\"${esc(def.type)}\"`, fieldValue(key, def), COMMON_NUMBER_FALLBACK_SUGGESTIONS)",
		"parseConfigNumber(key, objectElementValue(el), true)",
		"parseConfigNumber(key, objectElementValue(el), false)",
		"choiceCustomMarkup(`cfg_${key}`, `data-key=\"${esc(key)}\" data-type=\"string-choice-custom\"`, fieldValue(key, def), COMMON_TEXT_FALLBACK_SUGGESTIONS)",
		"const LLM_URL_SUGGESTIONS = [",
		"const LLM_MODEL_SUGGESTIONS = [\"auto\"",
		"https://api.openai.com/v1",
		"http://localhost:11434/v1",
		"default_proxy_port: [\"7890\", \"7897\", \"1080\", \"3128\", \"8080\"]",
		"remote_hubcenter_url: [\"https://hubcenter.example.com\"]",
		"lansenger_wss_url",
		"weixin_cdn_url",
		"skill_market_url: [\"https://hubcenter.example.com\"",
		"working_directory: [\"~/.maclaw/workspace\", \"~/workspace\", \"D:/workprj\"]",
		"local_needle_model_path: [\"~/.maclaw/models/needle\", \"models/needle\"]",
		"maclaw_llm_model: LLM_MODEL_SUGGESTIONS",
		"auth_type",
		"f.suggestions?.length",
		"keySuggestions",
		"valueSuggestions",
		"function secretFieldMarkup(id, attrs, current",
		"function isLikelySecretKey(key)",
		"function bindSecretGenerators()",
		"data-generate-secret",
		"generateSecret",
		"f.kind === \"kv\"",
		"data-list-kind=\"kv\"",
		"data-kv-key",
		"data-kv-key-custom",
		"data-kv-value",
		"data-kv-value-custom",
		"function suggestionInput(key, def, value)",
		"function audioDeviceInput(key, value)",
		"audio_input_device_id",
		"pet_voice_readback_enabled",
		"data-audio-device=\"${key === \"audio_output_device_id\" ? \"audiooutput\" : \"audioinput\"}\"",
		"function bindAudioDeviceInputs()",
		"navigator.mediaDevices?.enumerateDevices",
		"data-unset-empty=\"true\"",
		"mcp_servers",
		"ssh_hosts",
		"/api/v1/skills",
		"/api/v1/skills/search",
		"/api/v1/skills/install",
		"function renderSkillManager()",
		"id=\"skillSearchForm\" class=\"skill-search\" role=\"search\"",
		"function renderMCPManager()",
		"data-mcp-edit",
		"function renderMCPEditor(s)",
		"function updateMCPServer(id, editor)",
		"data-mcp-save",
		"data-mcp-param-add",
		"data-mcp-param-remove",
		"/api/v1/mcp/market",
		"/api/v1/mcp/market/install",
		"data-mcp-market-install",
		"function isMCPMarketInstalled(item)",
		"mcpMarketItemKeys(item).some",
		"id=\"mcpAddMode\"",
		"mcpEntriesFromJSON",
		"function renderWebSearchManager()",
		"data-web-search-manager",
		"web_search_current_provider",
		"mcp_servers\", \"local_mcp_servers\", \"ssh_hosts\", \"skill_hub_urls\", \"external_skill_dirs\", \"skill_sources_allowed",
		"skill_market_url",
		"function renderKnowledgeImporter()",
		"function renderMemoryManager()",
		"function renderMemorySummary(counts)",
		"MEMORY_MAX_CONTENT_CHARS",
		"function validateMemoryPayload(payload)",
		"const seen = new Set()",
		"memoryTagsTooMany",
		"memoryTagTooLong",
		"function clearMemoryFilters()",
		"function scheduleMemorySearch()",
		"function setMemoryLoading(on, append)",
		"memoryReloadPending",
		"function setMemorySaving(on)",
		"function bindMemoryManager()",
		"/api/v1/memory",
		"data-memory-manager",
		"memoryClearBtn",
		"memory-list",
		"KNOWLEDGE_TOPIC_SUGGESTIONS",
		"KNOWLEDGE_LABEL_SUGGESTIONS",
		"KNOWLEDGE_TITLE_SUGGESTIONS",
		"KNOWLEDGE_TEXT_TEMPLATES",
		"KNOWLEDGE_URL_SUGGESTIONS",
		"function datalistTextInput(id, suggestions",
		"function formChoiceValue(id)",
		"function requireKnowledgeChoiceValue(",
		"function clearKnowledgeChoiceError(el)",
		"aria-invalid",
		"function setFormChoiceValue(id, value)",
		"class=\"choice-custom knowledge-choice",
		"function knowledgeDepthInput()",
		"function knowledgeTemplateInput()",
		"function knowledgeURLExampleInput()",
		"id=\"knowledgeTextTemplate\"",
		"id=\"insertKnowledgeTemplateBtn\" type=\"button\"",
		"id=\"knowledgeURLExample\"",
		"id=\"addKnowledgeURLBtn\" type=\"button\"",
		"function insertKnowledgeTemplate()",
		"function addKnowledgeURLExample()",
		"setFormChoiceValue(\"knowledgeTextTitle\", tpl.title)",
		"setFormChoiceValue(\"knowledgeURLTopic\", item.topic)",
		"connectedKnowledge",
		"function loadKnowledgeAccessSummary()",
		"function formatKnowledgeImportStatus(value)",
		"function knowledgeProgressText(value)",
		"value.result && typeof value.result === \"object\" ? value.result : value",
		"stats.forEach(([key, label])",
		"setKnowledgeImportStatus({ id: jobID, status: \"queued\"",
		"}, true)",
		"importProgress",
		"function runKnowledgeImport(buttonID, task)",
		"lines.join(\"\\n\")",
		"for (let i = 0; i < 60; i++)",
		"function toastKnowledgeImportResult(job)",
		"toast(t(\"importStarted\"))",
		"importCompleted",
		"importStillRunning",
		"importSource",
		"importTitle",
		"importKind",
		"importProcessed",
		"importImported",
		"importFailed",
		"importWarnings",
		"/api/v1/knowledge/access",
		"knowledge-scope-chip",
		"function userKnowledgeScopeDisplay(scope, selfScope)",
		"knowledgeScopeIDs",
		"displayWithID(tenantLabel, tenantID)",
		"state.me?.tenant_name",
		"public:",
		"/api/v1/knowledge/import/text",
		"/api/v1/knowledge/import/file",
		"/api/v1/knowledge/import/urls",
		"/api/v1/knowledge/import/jobs/",
		"<select id=\"knowledgeURLDepth\">",
		"id=\"knowledgeTextImportBtn\" type=\"button\"",
		"id=\"knowledgeFileImportBtn\" type=\"button\"",
		"files.forEach((file) => form.append(\"file\", file))",
		"id=\"knowledgeURLImportBtn\" type=\"button\"",
		"accept=\".doc,.docx,.pdf,.pptx,.xlsx,.xls,.csv,.md,.markdown,.txt,.text,.zip,.rar\"",
		"role=\"search\"",
		"id=\"skillSearchBtn\" type=\"button\"",
		"data-install-skill",
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
	if strings.Contains(body, `keys: ["maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_model", "maclaw_llm_current_provider", "maclaw_llm_providers", "auxiliary_llm", "model_routes"]`) {
		t.Fatalf("LLM settings tab should not render advanced provider route editors")
	}
	if strings.Contains(body, `keys: ["maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_model", "maclaw_llm_current_provider"]`) {
		t.Fatalf("LLM settings tab should not render provider selection without provider editor")
	}
	if strings.Contains(body, `keys: ["maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_model", "maclaw_llm_protocol", "maclaw_llm_context_length", "maclaw_llm_timeout_sec"]`) {
		t.Fatalf("LLM settings tab should not render advanced protocol/context/timeout fields")
	}
	for _, stale := range []string{
		`maclaw_llm_protocol: ["openai", "anthropic"]`,
		`maclaw_llm_context_length: [32000, 64000, 110000, 200000]`,
		`maclaw_llm_timeout_sec: [60, 120, 300, 480, 900]`,
		"maclaw_llm_providers: {",
		"llm_prompt_cache: {",
		"auxiliary_llm: {",
		"model_routes: {",
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("user app should not carry stale complex LLM editor code %s", stale)
		}
	}
	if strings.Contains(body, "HIDDEN_CONFIG_KEYS.forEach((key) => delete next[key])") {
		t.Fatalf("user config save should only clear known complex user-facing fields, not every hidden managed key")
	}
	if strings.Contains(body, "prompt(t(\"instanceName\")") {
		t.Fatalf("new instance flow should not require browser text prompt")
	}
	if strings.Contains(body, "state.messages.slice().reverse()") || strings.Contains(body, "state.messages.unshift(snap.assistant_message)") {
		t.Fatalf("user app should render chat messages oldest-to-newest and append streamed assistant messages")
	}
	if strings.Contains(body, "<form id=\"skillSearchForm\"") || strings.Contains(body, "form.onsubmit = searchSkills") {
		t.Fatalf("skill search must not use a nested form that can submit the settings form")
	}
	for _, stale := range []string{
		"mcp_servers: {",
		"local_mcp_servers: {",
		"ssh_hosts: {",
		"skill_hub_urls: {",
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("user settings should not keep raw skill/MCP editor marker %s", stale)
		}
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
	for _, needle := range []string{"@media (prefers-color-scheme: dark)", "@media (prefers-reduced-motion", ".skip-link", "min-height: 100dvh", ".run-panel", ".chat-toolbar", ".clear-panel-btn", ".tool-detail", ".messages-wrap", ".jump-latest", ".message-head", ".message-meta", ".message-time", ".message.pending", ".md-content.thinking", "@keyframes thinking-dots", ".copy-btn", ".sr-copy-area", ".md-content", ".md-content .md-code", ".md-content .md-code-head", ".md-content blockquote", ".md-content hr", ".md-content .md-table-wrap", "min-width: max-content", ".md-content .task-list-item", ".composer textarea { min-height: 50px; max-height: 180px; resize: none; overflow: auto; }", ".cfg-group", ".cfg-group[hidden] { display: none !important; }", ".cfg-tabs", ".cfg-output", ".object-list", ".object-row", ".kv-list", ".kv-pair", ".kv-pair.custom-key-active", ".kv-pair.custom-value-active", ".choice-lines", ".choice-select-stack", ".choice-actions", ".choice-custom", ".choice-custom:not(.custom-active) [data-choice-custom]", ".custom-lines", ".raw-json-editor", ".secret-input", ".mcp-inline-editor", ".mcp-param-row", ".mcp-param-row button", ".memory-manager", ".memory-toolbar", ".memory-summary", ".memory-chip", ".memory-entry", ".memory-tags", ".memory-load-more", ".channel-overview", ".channel-card.managed", ".channel-protocol", ".knowledge-access-summary", ".knowledge-scope-chip", ".knowledge-scope-chip small", ".knowledge-importer", ".knowledge-import-grid", ".knowledge-progress", ".knowledge-field-error", ".knowledge-field-help", "#issues .error", ".fields { display: block; }", "width: 100%; border: 1px solid var(--line)"} {
		if !strings.Contains(css, needle) {
			t.Fatalf("user css missing marker %s", needle)
		}
	}
}

func TestUserWebIncludesChannelProtocolSettings(t *testing.T) {
	bodyBytes, err := fs.ReadFile(userWebFS, "user_web/app.js")
	if err != nil {
		t.Fatalf("read user app: %v", err)
	}
	body := string(bodyBytes)
	for _, needle := range []string{
		"Maclaw 第三方接入协议",
		"企业版微信、飞书、钉钉由 Hub 租户设置统一接入",
		"Hub 租户统一接入的企业 IM",
		"function hubManagedChannelCard()",
		"channelEnterpriseWeCom",
		"channelFeishu",
		"channelDingTalk",
		"个人微信 / iLink",
		"协议接入地址",
		"function thirdPartyProtocolEndpoint()",
		"copyThirdPartyEndpoint",
		"generateThirdPartyToken",
		"/api/im-gateway/v1",
		"window.crypto.getRandomValues(bytes)",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("user web missing channel marker %s", needle)
		}
	}
	for _, stale := range []string{
		"Third-party HTTP Gateway",
		"第三方 HTTP 网关",
		"启用第三方网关",
		"第三方网关本地模式",
		"微信网关",
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("user web still contains stale gateway wording %s", stale)
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
