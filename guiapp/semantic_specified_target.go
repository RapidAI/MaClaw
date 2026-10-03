package guiapp

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

// semanticDesktopForwardsBoundFile is the desktop/TUI host that must push a
// specified-target document through imFileSender. An IM channel's gateway
// already delivers the reply attachment; calling the sender there double-sends.
func semanticDesktopForwardsBoundFile(platform string) bool {
	switch normalizeIMMessagePlatformKind(platform) {
	case imMessagePlatformDesktop, imMessagePlatformTUI:
		return true
	default:
		return false
	}
}

// semanticSpecifiedTargetAwaitsGateway reports a specified-target delivery
// whose transport outcome arrives with the channel gateway. Desktop and TUI
// observe imFileSender before the adapter returns, so that outcome is the
// receipt and must not stay pending.
func semanticSpecifiedTargetAwaitsGateway(selection tool.PlannedSelection, platform string) bool {
	return semanticSpecifiedTargetArtifactDelivery(selection) && !semanticDesktopForwardsBoundFile(platform)
}
