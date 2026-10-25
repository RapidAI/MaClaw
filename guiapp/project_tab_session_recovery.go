package guiapp

import "strings"

func tabSessionHasRecoveryWork(session *TabSessionData) bool {
	if session == nil {
		return false
	}
	for _, raw := range session.Conversation {
		message, _ := raw.(map[string]interface{})
		if message == nil {
			continue
		}
		for _, key := range []string{"unfinishedSlot", "unfinished_slot", "recoverableSession", "recoverable_session"} {
			if value, ok := message[key]; ok && value != nil {
				return true
			}
		}
		for _, key := range []string{"resultStatus", "result_status", "status"} {
			status, _ := message[key].(string)
			switch strings.ToLower(strings.TrimSpace(status)) {
			case "incomplete", "interrupted", "running", "pending", "needs_input":
				return true
			}
		}
	}
	return false
}
