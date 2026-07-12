package doctor

import (
	"strings"
	"testing"
	"time"
)

func TestFormatReportIncludesChecks(t *testing.T) {
	text := FormatReport(Report{
		OK:          false,
		Summary:     "1 blocker(s), 0 warning(s) (1 checks)",
		GeneratedAt: time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
		Checks: []Check{
			{ID: "llm.primary", Status: StatusFail, Message: "missing", Hint: "configure LLM"},
		},
	})
	if !strings.Contains(text, "NOT READY") {
		t.Fatalf("text=%q", text)
	}
	if !strings.Contains(text, "llm.primary") || !strings.Contains(text, "configure LLM") {
		t.Fatalf("text=%q", text)
	}
}

func TestFormatReportIncludesSharedLoopLine(t *testing.T) {
	text := FormatReport(Report{
		OK:      true,
		Summary: "ready",
		Checks: []Check{
			{
				ID:      "agent.shared_loop",
				Status:  StatusOK,
				Message: "shared agent loop ON",
				Detail: map[string]any{
					"mode":           "on",
					"percent":        100,
					"config_enabled": true,
				},
			},
		},
	})
	if !strings.Contains(text, "shared-loop: on") {
		t.Fatalf("expected shared-loop summary line, got %q", text)
	}
}
