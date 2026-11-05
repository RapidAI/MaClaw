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

func TestApplyHostDatabaseReadGrantLeavesDiscovery(t *testing.T) {
	discovery := intent.ModelDiscoveryClassification()
	got := applyHostDatabaseReadGrant(discovery, true)
	if got.Primary != discovery.Primary || got.Reason != discovery.Reason || got.HasLabel(intent.LabelDatabase) {
		t.Fatalf("discovery gained a database grant: %+v", got)
	}
}

func TestApplyHostDatabaseReadGrantNoopWithoutGrant(t *testing.T) {
	got := applyHostDatabaseReadGrant(intent.ClassificationResult{Primary: intent.LabelUnknown}, false)
	if got.HasLabel(intent.LabelDatabase) {
		t.Fatal("no host grant must not add database")
	}
}
