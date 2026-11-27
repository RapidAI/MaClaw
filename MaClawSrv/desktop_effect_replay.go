package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop/inputcorrect"
)

// An external effect whose outcome was not observed is not issued again in
// the same run. A verified input procedure does not clear that block and
// does not satisfy publish approval.

var (
	desktopAmbiguousEffects sync.Map
	desktopPayLabelRe       = regexp.MustCompile(`(?i)^(支付|付款|确认支付|去支付|pay|pay now)$`)
)

func desktopExternalEffectKind(action string, args map[string]any) string {
	switch action {
	case "publish":
		return inputcorrect.EffectPublish
	case "click":
		label := strings.TrimSpace(desktopArg(args, "text"))
		if desktopPayLabelRe.MatchString(label) {
			return inputcorrect.EffectPay
		}
		if desktopIsPublishLabel(label) {
			return inputcorrect.EffectPublish
		}
	}
	return ""
}

func desktopAmbiguousKey(runID, kind string) string {
	return strings.TrimSpace(runID) + "\x00" + kind
}

func desktopNoteEffectOutcome(runID, action string, args map[string]any, out string, err error) {
	kind := desktopExternalEffectKind(action, args)
	if kind == "" || strings.TrimSpace(runID) == "" || err == nil {
		return
	}
	msg := strings.ToLower(err.Error() + " " + out)
	if strings.Contains(msg, "needs_confirmation") || strings.Contains(msg, "not replayed") {
		return
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out") || strings.Contains(msg, "unobserved") || strings.Contains(msg, "unknown") {
		desktopAmbiguousEffects.Store(desktopAmbiguousKey(runID, kind), true)
	}
}

func desktopRejectAmbiguousReplay(scope agentruntime.Scope, action string, args map[string]any) error {
	kind := desktopExternalEffectKind(action, args)
	if kind == "" {
		return nil
	}
	if _, blocked := desktopAmbiguousEffects.Load(desktopAmbiguousKey(scope.RunID, kind)); !blocked {
		return nil
	}
	decision := inputcorrect.DecideEffect(kind, true, inputcorrect.Procedure{Strategy: inputcorrect.StrategyClipboard, Successes: 1})
	if decision.Replay {
		return nil
	}
	return fmt.Errorf("refused: ambiguous %s outcome is not replayed", kind)
}
