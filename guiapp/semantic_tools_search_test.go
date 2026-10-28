package guiapp

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
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
	if !strings.Contains(got, "bash") || !strings.Contains(got, "[已在当前工具面]") {
		t.Fatalf("bash must be discoverable as a listed baseline tool: %s", got)
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
	needs := []string{"web_fetch", "bash", "office", "download_file"}
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
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
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
	second := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"fetch models","needs":["web_fetch"]}`)
	if !strings.Contains(second, "already returned") || strings.Contains(second, "web_fetch —") {
		t.Fatalf("repeated directory must close without another catalog: %s", second)
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
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
	got := semanticToolsSearchRun(cb, `{"query":"生成ppt幻灯片"}`)
	if !strings.Contains(got, "office") || !strings.Contains(got, "[可请愿：直接调用一次]") {
		t.Fatalf("unrouted petitionable office must be marked petitionable: %s", got)
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
	if !strings.Contains(got, "- mailbox_delete — ") || !strings.Contains(got, "[可请愿：直接调用一次]") {
		t.Fatalf("mailbox_delete needs lookup did not resolve: %s", got)
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

// The hand-built chain above matches the denial contract. This one uses the
// desktop planner's weather-PDF surface, including delivery behind generate.
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
	now := ""
	if at := strings.Index(decisionLine, "现在调用 "); at >= 0 {
		now = decisionLine[at:]
		if end := strings.Index(now, "。"); end >= 0 {
			now = now[:end]
		}
	}
	if searchName == "" || !strings.Contains(now, searchName) {
		t.Fatalf("call-now=%q want %s in %s", now, searchName, decisionLine)
	}
	for _, baseline := range []string{"bash", "read_file", "write_file"} {
		if strings.Contains(now, baseline) {
			t.Fatalf("baseline %s is not the weather predecessor: %s", baseline, now)
		}
	}
	if !strings.Contains(decisionLine, "generate_pdf（先调用 "+searchName) {
		t.Fatalf("generate clause missing: %s", decisionLine)
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
		t.Fatalf("real plan promised delivery on the next request: %s", sendClause)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- send_file — ") && strings.Contains(line, "自动出现") {
			t.Fatalf("delivery row promised automatic appearance: %s", line)
		}
	}
	if strings.Contains(got, "仍可直接调用一次") || strings.Contains(got, "直接调用一次") {
		t.Fatalf("petition invitation must not follow the weather decision: %s", got)
	}
	nowSentence := ""
	if at := strings.Index(decisionLine, "现在调用 "); at >= 0 {
		nowSentence = decisionLine[at:]
		if end := strings.Index(nowSentence, "。"); end >= 0 {
			nowSentence = nowSentence[:end+len("。")]
		}
	}
	if nowSentence == "" || semanticToolsSearchLastLine(got) != nowSentence {
		t.Fatalf("closing line = %q, want %q", semanticToolsSearchLastLine(got), nowSentence)
	}
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
