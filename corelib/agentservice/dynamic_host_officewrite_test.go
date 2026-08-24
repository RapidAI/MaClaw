package agentservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/excel"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostOfficeWriter struct {
	path      string
	data      excel.WriteData
	principal Principal
	result    string
	err       error
}

func (f *fakeHostOfficeWriter) WriteReviewedHostOffice(_ context.Context, principal Principal, path string, data excel.WriteData) (string, error) {
	f.principal = principal
	f.path = path
	f.data = data
	return f.result, f.err
}

func TestReviewedHostOfficeWriteExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	writer := &fakeHostOfficeWriter{result: "Wrote spreadsheet sheet.xlsx"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{OfficeWrite: writer})
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
			ID: "office", Capability: CapabilityOfficeWrite, Required: true,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatSpreadsheet},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("office write plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].AdapterName == "office" || plan.Selections[0].AdapterName == "write_excel" {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host office write must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"sheet.xlsx","sheets":[{"name":"S1","rows":[["a"]]}]}`)
	if !result.Succeeded || result.Result != writer.result || result.Unknown {
		t.Fatalf("office write result=%#v", result)
	}
	if writer.path != "sheet.xlsx" || len(writer.data.Sheets) != 1 || writer.data.Sheets[0].Name != "S1" || writer.principal.UserID != principal.UserID {
		t.Fatalf("writer=%#v", writer)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"sheet.xlsx","sheets":[{"name":"S1","rows":[["a"]]}],"action":"write_excel"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", rejected)
	}

	filePlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-file", TurnID: "turn-file", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "write", Capability: CapabilityFileWrite, Required: true}},
	})
	if err != nil || len(filePlan.Selections) != 0 {
		t.Fatalf("fs.write.local must not be satisfied by office write, plan=%#v err=%v", filePlan, err)
	}
	wordPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-word", TurnID: "turn-word", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "word", Capability: CapabilityOfficeWrite, Required: true,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatWord},
		}},
	})
	if err == nil && len(wordPlan.Selections) != 0 {
		t.Fatalf("word format must stay unpublished, plan=%#v", wordPlan)
	}
}

func TestReviewedHostOfficeWriteIsAbsentWithoutWriter(t *testing.T) {
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
			ID: "office", Capability: CapabilityOfficeWrite, Required: true,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatSpreadsheet},
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("office write without writer must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostOfficeWriteRejectsChannelAndSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostOfficeWriteProvider(&fakeHostOfficeWriter{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityOfficeWrite || provider.AdapterName == "office" || provider.AdapterName == "write_excel" {
		t.Fatalf("provider=%#v", provider)
	}
	if provider.Provides[0].Qualifiers[QualifierDocumentFormat] != DocumentFormatSpreadsheet {
		t.Fatalf("provision=%#v", provider.Provides[0])
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["path"]; !ok {
		t.Fatalf("office write schema missing path: %#v", props)
	}
	if _, ok := props["sheets"]; !ok || len(props) != 2 {
		t.Fatalf("office write schema=%#v", props)
	}
	for _, key := range []string{"channel", "destination", "group_name", "file_path", "action", "content"} {
		if _, ok := props[key]; ok {
			t.Fatalf("office write schema leaked %s", key)
		}
	}
}

func TestReviewedHostOfficeWriteStaysInsideWorkspace(t *testing.T) {
	dir := t.TempDir()
	principal := Principal{TenantID: "tenant", UserID: "user"}
	cb := &coreAgentCallbacks{principal: principal, workspace: dir}
	data := excel.WriteData{Sheets: []excel.WriteSheet{{Name: "S1", Rows: [][]excel.WriteCell{{{Value: "hello"}}}}}}
	out, err := cb.WriteReviewedHostOffice(context.Background(), principal, "sheet.xlsx", data)
	if err != nil || !strings.Contains(out, "sheet.xlsx") || strings.Contains(out, dir) {
		t.Fatalf("write office=%q err=%v", out, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sheet.xlsx")); err != nil {
		t.Fatalf("workspace spreadsheet missing: %v", err)
	}
	if _, err := cb.WriteReviewedHostOffice(context.Background(), principal, filepath.Join("..", "outside.xlsx"), data); err == nil {
		t.Fatal("workspace escape must fail closed")
	}
	escaped := &coreAgentCallbacks{principal: principal}
	if _, err := escaped.WriteReviewedHostOffice(context.Background(), principal, filepath.Join(dir, "sheet.xlsx"), data); err == nil {
		t.Fatal("empty workspace must not write absolute paths")
	}
}
