package botcapability

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApplyKeepsHTTPSecretAndStdioEnv(t *testing.T) {
	doc, err := Apply(Document{}, Update{UpsertMCP: []Server{{
		Name: "search", Transport: "http", URL: "https://mcp.example/mcp",
		AuthSecret: "sekret", Headers: map[string]string{"X-Token": "abc"},
	}, {
		Name: "local", Transport: "stdio", Command: "npx", Args: []string{"-y", "srv"},
		Env: map[string]string{"API_TOKEN": "tok"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	again, err := Apply(doc, Update{UpsertMCP: []Server{{
		ID: doc.MCP[0].ID, Name: "search", Transport: "http", URL: "https://mcp.example/mcp",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if again.MCP[0].AuthSecret != "sekret" || again.MCP[0].Headers["X-Token"] != "abc" {
		t.Fatalf("secret was dropped: %#v", again.MCP[0])
	}
	view := ViewOf(again)
	if !view.MCP[0].SecretSet || view.MCP[0].URL != "https://mcp.example/mcp" {
		t.Fatalf("view: %#v", view.MCP[0])
	}
	raw := Stamp(again)
	if strings.Contains(raw, "sekret") || len(raw) != 64 {
		t.Fatalf("stamp leaked or changed shape: %s", raw)
	}
	if again.MCP[1].Transport != "stdio" || again.MCP[1].Env["API_TOKEN"] != "tok" || again.MCP[1].AuthSecret != "" {
		t.Fatalf("stdio env: %#v", again.MCP[1])
	}
	kept, err := Apply(again, Update{UpsertMCP: []Server{{
		ID: again.MCP[1].ID, Name: "local", Transport: "stdio", Command: "npx", Args: []string{"-y", "srv"},
		Env: map[string]string{"API_TOKEN": ""},
	}}})
	if err != nil || kept.MCP[1].Env["API_TOKEN"] != "tok" {
		t.Fatalf("blank env wiped the stored value: %#v %v", kept.MCP, err)
	}
	viewRaw, _ := json.Marshal(ViewOf(kept))
	if strings.Contains(string(viewRaw), "tok") || strings.Contains(string(viewRaw), "sekret") {
		t.Fatalf("view leaked a secret: %s", viewRaw)
	}
	asHTTP, err := Apply(kept, Update{UpsertMCP: []Server{{
		ID: kept.MCP[1].ID, Name: "local", Transport: "http", URL: "https://mcp.example/other",
	}}})
	if err != nil || asHTTP.MCP[1].Env != nil || asHTTP.MCP[1].Command != "" {
		t.Fatalf("http kept stdio env: %#v %v", asHTTP.MCP[1], err)
	}
}

func TestApplyRejectsMetadataURLAndDuplicateSkill(t *testing.T) {
	_, err := Apply(Document{}, Update{UpsertMCP: []Server{{
		Name: "meta", Transport: "http", URL: "http://169.254.169.254/mcp",
	}}})
	if err == nil {
		t.Fatal("metadata url was accepted")
	}
	doc, err := Apply(Document{}, Update{UpsertSkills: []Skill{{Name: "Draft", Body: "write it"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(doc, Update{UpsertSkills: []Skill{{Name: "draft", Body: "other"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(doc.Skills[0].Name, "Draft") {
		t.Fatal(doc.Skills[0].Name)
	}
	replaced, err := Apply(doc, Update{UpsertSkills: []Skill{{Name: "draft", Body: "other"}}})
	if err != nil || replaced.Skills[0].Body != "other" || len(replaced.Skills) != 1 {
		t.Fatalf("replace: %#v %v", replaced.Skills, err)
	}
	_, err = Apply(Document{}, Update{UpsertSkills: []Skill{{Name: "../x", Body: "no"}}})
	if err == nil {
		t.Fatal("path skill name was accepted")
	}
}

func TestCatalogSkipsDisabled(t *testing.T) {
	doc := Document{
		MCP:    []Server{{ID: "a", Name: "A", Transport: "http", Disabled: true}, {ID: "b", Name: "B", Transport: "stdio"}},
		Skills: []Skill{{Name: "draft", Description: "save"}},
	}
	text := Catalog(doc)
	if strings.Contains(text, "mcp_server=a") || !strings.Contains(text, "mcp_server=b") || !strings.Contains(text, "draft") {
		t.Fatal(text)
	}
	if _, err := LookupMCP(doc, "a"); err == nil {
		t.Fatal("disabled server was callable")
	}
	got, err := LookupSkill(doc, "Draft")
	if err != nil || got.Name != "draft" {
		t.Fatalf("%v %#v", err, got)
	}
}

func TestLookupMCPIgnoresIDCase(t *testing.T) {
	doc, err := Normalize(Document{MCP: []Server{{
		ID: "GitHub", Name: "Hub", Transport: "http", URL: "https://mcp.example/mcp",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := LookupMCP(doc, "github")
	if err != nil || got.ID != "GitHub" {
		t.Fatalf("%v %#v", err, got)
	}
}
