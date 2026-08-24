package main

import (
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

// SemanticEffectResolutionRequest is the desktop operator's finding about one
// operation that ended unknown. It names no plan, selection or provider: those
// are recovered from the operation ledger, so the request cannot aim a verdict
// at an operation other than the one it identifies.
type SemanticEffectResolutionRequest struct {
	OperationID string `json:"operationId"`
	Confirm     bool   `json:"confirm"`
	// Outcome is a word rather than a boolean on purpose. Across the Wails
	// boundary an omitted boolean arrives as false, and false here means "it
	// demonstrably did not happen" -- a verdict. The state being replaced is
	// precisely that nobody knows, so a caller that says nothing must be
	// refused instead of being read as having said "failed".
	Outcome    string `json:"outcome"`
	Evidence   string `json:"evidence"`
	ReasonCode string `json:"reasonCode,omitempty"`
}

const (
	semanticEffectResolutionSucceeded = "succeeded"
	semanticEffectResolutionFailed    = "failed"
)

// ResolveUnknownSemanticEffect is the out-of-band exit for an operation this
// desktop host dispatched but could never observe. Everywhere else the answer
// to "did this happen" comes from the channel; here it comes from the person at
// this machine, and the point is to make that substitution visible rather than
// convenient. It demands an explicit outcome, the evidence actually checked,
// and an identifiable operator, and it records all three in the local audit
// trail before the ledger is touched.
func (a *App) ResolveUnknownSemanticEffect(req SemanticEffectResolutionRequest) error {
	if a == nil {
		return fmt.Errorf("semantic invocation host is unavailable")
	}
	if !req.Confirm {
		return fmt.Errorf("confirm=true is required")
	}
	succeeded, err := semanticEffectResolutionOutcome(req.Outcome)
	if err != nil {
		return err
	}
	if strings.TrimSpace(req.Evidence) == "" {
		return fmt.Errorf("evidence is required")
	}
	// A verdict is only as accountable as the identity attached to it. On a
	// desktop that identity is the account at this machine; if the host cannot
	// name even that, it has nothing to attach and issues no verdict.
	resolvedBy := desktopResolutionOperator()
	if resolvedBy == "" {
		return fmt.Errorf("resolution operator could not be identified")
	}
	routing, err := a.semanticDynamicRoutingForApp()
	if err != nil {
		return err
	}
	operationID := strings.TrimSpace(req.OperationID)
	if err := routing.ResolveUnknownDynamicSemanticExternalEffect(agentservice.DynamicSemanticManualResolution{
		OperationID: operationID, Succeeded: succeeded,
		Evidence: req.Evidence, ResolvedBy: resolvedBy, ReasonCode: req.ReasonCode,
	}); err != nil {
		return err
	}
	a.recordSemanticEffectResolutionAudit(operationID, req, resolvedBy)
	return nil
}

// semanticEffectResolutionOutcome converts the stated outcome into the verdict,
// refusing anything it was not told.
func semanticEffectResolutionOutcome(outcome string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case semanticEffectResolutionSucceeded:
		return true, nil
	case semanticEffectResolutionFailed:
		return false, nil
	default:
		return false, fmt.Errorf("outcome must be stated explicitly as %q or %q", semanticEffectResolutionSucceeded, semanticEffectResolutionFailed)
	}
}

// desktopResolutionOperator names who is answerable for a verdict on this
// machine.
//
// There is no account system here, so the honest answer is the operating
// system account and the host it ran on. That is a fact someone can later go
// and check, which a fixed placeholder like "desktop-user" would not be; the
// exit is worth having only because the name on it means something.
func desktopResolutionOperator() string {
	name := ""
	if current, err := user.Current(); err == nil {
		name = strings.TrimSpace(current.Username)
	}
	if name == "" {
		for _, key := range []string{"USER", "USERNAME", "LOGNAME"} {
			if value := strings.TrimSpace(os.Getenv(key)); value != "" {
				name = value
				break
			}
		}
	}
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	switch {
	case name != "" && host != "":
		return name + "@" + host
	case name != "":
		return name
	default:
		return host
	}
}

func (a *App) recordSemanticEffectResolutionAudit(operationID string, req SemanticEffectResolutionRequest, resolvedBy string) {
	a.ensureAuditLog()
	if a.auditLog == nil {
		return
	}
	outcome := semanticEffectResolutionFailed
	if strings.EqualFold(strings.TrimSpace(req.Outcome), semanticEffectResolutionSucceeded) {
		outcome = semanticEffectResolutionSucceeded
	}
	_ = a.auditLog.Log(security.AuditEntry{
		Timestamp: time.Now(),
		UserID:    resolvedBy,
		ToolName:  "semantic_effect_manual_resolution",
		Arguments: map[string]interface{}{
			"operation_id": operationID,
			"outcome":      outcome,
			"reason_code":  strings.TrimSpace(req.ReasonCode),
			// The evidence itself is the operator's claim and is kept in the
			// ledger as a digest; recording its length here is enough to show
			// that something was supplied without duplicating the text.
			"evidence_length": len(strings.TrimSpace(req.Evidence)),
		},
		RiskLevel:    security.RiskHigh,
		PolicyAction: security.PolicyUserOverride,
		Result:       "unknown_effect_resolved_" + outcome,
		Source:       "semantic_routing",
	})
}
