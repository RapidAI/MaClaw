package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedClockAdapter        = "semantic_read_trusted_clock"
	semanticTrustedClockImplementation = "trusted-clock-read-v1"
	semanticTrustedClockCapability     = tool.CapabilityID("information.current_time")
)

func semanticUnpublishedLegacyClockProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == semanticTrustedClockCapability {
			return true
		}
	}
	return false
}

func semanticTrustedClockDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedClockAdapter,
			"description": "Read the current local date, time, weekday, ISO week and timezone. The clock is host-bound; no fields are accepted.",
			"parameters":  semanticTrustedClockInvocationSchema(),
		},
	}
}

func semanticTrustedClockInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedClockArgsAllowed(args map[string]interface{}) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("trusted_clock_arguments_rejected")
}

func (h *IMMessageHandler) readTrustedClock(principalID string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_clock_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_clock_principal_required")
	}
	if h.semanticTrustedClock != nil {
		return h.semanticTrustedClock(principalID)
	}
	return formatTrustedClock(time.Now()), nil
}

func formatTrustedClock(now time.Time) string {
	isoYear, isoWeek := now.ISOWeek()
	return fmt.Sprintf(
		"%04d-%02d-%02d %s ISO week %04d-W%02d %02d:%02d:%02d (timezone: %s)",
		now.Year(), int(now.Month()), now.Day(),
		now.Weekday().String(), isoYear, isoWeek,
		now.Hour(), now.Minute(), now.Second(),
		now.Location().String(),
	)
}

func semanticTrustedClockResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_clock_delivery_token")
	}
	if strings.Contains(text, "current_datetime") || strings.Contains(text, "web_search") {
		return "", fmt.Errorf("trusted_clock_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_clock_empty")
	}
	return text, nil
}
