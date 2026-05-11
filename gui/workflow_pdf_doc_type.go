package main

import "github.com/RapidAI/CodeClaw/corelib/swarm"

func workflowPDFDocTypeFromMetadata(docType, phaseID string) swarm.DocType {
	switch workflowPhaseKindFromMetadata(phaseID, docType) {
	case workflowPhaseKind(workflowPhaseRequirements):
		return swarm.DocTypeRequirements
	case workflowPhaseKind(workflowPhaseDesign):
		return swarm.DocTypeDesign
	case workflowPhaseKind(workflowPhaseTasks):
		return swarm.DocTypeTaskPlan
	default:
		return swarm.DocType("")
	}
}
