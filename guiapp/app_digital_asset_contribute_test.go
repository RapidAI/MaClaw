package guiapp

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestDigitalAssetSubmissionProjectsRuntimeStatus(t *testing.T) {
	var view DigitalAssetSubmissionView
	if err := json.Unmarshal([]byte(`{"id":"s1","status":"pending_review"}`), &view); err != nil {
		t.Fatal(err)
	}
	if view.RuntimeStatus != agentruntime.JobStatusPending {
		t.Fatalf("runtime status=%q, want pending", view.RuntimeStatus)
	}
	if err := json.Unmarshal([]byte(`{"id":"s2","status":"approved","runtime_status":"failed"}`), &view); err != nil {
		t.Fatal(err)
	}
	if view.RuntimeStatus != agentruntime.JobStatusFailed {
		t.Fatalf("explicit runtime status=%q, want failed", view.RuntimeStatus)
	}
}

func TestOrgProjectLeaf(t *testing.T) {
	if got := orgProjectLeaf(`D:\work\foo`); got != "foo" {
		t.Fatalf("got %q", got)
	}
	if got := orgProjectLeaf("/tmp/bar"); got != "bar" {
		t.Fatalf("got %q", got)
	}
}

func TestExcludedPersonalContributeSource(t *testing.T) {
	if !isExcludedPersonalContributeSource(knowledge.Source{ID: "dal_1", Title: "x"}) {
		t.Fatal("expected enterprise id excluded")
	}
	if !isExcludedPersonalContributeSource(knowledge.Source{ID: "s1", Labels: []string{"save_scope:session"}}) {
		t.Fatal("expected session excluded")
	}
	if isExcludedPersonalContributeSource(knowledge.Source{ID: "s2", Title: "ok"}) {
		t.Fatal("expected personal source allowed")
	}
}

func TestMergeCodingExperiencesPrefersLocalThenEnterprise(t *testing.T) {
	local := []knowledge.CodingExperience{{Title: "A", TriggerCondition: "a"}}
	ent := []knowledge.CodingExperience{{Title: "A", TriggerCondition: "a"}, {Title: "B", TriggerCondition: "b"}}
	got := mergeCodingExperiences(local, ent, 4)
	if len(got) != 2 || got[0].Title != "A" || got[1].Title != "B" {
		t.Fatalf("got %+v", got)
	}
}
