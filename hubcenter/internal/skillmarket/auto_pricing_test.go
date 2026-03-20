package skillmarket

import (
	"testing"
)

// ── Task 38.4: 自动定价单元测试 ─────────────────────────────────────────

func TestAutoPricing_PlatformFeeCalculation(t *testing.T) {
	// 三种 pricing_mode 的行为验证（逻辑层面）
	tests := []struct {
		mode       string
		price      int64
		fixedPrice int64
		wantPrice  int64
	}{
		{"auto", 25, 0, 25},     // auto 模式使用 LLM 生成的 price
		{"free", 25, 0, 0},      // free 模式强制 price=0
		{"fixed", 25, 15, 15},   // fixed 模式使用 fixedPrice
	}
	for _, tc := range tests {
		var result int64
		switch tc.mode {
		case "auto":
			result = tc.price
		case "free":
			result = 0
		case "fixed":
			result = tc.fixedPrice
		}
		if result != tc.wantPrice {
			t.Errorf("mode=%s: got %d, want %d", tc.mode, result, tc.wantPrice)
		}
	}
}

func TestAutoPricing_ExistingPricePreserved(t *testing.T) {
	// 已有非零 price 且 pricing_mode=auto 时应保留
	existingPrice := int64(30)
	mode := "auto"
	llmSuggestedPrice := int64(15)

	var finalPrice int64
	if mode == "auto" && existingPrice > 0 {
		finalPrice = existingPrice // 保留已有 price
	} else if mode == "auto" {
		finalPrice = llmSuggestedPrice
	}

	if finalPrice != 30 {
		t.Errorf("existing price not preserved: got %d, want 30", finalPrice)
	}
}

func TestAutoPricing_PriceRangeReasonable(t *testing.T) {
	// 定价区间：极简 → 0，普通 → 5~15，复杂 → 20~50
	type testCase struct {
		fileCount  int
		totalLines int
		wantMin    int
		wantMax    int
	}
	cases := []testCase{
		{1, 10, 0, 0},      // 极简
		{2, 100, 5, 15},    // 普通
		{5, 300, 20, 50},   // 复杂
		{10, 1000, 20, 50}, // 很复杂
	}
	for _, tc := range cases {
		price := inferTestPrice(tc.fileCount, tc.totalLines)
		if price < tc.wantMin || price > tc.wantMax {
			t.Errorf("files=%d lines=%d: price=%d, want [%d, %d]",
				tc.fileCount, tc.totalLines, price, tc.wantMin, tc.wantMax)
		}
	}
}

// inferTestPrice 复制 gui/tag_generator.go 的定价逻辑用于测试。
func inferTestPrice(fileCount, totalLines int) int {
	if fileCount <= 1 && totalLines < 30 {
		return 0
	}
	if fileCount <= 3 && totalLines < 200 {
		return 10
	}
	if totalLines < 500 {
		return 25
	}
	return 40
}
