package cloudagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

type staticSessionNeeds struct {
	res      agentservice.DynamicCapabilityNeedResolution
	err      error
	calls    int
	lastText string
	lastTask string
}

func (s *staticSessionNeeds) ResolveSessionNeeds(_ context.Context, _, _, taskID, userText string) (agentservice.DynamicCapabilityNeedResolution, error) {
	s.calls++
	s.lastText = userText
	s.lastTask = taskID
	return s.res, s.err
}

func cloudSemanticHost(t *testing.T, coding bool, resolver sessionNeedResolver) *agentHost {
	t.Helper()
	dir := t.TempDir()
	bk, err := openBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bk.Close() })
	rt := &Runtime{}
	rt.SetSessionNeedResolver(resolver)
	kind := KindSession
	if coding {
		kind = KindCoding
	}
	return &agentHost{
		rt:     rt,
		task:   "task-1",
		coding: coding,
		inst:   &instance{dir: dir, meta: Spec{ID: "cag_route", TenantID: "ten", OwnerUserID: "usr", Kind: kind}, book: bk},
	}
}

func cloudToolNames(tools []map[string]interface{}) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		names = append(names, name)
	}
	return names
}

func cloudHasName(names []string, name string) bool {
	for _, got := range names {
		if got == name {
			return true
		}
	}
	return false
}

func TestCloudSemanticRouteDocumentPDF(t *testing.T) {
	resolver := &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs: []coretool.CapabilityNeed{
			{Capability: agentservice.CapabilityDocumentGenerate},
			{Capability: agentservice.CapabilityArtifactDeliverCurrent},
		},
		Classification: intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.95},
	}}
	h := cloudSemanticHost(t, false, resolver)
	names := cloudToolNames(h.BuildTools(""))
	if cloudHasName(names, "generate_pdf") || !cloudHasName(names, "bash") || resolver.calls != 0 {
		t.Fatalf("empty text decided the route: %v calls=%d", names, resolver.calls)
	}
	names = cloudToolNames(h.BuildTools("生成pdf"))
	if len(names) != 1 || names[0] != "generate_pdf" {
		t.Fatalf("document turn tools = %v", names)
	}
	if resolver.calls != 1 || resolver.lastText != "生成pdf" || resolver.lastTask != "task-1" {
		t.Fatalf("resolver calls=%d text=%q task=%q", resolver.calls, resolver.lastText, resolver.lastTask)
	}
	again := cloudToolNames(h.BuildTools("另一句"))
	if len(again) != 1 || again[0] != "generate_pdf" || resolver.calls != 1 {
		t.Fatalf("route was resolved again: %v calls=%d", again, resolver.calls)
	}
	prompt := h.BuildSystemPrompt("生成pdf", false)
	if strings.Contains(prompt, "bash") || strings.Contains(prompt, "读和写必须留在工作区里") || !strings.Contains(prompt, "generate_pdf") || !strings.Contains(prompt, "git.json") {
		t.Fatalf("document prompt = %s", prompt)
	}
	coding := cloudSemanticHost(t, true, &staticSessionNeeds{res: resolver.res})
	coding.plan = &planRecord{Steps: []planStep{{Index: 1, Title: "写 PDF"}}}
	codingNames := cloudToolNames(coding.BuildTools("生成pdf"))
	if len(codingNames) != 1 || codingNames[0] != "generate_pdf" {
		t.Fatalf("coding document tools = %v", codingNames)
	}
	codingPrompt := coding.BuildSystemPrompt("生成pdf", false)
	for _, banned := range []string{"git_commit", "git_push", "update_plan_step", "Final answer", "bash", "shell", "读和写必须留在工作区里"} {
		if strings.Contains(codingPrompt, banned) {
			t.Fatalf("routed coding prompt mentions %q: %s", banned, codingPrompt)
		}
	}
	for _, want := range []string{"generate_pdf", "git.json", "T1 写 PDF"} {
		if !strings.Contains(codingPrompt, want) {
			t.Fatalf("routed coding prompt missing %q: %s", want, codingPrompt)
		}
	}
}

func TestCloudSemanticRouteWithholdsPDFUntilSearch(t *testing.T) {
	h := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs:   []coretool.CapabilityNeed{{Capability: agentservice.CapabilityInformationSearchWeb}},
		Classification: intent.ClassificationResult{
			Primary:    intent.LabelLiveData,
			Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
			Confidence: 0.95,
		},
	}})
	names := cloudToolNames(h.BuildTools("北京天气，生成pdf"))
	if len(names) != 2 || names[0] != "web_search" || names[1] != "web_fetch" || cloudHasName(names, "generate_pdf") || cloudHasName(names, "bash") {
		t.Fatalf("lookup tools = %v", names)
	}
	prompt := h.BuildSystemPrompt("北京天气，生成pdf", false)
	if !strings.Contains(prompt, "检索成功之后才会出现 generate_pdf") || strings.Contains(prompt, "bash") {
		t.Fatalf("withheld prompt = %s", prompt)
	}
	prev := cloudWebSearch
	cloudWebSearch = func(context.Context, string) string { return "搜索失败: down" }
	t.Cleanup(func() { cloudWebSearch = prev })
	if out, failed := h.runTool("generate_pdf", `{"title":"天气","content":"# 天气"}`); !failed || !strings.Contains(out, "先调用 web_search") {
		t.Fatalf("early pdf = %q failed=%v", out, failed)
	}
	if out, failed := h.runTool("bash", `{"argv":["python3","make_weather_pdf.py"]}`); !failed || !strings.Contains(out, "不在本轮路由结果里") {
		t.Fatalf("routed bash = %q failed=%v", out, failed)
	}
	if out, failed := h.runTool("web_search", `{"query":""}`); !failed || !strings.Contains(out, "缺少 query") || h.searchDone {
		t.Fatalf("empty query = %q failed=%v done=%v", out, failed, h.searchDone)
	}
	out, failed := h.runTool("web_search", `{"query":"北京天气"}`)
	names = cloudToolNames(h.BuildTools("北京天气，生成pdf"))
	if !failed || h.searchDone || cloudHasName(names, "generate_pdf") {
		t.Fatalf("failed search unlocked pdf: %q failed=%v done=%v tools=%v", out, failed, h.searchDone, names)
	}
	cloudWebSearch = func(context.Context, string) string { return "未找到相关结果。" }
	if _, failed := h.runTool("web_search", `{"query":"北京天气"}`); failed || h.searchDone {
		t.Fatal("empty search was treated as a failure or unlocked pdf")
	}
	names = cloudToolNames(h.BuildTools("北京天气，生成pdf"))
	if cloudHasName(names, "generate_pdf") {
		t.Fatalf("empty search unlocked pdf: %v", names)
	}
	cloudWebSearch = func(context.Context, string) string { return "北京今日晴" }
	if _, failed := h.runTool("web_search", `{"query":"北京天气"}`); failed || !h.searchDone {
		t.Fatal("successful search was treated as a failure")
	}
	names = cloudToolNames(h.BuildTools("北京天气，生成pdf"))
	if !cloudHasName(names, "generate_pdf") || cloudHasName(names, "bash") {
		t.Fatalf("tools after search = %v", names)
	}
	prompt = h.BuildSystemPrompt("北京天气，生成pdf", false)
	if !strings.Contains(prompt, "调用 generate_pdf") || strings.Contains(prompt, "检索成功之后") {
		t.Fatalf("ready prompt = %s", prompt)
	}
	cloudWebSearch = func(context.Context, string) string {
		return "找到 1 条结果:\n\n1. 天气\n   https://example.com/w\n\n部分搜索引擎不可用，已使用浏览器兜底。" + cloudSearchBrowserSteer + "\n"
	}
	out, failed = h.runTool("web_search", `{"query":"北京天气"}`)
	if failed || strings.Contains(out, "browser") || strings.Contains(out, "浏览器") || !strings.Contains(out, "web_fetch") || !strings.Contains(out, "已改用备用搜索") {
		t.Fatalf("search steer = %q failed=%v", out, failed)
	}
	searchOnly := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs:   []coretool.CapabilityNeed{{Capability: agentservice.CapabilityInformationSearchWeb}},
		Classification: intent.ClassificationResult{
			Primary:    intent.LabelSearch,
			Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
			Confidence: 0.95,
		},
	}})
	searchOnlyNames := cloudToolNames(searchOnly.BuildTools("查资料并生成pdf"))
	if !cloudHasName(searchOnlyNames, "web_search") || cloudHasName(searchOnlyNames, "web_fetch") {
		t.Fatalf("search-only tools = %v", searchOnlyNames)
	}
	cloudWebSearch = func(context.Context, string) string {
		return "找到 1 条结果:\n\n部分搜索引擎不可用，" + cloudSearchBrowserFallback + "。" + cloudSearchBrowserSteer
	}
	out, failed = searchOnly.runTool("web_search", `{"query":"资料"}`)
	if failed || strings.Contains(out, "browser") || strings.Contains(out, "浏览器") || strings.Contains(out, "web_fetch") || !strings.Contains(out, "根据已返回的结果继续") || !strings.Contains(out, "已改用备用搜索") {
		t.Fatalf("search-only steer = %q failed=%v", out, failed)
	}
	line := cloudToolStatusLine("web_search", `{"query":"北京天气"}`)
	if !strings.Contains(line, "检索网页") || !strings.Contains(line, "北京天气") {
		t.Fatalf("status = %q", line)
	}
	event := map[string]any{}
	cloudToolEventArgs(event, "web_search", `{"query":"北京天气"}`)
	if event["command"] != "北京天气" {
		t.Fatalf("event = %+v", event)
	}

	stuck := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs:   []coretool.CapabilityNeed{{Capability: agentservice.CapabilityInformationSearchWeb}},
		Classification: intent.ClassificationResult{
			Primary:    intent.LabelLiveData,
			Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
			Confidence: 0.95,
		},
	}})
	stuck.BuildTools("北京天气，生成pdf")
	prevSearch := cloudWebSearch
	prevFetch := cloudWebFetch
	t.Cleanup(func() {
		cloudWebSearch = prevSearch
		cloudWebFetch = prevFetch
	})
	cloudWebSearch = func(context.Context, string) string { return "搜索失败: down" }
	if _, failed := stuck.runTool("web_search", `{"query":"北京天气"}`); !failed || stuck.searchDone {
		t.Fatal("failed search was treated as success on the fetch-unlock host")
	}
	if out, failed := stuck.runTool("generate_pdf", `{"content":"# 天气"}`); !failed || !strings.Contains(out, "先调用 web_fetch") {
		t.Fatalf("denial after failed search = %q failed=%v", out, failed)
	}
	var offsets []int
	cloudWebFetch = func(_ context.Context, rawURL string, offset int) string {
		offsets = append(offsets, offset)
		if rawURL == "" {
			return "抓取失败: missing"
		}
		if len(offsets) == 1 {
			return "抓取失败: down"
		}
		return `{"title":"weather","content":"北京今日晴"}`
	}
	if _, failed := stuck.runTool("web_fetch", `{"url":"https://example.com/weather","offset":-3}`); !failed || stuck.fetchDone {
		t.Fatal("failed fetch was treated as success")
	}
	if _, failed := stuck.runTool("web_fetch", `{"url":"https://example.com/weather","offset":"40"}`); failed || !stuck.fetchDone {
		t.Fatal("successful fetch was treated as a failure")
	}
	if _, failed := stuck.runTool("web_fetch", `{"url":"https://example.com/weather","offset":"1e20"}`); failed {
		t.Fatal("clamped offset was treated as a failure")
	}
	if len(offsets) != 3 || offsets[0] != 0 || offsets[1] != 40 || offsets[2] != cloudMaxJSONInt {
		t.Fatalf("offsets = %v", offsets)
	}
	if got := cloudJSONInt(`{"offset":" 40.9 "}`, "offset"); got != 40 {
		t.Fatalf("fractional string offset = %d", got)
	}
	if got := cloudJSONInt(`{"offset":"-3"}`, "offset"); got != 0 {
		t.Fatalf("negative string offset = %d", got)
	}
	if got := cloudJSONInt(`{"offset":"next"}`, "offset"); got != 0 {
		t.Fatalf("non-numeric offset = %d", got)
	}
	names = cloudToolNames(stuck.BuildTools("北京天气，生成pdf"))
	if !cloudHasName(names, "generate_pdf") || stuck.searchDone || cloudHasName(names, "bash") {
		t.Fatalf("tools after fetch with failed search = %v searchDone=%v", names, stuck.searchDone)
	}
	prompt = stuck.BuildSystemPrompt("北京天气，生成pdf", false)
	if !strings.Contains(prompt, "调用 generate_pdf") || strings.Contains(prompt, "检索成功之后") {
		t.Fatalf("fetch-ready prompt = %s", prompt)
	}
}

func TestCloudRoutedWorkStopNudgeNamesSearchThenPDF(t *testing.T) {
	weather := agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs: []coretool.CapabilityNeed{
			{Capability: agentservice.CapabilityInformationSearchWeb},
			{Capability: agentservice.CapabilityWebFetch},
		},
		Classification: intent.ClassificationResult{
			Primary:    intent.LabelLiveData,
			Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
			Confidence: 0.95,
		},
	}
	h := cloudSemanticHost(t, false, &staticSessionNeeds{res: weather})
	h.BuildTools("北京天气，生成pdf")
	first := h.RoutedWorkStopNudge()
	second := h.RoutedWorkStopNudge()
	for i, got := range []string{first, second} {
		if !strings.Contains(got, "web_search") || strings.Contains(got, "generate_pdf") {
			t.Fatalf("retrieval nudge %d = %q", i, got)
		}
	}
	if got := h.RoutedWorkStopNudge(); got != "" {
		t.Fatalf("third retrieval nudge = %q", got)
	}
	if got := h.UnresolvedRoutedWork(); !strings.Contains(got, "web_search") || !strings.Contains(got, "PDF") {
		t.Fatalf("unresolved search = %q", got)
	}
	h.searchDone = true
	if got := h.UnresolvedRoutedWork(); got != "" {
		t.Fatalf("pdf reminders still open: %q", got)
	}
	pdf := h.RoutedWorkStopNudge()
	if !strings.Contains(pdf, "generate_pdf") || !strings.Contains(pdf, "检索到的事实") || strings.Contains(pdf, "web_search") {
		t.Fatalf("pdf nudge = %q", pdf)
	}
	if got := h.RoutedWorkStopNudge(); !strings.Contains(got, "generate_pdf") {
		t.Fatalf("second pdf nudge = %q", got)
	}
	if got := h.RoutedWorkStopNudge(); got != "" {
		t.Fatalf("third pdf nudge = %q", got)
	}
	if got := h.UnresolvedRoutedWork(); !strings.Contains(got, "generate_pdf") {
		t.Fatalf("unresolved pdf = %q", got)
	}

	spent := cloudSemanticHost(t, false, &staticSessionNeeds{res: weather})
	spent.BuildTools("北京天气，生成pdf")
	spent.RoutedWorkStopNudge()
	spent.RoutedWorkStopNudge()
	spent.searchAttempted = true
	if got := spent.RoutedWorkStopNudge(); !strings.Contains(got, "web_fetch") || strings.Contains(got, "web_search") {
		t.Fatalf("fetch after spent search nudges = %q", got)
	}

	tried := cloudSemanticHost(t, false, &staticSessionNeeds{res: weather})
	tried.BuildTools("北京天气，生成pdf")
	tried.searchAttempted = true
	if got := tried.RoutedWorkStopNudge(); !strings.Contains(got, "web_fetch") || strings.Contains(got, "web_search") {
		t.Fatalf("after a failed search = %q", got)
	}
	tried.fetchAttempted = true
	if got := tried.RoutedWorkStopNudge(); got != "" {
		t.Fatalf("both lookups already ran: %q", got)
	}
	if got := tried.UnresolvedRoutedWork(); got != "" {
		t.Fatalf("failed lookups are not a missing call: %q", got)
	}

	spentCall := cloudSemanticHost(t, false, &staticSessionNeeds{res: weather})
	spentCall.BuildTools("北京天气，生成pdf")
	spentCall.RoutedWorkStopNudge()
	spentCall.RoutedWorkStopNudge()
	spentCall.searchAttempted = true
	spentCall.fetchAttempted = true
	if got := spentCall.UnresolvedRoutedWork(); got != "" {
		t.Fatalf("spent reminders after a real lookup: %q", got)
	}

	done := cloudSemanticHost(t, false, &staticSessionNeeds{res: weather})
	done.BuildTools("北京天气，生成pdf")
	done.searchDone = true
	done.pdfDone = true
	if got := done.RoutedWorkStopNudge(); got != "" {
		t.Fatalf("finished pdf nudge = %q", got)
	}

	searchOnly := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs:   []coretool.CapabilityNeed{{Capability: agentservice.CapabilityInformationSearchWeb}},
		Classification: intent.ClassificationResult{
			Primary:    intent.LabelLiveData,
			Confidence: 0.95,
		},
	}})
	searchOnly.BuildTools("北京天气")
	searchOnly.searchDone = true
	if got := searchOnly.RoutedWorkStopNudge(); got != "" {
		t.Fatalf("search-only after a hit = %q", got)
	}

	// LabelSearch does not add web_fetch. A search that already ran and
	// failed must not be described as a call that never happened, even
	// after its reminders are spent.
	failedSearch := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs:   []coretool.CapabilityNeed{{Capability: agentservice.CapabilityInformationSearchWeb}},
		Classification: intent.ClassificationResult{
			Primary:    intent.LabelSearch,
			Confidence: 0.95,
		},
	}})
	failedSearch.BuildTools("北京天气")
	failedSearch.RoutedWorkStopNudge()
	failedSearch.RoutedWorkStopNudge()
	failedSearch.searchAttempted = true
	if got := failedSearch.RoutedWorkStopNudge(); got != "" || failedSearch.UnresolvedRoutedWork() != "" {
		t.Fatalf("failed search nudge=%q unresolved=%q", got, failedSearch.UnresolvedRoutedWork())
	}

	document := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed:        true,
		Needs:          []coretool.CapabilityNeed{{Capability: agentservice.CapabilityDocumentGenerate}},
		Classification: intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.95},
	}})
	document.BuildTools("生成pdf")
	if got := document.RoutedWorkStopNudge(); !strings.Contains(got, "generate_pdf") || strings.Contains(got, "检索到的事实") {
		t.Fatalf("document nudge = %q", got)
	}
	prevRender := cloudRenderPDF
	cloudRenderPDF = func(string, string) ([]byte, error) { return nil, errors.New("render down") }
	t.Cleanup(func() { cloudRenderPDF = prevRender })
	if out, failed := document.runTool("generate_pdf", `{"title":"天气","content":"# 天气"}`); !failed || document.pdfDone || !document.pdfAttempted || !strings.Contains(out, "render down") {
		t.Fatalf("failed render = %q failed=%v done=%v attempted=%v", out, failed, document.pdfDone, document.pdfAttempted)
	}
	if got := document.RoutedWorkStopNudge(); got != "" || document.UnresolvedRoutedWork() != "" {
		t.Fatalf("failed render stays the model's report nudge=%q unresolved=%q", got, document.UnresolvedRoutedWork())
	}
	cloudRenderPDF = func(string, string) ([]byte, error) { return []byte("%PDF-1.4 ok"), nil }
	if out, failed := document.runTool("generate_pdf", `{"title":"天气","content":"# 天气"}`); failed || !document.pdfDone || !strings.Contains(out, "已生成 PDF") {
		t.Fatalf("written render = %q failed=%v done=%v", out, failed, document.pdfDone)
	}
	if got := document.RoutedWorkStopNudge(); got != "" || document.UnresolvedRoutedWork() != "" {
		t.Fatalf("after a written pdf nudge=%q unresolved=%q", got, document.UnresolvedRoutedWork())
	}

	missedPDF := cloudSemanticHost(t, false, &staticSessionNeeds{res: weather})
	missedPDF.BuildTools("北京天气，生成pdf")
	missedPDF.searchDone = true
	missedPDF.RoutedWorkStopNudge()
	missedPDF.RoutedWorkStopNudge()
	if got := missedPDF.UnresolvedRoutedWork(); !strings.Contains(got, "没有调用 generate_pdf") {
		t.Fatalf("pdf never called = %q", got)
	}
	if out, failed := missedPDF.runTool("generate_pdf", `{"content":""}`); !failed || missedPDF.pdfAttempted || !strings.Contains(out, "缺少 content") {
		t.Fatalf("empty pdf = %q failed=%v attempted=%v", out, failed, missedPDF.pdfAttempted)
	}
	if got := missedPDF.UnresolvedRoutedWork(); !strings.Contains(got, "没有调用 generate_pdf") {
		t.Fatalf("empty content is not a render attempt = %q", got)
	}
	cloudRenderPDF = func(string, string) ([]byte, error) { return nil, errors.New("render down") }
	if out, failed := missedPDF.runTool("generate_pdf", `{"title":"天气","content":"# 天气"}`); !failed || missedPDF.pdfDone || !missedPDF.pdfAttempted || !strings.Contains(out, "render down") {
		t.Fatalf("spent-nudge failed render = %q failed=%v done=%v attempted=%v", out, failed, missedPDF.pdfDone, missedPDF.pdfAttempted)
	}
	if got := missedPDF.RoutedWorkStopNudge(); got != "" || missedPDF.UnresolvedRoutedWork() != "" {
		t.Fatalf("called pdf is not a missing call nudge=%q unresolved=%q", got, missedPDF.UnresolvedRoutedWork())
	}

	idle := cloudSemanticHost(t, false, &staticSessionNeeds{})
	idle.BuildTools("你好")
	if got := idle.RoutedWorkStopNudge(); got != "" {
		t.Fatalf("workspace nudge = %q", got)
	}
}

func TestDefaultCloudRenderPDFRendersChinese(t *testing.T) {
	t.Setenv("MACLAW_FONT_DOWNLOAD", "off")
	pdf, err := defaultCloudRenderPDF("北京天气", "# 北京天气\n\n晴，3°C。")
	if err != nil {
		t.Fatal(err)
	}
	if len(pdf) < 5 || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("not a pdf (%d bytes)", len(pdf))
	}
}

func TestCloudSemanticRouteWithholdsPDFUntilFetch(t *testing.T) {
	h := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Classification: intent.ClassificationResult{
			Primary:    intent.LabelWebFetch,
			Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
			Confidence: 0.95,
		},
	}})
	names := cloudToolNames(h.BuildTools("打开这个页面并生成pdf"))
	if len(names) != 1 || names[0] != "web_fetch" {
		t.Fatalf("fetch tools = %v", names)
	}
	if out, failed := h.runTool("generate_pdf", `{"content":"# 页"}`); !failed || !strings.Contains(out, "先调用 web_fetch") {
		t.Fatalf("early pdf = %q failed=%v", out, failed)
	}
	prev := cloudWebFetch
	t.Cleanup(func() { cloudWebFetch = prev })
	cloudWebFetch = func(context.Context, string, int) string { return "抓取失败: down" }
	if _, failed := h.runTool("web_fetch", `{"url":"https://example.com"}`); !failed || h.fetchDone {
		t.Fatal("failed fetch was treated as success")
	}
	cloudWebFetch = func(context.Context, string, int) string {
		return `{"title":"empty","content":" \n "}`
	}
	if _, failed := h.runTool("web_fetch", `{"url":"https://example.com"}`); failed || h.fetchDone {
		t.Fatal("empty page was treated as a failed call or unlocked pdf")
	}
	names = cloudToolNames(h.BuildTools("打开这个页面并生成pdf"))
	if cloudHasName(names, "generate_pdf") {
		t.Fatalf("empty page unlocked pdf: %v", names)
	}
	names = cloudToolNames(h.BuildTools("打开这个页面并生成pdf"))
	if cloudHasName(names, "generate_pdf") {
		t.Fatalf("failed fetch unlocked pdf: %v", names)
	}
	cloudWebFetch = func(context.Context, string, int) string { return `{"title":"example","content":"正文"}` }
	if _, failed := h.runTool("web_fetch", `{"url":"https://example.com"}`); failed || !h.fetchDone {
		t.Fatal("successful fetch was treated as a failure")
	}
	names = cloudToolNames(h.BuildTools("打开这个页面并生成pdf"))
	if len(names) != 2 || names[0] != "web_fetch" || names[1] != "generate_pdf" {
		t.Fatalf("tools after fetch = %v", names)
	}
}

func TestCloudSemanticRouteWorkflowPhaseKeepsWorkspaceTools(t *testing.T) {
	resolver := &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs: []coretool.CapabilityNeed{
			{Capability: agentservice.CapabilityDocumentGenerate},
			{Capability: agentservice.CapabilityArtifactDeliverCurrent},
		},
		Classification: intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.95},
	}}
	h := cloudSemanticHost(t, false, resolver)
	h.phasePrompt = "用 write_file 写入本阶段文档，需要时用 bash 读取用户给出的文件。"
	names := cloudToolNames(h.BuildTools("请现在生成「交底书」阶段的完整文档内容。"))
	if !cloudHasName(names, "bash") || !cloudHasName(names, "read_file") || !cloudHasName(names, "write_file") || cloudHasName(names, "generate_pdf") {
		t.Fatalf("workflow tools = %v", names)
	}
	if resolver.calls != 0 {
		t.Fatalf("workflow phase consulted the semantic resolver: %d", resolver.calls)
	}
	prompt := h.BuildSystemPrompt("请现在生成「交底书」阶段的完整文档内容。", false)
	if !strings.Contains(prompt, "运行命令用 bash") || !strings.Contains(prompt, h.phasePrompt) || strings.Contains(prompt, "只使用列出的工具") {
		t.Fatalf("workflow prompt = %s", prompt)
	}
}

func TestCloudSemanticRouteKeepsWorkspaceTools(t *testing.T) {
	cases := []struct {
		name    string
		coding  bool
		resolve sessionNeedResolver
		wantGit bool
	}{
		{name: "nil resolver", coding: false},
		{name: "unmanaged", coding: false, resolve: &staticSessionNeeds{}},
		{name: "resolver error", coding: false, resolve: &staticSessionNeeds{err: errors.New("embedder down")}},
		{name: "browser only", coding: false, resolve: &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
			Managed: true,
			Needs:   []coretool.CapabilityNeed{{Capability: agentservice.CapabilityBrowserControl}},
		}}},
		{name: "coding unmanaged", coding: true, wantGit: true, resolve: &staticSessionNeeds{}},
		{name: "coverage gap", coding: false, resolve: &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{Managed: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := cloudSemanticHost(t, tc.coding, tc.resolve)
			names := cloudToolNames(h.BuildTools("生成pdf"))
			if !cloudHasName(names, "bash") || !cloudHasName(names, "read_file") || !cloudHasName(names, "write_file") {
				t.Fatalf("workspace tools = %v", names)
			}
			if cloudHasName(names, "generate_pdf") || cloudHasName(names, "web_search") {
				t.Fatalf("semantic tools leaked: %v", names)
			}
			if tc.wantGit != cloudHasName(names, "git_commit") || tc.wantGit != cloudHasName(names, "git_push") {
				t.Fatalf("git tools = %v wantGit=%v", names, tc.wantGit)
			}
			prompt := h.BuildSystemPrompt("生成pdf", false)
			if !tc.coding && !strings.Contains(prompt, "运行命令用 bash") {
				t.Fatalf("workspace prompt = %s", prompt)
			}
		})
	}
	blank := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs:   []coretool.CapabilityNeed{{Capability: agentservice.CapabilityDocumentGenerate}},
	}})
	blank.inst.meta.TenantID = ""
	if cloudHasName(cloudToolNames(blank.BuildTools("生成pdf")), "generate_pdf") {
		t.Fatal("blank tenant resolved a semantic surface")
	}
}

func TestCloudSemanticRouteFetchStaysPublic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cases := []struct {
		url    string
		marker string
	}{
		{url: "http://127.0.0.1/", marker: "not public"},
		{url: "http://[::1]/", marker: "not public"},
		{url: "http://169.254.169.254/latest/meta-data/", marker: "not public"},
		{url: "http://10.1.2.3/", marker: "not public"},
		{url: "http://192.168.1.1/", marker: "not public"},
		{url: "http://user:secret@example.com/report", marker: "credentials"},
		{url: "file:///etc/passwd", marker: "HTTP(S)"},
	}
	for _, tc := range cases {
		out := cloudWebFetch(ctx, tc.url, 0)
		if !strings.HasPrefix(strings.TrimSpace(out), "抓取失败") || !strings.Contains(out, tc.marker) {
			t.Fatalf("%s → %q", tc.url, out)
		}
	}

	h := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Classification: intent.ClassificationResult{
			Primary:    intent.LabelWebFetch,
			Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
			Confidence: 0.95,
		},
	}})
	// The checks above share a short deadline. A cancelled turn must not be
	// what this call proves: the metadata address itself has to be refused.
	turn, turnCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer turnCancel()
	h.turn = turn
	h.BuildTools("打开这个页面并生成pdf")
	out, failed := h.runTool("web_fetch", `{"url":"http://169.254.169.254/latest/meta-data/"}`)
	names := cloudToolNames(h.BuildTools("打开这个页面并生成pdf"))
	if !failed || h.fetchDone || cloudHasName(names, "generate_pdf") || !strings.Contains(out, "not public") {
		t.Fatalf("metadata fetch unlocked pdf: %q failed=%v done=%v tools=%v", out, failed, h.fetchDone, names)
	}
}

func TestCloudSemanticRouteFetchPageStaysInBudget(t *testing.T) {
	content := strings.Repeat("天气", 20000)
	longPage := &websearch.FetchResult{
		Title:      "北京天气",
		Content:    content,
		TotalChars: utf8.RuneCountInString(content),
		NextOffset: utf8.RuneCountInString(content),
	}
	out := cloudProjectFetchedPage("https://example.com/weather", longPage)
	if len(out) > agent.WebFetchMaxToolResult {
		t.Fatalf("long page len = %d", len(out))
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatalf("long page is not json: %v\n%s", err, out)
	}
	if body["has_more"] != true {
		t.Fatalf("long page has_more = %#v", body["has_more"])
	}
	next, ok := body["next_offset"].(float64)
	if !ok || next <= 0 || int(next) >= longPage.TotalChars {
		t.Fatalf("long page next_offset = %#v total=%d", body["next_offset"], longPage.TotalChars)
	}
	if !strings.Contains(body["content"].(string), "天气") {
		t.Fatal("long page dropped the body")
	}

	shortPage := &websearch.FetchResult{Title: "短", Content: "晴", TotalChars: 1, NextOffset: 1}
	short := cloudProjectFetchedPage("https://example.com/short", shortPage)
	var shortBody map[string]any
	if err := json.Unmarshal([]byte(short), &shortBody); err != nil {
		t.Fatal(err)
	}
	if _, ok := shortBody["has_more"]; ok || shortBody["content"] != "晴" {
		t.Fatalf("short page = %s", short)
	}

	clipped := &websearch.FetchResult{
		Title:           "短",
		Content:         "晴",
		TotalChars:      1,
		NextOffset:      1,
		ResponseClipped: true,
	}
	clippedOut := cloudProjectFetchedPage("https://example.com/short", clipped)
	var clippedBody map[string]any
	if err := json.Unmarshal([]byte(clippedOut), &clippedBody); err != nil {
		t.Fatal(err)
	}
	if clippedBody["response_clipped"] != true || clippedBody["note"] != websearch.ResponseClippedNote {
		t.Fatalf("clipped page = %s", clippedOut)
	}
	continuing := *clipped
	continuing.HasMore = true
	continuing.NextOffset = 1
	continuingOut := cloudProjectFetchedPage("https://example.com/short", &continuing)
	if strings.Contains(continuingOut, "response_clipped") || strings.Contains(continuingOut, websearch.ResponseClippedNote) {
		t.Fatalf("middle page named the clip: %s", continuingOut)
	}

	empty := cloudProjectFetchedPage("https://example.com/empty", &websearch.FetchResult{Title: "空", Content: "  "})
	if !strings.Contains(empty, "页面没有可读正文") {
		t.Fatalf("empty page = %s", empty)
	}
	longTitle := &websearch.FetchResult{Title: strings.Repeat("题", 300), Content: "晴", TotalChars: 1, NextOffset: 1}
	titled := cloudProjectFetchedPage("https://example.com/t", longTitle)
	var titledBody map[string]any
	if err := json.Unmarshal([]byte(titled), &titledBody); err != nil {
		t.Fatal(err)
	}
	if got := []rune(titledBody["title"].(string)); len(got) != 200 {
		t.Fatalf("title runes = %d", len(got))
	}
}

func TestCloudSemanticRouteGeneratePDFStaysInJail(t *testing.T) {
	h := cloudSemanticHost(t, false, &staticSessionNeeds{res: agentservice.DynamicCapabilityNeedResolution{
		Managed: true,
		Needs:   []coretool.CapabilityNeed{{Capability: agentservice.CapabilityDocumentGenerate}},
	}})
	var renderedTitle, renderedContent string
	var renders int
	prev := cloudRenderPDF
	cloudRenderPDF = func(title, content string) ([]byte, error) {
		renders++
		renderedTitle = title
		renderedContent = content
		return []byte("%PDF-1.4 stub"), nil
	}
	t.Cleanup(func() { cloudRenderPDF = prev })

	h.BuildTools("生成pdf")
	if out, failed := h.runTool("generate_pdf", `{"title":"天气","content":"   "}`); !failed || renders != 0 || !strings.Contains(out, "缺少 content") {
		t.Fatalf("empty content = %q failed=%v renders=%d", out, failed, renders)
	}
	out, failed := h.runTool("generate_pdf", `{"title":"../outside","content":"# 北京天气\n\n晴"}`)
	if failed || renders != 1 || renderedTitle != "outside" || !strings.Contains(renderedContent, "北京天气") {
		t.Fatalf("generate = %q failed=%v title=%q content=%q renders=%d", out, failed, renderedTitle, renderedContent, renders)
	}
	if !strings.Contains(out, "已生成 PDF：outside.pdf") {
		t.Fatalf("result = %q", out)
	}
	rel := "outside.pdf"
	body, err := os.ReadFile(filepath.Join(h.inst.dir, "workspace", rel))
	if err != nil || string(body) != "%PDF-1.4 stub" {
		t.Fatalf("jail pdf err=%v body=%q", err, body)
	}
	if _, err := os.Stat(filepath.Join(h.inst.dir, "outside.pdf")); !os.IsNotExist(err) {
		t.Fatalf("pdf escaped the jail: %v", err)
	}
	files := h.producedFiles()
	if len(files) != 1 || files[0] != rel {
		t.Fatalf("produced = %v", files)
	}
	cloudRenderPDF = func(string, string) ([]byte, error) { return []byte("not a pdf"), nil }
	if out, failed = h.runTool("generate_pdf", `{"title":"bad","content":"# 坏"}`); !failed || !strings.Contains(out, "不是 PDF") {
		t.Fatalf("non-pdf = %q failed=%v", out, failed)
	}
	oversize := bytes.Repeat([]byte("%PDF"), int(serveFileMaxBytes)/4+1)
	cloudRenderPDF = func(string, string) ([]byte, error) { return oversize, nil }
	if out, failed = h.runTool("generate_pdf", `{"title":"huge","content":"# 大"}`); !failed || !strings.Contains(out, "回传上限") || strings.Contains(out, "已生成") {
		t.Fatalf("oversize = %q failed=%v", out, failed)
	}
	for _, name := range []string{"bad.pdf", "huge.pdf"} {
		if _, err := os.Stat(filepath.Join(h.inst.dir, "workspace", name)); !os.IsNotExist(err) {
			t.Fatalf("%s was written: %v", name, err)
		}
	}
	files = h.producedFiles()
	if len(files) != 1 || files[0] != rel {
		t.Fatalf("produced after rejected renders = %v", files)
	}
	line := cloudToolStatusLine("generate_pdf", `{"title":"北京天气"}`)
	if !strings.Contains(line, "生成 PDF") || !strings.Contains(line, "北京天气") {
		t.Fatalf("status = %q", line)
	}
}
