package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestApplyHostDatabaseReadGrantAddsSecondary(t *testing.T) {
	got := applyHostDatabaseReadGrant(intent.ClassificationResult{Primary: intent.LabelGitInspect}, true)
	if got.Primary != intent.LabelGitInspect || !got.HasLabel(intent.LabelDatabase) {
		t.Fatalf("got primary=%s labels=%v", got.Primary, got.Labels())
	}
	same := applyHostDatabaseReadGrant(got, true)
	count := 0
	for _, label := range same.Secondary {
		if label == intent.LabelDatabase {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("duplicate database secondary: %#v", same.Secondary)
	}
}

func TestApplyHostDatabaseReadGrantNoopWithoutGrant(t *testing.T) {
	got := applyHostDatabaseReadGrant(intent.ClassificationResult{Primary: intent.LabelUnknown}, false)
	if got.HasLabel(intent.LabelDatabase) {
		t.Fatal("no host grant must not add database")
	}
}
