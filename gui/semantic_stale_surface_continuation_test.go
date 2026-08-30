package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// Production 2026-08-28 PPT turn: the model batched several web_fetch calls
// (distinct URLs — pulling candidate photos is exactly the fetch family's
// job). The first call succeeded, advanceSemanticToolSurface invalidated the
// request epoch while issuing sibling #2 under the SAME stable name, and the
// rest of the batch died on stale_surface. Four rejections and the model's
// re-issues burned the turn's budget on pure churn.
//
// The stale-epoch fence exists to stop a late response from binding a stable
// name to a SUCCESSOR materialization. A batched call that arrives after its
// sibling succeeded is not that shape: the name is still live, and the grant
// it would bind belongs to the same repeat family the name held when the
// epoch was issued, on the same route revision. That call may continue; every
// other stale shape stays rejected (pinned below).
func TestSemanticStaleEpochSameFamilyBatchContinuationExecutes(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelWebFetch)}
	h.semanticTrustedWebFetch = func(userID, url string) (string, error) {
		return "Fetched web evidence.\nTitle: Example\nURL: " + url, nil
	}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "抓取这几个链接的内容", "lansenger", "root-batch-fetch", "turn-batch-fetch", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}

	// One model request (epoch) issues a batch of three fetch calls; the core
	// loop executes them sequentially under that same epoch.
	epoch := cb.BeginToolSurfaceEpoch(0)
	if epoch == "" {
		t.Fatal("no surface epoch")
	}
	for i, url := range []string{"https://example.com/1", "https://example.com/2", "https://example.com/3"} {
		got := cb.ExecuteToolCallWithContext("web_fetch", `{"url":"`+url+`"}`, "call-batch-"+string(rune('1'+i)), agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
		if strings.Contains(got.Result, "stale_surface") {
			t.Fatalf("batched fetch %d rejected as stale: %#v", i+1, got)
		}
		if !strings.Contains(got.Result, "Fetched web evidence.") {
			t.Fatalf("batched fetch %d did not execute: %#v", i+1, got)
		}
	}
	// The continuation spent real siblings: after five executions the family
	// is exhausted and the stable name is gone from the live surface.
	for i, url := range []string{"https://example.com/4", "https://example.com/5"} {
		got := cb.ExecuteToolCallWithContext("web_fetch", `{"url":"`+url+`"}`, "call-batch-"+string(rune('4'+i)), agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
		if !strings.Contains(got.Result, "Fetched web evidence.") {
			t.Fatalf("batched fetch %d did not execute: %#v", i+4, got)
		}
	}
	if _, live := surface.grants["web_fetch"]; live {
		t.Fatal("web_fetch must leave the live surface once the family budget is spent")
	}
}

// A late batch call whose family had no successor sibling — the spent grant
// was the family's last — must stay rejected: the name no longer holds a live
// grant, so there is nothing for the stale call to bind.
func TestSemanticStaleEpochRetiredNameBatchCallStillRejected(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelCurrentTime)}
	h.semanticTrustedClock = func(userID string) (string, error) {
		return "2026-08-28 Friday 10:00:00 (timezone: Local)", nil
	}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "现在几点了", "lansenger", "root-batch-clock", "turn-batch-clock", currentTimeClassification(),
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}
	epoch := cb.BeginToolSurfaceEpoch(0)
	first := cb.ExecuteToolCallWithContext("current_datetime", `{}`, "call-clock-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(first.Result, "2026-08-28") {
		t.Fatalf("first clock call did not execute: %#v", first)
	}
	second := cb.ExecuteToolCallWithContext("current_datetime", `{}`, "call-clock-2", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(second.Result, "stale_surface") {
		t.Fatalf("batched call on a retired name must stay rejected: %#v", second)
	}
}

// The continuation channel closes the moment the next model request begins:
// a straggler from the previous response arriving after a successor epoch was
// issued must not bind the live grant a second time — the name may still be
// live and the family may still match, but the surface has moved on.
func TestSemanticStaleEpochAfterSuccessorRequestStillRejected(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelWebFetch)}
	calls := 0
	h.semanticTrustedWebFetch = func(userID, url string) (string, error) {
		calls++
		return "Fetched web evidence.\nURL: " + url, nil
	}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "抓取这几个链接的内容", "lansenger", "root-batch-next", "turn-batch-next", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}
	epochA := cb.BeginToolSurfaceEpoch(0)
	first := cb.ExecuteToolCallWithContext("web_fetch", `{"url":"https://example.com/1"}`, "call-next-1", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if !strings.Contains(first.Result, "Fetched web evidence.") {
		t.Fatalf("first fetch did not execute: %#v", first)
	}
	// The next model request begins before a duplicate of the first response's
	// batch drains.
	epochB := cb.BeginToolSurfaceEpoch(1)
	if epochB == "" || epochB == epochA {
		t.Fatalf("successor epoch did not begin: %q vs %q", epochA, epochB)
	}
	late := cb.ExecuteToolCallWithContext("web_fetch", `{"url":"https://example.com/1"}`, "call-next-late", agent.ToolCallExecutionContext{SurfaceEpoch: epochA})
	if !strings.Contains(late.Result, "stale_surface") {
		t.Fatalf("straggler from a superseded request must stay rejected: %#v", late)
	}
	if calls != 1 {
		t.Fatalf("the straggler must not reach the adapter: calls=%d", calls)
	}
}

// Cross-family rebinding stays rejected: if the stable name's live grant
// belongs to a different repeat family than the one the name held when the
// epoch was issued, the late call is exactly the "bind a stable name to a
// successor materialization" shape the fence exists for. (Within one
// published plan a name cannot rebind across families — one family per
// capability is pinned by the archetype dedup — so this white-boxes the grant
// table to prove the gate itself enforces it.)
func TestSemanticStaleEpochCrossFamilyRebindingStillRejected(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelWebFetch)}
	calls := 0
	h.semanticTrustedWebFetch = func(userID, url string) (string, error) {
		calls++
		return "Fetched web evidence.\nURL: " + url, nil
	}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "抓取这几个链接的内容", "lansenger", "root-batch-xfam", "turn-batch-xfam", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}
	epoch := cb.BeginToolSurfaceEpoch(0)
	first := cb.ExecuteToolCallWithContext("web_fetch", `{"url":"https://example.com/1"}`, "call-xfam-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(first.Result, "Fetched web evidence.") {
		t.Fatalf("first fetch did not execute: %#v", first)
	}
	// Rebind the live name to a grant of a DIFFERENT family.
	grant, ok := surface.grants["web_fetch"]
	if !ok {
		t.Fatal("sibling grant was not issued after the first success")
	}
	foreign := ""
	for _, selection := range surface.plan.Selections {
		if tool.RepeatFamilyID(selection.ID) != tool.RepeatFamilyID(grant.SelectionID) {
			foreign = selection.ID
			break
		}
	}
	if foreign == "" {
		t.Fatal("fixture plan must carry a second family")
	}
	grant.SelectionID = foreign
	surface.grants["web_fetch"] = grant
	late := cb.ExecuteToolCallWithContext("web_fetch", `{"url":"https://example.com/2"}`, "call-xfam-2", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(late.Result, "stale_surface") {
		t.Fatalf("cross-family rebinding must stay rejected: %#v", late)
	}
	if calls != 1 {
		t.Fatalf("the cross-family call must not reach the adapter: calls=%d", calls)
	}
}

// Cross-revision stays rejected: a petition publishes a child revision and
// replaces the surface. The stable name and even the family identity survive
// on the child, but the old epoch was never issued there — a late call from
// the parent revision's batch must not bind the child's grant.
func TestSemanticStaleEpochCrossRevisionBatchCallStillRejected(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	parent := cb.semanticSurface
	if name := semanticGrantNameForAdapter(parent, semanticTrustedWebSearchAdapter); name != "web_search" {
		t.Fatalf("fixture must render web_search, got %q", name)
	}
	epoch := cb.BeginToolSurfaceEpoch(0)
	first := cb.ExecuteToolCallWithContext("web_search", `{"query":"ragdoll kitten"}`, "call-xrev-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(first.Result, "found: ragdoll kitten") {
		t.Fatalf("first search did not execute: %#v", first)
	}
	granted, message := cb.PetitionToolCall("current_datetime")
	if !granted {
		t.Fatalf("fixture petition must publish a child revision: %q", message)
	}
	if cb.semanticSurface == parent {
		t.Fatal("petition did not replace the surface")
	}
	late := cb.ExecuteToolCallWithContext("web_search", `{"query":"ragdoll cat photo"}`, "call-xrev-2", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(late.Result, "stale_surface") {
		t.Fatalf("late call from the parent revision must stay rejected: %#v", late)
	}
}

// tools_search carries no grant and no authority — it is a discovery
// meta-tool bounded by its own per-turn counter. Rejecting it as
// stale_surface after a batched sibling's success (production 2026-08-28 PPT
// turn: send_file + tools_search in one response, the tools_search half died
// on stale_surface) is pure churn: nothing is consumed, nothing is bound.
func TestSemanticStaleEpochToolsSearchDiscoveryStillRuns(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelWebFetch)}
	h.semanticTrustedWebFetch = func(userID, url string) (string, error) {
		return "Fetched web evidence.\nURL: " + url, nil
	}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "抓取这几个链接的内容", "lansenger", "root-batch-ts", "turn-batch-ts", webFetchClassification(),
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, userID: "user-1"}
	epoch := cb.BeginToolSurfaceEpoch(0)
	first := cb.ExecuteToolCallWithContext("web_fetch", `{"url":"https://example.com/1"}`, "call-ts-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if !strings.Contains(first.Result, "Fetched web evidence.") {
		t.Fatalf("first fetch did not execute: %#v", first)
	}
	// The discovery meta-tool in the same retired-epoch batch must still run.
	discovery := cb.ExecuteToolCallWithContext(semanticToolsSearchName, `{"query":"download_file"}`, "call-ts-2", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
	if strings.Contains(discovery.Result, "stale_surface") {
		t.Fatalf("grant-less discovery meta-tool rejected as stale: %#v", discovery)
	}
	if !strings.Contains(discovery.Result, "download_file") {
		t.Fatalf("discovery did not run: %#v", discovery)
	}
}
