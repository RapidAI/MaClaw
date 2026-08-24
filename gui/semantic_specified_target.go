package main

import (
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticSpecifiedTargetDeliveryAdapter        = "semantic_deliver_specified_target"
	semanticSpecifiedTargetDeliveryImplementation = "specified-target-delivery-v1"
	semanticSpecifiedTargetDeliveryCapability     = tool.CapabilityID("artifact.deliver.specified_target")
)

func semanticSpecifiedTargetDeliveryPublished(channel, destination string) bool {
	return semanticFileDeliveryPublished(channel) && semanticTrustedDispatchDestination(destination)
}

func semanticSpecifiedTargetDeliveryDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticSpecifiedTargetDeliveryAdapter,
			"description": "Deliver a bound document artifact to the host-authenticated destination. No channel or group fields are accepted.",
			"parameters":  semanticSpecifiedTargetDeliveryInvocationSchema(),
		},
	}
}

func semanticSpecifiedTargetDeliveryInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticSpecifiedTargetArtifactDelivery(selection tool.PlannedSelection) bool {
	if selection.Provider.Kind != "channel" {
		return false
	}
	return selection.FitProof.MatchedCapability == semanticSpecifiedTargetDeliveryCapability
}
