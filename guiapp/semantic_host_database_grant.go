package guiapp

import "github.com/RapidAI/CodeClaw/corelib/intent"

func (h *IMMessageHandler) databaseHostReadGrant(userText string) bool {
	return h != nil && h.databaseManager != nil && h.databaseManager.HostReadGrant(userText)
}

// applyHostDatabaseReadGrant adds LabelDatabase as a secondary host-owned
// need. It does not rewrite the primary family and does not use model-tool
// history. Coverage then mints business.data.read on the governed surface.
func applyHostDatabaseReadGrant(current intent.ClassificationResult, grant bool) intent.ClassificationResult {
	if !grant || current.HasLabel(intent.LabelDatabase) {
		return current
	}
	current.Secondary = append(append([]intent.IntentLabel(nil), current.Secondary...), intent.LabelDatabase)
	return current
}
