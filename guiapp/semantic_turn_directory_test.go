package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestTurnDirectoryWordResumeGrantsOfficeOnly(t *testing.T) {
	result := intent.ClassificationResult{
		Primary:    intent.LabelOffice,
		Secondary:  []intent.IntentLabel{intent.LabelSearch},
		Confidence: 0.90,
	}
	if agentservice.ArchetypeBundleKey(result) != intent.LabelOffice {
		t.Fatalf("bundle key=%s", agentservice.ArchetypeBundleKey(result))
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticNeedsFromClassification(registry, result)
	if err != nil || !managed {
		t.Fatalf("managed=%v err=%v", managed, err)
	}
	for _, need := range needs {
		if need.Capability == agentservice.CapabilityInformationSearchWeb {
			t.Fatalf("word resume minted web_search: %#v", needs)
		}
	}
	sawOffice := false
	for _, need := range needs {
		if need.Capability == tool.CapabilityDocumentWriteOffice && need.Required {
			sawOffice = true
		}
	}
	if !sawOffice {
		t.Fatalf("office grant missing: %#v", needs)
	}
	if !semanticDirectoryAdmitsPetition(result, "web_search") {
		t.Fatal("secondary search stays petitionable and is not a grant")
	}
	for _, entry := range semanticTurnDirectory(result) {
		if entry.Name == "bash" || entry.Name == "write_file" {
			t.Fatalf("resume directory listed %s", entry.Name)
		}
	}
}

func TestTurnDirectoryImagePPTKeepsSearchAndOffersOffice(t *testing.T) {
	result := intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Secondary:  []intent.IntentLabel{intent.LabelOffice},
		Confidence: 0.90,
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, _, err := semanticNeedsFromClassification(registry, result)
	if err != nil {
		t.Fatal(err)
	}
	sawSearch := false
	for _, need := range needs {
		if need.Capability == tool.CapabilityDocumentWriteOffice {
			t.Fatalf("office was granted: %#v", needs)
		}
		if need.Capability == agentservice.CapabilityInformationSearchWeb && need.Required {
			sawSearch = true
		}
	}
	if !sawSearch {
		t.Fatalf("search grant missing: %#v", needs)
	}
	if !semanticDirectoryAdmitsPetition(result, "office") {
		t.Fatalf("directory=%v", semanticTurnDirectory(result))
	}
}

func TestTurnDirectoryWeatherTimeoutOffersPDF(t *testing.T) {
	hint := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Confidence: 0.74,
		Degraded:   true,
		Offered:    []intent.IntentLabel{intent.LabelDocumentGenerate},
	}
	if !semanticDirectoryAdmitsPetition(hint, "generate_pdf") {
		t.Fatalf("directory=%v", semanticTurnDirectory(hint))
	}
	sawSearch := false
	for _, entry := range semanticTurnDirectory(hint) {
		if entry.Name == "web_search" && entry.Role == "grant" {
			sawSearch = true
		}
		if entry.Name == "bash" || entry.Name == "write_file" {
			t.Fatalf("timeout directory listed %s", entry.Name)
		}
	}
	if !sawSearch {
		t.Fatalf("directory=%v", semanticTurnDirectory(hint))
	}
}

func TestTurnDirectoryOrdinaryQAOmitsWorkspaceTools(t *testing.T) {
	result := intent.ClassificationResult{Primary: intent.LabelNonCoding, Confidence: 0.9}
	if !semanticOrdinaryQA(result) {
		t.Fatal("non_coding should be ordinary QA")
	}
	entries := semanticTurnDirectory(result)
	if len(entries) != 0 {
		t.Fatalf("directory=%v", entries)
	}
	var b strings.Builder
	appendSemanticTurnDirectoryRules(&b, result)
	text := b.String()
	if !strings.Contains(text, "没有 bash") || !strings.Contains(text, "write_file") {
		t.Fatalf("prompt=%s", text)
	}
	if semanticDirectoryAdmitsPetition(result, "ssh") || semanticDirectoryAdmitsPetition(result, "mailbox_send") || semanticDirectoryAdmitsPetition(result, "computer_use") {
		t.Fatal("ordinary QA admitted a high-risk tool")
	}
}

func TestTurnDirectoryPlansTheFourAcceptanceFaces(t *testing.T) {
	resume := intent.ClassificationResult{
		Primary: intent.LabelOffice, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: 0.90,
	}
	resumeDefs, resumeSurface, resumeCB := turnDirectoryPlannedSurface(t, "帮我写一份 word 简历", resume)
	if !turnDirectoryLists(resumeDefs, "office") || turnDirectoryListsAny(resumeDefs, "web_search", "bash", "write_file") {
		t.Fatalf("resume function list=%v", turnDirectoryNames(resumeDefs))
	}
	if semanticGrantNameForAdapter(resumeSurface, semanticTrustedWebSearchAdapter) != "" {
		t.Fatal("word resume granted web_search")
	}
	if granted, message := resumeCB.PetitionToolCall("web_search"); !granted || !strings.Contains(message, "web_search") {
		t.Fatalf("secondary search must petition: granted=%v message=%q", granted, message)
	}

	ppt := intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelOffice}, Confidence: 0.90,
	}
	pptDefs, pptSurface, pptCB := turnDirectoryPlannedSurface(t, "网上找布偶猫照片，做成生日 PPT", ppt)
	if !turnDirectoryLists(pptDefs, "web_search") || turnDirectoryListsAny(pptDefs, "office", "bash", "write_file") {
		t.Fatalf("ppt function list=%v", turnDirectoryNames(pptDefs))
	}
	if semanticGrantNameForAdapter(pptSurface, semanticTrustedOfficeWriteAdapter) != "" {
		t.Fatal("ppt granted office")
	}
	if granted, message := pptCB.PetitionToolCall("office"); !granted || !strings.Contains(message, "office") {
		t.Fatalf("office must petition: granted=%v message=%q", granted, message)
	}

	weather := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.74, Degraded: true,
		Offered: []intent.IntentLabel{intent.LabelDocumentGenerate},
		Reason:  "embedding ambiguous; tree classification unavailable",
	}
	weatherDefs, weatherSurface, weatherCB := turnDirectoryPlannedSurface(t, "北京天气", weather)
	if weatherSurface == nil || !turnDirectoryLists(weatherDefs, "web_search") || turnDirectoryListsAny(weatherDefs, "generate_pdf", "bash", "write_file") {
		t.Fatalf("weather function list=%v", turnDirectoryNames(weatherDefs))
	}
	if weather.Primary == intent.LabelUnknown {
		t.Fatal("tree timeout collapsed the lookup")
	}
	granted, message := weatherCB.PetitionToolCall("generate_pdf")
	if !(granted && strings.Contains(message, "generate_pdf")) && !successorNotYetListedDenial(message, "generate_pdf") {
		t.Fatalf("timeout pdf must petition: granted=%v message=%q", granted, message)
	}

	ordinary := &LoopContext{Runtime: RuntimeContext{
		SemanticIntent: &intent.ClassificationResult{Primary: intent.LabelNonCoding, Confidence: 0.9},
	}}
	set := (&IMMessageHandler{}).prepareAgentLoopTools("desktop-user", "你是谁", ordinary, agentLoopPhase{})
	if len(set.Tools) != 0 || len(set.BaseTools) != 0 {
		t.Fatalf("ordinary QA published tools=%d base=%d", len(set.Tools), len(set.BaseTools))
	}
	denied, message := (&sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, loopCtx: ordinary}).PetitionToolCall("bash")
	if denied || !strings.Contains(message, "能力目录") {
		t.Fatalf("ordinary QA admitted bash: granted=%v message=%q", denied, message)
	}
}

func TestTurnDirectoryNamesLiveDataChart(t *testing.T) {
	visual := intent.ClassificationResult{Primary: intent.LabelLiveDataVisual, Confidence: .92}
	roles := map[string]string{}
	for _, entry := range semanticTurnDirectory(visual) {
		roles[entry.Name] = entry.Role
	}
	chart := semanticTrustedLiveDataVisualAdapter
	if roles["web_search"] != "grant" || roles[chart] != "grant" {
		t.Fatalf("chart directory=%v", roles)
	}
	if _, ok := roles["send_image"]; ok {
		t.Fatalf("chart listed send_image before it is granted: %v", roles)
	}
	if got := tool.RenderedSemanticFunctionName(chart, "invoke_turn_a"); got != chart {
		t.Fatalf("chart render=%q", got)
	}
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	if semanticDirectoryAdmitsPetition(search, chart) {
		t.Fatalf("search directory=%v", semanticTurnDirectory(search))
	}
	lookup := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual}, Confidence: .92,
	}
	if !semanticDirectoryAdmitsPetition(lookup, chart) {
		t.Fatalf("lookup directory=%v", semanticTurnDirectory(lookup))
	}
	for _, entry := range semanticTurnDirectory(lookup) {
		if entry.Name == "web_search" && entry.Role != "grant" {
			t.Fatalf("lookup web_search=%q", entry.Role)
		}
	}
}

func TestTurnDirectoryNamesPlannedInstalledTool(t *testing.T) {
	worklog := intent.ClassificationResult{Primary: intent.LabelWorklogUpdate, Confidence: .92}
	exposure := semanticDirectoryExposure{Planned: map[string]bool{
		"tengyun__post_work_log_add": true,
		"send_image":                 true,
	}}
	got := semanticPromptWithTurnDirectory("Governed tools:", worklog, exposure)
	if !strings.Contains(got, "- tengyun__post_work_log_add — Installed tool for this turn. 已列入本轮计划，不要直接调用，也不要请愿。") {
		t.Fatalf("planned installed tool missing:\n%s", got)
	}
	if strings.Contains(got, "send_image") || strings.Contains(got, "可请愿，不在当前函数列表里") {
		t.Fatalf("planned delivery or a petition leaked into the worklog directory:\n%s", got)
	}
	exposure.Granted = map[string]bool{"tengyun__post_work_log_add": true}
	got = semanticPromptWithTurnDirectory("Governed tools:", worklog, exposure)
	if !strings.Contains(got, "- tengyun__post_work_log_add — Installed tool for this turn. 已在当前函数列表里。") {
		t.Fatalf("granted installed tool missing:\n%s", got)
	}
	if strings.Count(got, "tengyun__post_work_log_add") != 1 {
		t.Fatalf("installed tool listed twice:\n%s", got)
	}
	adapter := "installed-mcp:tengyun__post_work_log_add:dynamic_mcp_abc"
	fromPlan := semanticSurfaceExposure(&semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{{AdapterName: adapter}}},
	})
	got = semanticPromptWithTurnDirectory("Governed tools:", worklog, fromPlan)
	if strings.Contains(got, "installed-mcp:") || strings.Contains(got, "dynamic_mcp") {
		t.Fatalf("directory printed the internal adapter:\n%s", got)
	}
	if !strings.Contains(got, "- tengyun__post_work_log_add — Installed tool for this turn. 已列入本轮计划，不要直接调用，也不要请愿。") {
		t.Fatalf("rendered plan name missing:\n%s", got)
	}
	fromPlan.Retired = map[string]bool{"tengyun__post_work_log_add": true}
	got = semanticPromptWithTurnDirectory("Governed tools:", worklog, fromPlan)
	if !strings.Contains(got, "已安装的 MCP 工具，继续用这个名字调用。") || strings.Contains(got, "不要直接调用") {
		t.Fatalf("retired installed tool:\n%s", got)
	}
	cb := &sharedAgentLoopCallbacks{
		semanticSurface: &semanticCallSurface{
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{{AdapterName: adapter}}},
		},
		discoveredMCPSurface: &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{
				"tengyun__post_work_log_add": {},
				"other__unplanned":           {},
			},
		},
	}
	got = semanticPromptWithTurnDirectory("Governed tools:", worklog, cb.semanticDirectoryExposure())
	if strings.Contains(got, "已在当前函数列表里") {
		t.Fatalf("sidecar grant without a definition was called listed:\n%s", got)
	}
	cb.tools = []map[string]interface{}{{
		"type":     "function",
		"function": map[string]interface{}{"name": "tengyun__post_work_log_add"},
	}}
	got = semanticPromptWithTurnDirectory("Governed tools:", worklog, cb.semanticDirectoryExposure())
	if !strings.Contains(got, "- tengyun__post_work_log_add — Installed tool for this turn. 已在当前函数列表里。") {
		t.Fatalf("sidecar grant must stay callable:\n%s", got)
	}
	if strings.Contains(got, "不要直接调用") || strings.Contains(got, "other__unplanned") {
		t.Fatalf("sidecar grant was told not to call, or an unplanned tool was listed:\n%s", got)
	}
	cb.systemPrompt = semanticPromptWithTurnDirectory("Governed tools:", worklog, semanticSurfaceExposure(cb.semanticSurface))
	cb.semanticSurface.replan = &semanticReplanInput{Classification: worklog}
	if strings.Contains(cb.systemPrompt, "已在当前函数列表里") {
		t.Fatalf("prompt claimed the tool before publication:\n%s", cb.systemPrompt)
	}
	cb.systemPrompt = appendTaskIdentityAnchorPrompt(cb.systemPrompt, &taskIdentityAnchor{Subject: "马勇"})
	cb.projectSemanticDirectoryPrompt()
	if !strings.Contains(cb.systemPrompt, "- tengyun__post_work_log_add — Installed tool for this turn. 已在当前函数列表里。") {
		t.Fatalf("publication did not rewrite the directory:\n%s", cb.systemPrompt)
	}
	if strings.Count(cb.systemPrompt, "[任务身份与来源锚点 — 强制约束]") != 1 || !strings.Contains(cb.systemPrompt, "马勇") {
		t.Fatalf("directory rewrite dropped the task anchor:\n%s", cb.systemPrompt)
	}
	if strings.Contains(cb.systemPrompt, "other__unplanned") {
		t.Fatalf("publication listed an unplanned tool:\n%s", cb.systemPrompt)
	}
	cb.semanticSurface.grants = map[string]tool.InvocationGrant{"tengyun__post_work_log_add": {}}
	cb.tools = nil
	got = semanticPromptWithTurnDirectory("Governed tools:", worklog, cb.semanticDirectoryExposure())
	if strings.Contains(got, "已在当前函数列表里") {
		t.Fatalf("managed grant without a definition was called listed:\n%s", got)
	}
	cb.tools = []map[string]interface{}{{
		"type":     "function",
		"function": map[string]interface{}{"name": "tengyun__post_work_log_add"},
	}}
	kept := cb.tools
	cb.systemPrompt = semanticPromptWithTurnDirectory("Governed tools:", worklog, cb.semanticDirectoryExposure())
	cb.SyncDirectoryToRenderedTools(nil)
	if strings.Contains(cb.systemPrompt, "已在当前函数列表里") {
		t.Fatalf("filtered list still described the tool as listed:\n%s", cb.systemPrompt)
	}
	if len(cb.tools) != len(kept) {
		t.Fatalf("sync replaced the host tool snapshot: %d", len(cb.tools))
	}
	cb.userText = "[系统续接][plan] 本轮只做规划"
	got = semanticPromptWithTurnDirectory("Governed tools:", worklog, cb.semanticDirectoryExposure())
	if strings.Contains(got, "已在当前函数列表里") || !strings.Contains(got, "已列入本轮计划，不要直接调用，也不要请愿。") {
		t.Fatalf("plan phase listed a tool the authorizer removes:\n%s", got)
	}
}

func TestTurnDirectoryNamesLocalSpeech(t *testing.T) {
	local := intent.ClassificationResult{Primary: intent.LabelAudioSynthesize, Confidence: .92}
	roles := map[string]string{}
	for _, entry := range semanticTurnDirectory(local) {
		roles[entry.Name] = entry.Role
	}
	if roles["tts_local"] != "grant" {
		t.Fatalf("local speech directory=%v", roles)
	}
	for _, closed := range []string{"tts", "tts_render", "send_voice", "bash", "write_file"} {
		if _, ok := roles[closed]; ok {
			t.Fatalf("local speech listed %s: %v", closed, roles)
		}
	}
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	if semanticDirectoryAdmitsPetition(search, "tts_local") {
		t.Fatalf("search directory=%v", semanticTurnDirectory(search))
	}
	deliver := intent.ClassificationResult{Primary: intent.LabelAudioDeliver, Confidence: .92}
	if semanticDirectoryAdmitsPetition(deliver, "tts_local") {
		t.Fatalf("voice deliver directory=%v", semanticTurnDirectory(deliver))
	}
}

func TestTurnDirectoryNamesScheduleDispatch(t *testing.T) {
	dispatch := intent.ClassificationResult{Primary: intent.LabelScheduleDispatch, Confidence: .92}
	roles := map[string]string{}
	for _, entry := range semanticTurnDirectory(dispatch) {
		roles[entry.Name] = entry.Role
	}
	if roles["manage_schedule"] != "grant" || roles["schedule_dispatch"] != "grant" {
		t.Fatalf("dispatch directory=%v", roles)
	}
	for _, closed := range []string{"send_to_im", "send_file", "bash", "write_file"} {
		if _, ok := roles[closed]; ok {
			t.Fatalf("dispatch listed %s: %v", closed, roles)
		}
	}
	manage := intent.ClassificationResult{Primary: intent.LabelScheduleManage, Confidence: .92}
	if semanticDirectoryAdmitsPetition(manage, "schedule_dispatch") {
		t.Fatalf("administer directory=%v", semanticTurnDirectory(manage))
	}
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	if semanticDirectoryAdmitsPetition(search, "schedule_dispatch") {
		t.Fatalf("search directory=%v", semanticTurnDirectory(search))
	}
}

func TestTurnDirectoryNamesSpeechRender(t *testing.T) {
	voice := intent.ClassificationResult{Primary: intent.LabelAudioDeliver, Confidence: .92}
	roles := map[string]string{}
	for _, entry := range semanticTurnDirectory(voice) {
		roles[entry.Name] = entry.Role
	}
	if roles["tts_render"] != "grant" {
		t.Fatalf("voice directory=%v", roles)
	}
	for _, closed := range []string{"send_voice", "tts", "tts_local", "bash", "write_file"} {
		if _, ok := roles[closed]; ok {
			t.Fatalf("voice deliver listed %s: %v", closed, roles)
		}
	}
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	if semanticDirectoryAdmitsPetition(search, "tts_render") {
		t.Fatalf("search directory=%v", semanticTurnDirectory(search))
	}
	offered := intent.ClassificationResult{
		Primary: intent.LabelSearch, Offered: []intent.IntentLabel{intent.LabelAudioDeliver}, Confidence: .92,
	}
	if !semanticDirectoryAdmitsPetition(offered, "tts_render") {
		t.Fatalf("offered voice directory=%v", semanticTurnDirectory(offered))
	}
}

func TestTurnDirectoryNamesTrustedDocumentRead(t *testing.T) {
	doc := intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: .92}
	roles := map[string]string{}
	for _, entry := range semanticTurnDirectory(doc) {
		roles[entry.Name] = entry.Role
	}
	if roles["read_attachment"] != "grant" {
		t.Fatalf("document read directory=%v", roles)
	}
	for _, closed := range []string{"read_document", "office", "write_file", "bash"} {
		if _, ok := roles[closed]; ok {
			t.Fatalf("document read listed %s: %v", closed, roles)
		}
	}
	search := intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelDocumentRead}, Confidence: .92,
	}
	if !semanticDirectoryAdmitsPetition(search, "read_attachment") {
		t.Fatalf("search directory=%v", semanticTurnDirectory(search))
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, _, err := semanticNeedsFromClassification(registry, search)
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range needs {
		if need.Capability == agentservice.CapabilityDocumentRead {
			t.Fatalf("search minted document read: %#v", needs)
		}
	}
}

func TestDirectoryListsGrantedNamesTheClassificationDidNotName(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	prompt := semanticPromptWithTurnDirectory("Governed tools: fence\n", search, semanticDirectoryExposure{
		Granted: map[string]bool{"web_search": true, "memory_recall": true, "send_image": true, "tools_search": true},
	})
	var recall, searchLine, image string
	for _, line := range strings.Split(prompt, "\n") {
		switch {
		case strings.HasPrefix(line, "- memory_recall "):
			recall = line
		case strings.HasPrefix(line, "- web_search "):
			searchLine = line
		case strings.HasPrefix(line, "- send_image "):
			image = line
		case strings.HasPrefix(line, "- tools_search "):
			t.Fatalf("meta tool entered the directory: %s", line)
		}
	}
	if !strings.Contains(recall, "已在当前函数列表里") || strings.Contains(recall, "可请愿") {
		t.Fatalf("recall=%q", recall)
	}
	if !strings.Contains(image, "已在当前函数列表里") || !strings.Contains(image, "approved image") || strings.Contains(image, "可请愿") {
		t.Fatalf("image=%q", image)
	}
	if !strings.Contains(searchLine, "已在当前函数列表里") {
		t.Fatalf("search=%q", searchLine)
	}
	cb := &sharedAgentLoopCallbacks{loopCtx: &LoopContext{Runtime: RuntimeContext{SemanticIntent: &search}}}
	if cb.semanticTurnAdmits("memory_recall") || cb.semanticTurnAdmits("send_image") {
		t.Fatal("a host grant became petitionable")
	}
}

func TestTurnDirectoryNamesCodingTools(t *testing.T) {
	names := func(result intent.ClassificationResult) map[string]string {
		got := map[string]string{}
		for _, entry := range semanticTurnDirectory(result) {
			got[entry.Name] = entry.Role
		}
		return got
	}
	coding := names(intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: .92})
	for _, name := range []string{"read_file", "write_file", "git_status", "build_verify"} {
		if coding[name] != "grant" {
			t.Fatalf("%s=%q directory=%v", name, coding[name], coding)
		}
	}
	if coding["bash"] != "" || coding["generate_pdf"] != "" || coding["office"] != "" {
		t.Fatalf("coding directory=%v", coding)
	}
	search := names(intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92})
	if search["write_file"] != "" || search["build_verify"] != "" || search["bash"] != "" {
		t.Fatalf("search directory=%v", search)
	}
}

func TestTurnDirectoryNamesDeliveryAndDocumentOpen(t *testing.T) {
	names := func(result intent.ClassificationResult) map[string]string {
		got := map[string]string{}
		for _, entry := range semanticTurnDirectory(result) {
			got[entry.Name] = entry.Role
		}
		return got
	}
	open := names(intent.ClassificationResult{Primary: intent.LabelDocumentOpen, Confidence: .92})
	if open["open"] != "grant" || open["send_file"] != "" {
		t.Fatalf("document open directory=%v", open)
	}
	file := names(intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: .92})
	if file["send_file"] != "grant" || file["send_to_im"] != "" {
		t.Fatalf("attachment delivery directory=%v", file)
	}
	target := names(intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: .92})
	if target["send_to_im"] != "grant" || target["send_file"] != "" {
		t.Fatalf("document delivery directory=%v", target)
	}
	search := names(intent.ClassificationResult{
		Primary: intent.LabelSearch, Confidence: .92,
		Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate},
	})
	if search["send_file"] != "" || search["send_to_im"] != "" {
		t.Fatalf("generate neighbor must not list delivery=%v", search)
	}
}

func TestToolsSearchPinsDirectoryAheadOfAlphabeticalTail(t *testing.T) {
	entries := []semanticToolsSearchEntry{
		{name: "bash", summary: "shell"},
		{name: "database_query", summary: "sql"},
		{name: "generate_pdf", summary: "pdf"},
		{name: "web_search", summary: "search"},
	}
	result := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.74, Degraded: true,
		Offered: []intent.IntentLabel{intent.LabelDocumentGenerate},
	}
	got := semanticToolsSearchApplyDirectory(entries, result)
	if len(got) < 2 || got[0].name != "web_search" || got[1].name != "generate_pdf" {
		t.Fatalf("pinned=%v", got)
	}
	if got[0].name == "database_query" || got[1].name == "database_query" {
		t.Fatalf("alphabetical tail hid the directory: %v", got)
	}
}

func turnDirectoryPlannedSurface(t *testing.T, text string, result intent.ClassificationResult) ([]map[string]interface{}, *semanticCallSurface, *sharedAgentLoopCallbacks) {
	t.Helper()
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, result.Primary)}
	h.semanticTrustedOfficeWrite = func(string, string, map[string]interface{}) (string, error) { return "ok", nil }
	h.semanticTrustedWebSearch = func(_, query string) (string, error) { return "found: " + query, nil }
	registerBuiltinTools(h.registry, h)
	stored := result
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", text, "desktop", "root-directory", "turn-directory", &stored,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("surface for %q handled=%v err=%v defs=%d", text, handled, err, len(defs))
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop",
		loopCtx: &LoopContext{Runtime: RuntimeContext{SemanticIntent: &stored}},
	}
	return defs, surface, cb
}

func turnDirectoryHave(defs []map[string]interface{}) map[string]bool {
	have := map[string]bool{}
	for _, def := range defs {
		have[extractToolName(def)] = true
	}
	return have
}

func turnDirectoryLists(defs []map[string]interface{}, names ...string) bool {
	have := turnDirectoryHave(defs)
	for _, name := range names {
		if !have[name] {
			return false
		}
	}
	return true
}

func turnDirectoryListsAny(defs []map[string]interface{}, names ...string) bool {
	have := turnDirectoryHave(defs)
	for _, name := range names {
		if have[name] {
			return true
		}
	}
	return false
}

func turnDirectoryNames(defs []map[string]interface{}) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, extractToolName(def))
	}
	return names
}

func TestTurnDirectoryArchetypeCompanionIsPetitionableNotGranted(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	if !semanticDirectoryAdmitsPetition(search, "web_fetch") {
		t.Fatalf("search directory=%v", semanticTurnDirectory(search))
	}
	if semanticDirectoryAdmitsPetition(search, "bash") || semanticDirectoryAdmitsPetition(search, "write_file") {
		t.Fatal("search directory opened bash or write_file")
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticNeedsFromClassification(registry, search)
	if err != nil || !managed {
		t.Fatalf("managed=%v err=%v", managed, err)
	}
	for _, need := range needs {
		if need.Capability == tool.CapabilityInformationFetchWeb {
			t.Fatalf("web_fetch was granted on a search primary: %#v", needs)
		}
	}
	read := intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: .92}
	if semanticDirectoryAdmitsPetition(read, "write_file") || semanticDirectoryAdmitsPetition(read, "bash") {
		t.Fatalf("file read directory=%v", semanticTurnDirectory(read))
	}
	defs, _, cb := turnDirectoryPlannedSurface(t, "网上找布偶猫照片", search)
	if turnDirectoryListsAny(defs, "web_fetch", "bash", "write_file") {
		t.Fatalf("function list=%v", turnDirectoryNames(defs))
	}
	granted, message := cb.PetitionToolCall("web_fetch")
	if !granted || !strings.Contains(message, "web_fetch") {
		t.Fatalf("web_fetch must petition onto the search turn: granted=%v message=%q", granted, message)
	}
}

func TestTurnDirectoryDesktopCompanionsStayPetitionable(t *testing.T) {
	browser := intent.ClassificationResult{Primary: intent.LabelBrowser, Confidence: .92}
	entries := semanticTurnDirectory(browser)
	roles := map[string]string{}
	for _, entry := range entries {
		roles[entry.Name] = entry.Role
	}
	if roles["browser"] != "grant" || roles["screenshot"] != "petition" || roles["open"] != "petition" {
		t.Fatalf("browser directory=%v", entries)
	}
	if semanticDirectoryAdmitsPetition(browser, "bash") || semanticDirectoryAdmitsPetition(browser, "computer_use") {
		t.Fatal("browser directory opened bash or computer_use")
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticNeedsFromClassification(registry, browser)
	if err != nil || !managed {
		t.Fatalf("managed=%v err=%v", managed, err)
	}
	for _, need := range needs {
		if need.Capability == agentservice.CapabilityVisualCapture || need.Capability == tool.CapabilitySystemLaunchLocal {
			t.Fatalf("desktop companion was granted: %#v", needs)
		}
	}
	desktop := intent.ClassificationResult{Primary: intent.LabelComputerUse, Confidence: .92}
	if !semanticDirectoryAdmitsPetition(desktop, "screenshot") || !semanticDirectoryAdmitsPetition(desktop, "open") {
		t.Fatalf("computer_use directory=%v", semanticTurnDirectory(desktop))
	}
	weak := intent.ClassificationResult{Primary: intent.LabelComputerUse, Confidence: .60, Degraded: true}
	if semanticDirectoryAdmitsPetition(weak, "computer_use") || semanticDirectoryAdmitsPetition(weak, "screenshot") || semanticDirectoryAdmitsPetition(weak, "open") {
		t.Fatalf("weak desktop directory=%v", semanticTurnDirectory(weak))
	}
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	if semanticDirectoryAdmitsPetition(search, "screenshot") || semanticDirectoryAdmitsPetition(search, "browser") {
		t.Fatalf("search directory=%v", semanticTurnDirectory(search))
	}
}

func TestTurnDirectoryNamesTheStableToolForItsOwnFamily(t *testing.T) {
	knowledge := intent.ClassificationResult{Primary: intent.LabelKnowledgeRead, Confidence: .92}
	entries := semanticTurnDirectory(knowledge)
	if len(entries) != 1 || entries[0].Name != "knowledge_search" || entries[0].Role != "grant" {
		t.Fatalf("knowledge directory=%v", entries)
	}
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	if semanticDirectoryAdmitsPetition(search, "knowledge_search") || semanticDirectoryAdmitsPetition(search, "mailbox_delete") {
		t.Fatalf("search directory=%v", semanticTurnDirectory(search))
	}
	mail := intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: .92}
	roles := map[string]string{}
	for _, entry := range semanticTurnDirectory(mail) {
		roles[entry.Name] = entry.Role
	}
	if roles["mailbox_query"] != "grant" || roles["mailbox_delete"] != "petition" || roles["mailbox_configure"] != "petition" {
		t.Fatalf("mailbox directory=%v", semanticTurnDirectory(mail))
	}
	if semanticDirectoryAdmitsPetition(mail, "mailbox_send") || semanticDirectoryAdmitsPetition(mail, "bash") {
		t.Fatal("mailbox read opened send or bash")
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticNeedsFromClassification(registry, mail)
	if err != nil || !managed {
		t.Fatalf("managed=%v err=%v", managed, err)
	}
	for _, need := range needs {
		if need.Capability == tool.CapabilityMessageDeleteMailbox || need.Capability == tool.CapabilityMessageSendMailbox {
			t.Fatalf("mailbox mutation was granted: %#v", needs)
		}
	}
}

func TestDirectoryReplacementKeepsTheFollowingList(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	prompt := "## 本回合能力目录\n- web_search — old 主标签授权。\n[本回合能力目录结束]\n- keep this skill\n"
	got := semanticPromptWithTurnDirectory(prompt, search, semanticDirectoryExposure{})
	if !strings.Contains(got, "- keep this skill") || strings.Count(got, semanticTurnDirectoryClose) != 1 {
		t.Fatalf("replacement ate the following list:\n%s", got)
	}
	live := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: .9,
		Offered: []intent.IntentLabel{intent.LabelDocumentGenerate},
	}
	held := semanticPromptWithTurnDirectory("Governed tools: fence\n", live, semanticDirectoryExposure{
		Granted: map[string]bool{"web_search": true},
		Planned: map[string]bool{"web_search": true, "generate_pdf": true},
	})
	var pdf, searchLine, fetch string
	for _, line := range strings.Split(held, "\n") {
		switch {
		case strings.HasPrefix(line, "- generate_pdf "):
			pdf = line
		case strings.HasPrefix(line, "- web_search "):
			searchLine = line
		case strings.HasPrefix(line, "- web_fetch "):
			fetch = line
		}
	}
	if !strings.Contains(pdf, "不要请愿") || strings.Contains(pdf, "可请愿") {
		t.Fatalf("held pdf=%q", pdf)
	}
	if !strings.Contains(searchLine, "已在当前函数列表里") || strings.Contains(searchLine, "不要请愿") {
		t.Fatalf("search=%q", searchLine)
	}
	if !strings.Contains(fetch, "可请愿") || strings.Contains(fetch, "不要请愿") {
		t.Fatalf("fetch=%q", fetch)
	}
	spent := semanticPromptWithTurnDirectory("Governed tools: fence\n", live, semanticDirectoryExposure{
		Planned: map[string]bool{"generate_pdf": true},
		Retired: map[string]bool{"generate_pdf": true},
	})
	var spentLine string
	for _, line := range strings.Split(spent, "\n") {
		if strings.HasPrefix(line, "- generate_pdf ") {
			spentLine = line
		}
	}
	if !strings.Contains(spentLine, "授权已用尽") || strings.Contains(spentLine, "可请愿") || strings.Contains(spentLine, "不要请愿") {
		t.Fatalf("spent pdf=%q", spentLine)
	}
}

func TestDirectoryNamesThePredecessorAPlannedToolWaitsOn(t *testing.T) {
	live := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: .9,
		Offered: []intent.IntentLabel{intent.LabelDocumentGenerate},
	}
	pdf := tool.CapabilityID("document.generate.file")
	search := tool.CapabilityID("information.search.web")
	cb := &sharedAgentLoopCallbacks{
		systemPrompt: "Governed tools: fence\n",
		semanticSurface: &semanticCallSurface{
			replan:    &semanticReplanInput{Classification: live},
			completed: map[string]bool{},
			grants:    map[string]tool.InvocationGrant{"web_search": {SelectionID: "search", Token: "live"}},
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
				{ID: "search", AdapterName: "web_search", FitProof: tool.FitProof{MatchedCapability: search}},
				{ID: "pdf", AdapterName: "generate_pdf", FitProof: tool.FitProof{MatchedCapability: pdf}, Requires: []string{"search"}},
			}},
		},
	}
	cb.projectSemanticDirectoryPrompt()
	var row string
	for _, line := range strings.Split(cb.systemPrompt, "\n") {
		if strings.HasPrefix(line, "- generate_pdf ") {
			row = line
		}
	}
	if !strings.Contains(row, "web_search") || !strings.Contains(row, "不要请愿") || strings.Contains(row, "可请愿") {
		t.Fatalf("pdf row=%q", row)
	}
	if status := semanticToolsSearchStatus(cb, "generate_pdf"); !strings.Contains(status, "web_search") || !strings.Contains(status, "已列入本轮计划") {
		t.Fatalf("status=%s", status)
	}
}

func TestDirectoryPromptWaitsForMailboxConfirmation(t *testing.T) {
	mail := intent.ClassificationResult{Primary: intent.LabelMailboxSend, Confidence: .92}
	line := func(prompt string) string {
		for _, row := range strings.Split(prompt, "\n") {
			if strings.HasPrefix(row, "- mailbox_send ") {
				return row
			}
		}
		return ""
	}
	waiting := semanticPromptWithTurnDirectory("Governed tools: fence\n", mail, semanticDirectoryExposure{
		Retired:         map[string]bool{"mailbox_send": true},
		AwaitingConfirm: map[string]bool{"mailbox_send": true},
	})
	if row := line(waiting); !strings.Contains(row, "等待用户确认") || !strings.Contains(row, "其它工具") || strings.Contains(row, "已用尽") {
		t.Fatalf("confirm=%q", row)
	}
	pending := semanticPromptWithTurnDirectory("Governed tools: fence\n", mail, semanticDirectoryExposure{
		Retired:         map[string]bool{"mailbox_send": true},
		AwaitingReceipt: map[string]bool{"mailbox_send": true},
	})
	if row := line(pending); !strings.Contains(row, "结果未定") || strings.Contains(row, "已用尽") {
		t.Fatalf("receipt=%q", row)
	}
	surface := &semanticCallSurface{retiredGrants: map[string]tool.InvocationGrant{
		"semantic_send_trusted_mailbox": {AdapterName: "semantic_send_trusted_mailbox"},
	}}
	if !semanticSurfaceExposure(surface).Retired["mailbox_send"] {
		t.Fatal("retired adapter key did not reach the directory name")
	}
}

func TestDirectoryPromptClosesTheSpentPetitionClass(t *testing.T) {
	live := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: .9,
		Offered: []intent.IntentLabel{intent.LabelDocumentGenerate},
	}
	line := func(prompt, name string) string {
		prefix := "- " + name + " "
		for _, row := range strings.Split(prompt, "\n") {
			if strings.HasPrefix(row, prefix) {
				return row
			}
		}
		return ""
	}
	effectful := semanticPromptWithTurnDirectory("Governed tools: fence\n", live, semanticDirectoryExposure{EffectfulPetitionSpent: true})
	if pdf, fetch := line(effectful, "generate_pdf"), line(effectful, "web_fetch"); !strings.Contains(pdf, "请愿机会已用完") || strings.Contains(pdf, "可请愿") || !strings.Contains(fetch, "可请愿") {
		t.Fatalf("effectful pdf=%q fetch=%q", pdf, fetch)
	}
	read := semanticPromptWithTurnDirectory("Governed tools: fence\n", live, semanticDirectoryExposure{ReadPetitionSpent: true})
	if pdf, fetch := line(read, "generate_pdf"), line(read, "web_fetch"); !strings.Contains(fetch, "请愿机会已用完") || strings.Contains(fetch, "可请愿") || !strings.Contains(pdf, "可请愿") {
		t.Fatalf("read pdf=%q fetch=%q", pdf, fetch)
	}
	denied := semanticPromptWithTurnDirectory("Governed tools: fence\n", live, semanticDirectoryExposure{Denied: map[string]bool{"generate_pdf": true}})
	if pdf := line(denied, "generate_pdf"); !strings.Contains(pdf, "请愿未通过") || strings.Contains(pdf, "可请愿") {
		t.Fatalf("denied pdf=%q", pdf)
	}
}

func TestDirectoryAdmitCacheFollowsClassification(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	cb := &sharedAgentLoopCallbacks{loopCtx: &LoopContext{Runtime: RuntimeContext{SemanticIntent: &search}}}
	if !cb.semanticTurnAdmits("web_search") || cb.semanticTurnAdmits("office") {
		t.Fatal("search directory cache")
	}
	office := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .92}
	cb.loopCtx.Runtime.SemanticIntent = &office
	if !cb.semanticTurnAdmits("office") {
		t.Fatal("directory cache kept the previous classification")
	}
}

func TestDirectoryPromptUpdatesWhenAHeldToolIsListed(t *testing.T) {
	live := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: .9,
		Offered: []intent.IntentLabel{intent.LabelDocumentGenerate},
	}
	cb := &sharedAgentLoopCallbacks{
		systemPrompt: semanticPromptWithTurnDirectory("Governed tools: fence\n", live, semanticDirectoryExposure{
			Granted: map[string]bool{"web_search": true},
			Planned: map[string]bool{"web_search": true, "generate_pdf": true},
		}),
		semanticSurface: &semanticCallSurface{
			replan: &semanticReplanInput{Classification: live},
			grants: map[string]tool.InvocationGrant{"web_search": {}, "generate_pdf": {}},
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
				{AdapterName: "web_search"},
				{AdapterName: "generate_pdf"},
			}},
		},
	}
	var before string
	for _, line := range strings.Split(cb.systemPrompt, "\n") {
		if strings.HasPrefix(line, "- generate_pdf ") {
			before = line
		}
	}
	if !strings.Contains(before, "不要请愿") {
		t.Fatalf("held line=%q", before)
	}
	cb.projectSemanticDirectoryPrompt()
	var after string
	for _, line := range strings.Split(cb.systemPrompt, "\n") {
		if strings.HasPrefix(line, "- generate_pdf ") {
			after = line
		}
	}
	if !strings.Contains(after, "已在当前函数列表里") || strings.Contains(after, "不要请愿") {
		t.Fatalf("listed line=%q\n%s", after, cb.systemPrompt)
	}
}

func TestDirectoryPromptMarksAListedPetitionAsGranted(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}
	prompt := semanticPromptWithTurnDirectory("Governed tools: fence\n", search, semanticDirectoryExposure{Granted: map[string]bool{"web_search": true, "web_fetch": true}})
	fetchLine := ""
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "- web_fetch ") {
			fetchLine = line
		}
	}
	if !strings.Contains(fetchLine, "已在当前函数列表里") || strings.Contains(fetchLine, "不在当前函数列表") {
		t.Fatalf("fetch line=%q\nprompt=%s", fetchLine, prompt)
	}
	_, _, cb := turnDirectoryPlannedSurface(t, "网上找布偶猫照片", search)
	cb.systemPrompt = semanticPromptWithTurnDirectory("light\nGoverned tools: fence\n", search, semanticDirectoryExposure{})
	granted, message := cb.PetitionToolCall("web_fetch")
	if !granted || !strings.Contains(message, "web_fetch") {
		t.Fatalf("granted=%v message=%q", granted, message)
	}
	fetchLine = ""
	for _, line := range strings.Split(cb.systemPrompt, "\n") {
		if strings.HasPrefix(line, "- web_fetch ") {
			fetchLine = line
		}
	}
	if !strings.Contains(fetchLine, "已在当前函数列表里") || strings.Contains(fetchLine, "不在当前函数列表") {
		t.Fatalf("prompt still hides the grant: %q\n%s", fetchLine, cb.systemPrompt)
	}
}

func TestPromptDirectoryFollowsThePlannedClassification(t *testing.T) {
	stale := "lookup preface\n## 本回合能力目录\n- 本回合没有 bash，也没有 write_file。不要调用它们。\nGoverned tools: stay\ncall web_fetch on a result page\n"
	planned := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.74, Degraded: true,
		Offered: []intent.IntentLabel{intent.LabelDocumentGenerate},
	}
	got := semanticPromptWithTurnDirectory(stale, planned, semanticDirectoryExposure{})
	if strings.Count(got, "## 本回合能力目录") != 1 {
		t.Fatalf("directory duplicated:\n%s", got)
	}
	web := strings.Index(got, "web_search")
	pdf := strings.Index(got, "generate_pdf")
	fetch := strings.Index(got, "web_fetch")
	fence := strings.Index(got, "Governed tools:")
	page := strings.Index(got, "call web_fetch on a result page")
	if web < 0 || pdf < web || fetch < pdf || fence < fetch || page < fence {
		t.Fatalf("directory order lost:\n%s", got)
	}
	if strings.Contains(got, "没有 bash") || strings.Contains(got, "web_search / web_fetch") {
		t.Fatalf("stale ordinary-QA directory survived:\n%s", got)
	}
	if !strings.HasPrefix(got, "lookup preface\n") {
		t.Fatalf("preface lost:\n%s", got)
	}

	light := "short light prompt\nGoverned tools: the live tool list\n"
	inserted := semanticPromptWithTurnDirectory(light, intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .92}, semanticDirectoryExposure{})
	if strings.Index(inserted, "web_fetch") > strings.Index(inserted, "Governed tools:") || strings.Index(inserted, "web_fetch") < 0 {
		t.Fatalf("light prompt hid the companion:\n%s", inserted)
	}
	if strings.Contains(inserted, "bash —") || strings.Contains(inserted, "write_file") {
		t.Fatalf("light directory opened workspace tools:\n%s", inserted)
	}
}

func TestTurnDirectorySSHStaysClosedUntilPrimaryClearsWriteFloor(t *testing.T) {
	weak := intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: 0.60}
	if semanticDirectoryAdmitsPetition(weak, "ssh") {
		t.Fatal("sub-floor ssh entered the directory")
	}
	ready := intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: 0.90}
	if !semanticDirectoryAdmitsPetition(ready, "ssh") {
		t.Fatalf("directory=%v", semanticTurnDirectory(ready))
	}
}
