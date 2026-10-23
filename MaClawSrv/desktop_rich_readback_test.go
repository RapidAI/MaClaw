package main

import (
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestRichReadbackLineAndNext(t *testing.T) {
	if s := os.Getenv("DUMP_RB"); s != "" { // dumps the script for a manual page check
		os.WriteFile(s, []byte("return (()=>{"+desktopRichReadbackScript(os.Getenv("RB_SEL"), os.Getenv("RB_WANT"))+"})()"), 0o644)
	}
	scope := agentruntime.Scope{RunID: "rb-test"}
	line := desktopRichReadbackLine(`x RICH_READBACK kind=field len=12 has=yes head="HF 论文 \"10.18\""`)
	if !strings.Contains(line, "field now holds 12 chars") || !strings.Contains(line, `present=yes`) {
		t.Fatalf("line=%q", line)
	}
	if n := desktopRichFillNext(scope, line, "HF 论文 10.18"); !strings.Contains(n, "VERIFIED title") {
		t.Fatalf("title next=%q", n)
	}
	body := strings.Repeat("论文摘要一行\n", 30)
	bl := desktopRichReadbackLine(`RICH_READBACK kind=editor len=210 has=yes head="论文摘要一行"`)
	if n := desktopRichFillNext(scope, bl, body); !strings.Contains(n, "action=publish") {
		t.Fatalf("both next=%q", n)
	}
	nl := desktopRichReadbackLine(`RICH_READBACK kind=editor len=0 has=no head=""`)
	if n := desktopRichFillNext(agentruntime.Scope{RunID: "rb2"}, nl, body); !strings.Contains(n, "NOT verified") {
		t.Fatalf("no next=%q", n)
	}
}

func TestRecipeKeyedBySite(t *testing.T) {
	zh := desktopRecipeIntent("做10.18的 Hugging Face Daily Papers 摘要，发到知乎上")
	xhs := desktopRecipeIntent("做10.18的 Hugging Face Daily Papers 摘要，配一张封面图，发一篇图文笔记到小红书，发布后打开笔记确认，回复笔记链接")
	if desktopRecipeSite(zh) != "知乎" || desktopRecipeSite(xhs) != "小红书" {
		t.Fatalf("sites %q %q", desktopRecipeSite(zh), desktopRecipeSite(xhs))
	}
	if desktopRecipeSameSite(zh, xhs) {
		t.Fatal("zhihu recipe must not serve an xhs post")
	}
	if !desktopRecipeSameSite(zh, desktopRecipeIntent("做10.20的 Hugging Face Daily Papers 摘要，发到知乎上")) {
		t.Fatal("same site should match")
	}
	if desktopRecipeSite(desktopRecipeIntent("summarize news.ycombinator.com front page")) != "ycombinator" {
		t.Fatal("domain")
	}
}
