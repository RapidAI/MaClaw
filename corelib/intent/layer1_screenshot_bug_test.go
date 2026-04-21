package intent

import (
	"testing"
)

// TestBugCondition_ScreenshotKeywords_ClassifyByKeywords tests that screenshot keywords
// ("截屏"/"截图") are NOT misclassified as LabelDocumentDelivery.
//
// **Validates: Requirements 1.1, 1.2, 1.4**
//
// Bug Condition: classifyByKeywords misclassifies screenshot messages because:
// - "截屏" is NOT in the keyword registry at all
// - "截图" is only LabelBrowser Weak (single weak gets deleted)
// - "发给我" wins as LabelDocumentDelivery Strong (confidence=0.92)
//
// These tests are EXPECTED TO FAIL on unfixed code — failure confirms the bug exists.

func TestBugCondition_Screenshot_FaGeiWo_NotDocDelivery(t *testing.T) {
	// Property 1a: "帮我截屏桌面发给我图片" should NOT be classified as document_delivery.
	// Bug: "截屏" not in registry, "发给我" Strong wins → LabelDocumentDelivery
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "帮我截屏桌面发给我图片",
	})

	if result.Primary == LabelDocumentDelivery {
		t.Errorf("BUG CONFIRMED: classifyByKeywords(%q) = %s (confidence=%.2f), want != LabelDocumentDelivery. "+
			"Root cause: '截屏' not in keyword registry, '发给我' Strong wins unopposed.",
			"帮我截屏桌面发给我图片", result.Primary, result.Confidence)
	}
}

func TestBugCondition_Jitu_FaGeiWo_NotDocDelivery(t *testing.T) {
	// Property 1a: "截图桌面发给我" should NOT be classified as document_delivery.
	// Bug: "截图" is Browser Weak (single weak deleted), "发给我" Strong wins
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "截图桌面发给我",
	})

	if result.Primary == LabelDocumentDelivery {
		t.Errorf("BUG CONFIRMED: classifyByKeywords(%q) = %s (confidence=%.2f), want != LabelDocumentDelivery. "+
			"Root cause: '截图' is Browser Weak (single weak deleted), '发给我' Strong wins.",
			"截图桌面发给我", result.Primary, result.Confidence)
	}
}

func TestBugCondition_Jieping_Alone_NotUnknown(t *testing.T) {
	// Property 1a: "帮我截屏" should NOT be classified as LabelUnknown.
	// Bug: "截屏" not in registry → no matches → LabelUnknown
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "帮我截屏",
	})

	if result.Primary == LabelUnknown {
		t.Errorf("BUG CONFIRMED: classifyByKeywords(%q) = %s (confidence=%.2f), want != LabelUnknown. "+
			"Root cause: '截屏' not in keyword registry at all.",
			"帮我截屏", result.Primary, result.Confidence)
	}
}

func TestBugCondition_Jitu_FaGeiWo_Short_NotDocDelivery(t *testing.T) {
	// Property 1a: "截图发给我" should NOT be classified as document_delivery.
	// Bug: "截图" Browser Weak deleted, "发给我" Strong wins
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "截图发给我",
	})

	if result.Primary == LabelDocumentDelivery {
		t.Errorf("BUG CONFIRMED: classifyByKeywords(%q) = %s (confidence=%.2f), want != LabelDocumentDelivery. "+
			"Root cause: '截图' Browser Weak deleted, '发给我' Strong wins.",
			"截图发给我", result.Primary, result.Confidence)
	}
}
