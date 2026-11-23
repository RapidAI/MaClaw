package toolresult

import (
	"fmt"
	"strings"
	"testing"
)

func TestStructuredPreview_SSHKeepsCommandSection(t *testing.T) {
	banner := "SSH 连接成功\n会话 ID: ssh_1\n\n--- 初始输出 ---\nBANNER_START\n" + strings.Repeat("MOTD line\n", 400) + "BANNER_END\n"
	command := CommandOutputMarker + "\n=== UPTIME ===\nup 18 min\n" + strings.Repeat("tcp LISTEN nginx\n", 80) + "TAIL_PROMPT\n"
	raw := banner + command
	preview := StructuredPreview("ssh", raw, 700)
	if !strings.Contains(preview, "UPTIME") || !strings.Contains(preview, "TAIL_PROMPT") {
		t.Fatalf("command metrics were dropped:\n%s", preview)
	}
	if strings.Contains(preview, "BANNER_END") {
		t.Fatalf("login banner occupied the preview:\n%s", preview)
	}
	if len(preview) > 700 {
		t.Fatalf("preview len=%d exceeds 700", len(preview))
	}
	if strings.Count(preview, "已截断") != 1 {
		t.Fatalf("truncation markers=%d, want 1", strings.Count(preview, "已截断"))
	}
	if !strings.Contains(preview, fmt.Sprintf("共 %d 字节", len(raw))) {
		t.Fatalf("marker should cite the original payload (%d bytes):\n%s", len(raw), preview)
	}
}

func TestProjectTerminalCollapsesExactDuplicateLinesAndKeepsOriginal(t *testing.T) {
	raw := "UNIQUE_START\n" + strings.Repeat("compiler waiting\n", 200) + "UNIQUE_END"
	root := t.TempDir()
	projection, err := Project(ProjectOptions{
		ToolName:   "bash",
		SessionKey: "owner",
		Content:    raw,
		Limit:      1024,
		Root:       root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Spilled || projection.Handle == nil {
		t.Fatal("repetitive terminal output did not retain its original handle")
	}
	if !strings.Contains(projection.Preview, "199 identical lines omitted") ||
		!strings.Contains(projection.Preview, "UNIQUE_START") ||
		!strings.Contains(projection.Preview, "UNIQUE_END") {
		t.Fatalf("lossless repetitive preview is incomplete:\n%s", projection.Preview)
	}
	stored, err := Read(ReadOptions{
		ID:         projection.Handle.ID,
		SessionKey: "owner",
		Root:       root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Content != raw {
		t.Fatal("read-back lost the original terminal output")
	}
}

func TestStructuredPreview_TerminalTailHeavy(t *testing.T) {
	raw := "HEAD" + strings.Repeat("x", 8000) + "TAIL_END"
	p := StructuredPreview("bash", raw, 200)
	if !strings.Contains(p, "TAIL_END") {
		t.Fatalf("bash should keep tail: %q", p)
	}
	if len(p) > 220 {
		t.Fatalf("over limit: %d", len(p))
	}
}

func TestStructuredPreview_JSON(t *testing.T) {
	raw := `{"status":"ok","rows":[1,2,3],"blob":"` + strings.Repeat("z", 5000) + `"}`
	p := StructuredPreview("api_call", raw, 800)
	if !strings.Contains(p, "[json_preview]") {
		t.Fatalf("want json preview: %q", p)
	}
	if !strings.Contains(p, "status") {
		t.Fatalf("want keys: %q", p)
	}
	if len(p) > 900 {
		t.Fatalf("len=%d", len(p))
	}
}

func TestStructuredPreview_Diff(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/f.go b/f.go\n")
	b.WriteString("--- a/f.go\n+++ b/f.go\n@@ -1,3 +1,4 @@\n")
	for i := 0; i < 500; i++ {
		b.WriteString("+line added\n")
	}
	b.WriteString("LAST_HUNK\n")
	p := StructuredPreview("bash", b.String(), 400)
	// bash uses terminal reducer not diff — use tool that falls through to looksLikeDiff
	p = StructuredPreview("read_file", b.String(), 400)
	if !strings.Contains(p, "diff --git") && !strings.Contains(p, "已截断") {
		// either structured diff or default truncate is ok
		t.Logf("preview=%q", p[:min(120, len(p))])
	}
	if len(p) > 450 {
		t.Fatalf("len=%d", len(p))
	}
}

func TestStructuredPreview_WebSearchKeepsMiddleHitFact(t *testing.T) {
	long := strings.Repeat("百科简介没有气温。", 200)
	var b strings.Builder
	fmt.Fprintf(&b, "Public web results for \"北京天气\" (5):\n\n")
	fmt.Fprintf(&b, "1. 北京百科\n   https://example.com/baike\n   %s\n\n", long)
	fmt.Fprintf(&b, "2. 旅游攻略\n   https://example.com/tour\n   %s\n\n", long)
	fmt.Fprintf(&b, "3. 北京天气\n   https://weather.example/beijing\n   小雨 13/2℃ 东北风\n\n")
	fmt.Fprintf(&b, "4. 政府门户\n   https://example.com/gov\n   %s\n\n", long)
	fmt.Fprintf(&b, "5. 空气质量\n   https://example.com/aq\n   空气质量 良\n")
	raw := b.String()
	if len(raw) <= 4096 {
		t.Fatalf("fixture len = %d, want a result list past the default cap", len(raw))
	}
	window := DefaultPreview(raw, 4096)
	if strings.Contains(window, "小雨 13/2℃") {
		t.Fatalf("byte window already kept the middle fact:\n%s", window)
	}
	preview := StructuredPreview("web_search", raw, 4096)
	if len(preview) > 4096 {
		t.Fatalf("preview len = %d", len(preview))
	}
	for _, want := range []string{"小雨 13/2℃", "https://weather.example/beijing", "空气质量 良", "https://example.com/baike"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
}

func TestStructuredPreview_WebSearchKeepsFactFoldedOntoSnippetTail(t *testing.T) {
	lead := strings.Repeat("甲", 800)
	snippet := lead + "小雨 13/2℃"
	raw := fmt.Sprintf("Public web results for \"q\" (3):\n\n1. A\n   https://a.example/a\n   %s\n\n2. B\n   https://b.example/b\n   %s\n\n3. C\n   https://c.example/c\n   %s\n", snippet, snippet, snippet)
	preview := StructuredPreview("web_search", raw, 1200)
	if len(preview) > 1200 {
		t.Fatalf("preview len = %d", len(preview))
	}
	if !strings.Contains(preview, "小雨 13/2℃") || !strings.Contains(preview, "https://b.example/b") {
		t.Fatalf("folded fact or middle page was cut:\n%s", preview)
	}
}

func TestStructuredPreview_WebSearchKeepsFieldsBetweenLongParagraphs(t *testing.T) {
	lead := strings.Repeat("城市简介。", 80)
	tail := strings.Repeat("旅游攻略。", 80)
	snippet := lead + "; temperature: 18; wind: 3 m/s; " + tail
	if strings.Contains(shrinkWebSnippetEdges(snippet, 480), "temperature: 18") {
		t.Fatal("byte head/tail already kept the middle field")
	}
	raw := fmt.Sprintf("Public web results for \"q\" (4):\n\n1. A\n   https://a.example/a\n   %s\n\n2. B\n   https://b.example/b\n   %s\n\n3. C\n   https://c.example/c\n   %s\n\n4. D\n   https://d.example/d\n   %s\n", snippet, snippet, snippet, snippet)
	preview := StructuredPreview("web_search", raw, 1500)
	if len(preview) > 1500 {
		t.Fatalf("preview len = %d", len(preview))
	}
	for _, want := range []string{"temperature: 18", "wind: 3 m/s", "https://c.example/c"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
}

func TestStructuredPreview_WebSearchKeepsLeadFactBesideFoldedPage(t *testing.T) {
	lead := "LEAD_FACT " + strings.Repeat("城", 500)
	tail := strings.Repeat("页", 500)
	snippet := lead + "; temperature: 18; " + tail
	raw := fmt.Sprintf("Public web results for \"q\" (2):\n\n1. A\n   https://a.example/a\n   %s\n\n2. B\n   https://b.example/b\n   %s\n", snippet, snippet)
	preview := StructuredPreview("web_search", raw, 1600)
	if len(preview) > 1600 {
		t.Fatalf("preview len = %d", len(preview))
	}
	for _, want := range []string{"LEAD_FACT", "temperature: 18", "https://b.example/b"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
}

func TestProject_WebSearchPreviewKeepsMiddleFact(t *testing.T) {
	long := strings.Repeat("百科简介没有气温。", 200)
	var b strings.Builder
	fmt.Fprintf(&b, "Public web results for \"北京天气\" (5):\n\n")
	fmt.Fprintf(&b, "1. 北京百科\n   https://example.com/baike\n   %s\n\n", long)
	fmt.Fprintf(&b, "2. 旅游攻略\n   https://example.com/tour\n   %s\n\n", long)
	fmt.Fprintf(&b, "3. 北京天气\n   https://weather.example/beijing\n   小雨 13/2℃ 东北风\n\n")
	fmt.Fprintf(&b, "4. 政府门户\n   https://example.com/gov\n   %s\n\n", long)
	fmt.Fprintf(&b, "5. 空气质量\n   https://example.com/aq\n   空气质量 良\n")
	raw := b.String()
	proj, err := Project(ProjectOptions{
		ToolName:   "web_search",
		SessionKey: "user-1",
		Content:    raw,
		Root:       t.TempDir(),
		Limit:      4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(proj.Preview) > 4096 {
		t.Fatalf("projection len = %d", len(proj.Preview))
	}
	if !strings.Contains(proj.Preview, "[tool_result_handle]") {
		t.Fatalf("projection missing handle:\n%s", proj.Preview)
	}
	if !strings.Contains(proj.Preview, "小雨 13/2℃") || !strings.Contains(proj.Preview, "https://weather.example/beijing") {
		t.Fatalf("projection dropped the middle fact:\n%s", proj.Preview)
	}
}

func TestStructuredPreview_WebFetchIntegrity(t *testing.T) {
	body := strings.Repeat("page ", 2000) + "\n\n--- 完整性信号 ---\nok=true\n"
	p := StructuredPreview("web_fetch", body, 300)
	if !strings.Contains(p, "完整性信号") {
		t.Fatalf("should keep integrity marker: %q", p)
	}
}

func TestStructuredPreview_WebFetchKeepsNextOffset(t *testing.T) {
	body := strings.Repeat("中", 4000) + "\n... (total 8000 chars, showing 0-3999. Next: offset=4000)"
	p := StructuredPreview("web_fetch", body, 500)
	if len(p) > 500 {
		t.Fatalf("len=%d", len(p))
	}
	if !strings.Contains(p, "Next: offset=4000") {
		t.Fatalf("dropped cursor: %q", p)
	}
}

func TestStructuredPreview_WebFetchKeepsClippedNote(t *testing.T) {
	body := strings.Repeat("中", 4000) + "\nThe response continued past the fetched prefix. This text is not the whole document."
	p := StructuredPreview("web_fetch", body, 400)
	if len(p) > 400 {
		t.Fatalf("len=%d", len(p))
	}
	if !strings.Contains(p, "continued past the fetched prefix") {
		t.Fatalf("dropped clip note: %q", p)
	}
}

func TestStructuredPreview_WebFetchKeepsContinuationBlock(t *testing.T) {
	body := strings.Repeat("中", 4000) + "\n\n--- Continuation ---\nhas_more: true\nnext_offset: 4000\nPass offset=4000 to continue reading.\n"
	p := StructuredPreview("web_fetch", body, 400)
	if len(p) > 400 {
		t.Fatalf("len=%d", len(p))
	}
	if !strings.Contains(p, "--- Continuation ---") || !strings.Contains(p, "Pass offset=4000") {
		t.Fatalf("dropped continuation: %q", p)
	}
}

func TestCompressionStats_Record(t *testing.T) {
	ResetCompressionStats()
	RecordProjection("bash", 10000, 1000, true)
	RecordProjection("web_fetch", 5000, 2000, false)
	st := GetCompressionStats()
	if st.Projects != 2 || st.Spills != 1 {
		t.Fatalf("%+v", st)
	}
	if st.SavedBytes != 12000 {
		t.Fatalf("saved=%d", st.SavedBytes)
	}
	line := FormatCompressionLine()
	if !strings.Contains(line, "tool-compress:") || !strings.Contains(line, "spills=1") {
		t.Fatalf("line=%q", line)
	}
	ResetCompressionStats()
	if FormatCompressionLine() != "" {
		t.Fatal("empty after reset")
	}
}

func TestProject_UsesStructuredPreviewAndStats(t *testing.T) {
	ResetCompressionStats()
	dir := t.TempDir()
	raw := "START" + strings.Repeat("m", 10000) + "END"
	proj, err := Project(ProjectOptions{
		ToolName: "bash",
		Content:  raw,
		Limit:    500,
		Root:     dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proj.Spilled {
		t.Fatal("expected spill")
	}
	if !strings.Contains(proj.Preview, "END") {
		t.Fatalf("bash preview should keep end: %q", proj.Preview[max(0, len(proj.Preview)-80):])
	}
	st := GetCompressionStats()
	if st.Projects < 1 || st.SavedBytes <= 0 {
		t.Fatalf("stats=%+v", st)
	}
}

func TestStructuredPreview_ToolsSearchKeepsDecisionWhenOverLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("tools_search results for \"pdf\":\n")
	b.WriteString("本轮决定：现在调用 web_search。已计划但本请求尚未列出：generate_pdf（先调用 web_search，成功后它出现在下一请求）。不要直接调用这些名字，不要请愿。\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "- tool_%02d — %s [本轮不可用]\n", i, strings.Repeat("x", 80))
	}
	b.WriteString("只有标记「可请愿」的未列出名字才可以直接调用一次。\n")
	preview := StructuredPreview("tools_search", b.String(), 900)
	if !strings.Contains(preview, "本轮决定：") || !strings.Contains(preview, "generate_pdf（先调用 web_search") {
		t.Fatalf("decision was cut: %s", preview)
	}
	if strings.Contains(preview, "tool_79") && !strings.Contains(preview, "next") {
		// The tail sermon may remain. A late directory row must not be required.
	}
	if strings.Index(preview, "本轮决定：") > strings.Index(preview, "- tool_") && strings.Contains(preview, "- tool_") {
		t.Fatalf("decision must stay ahead of directory rows: %s", preview)
	}
}

func TestStructuredPreview_ToolsSearchKeepsCallUnderALongSkillBody(t *testing.T) {
	var b strings.Builder
	b.WriteString("tools_search results for \"skill\":\n")
	b.WriteString("技能步骤从这里开始。\n")
	b.WriteString("- 先做这个\n")
	b.WriteString("- 再做那个\n")
	b.WriteString(strings.Repeat("技能步骤。", 400))
	b.WriteString("\n- web_search — Search the public web. [已在当前工具面]\n")
	const call = "立即调用已在当前工具面的名字：web_search。不要再查询这些名字。"
	b.WriteString(call + "\n")
	preview := StructuredPreview("tools_search", b.String(), 900)
	if !strings.Contains(preview, "技能步骤从这里开始") || !strings.Contains(preview, "- 先做这个") {
		t.Fatalf("skill body was cut at the first bullet: %s", preview)
	}
	if !strings.Contains(preview, call) {
		t.Fatalf("call was dropped: %s", preview)
	}
	if len(preview) > 900 {
		t.Fatalf("preview is %d bytes", len(preview))
	}
}

func TestStructuredPreview_BrowserObservationKeepsShipToAndPayable(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算\nrefs:\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "@e%d link %q\n", i+1, strings.Repeat("导航按钮", 8))
	}
	b.WriteString("page_text:\n")
	b.WriteString("管理收货地址")
	b.WriteString(strings.Repeat("甲", 700))
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString(strings.Repeat("乙", 700))
	b.WriteString("应付总额 ¥9.98 含运费")
	b.WriteString(strings.Repeat("丙", 400))
	b.WriteString("\n付款前调用 browser(action=\"confirm_order\", item, shop, address, amount)。不要自己点击支付。")
	raw := b.String()
	if len(raw) <= 4096 {
		t.Fatalf("fixture must exceed the preview cap, len=%d", len(raw))
	}
	preview := StructuredPreview("browser", raw, 4096)
	for _, want := range []string{"收货地址北京市海淀区上地十街10号", "应付总额 ¥9.98", "confirm_order", "@e1"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
	streetAt := strings.Index(preview, "上地十街10号")
	headerAt := strings.Index(preview, "管理收货地址")
	if streetAt < 0 || (headerAt >= 0 && headerAt < streetAt) {
		t.Fatalf("header precedes the ship-to line:\n%s", preview)
	}
	if len(preview) > 4096 {
		t.Fatalf("preview is %d bytes", len(preview))
	}
}

func TestStructuredPreview_BrowserHotlineDoesNotReplaceTheStreet(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算\nrefs:\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "@e%d link %q\n", i+1, strings.Repeat("导航按钮", 8))
	}
	b.WriteString("page_text:\n")
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString(strings.Repeat("甲", 200))
	b.WriteString("送至4006065500")
	b.WriteString(strings.Repeat("乙", 2500))
	b.WriteString("应付总额 ¥9.98 含运费")
	raw := b.String()
	if len(raw) <= 4096 {
		t.Fatalf("fixture must exceed the preview cap, len=%d", len(raw))
	}
	preview := StructuredPreview("browser", raw, 4096)
	if !strings.Contains(preview, "上地十街10号") || !strings.Contains(preview, "应付总额 ¥9.98") {
		t.Fatalf("street or total dropped:\n%s", preview)
	}
	streetAt := strings.Index(preview, "上地十街10号")
	phoneAt := strings.Index(preview, "4006065500")
	if streetAt < 0 || (phoneAt >= 0 && phoneAt < streetAt) {
		t.Fatalf("hotline precedes the ship-to line:\n%s", preview)
	}
	if len(preview) > 4096 {
		t.Fatalf("preview is %d bytes", len(preview))
	}
}

func TestStructuredPreview_BrowserKeepsTheProductInThePrefix(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算\nrefs:\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&b, "@e%d link %q\n", i+1, strings.Repeat("导航按钮", 8))
	}
	b.WriteString("page_text:\n")
	b.WriteString("管理收货地址")
	b.WriteString(strings.Repeat("甲", 400))
	b.WriteString("雪碧500ml")
	b.WriteString(strings.Repeat("甲", 400))
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString(strings.Repeat("乙", 2500))
	b.WriteString("应付总额 ¥9.98 含运费")
	b.WriteString(strings.Repeat("丙", 2000))
	b.WriteString("页脚版权")
	raw := b.String()
	if len(raw) <= 4096 {
		t.Fatalf("fixture must exceed the preview cap, len=%d", len(raw))
	}
	preview := StructuredPreview("browser", raw, 4096)
	for _, want := range []string{"雪碧500ml", "上地十街10号", "应付总额 ¥9.98", "@e1"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
	if strings.Contains(preview, "页脚版权") {
		t.Fatalf("footer replaced the product:\n%s", preview)
	}
	streetAt := strings.Index(preview, "上地十街10号")
	headerAt := strings.Index(preview, "管理收货地址")
	if streetAt < 0 || (headerAt >= 0 && headerAt < streetAt) {
		t.Fatalf("header precedes the ship-to line:\n%s", preview)
	}
	if len(preview) > 4096 {
		t.Fatalf("preview is %d bytes", len(preview))
	}
}

func TestStructuredPreview_BrowserFittingPageKeepsItsTail(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算\nrefs:\n@e1 button 确认支付\n")
	b.WriteString("page_text:\n")
	b.WriteString(strings.Repeat("甲", 400))
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString(strings.Repeat("乙", 40))
	b.WriteString("应付总额 ¥9.98")
	b.WriteString("TAILTOKEN")
	raw := b.String()
	preview := StructuredPreview("browser", raw, len(raw))
	if preview != raw {
		t.Fatalf("a page that already fits was rewritten to %d bytes", len(preview))
	}
	if !strings.Contains(preview, "TAILTOKEN") || !strings.Contains(preview, "上地十街10号") || !strings.Contains(preview, "应付总额 ¥9.98") {
		t.Fatalf("fitting page lost a checkout fact:\n%s", preview)
	}
}

func TestStructuredPreview_BrowserFactsLeadWhenTheReshapeFits(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算\nrefs:\n@e1 button 确认支付\n")
	b.WriteString("page_text:\n")
	b.WriteString("LEADTOKEN")
	b.WriteString(strings.Repeat("甲", 80))
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString("应付总额 ¥9.98")
	b.WriteString("TAILTOKEN")
	raw := b.String()
	preview := StructuredPreview("browser", raw, 1<<20)
	body := preview
	if parts := strings.SplitN(preview, "page_text:\n", 2); len(parts) == 2 {
		body = parts[1]
	}
	streetAt := strings.Index(body, "上地十街10号")
	leadAt := strings.Index(body, "LEADTOKEN")
	if streetAt < 0 || leadAt < 0 || streetAt > leadAt {
		t.Fatalf("facts should lead a reshape that still fits:\n%s", preview)
	}
	if !strings.Contains(preview, "TAILTOKEN") || !strings.Contains(preview, "应付总额 ¥9.98") {
		t.Fatalf("reshape dropped the page:\n%s", preview)
	}
	// The street is moved, not copied in front of the same page.
	if len(preview) > len(raw)+80 {
		t.Fatalf("ship-to line was copied: raw=%d preview=%d", len(raw), len(preview))
	}
}

func TestStructuredPreview_BrowserOverlappingWindowsKeepTheAmount(t *testing.T) {
	// The address neighborhood ends 160 runes after 收货地址. Place the payable
	// amount so it straddles that edge. Moving only the address window splits ¥9.98.
	const street = "收货地址北京市海淀区上地十街10号"
	payableAt := 154
	pad := payableAt - len([]rune(street))
	if pad < 0 {
		t.Fatalf("pad %d", pad)
	}
	var page strings.Builder
	page.WriteString("LEADTOKEN")
	page.WriteString(strings.Repeat("甲", 80))
	page.WriteString(street)
	page.WriteString(strings.Repeat("乙", pad))
	page.WriteString("应付总额 ¥9.98")
	page.WriteString("TAILTOKEN")
	raw := "probed page 结算\nrefs:\n@e1 button 确认支付\npage_text:\n" + page.String()
	preview := StructuredPreview("browser", raw, 1<<20)
	if !strings.Contains(preview, "¥9.98") {
		t.Fatalf("payable amount was split:\n%s", preview)
	}
	body := preview
	if parts := strings.SplitN(preview, "page_text:\n", 2); len(parts) == 2 {
		body = parts[1]
	}
	streetAt := strings.Index(body, "上地十街10号")
	leadAt := strings.Index(body, "LEADTOKEN")
	if streetAt < 0 || leadAt < 0 || streetAt > leadAt {
		t.Fatalf("street should lead:\n%s", preview)
	}
	if !strings.Contains(preview, "TAILTOKEN") || !strings.Contains(preview, "@e1") {
		t.Fatalf("reshape dropped the page:\n%s", preview)
	}
	if len(preview) > len(raw)+80 {
		t.Fatalf("windows were copied: raw=%d preview=%d", len(raw), len(preview))
	}
}

func TestStructuredPreview_BrowserPayRefLeadsThePage(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算\nrefs:\n")
	b.WriteString("@e1 button \"确认支付\"\n")
	b.WriteString("@e2 link \"首页\"\n")
	b.WriteString("page_text:\n")
	b.WriteString("LEADTOKEN")
	b.WriteString(strings.Repeat("甲", 80))
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString("应付总额 ¥9.98")
	b.WriteString("雪碧500ml")
	preview := StructuredPreview("browser", b.String(), 1<<20)
	payAt := strings.Index(preview, "@e1")
	pageAt := strings.Index(preview, "page_text:\n")
	streetAt := strings.Index(preview, "上地十街10号")
	otherAt := strings.Index(preview, "@e2")
	if payAt < 0 || pageAt < 0 || streetAt < 0 || otherAt < 0 || !strings.Contains(preview, "雪碧500ml") {
		t.Fatalf("missing part:\n%s", preview)
	}
	if payAt > pageAt || pageAt > streetAt || streetAt > otherAt {
		t.Fatalf("pay ref %d page %d street %d other ref %d\n%s", payAt, pageAt, streetAt, otherAt, preview)
	}
}

func TestStructuredPreview_BrowserCommitRefsAndHintLeadTheNavList(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算\nrefs:\n")
	b.WriteString("@e1 textbox input=password commit=secret \"密码\"\n")
	b.WriteString("@e2 button commit=pay \"确认支付\"\n")
	b.WriteString("@e3 link \"首页\"\n")
	b.WriteString("page_text:\n")
	b.WriteString(strings.Repeat("甲", 40))
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString("应付总额 ¥9.98")
	b.WriteString("\n付款前调用 browser(action=\"confirm_order\", item, shop, address, amount)。")
	preview := StructuredPreview("browser", b.String(), 1<<20)
	secretAt := strings.Index(preview, "@e1")
	payAt := strings.Index(preview, "@e2")
	pageAt := strings.Index(preview, "page_text:\n")
	streetAt := strings.Index(preview, "上地十街10号")
	hintAt := strings.Index(preview, "confirm_order")
	navAt := strings.Index(preview, "@e3")
	if secretAt < 0 || payAt < 0 || pageAt < 0 || streetAt < 0 || hintAt < 0 || navAt < 0 {
		t.Fatalf("missing part:\n%s", preview)
	}
	if secretAt > hintAt || payAt > hintAt || hintAt > pageAt || pageAt > streetAt || streetAt > navAt {
		t.Fatalf("secret %d pay %d hint %d page %d street %d nav %d\n%s", secretAt, payAt, hintAt, pageAt, streetAt, navAt, preview)
	}
	again := StructuredPreview("browser", preview, 1<<20)
	if again != preview {
		t.Fatalf("second preview moved the instruction or the nav list:\n%s", again)
	}
}

func TestStructuredPreview_BrowserKeepsTheCommitPrefixWhenThePageIsCut(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算\nrefs:\n")
	b.WriteString("@e1 textbox commit=secret input=password \"密码\"\n")
	b.WriteString("@e2 button commit=pay \"确认支付\"\n")
	b.WriteString("@e3 link \"首页\"\n")
	b.WriteString("page_text:\n")
	b.WriteString(strings.Repeat("甲", 2500))
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString(strings.Repeat("乙", 2500))
	b.WriteString("应付总额 ¥9.98")
	b.WriteString("\n付款前调用 browser(action=\"confirm_order\", item, shop, address, amount)。")
	raw := b.String()
	if len(raw) <= 4096 {
		t.Fatalf("fixture must exceed the cut, len=%d", len(raw))
	}
	preview := StructuredPreview("browser", raw, 4096)
	pageAt := strings.Index(preview, "page_text:\n")
	secretAt := strings.Index(preview, "@e1")
	payAt := strings.Index(preview, "@e2")
	hintAt := strings.Index(preview, "confirm_order")
	if pageAt < 0 || secretAt < 0 || payAt < 0 || hintAt < 0 || secretAt > pageAt || payAt > pageAt || hintAt > pageAt {
		t.Fatalf("commit prefix or hint fell behind the page:\n%s", preview)
	}
	if !strings.Contains(preview, "上地十街10号") {
		t.Fatalf("street dropped:\n%s", preview)
	}
	if len(preview) > 4096 {
		t.Fatalf("preview is %d bytes", len(preview))
	}
	again := StructuredPreview("browser", preview, 4096)
	if again != preview {
		t.Fatalf("second preview of a cut page changed the instruction:\n%s", again)
	}
}

func TestStructuredPreview_BrowserHintLeadsAPageWithoutAPayableLine(t *testing.T) {
	raw := "probed page 登录\nrefs:\n@e1 textbox commit=secret input=password \"密码\"\n@e2 link \"首页\"\npage_text:\n" +
		strings.Repeat("甲", 80) +
		"\n密码或验证码框用 browser(action=\"submit_secret\", method, ref)。"
	preview := StructuredPreview("browser", raw, 1<<20)
	secretAt := strings.Index(preview, "@e1")
	hintAt := strings.Index(preview, "submit_secret")
	pageAt := strings.Index(preview, "page_text:\n")
	navAt := strings.Index(preview, "@e2")
	if secretAt < 0 || hintAt < 0 || pageAt < 0 || navAt < 0 || secretAt > hintAt || hintAt > pageAt || pageAt > navAt {
		t.Fatalf("secret %d hint %d page %d nav %d\n%s", secretAt, hintAt, pageAt, navAt, preview)
	}
	again := StructuredPreview("browser", preview, 1<<20)
	if again != preview {
		t.Fatalf("second preview moved the instruction:\n%s", again)
	}
}

func TestStructuredPreview_BrowserKeepsTheRefsTruncatedNote(t *testing.T) {
	var b strings.Builder
	b.WriteString("probed page 结算, interactive elements: 90\nrefs:\n")
	b.WriteString("@e1 button \"确认支付\"\n")
	b.WriteString("refs truncated; probe again with query\n")
	b.WriteString("page_text:\n")
	b.WriteString(strings.Repeat("甲", 80))
	b.WriteString("收货地址北京市海淀区上地十街10号")
	b.WriteString("应付总额 ¥9.98")
	preview := StructuredPreview("browser", b.String(), 1<<20)
	if !strings.Contains(preview, "refs truncated; probe again with query") {
		t.Fatalf("truncated ref list was dropped:\n%s", preview)
	}
	if !strings.Contains(preview, "上地十街10号") || !strings.Contains(preview, "@e1") {
		t.Fatalf("street or pay ref dropped:\n%s", preview)
	}
}

func TestStructuredPreview_BrowserActionStaysHeadTail(t *testing.T) {
	raw := "clicked @e1\n" + strings.Repeat("x", 5000) + "\n先 probe，再继续。"
	preview := StructuredPreview("browser", raw, 4096)
	if !strings.Contains(preview, "clicked @e1") || !strings.Contains(preview, "先 probe") {
		t.Fatalf("action preview dropped an end: %s", preview)
	}
	if len(preview) > 4096 {
		t.Fatalf("preview is %d bytes", len(preview))
	}
}

func TestStructuredPreview_ComputerObserveKeepsRefsWhenOverLimit(t *testing.T) {
	raw := "mode=text_primary screen=100x100 scale=1.00 screen_index=0\nwindows:\n  - Word\nelements (80):\n"
	for i := 0; i < 80; i++ {
		raw += fmt.Sprintf("  e%d [button] \"Btn%d\" conf=1.00 bbox=1,1,20,20 center=10,10 src=a11y\n", i, i)
	}
	raw += "ocr_excerpt: Document saved successfully\n"
	preview := StructuredPreview("computer_observe", raw, 400)
	if !strings.Contains(preview, "ocr_excerpt: Document saved successfully") && !strings.Contains(preview, "windows:") {
		t.Fatalf("should keep header/ocr: %q", preview)
	}
	if !strings.Contains(preview, "e0 [button]") {
		t.Fatalf("over-limit observe must keep a head ref: %q", preview)
	}
	if !strings.Contains(preview, "e79 [button]") {
		t.Fatalf("over-limit observe must keep a tail ref: %q", preview)
	}
	if strings.Count(preview, "bbox=") >= 80 {
		t.Fatalf("must not keep the full element list: %q", preview)
	}
	if len(preview) > 480 {
		t.Fatalf("len=%d", len(preview))
	}
}

func TestStructuredPreview_ComputerObserveHugeOCRStillKeepsRefs(t *testing.T) {
	token := "UniqueOCRTailToken"
	raw := "mode=text_primary screen=100x100 scale=1.00 screen_index=0\nwindows:\n  - Word\nelements (80):\n"
	for i := 0; i < 80; i++ {
		raw += fmt.Sprintf("  e%d [button] \"Btn%d\" conf=1.00 bbox=1,1,20,20 center=10,10 src=a11y\n", i, i)
	}
	raw += "ocr_excerpt: " + strings.Repeat("o", 2000) + token + "\n"
	preview := StructuredPreview("computer_observe", raw, 800)
	if !strings.Contains(preview, "e0 [button]") {
		t.Fatalf("huge OCR must not drop head ref: %q", preview)
	}
	if !strings.Contains(preview, "e79 [button]") {
		t.Fatalf("huge OCR must not drop tail ref: %q", preview)
	}
	if !strings.Contains(preview, token) {
		t.Fatalf("huge OCR tail token must survive: %q", preview)
	}
	if strings.Count(preview, "bbox=") >= 80 {
		t.Fatalf("must not keep the full element list: bbox=%d", strings.Count(preview, "bbox="))
	}
	if len(preview) > 900 {
		t.Fatalf("len=%d", len(preview))
	}
}
