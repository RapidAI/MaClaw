package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedComputerUseAdapter        = "semantic_control_trusted_desktop"
	semanticTrustedComputerUseImplementation = "trusted-computer-control-v1"
)

func semanticUnpublishedLegacyComputerUseProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityComputerControlDesktop {
			return true
		}
	}
	return false
}

func semanticTrustedComputerUsePublished(h *IMMessageHandler) bool {
	return h != nil && (h.semanticTrustedComputerUse != nil || trustedComputerUseRuntimeAvailable(h))
}

func trustedComputerUseRuntimeAvailable(h *IMMessageHandler) bool {
	if h == nil || h.app == nil {
		return false
	}
	if cfg := h.app.PeekConfig(); cfg != nil {
		return computerUseEnabledFromConfig(cfg)
	}
	return true
}

// semanticComputerUseActions is the GUI managed desktop vocabulary. It keeps
// the shared observe/click/done floor and adds type and key, so a pay button
// and a password field reach the same handoff the full computer tools use.
// The Hub host desktop keeps the shared floor only.
func semanticComputerUseActions() []string {
	return []string{"observe", "click", "type", "key", "done"}
}

func semanticComputerUseActionAllowed(action string) bool {
	for _, allowed := range semanticComputerUseActions() {
		if action == allowed {
			return true
		}
	}
	return false
}

func semanticComputerUseActionHint() string {
	return "allowed: " + strings.Join(semanticComputerUseActions(), ", ")
}

func semanticTrustedComputerUseDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedComputerUseAdapter,
			"description": "Observe the desktop, then click or type by ref from that observe, or press a key. A pay button and a password field are handed to the person. done finishes the task. Do not pass a secret.",
			"parameters":  semanticTrustedComputerUseInvocationSchema(),
		},
	}
}

func semanticTrustedComputerUseInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			// An unlabelled string leaves the model to guess the verb, which is
			// how the browser adapter started refusing "open" (2026-10-10). The
			// canonicalizer does not read enum, so the check in ArgsAllowed
			// stays the boundary and this is documentation for the model.
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        semanticComputerUseActions(),
				"description": "observe reads the screen and returns refs. click uses ref from that observe. type writes text into ref, or into the caret when ref is omitted; a password or code field is handed to the person. key presses enter, space, or a shortcut such as ctrl+c. done finishes.",
			},
			"ref":  map[string]interface{}{"type": "string", "description": "Element ref from the last observe, such as e3."},
			"text": map[string]interface{}{"type": "string", "description": "type: the text to write. Never a password or one-time code."},
			"keys": map[string]interface{}{"type": "string", "description": "key: enter, space, or a shortcut such as ctrl+c."},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func semanticTrustedComputerUseArgsAllowed(args map[string]interface{}) (map[string]interface{}, error) {
	if args == nil {
		return nil, fmt.Errorf("trusted_computer_use_action_required")
	}
	cleaned := make(map[string]interface{}, len(args))
	hasAction := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("trusted_computer_use_arguments_rejected")
		}
		value = strings.TrimSpace(value)
		switch key {
		case "action":
			cleaned["action"] = strings.ToLower(value)
			hasAction = true
		case "ref", "text", "keys":
			cleaned[key] = value
		case "password", "secret", "code", "pin", "otp":
			return nil, fmt.Errorf("trusted_computer_use_secret_rejected")
		default:
			return nil, fmt.Errorf("trusted_computer_use_arguments_rejected")
		}
	}
	action, _ := cleaned["action"].(string)
	if !hasAction {
		return nil, fmt.Errorf("trusted_computer_use_action_required")
	}
	if action == "" || !semanticComputerUseActionAllowed(action) {
		return nil, fmt.Errorf("trusted_computer_use_action_rejected (%s)", semanticComputerUseActionHint())
	}
	text, _ := cleaned["text"].(string)
	keys, _ := cleaned["keys"].(string)
	if action == "type" && text == "" {
		return nil, fmt.Errorf("trusted_computer_use_text_required")
	}
	if action == "key" && keys == "" {
		return nil, fmt.Errorf("trusted_computer_use_keys_required")
	}
	return cleaned, nil
}

func (h *IMMessageHandler) controlTrustedDesktop(principalID, action string) (string, error) {
	return h.performTrustedDesktop(principalID, map[string]interface{}{"action": action})
}

func (h *IMMessageHandler) performTrustedDesktop(principalID string, args map[string]interface{}) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_computer_use_runtime_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_computer_use_principal_required")
	}
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	if h.semanticTrustedComputerUse != nil {
		return h.semanticTrustedComputerUse(principalID, action)
	}
	if !trustedComputerUseRuntimeAvailable(h) {
		return "", fmt.Errorf("trusted_computer_use_runtime_unavailable")
	}
	var text string
	switch action {
	case "observe":
		// The runtime already decided whether it saw the screen; reading that
		// verdict is the only way to keep a failed observation from arriving
		// as a successful one whose contents happen to be an error message.
		observed, ok := cuObserveResult(map[string]interface{}{})
		if !ok {
			return "", fmt.Errorf("trusted_computer_use_observe_failed")
		}
		text = observed
	case "click":
		ref, _ := args["ref"].(string)
		if strings.TrimSpace(ref) == "" {
			return "", fmt.Errorf("trusted_computer_use_click_target_missing")
		}
		text = cuHandleClick(map[string]interface{}{"ref": ref})
	case "type":
		payload := map[string]interface{}{"text": args["text"]}
		if ref, _ := args["ref"].(string); strings.TrimSpace(ref) != "" {
			payload["ref"] = ref
		}
		text = cuHandleType(payload)
	case "key":
		text = cuHandleKey(map[string]interface{}{"keys": args["keys"]})
	case "done":
		// A refused completion changed nothing, so it is a definite failure
		// rather than an outcome nobody observed.
		completed, ok := cuDoneResult("")
		if !ok {
			return "", fmt.Errorf("trusted_computer_use_done_refused")
		}
		text = completed
	default:
		return "", fmt.Errorf("trusted_computer_use_action_rejected (%s)", semanticComputerUseActionHint())
	}
	text = managedComputerUseModelText(strings.TrimSpace(text))
	if text == "" {
		return "", fmt.Errorf("trusted_computer_use_empty")
	}
	// The settings guard is a sentence. Matching the word "disabled" turned
	// every screen that contained a disabled control into "runtime unavailable".
	if computerUseResultIsRuntimeDisabled(text) {
		return "", fmt.Errorf("trusted_computer_use_runtime_unavailable")
	}
	return text, nil
}

func computerUseResultIsRuntimeDisabled(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "computer_use_enabled=false") || strings.Contains(lower, "computer use is disabled")
}

// managedComputerUseModelText names the tool the desktop model can call.
// Observe and click results still say computer_observe / computer_click,
// which are unpublished on a managed turn, so the next call is "tool not found".
func managedComputerUseModelText(text string) string {
	text = strings.ReplaceAll(text, "hint: Use computer_click with ref=eN (or computer_type/key/scroll/select). ",
		"hint: Use computer_use action=click with ref=eN (or action=type with ref and text, or action=key). ")
	text = strings.ReplaceAll(text, "hint: Click with computer_click x,y in screenshot pixel space, or ref=eN for a drawn mark. ",
		"hint: Use computer_use action=click with ref=eN for a drawn mark. ")
	text = strings.ReplaceAll(text, "call computer_observe again", "call computer_use action=observe again")
	text = strings.ReplaceAll(text, "call computer_observe first", "call computer_use action=observe first")
	text = strings.ReplaceAll(text, "先 computer_observe", "先调用 computer_use action=observe")
	text = strings.ReplaceAll(text, "computer_done", "computer_use action=done")
	text = strings.ReplaceAll(text, "computer_click", "computer_use action=click")
	text = strings.ReplaceAll(text, "computer_type", "computer_use action=type")
	text = strings.ReplaceAll(text, "computer_observe", "computer_use action=observe")
	return text
}

func semanticTrustedComputerUseResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_computer_use_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_computer_use_empty")
	}
	return text, nil
}
