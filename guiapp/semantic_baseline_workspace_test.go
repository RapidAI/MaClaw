package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestFailureReplanPlanContextForcesStoredBaselineOnly(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98}
	shell := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98}
	forced := semanticFailureReplanPlanContext(context.Background(), semanticReplanInput{
		BaselineWorkspace: true, Classification: search,
	}, intent.LabelShellCommand)
	if !semanticApplyBaselineWorkspace(forced, search, false) {
		t.Fatal("a stored ceiling must stay on when the classification would not re-derive it")
	}
	plain := semanticFailureReplanPlanContext(context.Background(), semanticReplanInput{
		Classification: search,
	}, intent.LabelSearch)
	if semanticApplyBaselineWorkspace(plain, search, false) {
		t.Fatal("a replan that never recorded a ceiling must leave the classification in charge")
	}
	if semanticApplyBaselineWorkspace(forced, search, true) {
		t.Fatal("slim office must still suppress the stored ceiling")
	}
	if !semanticApplyBaselineWorkspace(context.Background(), shell, false) {
		t.Fatal("shell classification still raises the ceiling when no replan flag is set")
	}
	petitionOff := withSemanticPetitionBaseline(withSemanticPetitionExpansion(forced), false)
	if semanticApplyBaselineWorkspace(petitionOff, shell, false) {
		t.Fatal("petition expansion must keep its own baseline bit")
	}
}

func TestFailureReplanKeepsStoredWorkspaceCeiling(t *testing.T) {
	cb := petitionTestShellCallbacks(t)
	surface := cb.semanticSurface
	if surface.replan == nil || !surface.replan.BaselineWorkspace {
		t.Fatal("shell plan must record the workspace ceiling")
	}
	if semanticPlanHasBaseline(surface.plan) {
		t.Fatal("a raised read ceiling keeps the archetype need id")
	}
	reads := countPlanCapability(surface.plan, tool.CapabilityFSReadLocal)
	if reads < 2 {
		t.Fatalf("shell baseline must raise read siblings, got %d", reads)
	}
	child, _, err := cb.handler.replanSemanticCallSurface(surface, "dynamic_binding_stale")
	if err != nil || child == nil || child.replan == nil || !child.replan.BaselineWorkspace {
		t.Fatalf("replan err=%v", err)
	}
	if got := countPlanCapability(child.plan, tool.CapabilityFSReadLocal); got != reads {
		t.Fatalf("failure replan changed the read ceiling from %d to %d", reads, got)
	}
}

func countPlanCapability(plan tool.ToolPlan, capability tool.CapabilityID) int {
	count := 0
	for _, selection := range plan.Selections {
		if selection.FitProof.MatchedCapability == capability {
			count++
		}
	}
	return count
}

func TestSemanticDocumentGenerateOmitsWorkspaceShell(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: .98})
	for _, adapter := range []string{semanticTrustedShellAdapter, semanticTrustedFileWriteAdapter} {
		if name := semanticGrantNameForAdapter(cb.semanticSurface, adapter); name != "" {
			t.Fatalf("pdf turn listed %s as %q", adapter, name)
		}
	}
	if len(cb.semanticSurface.grants) > 6 {
		t.Fatalf("pdf turn exposed %d tools, want at most 6: %#v", len(cb.semanticSurface.grants), cb.semanticSurface.grants)
	}
}

func TestSemanticSearchTurnKeepsBaselineWorkspaceToolsListed(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
	for _, adapter := range []string{semanticTrustedShellAdapter, semanticTrustedFileReadAdapter, semanticTrustedFileWriteAdapter} {
		if name := semanticGrantNameForAdapter(cb.semanticSurface, adapter); name != "" {
			t.Fatalf("search turn listed baseline %s as %q", adapter, name)
		}
	}
}

func TestLookupInheritedOfficeOmitsWorkspaceShell(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelOffice}, Confidence: .98,
	})
	if semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedWebSearchAdapter) == "" {
		t.Fatal("lookup must keep web_search")
	}
	if !planHasCapabilities(cb.semanticSurface.plan, tool.CapabilityDocumentWriteOffice) {
		t.Fatal("inherited office must stay on the plan")
	}
	for _, adapter := range []string{semanticTrustedShellAdapter, semanticTrustedFileWriteAdapter} {
		if name := semanticGrantNameForAdapter(cb.semanticSurface, adapter); name != "" {
			t.Fatalf("lookup that inherited office listed %s as %q", adapter, name)
		}
	}
	office := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	if semanticGrantNameForAdapter(office.semanticSurface, semanticTrustedShellAdapter) == "" {
		t.Fatal("an office turn must still list bash")
	}
	inherited := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelOffice}, Confidence: .98,
		Reason: "bare search; session residue",
	})
	if semanticGrantNameForAdapter(inherited.semanticSurface, semanticTrustedAcquireRemoteAdapter) != "" {
		t.Fatal("a residue lookup must not grow download_file from the office bundle")
	}
	if !planHasCapabilities(inherited.semanticSurface.plan, tool.CapabilityDocumentWriteOffice, "information.search.web") {
		t.Fatal("a residue lookup must keep office and search")
	}
	declared := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelOffice}, Confidence: .98,
	})
	if semanticGrantNameForAdapter(declared.semanticSurface, semanticTrustedAcquireRemoteAdapter) != "download_file" {
		t.Fatal("a declared office composite must still list download_file")
	}
}

func TestResidueShortDocumentEditOmitsOfficeBundle(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice)}
	h.semanticTrustedOfficeWrite = func(string, string, map[string]interface{}) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	documentTurn := &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98, Reason: "session residue document"}
	continuation := &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98, Reason: "session residue continuation"}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "改短一点", "desktop", "root-short-edit", "turn-short-edit", documentTurn,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedOfficeWriteAdapter) != "office" {
		t.Fatal("a short edit must keep the office tool")
	}
	for _, adapter := range []string{semanticTrustedShellAdapter, semanticTrustedFileWriteAdapter, semanticTrustedAcquireRemoteAdapter} {
		if name := semanticGrantNameForAdapter(surface, adapter); name != "" {
			t.Fatalf("short edit listed %s as %q", adapter, name)
		}
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedFileReadAdapter) == "" {
		t.Fatal("a short edit must keep read_file so the open document can be inspected")
	}
	if planHasCapability(surface.plan, tool.CapabilityKnowledgeReadLocal) || planHasCapability(surface.plan, tool.CapabilityMemoryRecallAgent) {
		t.Fatalf("short edit planned warehouse tools: %+v", surface.plan.Selections)
	}
	_, kept, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "继续", "desktop", "root-continue", "turn-continue", continuation,
	)
	if err != nil || !handled || kept == nil {
		t.Fatalf("continue handled=%v err=%v", handled, err)
	}
	_, asked, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "然后结论是什么", "desktop", "root-about", "turn-about", documentTurn,
	)
	if err != nil || !handled || asked == nil {
		t.Fatalf("about handled=%v err=%v", handled, err)
	}
	if semanticGrantNameForAdapter(asked, semanticTrustedOfficeWriteAdapter) != "office" {
		t.Fatal("the same document label keeps the office writer")
	}
	if semanticGrantNameForAdapter(asked, semanticTrustedShellAdapter) != "" || planHasCapability(asked.plan, tool.CapabilityKnowledgeReadLocal) {
		t.Fatalf("a document question grew workspace tools: %+v", asked.plan.Selections)
	}
	if semanticGrantNameForAdapter(asked, semanticTrustedFileReadAdapter) == "" {
		t.Fatal("a document question must keep read_file")
	}
	replayed, _, err := h.replanSemanticCallSurface(asked, "dynamic_binding_stale")
	if err != nil || replayed == nil {
		t.Fatalf("question replan: %v", err)
	}
	if semanticGrantNameForAdapter(replayed, semanticTrustedOfficeWriteAdapter) != "office" {
		t.Fatal("replanning a short document turn dropped the office writer")
	}
	if semanticGrantNameForAdapter(replayed, semanticTrustedFileReadAdapter) == "" {
		t.Fatal("replanning a document question dropped read_file")
	}
	_, petitionSurface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "然后结论是什么", "desktop", "root-petition", "turn-petition", documentTurn,
	)
	if err != nil || !handled || petitionSurface == nil {
		t.Fatalf("petition parent handled=%v err=%v", handled, err)
	}
	bashChild, _, err := h.petitionExpandSemanticCallSurface(context.Background(), petitionSurface, intent.LabelShellCommand)
	if err != nil || bashChild == nil {
		t.Fatalf("bash petition on a document question: %v", err)
	}
	if semanticGrantNameForAdapter(bashChild, semanticTrustedOfficeWriteAdapter) != "office" {
		t.Fatal("petitioning bash dropped the office writer")
	}
	if semanticGrantNameForAdapter(bashChild, semanticTrustedShellAdapter) == "" || semanticGrantNameForAdapter(bashChild, semanticTrustedFileReadAdapter) == "" {
		t.Fatal("petitioning bash must add bash and keep read_file")
	}
	if semanticGrantNameForAdapter(kept, semanticTrustedShellAdapter) == "" {
		t.Fatal("继续 on an office task must still list bash")
	}
	if !planHasCapability(kept.plan, tool.CapabilityKnowledgeReadLocal) || !planHasCapability(kept.plan, tool.CapabilityMemoryRecallAgent) {
		t.Fatalf("继续 must keep warehouse tools: %+v", kept.plan.Selections)
	}
	_, runAgain, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "然后再执行一下", "desktop", "root-run", "turn-run", continuation,
	)
	if err != nil || !handled || runAgain == nil {
		t.Fatalf("run again handled=%v err=%v", handled, err)
	}
	if semanticGrantNameForAdapter(runAgain, semanticTrustedShellAdapter) == "" {
		t.Fatal("然后再执行一下 must still list bash")
	}
	if semanticResidueSlimOfficeTurn(*documentTurn, "") {
		t.Fatal("a replan without the utterance must not slim the office surface")
	}
	_, replay, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "", "desktop", "root-replan", "turn-replan", documentTurn,
	)
	if err != nil || !handled || replay == nil {
		t.Fatalf("replan handled=%v err=%v", handled, err)
	}
	if semanticGrantNameForAdapter(replay, semanticTrustedOfficeWriteAdapter) != "office" || semanticGrantNameForAdapter(replay, semanticTrustedShellAdapter) == "" {
		t.Fatal("a replan must keep the office writer and bash")
	}
}

func TestSemanticBaselineBashSurvivesSpentEffectfulPetition(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98})
	cb.userText = "查看驱网服务器状态"
	if granted, message := cb.PetitionToolCall("office"); !granted {
		t.Fatalf("office petition should consume the effectful budget: granted=%v message=%q", granted, message)
	}
	if !cb.semanticEffectfulPetitionConsumed {
		t.Fatal("office petition must spend the effectful budget")
	}
	if name := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedShellAdapter); name != "bash" {
		t.Fatalf("bash must stay listed after memory spends the petition budget, got %q", name)
	}
	if granted, message := cb.PetitionToolCall("bash"); granted || message != "" {
		t.Fatalf("listed bash is not a petition: granted=%v message=%q", granted, message)
	}
	got := semanticToolsSearchRun(cb, `{"query":"run remote command ssh host server shell"}`)
	if !strings.Contains(got, "bash") || !strings.Contains(got, "[已在当前工具面]") {
		t.Fatalf("tools_search must keep bash listed, not spent: %s", got)
	}
}

func TestSemanticToolsSearchExpandsSSHWhenUserDeclaredIt(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSearch)}
	h.semanticTrustedWebSearch = func(userID, query string) (string, error) { return "found: " + query, nil }
	h.semanticTrustedSSH = func(userID, command string) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "查看驱网服务器状态，用ssh访问", "desktop", "root-search-ssh", "turn-search-ssh",
		&intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("search+ssh surface handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop", userText: "查看驱网服务器状态，用ssh访问"}
	got := semanticToolsSearchRun(cb, `{"query":"run remote command ssh host server shell"}`)
	if strings.Contains(got, "tools_search_scope") && strings.Contains(got, "rejected") {
		t.Fatalf("discovery must run: %s", got)
	}
	if !planHasCapabilities(cb.semanticSurface.plan, tool.CapabilityShellExecuteRemoteHost) && semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedSSHAdapter) == "" {
		t.Fatalf("user-declared ssh must expand the live plan: plan=%#v grants=%#v result=%s", cb.semanticSurface.plan.Selections, cb.semanticSurface.grants, got)
	}
}

func TestSemanticToolsSearchPetitionsUnboundSSHForConnect(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
	cb.userText = "查看驱网服务器状态，用ssh访问"
	semanticToolsSearchMaybeExpandScope(cb, "run remote command ssh host server shell")
	// Unbound ssh now publishes the connect provider, so discovery must spend
	// the effectful petition and expand the plan with the connect leg
	// (production 2026-09-18 08:26: without this the "密码是 …" turn had no
	// connect path at all).
	if !cb.semanticEffectfulPetitionConsumed {
		t.Fatal("unbound ssh discovery must spend the effectful petition on the connect surface")
	}
	if !planHasCapabilities(cb.semanticSurface.plan, tool.CapabilityShellExecuteRemoteHost) {
		t.Fatal("unbound ssh must enter the managed plan as a connect selection")
	}
}

func TestSemanticUserTextSupportsRemoteIgnoresEmbeddedHost(t *testing.T) {
	if semanticUserTextSupportsRemote("photoshop ghost story") {
		t.Fatal("ghost/host substrings must not count as remote intent")
	}
	if !semanticUserTextSupportsRemote("check the server logs") {
		t.Fatal("server token must count as remote intent")
	}
	if !semanticUserTextSupportsRemote("查看驱网服务器状态") {
		t.Fatal("服务器 must count as remote intent")
	}
}

func TestSemanticUserTextDeclaresSSHRequiresToken(t *testing.T) {
	if semanticUserTextDeclaresSSH("install openssh-client") {
		t.Fatal("openssh must not count as an ssh access request")
	}
	if !semanticUserTextDeclaresSSH("用ssh访问") {
		t.Fatal("用ssh must count as an ssh access request")
	}
	if !semanticUserTextDeclaresSSH("ssh into the box") {
		t.Fatal("ssh token must count as an ssh access request")
	}
}

func TestSemanticGroupPolicyOmitsBaselineShell(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
	cb.loopCtx = &LoopContext{LansengerGroupPermissions: &lansengerGroupPermissionPolicy{}}
	if granted, message := cb.PetitionToolCall("delegate_task"); granted || message != "" {
		t.Fatalf("group policy must still deny a true effectful expansion: granted=%v message=%q", granted, message)
	}
}
