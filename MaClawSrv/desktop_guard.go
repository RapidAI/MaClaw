package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Repeated-action guard. A model that is stuck tends to send the very same
// desktop call again and again (Bot 1 on Zhihu: navigate blocked 7 times,
// clipboard_get "SENTINEL-EMPTY" 3 times). The third identical call in a row
// still runs but carries a warning; the fourth and later are refused until the
// model changes action or arguments.
const (
	desktopRepeatWarnAt = 3
	desktopRepeatStopAt = 4
)

// desktopRepeatExempt actions are legitimately repeated or are the way out.
var desktopRepeatExempt = map[string]bool{"request_takeover": true, "deliver": true}

func desktopCallSignature(action string, args map[string]any) string {
	raw, _ := json.Marshal(args) // map keys are sorted by encoding/json
	sum := sha256.Sum256(append([]byte(action+"\x00"), raw...))
	return hex.EncodeToString(sum[:8])
}

// desktopRepeatCheck counts identical consecutive calls in this run. It
// returns a warning to append to a successful result, or an error that
// refuses the call.
func desktopRepeatCheck(scope agentruntime.Scope, action string, args map[string]any) (string, error) {
	st := desktopRunFor(scope)
	if st == nil || desktopRepeatExempt[action] {
		return "", nil
	}
	sig := desktopCallSignature(action, args)
	st.mu.Lock()
	if st.lastSig == sig {
		st.repeat++
	} else {
		st.lastSig, st.repeat = sig, 1
	}
	n := st.repeat
	st.mu.Unlock()
	switch {
	case n >= desktopRepeatStopAt:
		return "", fmt.Errorf("refused: this is the same desktop call (action=%s, same arguments) %d times in a row and it is not making progress. Do not repeat it. Use a different method (another action, a real key event via app_run, a different element, or a screenshot to re-check the page), or call request_takeover / tell the person what is blocking", action, n)
	case n >= desktopRepeatWarnAt:
		return fmt.Sprintf("\n\nWarning: you have now sent the same desktop call (action=%s, same arguments) %d times in a row. One more identical call will be refused. Switch to a different method instead of repeating it.", action, n), nil
	}
	return "", nil
}

// desktopShotWidthEnv pins full-screen screenshots to one width so every
// image the model sees has the same scale. 0 (default) means the native
// screen size; per-call max_width/max_height are then ignored for full shots
// (region zoom still works).
const desktopShotWidthEnv = "MACLAW_DESKTOP_SHOT_WIDTH"

func desktopPinShotSize(opts desktop.ShotOptions) desktop.ShotOptions {
	if !opts.Region.Empty() {
		return opts
	}
	opts.MaxWidth = desktopEnvInt(desktopShotWidthEnv, 0, 0, desktop.ShotMaxEdge)
	if opts.MaxWidth > 0 && opts.MaxWidth < 16 {
		opts.MaxWidth = 0
	}
	opts.MaxHeight = 0
	return opts
}

// desktopModelLabel names the model of this turn for logs: provider/model,
// never the key or URL.
func desktopModelLabel(request agentruntime.TurnRequest) string {
	cfg, ok := turnAppConfig(request)
	if !ok {
		return "unknown"
	}
	llm, err := agentservice.ResolveLLMConfig(cfg)
	if err != nil {
		return "unknown"
	}
	label := strings.TrimSpace(llm.Model)
	if p := strings.TrimSpace(llm.ProviderName); p != "" {
		label = p + "/" + label
	} else if p := strings.TrimSpace(llm.ProviderID); p != "" {
		label = p + "/" + label
	}
	if label == "" {
		return "unknown"
	}
	return strings.ReplaceAll(label, " ", "_")
}

// desktopBudgetLogFields reports this run's step count and budget use.
func desktopBudgetLogFields(scope agentruntime.Scope) string {
	st := desktopRunFor(scope)
	actions, tokens := 0, 0
	if st != nil {
		st.mu.Lock()
		actions, tokens = st.actions, st.imageTokens
		st.mu.Unlock()
	}
	return fmt.Sprintf("step=%d actions=%d/%d image_tokens=%d/%d batch_max=%d", actions, actions, desktopRunMaxActions(), tokens, desktopRunMaxImageTokens(), desktopMaxBatchSteps())
}
