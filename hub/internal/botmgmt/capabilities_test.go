package botmgmt

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/botcapability"
)

func TestOwnerCapabilitiesPushKeepsOtherConfigAndSkipsASecondPush(t *testing.T) {
	svc, rec := newPrincipalLLMFixture(t, nil, record{})
	view, err := svc.UpdateOwnerCapabilities(context.Background(), "tenant-a", "alice", botcapability.Update{
		UpsertMCP: []botcapability.Server{{
			Name: "search", Transport: "http", URL: "https://mcp.example/mcp", AuthSecret: "sekret",
		}},
		UpsertSkills: []botcapability.Skill{{Name: "draft", Description: "save", Body: "Write the draft."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Warning != "" || len(view.MCP) != 1 || !view.MCP[0].SecretSet || view.MCP[0].URL != "https://mcp.example/mcp" {
		t.Fatalf("view: %#v", view)
	}
	pushed := rec.last(t)
	if pushed["language"] != "zh" {
		t.Fatalf("push replaced the config: %#v", pushed)
	}
	got, ok := pushed["desktop_bot_capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities missing: %#v", pushed["desktop_bot_capabilities"])
	}
	raw, _ := got["mcp"].([]any)
	if len(raw) != 1 {
		t.Fatalf("mcp: %#v", got)
	}
	if rec.count() != 1 {
		t.Fatalf("pushes = %d", rec.count())
	}
	if err := svc.ensureBotCapabilities(context.Background(), "tenant-a", "alice"); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 {
		t.Fatalf("unchanged document was pushed again: %d", rec.count())
	}
	gotView, err := svc.GetOwnerCapabilities(context.Background(), "tenant-a", "alice")
	if err != nil || len(gotView.Skills) != 1 || gotView.Skills[0].Name != "draft" {
		t.Fatalf("get: %#v %v", gotView, err)
	}
}
