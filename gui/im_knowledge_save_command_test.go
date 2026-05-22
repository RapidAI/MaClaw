package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestParseImmediateKnowledgeSaveText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "screenshot style command",
			in:   "\u5c06\u4ee5\u4e0b\u5185\u5bb9\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93\uff1a\u4e4c\u514b\u5170\u3001\u56db\u5ddd\u516c\u6295\u5df2\u53d1\u751f",
			want: "\u4e4c\u514b\u5170\u3001\u56db\u5ddd\u516c\u6295\u5df2\u53d1\u751f",
			ok:   true,
		},
		{
			name: "quoted panel prefix",
			in:   "> \u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93: \u5173\u952e\u7ed3\u8bba A",
			want: "\u5173\u952e\u7ed3\u8bba A",
			ok:   true,
		},
		{
			name: "polite command prefix",
			in:   "\u8bf7\u5e2e\u6211\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93: \u5173\u952e\u7ed3\u8bba B",
			want: "\u5173\u952e\u7ed3\u8bba B",
			ok:   true,
		},
		{
			name: "question is not command",
			in:   "\u4eceAI\u52a9\u624b\u9762\u677f\u4e0d\u80fd\u4fdd\u5b58\u5185\u5bb9\u8fdb\u77e5\u8bc6\u5e93\uff1f",
			ok:   false,
		},
		{
			name: "generic question is not command",
			in:   "\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93\u4e86\u5417\uff1f",
			ok:   false,
		},
		{
			name: "embedded question is not command",
			in:   "\u80fd\u4e0d\u80fd\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93\uff1a\u8fd9\u4e2a\u5148\u522b\u5b58",
			ok:   false,
		},
		{
			name: "no payload",
			in:   "\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93",
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseImmediateKnowledgeSaveText(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("parseImmediateKnowledgeSaveText() = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestTransactionalTokenBufferSuppressesToolRoundTokens(t *testing.T) {
	var got []string
	b := newTransactionalTokenBuffer(func(delta string) { got = append(got, delta) })
	b.Write("\u6211\u6765\u4fdd\u5b58\uff0c")
	b.Write("\u5df2\u4fdd\u5b58\u3002")
	b.Discard()
	if len(got) != 0 {
		t.Fatalf("discarded tool-round tokens leaked: %#v", got)
	}
	b.Write("\u6700\u7ec8\u56de\u590d")
	b.Flush()
	if len(got) != 1 || got[0] != "\u6700\u7ec8\u56de\u590d" {
		t.Fatalf("flush tokens = %#v", got)
	}
}

func TestHandleImmediateKnowledgeSaveTextPersistsToStore(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	resp, handled := h.handleImmediateKnowledgeSaveText(IMUserMessage{UserID: "desktop-user", Platform: desktopPlatform}, "\u5c06\u4ee5\u4e0b\u5185\u5bb9\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93\uff1a\u786e\u5b9a\u6027\u4fdd\u5b58\u951a\u70b9 alpha-knowledge")
	if !handled {
		t.Fatal("expected immediate knowledge save handler")
	}
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "\u5df2\u4fdd\u5b58\u5230\u77e5\u8bc6\u5e93") || !strings.Contains(resp.Text, "Source ID:") {
		t.Fatalf("unexpected response: %#v", resp)
	}
	results, err := app.KnowledgeSearch(knowledge.SearchOptions{Query: "alpha-knowledge", SearchScope: knowledge.SaveScopeProject, Limit: 5})
	if err != nil {
		t.Fatalf("KnowledgeSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("saved text not searchable")
	}
}
