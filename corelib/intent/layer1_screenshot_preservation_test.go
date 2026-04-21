package intent

import (
	"testing"
)

// Preservation property tests for classifyByKeywords (Property 2a).
// These tests capture baseline behavior that MUST be preserved after the fix.
//
// **Validates: Requirements 3.1, 3.2, 3.4, 3.5**
//
// Observation-first methodology: behavior observed on UNFIXED code, then asserted.

func TestPreservation_DocDelivery_BaoBaoGaoFaGeiWo(t *testing.T) {
	// "把报告发给我" → LabelDocumentDelivery (preserved)
	// "报告" and "发给我" are both LabelDocumentDelivery Strong keywords.
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "把报告发给我",
	})

	if result.Primary != LabelDocumentDelivery {
		t.Errorf("PRESERVATION FAILED: classifyByKeywords(%q) = %s, want LabelDocumentDelivery. "+
			"Pure document delivery messages must continue to classify as document_delivery.",
			"把报告发给我", result.Primary)
	}
}

func TestPreservation_DocDelivery_DaoChuPDF(t *testing.T) {
	// "导出 PDF" → LabelDocumentDelivery (preserved)
	// "导出" and "pdf" are both LabelDocumentDelivery Strong keywords.
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "导出 PDF",
	})

	if result.Primary != LabelDocumentDelivery {
		t.Errorf("PRESERVATION FAILED: classifyByKeywords(%q) = %s, want LabelDocumentDelivery. "+
			"Pure document delivery messages must continue to classify as document_delivery.",
			"导出 PDF", result.Primary)
	}
}

func TestPreservation_SSH_DengLuFuWuQi(t *testing.T) {
	// "登录服务器" → LabelSSH (preserved)
	// "登录服务器" and "服务器" are LabelSSH Strong keywords.
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "登录服务器",
	})

	if result.Primary != LabelSSH {
		t.Errorf("PRESERVATION FAILED: classifyByKeywords(%q) = %s, want LabelSSH. "+
			"SSH messages must continue to classify as ssh.",
			"登录服务器", result.Primary)
	}
}

func TestPreservation_Browser_DaKaiLiuLanQiJieTu(t *testing.T) {
	// "打开浏览器帮我截图" → LabelBrowser (browser Strong "浏览器" wins by priority)
	// "浏览器" is LabelBrowser Strong, which has higher priority than any screenshot keyword.
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "打开浏览器帮我截图",
	})

	if result.Primary != LabelBrowser {
		t.Errorf("PRESERVATION FAILED: classifyByKeywords(%q) = %s, want LabelBrowser. "+
			"Browser strong keyword '浏览器' must win by priority over screenshot keywords.",
			"打开浏览器帮我截图", result.Primary)
	}
}

func TestPreservation_Unknown_NiHaoMa(t *testing.T) {
	// "你好吗" → LabelUnknown (preserved)
	// No keyword matches → LabelUnknown.
	registry := NewKeywordRegistry()
	affinity := NewToolAffinityRegistry()

	result, _ := classifyByKeywords(registry, affinity, MessageContext{
		Text: "你好吗",
	})

	if result.Primary != LabelUnknown {
		t.Errorf("PRESERVATION FAILED: classifyByKeywords(%q) = %s, want LabelUnknown. "+
			"Messages without keyword matches must continue to classify as unknown.",
			"你好吗", result.Primary)
	}
}
