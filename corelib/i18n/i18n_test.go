package i18n

import "testing"

func TestT_ZhLookup(t *testing.T) {
	got := T(MsgAckProcessing, "zh")
	want := "⏳ 需要一点时间处理，请稍候..."
	if got != want {
		t.Errorf("T(%q, %q) = %q, want %q", MsgAckProcessing, "zh", got, want)
	}
}

func TestT_EnLookup(t *testing.T) {
	got := T(MsgAckProcessing, "en")
	want := "⏳ Processing, please wait..."
	if got != want {
		t.Errorf("T(%q, %q) = %q, want %q", MsgAckProcessing, "en", got, want)
	}
}

func TestT_EmptyLangFallbackToZh(t *testing.T) {
	got := T(MsgTaskComplex, "")
	want := T(MsgTaskComplex, "zh")
	if got != want {
		t.Errorf("T with empty lang = %q, want zh fallback %q", got, want)
	}
}

func TestT_UnknownLangFallbackToZh(t *testing.T) {
	got := T(MsgTaskComplex, "fr")
	want := T(MsgTaskComplex, "zh")
	if got != want {
		t.Errorf("T with unknown lang 'fr' = %q, want zh fallback %q", got, want)
	}
}

func TestT_UnknownKeyReturnsKey(t *testing.T) {
	key := "msg.does_not_exist"
	got := T(key, "zh")
	if got != key {
		t.Errorf("T with unknown key = %q, want key itself %q", got, key)
	}
}

func TestTf_FormatWithArgs(t *testing.T) {
	got := Tf(MsgAgentRoundOf, "zh", 2, 5)
	want := "🔄 Agent 推理中（第 2/5 轮）…"
	if got != want {
		t.Errorf("Tf(%q, zh, 2, 5) = %q, want %q", MsgAgentRoundOf, got, want)
	}
}

func TestTf_FormatEn(t *testing.T) {
	got := Tf(MsgAgentRoundOf, "en", 3, 10)
	want := "🔄 Agent reasoning (round 3/10)…"
	if got != want {
		t.Errorf("Tf(%q, en, 3, 10) = %q, want %q", MsgAgentRoundOf, got, want)
	}
}

func TestTf_SingleArg(t *testing.T) {
	got := Tf(MsgAgentRound, "en", 4)
	want := "🔄 Agent reasoning (round 4)…"
	if got != want {
		t.Errorf("Tf(%q, en, 4) = %q, want %q", MsgAgentRound, got, want)
	}
}

func TestTf_FileGeneric(t *testing.T) {
	got := Tf(MsgFileGeneric, "zh", "design.pdf")
	want := "📄 已生成文件 design.pdf，请查看并确认，或提出修改意见。"
	if got != want {
		t.Errorf("Tf(%q, zh, design.pdf) = %q, want %q", MsgFileGeneric, got, want)
	}
}

func TestNormalizeLang_Variants(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"zh-CN", "zh"},
		{"zh-Hans", "zh"},
		{"zh-TW", "zh"},
		{"en-US", "en"},
		{"en-GB", "en"},
		{"", "zh"},
		{"ja", "zh"}, // unknown → fallback
	}
	for _, tc := range cases {
		got := NormalizeLang(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestAllKeysPresent(t *testing.T) {
	// Ensure every key in zh also exists in en and vice versa.
	for key := range translations["zh"] {
		if _, ok := translations["en"][key]; !ok {
			t.Errorf("key %q present in zh but missing in en", key)
		}
	}
	for key := range translations["en"] {
		if _, ok := translations["zh"][key]; !ok {
			t.Errorf("key %q present in en but missing in zh", key)
		}
	}
}
