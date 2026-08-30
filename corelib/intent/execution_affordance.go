package intent

import (
	"strings"
)

// BrowserPublicationAffordance detects tasks whose primary work may be search,
// writing, or analysis, but whose delivery step requires operating a web UI.
// This is an execution affordance: it adds Browser as a secondary intent rather
// than replacing the primary intent.
func BrowserPublicationAffordance(text string) bool {
	msg := strings.ToLower(strings.TrimSpace(text))
	if msg == "" {
		return false
	}

	hasPlatform := false
	for _, marker := range []string{
		"\u77e5\u4e4e", "\u5c0f\u7ea2\u4e66", "\u5fae\u535a", "\u516c\u4f17\u53f7", "\u5fae\u4fe1\u516c\u4f17\u53f7", "b\u7ad9", "bilibili",
		"medium", "reddit", "linkedin", "twitter", "x.com", "zhihu",
	} {
		if strings.Contains(msg, marker) {
			hasPlatform = true
			break
		}
	}
	if !hasPlatform {
		return false
	}

	for _, marker := range []string{
		"\u53d1\u8868", "\u53d1\u5e03", "\u53d1\u5e16", "\u53d1\u8d34", "\u53d1\u6587", "\u53d1\u6587\u7ae0", "\u53d1\u5230", "\u53d1\u5728", "\u6295\u7a3f", "\u63d0\u4ea4", "\u767b\u5f55", "\u767b\u5165", "\u6253\u5f00",
		"publish", "post", "submit", "login", "log in", "sign in", "open",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func applyExecutionAffordances(text string, result *ClassificationResult) {
	// Classifier output is the sole source of capability labels. Text heuristics
	// may serve non-authorizing UX decisions, but cannot add executable intent.
	_ = text
	_ = result
}

// declaredCompositeIntentPair is the only 0.20-gap exception: lookup labels
// may keep document_generate/live_data_visual/office (and vice versa) even
// when the score gap is wide. The office pair is escalation evidence only —
// measurements on the installed 768-dim model (2026-08-25) show office-only
// negatives ("把数据整理成Excel表格" search 0.737) outscoring genuine
// find-images-online requests ("网上找几张布偶猫照片，做成生日PPT" search
// 0.652), so no local floor separates them and the pair must stay a tree
// decision, never a local grant.
func declaredCompositeIntentPair(left, right IntentLabel) bool {
	return (isLookupIntentLabel(left) && right == LabelDocumentGenerate) ||
		(isLookupIntentLabel(right) && left == LabelDocumentGenerate) ||
		(isLookupIntentLabel(left) && right == LabelLiveDataVisual) ||
		(isLookupIntentLabel(right) && left == LabelLiveDataVisual) ||
		(isLookupIntentLabel(left) && right == LabelOffice) ||
		(isLookupIntentLabel(right) && left == LabelOffice)
}

// locallyVerifiedEmbeddingCompositePair limits the L2 fast path to capability
// chains whose prerequisite has a distinct semantic boundary in the installed
// embedding model. Search is intentionally excluded: supplied material can be
// close to a search request in embedding space, so search + document_generate
// remains a tree decision. This preserves the taxonomy's broader composite
// relation without turning a weak retrieval resemblance into a network grant.
func locallyVerifiedEmbeddingCompositePair(left, right IntentLabel) bool {
	return (left == LabelLiveData && right == LabelDocumentGenerate) ||
		(right == LabelLiveData && left == LabelDocumentGenerate)
}

// NormalizeDeclaredComposite gives an approved lookup+document composite a
// canonical direction. Classification candidates are sorted by confidence, but
// a document_generate candidate can occasionally score above its lookup
// prerequisite. The execution graph is directional regardless: facts must be
// acquired before they are rendered. Keeping the lookup as Primary makes that
// dependency explicit to every consumer without deriving a capability from
// surface wording.
func NormalizeDeclaredComposite(result *ClassificationResult) {
	if result == nil || result.Primary != LabelDocumentGenerate {
		return
	}
	var lookupLabel IntentLabel
	lookupCount := 0
	for _, label := range result.Secondary {
		// Generic classifier states carry no capability obligation. Any other
		// capability family makes the result multi-purpose rather than the
		// narrowly reviewed live-data-to-document chain.
		if label.IsNonCapabilityLabel() {
			continue
		}
		if !isLookupIntentLabel(label) && label != LabelDocumentGenerate {
			return
		}
		if isLookupIntentLabel(label) {
			lookupCount++
			if lookupLabel == "" {
				lookupLabel = label
			}
		}
	}
	if lookupLabel == "" || lookupCount != 1 {
		return
	}
	secondary := make([]IntentLabel, 0, len(result.Secondary))
	secondary = append(secondary, LabelDocumentGenerate)
	for _, label := range result.Secondary {
		if label == lookupLabel || label == LabelDocumentGenerate {
			continue
		}
		if isLookupIntentLabel(label) {
			continue
		}
		secondary = append(secondary, label)
	}
	result.Primary = lookupLabel
	result.Secondary = secondary
}

func isLookupIntentLabel(label IntentLabel) bool {
	return label == LabelSearch || label == LabelLiveData || label == LabelWebFetch
}
