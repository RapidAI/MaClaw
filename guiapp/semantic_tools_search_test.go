package guiapp

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The discovery meta-tool must ride every governed surface render, so the
// model always has a deterministic path from "tool missing" to an exact
// petitionable name. The production loop additionally passes the render
// through the authorizer filter and the per-call intake gate; both must
// admit tools_search even though it carries no grant.
func TestSemanticToolsSearchIsRenderedOnGovernedSurface(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	defs := agent.FilterToolDefinitionsByAuthorizer(cb, cb.BuildToolsForModelRequest("生成生日PPT", 1))
	found := false
	for _, def := range defs {
		if extractToolName(def) == semanticToolsSearchName {
			found = true
		}
	}
	if !found {
		t.Fatalf("governed surface must render %s through the authorizer filter: %#v", semanticToolsSearchName, defs)
	}
	if !cb.IsToolAllowed(semanticToolsSearchName) {
		t.Fatal("authorizer must admit tools_search")
	}
	if allowed, reason := cb.IsToolCallAllowed(semanticToolsSearchName, `{"query":"ppt"}`); !allowed {
		t.Fatalf("intake gate must admit tools_search: %s", reason)
	}
}

// A Chinese natural-language query must resolve to the exact stable names,
// marked with their live status: office is listed on the fixture surface,
// bash is a listed baseline workspace tool.
func TestSemanticToolsSearchFindsExactNames(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	got := semanticToolsSearchRun(cb, `{"query":"生成ppt并网上找照片"}`)
	for _, want := range []string{"office", "web_search"} {
		if !strings.Contains(got, want) {
			t.Fatalf("result must name %q: %s", want, got)
		}
	}
	if !strings.Contains(got, "office — "+"Write a spreadsheet") || !strings.Contains(got, "[已在当前工具面]") {
		t.Fatalf("listed office must carry the listed status: %s", got)
	}
	got = semanticToolsSearchRun(cb, `{"query":"运行脚本生成带图片的幻灯片"}`)
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- bash — ") && strings.Contains(line, "[已在当前工具面]") {
			t.Fatalf("office discovery must not list bash as a granted baseline: %s", line)
		}
	}
	// Discovery is not authorization: a query must not mint extra grants.
	before := len(cb.semanticSurface.grants)
	_ = semanticToolsSearchRun(cb, `{"query":"运行脚本生成带图片的幻灯片"}`)
	if len(cb.semanticSurface.grants) != before {
		t.Fatalf("discovery must not mint extra grants: before=%d after=%d", before, len(cb.semanticSurface.grants))
	}
	// Garbage arguments fail closed without touching the surface.
	if got := semanticToolsSearchRun(cb, `not json`); !strings.Contains(got, "tools_search_arguments_invalid") {
		t.Fatalf("garbage args: %s", got)
	}
	if got := semanticToolsSearchRun(cb, `{"query":"  "}`); !strings.Contains(got, "tools_search_query_required") {
		t.Fatalf("empty query: %s", got)
	}
}

// The execution entries must answer tools_search directly, without a grant
// and without burning one. Each entry is a fresh turn: a second identical
// directory on the same callback is a repeated lookup and closes discovery.
func TestSemanticToolsSearchExecutesThroughBothEntries(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	if got := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"pdf"}`); !strings.Contains(got, "generate_pdf") {
		t.Fatalf("executeSemanticTool: %s", got)
	}
	cb = petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	if got := cb.executeSemanticToolCallWithEpoch(semanticToolsSearchName, `{"query":"pdf"}`, "call-ts", ""); !strings.Contains(got, "generate_pdf") {
		t.Fatalf("executeSemanticToolCallWithEpoch: %s", got)
	}
}

// Discovery is a helper, not a way of life: after the turn budget the host
// answers with a deterministic redirect instead of more results, ending the
// burned-grant spiral observed in production. Distinct needs still answer,
// because each one reveals a row the turn has not seen. The counter is shared
// across both execution entries and never touches grants.
func TestSemanticToolsSearchTurnBudgetEndsSpiral(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	needs := []string{"web_search", "office", "generate_pdf", "download_file"}
	if len(needs) != semanticToolsSearchMaxPerTurn {
		t.Fatalf("fixture must cover the budget, got %d cap %d", len(needs), semanticToolsSearchMaxPerTurn)
	}
	for i, name := range needs {
		args := fmt.Sprintf(`{"query":"need %s","needs":[%q]}`, name, name)
		var got string
		if i%2 == 0 {
			got = cb.executeSemanticTool(semanticToolsSearchName, args)
		} else {
			got = cb.executeSemanticToolCallWithEpoch(semanticToolsSearchName, args, "call-ts", "")
		}
		if !strings.Contains(got, "- "+name+" — ") {
			t.Fatalf("call %d within budget must answer %s: %s", i+1, name, got)
		}
	}
	got := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"pdf","needs":["generate_pdf"]}`)
	if !strings.Contains(got, "no longer available") || strings.Contains(got, "generate_pdf") {
		t.Fatalf("over-budget call must redirect without results: %s", got)
	}
	again := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"ssh","needs":["ssh"]}`)
	if !strings.Contains(again, "reached its limit") || strings.Contains(again, "already returned") {
		t.Fatalf("a later call must stay on the budget message: %s", again)
	}
	if len(cb.semanticSurface.grants) == 0 {
		t.Fatal("discovery budget must not consume grants")
	}
}

// A lookup that only repeats rows already returned closes discovery at once.
// Waiting for the numeric cap let the 2026-09-27 save turn spend five model
// rounds re-reading the same petitionable line.
func TestSemanticToolsSearchRepeatedDirectoryCloses(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{
		Primary: intent.LabelOffice, Secondary: []intent.IntentLabel{intent.LabelWebFetch}, Confidence: .98,
	})
	cb.semanticSurface.grants["knowledge_save_text"] = tool.InvocationGrant{AdapterName: "knowledge_save_text"}
	first := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"save the endpoint","needs":["web_fetch","http_request","knowledge_save_text"]}`)
	if !strings.Contains(first, "- knowledge_save_text — ") || !strings.Contains(first, "[已在当前工具面]") {
		t.Fatalf("listed save tool dropped beside web_fetch: %s", first)
	}
	if !strings.Contains(first, "- web_fetch — ") || !strings.Contains(first, "[可请愿]") || strings.Contains(first, "直接调用一次") {
		t.Fatalf("petitionable fetch must stay eligible without a second call: %s", first)
	}
	if !strings.Contains(first, "立即调用已在当前工具面的名字：knowledge_save_text") {
		t.Fatalf("listed-name directive missing: %s", first)
	}
	if strings.LastIndex(first, "立即调用已在当前工具面的名字：knowledge_save_text") < strings.Index(first, "- knowledge_save_text — ") {
		t.Fatalf("directive must be the last action, after the rows: %s", first)
	}
	if strings.Contains(first, "查询文字本身不会挑选工具") {
		t.Fatalf("petition footer must not follow a listed-name directive: %s", first)
	}
	if strings.Contains(first, "- office — ") || strings.Contains(first, "- ssh — ") {
		t.Fatalf("unrequested tools leaked into the filtered directory: %s", first)
	}
	decisionAt := strings.Index(first, "本轮决定：")
	if decisionAt < 0 {
		t.Fatalf("planned delivery must stay visible beside the listed save: %s", first)
	}
	decisionLine := first[decisionAt:]
	if end := strings.IndexByte(decisionLine, '\n'); end >= 0 {
		decisionLine = decisionLine[:end]
	}
	if strings.Contains(decisionLine, "现在调用") || strings.Contains(decisionLine, "先调用") || strings.Contains(decisionLine, "它出现") {
		t.Fatalf("listed-name lookup must not add a second imperative: %s", decisionLine)
	}
	if !strings.Contains(decisionLine, "send_file（office 成功后，send_file 出现在下一请求）") {
		t.Fatalf("planned delivery must stay a fact, without a call: %s", decisionLine)
	}
	if last := semanticToolsSearchLastLine(first); !strings.HasPrefix(last, semanticToolsSearchListedDirectivePrefix) {
		t.Fatalf("directive must close the page: %q", last)
	}
	// A needs list is a petition, not a directory re-read: a first-time set
	// returns its page even when every row was listed before. Only an exact
	// repeat of an unanswered petition closes.
	second := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"fetch models","needs":["web_fetch"]}`)
	if !strings.Contains(second, "- web_fetch — ") {
		t.Fatalf("a new needs set must answer its petition page: %s", second)
	}
	repeat := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"fetch models again","needs":["web_fetch"]}`)
	if !strings.Contains(repeat, "already returned") || strings.Contains(repeat, "web_fetch —") {
		t.Fatalf("repeating the same needs petition must close: %s", repeat)
	}
	third := cb.executeSemanticToolCallWithEpoch(semanticToolsSearchName, `{"query":"bash curl","needs":["bash"]}`, "call-ts", "")
	if !strings.Contains(third, "- bash — ") {
		t.Fatalf("a new name must still answer after one repeated lookup: %s", third)
	}
}

// An unmatched needs token falls back to the whole directory. That dump must
// not count as "already returned" in a way that swallows a later exact lookup
// whose only new information is the call-this-name line.
func TestSemanticToolsSearchUnmatchedNeedsDoesNotSwallowListedDirective(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	cb.semanticSurface.grants["knowledge_save_text"] = tool.InvocationGrant{AdapterName: "knowledge_save_text"}
	first := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"http request","needs":["http_request"]}`)
	if !strings.Contains(first, "needs filter matched nothing") {
		t.Fatalf("unknown need must fall back to the directory: %s", first)
	}
	if strings.Contains(first, "不要告诉用户工具缺失") || strings.Contains(first, "现在调用") {
		t.Fatalf("unmatched need must not order the planned tool: %s", first)
	}
	if !strings.Contains(first, "所查的名字不在本回合目录里") {
		t.Fatalf("unmatched need must say the name is absent: %s", first)
	}
	second := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"save it","needs":["knowledge_save_text"]}`)
	if !strings.Contains(second, semanticToolsSearchListedDirectivePrefix+"knowledge_save_text") {
		t.Fatalf("exact lookup after a fallback dump must still direct the listed tool: %s", second)
	}
	other := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"office","needs":["office"]}`)
	if !strings.Contains(other, semanticToolsSearchListedDirectivePrefix+"office") {
		t.Fatalf("a different listed name must still be directed: %s", other)
	}
	third := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"save it again","needs":["knowledge_save_text"]}`)
	if !strings.Contains(third, "already returned") || strings.Contains(third, "knowledge_save_text —") {
		t.Fatalf("repeating the directed lookup must close: %s", third)
	}
}

// ssh's inventory capability id and its petition capability id differ. A needs
// filter that only compares capability ids drops the row whenever another
// requested name matches, which is how knowledge_save_text disappeared.
func TestSemanticToolsSearchNeedsKeepsNameWhenCapabilityIDDrifted(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	got := semanticToolsSearchRun(cb, `{"query":"ssh to the host","needs":["ssh","web_fetch"]}`)
	if !strings.Contains(got, "- ssh — ") || !strings.Contains(got, "- web_fetch — ") {
		t.Fatalf("drifted name was dropped from a mixed needs filter: %s", got)
	}
	if strings.Contains(got, "- office — ") {
		t.Fatalf("name match must stay narrow: %s", got)
	}
	got = semanticToolsSearchRun(cb, `{"query":"read a file","needs":["read_file"]}`)
	if !strings.Contains(got, "- read_file — ") || strings.Contains(got, "- list_directory — ") || strings.Contains(got, "- search_files — ") {
		t.Fatalf("a tool name must not expand to capability aliases: %s", got)
	}
	got = semanticToolsSearchRun(cb, `{"query":"read","needs":["fs.read.local"]}`)
	if !strings.Contains(got, "- read_file — ") || !strings.Contains(got, "- list_directory — ") {
		t.Fatalf("a capability id must still select every row of that capability: %s", got)
	}
}

// English discovery queries must hit the inventory too: a production turn
// asked "weather nanjing forecast" and got "(no matching capability)",
// blocking the petition self-rescue that the result would have suggested.
func TestSemanticToolsSearchMatchesEnglishQueries(t *testing.T) {
	cases := map[string]string{
		"weather nanjing forecast": "web_search",
		"find cat photos online":   "web_search",
		"wikipedia article":        "web_fetch",
		"download cat picture":     "download_file",
	}
	for query, want := range cases {
		cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
		got := cb.executeSemanticTool(semanticToolsSearchName, `{"query":`+strconv.Quote(query)+`}`)
		if !strings.Contains(got, want) {
			t.Fatalf("query %q must find %s: %s", query, want, got)
		}
	}
}

// The inventory's own names must be its strongest queries: a model that
// guesses the exact spelling ("office") and hears "(no matching capability)"
// concludes the tool does not exist and never petitions it — even when the
// plan scheduled that very capability (2026-08-27 birthday-deck turn: office
// was planned, the agent asked by name, panicked, and burned the discovery
// budget). Pin every entry against its own name so a keyword edit can never
// re-blind discovery.
func TestSemanticToolsSearchMatchesEveryEntryByItsOwnName(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	for _, entry := range semanticToolsSearchInventory {
		got := semanticToolsSearchRun(cb, `{"query":`+strconv.Quote(entry.name)+`}`)
		if !strings.Contains(got, "- "+entry.name+" — ") {
			t.Fatalf("entry %q must be found by its own name: %s", entry.name, got)
		}
	}
}

// Statuses must be honest about THIS turn. A petitionable name the turn never
// routed (office on a search-only surface) is "petitionable, call once" — the
// generalized petition gate really does admit it now, so discovery may say so;
// the old ambiguous "按计划路由提供" must never come back (it read as an
// invitation and the production model burned eight iterations calling office
// against the old gate's hard denial, 2026-08-27). A name whose class budget
// is spent must say so instead of inviting the call, and a legacy alias the
// managed catalog never renders (list_directory) stays plainly unavailable.
func TestSemanticToolsSearchStatusesAreHonestAboutThisTurn(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelOffice}, Confidence: .98,
	})
	got := semanticToolsSearchRun(cb, `{"query":"生成ppt幻灯片"}`)
	if !strings.Contains(got, "office") || !strings.Contains(got, "[可请愿：直接调用一次]") {
		t.Fatalf("directory office must be marked petitionable: %s", got)
	}
	if strings.Contains(got, "按计划路由提供") {
		t.Fatalf("ambiguous planned-status must be gone: %s", got)
	}
	got = semanticToolsSearchRun(cb, `{"query":"列目录"}`)
	if !strings.Contains(got, "list_directory") || !strings.Contains(got, "[本轮不可用：不要调用") {
		t.Fatalf("never-rendered legacy alias must be marked unavailable: %s", got)
	}
	cb.semanticEffectfulPetitionConsumed = true
	got = semanticToolsSearchRun(cb, `{"query":"运行脚本"}`)
	if !strings.Contains(got, "bash") || !strings.Contains(got, "[本轮请愿机会已用完，不要调用]") {
		t.Fatalf("unlisted bash must report the spent effectful petition, not a baseline listing: %s", got)
	}
	got = semanticToolsSearchRun(cb, `{"query":"生成ppt幻灯片"}`)
	if !strings.Contains(got, "[本轮请愿机会已用完，不要调用]") {
		t.Fatalf("spent effectful budget must be stated for office too: %s", got)
	}
}

// A name whose grant was retired must not be advertised as petitionable: the
// petition gate rejects retired names, so discovery inviting the call would
// send the model into the same hard denial twice.
func TestSemanticToolsSearchDoesNotMarkCapabilityAliasAsPlanned(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(0)
	cb.semanticSurface.plan.Selections = []tool.PlannedSelection{{
		AdapterName: "read_file",
		FitProof:    tool.FitProof{MatchedCapability: tool.CapabilityFSReadLocal},
	}}
	got := semanticToolsSearchRun(cb, "{\"scope_id\":\"scope-search\",\"catalog_digest\":\"catalog-search\",\"query\":\"list directory\"}")
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- list_directory — ") && strings.Contains(line, "[已列入本轮计划") {
			t.Fatalf("same-capability alias was falsely marked planned: %s", got)
		}
	}
	if !strings.Contains(got, "- list_directory — ") || !strings.Contains(got, "[本轮不可用：不要调用") {
		t.Fatalf("unselected alias must remain unavailable: %s", got)
	}
}
func TestSemanticToolsSearchUsesRenderedNameForDynamicGrant(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(0)
	cb.semanticSurface.plan.Selections = []tool.PlannedSelection{{
		ID: "dynamic-selection", AdapterName: "dynamic_mcp_internal",
		FitProof: tool.FitProof{MatchedCapability: "information.search.web"},
	}}
	cb.semanticSurface.schemas = map[string]map[string]interface{}{
		"dynamic_mcp_internal": {"type": "function", "function": map[string]interface{}{"name": "dynamic_mcp_internal", "description": "internal"}},
	}
	cb.semanticSurface.grants = map[string]tool.InvocationGrant{
		"invoke_opaque_token": {SelectionID: "dynamic-selection", AdapterName: "dynamic_mcp_internal"},
	}
	got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"search"}`)
	if !strings.Contains(got, "- invoke_opaque_token — ") || !strings.Contains(got, "[已在当前工具面]") {
		t.Fatalf("search omitted rendered dynamic grant name: %s", got)
	}
	if strings.Contains(got, "- dynamic_mcp_internal — ") {
		t.Fatalf("search leaked unrendered dynamic adapter name: %s", got)
	}
}

func TestSemanticToolsSearchNamesInstalledMCPBeforeItsGrant(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(0)
	adapter := "installed-mcp:tengyun__post_work_log_add:dynamic_mcp_abc"
	cb.semanticSurface.plan.Selections = []tool.PlannedSelection{{
		ID: "worklog", AdapterName: adapter,
		FitProof: tool.FitProof{MatchedCapability: "record.update.worklog"},
	}}
	if !semanticSurfacePlansTool(cb.semanticSurface, "tengyun__post_work_log_add") {
		t.Fatal("planned installed MCP tool must match its server__tool name before a grant")
	}
	planNames := semanticSurfacePlanNames(cb.semanticSurface)
	if !planNames["tengyun__post_work_log_add"] {
		t.Fatal("plan names omitted the installed MCP spelling")
	}
	if planNames[adapter] {
		t.Fatal("plan names kept the internal installed-mcp adapter")
	}
	if semanticSurfacePlansTool(cb.semanticSurface, "dynamic_mcp_abc") {
		t.Fatal("opaque dynamic adapter must stay unmatched")
	}
	catalog := semanticToolsSearchCatalog(cb)
	found := false
	for _, entry := range catalog {
		if entry.name == "tengyun__post_work_log_add" {
			found = true
		}
		if strings.Contains(entry.name, "dynamic_mcp") {
			t.Fatalf("catalog leaked an opaque adapter: %+v", entry)
		}
	}
	if !found {
		t.Fatal("catalog omitted the installed MCP name before its grant")
	}
	if status := semanticToolsSearchStatus(cb, "tengyun__post_work_log_add"); !strings.Contains(status, "已列入本轮计划") || strings.Contains(status, "可请愿") {
		t.Fatalf("status=%s", status)
	}
	cb.semanticSurface.schemas = map[string]map[string]interface{}{
		adapter: {"type": "function", "function": map[string]interface{}{
			"name": "dynamic_provider", "description": "Write one approved work-log row.",
		}},
		"installed-mcp:other__tool:dynamic_mcp_zzz": {"type": "function", "function": map[string]interface{}{
			"name": "dynamic_provider", "description": "Unplanned installed tool.",
		}},
	}
	catalog = semanticToolsSearchCatalog(cb)
	found = false
	for _, entry := range catalog {
		if entry.name == "tengyun__post_work_log_add" {
			found = true
			if entry.summary != "Write one approved work-log row." {
				t.Fatalf("summary=%q", entry.summary)
			}
		}
		if entry.name == "other__tool" || strings.HasPrefix(entry.name, "installed-mcp:") {
			t.Fatalf("schema advertised a tool the plan did not select: %+v", entry)
		}
	}
	if !found {
		t.Fatal("description enrich dropped the planned installed tool")
	}
}

func TestSemanticToolsSearchRetiredGrantIsNotPetitionable(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	name := "office"
	grant, ok := cb.semanticSurface.grants[name]
	if !ok {
		t.Fatalf("fixture must grant office: %#v", cb.semanticSurface.grants)
	}
	delete(cb.semanticSurface.grants, name)
	cb.semanticSurface.retiredGrants[name] = grant
	got := semanticToolsSearchRun(cb, `{"query":"ppt 幻灯片 office"}`)
	if !strings.Contains(got, "[本轮授权已用尽，不要调用]") {
		t.Fatalf("retired grant must be marked exhausted: %s", got)
	}
}

// Directory discovery is scope/catalog driven.  The explanatory query is
// echoed for the model, but changing it must not select a different set or
// ordering of entries.
func TestSemanticToolsSearchQueryDoesNotChangeCollection(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(12)
	left := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"weather in Nanjing"}`)
	right := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"unrelated nonsense"}`)
	if got, want := semanticSearchResultNames(left), semanticSearchResultNames(right); !equalStringSlices(got, want) {
		t.Fatalf("query changed directory collection: left=%v right=%v\nleft=%s\nright=%s", got, want, left, right)
	}
	if !strings.Contains(left, `tools_search results for "weather in Nanjing"`) || !strings.Contains(right, `tools_search results for "unrelated nonsense"`) {
		t.Fatalf("query should remain explanatory output: left=%s right=%s", left, right)
	}
}

// A scope-bound request must reject both a foreign scope and a foreign
// catalog generation.  Accepting either would let a continuation enumerate a
// different task's directory while retaining the caller's authorization.
func TestSemanticToolsSearchRejectsScopeAndCatalogMismatch(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(1)
	if got := semanticToolsSearchRun(cb, `{"scope_id":"scope-other","catalog_digest":"catalog-search","query":"list"}`); !strings.Contains(got, "tools_search_scope_mismatch") {
		t.Fatalf("foreign scope accepted: %s", got)
	}
	if got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-other","query":"list"}`); !strings.Contains(got, "tools_search_scope_mismatch") {
		t.Fatalf("foreign catalog accepted: %s", got)
	}
	// A copied header field is enough; the live surface supplies the other
	// half, and the supplied half is still verified. The production failure
	// this pins: a model that copied only scope_id or only catalog_digest was
	// told the identity was missing and could not re-read the surface.
	got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","query":"list"}`)
	if strings.Contains(got, "tools_search_scope_") || !strings.Contains(got, "dynamic_000") {
		t.Fatalf("partial scope identity was not host-bound: %s", got)
	}
	got = semanticToolsSearchRun(cb, `{"catalog_digest":"catalog-search","query":"list"}`)
	if strings.Contains(got, "tools_search_scope_") || !strings.Contains(got, "dynamic_000") {
		t.Fatalf("partial catalog identity was not host-bound: %s", got)
	}
	if got := semanticToolsSearchRun(cb, `{"scope_id":"scope-other","query":"list"}`); !strings.Contains(got, "tools_search_scope_mismatch") {
		t.Fatalf("foreign half accepted: %s", got)
	}
	if got := semanticToolsSearchRun(cb, `{"catalog_digest":"catalog-other","query":"list"}`); !strings.Contains(got, "tools_search_scope_mismatch") {
		t.Fatalf("foreign catalog half accepted: %s", got)
	}
	// The host-bound half also satisfies the empty-query directory read: the
	// caller proved it belongs to this surface, so a missing explanation is not
	// a reason to reject the page.
	got = semanticToolsSearchRun(cb, `{"scope_id":"scope-search"}`)
	if strings.Contains(got, "tools_search_query_required") || !strings.Contains(got, "dynamic_000") {
		t.Fatalf("partial identity did not bind an empty-query page: %s", got)
	}
}

func TestSemanticToolsSearchAllowsEmptyQueryOnBoundSurface(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(1)
	got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search"}`)
	if strings.Contains(got, "tools_search_query_required") || strings.Contains(got, "tools_search_scope_unavailable") {
		t.Fatalf("bound directory page with empty query was rejected: %s", got)
	}
	if !strings.Contains(got, "dynamic_000") {
		t.Fatalf("bound directory page omitted the catalog entry: %s", got)
	}
}

func TestSemanticToolsSearchNeedsNameDoesNotBlankTheDirectory(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: .98})
	for _, args := range []string{
		`{"query":"崇州天气","needs":["web_search"]}`,
		`{"query":"崇州天气","needs":["not-a-capability"]}`,
	} {
		got := semanticToolsSearchRun(cb, args)
		if strings.Contains(got, "no capability in this task scope") || !strings.Contains(got, "web_search") {
			t.Fatalf("needs filter erased the directory: args=%s result=%s", args, got)
		}
	}
	got := semanticToolsSearchRun(cb, `{"query":"崇州天气","needs":["information.search.web"]}`)
	if !strings.Contains(got, "web_search") || strings.Contains(got, "office — ") {
		t.Fatalf("a real capability filter must stay narrow: %s", got)
	}
	// The delete discovery call from production 2026-10-27 must resolve by
	// name and report that the call is reachable by petition, not that it
	// matched nothing or is permanently unavailable.
	got = semanticToolsSearchRun(cb, `{"query":"删除邮件 mailbox delete","needs":["mailbox_delete"]}`)
	if !strings.Contains(got, "- mailbox_delete — ") || !strings.Contains(got, "[本轮不可用") {
		t.Fatalf("mailbox_delete stays closed unless it is the primary: %s", got)
	}
}

func TestSemanticToolsSearchRejectsMalformedNeedsInsteadOfBroadening(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(1)
	for _, args := range []string{
		`{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"list","needs":"information.search.web"}`,
		`{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"list","needs":["information.search.web",7]}`,
		`{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"list","needs":[" "]}`,
	} {
		if got := semanticToolsSearchRun(cb, args); !strings.Contains(got, "tools_search_needs_invalid") {
			t.Fatalf("malformed needs broadened discovery: args=%s result=%s", args, got)
		}
	}
}

func TestSemanticToolsSearchRejectsDetachedScopeIdentity(t *testing.T) {
	for _, args := range []string{
		`{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"list"}`,
		`{"query":"list"}`,
	} {
		if got := semanticToolsSearchRun(nil, args); !strings.Contains(got, "tools_search_scope_unavailable") {
			t.Fatalf("detached discovery request was accepted: args=%s result=%s", args, got)
		}
	}
}

// Pagination is a closed ordered directory walk: pages are disjoint, cover
// every entry exactly once, and the continuation token cannot be replayed on a
// different scope/catalog.  The second query also proves that page boundaries
// are independent of wording.
func TestSemanticToolsSearchPaginationClosure(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(320)
	first := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"first"}`)
	token := semanticSearchNextPageToken(first)
	if token == "" || !strings.HasPrefix(token, "v1:") {
		t.Fatalf("page token is not scope-bound: %q\n%s", token, first)
	}
	second := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"second","page_token":"`+token+`"}`)
	if strings.Contains(second, "tools_search_page_token_invalid") {
		t.Fatalf("valid continuation rejected: %s", second)
	}
	left := semanticSearchAllPageNames(t, cb, "first")
	if len(left) != len(uniqueStrings(left)) {
		t.Fatalf("pagination overlaps entries: total=%d unique=%d", len(left), len(uniqueStrings(left)))
	}
	all := semanticToolsSearchCatalog(cb)
	if len(left) != len(all) {
		t.Fatalf("pagination did not cover catalog: got=%d catalog=%d", len(left), len(all))
	}
	// A token is bound to the scope and catalog, so copying it into another
	// surface must fail before reading the page offset.
	foreign := semanticToolsSearchRegressionCallbacks(320)
	foreign.semanticSurface.scopePlan.ScopeID = "scope-foreign"
	if got := semanticToolsSearchRun(foreign, `{"scope_id":"scope-foreign","catalog_digest":"catalog-search","query":"second","page_token":"`+token+`"}`); !strings.Contains(got, "tools_search_page_token_invalid") {
		t.Fatalf("continuation escaped its scope: %s", got)
	}
	// Replaying the same token with different explanatory text returns the same
	// page, proving query does not affect pagination closure.
	replay := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"third","page_token":"`+token+`"}`)
	if got, want := semanticSearchResultNames(second), semanticSearchResultNames(replay); !equalStringSlices(got, want) {
		t.Fatalf("query changed continuation page: got=%v want=%v", got, want)
	}
}

// The publication guard must reject a scope projection whose catalog identity
// was changed after planning.  This catches a class of stale-surface bugs that
// a name-only closure check cannot see.
func TestValidatePreparedSemanticToolScopeRejectsCatalogMismatch(t *testing.T) {
	prepared := &semanticPlanPreparation{
		plan:      tool.ToolPlan{ID: "plan-search", CatalogDigest: "catalog-current", CatalogGeneration: 4},
		scopePlan: tool.ToolScopePlan{ScopeID: "scope-search", CatalogDigest: "catalog-stale", CatalogGeneration: 3},
	}
	err := validatePreparedSemanticToolScope(prepared)
	if err == nil || !strings.Contains(err.Error(), "scope_catalog_digest_mismatch") {
		t.Fatalf("stale catalog projection accepted: %v", err)
	}
}

func semanticToolsSearchRegressionCallbacks(extra int) *sharedAgentLoopCallbacks {
	schemas := make(map[string]map[string]interface{}, extra)
	for i := 0; i < extra; i++ {
		name := fmt.Sprintf("dynamic_%03d", i)
		schemas[name] = map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        name,
				"description": "regression provider",
			},
		}
	}
	return &sharedAgentLoopCallbacks{
		semanticSurface: &semanticCallSurface{
			plan:          tool.ToolPlan{ID: "plan-search", CatalogDigest: "catalog-search", CatalogGeneration: 1},
			scopePlan:     tool.ToolScopePlan{ScopeID: "scope-search", CatalogDigest: "catalog-search", CatalogGeneration: 1},
			schemas:       schemas,
			grants:        make(map[string]tool.InvocationGrant),
			retiredGrants: make(map[string]tool.InvocationGrant),
		},
	}
}

func semanticSearchResultNames(result string) []string {
	names := make([]string, 0)
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if idx := strings.Index(name, " — "); idx >= 0 {
			name = name[:idx]
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// A weather-PDF turn plans generate_pdf behind web_search. Discovery must say
// that on the first page, ahead of the directory, and the page must fit the
// inline budget even when later rows are long. Projection then has nothing
// to cut out of the middle. Baseline grants stay directory rows.
func TestSemanticToolsSearchWeatherPDFDecisionSurvivesCatalogBulk(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(0)
	cb.semanticSurface.plan.Selections = []tool.PlannedSelection{
		{ID: "search", AdapterName: "web_search", FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		{ID: "pdf", AdapterName: "host_document_generate_file", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}, Requires: []string{"search"}},
		{ID: "deliver", AdapterName: "semantic_deliver_current_file", FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel"}, Requires: []string{"pdf"}},
	}
	cb.semanticSurface.grants["web_search"] = tool.InvocationGrant{SelectionID: "search", Token: "search-token", AdapterName: "web_search"}
	// Baseline workspace tools are granted on a managed turn. They are on the
	// surface, and they are not the predecessor of generate_pdf.
	for _, name := range []string{"bash", "read_file", "write_file"} {
		cb.semanticSurface.grants[name] = tool.InvocationGrant{SelectionID: "baseline-" + name, Token: name + "-token", AdapterName: name}
	}
	// Ungranted dynamic_mcp_ schemas never enter the directory. These names
	// are admitted display metadata and sort before generate_pdf, so a
	// missing builtin-first partition would push the planned row off page 0.
	long := strings.Repeat("vendor tool description that used to fill the preview ", 30)
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("dynamic_%03d", i)
		cb.semanticSurface.schemas[name] = map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": name, "description": long},
		}
	}
	got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"pdf office document"}`)
	if len(got) > agent.ToolsSearchMaxToolResult {
		t.Fatalf("page is %d bytes, budget %d", len(got), agent.ToolsSearchMaxToolResult)
	}
	decisionAt := strings.Index(got, "本轮决定：")
	rowAt := strings.Index(got, "- generate_pdf — ")
	decisionLine := got
	if decisionAt >= 0 {
		decisionLine = got[decisionAt:]
		if end := strings.IndexByte(decisionLine, '\n'); end >= 0 {
			decisionLine = decisionLine[:end]
		}
	}
	if decisionAt < 0 || !strings.Contains(decisionLine, "现在调用 web_search") || !strings.Contains(decisionLine, "generate_pdf（先调用 web_search") {
		t.Fatalf("decision must name the listed predecessor and the planned pdf: %s", got)
	}
	sendAt := strings.Index(decisionLine, "send_file（")
	if sendAt < 0 {
		t.Fatalf("decision must name planned delivery: %s", decisionLine)
	}
	sendClause := decisionLine[sendAt:]
	if end := strings.Index(sendClause, "）"); end >= 0 {
		sendClause = sendClause[:end]
	}
	if strings.Contains(sendClause, "它出现在下一请求") || !strings.Contains(sendClause, "generate_pdf 出现在下一请求") {
		t.Fatalf("delivery must wait for generate_pdf, not the lookup: %s", sendClause)
	}
	for _, baseline := range []string{"bash", "read_file", "write_file"} {
		if strings.Contains(decisionLine, baseline) {
			t.Fatalf("baseline %s is granted but is not the predecessor: %s", baseline, decisionLine)
		}
		row := "- " + baseline + " — "
		if !strings.Contains(got, row) {
			t.Fatalf("baseline %s must stay a directory row: %s", baseline, got)
		}
	}
	if strings.Contains(got, "不要再查目录") || !strings.Contains(got, "next_page_token=") {
		t.Fatalf("a partial page must keep its continuation and must not forbid another read: %s", got)
	}
	if rowAt < 0 || decisionAt > rowAt {
		t.Fatalf("decision must precede the planned row on the first page: %s", got)
	}
	if dyn := strings.Index(got, "- dynamic_"); dyn >= 0 && dyn < rowAt {
		t.Fatalf("dynamic rows sorted ahead of the planned builtin: %s", got)
	}
	if strings.Contains(got, "只有标记「可请愿」") || strings.Contains(got, "仍可直接调用一次") {
		t.Fatalf("petition invitation must not be the page action: %s", got)
	}
	if strings.Contains(got, "直接调用一次") || !strings.Contains(got, "[可请愿]") || !strings.Contains(decisionLine, "不要请愿") {
		t.Fatalf("petition stays a row label and is not a second call: %s", got)
	}
	if last := semanticToolsSearchLastLine(got); last != "现在调用 web_search。" {
		t.Fatalf("closing line = %q, want the same call as the decision", last)
	}
	for _, line := range strings.Split(got, "\n") {
		if (strings.HasPrefix(line, "- generate_pdf — ") || strings.HasPrefix(line, "- send_file — ")) && (strings.Contains(line, "[可请愿") || strings.Contains(line, "自动出现")) {
			t.Fatalf("planned successor row promised a petition or an automatic appearance: %s", line)
		}
	}
	proj, err := agent.ProjectToolResult("tools_search", "sess", got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proj.Preview, "本轮决定：") || !strings.Contains(proj.Preview, "generate_pdf（先调用 web_search") {
		t.Fatalf("projection dropped the decision: %s", proj.Preview)
	}
}

// After the lookup commits, generate_pdf is held for the next request and
// delivery is still behind it. Discovery must not send the model back to
// web_search, and must not say send_file is the name that appears next.
func TestSemanticToolsSearchHeldDeliveryDoesNotRestartSearch(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(0)
	cb.semanticHoldDependantIssue = true
	cb.semanticSurface.completed = map[string]bool{"search": true}
	cb.semanticSurface.plan.Selections = []tool.PlannedSelection{
		{ID: "search", AdapterName: "web_search", FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		{ID: "pdf", AdapterName: "host_document_generate_file", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}, Requires: []string{"search"}},
		{ID: "deliver", AdapterName: "semantic_deliver_current_file", FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel"}, Requires: []string{"pdf"}},
	}
	cb.semanticSurface.grants["web_search"] = tool.InvocationGrant{SelectionID: "search", Token: "search-token", AdapterName: "web_search"}
	got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"pdf"}`)
	decisionLine := got
	if at := strings.Index(got, "本轮决定："); at >= 0 {
		decisionLine = got[at:]
		if end := strings.IndexByte(decisionLine, '\n'); end >= 0 {
			decisionLine = decisionLine[:end]
		}
	}
	if strings.Contains(decisionLine, "现在调用") || strings.Contains(decisionLine, "web_search") {
		t.Fatalf("a finished lookup must not be the next call: %s", decisionLine)
	}
	if !strings.Contains(decisionLine, "generate_pdf（本响应的其它调用结束后出现在下一请求）") {
		t.Fatalf("held generate must stay the next name: %s", decisionLine)
	}
	sendAt := strings.Index(decisionLine, "send_file（")
	if sendAt < 0 {
		t.Fatalf("delivery clause missing: %s", decisionLine)
	}
	sendClause := decisionLine[sendAt:]
	if end := strings.Index(sendClause, "）"); end >= 0 {
		sendClause = sendClause[:end]
	}
	if strings.Contains(sendClause, "它出现在下一请求") || !strings.Contains(sendClause, "generate_pdf 出现在下一请求") {
		t.Fatalf("delivery must wait until generate_pdf is listed: %s", sendClause)
	}
	if strings.Contains(got, "仍可直接调用一次") || strings.Contains(got, "直接调用一次") || strings.Contains(got, "现在调用") {
		t.Fatalf("held page must not invite a petition or restart a call: %s", got)
	}
	const heldCloser = "下一请求出现：generate_pdf。现在不要调用。"
	if last := semanticToolsSearchLastLine(got); last != heldCloser {
		t.Fatalf("closing line = %q, want %s", last, heldCloser)
	}
}

// Grants with no unlisted plan step are already callable. The closing line
// must not order every grant: bash is one of those grants.
func TestSemanticToolsSearchListedSurfaceDoesNotOrderEveryGrant(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(0)
	for _, name := range []string{"bash", "web_search"} {
		cb.semanticSurface.grants[name] = tool.InvocationGrant{SelectionID: name, Token: name + "-token", AdapterName: name}
	}
	got := semanticToolsSearchRun(cb, `{"query":"天气"}`)
	decisionLine := ""
	if at := strings.Index(got, "本轮决定："); at >= 0 {
		decisionLine = got[at:]
		if end := strings.IndexByte(decisionLine, '\n'); end >= 0 {
			decisionLine = decisionLine[:end]
		}
	}
	if !strings.Contains(decisionLine, "已在当前工具面：") || !strings.Contains(decisionLine, "bash") || !strings.Contains(decisionLine, "web_search") {
		t.Fatalf("listed grants must stay in the decision: %s", decisionLine)
	}
	if strings.Contains(got, "现在调用") || strings.Contains(got, "仍可直接调用一次") || strings.Contains(got, "只有标记「可请愿」") {
		t.Fatalf("listed surface must not order a call or invite a petition: %s", got)
	}
	if !strings.Contains(got, "[可请愿：直接调用一次]") {
		t.Fatalf("with no planned step, a petitionable row must still admit one call: %s", got)
	}
	if last := semanticToolsSearchLastLine(got); last != semanticToolsSearchListedCloser {
		t.Fatalf("closing line = %q, want %s", last, semanticToolsSearchListedCloser)
	}
}

// A classified page with no listed directive and no plan still ends on that
// turn's petition rule. Discovery omits the same sentence because it has no
// once-per-class budget.
func TestSemanticToolsSearchClassifiedPageKeepsPetitionFooter(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(0)
	got := cb.runSemanticToolsSearch(`{"query":"目录"}`)
	if !strings.Contains(got, "每轮每类限一次") || !strings.Contains(got, "不在普通回合里") {
		t.Fatalf("classified page lost its petition rule:\n%s", got)
	}
	again := cb.runSemanticToolsSearch(`{"query":"目录"}`)
	if again != semanticToolsSearchRepeatExhausted {
		t.Fatalf("classified repeat = %s", again)
	}
}

// The hand-built chain above matches the denial contract. This one uses the
// desktop planner's weather-PDF surface: lookup is the grant, generate_pdf
// stays a directory petition until the model names it.
func TestSemanticToolsSearchRealWeatherPDFNamesLookupNotDelivery(t *testing.T) {
	cb, searchName := weatherPDFDesktopBeforeSearch(t)
	// The loop adopts the request list before the model can call tools_search.
	// Until then the publish leaves rendered empty, so a live grant looks unlisted.
	if len(cb.BuildToolsForModelRequest(cb.userText, 1)) == 0 {
		t.Fatal("request surface was not adopted")
	}
	got := semanticToolsSearchRun(cb, `{"query":"生成pdf"}`)
	if len(got) > agent.ToolsSearchMaxToolResult {
		t.Fatalf("page is %d bytes, budget %d", len(got), agent.ToolsSearchMaxToolResult)
	}
	decisionLine := got
	if at := strings.Index(got, "本轮决定："); at >= 0 {
		decisionLine = got[at:]
		if end := strings.IndexByte(decisionLine, '\n'); end >= 0 {
			decisionLine = decisionLine[:end]
		}
	} else {
		t.Fatalf("real weather plan produced no decision: %s", got)
	}
	if searchName == "" || !strings.Contains(decisionLine, "已在当前工具面") || !strings.Contains(decisionLine, searchName) {
		t.Fatalf("listed lookup missing: %s", decisionLine)
	}
	if strings.Contains(decisionLine, "现在调用") || strings.Contains(decisionLine, "generate_pdf（") || strings.Contains(decisionLine, "send_file（") {
		t.Fatalf("unpetitioned generate must not be a planned successor: %s", decisionLine)
	}
	for _, baseline := range []string{"bash", "read_file", "write_file"} {
		if strings.Contains(decisionLine, baseline) {
			t.Fatalf("baseline %s is not the weather grant: %s", baseline, decisionLine)
		}
	}
	var generateRow string
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- generate_pdf — ") {
			generateRow = line
		}
		if strings.HasPrefix(line, "- send_file — ") && (strings.Contains(line, "自动出现") || strings.Contains(line, "[已在当前工具面]")) {
			t.Fatalf("delivery is not a first-wave grant: %s", line)
		}
		if strings.HasPrefix(line, "- bash — ") && strings.Contains(line, "[已在当前工具面]") {
			t.Fatalf("bash is not a weather grant: %s", line)
		}
	}
	if generateRow == "" || !strings.Contains(generateRow, "可请愿") {
		t.Fatalf("generate_pdf must stay petitionable: %s", generateRow)
	}
}

func TestLiftExpiredToolsSearchHoldWhenNameIsListed(t *testing.T) {
	const page = "tools_search results for \"生成北京天气PDF\":\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）；send_file（generate_pdf 出现在下一请求后再调用；send_file 要等它自己出现在列表）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF [已列入本轮计划。本响应的其它工具调用结束后，下一请求会出现。不要直接调用，也不要请愿。]\n" +
		"- send_file — Attach the published file [已列入本轮计划，先等 generate_pdf。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	stored := map[string]interface{}{"role": "tool", "tool_call_id": "call-1", "content": page}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	cb := &sharedAgentLoopCallbacks{tools: []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "send_file"}},
	}}
	conv := []interface{}{stored, map[string]interface{}{"role": "user", "content": "生成pdf"}}
	got := cb.LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed generate_pdf must lift the hold on the outbound copy")
	}
	if stored["content"] != page {
		t.Fatal("stored tools_search result was rewritten")
	}
	content, _ := got[0].(map[string]interface{})["content"].(string)
	if strings.Contains(content, "现在不要调用。") || strings.Contains(content, "尚未列出：generate_pdf") {
		t.Fatalf("generate_pdf hold survived:\n%s", content)
	}
	if !strings.Contains(content, "立刻调用 generate_pdf。") || !strings.Contains(content, "现在调用 generate_pdf。") {
		t.Fatalf("outbound page did not order the listed tool:\n%s", content)
	}
	if !strings.Contains(content, "[已在当前工具列表，立刻调用。]") {
		t.Fatalf("generate_pdf row still forbids the call:\n%s", content)
	}
	if !strings.Contains(content, "尚未列出：send_file") || !strings.Contains(content, "不要直接调用 send_file") || !strings.Contains(content, "不要直接调用 send_file，也不要请愿。]") {
		t.Fatalf("unlisted send_file must stay held:\n%s", content)
	}
	if strings.Contains(content, "这些名字") || strings.Contains(content, "不要直接调用，也不要请愿") || strings.Contains(content, "完成后再调用") {
		t.Fatalf("the ban lost its subject, or generate_pdf picked up an unbound wait:\n%s", content)
	}
	if strings.Contains(content, "出现在下一请求") || strings.Contains(content, "先等 generate_pdf") || strings.Contains(content, "下一请求会出现") || !strings.Contains(content, "generate_pdf 已在当前工具列表") {
		t.Fatalf("send_file clause still says generate_pdf appears on the next request:\n%s", content)
	}
	if _, ok := got[1].(map[string]interface{}); !ok {
		t.Fatal("later messages were dropped")
	}
}

// The stored Beijing weather page. generate_pdf is the only directory row;
// send_file lives in the decision clause. Once generate_pdf is on the wire
// list, that clause must not grow an unbound "后再调用".
func TestLiftExpiredToolsSearchHoldLiftsBeijingPage(t *testing.T) {
	const page = "tools_search results for \"生成北京天气PDF\":\n" +
		"scope_id=semantic-scope:a5748ba1f335cd4d1141c5f1ff96d7e303e15272ccaae77a0dfa7214f3849c9a catalog_digest=catalog:sha256:93790b07e3b20e9e4a3a7b140432c2219e3af3797afe3f81ba9216716fef3a3d decision_id=decision:1d74fdb526559435b5f832d93863478f3e727317c5803864499cef5b52b209a8\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）；send_file（generate_pdf 出现在下一请求后再调用；send_file 要等它自己出现在列表）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF and publish an Artif… [已列入本轮计划。本响应的其它工具调用结束后，下一请求会出现。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	stored := map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page}
	conv := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				Function: llm.ToolCallFunction{Name: "tools_search"},
			}},
		},
		stored,
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed generate_pdf must lift the Beijing page")
	}
	if stored["content"] != page {
		t.Fatal("stored tools_search result was rewritten")
	}
	content, _ := got[1].(map[string]interface{})["content"].(string)
	for _, stale := range []string{
		"尚未列出：generate_pdf",
		"下一请求出现：generate_pdf",
		"下一请求会出现",
		"出现在下一请求",
		"现在不要调用。",
		"不要直接调用这些名字",
		"完成后再调用",
		"不要直接调用，也不要请愿",
		"本响应的其它调用结束后",
		"本响应的其它工具调用结束后",
	} {
		if strings.Contains(content, stale) {
			t.Fatalf("Beijing page kept %q:\n%s", stale, content)
		}
	}
	if !strings.Contains(content, "立刻调用 generate_pdf。") || !strings.Contains(content, "现在调用 generate_pdf。") {
		t.Fatalf("Beijing page did not order generate_pdf:\n%s", content)
	}
	if !strings.Contains(content, "不要直接调用 send_file") || !strings.Contains(content, "send_file 要等它自己出现在列表") {
		t.Fatalf("unlisted send_file lost its hold:\n%s", content)
	}
	if !strings.Contains(content, "- generate_pdf — Render Markdown into a PDF and publish an Artif… [已在当前工具列表，立刻调用。]") {
		t.Fatalf("generate_pdf row still forbids the call:\n%s", content)
	}
}

func TestLiftExpiredToolsSearchHoldLeavesUnlistedPage(t *testing.T) {
	const page = "tools_search results for \"pdf\":\n下一请求出现：generate_pdf。现在不要调用。\n"
	cb := &sharedAgentLoopCallbacks{tools: []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
	}}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	conv := []interface{}{map[string]interface{}{"role": "tool", "content": page}}
	if got := cb.LiftExpiredDirectoryHolds(conv, wire); got != nil {
		t.Fatalf("a name absent from the wire list must keep the hold, got %#v", got)
	}
}

func TestLiftExpiredToolsSearchHoldDoesNotReorderACalledName(t *testing.T) {
	const page = "tools_search results for \"pdf\":\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF [已列入本轮计划。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	conv := []interface{}{
		map[string]interface{}{"role": "tool", "content": page},
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{
				map[string]interface{}{"function": map[string]interface{}{"name": "generate_pdf"}},
			},
		},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("a listed name must still lose the expired prohibition")
	}
	content, _ := got[0].(map[string]interface{})["content"].(string)
	if strings.Contains(content, "现在不要调用") || strings.Contains(content, "尚未列出：generate_pdf") || strings.Contains(content, "立刻调用") || strings.Contains(content, "现在调用") {
		t.Fatalf("a name already called must not be ordered again:\n%s", content)
	}
	if !strings.Contains(content, "[已在当前工具列表。]") {
		t.Fatalf("row still forbids the listed name:\n%s", content)
	}
	if conv[0].(map[string]interface{})["content"] != page {
		t.Fatal("stored page was rewritten")
	}
}

func TestLiftExpiredToolsSearchHoldOrdersOnlyTheLatestPage(t *testing.T) {
	const older = "tools_search results for \"抓取网页\":\n" +
		"本轮决定：已计划但本请求尚未列出：web_fetch（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- web_fetch — Fetch one page [已列入本轮计划。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：web_fetch。现在不要调用。\n"
	const latest = "tools_search results for \"pdf\":\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF [已列入本轮计划。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	conv := []interface{}{
		map[string]interface{}{"role": "tool", "content": older},
		map[string]interface{}{"role": "tool", "content": latest},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_fetch"}},
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed names must lift")
	}
	oldText, _ := got[0].(map[string]interface{})["content"].(string)
	newText, _ := got[1].(map[string]interface{})["content"].(string)
	if strings.Contains(oldText, "现在调用") || strings.Contains(oldText, "立刻调用") || strings.Contains(oldText, "现在不要调用") {
		t.Fatalf("an older hold must not become a new order:\n%s", oldText)
	}
	if !strings.Contains(oldText, "[已在当前工具列表。]") {
		t.Fatalf("older listed row still forbids the call:\n%s", oldText)
	}
	if !strings.Contains(newText, "现在调用 generate_pdf。") || !strings.Contains(newText, "立刻调用 generate_pdf。") {
		t.Fatalf("the latest hold must order the listed tool:\n%s", newText)
	}
}

func TestLiftExpiredToolsSearchHoldOrdersAListedClauseName(t *testing.T) {
	const page = "tools_search results for \"生成北京天气PDF\":\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）；send_file（generate_pdf 出现在下一请求后再调用；send_file 要等它自己出现在列表）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF [已列入本轮计划。不要直接调用，也不要请愿。]\n" +
		"- send_file — Attach the published file [已列入本轮计划，先等 generate_pdf。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	stored := map[string]interface{}{"role": "tool", "tool_call_id": "call-1", "content": page}
	conv := []interface{}{
		stored,
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				Function: llm.ToolCallFunction{Name: "generate_pdf"},
			}},
		},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "send_file"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("a listed send_file clause must lift")
	}
	if stored["content"] != page {
		t.Fatal("stored tools_search result was rewritten")
	}
	content, _ := got[0].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, "立刻调用 send_file。") || !strings.Contains(content, "现在调用 send_file。") {
		t.Fatalf("listed send_file was not ordered:\n%s", content)
	}
	if strings.Contains(content, "立刻调用 generate_pdf") || strings.Contains(content, "现在调用 generate_pdf") || strings.Contains(content, "不要直接调用 send_file") || strings.Contains(content, "尚未列出：send_file") {
		t.Fatalf("spent generate_pdf was reordered or send_file stayed held:\n%s", content)
	}
	if !strings.Contains(content, "- generate_pdf — Render Markdown into a PDF [已在当前工具列表。]") {
		t.Fatalf("spent generate_pdf row still forbids the call:\n%s", content)
	}
	if !strings.Contains(content, "- send_file — Attach the published file [已在当前工具列表，立刻调用。]") {
		t.Fatalf("send_file row still forbids the call:\n%s", content)
	}
}

// The lookup page orders web_search and forbids generate_pdf in the same
// decision. It does not end with the next-request closer. After web_search
// has run and generate_pdf is on the wire list, that prohibition is stale.
func TestLiftExpiredToolsSearchHoldAfterPredecessorRuns(t *testing.T) {
	page := semanticToolsSearchWeatherPredecessorPage(t)
	stored := map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page}
	conv := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{
				{Function: llm.ToolCallFunction{Name: "tools_search"}},
				{Function: llm.ToolCallFunction{Name: "web_search"}},
			},
		},
		stored,
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed generate_pdf must lift a predecessor page")
	}
	if stored["content"] != page {
		t.Fatal("stored tools_search result was rewritten")
	}
	content, _ := got[1].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, "立刻调用 generate_pdf。") || !strings.Contains(content, "现在调用 generate_pdf。") {
		t.Fatalf("predecessor page did not order generate_pdf:\n%s", content)
	}
	if strings.Contains(content, "现在调用 web_search") || strings.Contains(content, "立刻调用 web_search") || strings.Contains(content, "先调用 web_search") || strings.Contains(content, "这些名字") {
		t.Fatalf("spent lookup stayed an order, or the ban still says 这些名字:\n%s", content)
	}
	if !strings.Contains(content, "不要直接调用 send_file") {
		t.Fatalf("unlisted send_file lost its hold:\n%s", content)
	}
	if strings.Contains(content, "先调用 web_search") || strings.Contains(content, "之后会出现 generate_pdf") || strings.Contains(content, "下一请求会出现") || strings.Contains(content, "它出现在下一请求") || strings.Contains(content, "完成后再调用") {
		t.Fatalf("predecessor row still describes generate_pdf as absent:\n%s", content)
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "- send_file — ") && strings.Contains(line, "不要直接调用，也不要请愿") {
			t.Fatalf("send_file row ban does not name its subject:\n%s", line)
		}
	}
}

// web_search in the tools_search message is spent only when that batch let it
// run. A planned denial in the same batch, before or after the page, leaves
// the order in place.
func TestLiftExpiredToolsSearchHoldKeepsADeniedIssuingOrder(t *testing.T) {
	const page = "tools_search results for \"pdf\":\n" +
		"本轮决定：现在调用 web_search。已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- send_file — Attach the published file [已列入本轮计划，先调用 web_search。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	denial := plannedToolNotYetListedMessage("web_search", nil)
	lift := func(t *testing.T, conv []interface{}, wireNames []string) (string, bool) {
		t.Helper()
		wire := make([]map[string]interface{}, 0, len(wireNames)+1)
		for _, name := range wireNames {
			wire = append(wire, map[string]interface{}{
				"type": "function", "function": map[string]interface{}{"name": name},
			})
		}
		wire = append(wire, map[string]interface{}{
			"type": "function", "function": map[string]interface{}{"name": "tools_search"},
		})
		got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
		if got == nil {
			return "", false
		}
		for _, item := range conv {
			content, ok := item.(map[string]interface{})["content"].(string)
			if ok && strings.HasPrefix(content, "tools_search results") && content != page {
				t.Fatal("stored tools_search result was rewritten")
			}
		}
		for _, item := range got {
			content, _ := item.(map[string]interface{})["content"].(string)
			if strings.HasPrefix(content, "tools_search results") {
				return content, true
			}
		}
		t.Fatal("rewritten conversation dropped the tools_search page")
		return "", false
	}
	issuing := func(calls ...string) map[string]interface{} {
		toolCalls := make([]llm.ToolCall, 0, len(calls))
		for _, name := range calls {
			toolCalls = append(toolCalls, llm.ToolCall{Function: llm.ToolCallFunction{Name: name}})
		}
		return map[string]interface{}{"role": "assistant", "tool_calls": toolCalls}
	}
	pageMsg := map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page}
	denialMsg := map[string]interface{}{"role": "tool", "tool_call_id": "call-search-web", "content": denial}

	// The denial itself is stale once web_search is listed. The tools_search
	// page already orders it, and that sentence must stay byte-identical.
	afterPage, ok := lift(t, []interface{}{issuing("tools_search", "web_search"), pageMsg, denialMsg}, []string{"web_search"})
	if !ok || afterPage != page {
		t.Fatalf("a denied web_search after the page must keep the live order:\n%s", afterPage)
	}
	beforePage, ok := lift(t, []interface{}{issuing("web_search", "tools_search"), denialMsg, pageMsg}, []string{"web_search"})
	if !ok || beforePage != page {
		t.Fatalf("a denied web_search before the page must keep the live order:\n%s", beforePage)
	}

	listed, ok := lift(t, []interface{}{issuing("tools_search", "web_search"), pageMsg, denialMsg}, []string{"web_search", "generate_pdf"})
	if !ok {
		t.Fatal("listed generate_pdf must still lift the page when web_search was denied")
	}
	if !strings.Contains(listed, "立刻调用 web_search、generate_pdf。") || !strings.Contains(listed, "先调用 web_search") {
		t.Fatalf("denied web_search lost its order or its lead:\n%s", listed)
	}
	if strings.Contains(listed, "web_search 已调用") {
		t.Fatalf("denied web_search was described as already called:\n%s", listed)
	}

	spent, ok := lift(t, []interface{}{
		issuing("tools_search", "web_search"),
		pageMsg,
		map[string]interface{}{"role": "tool", "tool_call_id": "call-web", "content": "北京今天晴"},
	}, []string{"web_search"})
	if !ok {
		t.Fatal("an issuing web_search that returned a result is spent")
	}
	if strings.Contains(spent, "现在调用 web_search") || strings.Contains(spent, "立刻调用 web_search") || strings.Contains(spent, "先调用 web_search") {
		t.Fatalf("spent web_search stayed an order:\n%s", spent)
	}
	if !strings.Contains(spent, "web_search 已调用") {
		t.Fatalf("spent web_search lost its called fact:\n%s", spent)
	}
}

// An unrendered denial and a repeat-wave continuation are the same fact as a
// planned denial: the call did not run. Once the name is listed, the page
// keeps its order and the notice itself says to call it. A consumed-grant
// result records a prior success and still spends the order.
func TestLiftExpiredSurfaceRejectionIsNotACall(t *testing.T) {
	const page = "tools_search results for \"pdf\":\n" +
		"本轮决定：现在调用 web_search。已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- send_file — Attach the published file [已列入本轮计划，先调用 web_search。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	unrendered := `Error: tool "web_search" was not available in this request's rendered tool surface. Do not retry "web_search" and do not ask the user to re-authorize tools; continue with the tools rendered in this request or answer from what you already have.`
	repeatWave := `Error: tool "web_search" is listed on the next request in this same reply. Call it then. This turn is not finished, so do not ask the user to continue and do not ask them to re-authorize tools.`
	consumed := `Error: tool "web_search" already ran successfully earlier in this turn. That result still stands (e.g. an artifact was already generated or delivered). If the name is listed, call it and continue the unfinished task. Do not tell the user that a quota or usage limit was used up, do not ask them to send another message, do not retry "web_search" when it is not listed, and do not report that action as failed, consumed, or undelivered.`
	issuing := map[string]interface{}{
		"role": "assistant",
		"tool_calls": []llm.ToolCall{
			{Function: llm.ToolCallFunction{Name: "tools_search"}},
			{Function: llm.ToolCallFunction{Name: "web_search"}},
		},
	}
	pageMsg := map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page}
	lift := func(t *testing.T, notice string, wireNames []string, extra ...interface{}) (string, string) {
		t.Helper()
		noticeMsg := map[string]interface{}{"role": "tool", "tool_call_id": "call-web", "content": notice}
		conv := []interface{}{issuing, pageMsg, noticeMsg}
		conv = append(conv, extra...)
		wire := make([]map[string]interface{}, 0, len(wireNames)+1)
		for _, name := range wireNames {
			wire = append(wire, map[string]interface{}{
				"type": "function", "function": map[string]interface{}{"name": name},
			})
		}
		wire = append(wire, map[string]interface{}{
			"type": "function", "function": map[string]interface{}{"name": "tools_search"},
		})
		got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
		if pageMsg["content"] != page || noticeMsg["content"] != notice {
			t.Fatal("stored tool result was rewritten")
		}
		if got == nil {
			return "", ""
		}
		var pageText, noticeText string
		for _, item := range got {
			content, _ := item.(map[string]interface{})["content"].(string)
			switch {
			case strings.HasPrefix(content, "tools_search results"):
				pageText = content
			case strings.HasPrefix(content, "Error: tool \"web_search\""):
				noticeText = content
			}
		}
		return pageText, noticeText
	}
	const callNow = `Error: tool "web_search" is listed on this request. Call it now.`

	pageText, noticeText := lift(t, unrendered, []string{"web_search"})
	if pageText != page {
		t.Fatalf("an unrendered web_search must keep the live order:\n%s", pageText)
	}
	if noticeText != callNow || strings.Contains(noticeText, "was not available") || strings.Contains(noticeText, "Do not retry") {
		t.Fatalf("unrendered notice stayed next to the order:\n%s", noticeText)
	}

	pageText, noticeText = lift(t, repeatWave, []string{"web_search"})
	if pageText != page {
		t.Fatalf("a repeat-wave web_search must keep the live order:\n%s", pageText)
	}
	if noticeText != callNow || strings.Contains(noticeText, "on the next request") {
		t.Fatalf("repeat-wave notice stayed next to the order:\n%s", noticeText)
	}

	pageText, noticeText = lift(t, consumed, []string{"web_search"})
	if pageText == "" {
		t.Fatal("a consumed-grant web_search is a completed call and must lift the page")
	}
	if strings.Contains(pageText, "现在调用 web_search") || strings.Contains(pageText, "立刻调用 web_search") || strings.Contains(pageText, "先调用 web_search") {
		t.Fatalf("consumed-grant web_search stayed an order:\n%s", pageText)
	}
	if !strings.Contains(pageText, "web_search 已调用") {
		t.Fatalf("consumed-grant web_search lost its called fact:\n%s", pageText)
	}
	if noticeText != consumed {
		t.Fatalf("consumed-grant result was rewritten:\n%s", noticeText)
	}

	later := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				Function: llm.ToolCallFunction{Name: "web_search"},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call-web-2", "content": "北京今天晴"},
	}
	pageText, noticeText = lift(t, repeatWave, nil, later...)
	if !strings.Contains(pageText, "web_search 已调用") {
		t.Fatalf("a later web_search result must spend the order:\n%s", pageText)
	}
	if noticeText != `Error: tool "web_search" has already been called.` {
		t.Fatalf("repeat-wave notice kept a next-request promise after the call ran:\n%s", noticeText)
	}
}

// Invalid arguments never reach the tool. The result says to send the call
// again, so the directory page must keep that order. The regenerate sentence
// stays until a later call actually returns.
func TestLiftExpiredArgumentRejectionIsNotACall(t *testing.T) {
	const page = "tools_search results for \"pdf\":\n" +
		"本轮决定：现在调用 web_search。已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- send_file — Attach the published file [已列入本轮计划，先调用 web_search。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	invalid := `Error: tool call "web_search" has invalid JSON object arguments. The tool was not executed. Regenerate the same tool call with complete valid JSON object arguments, including all required fields, and do not summarize or truncate file content.`
	issuing := map[string]interface{}{
		"role": "assistant",
		"tool_calls": []llm.ToolCall{
			{Function: llm.ToolCallFunction{Name: "tools_search"}},
			{Function: llm.ToolCallFunction{Name: "web_search"}},
		},
	}
	pageMsg := map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page}
	lift := func(t *testing.T, wireNames []string, extra ...interface{}) (string, string) {
		t.Helper()
		noticeMsg := map[string]interface{}{"role": "tool", "tool_call_id": "call-web", "content": invalid}
		conv := []interface{}{issuing, pageMsg, noticeMsg}
		conv = append(conv, extra...)
		wire := make([]map[string]interface{}, 0, len(wireNames)+1)
		for _, name := range wireNames {
			wire = append(wire, map[string]interface{}{
				"type": "function", "function": map[string]interface{}{"name": name},
			})
		}
		wire = append(wire, map[string]interface{}{
			"type": "function", "function": map[string]interface{}{"name": "tools_search"},
		})
		got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
		if pageMsg["content"] != page || noticeMsg["content"] != invalid {
			t.Fatal("stored tool result was rewritten")
		}
		if got == nil {
			return "", ""
		}
		var pageText, noticeText string
		for _, item := range got {
			m, _ := item.(map[string]interface{})
			content, _ := m["content"].(string)
			switch m["tool_call_id"] {
			case "call-search":
				pageText = content
			case "call-web":
				noticeText = content
			}
		}
		return pageText, noticeText
	}
	later := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				Function: llm.ToolCallFunction{Name: "web_search"},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call-web-2", "content": "北京今天晴"},
	}

	pageText, noticeText := lift(t, []string{"web_search"})
	if pageText != "" && pageText != page {
		t.Fatalf("invalid arguments spent the live order:\n%s", pageText)
	}
	if noticeText != "" && noticeText != invalid {
		t.Fatalf("outstanding argument rejection was rewritten:\n%s", noticeText)
	}

	pageText, noticeText = lift(t, nil, later...)
	if !strings.Contains(pageText, "web_search 已调用") || strings.Contains(pageText, "现在调用 web_search") {
		t.Fatalf("a later web_search result must spend the order:\n%s", pageText)
	}
	if noticeText != `Error: tool "web_search" has already been called.` {
		t.Fatalf("argument rejection still says to regenerate after the call ran:\n%s", noticeText)
	}

	pageText, noticeText = lift(t, []string{"web_search"}, later...)
	if strings.Contains(pageText, "现在调用 web_search") || strings.Contains(pageText, "立刻调用 web_search") || strings.Contains(pageText, "先调用 web_search") {
		t.Fatalf("a completed web_search stayed an order while it is listed:\n%s", pageText)
	}
	if !strings.Contains(pageText, "web_search 已调用") {
		t.Fatalf("a completed web_search lost its called fact:\n%s", pageText)
	}
	if noticeText != `Error: tool "web_search" is listed on this request.` || strings.Contains(noticeText, "Call it now") || strings.Contains(noticeText, "was not executed") {
		t.Fatalf("argument rejection still orders a call that already returned:\n%s", noticeText)
	}
}

// The production held page, not a hand-built sentence. Search has already
// committed and generate_pdf is withheld only until that batch ends. The next
// request lists it.
func TestLiftExpiredToolsSearchHoldLiftsRealHeldPage(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(0)
	cb.semanticHoldDependantIssue = true
	cb.semanticSurface.completed = map[string]bool{"search": true}
	cb.semanticSurface.plan.Selections = []tool.PlannedSelection{
		{ID: "search", AdapterName: "web_search", FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		{ID: "pdf", AdapterName: "host_document_generate_file", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}, Requires: []string{"search"}},
		{ID: "deliver", AdapterName: "semantic_deliver_current_file", FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel"}, Requires: []string{"pdf"}},
	}
	cb.semanticSurface.grants["web_search"] = tool.InvocationGrant{SelectionID: "search", Token: "search-token", AdapterName: "web_search"}
	page := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"pdf"}`)
	stored := map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page}
	conv := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				Function: llm.ToolCallFunction{Name: "tools_search"},
			}},
		},
		stored,
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed generate_pdf must lift the real held page")
	}
	if stored["content"] != page {
		t.Fatal("stored tools_search result was rewritten")
	}
	content, _ := got[1].(map[string]interface{})["content"].(string)
	for _, stale := range []string{
		"现在不要调用。",
		"下一请求会出现",
		"先等 generate_pdf",
		"出现在下一请求",
		"尚未列出：generate_pdf",
		"不要直接调用这些名字",
		"它出现在下一请求",
		"完成后再调用",
	} {
		if strings.Contains(content, stale) {
			t.Fatalf("real held page kept %q:\n%s", stale, content)
		}
	}
	if !strings.Contains(content, "立刻调用 generate_pdf。") || !strings.Contains(content, "现在调用 generate_pdf。") {
		t.Fatalf("real held page did not order generate_pdf:\n%s", content)
	}
	if !strings.Contains(content, "不要直接调用 send_file") {
		t.Fatalf("unlisted send_file lost its hold:\n%s", content)
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, "不要直接调用，也不要请愿") && strings.Contains(line, "generate_pdf") {
			t.Fatalf("a row names listed generate_pdf and still has an unnamed ban:\n%s", line)
		}
	}
}

func TestLiftExpiredToolsSearchHoldRelaxesNameLists(t *testing.T) {
	const page = "tools_search results for \"pdf\":\n" +
		"本轮决定：现在调用 web_search。已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- send_file — Attach the published file [已列入本轮计划，先调用 web_search、web_fetch。之后会出现 generate_pdf、office。先等 generate_pdf。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	conv := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{
				{Function: llm.ToolCallFunction{Name: "tools_search"}},
				{Function: llm.ToolCallFunction{Name: "web_search"}},
			},
		},
		map[string]interface{}{"role": "tool", "content": page},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "web_fetch"}},
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed generate_pdf must lift the page")
	}
	content, _ := got[1].(map[string]interface{})["content"].(string)
	if strings.Contains(content, "先调用 web_search") || strings.Contains(content, "先等 generate_pdf") || strings.Contains(content, "之后会出现 generate_pdf") {
		t.Fatalf("spent or listed names stayed inside the lead:\n%s", content)
	}
	if !strings.Contains(content, "web_search 已调用，先调用 web_fetch") {
		t.Fatalf("the unspent name lost its lead:\n%s", content)
	}
	if !strings.Contains(content, "generate_pdf 已在当前工具列表，之后会出现 office") {
		t.Fatalf("unlisted office lost its wait, or listed generate_pdf stayed absent:\n%s", content)
	}
	if !strings.Contains(content, "立刻调用 generate_pdf。") {
		t.Fatalf("hold was not ordered:\n%s", content)
	}
}

// A 、 list before "出现在下一请求" is one fact. Rewriting only the name
// glued to the suffix leaves an earlier listed name absent, and glues an
// earlier unlisted name onto "已在当前工具列表".
func TestLiftExpiredToolsSearchHoldRelaxesNextRequestNameList(t *testing.T) {
	lift := func(t *testing.T, clause, closer string, called []string, wireNames []string) string {
		t.Helper()
		page := "tools_search results for \"pdf\":\n本轮决定：已计划但本请求尚未列出：" + clause + "。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n下一请求出现：" + closer + "。现在不要调用。\n"
		calls := []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "tools_search"}}}
		for _, name := range called {
			calls = append(calls, llm.ToolCall{Function: llm.ToolCallFunction{Name: name}})
		}
		wire := make([]map[string]interface{}, 0, len(wireNames)+1)
		for _, name := range wireNames {
			wire = append(wire, map[string]interface{}{
				"type": "function", "function": map[string]interface{}{"name": name},
			})
		}
		wire = append(wire, map[string]interface{}{
			"type": "function", "function": map[string]interface{}{"name": "tools_search"},
		})
		conv := []interface{}{
			map[string]interface{}{"role": "assistant", "tool_calls": calls},
			map[string]interface{}{"role": "tool", "content": page},
		}
		got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
		if got == nil {
			t.Fatalf("listed closer %s must lift the page:\n%s", closer, page)
		}
		content, _ := got[1].(map[string]interface{})["content"].(string)
		return content
	}

	laterOnly := semanticToolsSearchLaterClause("archive_file", predecessorNames{
		nextRequest: []string{"generate_pdf", "office_export"},
	}, false)
	firstListed := lift(t, laterOnly, "generate_pdf", nil, []string{"generate_pdf"})
	if !strings.Contains(firstListed, "generate_pdf 已在当前工具列表，office_export 出现在下一请求后再调用") {
		t.Fatalf("listed generate_pdf stayed inside the next-request list:\n%s", firstListed)
	}
	if strings.Contains(firstListed, "generate_pdf、office_export") || strings.Contains(firstListed, "generate_pdf 出现在下一请求") || strings.Contains(firstListed, "office_export 已在当前工具列表") {
		t.Fatalf("the next-request list was rewritten as one fact:\n%s", firstListed)
	}
	if !strings.Contains(firstListed, "不要直接调用 archive_file") || !strings.Contains(firstListed, "立刻调用 generate_pdf。") {
		t.Fatalf("archive_file lost its hold, or generate_pdf was not ordered:\n%s", firstListed)
	}

	lastListed := lift(t, laterOnly, "office_export", nil, []string{"office_export"})
	if !strings.Contains(lastListed, "generate_pdf 出现在下一请求后再调用，office_export 已在当前工具列表") {
		t.Fatalf("unlisted generate_pdf was pulled into the listed fact:\n%s", lastListed)
	}
	if strings.Contains(lastListed, "generate_pdf 已在当前工具列表") || strings.Contains(lastListed, "立刻调用 generate_pdf") || strings.Contains(lastListed, "现在调用 generate_pdf") {
		t.Fatalf("unlisted generate_pdf was described as listed or ordered:\n%s", lastListed)
	}

	withCall := semanticToolsSearchLaterClause("archive_file", predecessorNames{
		callNow:     []string{"web_search", "web_fetch"},
		nextRequest: []string{"generate_pdf", "office_export"},
	}, false)
	mixed := lift(t, withCall, "generate_pdf", []string{"web_search"}, []string{"web_search", "web_fetch", "generate_pdf"})
	if !strings.Contains(mixed, "web_search 已调用，先调用 web_fetch；成功后 generate_pdf 已在当前工具列表，office_export 出现在下一请求。") {
		t.Fatalf("call list and next-request list were not rewritten together:\n%s", mixed)
	}
	if strings.Contains(mixed, "generate_pdf、office_export 出现在下一请求") || strings.Contains(mixed, "先调用 web_search") {
		t.Fatalf("spent or listed names stayed in the production clause:\n%s", mixed)
	}
}

// After generate_pdf runs, the one-shot grant is spent and the next request
// lists send_file without generate_pdf. The stored page still says
// generate_pdf appears on the next request. That wait is false. A denied
// call in the issuing message is not that run.
func TestLiftExpiredToolsSearchHoldClearsCalledUnlistedAppearance(t *testing.T) {
	const beijing = "tools_search results for \"生成北京天气PDF\":\n" +
		"scope_id=semantic-scope:a5748ba1f335cd4d1141c5f1ff96d7e303e15272ccaae77a0dfa7214f3849c9a catalog_digest=catalog:sha256:93790b07e3b20e9e4a3a7b140432c2219e3af3797afe3f81ba9216716fef3a3d decision_id=decision:1d74fdb526559435b5f832d93863478f3e727317c5803864499cef5b52b209a8\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）；send_file（generate_pdf 出现在下一请求后再调用；send_file 要等它自己出现在列表）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF and publish an Artif… [已列入本轮计划。本响应的其它工具调用结束后，下一请求会出现。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	lift := func(t *testing.T, page string, before, after []string, wireNames []string) (string, bool) {
		t.Helper()
		issuing := []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "tools_search"}}}
		for _, name := range before {
			issuing = append(issuing, llm.ToolCall{Function: llm.ToolCallFunction{Name: name}})
		}
		stored := map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page}
		conv := []interface{}{
			map[string]interface{}{"role": "assistant", "tool_calls": issuing},
			stored,
		}
		if len(after) > 0 {
			calls := make([]llm.ToolCall, 0, len(after))
			for _, name := range after {
				calls = append(calls, llm.ToolCall{Function: llm.ToolCallFunction{Name: name}})
			}
			conv = append(conv, map[string]interface{}{
				"role": "assistant", "tool_calls": calls,
			})
		}
		wire := make([]map[string]interface{}, 0, len(wireNames)+1)
		for _, name := range wireNames {
			wire = append(wire, map[string]interface{}{
				"type": "function", "function": map[string]interface{}{"name": name},
			})
		}
		wire = append(wire, map[string]interface{}{
			"type": "function", "function": map[string]interface{}{"name": "tools_search"},
		})
		got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
		if stored["content"] != page {
			t.Fatal("stored tools_search result was rewritten")
		}
		if got == nil {
			return "", false
		}
		content, _ := got[1].(map[string]interface{})["content"].(string)
		return content, true
	}

	delivered, ok := lift(t, beijing, nil, []string{"generate_pdf"}, []string{"send_file"})
	if !ok {
		t.Fatal("a called generate_pdf must clear the appearance promise once send_file is listed")
	}
	for _, stale := range []string{
		"出现在下一请求",
		"下一请求会出现",
		"尚未列出：generate_pdf",
		"generate_pdf 已在当前工具列表",
		"不要直接调用 send_file",
		"下一请求出现：generate_pdf",
	} {
		if strings.Contains(delivered, stale) {
			t.Fatalf("delivery request kept %q:\n%s", stale, delivered)
		}
	}
	if !strings.Contains(delivered, "立刻调用 send_file。") || !strings.Contains(delivered, "现在调用 send_file。") {
		t.Fatalf("listed send_file was not ordered:\n%s", delivered)
	}
	if !strings.Contains(delivered, "- generate_pdf — Render Markdown into a PDF and publish an Artif… [已调用。]") {
		t.Fatalf("spent generate_pdf row still promises a later appearance:\n%s", delivered)
	}

	spent, ok := lift(t, beijing, nil, []string{"generate_pdf"}, nil)
	if !ok {
		t.Fatal("a called generate_pdf must clear the appearance promise even when send_file is still absent")
	}
	for _, stale := range []string{
		"出现在下一请求",
		"下一请求会出现",
		"generate_pdf 已在当前工具列表",
		"立刻调用",
		"现在调用 ",
	} {
		if strings.Contains(spent, stale) {
			t.Fatalf("spent generate_pdf stayed a future appearance:\n%s", spent)
		}
	}
	if !strings.Contains(spent, "send_file（generate_pdf 已调用；send_file 要等它自己出现在列表）") {
		t.Fatalf("send_file clause did not record that generate_pdf already ran:\n%s", spent)
	}
	if !strings.Contains(spent, "不要直接调用 send_file") || !strings.Contains(spent, "仍未列出、现在不要调用：send_file。") {
		t.Fatalf("unlisted send_file lost its hold:\n%s", spent)
	}
	if !strings.Contains(spent, "- generate_pdf — Render Markdown into a PDF and publish an Artif… [已调用。]") {
		t.Fatalf("spent generate_pdf row still promises a later appearance:\n%s", spent)
	}

	if _, ok := lift(t, beijing, []string{"generate_pdf"}, nil, nil); ok {
		t.Fatal("a denied generate_pdf in the issuing message is not a finished call")
	}
	deniedListed, ok := lift(t, beijing, []string{"generate_pdf"}, nil, []string{"generate_pdf"})
	if !ok || !strings.Contains(deniedListed, "立刻调用 generate_pdf。") || strings.Contains(deniedListed, "[已调用。]") {
		t.Fatalf("a denied issuing call must still be ordered once it is listed:\n%s", deniedListed)
	}

	const rows = "tools_search results for \"生成北京天气PDF\":\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）；send_file（generate_pdf 出现在下一请求后再调用；send_file 要等它自己出现在列表）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF [已列入本轮计划。本响应的其它工具调用结束后，下一请求会出现。不要直接调用，也不要请愿。]\n" +
		"- send_file — Attach the published file [已列入本轮计划，先等 generate_pdf。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	rowText, ok := lift(t, rows, nil, []string{"generate_pdf"}, nil)
	if !ok {
		t.Fatal("a called generate_pdf must retarget the send_file row ban")
	}
	if !strings.Contains(rowText, "- send_file — Attach the published file [已列入本轮计划，generate_pdf 已调用。不要直接调用 send_file，也不要请愿。]") {
		t.Fatalf("send_file row still treats generate_pdf as absent, or the ban has no subject:\n%s", rowText)
	}
	if strings.Contains(rowText, "先等 generate_pdf") || strings.Contains(rowText, "generate_pdf 已在当前工具列表") || strings.Contains(rowText, "不要直接调用，也不要请愿") {
		t.Fatalf("called generate_pdf was described as listed or the ban stayed anonymous:\n%s", rowText)
	}

	later := semanticToolsSearchLaterClause("archive_file", predecessorNames{
		nextRequest: []string{"generate_pdf", "office_export"},
	}, false)
	listPage := "tools_search results for \"pdf\":\n本轮决定：已计划但本请求尚未列出：" + later + "。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n下一请求出现：generate_pdf。现在不要调用。\n"
	listText, ok := lift(t, listPage, nil, []string{"generate_pdf"}, nil)
	if !ok {
		t.Fatal("a called name inside a next-request list must lift")
	}
	if !strings.Contains(listText, "generate_pdf 已调用，office_export 出现在下一请求后再调用") {
		t.Fatalf("called generate_pdf stayed in the absent list, or office_export lost its wait:\n%s", listText)
	}
	if strings.Contains(listText, "generate_pdf 已在当前工具列表") || strings.Contains(listText, "office_export 已调用") || strings.Contains(listText, "generate_pdf、office_export") {
		t.Fatalf("the next-request list was rewritten as one fact:\n%s", listText)
	}
}

// A later assistant call whose tool result is a planned denial did not run.
// Once generate_pdf is actually listed, that page still orders it. A result
// that is not a denial is the call, and the held denial's "It appears" dies.
func TestLiftExpiredDirectoryHoldSeparatesDeniedCallFromExecution(t *testing.T) {
	const beijing = "tools_search results for \"生成北京天气PDF\":\n" +
		"scope_id=semantic-scope:a5748ba1f335cd4d1141c5f1ff96d7e303e15272ccaae77a0dfa7214f3849c9a catalog_digest=catalog:sha256:93790b07e3b20e9e4a3a7b140432c2219e3af3797afe3f81ba9216716fef3a3d decision_id=decision:1d74fdb526559435b5f832d93863478f3e727317c5803864499cef5b52b209a8\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）；send_file（generate_pdf 出现在下一请求后再调用；send_file 要等它自己出现在列表）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF and publish an Artif… [已列入本轮计划。本响应的其它工具调用结束后，下一请求会出现。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	denied := plannedToolHeldForNextRequestMessage("generate_pdf")
	conv := []interface{}{
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "tools_search"}}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": beijing},
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{ID: "call-pdf", Function: llm.ToolCallFunction{Name: "generate_pdf"}}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call-pdf", "content": denied},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed generate_pdf must be ordered when the later call was denied")
	}
	if conv[3].(map[string]interface{})["content"] != denied {
		t.Fatal("stored denial was rewritten")
	}
	pageText, _ := got[1].(map[string]interface{})["content"].(string)
	denialText, _ := got[3].(map[string]interface{})["content"].(string)
	if !strings.Contains(pageText, "立刻调用 generate_pdf。") || !strings.Contains(pageText, "现在调用 generate_pdf。") {
		t.Fatalf("denied generate_pdf was not ordered once listed:\n%s", pageText)
	}
	if strings.Contains(pageText, "[已调用。]") || strings.Contains(pageText, "generate_pdf 已调用") {
		t.Fatalf("denied generate_pdf was described as already called:\n%s", pageText)
	}
	if !strings.Contains(denialText, `tool "generate_pdf" is listed on this request. Call it now.`) {
		t.Fatalf("denied call was not ordered from the denial:\n%s", denialText)
	}
	if strings.Contains(denialText, "has already been called") || strings.Contains(denialText, "It appears") || strings.Contains(denialText, "before it is listed") {
		t.Fatalf("denied call kept the appearance promise:\n%s", denialText)
	}

	executed := []interface{}{
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "generate_pdf"}}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call-pdf", "content": denied},
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{ID: "call-pdf-2", Function: llm.ToolCallFunction{Name: "generate_pdf"}}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call-pdf-2", "content": "rendered the pdf"},
	}
	execWire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "send_file"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	execGot := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(executed, execWire)
	if execGot == nil {
		t.Fatal("a generate_pdf call that returned a result must clear the appearance promise")
	}
	if executed[1].(map[string]interface{})["content"] != denied {
		t.Fatal("stored denial was rewritten")
	}
	cleared, _ := execGot[1].(map[string]interface{})["content"].(string)
	if cleared != `Error: tool "generate_pdf" has already been called.` {
		t.Fatalf("executed generate_pdf kept the next-request denial:\n%s", cleared)
	}

	rejectedAgain := plannedToolHeldForNextRequestMessage("generate_pdf")
	stillHeld := []interface{}{
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "generate_pdf"}}},
		},
		map[string]interface{}{"role": "tool", "content": denied},
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "generate_pdf"}}},
		},
		map[string]interface{}{"role": "tool", "content": rejectedAgain},
	}
	holdWire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	if got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(stillHeld, holdWire); got != nil {
		t.Fatalf("a later denied call must leave the appearance promise, got %#v", got)
	}
}

func TestLiftExpiredPlannedDenialWhenNameIsListed(t *testing.T) {
	denial := plannedToolHeldForNextRequestMessage("generate_pdf")
	stored := map[string]interface{}{"role": "tool", "tool_call_id": "call-pdf", "content": denial}
	conv := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				Function: llm.ToolCallFunction{Name: "generate_pdf"},
			}},
		},
		stored,
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("a listed generate_pdf must lift the petition denial")
	}
	if stored["content"] != denial {
		t.Fatal("stored denial was rewritten")
	}
	content, _ := got[1].(map[string]interface{})["content"].(string)
	if !strings.Contains(content, `tool "generate_pdf" is listed on this request. Call it now.`) {
		t.Fatalf("denial did not order the listed tool:\n%s", content)
	}
	if strings.Contains(content, "before it is listed") || strings.Contains(content, "not listed yet") || strings.Contains(content, "Do not call") {
		t.Fatalf("expired prohibition survived:\n%s", content)
	}
}

func TestLiftExpiredPlannedDenialBesideHoldPageDropsTheBan(t *testing.T) {
	const page = "tools_search results for \"pdf\":\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF [已列入本轮计划。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	denial := plannedToolHeldForNextRequestMessage("generate_pdf")
	storedDenial := map[string]interface{}{"role": "tool", "tool_call_id": "call-pdf", "content": denial}
	conv := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{
				{Function: llm.ToolCallFunction{Name: "generate_pdf"}},
				{Function: llm.ToolCallFunction{Name: "tools_search"}},
			},
		},
		storedDenial,
		map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed generate_pdf must lift the page and the denial")
	}
	if storedDenial["content"] != denial {
		t.Fatal("stored denial was rewritten")
	}
	denialText, _ := got[1].(map[string]interface{})["content"].(string)
	pageText, _ := got[2].(map[string]interface{})["content"].(string)
	if strings.Contains(denialText, "before it is listed") || strings.Contains(denialText, "Do not call") || strings.Contains(denialText, "Call it now") {
		t.Fatalf("denial before the hold page must only drop the ban:\n%s", denialText)
	}
	if !strings.Contains(denialText, "is listed on this request") {
		t.Fatalf("denial still describes generate_pdf as absent:\n%s", denialText)
	}
	if !strings.Contains(pageText, "立刻调用 generate_pdf。") || !strings.Contains(pageText, "现在调用 generate_pdf。") {
		t.Fatalf("hold page did not order generate_pdf:\n%s", pageText)
	}
}

func TestLiftExpiredPlannedDenialDoesNotReorderACalledName(t *testing.T) {
	denial := plannedToolNotYetListedMessage("generate_pdf", nil)
	conv := []interface{}{
		map[string]interface{}{"role": "tool", "content": denial},
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				Function: llm.ToolCallFunction{Name: "generate_pdf"},
			}},
		},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("a listed name must still lose the expired prohibition")
	}
	content, _ := got[0].(map[string]interface{})["content"].(string)
	if strings.Contains(content, "Call it now") || strings.Contains(content, "not listed yet") || strings.Contains(content, "call it then") {
		t.Fatalf("a name already called must not be ordered again:\n%s", content)
	}
}

// send_file was rejected while web_search was still the listed step. After
// web_search has run and generate_pdf is on the wire list, that denial still
// says to call web_search and that generate_pdf appears on the next request.
func TestLiftExpiredPlannedDenialRelaxesListedPredecessor(t *testing.T) {
	denial := plannedToolListedAfterEarlierStepsMessage("send_file", []string{"web_search"}, []string{"generate_pdf"})
	stored := map[string]interface{}{"role": "tool", "tool_call_id": "call-send", "content": denial}
	conv := []interface{}{
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "send_file"}}},
		},
		stored,
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "web_search"}}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": "北京今天晴"},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("a listed generate_pdf inside a send_file denial must be lifted")
	}
	if stored["content"] != denial {
		t.Fatal("stored denial was rewritten")
	}
	content, _ := got[1].(map[string]interface{})["content"].(string)
	if strings.Contains(content, `Call "web_search" now`) || strings.Contains(content, "appears on the next request") {
		t.Fatalf("spent lookup or the next-request claim survived:\n%s", content)
	}
	if !strings.Contains(content, `Call "generate_pdf" now.`) {
		t.Fatalf("listed generate_pdf was not ordered:\n%s", content)
	}
	if !strings.Contains(content, `Call "send_file" only after "send_file" is listed.`) {
		t.Fatalf("send_file lost its hold:\n%s", content)
	}
	if strings.Contains(content, "Do not call") {
		t.Fatalf("a listed predecessor kept a call ban:\n%s", content)
	}
}

func TestLiftExpiredPlannedDenialPredecessorBesideHoldPage(t *testing.T) {
	const page = "tools_search results for \"pdf\":\n" +
		"本轮决定：已计划但本请求尚未列出：generate_pdf（本响应的其它调用结束后出现在下一请求）。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n" +
		"- generate_pdf — Render Markdown into a PDF [已列入本轮计划。不要直接调用，也不要请愿。]\n" +
		"下一请求出现：generate_pdf。现在不要调用。\n"
	denial := plannedToolListedAfterEarlierStepsMessage("send_file", []string{"web_search"}, []string{"generate_pdf"})
	storedDenial := map[string]interface{}{"role": "tool", "tool_call_id": "call-send", "content": denial}
	conv := []interface{}{
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "web_search"}}},
		},
		map[string]interface{}{"role": "tool", "content": "北京今天晴"},
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{
				{Function: llm.ToolCallFunction{Name: "send_file"}},
				{Function: llm.ToolCallFunction{Name: "tools_search"}},
			},
		},
		storedDenial,
		map[string]interface{}{"role": "tool", "tool_call_id": "call-search", "content": page},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "generate_pdf"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("listed generate_pdf must lift the denial and the hold page")
	}
	if storedDenial["content"] != denial {
		t.Fatal("stored denial was rewritten")
	}
	denialText, _ := got[3].(map[string]interface{})["content"].(string)
	pageText, _ := got[4].(map[string]interface{})["content"].(string)
	if strings.Contains(denialText, "appears on the next request") || strings.Contains(denialText, `Call "generate_pdf" now`) || strings.Contains(denialText, `Call "web_search" now`) {
		t.Fatalf("denial before the hold page must only correct the stale fact:\n%s", denialText)
	}
	if !strings.Contains(denialText, `"generate_pdf" is listed on this request.`) {
		t.Fatalf("denial still describes generate_pdf as absent:\n%s", denialText)
	}
	if !strings.Contains(denialText, `Call "send_file" only after "send_file" is listed.`) {
		t.Fatalf("send_file lost its hold:\n%s", denialText)
	}
	if !strings.Contains(pageText, "立刻调用 generate_pdf。") || !strings.Contains(pageText, "现在调用 generate_pdf。") {
		t.Fatalf("hold page did not order generate_pdf:\n%s", pageText)
	}
}

func TestLiftExpiredPlannedDenialDropsSpentPredecessorOnly(t *testing.T) {
	denial := plannedToolListedAfterEarlierStepsMessage("send_file", []string{"web_search"}, []string{"generate_pdf"})
	conv := []interface{}{
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "send_file"}}},
		},
		map[string]interface{}{"role": "tool", "content": denial},
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "web_search"}}},
		},
		map[string]interface{}{"role": "tool", "content": "北京今天晴"},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("a spent web_search order must leave the denial")
	}
	content, _ := got[1].(map[string]interface{})["content"].(string)
	if strings.Contains(content, `Call "web_search" now`) || strings.Contains(content, `Call "generate_pdf" now`) {
		t.Fatalf("spent lookup was ordered again, or unlisted generate_pdf was ordered:\n%s", content)
	}
	if !strings.Contains(content, `"generate_pdf" appears on the next request`) {
		t.Fatalf("unlisted generate_pdf lost its next-request fact:\n%s", content)
	}
	if !strings.Contains(content, `Call "send_file" only after "send_file" is listed.`) {
		t.Fatalf("send_file lost its hold:\n%s", content)
	}
}

func TestLiftExpiredPlannedDenialKeepsTheUnspentPredecessor(t *testing.T) {
	denial := plannedToolNotYetListedMessage("generate_pdf", []string{"web_search", "web_fetch"})
	conv := []interface{}{
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "generate_pdf"}}},
		},
		map[string]interface{}{"role": "tool", "content": denial},
		map[string]interface{}{
			"role":       "assistant",
			"tool_calls": []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "web_search"}}},
		},
		map[string]interface{}{"role": "tool", "content": "北京今天晴"},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "web_fetch"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire)
	if got == nil {
		t.Fatal("a spent web_search beside a live web_fetch must leave the denial")
	}
	content, _ := got[1].(map[string]interface{})["content"].(string)
	if strings.Contains(content, "web_search") || strings.Contains(content, `Call "generate_pdf" now`) {
		t.Fatalf("spent lookup survived, or unlisted generate_pdf was ordered:\n%s", content)
	}
	if !strings.Contains(content, `Call "web_fetch" now.`) {
		t.Fatalf("live web_fetch order was dropped:\n%s", content)
	}
	if !strings.Contains(content, `Call "generate_pdf" only after "generate_pdf" is listed.`) {
		t.Fatalf("generate_pdf lost its hold:\n%s", content)
	}
}

func TestLiftExpiredPlannedDenialLeavesLivePredecessorOrder(t *testing.T) {
	denial := plannedToolListedAfterEarlierStepsMessage("send_file", []string{"web_search"}, []string{"generate_pdf"})
	conv := []interface{}{
		map[string]interface{}{"role": "tool", "content": denial},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	if got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire); got != nil {
		content, _ := got[0].(map[string]interface{})["content"].(string)
		t.Fatalf("a live web_search order must stay while generate_pdf is unlisted:\n%s", content)
	}
}

func TestLiftExpiredPlannedDenialLeavesUnlistedTool(t *testing.T) {
	denial := plannedToolHeldForNextRequestMessage("generate_pdf")
	conv := []interface{}{
		map[string]interface{}{"role": "tool", "content": denial},
		map[string]interface{}{"role": "tool", "content": `Error: tool "generate_pdf" failed to render.`},
	}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	if got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire); got != nil {
		t.Fatalf("an unlisted name and an unrelated error must stay, got %#v", got)
	}
}

func TestLiftExpiredToolsSearchHoldLeavesLivePredecessorOrder(t *testing.T) {
	page := semanticToolsSearchWeatherPredecessorPage(t)
	conv := []interface{}{map[string]interface{}{"role": "tool", "content": page}}
	wire := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "web_search"}},
		{"type": "function", "function": map[string]interface{}{"name": "tools_search"}},
	}
	if got := (&sharedAgentLoopCallbacks{}).LiftExpiredDirectoryHolds(conv, wire); got != nil {
		content, _ := got[0].(map[string]interface{})["content"].(string)
		t.Fatalf("a live web_search order must stay until generate_pdf is listed or the lookup is spent:\n%s", content)
	}
}

func semanticToolsSearchWeatherPredecessorPage(t *testing.T) string {
	t.Helper()
	cb := semanticToolsSearchRegressionCallbacks(0)
	cb.semanticSurface.plan.Selections = []tool.PlannedSelection{
		{ID: "search", AdapterName: "web_search", FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
		{ID: "pdf", AdapterName: "host_document_generate_file", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}, Requires: []string{"search"}},
		{ID: "deliver", AdapterName: "semantic_deliver_current_file", FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel"}, Requires: []string{"pdf"}},
	}
	cb.semanticSurface.grants["web_search"] = tool.InvocationGrant{SelectionID: "search", Token: "search-token", AdapterName: "web_search"}
	got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"pdf"}`)
	if !strings.Contains(got, "现在调用 web_search") || !strings.Contains(got, "不要直接调用这些名字") || !strings.Contains(got, "generate_pdf（") {
		t.Fatalf("predecessor page shape changed:\n%s", got)
	}
	if _, _, ok := directoryHoldCloser(got); ok {
		t.Fatalf("predecessor page used the next-request closer:\n%s", semanticToolsSearchLastLine(got))
	}
	return got
}

func semanticToolsSearchLastLine(page string) string {
	lines := strings.Split(strings.TrimRight(page, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func semanticSearchAllPageNames(t *testing.T, cb *sharedAgentLoopCallbacks, query string) []string {
	t.Helper()
	args := `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"` + query + `"}`
	var names []string
	seen := map[string]bool{}
	for page := 0; page < 40; page++ {
		got := semanticToolsSearchRun(cb, args)
		if len(got) > agent.ToolsSearchMaxToolResult {
			t.Fatalf("page %d is %d bytes, over the inline budget %d", page, len(got), agent.ToolsSearchMaxToolResult)
		}
		names = append(names, semanticSearchResultNames(got)...)
		token := semanticSearchNextPageToken(got)
		if token == "" {
			return names
		}
		if seen[token] {
			t.Fatalf("page token repeated: %s", token)
		}
		seen[token] = true
		args = `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"` + query + `","page_token":"` + token + `"}`
	}
	t.Fatal("pagination did not finish")
	return nil
}

func semanticSearchNextPageToken(result string) string {
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "next_page_token=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "next_page_token="))
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// A cut page must still publish every hidden name. Existence is what the
// model uses to decide "the write API is absent": post_work_log_add sat on
// page 2 while the turn reported 腾云没有写接口 (production 2026-11-11).
// Shown rows plus the unshown-name index must cover the whole directory.
func TestSemanticToolsSearchCutPageListsEveryHiddenName(t *testing.T) {
	cb := semanticToolsSearchRegressionCallbacks(60)
	got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","catalog_digest":"catalog-search","query":"list everything"}`)
	if len(got) > agent.ToolsSearchMaxToolResult {
		t.Fatalf("page is %d bytes, budget %d", len(got), agent.ToolsSearchMaxToolResult)
	}
	if !strings.Contains(got, "next_page_token=") {
		t.Fatalf("fixture must cut the page: %s", got)
	}
	marker := "本页未列出的名字还有 "
	idx := strings.Index(got, marker)
	if idx < 0 {
		t.Fatalf("cut page hides its remaining names:\n%s", got)
	}
	indexLine := got[idx:]
	if end := strings.IndexByte(indexLine, '\n'); end >= 0 {
		indexLine = indexLine[:end]
	}
	if !strings.Contains(indexLine, "不要断言该能力不存在") {
		t.Fatalf("index line must forbid an absent-capability conclusion: %s", indexLine)
	}
	shown := map[string]bool{}
	for _, name := range semanticSearchResultNames(got) {
		shown[name] = true
	}
	indexed := map[string]bool{}
	for _, token := range strings.FieldsFunc(indexLine, func(r rune) bool {
		return r == '、' || r == ' ' || r == '：' || r == '。' || r == '，'
	}) {
		token = strings.TrimSpace(token)
		if strings.HasPrefix(token, "dynamic_") || strings.Contains(token, "__") {
			indexed[token] = true
		}
	}
	all := uniqueStrings(semanticSearchAllPageNames(t, semanticToolsSearchRegressionCallbacks(60), "list everything"))
	for _, name := range all {
		if shown[name] {
			continue
		}
		if !indexed[name] {
			t.Fatalf("name %q is neither a shown row nor in the hidden-name index on page 1:\n%s", name, indexLine)
		}
	}
}
