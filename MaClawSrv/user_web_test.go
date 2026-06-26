package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	if csp := w.Result().Header.Get("Content-Security-Policy"); !strings.Contains(csp, "img-src 'self' data: blob:") {
		t.Fatalf("user web CSP must allow authorized QR blob images, got %q", csp)
	}
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
		"/api/v1/web/refresh",
		"const hasLaunchToken = params.has(\"launch_token\");",
		"const TOKEN_REFRESH_GRACE_MS = 2 * 60 * 1000;",
		"const savedExpiry = sessionStorage.getItem(\"maclaw.user.token_expires_at\") || \"\";",
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
		`if (state.view !== b.dataset.view) { resetRunState(); if (state.view === "settings") resetWeixinQRLogin(); }`,
		"$(\"logoutBtn\").onclick = () => { clearAccessToken(); renderMissingToken(); };",
		"closeRunStream();\n  state.currentRun = null;",
		"err.status = resp.status",
		"if (resp.status === 401)",
		"function applyAccessToken(token, expiresAt)",
		"if (state.token) state.tokenExpiresAt = parseTokenExpiry(savedExpiry);",
		"sessionStorage.setItem(\"maclaw.user.token_expires_at\", new Date(state.tokenExpiresAt).toISOString());",
		"sessionStorage.removeItem(\"maclaw.user.token_expires_at\");",
		"function clearAccessToken()",
		"function refreshAccessToken(reason = \"activity\", force = false)",
		"if (!data.access_token) throw new Error(\"missing refreshed access token\")",
		"scheduleTokenRefresh(Math.min(Math.max(tokenExpiresInMs() - 1000, 1000), TOKEN_REFRESH_RECHECK_MS));",
		"await refreshAccessToken(\"bootstrap\", !state.tokenExpiresAt);",
		"applyAccessToken(data.access_token, data.expires_at);",
		"[\"pointerdown\", \"keydown\", \"input\", \"focus\"].forEach((eventName) => document.addEventListener(eventName, markUserActivity, true));",
		"throw err; }",
		"const reader = resp.body.getReader()",
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
		"function bindVoiceComposer()",
		"function transcribeVoiceFile(file, btn)",
		"/api/v1/ai-models/asr/transcribe",
		"X-MaClaw-Audio-Format",
		"voiceUploadContentType(file)",
		"function voiceUploadFormat(file)",
		"function voiceUploadContentType(file)",
		"audio/mpeg",
		"audio/aac",
		"audio/mp4",
		"if (name.endsWith(\".aac\")) return \"aac\";",
		"if (name.endsWith(\".m4a\")) return \"m4a\";",
		"id=\"voiceFile\" type=\"file\" accept=\"audio/wav,audio/ogg,audio/opus,audio/mpeg,audio/aac,audio/mp4,.wav,.ogg,.opus,.oga,.silk,.mp3,.aac,.m4a\" hidden",
		"id=\"voiceBtn\" type=\"button\"",
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
		"function bindMessageSpeakButtons(msgs)",
		"function speakMessage(message, btn)",
		"safeTTSEndpoint(message?.metadata?.tts_endpoint)",
		"function safeTTSEndpoint(value)",
		"endpoint.startsWith(\"/api/\") && !endpoint.startsWith(\"//\")",
		"/api/v1/ai-models/tts/synthesize",
		"format: \"mp3\"",
		"data-speak-message",
		"tts_available",
		"tts_endpoint",
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
		`"thirdparty_gateway_host", "thirdparty_gateway_port"`,
		"!HIDDEN_CONFIG_KEYS.has(key)",
		"function userConfigDraft",
		"state.config = userConfigDraft(cfgResp.app_config)",
		"const next = stripAdminManagedConfig(userConfigDraft(state.config))",
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
		"function refreshSettingsAfterSave()",
		"const savedConfig = state.config",
		"api(\"/api/v1/config/validate\"",
		"refreshSettingsAfterSave();",
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
		"groupIM",
		`id: "im"`,
		`group.id === "im" ? renderIMConfigEditor(defs)`,
		`function renderIMSubTabs`,
		`function renderQQIMPanel`,
		`function configFieldMarkup`,
		`const fields = group.id === "im" ? ""`,
		`normalizeSettingsTab(tab)`,
		`["llm", "tools", "skills", "memory", "migration", "security", "im", "interface", "proxy"].includes(tab) ? tab : ""`,
		"groupMigration",
		`id: "migration"`,
		`group.id === "migration" ? renderMigrationManager()`,
		"function renderMigrationManager()",
		"function loadMigrationState()",
		"function startMigrationExport()",
		"function startMigrationImport()",
		"function watchMigrationJob(jobID)",
		"/api/v1/migration/status",
		"/api/v1/migration/instances",
		"/api/v1/migration/export",
		"/api/v1/migration/import",
		"migrationOverwriteWarning",
		"migrationExportPasswordConfirm",
		"groupInterface",
		"groupProxy",
		`id: "interface"`,
		`id: "proxy"`,
		`const interfaceKeys = pick((key) => key === "language")`,
		"const allKeys = [...new Set",
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
		"function providerChoiceOptions(key)",
		"web_search_current_provider",
		"function stringChoiceInput(key, value)",
		"genericChoiceOptions(key).length",
		"CONFIG_NUMBER_CHOICE_FIELDS",
		"subagent_concurrency: [1, 2, 3, 4]",
		"ui_zoom_factor: [0, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 2]",
		"function numberChoiceInput(key, value, type)",
		"function numberChoiceCustomMarkup(id, attrs, current, suggestions)",
		"CONFIG_ARRAY_CHOICE_FIELDS",
		"function arrayChoiceInput(key)",
		"data-type=\"array-choice\" multiple",
		"[...el.selectedOptions].map",
		"CONFIG_LINE_ARRAY_FIELDS",
		"CONFIG_LINE_ARRAY_SUGGESTION_FIELDS",
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
		"lansenger_wss_url",
		"weixin_cdn_url",
		"skill_market_url: [\"https://hubcenter.example.com\"",
		"maclaw_llm_model: LLM_MODEL_SUGGESTIONS",
		"auth_type",
		"f.suggestions?.length",
		"keySuggestions",
		"valueSuggestions",
		"function secretFieldMarkup(id, attrs, current",
		`String(id || "").replace(/^cfg_/, "")`,
		"function secretGenerateLabel(id)",
		"Generate secret",
		"\\u751f\\u6210\\u5bc6\\u94a5",
		"const NON_GENERATABLE_SECRET_KEYS",
		`new Set(["maclaw_llm_key", "qqbot_app_secret", "lansenger_app_secret"])`,
		"function canGenerateSecretForKey(key)",
		`maclaw_llm_key: ["LLM Access Token", "Provider access token entered manually. This field does not generate a key."]`,
		`maclaw_llm_key: ["LLM 访问令牌", "这里填写服务商访问令牌，不需要生成密钥。"]`,
		"canGenerateSecretForKey(key)",
		"function fieldSecretHint(d)",
		"Masked value keeps the existing access token. Enter a new value only when replacing it.",
		"显示为掩码时会保留现有访问令牌；只有需要替换时才输入新值。",
		`<div class="settings-head">`,
		`<div class="settings-actions">`,
		"function isLikelySecretKey(key)",
		"function bindSecretGenerators()",
		"data-generate-secret",
		"data-toggle-secret",
		"showSecret",
		"hideSecret",
		"generateSecret",
		"const IM_ENABLED_KEYS = new Set",
		"function boolRadioInput(key, value)",
		`const options = [["true", t("trueValue")], ["false", t("falseValue")]]`,
		"function renderIMToggleField(enabledKey, defs)",
		"function renderIMProgressHintSettings(defs)",
		"im_progress_nudge_enabled",
		"imProgressHints",
		`return { label: t("channelDisabled"), cls: "" }`,
		"data-bool-group=\"true\"",
		"role=\"radiogroup\"",
		"im-channel-toolbar",
		"im-enable-row",
		"im-field-grid",
		"channel-protocol-actions",
		"f.kind === \"kv\"",
		"data-list-kind=\"kv\"",
		"data-kv-key",
		"data-kv-key-custom",
		"data-kv-value",
		"data-kv-value-custom",
		"function suggestionInput(key, def, value)",
		"function audioDeviceInput(key, value)",
		"audio_input_device_id",
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
		"const SKILL_PAGE_SIZE = 20",
		"function renderSkillCard",
		"class=\"skill-grid\"",
		"data-skill-page=\"next\"",
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
		"data-web-search-readonly",
		"webSearchManagedByAdmin",
		"web_search_current_provider",
		"mcp_servers\", \"local_mcp_servers\", \"ssh_hosts\", \"skill_hub_urls\", \"external_skill_dirs\", \"skill_sources_allowed",
		"skill_market_url",
		"data-view=\"knowledge\"",
		"function renderKnowledge()",
		"function renderKnowledgeQuery()",
		"knowledge-search-main",
		"knowledge-search-limit",
		"id=\"knowledgeSearchForm\"",
		"/api/v1/knowledge/search",
		"knowledgeQueryHint",
		"const rawLimit = Number",
		"[5, 8, 12, 20].includes(rawLimit) ? rawLimit : 8",
		"const hasScore = r.score !== undefined && r.score !== null",
		"const score = Number(r.score)",
		"hasScore && Number.isFinite(score)",
		"function renderKnowledgeImporter()",
		"function knowledgeField(forID, label, control",
		"knowledge-import-fields",
		"knowledge-span-2",
		"knowledge-check",
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
		"function finishKnowledgeImport(out)",
		`if (!jobID) { toastKnowledgeImportResult(out); await loadKnowledgeImportBatches(); return out; }`,
		"toastKnowledgeImportResult(finalJob || out)",
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
		"function userKnowledgeScopeKind(scope, access)",
		`String(scope.tenant_id || "") === String(access?.tenant_id || "")`,
		"function userKnowledgeTenantLabel(scope, tenantID)",
		"tenantID && tenantID === selfTenantID",
		"function userKnowledgeScopeDisplay(scope, kind)",
		"knowledge-scope-badge",
		"otherUserKnowledge",
		"knowledgeOwner",
		"knowledgeTenant",
		"knowledgeScopeIDs",
		"clearOwnKnowledge",
		"clearOwnKnowledgeConfirm",
		"clearOwnKnowledgePasswordPrompt",
		"data-clear-own-knowledge",
		"function requestDangerPassword",
		`role="dialog" aria-modal="true"`,
		`data-danger-password type="password"`,
		"const dialogID = `dangerModal",
		"document.activeElement === input",
		"event.currentTarget",
		"async function clearOwnKnowledgeBase(btn)",
		`api("/api/v1/knowledge?confirm=true", { method: "DELETE"`,
		"admin_password: adminCredential",
		"knowledge-scope-ids",
		"displayWithID(tenantLabel, tenantID)",
		"state.me?.tenant_name",
		"public:",
		"/api/v1/knowledge/import/text",
		"/api/v1/knowledge/import/file",
		"/api/v1/knowledge/import/urls",
		"/api/v1/knowledge/import/batches",
		"data-delete-knowledge-batch",
		"function requestDangerConfirm",
		"function deleteKnowledgeBatch(btn)",
		"/api/v1/knowledge/import/jobs/",
		"<select id=\"knowledgeURLDepth\">",
		"const rawDepth = Number",
		"[0, 1, 2, 3, 4, 5].includes(rawDepth) ? rawDepth : 0",
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
	if strings.Contains(body, "renderMemoryManager() + renderKnowledgeImporter()") {
		t.Fatalf("knowledge importer should stay in the knowledge tab, not the memory settings panel")
	}
	if strings.Contains(body, `id: "channels_more"`) || strings.Contains(body, `id: "advanced"`) || strings.Contains(body, `groups[groups.length - 1].keys = rest.filter`) {
		t.Fatalf("user settings should hide channels_more and advanced schema tabs")
	}
	if strings.Contains(body, "groupChannels") || strings.Contains(body, "groupAdvanced") {
		t.Fatalf("user settings should not keep legacy Channels or Advanced tab labels")
	}
	if strings.Contains(body, `"asr_enabled", "tts_enabled"`) || strings.Contains(body, `"qqbot_enabled", "qqbot_app_id", "qqbot_app_secret", "qqbot_local_mode"`) {
		t.Fatalf("IM settings should default to simple binding fields only")
	}
	if strings.Contains(body, `channelCard(t("channelVoice")`) || strings.Contains(body, `function channelCard`) {
		t.Fatalf("IM overview should only show configured protocol panels")
	}
	if strings.Contains(body, `function localModeLabel`) {
		t.Fatalf("IM settings should not expose local/hub routing mode in user binding UI")
	}
	if strings.Contains(body, `hubManagedChannelCard(),`) {
		t.Fatalf("per-user IM tab should not show Hub tenant enterprise IM as a user binding")
	}
	for _, needle := range []string{
		`imSubTab: "qq"`,
		`function renderIMSubTabs()`,
		`data-im-subtab`,
		`function renderQQIMPanel(defs)`,
		`function renderTelegramIMPanel(defs)`,
		`function renderWeixinIMPanel(defs)`,
		`function renderLansengerIMPanel(defs)`,
		`function renderThirdPartyIMPanel(defs)`,
		`renderIMChannelShell(t("channelQQ"), t("imQQDescription"), "qqbot_enabled", "qq", defs`,
		`renderIMChannelShell(t("channelTelegram"), t("imTelegramDescription"), "telegram_bot_enabled", "telegram", defs`,
		`renderIMChannelShell(t("channelWeixin"), t("imWeixinDescription"), "weixin_enabled", "weixin", defs`,
		`renderIMChannelShell(t("channelLansenger"), t("imLansengerDescription"), "lansenger_enabled", "lansenger", defs`,
		`renderIMChannelShell(t("channelThirdParty"), t("imThirdPartyDescription"), "thirdparty_gateway_enabled", "thirdparty", defs`,
		`const IM_DOC_LINKS`,
		`function renderIMLinkAction(href, label)`,
		`renderIMLinkAction(IM_DOC_LINKS.qq, t("imGetAppID"))`,
		`renderIMLinkAction(IM_DOC_LINKS.telegram, t("imTutorial"))`,
		`renderIMLinkAction(thirdPartyDocsURL(), t("imOpenDocs"))`,
		`const fields = group.id === "im" ? ""`,
		`renderIMProgressHintSettings(defs)`,
		`weixinBoundAccount`,
		`weixinRuntimeStatus`,
		`imRuntimes: {}`,
		`const IM_RUNTIME_PLATFORMS`,
		`thirdparty_gateway_enabled: "thirdparty"`,
		`function imRuntimeBadge(platform)`,
		`const detail = String(info.last_error || "").trim()`,
		"detail ? `${label}: ${detail}` : label",
		`function weixinRuntimeBadge()`,
		`function renderWeixinActions(boundAccount)`,
		`/api/v1/im/weixin/status`,
		`/api/v1/im/status`,
		`/api/v1/im/weixin/restart`,
		`function loadWeixinRuntimeStatus()`,
		`function loadIMRuntimeStatuses()`,
		`actionOverride === null ? renderIMCardActions(enabledKey) : actionOverride`,
		`state.config?.[enabledKey] === true && imRequiredFieldsReady(enabledKey)`,
		`id="startWeixinQRLogin"`,
		`/api/v1/im/weixin/qr/start`,
		`function authorizedObjectURL(path)`,
		`state.weixinQRCodeURL = imageURL ? await authorizedObjectURL(imageURL) : String(out.qrcode_url || "")`,
		`/api/v1/im/weixin/qr/poll`,
		`state.config = userConfigDraft(cfgResp.app_config)`,
		`function normalizeWeixinQRStatus(status)`,
		`["wait", "waiting", "pending", "polling", "timeout"].includes(s)`,
		`function normalizeWeixinQRMessage(status, message, fallback)`,
		`(normalizedStatus === "wait" || !normalizedStatus) && /^timeout$/i.test(text)`,
		`out.retryable && (state.weixinQRStatus === "wait" || state.weixinQRStatus === "error" || !state.weixinQRStatus)`,
		`function revokeWeixinQRCodeURL()`,
		`URL.revokeObjectURL(state.weixinQRCodeURL)`,
		`function resetWeixinQRLogin()`,
		`id="cancelWeixinQRLogin"`,
		`function missingIMRequiredFields(enabledKey)`,
		`t("imMissingRequired")`,
		`enabledKey === "weixin_enabled" && !imRequiredFieldsReady(enabledKey)`,
		`enabledKey === "thirdparty_gateway_enabled"`,
		`if (state.view === "settings") resetWeixinQRLogin();`,
		`window.addEventListener("beforeunload", clearWeixinQRPoll)`,
		`const PLAIN_TEXT_CONFIG_FIELDS = new Set(["qqbot_app_id", "lansenger_app_id"])`,
		`data-save-start-im`,
		`data-disconnect-im`,
		`data-im-watch-history`,
		`state.imAuditPlatform = String(button.dataset.imWatchHistory || "").trim()`,
		`function saveAndStartIM(enabledKey)`,
		`const saved = await saveConfig({ toastMessage: t("channelSavedStarted") })`,
		`if (!saved) return`,
		`function disconnectIM(enabledKey)`,
		`channelSaveStart`,
		`channelDisconnect`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("IM binding editor missing marker %s", needle)
		}
	}
	if strings.Contains(body, `renderIMBindingCard(t("channelWeixin"), "weixin_enabled", ["weixin_token", "weixin_account_id"]`) {
		t.Fatalf("WeChat IM card should bind via QR and not expose token/account as ordinary text fields")
	}
	if strings.Contains(body, `const options = [["", t("unset")], ["true", t("trueValue")], ["false", t("falseValue")]]`) {
		t.Fatalf("IM enable controls should be two-state yes/no, not three-state unset/yes/no")
	}
	if strings.Contains(body, `if (configured) return { label: t("channelReady"), cls: "" }`) {
		t.Fatalf("disabled IM channels should show disabled, not configured")
	}
	if strings.Contains(body, `/^(language$|ui_|show_|screen_)/.test(key)`) {
		t.Fatalf("user interface settings should only expose language, not screen/show/ui fields")
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
	if !strings.Contains(body, `"skill_runner_timeout_sec"`) {
		t.Fatalf("user app hidden config keys should include skill_runner_timeout_sec")
	}
	for _, stale := range []string{
		`maclaw_llm_protocol: ["openai", "anthropic"]`,
		`maclaw_llm_context_length: [32000, 64000, 110000, 200000]`,
		`maclaw_llm_timeout_sec: [60, 120, 300, 480, 900]`,
		`skill_runner_timeout_sec: [`,
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
	if strings.Contains(body, "CLEARED_USER_COMPLEX_CONFIG_KEYS") || strings.Contains(body, "stripUserComplexConfig(") {
		t.Fatalf("user config save must preserve hidden complex LLM fields and only apply visible control edits")
	}
	if strings.Contains(body, "prompt(t(\"instanceName\")") {
		t.Fatalf("new instance flow should not require browser text prompt")
	}
	if strings.Contains(body, "prompt(t(\"clearOwnKnowledgePasswordPrompt\")") {
		t.Fatalf("knowledge clear should use an in-page password dialog, not a browser prompt")
	}
	if strings.Contains(body, "$(\"dangerModalPassword\")") {
		t.Fatalf("knowledge clear dialog should read the local password input, not a global fixed id")
	}
	if strings.Contains(body, "admin_secret: adminCredential") || strings.Contains(body, "admin_secret: adminPassword") {
		t.Fatalf("knowledge clear should submit the single credential field without duplicating it as admin_secret")
	}
	if strings.Contains(body, "const adminPassword = await requestDangerPassword") {
		t.Fatalf("knowledge clear credential variable should not be named as password-only")
	}
	for _, hiddenTab := range []string{
		`id: "pet"`,
		`id: "startup"`,
		`id: "local_runtime"`,
		`groupPet`,
		`groupStartup`,
		`groupLocalRuntime`,
		`return "local_runtime"`,
		`pet_interaction_mode: ["quiet", "balanced", "active"]`,
		`pet_enabled: ["Pet assistant"`,
		`hide_startup_popup: ["Hide startup popup"`,
		`default_launch_mode: ["local", "remote"]`,
		`working_directory: ["Working directory"`,
		`working_directory: ["~/.maclaw/workspace"`,
		`remote_hubcenter_url: ["https://hubcenter.example.com"]`,
		`local_needle_min_confidence: [0, 0.5`,
		`local_needle_model_path: ["~/.maclaw/models/needle"`,
	} {
		if strings.Contains(body, hiddenTab) {
			t.Fatalf("user settings should not expose pet/startup/local runtime tabs, found %s", hiddenTab)
		}
	}
	if strings.Contains(body, "state.messages.slice().reverse()") || strings.Contains(body, "state.messages.unshift(snap.assistant_message)") {
		t.Fatalf("user app should render chat messages oldest-to-newest and append streamed assistant messages")
	}
	for _, needle := range []string{
		"function orderedIMAuditItems()",
		"const displayItems = orderedIMAuditItems();",
		"displayItems.map(renderIMAuditRow)",
		"a.ts !== b.ts ? a.ts - b.ts : a.idx - b.idx",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("IM audit history should render messages oldest-to-newest, missing %s", needle)
		}
	}
	if strings.Contains(body, "state.imAuditItems.map(renderIMAuditRow)") {
		t.Fatalf("IM audit history must not render API newest-first order directly")
	}
	if strings.Contains(body, "<form id=\"skillSearchForm\"") || strings.Contains(body, "form.onsubmit = searchSkills") {
		t.Fatalf("skill search must not use a nested form that can submit the settings form")
	}
	for _, staleSearchControl := range []string{
		"data-web-search-field",
		"data-web-search-delete",
		"webSearchAddBtn",
		"webSearchAddType",
		"webSearchProviderRow",
	} {
		if strings.Contains(body, staleSearchControl) {
			t.Fatalf("user search settings should be read-only, found %s", staleSearchControl)
		}
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
	if strings.Contains(body, "[.,;:!?\\uFF0C\\u3002\\uFF1B\\uFF1A\\uFF01\\uFF1F]") {
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
	for _, needle := range []string{"@media (prefers-color-scheme: dark)", "@media (prefers-reduced-motion", ".skip-link", "min-height: 100dvh", ".run-panel", ".chat-toolbar", ".clear-panel-btn", ".tool-detail", ".messages-wrap", ".jump-latest", ".message-head", ".message-meta", ".message-time", ".message.pending", ".message-actions", ".md-content.thinking", "@keyframes thinking-dots", ".copy-btn", ".sr-copy-area", ".md-content", ".md-content .md-code", ".md-content .md-code-head", ".md-content blockquote", ".md-content hr", ".md-content .md-table-wrap", "min-width: max-content", ".md-content .task-list-item", ".composer textarea { min-height: 50px; max-height: 180px; resize: none; overflow: auto; }", ".composer-actions", ".composer-actions button", ".cfg-group", ".cfg-group[hidden] { display: none !important; }", ".cfg-tabs", ".cfg-output", ".settings-head", ".settings-actions", ".object-list", ".object-row", ".kv-list", ".kv-pair", ".kv-pair.custom-key-active", ".kv-pair.custom-value-active", ".choice-lines", ".choice-select-stack", ".choice-actions", ".choice-custom", ".choice-custom:not(.custom-active) [data-choice-custom]", ".custom-lines", ".raw-json-editor", ".secret-input", ".bool-radio", ".bool-radio-two", ".bool-radio input:checked + span", ".skill-grid", ".skill-card", ".skill-pager", ".mcp-inline-editor", ".mcp-param-row", ".mcp-param-row button", ".memory-manager", ".memory-toolbar", ".memory-summary", ".memory-chip", ".memory-entry", ".memory-tags", ".memory-load-more", ".migration-manager", ".migration-kv", ".migration-warning", ".migration-grid", ".migration-progress", ".channel-overview", ".im-progress-settings", ".im-subtabs", ".im-subtab.active", ".im-channel-panel", ".im-channel-toolbar", ".im-channel-actions", ".im-link-action", ".im-enable-row", ".im-field-grid", ".im-field-grid-two", ".weixin-account-status", ".weixin-qr-actions", ".channel-protocol", ".channel-protocol-actions", ".im-audit-shell", ".danger-modal-backdrop", ".danger-modal", ".danger-modal-actions", ".knowledge-access-summary", ".knowledge-section-head", ".knowledge-access-layout", ".knowledge-scope-list { display: grid; grid-template-columns: 1fr;", ".knowledge-scope-chip", ".knowledge-scope-head { display: grid; grid-template-columns: minmax(0, 1fr) auto;", ".knowledge-scope-actions", ".knowledge-clear-btn { min-height: 36px;", ".knowledge-scope-badge { flex: 0 0 auto; display: inline-flex;", ".knowledge-scope-meta", ".knowledge-scope-ids", ".knowledge-scope-ids code", ".knowledge-scope-chip small", ".knowledge-batch-panel", ".knowledge-batch-row", "grid-template-rows: auto auto;", ".knowledge-batch-meta", "white-space: nowrap; overflow: hidden;", ".knowledge-batch-pager", ".knowledge-importer", ".knowledge-import-grid { display: grid; grid-template-columns: repeat(2", "align-items: stretch;", ".knowledge-import-grid section { display: grid; grid-template-rows: auto 1fr;", ".knowledge-import-grid section:nth-child(3) { grid-column: 1 / -1; }", ".knowledge-import-fields { display: grid; grid-template-columns: repeat(2", ".knowledge-import-fields > button { align-self: end;", ".knowledge-import-grid h3", ".knowledge-span-2", ".knowledge-search-form input,", ".knowledge-search-form { display: grid; grid-template-columns: minmax(260px, 1fr) minmax(112px, 150px) 96px;", ".knowledge-search-form button { align-self: end; width: 96px;", ".knowledge-progress", ".knowledge-field-error", ".knowledge-field-help", "#issues .error", ".fields { display: block; }", "width: 100%; border: 1px solid var(--line)"} {
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
		`channelThirdParty: "MaClaw Third-party Integration Protocol"`,
		`channelOverviewHint: "Configure this user's QQ, WeChat, Telegram, Lansenger, and third-party IM access."`,
		`channelLansenger: "Lansenger"`,
		`imChannelTabLansenger: "Lansenger"`,
		`imLansengerDescription: "Configure this user's Lansenger app credentials and start the Lansenger channel."`,
		"function renderIMConfigEditor(defs)",
		"function renderActiveIMPanel(defs)",
		"function renderLansengerIMPanel(defs)",
		"function renderThirdPartyIMPanel(defs)",
		"function loadIMRuntimeStatuses()",
		`channelWeixin: "Personal WeChat / iLink"`,
		`channelProtocolEndpoint: "Protocol endpoint"`,
		`lansenger_enabled: ["Enable Lansenger", "Enable this user's Lansenger binding."]`,
		`lansenger_app_id: ["Lansenger App ID", "Lansenger application ID."]`,
		`lansenger_gateway_url: ["Lansenger API Gateway", "Lansenger API gateway base URL."]`,
		`lansenger_enabled: ["lansenger_app_id", "lansenger_app_secret", "lansenger_gateway_url"]`,
		"imAuditLoadOlder",
		`["lansenger", "Lansenger"]`,
		"data-im-audit-days",
		"resetIMAuditPagination",
		"cleanupBtn.onclick = () => { syncFilters(); cleanupIMAuditMessages(); }",
		"if (state.imAuditLoading || (append && !state.imAuditNextBefore)) return;",
		"state.imAuditLoaded = true;",
		"state.imAuditLoaded = false;",
		`const busy = state.imAuditLoading ? "disabled" : "";`,
		`id="imAuditCleanup" ${busy}`,
		`second: "2-digit"`,
		"function thirdPartyProtocolEndpoint()",
		"function thirdPartyEndpointBase()",
		"window.location.origin",
		"window.location.host",
		"copyThirdPartyEndpoint",
		"/api/v1/im/status",
		"/api/im-gateway/v1",
		"window.crypto.getRandomValues(bytes)",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("user web missing channel marker %s", needle)
		}
	}
	for _, stale := range []string{
		"function hubManagedChannelCard()",
		"Third-party HTTP Gateway",
		"channelHubManaged",
		"channelLocalModeHint",
		"channelVoice",
		`["thirdparty_gateway_token", "thirdparty_gateway_host", "thirdparty_gateway_port"]`,
		`cfg_thirdparty_gateway_host`,
		`cfg_thirdparty_gateway_port`,
		`id="generateThirdPartyToken"`,
		`function generateThirdPartyToken()`,
		"asr_enabled:",
		"thirdparty_gateway_local_mode:",
		"qqbot_local_mode:",
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("user web still contains stale IM wording %s", stale)
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

func TestUserWebSearchConfigIsReadOnlyAndAdminManaged(t *testing.T) {
	secret := "test-token-secret-0123456789012345"
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: secret}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
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
	p := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateDefaultClientConfig(t.Context(), corelib.AppConfig{
		WebSearchProviders:       []corelib.WebSearchProvider{{Name: "admin-search", Type: "tinyfish"}},
		WebSearchCurrentProvider: "admin-search",
		DefaultProxyEnabled:      true,
		DefaultProxyHost:         "admin.proxy",
		SecurityPolicyMode:       "strict",
		NetworkLevel:             "allowlist",
		Language:                 "zh-CN",
	}); err != nil {
		t.Fatalf("UpdateDefaultClientConfig: %v", err)
	}
	token, _, err := agentservice.NewTokenManager(secret, time.Hour).Issue(p)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"app_config":{"web_search_providers":[{"name":"user-search","type":"brave"}],"web_search_current_provider":"user-search","default_proxy_enabled":false,"default_proxy_host":"user.proxy","security_policy_mode":"developer","network_level":"full","language":"en-US","maclaw_llm_url":"https://llm.example/v1","maclaw_llm_key":"secret-key","maclaw_llm_model":"gpt-test"}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("user update config = %d body = %s", w.Code, w.Body.String())
	}
	raw, err := svc.GetRawUserConfig(t.Context(), p)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	if len(raw.AppConfig.WebSearchProviders) != 0 || raw.AppConfig.WebSearchCurrentProvider != "" || raw.AppConfig.DefaultProxyHost != "" || raw.AppConfig.SecurityPolicyMode != "" || raw.AppConfig.NetworkLevel != "" || raw.AppConfig.Language != "" {
		t.Fatalf("user raw config should not persist admin-managed client config: %#v", raw.AppConfig)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get config = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		AppConfig corelib.AppConfig `json:"app_config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if out.AppConfig.WebSearchCurrentProvider != "admin-search" || len(out.AppConfig.WebSearchProviders) != 1 || out.AppConfig.WebSearchProviders[0].Type != "tinyfish" {
		t.Fatalf("user visible config should show admin-managed search: %#v", out.AppConfig)
	}
	if !out.AppConfig.DefaultProxyEnabled || out.AppConfig.DefaultProxyHost != "admin.proxy" || out.AppConfig.SecurityPolicyMode != "strict" || out.AppConfig.NetworkLevel != "allowlist" || out.AppConfig.Language != "zh-CN" {
		t.Fatalf("user visible config should show admin-managed shared config: %#v", out.AppConfig)
	}
}

func TestAdminDefaultClientConfigPartialUpdatePreservesBooleans(t *testing.T) {
	secret := "test-token-secret-0123456789012345"
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: secret}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/client-config/default", strings.NewReader(`{"app_config":{"web_search_providers":[{"name":"new-search","type":"serpapi"}],"web_search_current_provider":"new-search"}}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("partial update = %d body = %s", w.Code, w.Body.String())
	}
	got, err := svc.GetDefaultClientConfig(t.Context())
	if err != nil {
		t.Fatalf("GetDefaultClientConfig after partial update: %v", err)
	}
	if got.AppConfig.DefaultProxyEnabled || !got.AppConfig.VectorSearchEnabled || !got.AppConfig.ASREnabled || !got.AppConfig.TTSEnabled {
		t.Fatalf("first partial update should use AppConfig shared defaults: %#v", got.AppConfig)
	}
	if got.AppConfig.WebSearchCurrentProvider != "new-search" || len(got.AppConfig.WebSearchProviders) != 1 || got.AppConfig.WebSearchProviders[0].Type != "serpapi" {
		t.Fatalf("partial update should apply submitted search config: %#v", got.AppConfig.WebSearchProviders)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/client-config/default", strings.NewReader(`{"app_config":{"default_proxy_enabled":true,"default_proxy_scope_coding_tools":true,"default_proxy_scope_agent":true}}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("proxy update = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/client-config/default", strings.NewReader(`{"app_config":{"vector_search_enabled":false}}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("explicit false update = %d body = %s", w.Code, w.Body.String())
	}
	got, err = svc.GetDefaultClientConfig(t.Context())
	if err != nil {
		t.Fatalf("GetDefaultClientConfig after false update: %v", err)
	}
	if !got.AppConfig.VectorSearchEnabled || !got.AppConfig.DefaultProxyEnabled || !got.AppConfig.DefaultProxyScopeCodingTools || !got.AppConfig.DefaultProxyScopeAgent || !got.AppConfig.ASREnabled || !got.AppConfig.TTSEnabled {
		t.Fatalf("AI model toggles should stay auto-enabled while other booleans update: %#v", got.AppConfig)
	}
}

func TestAdminDefaultClientConfigSchemaOnlyExposesSharedFields(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/client-config/schema", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "root-admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("schema = %d body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"web_search_providers", "default_proxy_enabled", "default_proxy_scope_coding_tools", "security_policy_mode", "network_level", "vector_search_enabled", "maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_providers", "llm_prompt_cache", "tts_voice_id", "knowledge_vision_llm", "auxiliary_llm", "model_routes"} {
		if !strings.Contains(body, want) {
			t.Fatalf("client config schema missing shared field %s: %s", want, body)
		}
	}
	for _, forbidden := range []string{"memory_max_backups", "yolo_mode_allowed"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("client config schema should not expose private user field %s: %s", forbidden, body)
		}
	}
}
