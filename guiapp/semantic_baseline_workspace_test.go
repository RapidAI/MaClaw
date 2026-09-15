package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestSemanticSearchTurnKeepsBaselineWorkspaceToolsListed(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
	if name := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedShellAdapter); name != "bash" {
		t.Fatalf("search turn must list baseline bash, got %q", name)
	}
	if name := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedFileReadAdapter); name != "read_file" {
		t.Fatalf("search turn must list baseline read_file, got %q", name)
	}
	if name := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedFileWriteAdapter); name != "write_file" {
		t.Fatalf("search turn must list baseline write_file, got %q", name)
	}
	defs := cb.BuildToolsForModelRequest("查看驱网服务器状态", 1)
	found := map[string]bool{}
	for _, def := range defs {
		found[extractToolName(def)] = true
	}
	for _, name := range []string{"bash", "read_file", "write_file"} {
		if !found[name] {
			t.Fatalf("rendered search surface lacks baseline %s: %#v", name, found)
		}
	}
}

func TestSemanticBaselineBashSurvivesSpentEffectfulPetition(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
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

func TestSemanticToolsSearchDoesNotPetitionUnboundSSH(t *testing.T) {
	cb := petitionTestOfficeCallbacks(t, &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98})
	cb.userText = "查看驱网服务器状态，用ssh访问"
	semanticToolsSearchMaybeExpandScope(cb, "run remote command ssh host server shell")
	if cb.semanticEffectfulPetitionConsumed {
		t.Fatal("unbound ssh discovery must not spend the effectful petition")
	}
	if planHasCapabilities(cb.semanticSurface.plan, tool.CapabilityShellExecuteRemoteHost) {
		t.Fatal("unbound ssh must not enter the managed plan")
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
