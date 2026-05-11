package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/swarm"
)

func TestWorkflowPDFDocTypeFromMetadata(t *testing.T) {
	tests := []struct {
		name    string
		docType string
		phaseID string
		want    swarm.DocType
	}{
		{name: "requirements doc type", docType: "requirements", want: swarm.DocTypeRequirements},
		{name: "design phase alias", phaseID: "tech_design", want: swarm.DocTypeDesign},
		{name: "tasks phase alias", phaseID: "task_breakdown", want: swarm.DocTypeTaskPlan},
		{name: "task plan doc type", docType: "task_plan", want: swarm.DocTypeTaskPlan},
		{name: "phase wins over stale doc type", docType: "requirements", phaseID: "tasks", want: swarm.DocTypeTaskPlan},
		{name: "unknown", docType: "other", phaseID: "", want: swarm.DocType("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workflowPDFDocTypeFromMetadata(tt.docType, tt.phaseID); got != tt.want {
				t.Fatalf("workflowPDFDocTypeFromMetadata(%q, %q) = %q, want %q", tt.docType, tt.phaseID, got, tt.want)
			}
		})
	}
}
