package agentservice

import (
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// TestToolPolicyFromMetadata pins the tool_policy metadata resolution rules.
//
// The unspecified default is deliberately ToolFilterFull, not the
// ToolPolicyNone sentinel: name-level checks treat None and Full identically
// (IsToolAllowedByPolicy default branch; ValidateToolCallByPolicyWithApproval
// exempts both from the database write gate), so the wide default keeps
// behavior identical while "no workflow decision" stops borrowing a sentinel
// value. Message metadata wins over session metadata; an unrecognized value
// falls through to the next source before hitting the default.
func TestToolPolicyFromMetadata(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		messageMetadata map[string]string
		sessionMetadata map[string]string
		want            v2.ToolFilterPolicy
	}{
		{
			name: "unspecified defaults to full",
			want: v2.ToolFilterFull,
		},
		{
			name:            "message metadata wins over session",
			messageMetadata: map[string]string{"tool_policy": "doc_only"},
			sessionMetadata: map[string]string{"tool_policy": "ops_controlled"},
			want:            v2.ToolFilterDocOnly,
		},
		{
			name:            "session metadata used when message silent",
			sessionMetadata: map[string]string{"tool_policy": "planning"},
			want:            v2.ToolFilterPlanning,
		},
		{
			name:            "unrecognized message value falls through to session",
			messageMetadata: map[string]string{"tool_policy": "  yolo  "},
			sessionMetadata: map[string]string{"tool_policy": "ops_controlled"},
			want:            v2.ToolFilterOpsControlled,
		},
		{
			name:            "unrecognized everywhere falls to default",
			messageMetadata: map[string]string{"tool_policy": "yolo"},
			sessionMetadata: map[string]string{"tool_policy": ""},
			want:            v2.ToolFilterFull,
		},
		{
			name:            "explicit full is honored",
			messageMetadata: map[string]string{"tool_policy": "full"},
			want:            v2.ToolFilterFull,
		},
		{
			// The ToolPolicyNone sentinel means "no workflow decision
			// available"; it is never a value a host can declare. A stray
			// "none" in metadata must not resurrect the old behavior where a
			// declared none borrowed the sentinel and bypassed the capability
			// layer entirely — it takes the wide-but-ceilinged execution
			// default instead.
			name:            "sentinel none is not metadata-addressable",
			messageMetadata: map[string]string{"tool_policy": "none"},
			sessionMetadata: map[string]string{"tool_policy": "none"},
			want:            v2.ToolFilterFull,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := toolPolicyFromMetadata(tc.messageMetadata, tc.sessionMetadata)
			if got != tc.want {
				t.Fatalf("toolPolicyFromMetadata = %q, want %q", got, tc.want)
			}
		})
	}
}
