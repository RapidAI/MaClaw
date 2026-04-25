package workflow

import "testing"

// TestIsOnlyConfirmWord verifies that the confirm word detection correctly
// distinguishes pure confirm messages from messages that contain confirm
// words plus modification requests.
func TestIsOnlyConfirmWord(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		// Pure confirm words — should return true
		{"pure_confirm_ok", "确认", true},
		{"pure_confirm_continue", "继续", true},
		{"pure_confirm_no_problem", "没问题", true},
		{"pure_confirm_good", "好的", true},
		{"pure_confirm_pass", "通过", true},
		{"pure_confirm_can", "可以", true},
		{"pure_confirm_next", "下一步", true},

		// Confirm word with punctuation — should return true
		{"confirm_with_period", "确认。", true},
		{"confirm_with_comma", "好的，", true},
		{"confirm_with_exclamation", "没问题！", true},
		{"confirm_with_emoji", "好的👍", true},
		{"confirm_with_spaces", "  确认  ", true},

		// Multiple confirm words — should return true
		{"multiple_confirms", "好的，继续", true},
		{"multiple_confirms_2", "没问题，通过", true},

		// Confirm word + modification request — should return false
		{"confirm_plus_modify", "好的，但是把技术栈改成React", false},
		{"confirm_plus_add", "确认，再加一个登录功能", false},
		{"confirm_plus_change", "没问题，不过需求里漏了XX", false},
		{"confirm_plus_question", "好的，但是性能要求是多少？", false},

		// Non-confirm messages — should return false
		{"unrelated", "查询天气", false},
		{"empty", "", false},
		{"just_punctuation", "，。！", false},

		// Edge cases
		{"confirm_in_longer_text", "我觉得好的方案应该是用Go", false},
		{"short_addition", "好的ok", true}, // "ok" is < 3 runes after stripping
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOnlyConfirmWord(tt.text, confirmWords)
			if got != tt.expected {
				t.Errorf("isOnlyConfirmWord(%q) = %v, want %v", tt.text, got, tt.expected)
			}
		})
	}
}

// TestIsOnlyConfirmWordExported verifies the exported wrapper.
func TestIsOnlyConfirmWordExported(t *testing.T) {
	if !IsOnlyConfirmWordExported("确认") {
		t.Error("IsOnlyConfirmWordExported(\"确认\") should be true")
	}
	if IsOnlyConfirmWordExported("确认，加个登录功能") {
		t.Error("IsOnlyConfirmWordExported(\"确认，加个登录功能\") should be false")
	}
}
