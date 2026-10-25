package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func reportPathBinding() admittedSkillBinding {
	return admittedSkillBinding{
		Alias:            "make_report",
		Name:             "make_report",
		StableID:         "skill.make_report",
		DeclaredCaptures: []string{"report_path"},
	}
}

func TestUnassignedSkillCaptureNeverRun(t *testing.T) {
	binding := reportPathBinding()
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	got := cb.UnassignedDeliverables("已经做完")
	if len(got) != 1 || got[0] != "report_path" {
		t.Fatalf("never-run capture = %#v", got)
	}
}

func TestUnassignedSkillCaptureAssignedClears(t *testing.T) {
	binding := reportPathBinding()
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	cb.noteAssignedSkillCaptures(binding, map[string]string{"report_path": "ok"})
	if got := cb.UnassignedDeliverables("已经做完"); len(got) != 0 {
		t.Fatalf("assigned capture still open: %#v", got)
	}
}

func TestUnassignedSkillCaptureEmptyStaysOpen(t *testing.T) {
	binding := reportPathBinding()
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	cb.noteAssignedSkillCaptures(binding, map[string]string{"report_path": "  "})
	got := cb.UnassignedDeliverables("已经做完")
	if len(got) != 1 || got[0] != "report_path" {
		t.Fatalf("blank capture = %#v", got)
	}
}

func TestUnassignedSkillCaptureCanonicalKey(t *testing.T) {
	binding := reportPathBinding()
	binding.DeclaredCaptures = []string{"report-path"}
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	cb.noteAssignedSkillCaptures(binding, map[string]string{"report_path": "C:/out.pdf"})
	if got := cb.UnassignedDeliverables("已经做完"); len(got) != 0 {
		t.Fatalf("canonical capture still open: %#v", got)
	}
}

func TestUnassignedSkillCaptureWithoutDeclaration(t *testing.T) {
	binding := reportPathBinding()
	binding.DeclaredCaptures = nil
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	if got := cb.UnassignedDeliverables("已经做完"); len(got) != 0 {
		t.Fatalf("skill without a capture became a deliverable: %#v", got)
	}
}

func TestUnassignedSkillCaptureDoesNotInheritAcrossSkills(t *testing.T) {
	filled := reportPathBinding()
	other := admittedSkillBinding{
		Alias:            "other_report",
		StableID:         "skill.other",
		DeclaredCaptures: []string{"report_path"},
	}
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{
			filled.Alias: filled,
			other.Alias:  other,
		},
	}
	cb.noteAssignedSkillCaptures(filled, map[string]string{"report_path": "ok"})
	got := cb.UnassignedDeliverables("已经做完")
	if len(got) != 1 || got[0] != "report_path" {
		t.Fatalf("other skill inherited the assignment: %#v", got)
	}
}

func TestUnassignedSkillCaptureSurvivesReadmission(t *testing.T) {
	binding := reportPathBinding()
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	cb.noteAssignedSkillCaptures(binding, map[string]string{"report_path": "ok"})
	cb.clearSkillAdmission()
	cb.admittedSkillBindings = map[string]admittedSkillBinding{binding.Alias: binding}
	if got := cb.UnassignedDeliverables("已经做完"); len(got) != 0 {
		t.Fatalf("readmission forgot the assignment: %#v", got)
	}
}

func TestUnassignedSkillCaptureHiddenWhileToolsPending(t *testing.T) {
	binding := reportPathBinding()
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
		hasPendingToolBatch:   true,
	}
	if got := cb.UnassignedDeliverables("已经做完"); len(got) != 0 {
		t.Fatalf("pending tool batch reported a capture: %#v", got)
	}
}

func TestUnassignedSkillCapturePartialRunKeepsTheRest(t *testing.T) {
	binding := reportPathBinding()
	binding.DeclaredCaptures = []string{"zeta", "alpha"}
	cb := &sharedAgentLoopCallbacks{
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	cb.noteAssignedSkillCaptures(binding, map[string]string{"alpha": "ok"})
	got := cb.UnassignedDeliverables("已经做完")
	if len(got) != 1 || got[0] != "zeta" {
		t.Fatalf("partial capture = %#v", got)
	}
}

func TestFailedSkillRunDoesNotAssignDeclaredCapture(t *testing.T) {
	entry := corelib.NLSkillEntry{
		Name:   "make_report",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action:  "bash",
			Params:  map[string]interface{}{"command": "echo report_path=ok; exit 1"},
			Capture: map[string]string{"report_path": `report_path=(.*)`},
		}},
	}
	app, _ := newSkillExecutorWithEntries(t, entry)
	binding := reportPathBinding()
	binding.StableID = agentservice.DynamicSkillStableID(entry)
	binding.ContentDigest = agentservice.DynamicSkillContentDigest(entry)
	cb := &sharedAgentLoopCallbacks{
		handler:               &IMMessageHandler{app: app},
		admittedSkillBindings: map[string]admittedSkillBinding{binding.Alias: binding},
	}
	_, _, _ = cb.executeAdmittedSkillHostCall(binding.Alias, `{}`)
	got := cb.UnassignedDeliverables("已经做完")
	if len(got) != 1 || got[0] != "report_path" {
		t.Fatalf("failed skill closed its capture: %#v", got)
	}
}
