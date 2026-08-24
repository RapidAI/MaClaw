package main

import "testing"

func TestComputerUseE2EMatrixDocumentsCases(t *testing.T) {
	cases := computerUseE2EMatrix()
	if len(cases) < 5 {
		t.Fatalf("golden matrix too small: %d", len(cases))
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if c.Name == "" {
			t.Fatal("matrix case missing name")
		}
		seen[c.Name] = true
	}
	for _, want := range []string{"editor-type", "file-manager", "browser-tools", "vscode-palette", "im-search"} {
		if !seen[want] {
			t.Fatalf("matrix missing %s", want)
		}
	}
}

type computerUseE2ECase struct {
	Name   string
	Window string
	Via    string
}

func computerUseE2EMatrix() []computerUseE2ECase {
	return []computerUseE2ECase{
		{Name: "editor-type", Window: "Notepad/TextEdit/gedit", Via: "computer_type roundtrip (MACLAW_CU_E2E=1)"},
		{Name: "file-manager", Window: "Explorer/Finder", Via: "shell/file tools; CU only for pickers"},
		{Name: "browser-tools", Window: "Chrome/Edge", Via: "browser_* not generic CU"},
		{Name: "vscode-palette", Window: "Visual Studio Code", Via: "computer_key ctrl+shift+p then type"},
		{Name: "im-search", Window: "WeChat/Slack", Via: "search box first via computer_find"},
	}
}
