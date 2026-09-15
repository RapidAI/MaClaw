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
// and without burning one.
func TestSemanticToolsSearchExecutesThroughBothEntries(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	if got := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"pdf"}`); !strings.Contains(got, "generate_pdf") {
		t.Fatalf("executeSemanticTool: %s", got)
	}
	if got := cb.executeSemanticToolCallWithEpoch(semanticToolsSearchName, `{"query":"pdf"}`, "call-ts", ""); !strings.Contains(got, "generate_pdf") {
		t.Fatalf("executeSemanticToolCallWithEpoch: %s", got)
	}
}

// Discovery is a helper, not a way of life: after the turn budget the host
// answers with a deterministic redirect instead of more results, ending the
// burned-grant spiral observed in production. The counter is shared across
// both execution entries and never touches grants.
func TestSemanticToolsSearchTurnBudgetEndsSpiral(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	for i := 0; i < semanticToolsSearchMaxPerTurn; i++ {
		entry := i % 2
		var got string
		if entry == 0 {
			got = cb.executeSemanticTool(semanticToolsSearchName, `{"query":"pdf"}`)
		} else {
			got = cb.executeSemanticToolCallWithEpoch(semanticToolsSearchName, `{"query":"pdf"}`, "call-ts", "")
		}
		if !strings.Contains(got, "generate_pdf") {
			t.Fatalf("call %d within budget must answer: %s", i+1, got)
		}
	}
	got := cb.executeSemanticTool(semanticToolsSearchName, `{"query":"pdf"}`)
	if !strings.Contains(got, "no longer available") || strings.Contains(got, "generate_pdf") {
		t.Fatalf("over-budget call must redirect without results: %s", got)
	}
	if len(cb.semanticSurface.grants) == 0 {
		t.Fatal("discovery budget must not consume grants")
	}
}

// English discovery queries must hit the inventory too: a production turn
// asked "weather nanjing forecast" and got "(no matching capability)",
// blocking the petition self-rescue that the result would have suggested.
func TestSemanticToolsSearchMatchesEnglishQueries(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	cases := map[string]string{
		"weather nanjing forecast": "web_search",
		"find cat photos online":   "web_search",
		"wikipedia article":        "web_fetch",
		"download cat picture":     "download_file",
	}
	for query, want := range cases {
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
	if !strings.Contains(got, "bash") || !strings.Contains(got, "[已在当前工具面]") {
		t.Fatalf("baseline bash must stay listed after the petition budget is spent: %s", got)
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
	if got := semanticToolsSearchRun(cb, `{"scope_id":"scope-search","query":"list"}`); !strings.Contains(got, "tools_search_scope_identity_required") {
		t.Fatalf("partial scope identity accepted: %s", got)
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
	left := append(semanticSearchResultNames(first), semanticSearchResultNames(second)...)
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
