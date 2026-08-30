package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostFileDownloader struct {
	principal Principal
	rawURL    string
	result    string
	err       error
}

func (f *fakeHostFileDownloader) AcquireReviewedHostRemoteArtifact(_ context.Context, principal Principal, rawURL string) (string, error) {
	f.principal = principal
	f.rawURL = rawURL
	return f.result, f.err
}

func TestReviewedHostFileDownloadExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	downloader := &fakeHostFileDownloader{result: "Downloaded report.pdf (12 bytes). This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{FileDownload: downloader})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-dl", TurnID: "turn-dl", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "download", Capability: CapabilityFileDownload, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("download plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "download_file" || plan.Selections[0].AdapterName == "web_fetch" || plan.Selections[0].AdapterName == "wget" {
		t.Fatalf("download leaked soup: %#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("download must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.com/report.pdf"}`)
	if !result.Succeeded || result.Unknown || !strings.Contains(result.Result, "This is not a send.") || downloader.rawURL != "https://example.com/report.pdf" {
		t.Fatalf("download result=%#v writer=%#v", result, downloader)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.com/report.pdf","save_path":"out.pdf","path":"out.pdf"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("save_path soup must fail closed, result=%#v", rejected)
	}
	channelRejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.com/report.pdf","channel":"lansenger"}`)
	if channelRejected.Succeeded || channelRejected.Unknown {
		t.Fatalf("channel soup must fail closed, result=%#v", channelRejected)
	}
	bypass := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"[file_base64|a.pdf|application/pdf]AAAA"}`)
	if bypass.Succeeded || bypass.Unknown {
		t.Fatalf("file_base64 must fail closed, result=%#v", bypass)
	}
	ftp := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"ftp://example.com/report.pdf"}`)
	if ftp.Succeeded || ftp.Unknown {
		t.Fatalf("ftp must fail closed, result=%#v", ftp)
	}
	placeholder := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.invalid/skip"}`)
	if placeholder.Succeeded || placeholder.Unknown {
		t.Fatalf("reserved host must fail closed, result=%#v", placeholder)
	}

	fetchPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-fetch", TurnID: "turn-fetch", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "fetch", Capability: CapabilityWebFetch, Required: true}},
	})
	if err != nil || len(fetchPlan.Selections) != 0 {
		t.Fatalf("web fetch must not be satisfied by download, plan=%#v err=%v", fetchPlan, err)
	}
	writePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-write", TurnID: "turn-write", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "write", Capability: CapabilityFileWrite, Required: true}},
	})
	if err != nil || len(writePlan.Selections) != 0 {
		t.Fatalf("fs.write.local must not be satisfied by download, plan=%#v err=%v", writePlan, err)
	}
}

func TestReviewedHostFileDownloadIsAbsentWithoutDownloader(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "download", Capability: CapabilityFileDownload, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("download without writer must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostFileDownloadRejectsSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostFileDownloadProvider(&fakeHostFileDownloader{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityFileDownload || provider.AdapterName == "download_file" || provider.AdapterName == "web_fetch" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["url"]; !ok || len(props) != 1 {
		t.Fatalf("download schema=%#v", props)
	}
	for _, key := range []string{"path", "save_path", "output", "channel", "destination", "group_name", "headers", "via_browser"} {
		if _, ok := props[key]; ok {
			t.Fatalf("download schema leaked %s", key)
		}
	}
}

func TestReviewedHostFileDownloadCallbackWritesWorkspaceBasename(t *testing.T) {
	dir := t.TempDir()
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal: principal,
		workspace: dir,
		reviewedHostURLDownloader: func(_ context.Context, rawURL, dest, root string) (int, error) {
			if rawURL != "https://example.com/docs/report.pdf" || root != dir {
				t.Fatalf("download args url=%q dest=%q root=%q", rawURL, dest, root)
			}
			if err := os.WriteFile(dest, []byte("%PDF-1.4\n"), 0o644); err != nil {
				return 0, err
			}
			return 9, nil
		},
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("download must not call send_file soup")
			return "sent"
		},
	}
	out, err := cb.AcquireReviewedHostRemoteArtifact(context.Background(), principal, "https://example.com/docs/report.pdf")
	if err != nil || !strings.Contains(out, "report.pdf") || !strings.Contains(out, "This is not a send.") || strings.Contains(out, dir) {
		t.Fatalf("download=%q err=%v", out, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.pdf"))
	if err != nil || string(data) != "%PDF-1.4\n" {
		t.Fatalf("workspace file=%q err=%v", data, err)
	}
	if _, err := cb.AcquireReviewedHostRemoteArtifact(context.Background(), principal, "file:///tmp/secret.pdf"); err == nil {
		t.Fatal("file URL must fail closed")
	}
	empty := &coreAgentCallbacks{principal: principal}
	if _, err := empty.AcquireReviewedHostRemoteArtifact(context.Background(), principal, "https://example.com/report.pdf"); err == nil {
		t.Fatal("empty workspace must stay unpublished")
	}
}

func TestReviewedHostDownloadFileNameUsesURLBasename(t *testing.T) {
	name, err := reviewedHostDownloadFileName("https://cdn.example.com/a/b/notes.txt?token=1")
	if err != nil || name != "notes.txt" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	fallback, err := reviewedHostDownloadFileName("https://example.com/")
	if err != nil || fallback != reviewedHostFileDownloadDefaultName {
		t.Fatalf("fallback=%q err=%v", fallback, err)
	}
	if _, err := reviewedHostDownloadFileName("ftp://example.com/a.bin"); err == nil {
		t.Fatal("ftp must fail closed")
	}
	if _, err := reviewedHostDownloadFileName("https://user:pass@example.com/a.bin"); err == nil {
		t.Fatal("userinfo must fail closed")
	}
}

func TestReviewedDynamicIntentRulesResolveFileDownloadWithoutFetchOrWrite(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelFileDownload, Confidence: .99,
			ToolNames: []string{"download_file", "web_fetch", "write_file", "wget", "curl"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "把这个链接的文件下载到本地"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 || resolution.Needs[0].Capability != CapabilityFileDownload {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability == CapabilityWebFetch || resolution.Needs[0].Capability == CapabilityFileWrite {
		t.Fatal("file_download must not resolve to information.fetch.web or fs.write.local")
	}
}

func TestReviewedHostOwnedServicesPublishFileDownloadWithWorkspace(t *testing.T) {
	cb := &coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}, workspace: t.TempDir()}
	services := cb.reviewedHostOwnedServices()
	if services.FileDownload == nil {
		t.Fatal("workspace must publish file download")
	}
	empty := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}}).reviewedHostOwnedServices()
	if empty.FileDownload != nil {
		t.Fatal("missing workspace must keep file download unpublished")
	}
}
