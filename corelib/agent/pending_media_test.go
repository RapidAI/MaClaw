package agent

import "testing"

func TestCanInferIntentFromHistoryWithoutClassifierDoesNotUseKeywords(t *testing.T) {
	old := GetUnifiedClassifier()
	SetUnifiedClassifier(nil)
	t.Cleanup(func() { SetUnifiedClassifier(old) })

	entries := []ConversationEntry{{Role: "user", Content: "bug error code review screenshot"}}
	if CanInferIntentFromHistory(entries) {
		t.Fatal("expected false without semantic classifier; local phrase matching must not infer media intent")
	}
}
