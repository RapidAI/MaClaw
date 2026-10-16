package llmservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// TokenBankRequestCapError is a caller-caused refusal. The proxy maps it to
// HTTP 400 by type, not by searching the message, so a provider error that
// happens to mention a cap cannot take this status.
type TokenBankRequestCapError struct {
	Limit    int64
	Estimate int64
}

func (e *TokenBankRequestCapError) Error() string {
	if e == nil {
		return "token bank request exceeds the per-request cap"
	}
	return fmt.Sprintf("token bank input exceeds the per-request cap (%d > %d)", e.Estimate, e.Limit)
}

// applyTokenBankRequestCaps enforces §3.1 for a token-bank member.
// Input over the cap is an error. Output only lowers max_tokens or
// max_completion_tokens, and only when the field is higher than the cap or
// both fields are unset. A smaller caller value is left as it is.
func applyTokenBankRequestCaps(provider *llmpool.ProviderConfig, body map[string]any) error {
	if provider == nil || body == nil || !IsTokenBankMemberID(provider.ID) {
		return nil
	}
	if cap := provider.MaxInputTokensPerRequest; cap > 0 {
		estimate := estimateTokenBankInputTokens(body)
		if estimate > cap {
			return &TokenBankRequestCapError{Limit: cap, Estimate: estimate}
		}
	}
	applyTokenBankOutputCap(body, provider.MaxOutputTokensPerRequest)
	return nil
}

func enforceTokenBankRequestCaps(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, body map[string]any) error {
	err := applyTokenBankRequestCaps(provider, body)
	if err != nil {
		noteTokenBankRequestCap(ctx, cfg, provider, err)
	}
	return err
}

// tokenBankCappedRequestBody copies the top-level map when this member's
// output cap will write max_tokens. The proxy cache key is the caller's body.
// Writing the cap onto that map stores the response under a different key
// than the next identical request, so the owner is paid again. A later member
// in the same attempt must also see the caller's max_tokens, not this one.
func tokenBankCappedRequestBody(provider *llmpool.ProviderConfig, body map[string]any) map[string]any {
	if provider == nil || body == nil || provider.MaxOutputTokensPerRequest <= 0 || !IsTokenBankMemberID(provider.ID) {
		return body
	}
	copied := make(map[string]any, len(body)+1)
	for k, v := range body {
		copied[k] = v
	}
	return copied
}

func applyTokenBankOutputCap(body map[string]any, cap int64) {
	if body == nil || cap <= 0 {
		return
	}
	_, hasMax := body["max_tokens"]
	_, hasComp := body["max_completion_tokens"]
	if !hasMax && !hasComp {
		body["max_tokens"] = cap
		return
	}
	if hasMax {
		if n, ok := tokenBankBodyInt(body["max_tokens"]); !ok || n > cap {
			body["max_tokens"] = cap
		}
	}
	if hasComp {
		if n, ok := tokenBankBodyInt(body["max_completion_tokens"]); !ok || n > cap {
			body["max_completion_tokens"] = cap
		}
	}
}

// estimateTokenBankInputTokens is ceil(total message string length / 4).
// Only message text counts. A 4-byte string is one token; one extra byte
// rounds up.
func estimateTokenBankInputTokens(body map[string]any) int64 {
	if body == nil {
		return 0
	}
	var n int
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case string:
			n += utf8.RuneCountInString(typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(body["messages"])
	if n <= 0 {
		return 0
	}
	return int64((n + 3) / 4)
}

func tokenBankBodyInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n < 0 {
			return 0, false
		}
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func tokenBankCapErr(err error) bool {
	var cap *TokenBankRequestCapError
	return errors.As(err, &cap)
}

func noteTokenBankRequestCap(ctx context.Context, cfg *ProxyConfig, provider *llmpool.ProviderConfig, cause error) {
	if cfg == nil || cfg.TokenBank == nil || provider == nil || cause == nil {
		return
	}
	reporter, ok := cfg.TokenBank.(interface {
		NoteShareModelError(ctx context.Context, shareID, model, message string) error
	})
	if !ok {
		return
	}
	shareID, model, ok := ParseTokenBankMemberID(provider.ID)
	if !ok {
		return
	}
	if err := reporter.NoteShareModelError(ctx, shareID, model, cause.Error()); err != nil {
		// The refusal still stands. Losing the note must not turn it into a 500.
		return
	}
}
