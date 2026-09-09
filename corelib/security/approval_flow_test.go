package security

import "testing"

func TestParseApprovalReply(t *testing.T) {
	tests := []struct {
		name      string
		reply     string
		wantAppr  bool
		wantScope string
	}{
		// --- 肯定回复 ---
		{name: "中文批准", reply: "批准", wantAppr: true, wantScope: "once"},
		{name: "中文允许", reply: "允许", wantAppr: true, wantScope: "once"},
		{name: "英文 approve", reply: "approve", wantAppr: true, wantScope: "once"},
		{name: "英文 yes 大写", reply: "YES", wantAppr: true, wantScope: "once"},
		{name: "英文 ok", reply: "ok", wantAppr: true, wantScope: "once"},
		{name: "带前后空白", reply: "  批准  ", wantAppr: true, wantScope: "once"},

		// --- 范围类批准 ---
		{name: "批准同类", reply: "批准同类", wantAppr: true, wantScope: "category"},
		{name: "英文 approve category", reply: "approve category", wantAppr: true, wantScope: "category"},
		{name: "批准全部", reply: "批准全部", wantAppr: true, wantScope: "session"},
		{name: "英文 approve all", reply: "approve all", wantAppr: true, wantScope: "session"},

		// --- 否定回复 ---
		{name: "中文拒绝", reply: "拒绝", wantAppr: false, wantScope: "once"},
		{name: "中文不允许", reply: "不允许", wantAppr: false, wantScope: "once"},
		{name: "英文 deny", reply: "deny", wantAppr: false, wantScope: "once"},
		{name: "英文 reject", reply: "reject", wantAppr: false, wantScope: "once"},
		{name: "英文 no 独立词", reply: "no", wantAppr: false, wantScope: "once"},
		{name: "英文 no 带标点", reply: "No, thanks.", wantAppr: false, wantScope: "once"},

		// --- 空回复 / 无意义回复：fail-closed ---
		{name: "空回复", reply: "", wantAppr: false, wantScope: "once"},
		{name: "纯空白", reply: "   ", wantAppr: false, wantScope: "once"},
		{name: "无关内容", reply: "今天天气不错", wantAppr: false, wantScope: "once"},

		// --- 子串误判防护：这些必须不被 "no" 误伤 ---
		{name: "node 部署不含否定", reply: "deploy node", wantAppr: false, wantScope: "once"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseApprovalReply("req-1", tt.reply)
			if got.Approved != tt.wantAppr {
				t.Errorf("Approved = %v, want %v (reply=%q)", got.Approved, tt.wantAppr, tt.reply)
			}
			if got.ApproveScope != tt.wantScope {
				t.Errorf("ApproveScope = %q, want %q (reply=%q)", got.ApproveScope, tt.wantScope, tt.reply)
			}
			if got.RequestID != "req-1" {
				t.Errorf("RequestID = %q, want %q", got.RequestID, "req-1")
			}
		})
	}
}

// TestParseApprovalReply_NoSubstringFalsePositive 保证短英文否定词按整词匹配，
// 不会命中 node / not / know / nothing 等包含 "no" 的正常回复。
func TestParseApprovalReply_NoSubstringFalsePositive(t *testing.T) {
	// 这些回复不含任何肯定关键词，因此预期仍为未批准（fail-closed），
	// 但关键是它们必须走到 default 分支，而不是被 "no" 子串命中。
	for _, reply := range []string{"deploy node", "not sure", "I know it", "nothing"} {
		if got := ParseApprovalReply("req-1", reply); got.Approved {
			t.Errorf("reply %q should not be approved", reply)
		}
	}

	// "yes" 不含 "no"，不应被否定分支拦截。
	if got := ParseApprovalReply("req-1", "yes"); !got.Approved {
		t.Errorf(`reply "yes" should be approved`)
	}
}
