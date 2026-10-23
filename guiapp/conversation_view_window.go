package guiapp

import "strings"

// The conversation view restores and scrolls only a recent tail. Older turns
// stay in an archive the user opens on demand. Keep the cut rules in step
// with guiapp/frontend/src/components/ai/conversationViewWindow.ts.
const (
	conversationViewHotMessages       = 40
	conversationViewTurnAlignLookback = 12
	conversationViewOpenCardLookback  = 30
	conversationViewArchiveMax        = 800
	conversationViewArchiveSuffix     = ".view-archive.json"
)

func conversationViewArchiveFileName(tabID string) string {
	return strings.TrimSpace(tabID) + conversationViewArchiveSuffix
}

func isConversationViewArchiveFile(name string) bool {
	return strings.HasSuffix(name, conversationViewArchiveSuffix)
}

func conversationViewCut(n int, roleAt func(i int) string, stayHot func(i int) bool) int {
	if n <= conversationViewHotMessages || conversationViewHotMessages < 1 {
		return 0
	}
	cut := n - conversationViewHotMessages
	if roleAt(cut) != "user" {
		floor := cut - conversationViewTurnAlignLookback
		if floor < 0 {
			floor = 0
		}
		for i := cut; i >= floor; i-- {
			if roleAt(i) == "user" {
				cut = i
				break
			}
		}
	}
	floor := n - conversationViewHotMessages - conversationViewOpenCardLookback
	if floor < 0 {
		floor = 0
	}
	for i := cut - 1; i >= floor; i-- {
		if stayHot(i) {
			cut = i
		}
	}
	return cut
}

func splitConversationViewAny(messages []interface{}) (hot, earlier []interface{}) {
	if len(messages) == 0 {
		return []interface{}{}, []interface{}{}
	}
	cut := conversationViewCut(len(messages), func(i int) string {
		return conversationViewRole(messages[i])
	}, func(i int) bool {
		return conversationViewStayHot(messages[i])
	})
	earlier = append([]interface{}{}, messages[:cut]...)
	hot = append([]interface{}{}, messages[cut:]...)
	return hot, earlier
}

func capConversationViewArchive(messages []interface{}) []interface{} {
	if len(messages) <= conversationViewArchiveMax {
		return messages
	}
	start := len(messages) - conversationViewArchiveMax
	out := make([]interface{}, conversationViewArchiveMax)
	copy(out, messages[start:])
	return out
}

func conversationViewRole(raw interface{}) string {
	message := conversationViewMap(raw)
	if message == nil {
		return ""
	}
	role, _ := message["role"].(string)
	return strings.ToLower(strings.TrimSpace(role))
}

func conversationViewMap(raw interface{}) map[string]interface{} {
	message, _ := raw.(map[string]interface{})
	return message
}

func conversationViewStayHot(raw interface{}) bool {
	message := conversationViewMap(raw)
	if message == nil {
		return false
	}
	if slot, ok := message["unfinishedSlot"].(map[string]interface{}); ok && len(slot) > 0 {
		status, _ := slot["status"].(string)
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "completed", "resumed", "dismissed":
			return false
		default:
			return true
		}
	}
	if confirmation, ok := message["confirmation"].(map[string]interface{}); ok && len(confirmation) > 0 {
		status, _ := confirmation["status"].(string)
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "", "pending":
			return true
		}
	}
	return false
}

func conversationViewIsLocal(raw interface{}) bool {
	message := conversationViewMap(raw)
	if message == nil {
		return false
	}
	key, _ := message["sessionKey"].(string)
	key = strings.TrimSpace(key)
	return key == "" || strings.EqualFold(key, "desktop-user")
}

func conversationViewLocalCount(messages []interface{}) int {
	count := 0
	for _, message := range messages {
		if conversationViewIsLocal(message) {
			count++
		}
	}
	return count
}

func mapsToAny(messages []map[string]interface{}) []interface{} {
	if len(messages) == 0 {
		return []interface{}{}
	}
	out := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		if message != nil {
			out = append(out, message)
		}
	}
	return out
}

func anyToMaps(messages []interface{}) []map[string]interface{} {
	if len(messages) == 0 {
		return []map[string]interface{}{}
	}
	out := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		if mapped := conversationViewMap(message); mapped != nil {
			out = append(out, mapped)
		}
	}
	return out
}

func mergeConversationViewMaps(older, newer []map[string]interface{}) []map[string]interface{} {
	return anyToMaps(mergeConversationSnapshots(mapsToAny(older), mapsToAny(newer)))
}

func splitConversationViewMaps(messages []map[string]interface{}) (hot, earlier []map[string]interface{}) {
	hotAny, earlierAny := splitConversationViewAny(mapsToAny(messages))
	return anyToMaps(hotAny), anyToMaps(earlierAny)
}

func conversationViewIDs(messages []interface{}) []string {
	ids := make([]string, len(messages))
	for i, message := range messages {
		ids[i] = conversationMessageID(message)
	}
	return ids
}

func sameConversationViewIDs(left, right []interface{}) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if conversationMessageID(left[i]) != conversationMessageID(right[i]) {
			return false
		}
	}
	return true
}
