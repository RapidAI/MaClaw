package main

func workflowDocDeliveryMessage(args map[string]interface{}) string {
	phase := workflowPhaseKindFromMetadata(stringVal(args, "phase_id"), stringVal(args, "doc_type"))
	if phase == workflowPhaseUnknown {
		return ""
	}
	return fileDeliveryMessageForPhaseKind(phase, "")
}

func workflowDocDeliveryMessagePayloadFlag(args map[string]interface{}) string {
	return encodeToolPayloadMessage(workflowDocDeliveryMessage(args))
}
