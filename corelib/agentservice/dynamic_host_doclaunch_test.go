package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostSystemLauncher struct {
	principal Principal
	path      string
	result    string
	err       error
}

func (f *fakeHostSystemLauncher) OpenReviewedHostDocument(_ context.Context, principal Principal, path string) (string, error) {
	f.principal = principal
	f.path = path
	return f.result, f.err
}

type fakeHostDocumentLauncher struct {
	absPath string
	err     error
}

func (f *fakeHostDocumentLauncher) OpenDocument(_ context.Context, absPath string) error {
	f.absPath = absPath
	return f.err
}

func TestReviewedHostSystemLaunchExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	launcher := &fakeHostSystemLauncher{result: "Document opened with the system handler (notes.pdf). This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{SystemLaunch: launcher})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-open", TurnID: "turn-open", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "open", Capability: CapabilitySystemLaunch, Required: true,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindDocument},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("launch plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "open" {
		t.Fatalf("launch leaked soup: %#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("launch must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"notes.pdf"}`)
	if !result.Succeeded || result.Unknown || !strings.Contains(result.Result, "This is not a send.") || launcher.path != "notes.pdf" {
		t.Fatalf("launch result=%#v launcher=%#v", result, launcher)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"notes.pdf","target":"https://example.com","channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("open soup must fail closed, result=%#v", rejected)
	}
}

func TestReviewedHostSystemLaunchIsAbsentWithoutLauncher(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "open", Capability: CapabilitySystemLaunch, Required: true,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindDocument},
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("launch without launcher must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostSystemLaunchRejectsSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostSystemLaunchProvider(&fakeHostSystemLauncher{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilitySystemLaunch || provider.AdapterName == "open" {
		t.Fatalf("provider=%#v", provider)
	}
	if provider.Provides[0].Qualifiers[QualifierLaunchKind] != LaunchKindDocument {
		t.Fatalf("kind=%#v", provider.Provides[0].Qualifiers)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["path"]; !ok || len(props) != 1 {
		t.Fatalf("launch schema=%#v", props)
	}
	for _, key := range []string{"target", "url", "app", "channel", "destination", "group_name"} {
		if _, ok := props[key]; ok {
			t.Fatalf("launch schema leaked %s", key)
		}
	}
}

func TestReviewedHostSystemLaunchCallbackOpensWorkspaceDocumentWithoutSend(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.pdf"), []byte("%PDF-1.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "photo.png"), []byte("\x89PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	opened := &fakeHostDocumentLauncher{}
	cb := &coreAgentCallbacks{
		principal:        principal,
		workspace:        dir,
		documentLauncher: opened,
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("document open must not call send_file soup")
			return "sent"
		},
	}
	out, err := cb.OpenReviewedHostDocument(context.Background(), principal, "notes.pdf")
	if err != nil || !strings.Contains(out, "This is not a send.") || !strings.Contains(out, "notes.pdf") || strings.Contains(out, dir) {
		t.Fatalf("open=%q err=%v", out, err)
	}
	if opened.absPath != filepath.Join(dir, "notes.pdf") {
		t.Fatalf("opened=%q", opened.absPath)
	}
	if _, err := cb.OpenReviewedHostDocument(context.Background(), principal, "photo.png"); err == nil {
		t.Fatal("png must stay unpublished")
	}
	if _, err := cb.OpenReviewedHostDocument(context.Background(), principal, "folder"); err == nil {
		t.Fatal("directory must stay unpublished")
	}
	if _, err := cb.OpenReviewedHostDocument(context.Background(), principal, "https://example.com/doc.pdf"); err == nil {
		t.Fatal("url must stay unpublished")
	}
	empty := &coreAgentCallbacks{principal: principal, documentLauncher: opened}
	if _, err := empty.OpenReviewedHostDocument(context.Background(), principal, "notes.pdf"); err == nil {
		t.Fatal("empty workspace must stay unpublished")
	}
}

func TestReviewedDynamicIntentRulesResolveDocumentOpenWithoutAppLaunch(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary:    intent.LabelDocumentOpen,
			Confidence: .99,
			ToolNames:  []string{"open", "send_file", "browser"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "open this document with the default app"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilitySystemLaunch || resolution.Needs[0].Qualifiers[QualifierLaunchKind] != LaunchKindDocument {
		t.Fatalf("needs=%#v", resolution.Needs)
	}
	if resolution.Needs[0].Capability == CapabilityComputerUse {
		t.Fatal("document_open must not resolve to computer.control.desktop")
	}
}

func TestReviewedHostOwnedServicesPublishSystemLaunchWhenLauncherReady(t *testing.T) {
	dir := t.TempDir()
	cb := &coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}, workspace: dir, documentLauncher: &fakeHostDocumentLauncher{}}
	services := cb.reviewedHostOwnedServices()
	if services.SystemLaunch == nil {
		t.Fatal("ready launcher plus workspace must publish system launch")
	}
	noWS := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}, documentLauncher: &fakeHostDocumentLauncher{}}).reviewedHostOwnedServices()
	if noWS.SystemLaunch != nil {
		t.Fatal("missing workspace must keep system launch unpublished")
	}
	empty := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}, workspace: dir}).reviewedHostOwnedServices()
	if empty.SystemLaunch != nil {
		t.Fatal("missing launcher must keep system launch unpublished")
	}
}

func TestReviewedHostNativeDocumentLauncherReadyFollowsDisplay(t *testing.T) {
	launcher := reviewedHostNativeDocumentLauncher{}
	ok, _ := remote.DetectDisplayServer()
	if launcher.Ready() != ok {
		t.Fatalf("native ready=%v display=%v goos=%s", launcher.Ready(), ok, runtime.GOOS)
	}
}
