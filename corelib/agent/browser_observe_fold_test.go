package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func sampleBrowserProbe(display, page string) string {
	return display + "\nrefs:\n@e1 button \"确认支付\"\npage_text:\n" + page + "\n付款前调用 browser(action=\"confirm_order\", item, shop, address, amount)。"
}

func sampleBrowserProbeConversation(parts ...struct{ name, content string }) []interface{} {
	msgs := []interface{}{map[string]string{"role": "system", "content": "sys"}}
	for i, part := range parts {
		id := fmt.Sprintf("br-%d", i)
		msgs = append(msgs,
			map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"tool_calls": []interface{}{
					map[string]interface{}{
						"id":   id,
						"type": "function",
						"function": map[string]interface{}{
							"name":      part.name,
							"arguments": "{}",
						},
					},
				},
			},
			map[string]interface{}{
				"role":         "tool",
				"tool_call_id": id,
				"content":      part.content,
			},
		)
	}
	return msgs
}

func TestFoldBrowserObservationsKeepsLatestPageAndTheClick(t *testing.T) {
	oldPage := sampleBrowserProbe("probed page 旧结算", "收货地址旧地址上地九街1号 应付总额 ¥1.00")
	click := "clicked @e1\n先 probe，再继续。"
	newPage := sampleBrowserProbe("probed page 新结算", "收货地址新地址上地十街10号 应付总额 ¥9.98 雪碧500ml")
	msgs := sampleBrowserProbeConversation(
		struct{ name, content string }{"browser", oldPage},
		struct{ name, content string }{"browser", click},
		struct{ name, content string }{"browser", newPage},
	)
	got := FoldBrowserObservations(msgs)
	_, older := ExtractRoleContent(got[2])
	_, acted := ExtractRoleContent(got[4])
	_, newer := ExtractRoleContent(got[6])
	if !isBrowserProbeFingerprint(older) {
		t.Fatalf("older probe must be fingerprinted: %q", older)
	}
	if strings.Contains(older, "九街") || strings.Contains(older, "¥1.00") || strings.Contains(older, "应付总额") || strings.Contains(older, "page_text:") {
		t.Fatalf("fingerprint kept the old checkout page: %q", older)
	}
	if !strings.Contains(older, "probed page 旧结算") || !strings.Contains(older, "full page omitted; use the latest browser probe") {
		t.Fatalf("fingerprint dropped the page identity: %q", older)
	}
	if acted != click {
		t.Fatalf("click result was folded: %q", acted)
	}
	if newer != newPage {
		t.Fatalf("latest probe was folded: %q", newer)
	}
}

func TestFoldBrowserObservationsSkipsACheckoutFactInTheDisplayLine(t *testing.T) {
	oldPage := sampleBrowserProbe("收货地址旧地址上地九街1号", "商品 雪碧")
	newPage := sampleBrowserProbe("probed page 新结算", "商品 可乐")
	got := FoldBrowserObservations(sampleBrowserProbeConversation(
		struct{ name, content string }{"browser", oldPage},
		struct{ name, content string }{"browser", newPage},
	))
	_, older := ExtractRoleContent(got[2])
	if strings.Contains(older, "九街") || strings.Contains(older, "收货地址") {
		t.Fatalf("display line kept a stale address: %q", older)
	}
}

func TestFoldBrowserObservationsReadsTypedToolCalls(t *testing.T) {
	oldPage := sampleBrowserProbe("probed page 旧", "旧地址上地九街1号")
	newPage := sampleBrowserProbe("probed page 新", "新地址上地十街10号")
	msgs := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				ID:       "obs-0",
				Type:     "function",
				Function: llm.ToolCallFunction{Name: "browser", Arguments: "{}"},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "obs-0", "content": oldPage},
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []llm.ToolCall{{
				ID:       "obs-1",
				Type:     "function",
				Function: llm.ToolCallFunction{Name: "browser", Arguments: "{}"},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "obs-1", "content": newPage},
	}
	got := FoldBrowserObservations(msgs)
	_, older := ExtractRoleContent(got[1])
	_, newer := ExtractRoleContent(got[3])
	if !isBrowserProbeFingerprint(older) {
		t.Fatalf("typed tool call did not identify the older probe: %q", older)
	}
	if newer != newPage {
		t.Fatal("latest probe was folded")
	}
}

func TestFoldBrowserObservationsKeepsThePageURLWhenTheTitleIsATotal(t *testing.T) {
	display := "probed page 订单合计 (https://trade.example/old), interactive elements: 4"
	oldPage := sampleBrowserProbe(display, "收货地址旧地址上地九街1号 应付总额 ¥1.00")
	newPage := sampleBrowserProbe("probed page 新结算 (https://trade.example/new), interactive elements: 4", "商品")
	got := FoldBrowserObservations(sampleBrowserProbeConversation(
		struct{ name, content string }{"browser", oldPage},
		struct{ name, content string }{"browser", newPage},
	))
	_, older := ExtractRoleContent(got[2])
	if strings.Contains(older, "九街") || strings.Contains(older, "订单合计") || strings.Contains(older, "¥1.00") {
		t.Fatalf("fingerprint kept a checkout fact: %q", older)
	}
	if !strings.Contains(older, "https://trade.example/old") || !strings.Contains(older, "interactive elements: 4") {
		t.Fatalf("fingerprint dropped the page identity: %q", older)
	}
}

func TestFoldBrowserObservationsUsesTheToolMessageName(t *testing.T) {
	quoted := "notes\nprobed page 结算, interactive elements: 3\npage_text:\n收货地址旧地址上地九街1号"
	msgs := []interface{}{
		map[string]interface{}{"role": "tool", "name": "read_file", "tool_call_id": "r1", "content": quoted},
		map[string]interface{}{"role": "tool", "name": "read_file", "tool_call_id": "r2", "content": quoted + " 第二份"},
	}
	got := FoldBrowserObservations(msgs)
	_, first := ExtractRoleContent(got[0])
	if first != quoted {
		t.Fatalf("named read_file was folded: %q", first)
	}
}

func TestFoldBrowserObservationsKeepsAnUnlinkedObservation(t *testing.T) {
	oldPage := "probed page 旧结算 (https://trade.example/old), interactive elements: 3\nrefs:\n@e1 button \"确认支付\"\npage_text:\n收货地址旧地址上地九街1号"
	newPage := "probed page 新结算 (https://trade.example/new), interactive elements: 3\nrefs:\n@e1 button \"确认支付\"\npage_text:\n收货地址新地址上地十街10号"
	msgs := []interface{}{
		map[string]interface{}{"role": "tool", "tool_call_id": "p1", "content": oldPage},
		map[string]interface{}{"role": "tool", "tool_call_id": "p2", "content": newPage},
	}
	got := FoldBrowserObservations(msgs)
	_, older := ExtractRoleContent(got[0])
	_, newer := ExtractRoleContent(got[1])
	if !isBrowserProbeFingerprint(older) {
		t.Fatalf("unlinked observation was not folded: %q", older)
	}
	if strings.Contains(older, "九街") {
		t.Fatalf("fingerprint kept the street: %q", older)
	}
	if newer != newPage {
		t.Fatal("latest unlinked observation was folded")
	}
}

func TestFoldBrowserObservationsLeavesOtherTools(t *testing.T) {
	quoted := "notes\npage_text:\n收货地址旧地址上地九街1号"
	msgs := sampleBrowserProbeConversation(
		struct{ name, content string }{"bash", quoted},
		struct{ name, content string }{"bash", quoted},
	)
	got := FoldBrowserObservations(msgs)
	_, first := ExtractRoleContent(got[2])
	if first != quoted {
		t.Fatalf("a non-browser tool was folded: %q", first)
	}
}

func TestFoldBrowserObservationsIdempotent(t *testing.T) {
	msgs := sampleBrowserProbeConversation(
		struct{ name, content string }{"browser", sampleBrowserProbe("probed page A", "甲")},
		struct{ name, content string }{"browser", sampleBrowserProbe("probed page B", "乙")},
	)
	once := FoldBrowserObservations(msgs)
	twice := FoldBrowserObservations(once)
	_, c1 := ExtractRoleContent(once[2])
	_, c2 := ExtractRoleContent(twice[2])
	if c1 != c2 {
		t.Fatalf("second fold changed the fingerprint:\n%s\n%s", c1, c2)
	}
}

func TestTrimConversationFoldsBrowserProbesUnderBudget(t *testing.T) {
	oldPage := sampleBrowserProbe("probed page 旧结算", "收货地址旧地址上地九街1号 应付总额 ¥1.00")
	newPage := sampleBrowserProbe("probed page 新结算", "收货地址新地址上地十街10号 应付总额 ¥9.98")
	got := TrimConversation(sampleBrowserProbeConversation(
		struct{ name, content string }{"browser", oldPage},
		struct{ name, content string }{"browser", newPage},
	), 100000, 0, nil)
	_, older := ExtractRoleContent(got[2])
	_, newer := ExtractRoleContent(got[4])
	if !isBrowserProbeFingerprint(older) {
		t.Fatalf("TrimConversation must fold an older probe under budget: %q", older)
	}
	if strings.Contains(older, "九街") {
		t.Fatalf("folded probe kept the street: %q", older)
	}
	if newer != newPage {
		t.Fatal("latest probe must stay full after trim")
	}
}

func TestTrimHistoryFoldsBrowserProbes(t *testing.T) {
	oldPage := sampleBrowserProbe("probed page 旧结算", "收货地址旧地址上地九街1号")
	click := "clicked @e1\n先 probe，再继续。"
	newPage := sampleBrowserProbe("probed page 新结算", "收货地址新地址上地十街10号")
	got := TrimHistory([]ConversationEntry{
		{Role: "tool", ToolName: "browser", Content: oldPage},
		{Role: "tool", ToolName: "browser", Content: click},
		{Role: "tool", ToolName: "browser", Content: newPage},
	})
	if !isBrowserProbeFingerprint(entryContentString(got[0])) {
		t.Fatalf("older probe: %q", got[0].Content)
	}
	if strings.Contains(entryContentString(got[0]), "九街") {
		t.Fatalf("history fingerprint kept the street: %q", got[0].Content)
	}
	if entryContentString(got[1]) != click {
		t.Fatalf("click: %q", got[1].Content)
	}
	if entryContentString(got[2]) != newPage {
		t.Fatal("latest probe must stay full")
	}
}

func TestFoldComputerUseObservesAlsoFoldsBrowserProbes(t *testing.T) {
	oldScreen := sampleComputerObserveDump("OLD")
	newScreen := sampleComputerObserveDump("NEW")
	oldPage := sampleBrowserProbe("probed page 旧结算", "收货地址旧地址上地九街1号")
	newPage := sampleBrowserProbe("probed page 新结算", "收货地址新地址上地十街10号")
	msgs := sampleComputerObserveConversation(oldScreen, newScreen)
	msgs = append(msgs, sampleBrowserProbeConversation(
		struct{ name, content string }{"browser", oldPage},
		struct{ name, content string }{"browser", newPage},
	)[1:]...)
	got := FoldComputerUseObserves(msgs)
	_, screen := ExtractRoleContent(got[2])
	_, page := ExtractRoleContent(got[6])
	if !isComputerObserveFingerprint(screen) {
		t.Fatalf("older desktop observe was not folded: %q", screen)
	}
	if !isBrowserProbeFingerprint(page) {
		t.Fatalf("older browser probe was not folded with the desktop pass: %q", page)
	}
	if strings.Contains(page, "九街") {
		t.Fatalf("desktop pass kept the old street: %q", page)
	}
}
