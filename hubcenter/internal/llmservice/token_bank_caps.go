package llmservice

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
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

// applyTokenBankRequestCaps enforces the token-bank input cap and the member
// output cap. Input over the cap is an error and applies only to a token-bank
// member. Output applies to every member whose integrator set a positive
// MaxOutputTokensPerRequest.
func applyTokenBankRequestCaps(provider *llmpool.ProviderConfig, body map[string]any) error {
	if provider == nil || body == nil {
		return nil
	}
	if IsTokenBankMemberID(provider.ID) {
		if cap := provider.MaxInputTokensPerRequest; cap > 0 {
			estimate := estimateTokenBankInputTokens(body)
			if estimate > cap {
				return &TokenBankRequestCapError{Limit: cap, Estimate: estimate}
			}
		}
	}
	ApplyMemberOutputCap(body, provider.MaxOutputTokensPerRequest)
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
// output cap will write max_tokens or max_completion_tokens. The proxy cache
// key is the caller's body. Writing the cap onto that map stores the response
// under a different key than the next identical request. A later member in
// the same attempt must also see the caller's output fields, not this one.
// The copy is for every member with a positive output cap, not only a token
// bank id.
func tokenBankCappedRequestBody(provider *llmpool.ProviderConfig, body map[string]any) map[string]any {
	if provider == nil || body == nil || provider.MaxOutputTokensPerRequest <= 0 {
		return body
	}
	copied := make(map[string]any, len(body)+1)
	for k, v := range body {
		copied[k] = v
	}
	return copied
}

// ApplyMemberOutputCap lowers each present output budget to cap.
// max_tokens, max_completion_tokens, and max_output_tokens are judged
// separately. A usable value at or below the cap keeps its original Go type,
// including 0. A value above the cap, or a value that is not a non-negative
// number, is replaced with the cap. A field the caller did not send is not
// invented, and a smaller sibling is not copied onto the other field.
//
// Newer OpenAI models read max_completion_tokens. Older models read
// max_tokens. Responses reads max_output_tokens. A protocol that can send
// only one number keeps the field that protocol reads: Anthropic uses
// max_tokens, otherwise max_completion_tokens; Responses and Google use
// max_completion_tokens, otherwise max_tokens. When none of the three fields
// is present, max_tokens is set to the cap so an omitted chat budget cannot
// exceed the member limit. cap <= 0 means the integrator did not set a limit.
func ApplyMemberOutputCap(body map[string]any, cap int64) {
	if body == nil || cap <= 0 {
		return
	}
	_, hasMax := body["max_tokens"]
	_, hasComp := body["max_completion_tokens"]
	_, hasOut := body["max_output_tokens"]
	if !hasMax && !hasComp && !hasOut {
		body["max_tokens"] = cap
		return
	}
	corelib.ClampMemberOutputFields(body, cap)
}

// MemberProbeOutputBody is the output-limit object one member probe sends.
// When the caller set neither field, max_tokens is the short probe default
// (64, or 256 when tools are present). The member cap is then applied with
// the same rule as a live forward, so an oversized probe is truncated and a
// smaller probe value is kept.
func MemberProbeOutputBody(cap int64, maxTokens, maxCompletion *int64, tools bool) map[string]any {
	fallback := int64(64)
	if tools {
		fallback = 256
	}
	body := map[string]any{}
	if maxTokens != nil {
		body["max_tokens"] = *maxTokens
	}
	if maxCompletion != nil {
		body["max_completion_tokens"] = *maxCompletion
	}
	if len(body) == 0 {
		body["max_tokens"] = fallback
	}
	ApplyMemberOutputCap(body, cap)
	return body
}

// MemberProbeFieldBudget is the one output number a protocol probe sends.
// keys is that protocol's preference. The first present non-negative value
// wins, including 0, so a caller-chosen zero is not replaced by the other
// field or by the short probe default. An unusable value is skipped.
func MemberProbeFieldBudget(fields map[string]any, fallback int, keys ...string) int {
	if len(keys) == 0 {
		keys = []string{"max_completion_tokens", "max_tokens"}
	}
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		n, usable := tokenBankBodyInt(raw)
		if !usable || n < 0 || n > int64(math.MaxInt) {
			continue
		}
		return int(n)
	}
	return fallback
}

// estimateTokenBankInputTokens is ceil(total message text runes / 4).
// Only message text counts. Image blocks (type image_url, image, or
// input_image) and a bare image_url payload are omitted, so a picture page
// does not consume the per-request input cap. A 4-rune string is one token;
// one extra rune rounds up.
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
			if tokenBankVisionContentBlock(typed) {
				return
			}
			for key, item := range typed {
				if key == "image_url" {
					continue
				}
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

// tokenBankVisionContentBlock reports a picture part. Its strings are image
// bytes, not the message text the per-request cap measures.
func tokenBankVisionContentBlock(block map[string]any) bool {
	kind, _ := block["type"].(string)
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image_url", "image", "input_image":
		return true
	default:
		return false
	}
}

func tokenBankBodyInt(v any) (int64, bool) {
	return corelib.OutputBudgetInt(v)
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
