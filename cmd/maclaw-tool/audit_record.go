package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// AuditRecordRequest is the universal input format for the audit-record command.
type AuditRecordRequest struct {
	ToolName      string                 `json:"tool_name"`
	ToolInput     map[string]interface{} `json:"tool_input,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
	Result        string                 `json:"result,omitempty"`
	OutputSnippet string                 `json:"output_snippet,omitempty"`
	Source        string                 `json:"source,omitempty"`
}

// runAuditRecord executes the audit-record subcommand logic.
// It always returns exit code 0.
func runAuditRecord(auditDir string) int {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "maclaw audit-record: failed to read stdin: %v\n", err)
		return 0
	}

	if len(data) == 0 {
		fmt.Fprintln(os.Stderr, "maclaw audit-record: empty stdin, nothing to record")
		return 0
	}

	var req AuditRecordRequest
	if err := json.Unmarshal(data, &req); err != nil {
		fmt.Fprintf(os.Stderr, "maclaw audit-record: JSON parse error: %v\n", err)
		return 0
	}

	if auditDir == "" {
		auditDir = defaultAuditDir()
	}

	audit, err := security.NewAuditLog(auditDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "maclaw audit-record: audit log init warning: %v\n", err)
		return 0
	}

	// Sensitive info detection on output_snippet
	resultStr := req.Result
	snippet := req.OutputSnippet
	var sensitiveDetected bool
	var sensitiveCategories []string

	if snippet != "" {
		detector := security.NewSensitiveDetector()
		matches := detector.Detect(snippet)
		if len(matches) > 0 {
			sensitiveDetected = true
			seen := map[string]bool{}
			for _, m := range matches {
				if !seen[m.Category] {
					seen[m.Category] = true
					sensitiveCategories = append(sensitiveCategories, m.Category)
				}
			}
			// Redact before writing to audit log
			snippet = detector.Redact(snippet)

			// Append sensitive info to result
			resultStr = appendAuditResult(resultStr,
				fmt.Sprintf("[sensitive_detected: %s]", strings.Join(sensitiveCategories, ", ")))

			// Warn on stdout if sensitive info found
			fmt.Fprintf(os.Stdout, "⚠️ 敏感信息检测: 输出中包含 %s\n", strings.Join(sensitiveCategories, ", "))
		}
	}

	// Build audit entry
	entry := security.AuditEntry{
		Timestamp:           time.Now(),
		SessionID:           req.SessionID,
		ToolName:            req.ToolName,
		Arguments:           req.ToolInput,
		Result:              resultStr,
		Source:              req.Source,
		SensitiveDetected:   sensitiveDetected,
		SensitiveCategories: sensitiveCategories,
		OutputSnippet:       snippet,
	}

	if err := audit.Log(entry); err != nil {
		fmt.Fprintf(os.Stderr, "maclaw audit-record: write audit log warning: %v\n", err)
	}

	// Update session state
	if req.SessionID != "" {
		state, _ := security.LoadSessionState(req.SessionID)
		state.IncrementToolCall()
		_ = state.Save()
	}

	return 0
}

func appendAuditResult(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + " " + addition
}
