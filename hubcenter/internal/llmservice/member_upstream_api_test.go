package llmservice

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestPatchProviderMemberUpstreamOverrides(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.SaveRegistry(ctx, &Registry{
		Providers: []llmpool.ProviderConfig{{
			ID: "a", Name: "A", APIURL: "https://a.example/v1", APIKey: "secret-a",
			ExtraBody:         map[string]any{"temperature": 0.4},
			ExtraBodyPriority: "downstream",
			DropFields:        []string{"thinking"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.PatchProviderMember(ctx, "a", ProviderMemberPatch{
		ExtraBodySet:         true,
		ExtraBody:            map[string]any{"reasoning_effort": "none"},
		ExtraBodyPrioritySet: true,
		ExtraBodyPriority:    "member",
	}); err != nil {
		t.Fatal(err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := reg.Providers[0]
	if got.ExtraBody["reasoning_effort"] != "none" || got.ExtraBodyPriority != "" || len(got.DropFields) != 1 {
		t.Fatalf("patched = extra %#v priority %q drop %#v", got.ExtraBody, got.ExtraBodyPriority, got.DropFields)
	}

	preview, err := svc.PreviewProviderMember(ctx, "a", ProviderMemberPatch{
		ExtraBodySet:  true,
		ExtraBody:     map[string]any{},
		DropFieldsSet: true,
		DropFields:    []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExtraBody != nil || preview.DropFields != nil {
		t.Fatalf("preview did not clear: %#v %#v", preview.ExtraBody, preview.DropFields)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Providers[0].ExtraBody["reasoning_effort"] != "none" || len(reg.Providers[0].DropFields) != 1 {
		t.Fatalf("dry-run wrote the registry: %#v", reg.Providers[0])
	}

	if err := svc.PatchProviderMember(ctx, "a", ProviderMemberPatch{ExtraBodyPrioritySet: true, ExtraBodyPriority: "sideways"}); err == nil {
		t.Fatal("expected invalid priority")
	}
}

func TestProviderImportKeepsAndClearsUpstreamOverrides(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	first := []ProviderArrayImport{{
		ID: "pool", Name: "Pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-1", Name: "One", APIURL: "https://api.example/v1", APIKey: "secret",
			ExtraBody:         map[string]any{"reasoning_effort": "none"},
			ExtraBodyPriority: "downstream",
			DropFields:        []string{"thinking"},
		}},
	}}
	if _, err := svc.ImportProviderArrays(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool", Name: "Pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-1", Name: "One", APIURL: "https://api.example/v1",
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := reg.Providers[0]
	if got.ExtraBody["reasoning_effort"] != "none" || got.ExtraBodyPriority != "downstream" || strings.Join(got.DropFields, ",") != "thinking" {
		t.Fatalf("omitted import dropped overrides: %#v priority %q drop %#v", got.ExtraBody, got.ExtraBodyPriority, got.DropFields)
	}
	if _, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool", Name: "Pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-1", Name: "One", APIURL: "https://api.example/v1",
			ExtraBody:         map[string]any{},
			ExtraBodyPriority: "member",
			DropFields:        []string{},
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got = reg.Providers[0]
	if got.ExtraBody != nil || got.ExtraBodyPriority != "" || got.DropFields != nil {
		t.Fatalf("explicit clear failed: %#v priority %q drop %#v", got.ExtraBody, got.ExtraBodyPriority, got.DropFields)
	}
	if _, err := svc.PreviewProviderArrays(ctx, []ProviderArrayImport{{
		ID: "pool", Name: "Pool",
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-1", Name: "One", APIURL: "https://api.example/v1",
			ExtraBody: map[string]any{"model": "hijack"},
		}},
	}}); err == nil {
		t.Fatal("expected dry-run to reject a protected extra_body key")
	}
}

func TestProviderInventoryIncludesUpstreamOverrides(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.SaveRegistry(ctx, &Registry{
		Providers: []llmpool.ProviderConfig{{
			ID: "pool-a-1", Name: "Primary", APIURL: "https://api.example/v1", APIKey: "sk-live-secret-ABCD",
			ExtraBody:         map[string]any{"reasoning_effort": "none"},
			ExtraBodyPriority: "downstream",
			DropFields:        []string{"thinking"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	inventory, err := svc.ListProviderArrayInventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "sk-live-secret-ABCD") || strings.Contains(text, `"api_key"`) {
		t.Fatalf("inventory leaked the key: %s", text)
	}
	member := inventory.Arrays[0].Members[0]
	if member.ExtraBody["reasoning_effort"] != "none" || member.ExtraBodyPriority != "downstream" || strings.Join(member.DropFields, ",") != "thinking" {
		t.Fatalf("member = %#v", member)
	}
}
