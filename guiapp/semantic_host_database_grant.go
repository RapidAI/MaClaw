package guiapp

import "github.com/RapidAI/CodeClaw/corelib/intent"

func (h *IMMessageHandler) databaseHostReadGrant(userText string) bool {
	return h != nil && h.databaseManager != nil && h.databaseManager.HostReadGrant(userText)
}

// applyHostDatabaseReadGrant adds LabelDatabase as a secondary host-owned
// need. It does not rewrite the primary family and does not use model-tool
// history. Coverage then mints business.data.read on the governed surface.
func applyHostDatabaseReadGrant(current intent.ClassificationResult, grant bool) intent.ClassificationResult {
	// Discovery petitions database by name. Adding the label keeps the
	// discovery reason and publishes database_query on the first wave.
	if !grant || current.HasLabel(intent.LabelDatabase) || intent.ClassificationIsModelDiscovery(current) {
		return current
	}
	current.Secondary = append(append([]intent.IntentLabel(nil), current.Secondary...), intent.LabelDatabase)
	return current
}
