package intent

import "strings"

// ExplicitCapabilityRequestTrigger reports whether the user text narrowly
// reads as an explicit knowledge-base write request (e.g. 「保存到知识库」「记入
// 备忘录」「记住…」).
//
// trigger only, not authority: this predicate decides whether a degraded
// turn is worth re-trying automatically (P0-1 recovery recall). It must never
// grant tools, bypass fail-closed routing, or activate knowledge_save_* by
// itself. Biased toward precision — a miss only costs one manual resend.
func ExplicitCapabilityRequestTrigger(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	hasSaveVerb := false
	for _, marker := range explicitCapabilitySaveVerbs {
		if strings.Contains(t, marker) {
			hasSaveVerb = true
			break
		}
	}
	if hasSaveVerb {
		for _, dest := range explicitCapabilityKBDests {
			if strings.Contains(t, dest) {
				return true
			}
		}
	}
	// 「记住…」 is the canonical knowledge-write phrasing and carries its own
	// implicit destination; it needs no explicit save verb + destination pair.
	return strings.Contains(t, "记住")
}

// explicitCapabilitySaveVerbs are narrow Chinese "persist this" verbs.
var explicitCapabilitySaveVerbs = []string{
	"保存", "存到", "存入", "存进", "存于", "储存", "存储",
	"记入", "记到", "记进", "记录到", "记下",
	"收录", "收藏",
}

// explicitCapabilityKBDests are knowledge-store destinations that, combined
// with a save verb, mark an explicit knowledge-write request.
var explicitCapabilityKBDests = []string{
	"知识库", "笔记", "备忘录", "备忘", "收藏夹",
}
