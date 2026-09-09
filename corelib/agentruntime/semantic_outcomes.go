package agentruntime

import (
	"fmt"
	"strings"
)

// PetitionGrantedMessage is the deterministic acknowledgement returned when a
// host expands a governed semantic tool surface. Keeping this text in the
// Runtime contract prevents GUI and headless adapters from teaching the model
// different retry instructions.
func PetitionGrantedMessage(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return fmt.Sprintf("工具 %s 已由主机授权并加入当前工具面，请立即重新发起对 %s 的调用（参数不变）。", name, name)
}

// AdvanceAfterSuccess preserves a successful host result when publishing the
// next semantic surface fails. A surface-advance error must not turn a
// committed artifact into a retryable rejection, otherwise GUI and headless
// clients can duplicate the same external effect.
func AdvanceAfterSuccess(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		result = "The previous step succeeded."
	}
	return result + "\n\n[system] Next-step tools could not be exposed after this succeeded. Continue from the published artifact; do not retry this grant."
}

// SelectionFailed reports whether a trusted tool adapter returned a hard
// rejection.  This is intentionally based on the stable system markers, not
// on a provider/tool name, so every host makes the same retry decision.
func SelectionFailed(result string) bool {
	trimmed := strings.TrimSpace(result)
	return strings.HasPrefix(trimmed, "[system rejected]") || strings.HasPrefix(strings.ToLower(trimmed), "error:")
}

// SelectionOutcomeUnknown reports that an external effect may have landed but
// the adapter cannot prove its outcome. Unknown is deliberately different
// from failure: callers must consume the grant and reconcile instead of
// blindly retrying a potentially duplicated side effect.
func SelectionOutcomeUnknown(result string) bool {
	return strings.HasPrefix(strings.TrimSpace(result), "[system unknown]")
}

// GrantRejectMessage keeps a machine-readable grant code while giving the
// model deterministic recovery guidance. Hosts must not ask users to
// re-authorize a tool that this turn cannot safely execute.
func GrantRejectMessage(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "selection_not_authorized"
	}
	msg := "[system rejected] " + code
	switch {
	case strings.Contains(code, "stale_surface"):
		return msg + ". The tool list changed because of the other call in your batch. Re-issue this call as its own response; do not batch tool calls."
	case strings.Contains(code, "selection_not_authorized"),
		strings.Contains(code, "invocation_grant_replayed"),
		strings.Contains(code, "invocation_grant_expired"),
		strings.Contains(code, "invocation_grant_selection_not_found"):
		return msg + ". If a lookup already returned evidence, answer from that evidence. Do not ask the user to re-authorize tools."
	default:
		return msg
	}
}
