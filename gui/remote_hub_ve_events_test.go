package main

import (
	"testing"
)

func TestIsVEEvent(t *testing.T) {
	tests := []struct {
		msgType string
		want    bool
	}{
		{"ve:list_update", true},
		{"ve:status_change", true},
		{"ve:auth_request", true},
		{"ve:approved", true},
		{"ve:rejected", true},
		{"ve:disabled", true},
		{"ve:group_config", true},
		{"session.start", false},
		{"im.user_message", false},
		{"error", false},
		{"", false},
		{"ve", false},  // no colon
		{"VE:list_update", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.msgType, func(t *testing.T) {
			got := isVEEvent(tt.msgType)
			if got != tt.want {
				t.Errorf("isVEEvent(%q) = %v, want %v", tt.msgType, got, tt.want)
			}
		})
	}
}

func TestNormalizeHubInboundMessageType_VEEvents(t *testing.T) {
	tests := []struct {
		msgType string
		want    hubInboundMessageType
	}{
		{"ve:list_update", hubInboundMessageVEEvent},
		{"ve:status_change", hubInboundMessageVEEvent},
		{"ve:auth_request", hubInboundMessageVEEvent},
		{"ve:approved", hubInboundMessageVEEvent},
		{"ve:rejected", hubInboundMessageVEEvent},
		{"ve:disabled", hubInboundMessageVEEvent},
		{"ve:group_config", hubInboundMessageVEEvent},
		// Non-VE events should not match
		{"session.start", hubInboundMessageSessionStart},
		{"im.user_message", hubInboundMessageIMUserMessage},
		{"error", hubInboundMessageError},
		{"ack", hubInboundMessageAck},
		// Unknown types
		{"unknown_type", hubInboundMessageUnknown},
		{"", hubInboundMessageUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.msgType, func(t *testing.T) {
			got := normalizeHubInboundMessageType(tt.msgType)
			if got != tt.want {
				t.Errorf("normalizeHubInboundMessageType(%q) = %q, want %q", tt.msgType, got, tt.want)
			}
		})
	}
}

func TestNormalizeHubInboundMessageType_VEEventWithWhitespace(t *testing.T) {
	// Whitespace should be trimmed
	got := normalizeHubInboundMessageType("  ve:list_update  ")
	if got != hubInboundMessageVEEvent {
		t.Errorf("expected hubInboundMessageVEEvent, got %q", got)
	}
}
