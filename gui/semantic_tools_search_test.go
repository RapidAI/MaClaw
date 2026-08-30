package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
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
// bash is petitionable.
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
	if !strings.Contains(got, "bash") || !strings.Contains(got, "[可请愿：直接调用一次]") {
		t.Fatalf("bash must be discoverable as petitionable: %s", got)
	}
	// Discovery is not authorization: no grant is minted by a query.
	if _, ok := cb.semanticSurface.grants["bash"]; ok {
		t.Fatal("discovery must not mint a grant")
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
	if !strings.Contains(got, "[本轮请愿机会已用完，不要调用]") {
		t.Fatalf("spent petition budget must be stated: %s", got)
	}
	got = semanticToolsSearchRun(cb, `{"query":"生成ppt幻灯片"}`)
	if !strings.Contains(got, "[本轮请愿机会已用完，不要调用]") {
		t.Fatalf("spent effectful budget must be stated for office too: %s", got)
	}
}

// A name whose grant was retired must not be advertised as petitionable: the
// petition gate rejects retired names, so discovery inviting the call would
// send the model into the same hard denial twice.
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
