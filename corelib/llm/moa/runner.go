package moa

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// CallRefFunc is injected by the agent package to avoid import cycles.
// Must be non-streaming, tools=nil capable (host passes nil tools).
type CallRefFunc func(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}) (*llm.Response, error)

// AccountedCall is one LLM round for usage accounting.
type AccountedCall struct {
	Config corelib.MaclawLLMConfig
	Usage  *llm.Usage
}

// FanOutResult is the product of running advisors.
type FanOutResult struct {
	Advice   string
	Items    []RefAdvice
	Calls    []AccountedCall
	Progress string // short user-visible progress line
	// RefOK / RefFail count advisors that returned content vs error.
	RefOK   int
	RefFail int
	// Duration is wall time for the parallel fan-out wave.
	Duration time.Duration
}

// Runner runs reference models in parallel.
type Runner struct {
	CallRef CallRefFunc
	// MaxParallel bounds concurrent reference calls (default 3).
	MaxParallel int64
}

// RunReferences fans out to preset.References. Partial failures continue.
func (r *Runner) RunReferences(ctx context.Context, preset ResolvedPreset, conversation []interface{}) FanOutResult {
	out := FanOutResult{}
	if r == nil || r.CallRef == nil || len(preset.References) == 0 {
		return out
	}
	start := time.Now()
	msgs := BuildReferenceMessages(conversation)
	n := int64(len(preset.References))
	maxP := r.MaxParallel
	if maxP <= 0 {
		maxP = 3
	}
	if maxP > n {
		maxP = n
	}
	sem := semaphore.NewWeighted(maxP)
	var mu sync.Mutex
	items := make([]RefAdvice, len(preset.References))
	calls := make([]AccountedCall, 0, len(preset.References))

	timeout := time.Duration(preset.ReferenceTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 45 * time.Second
	}

	g, gctx := errgroup.WithContext(ctx)
	for i, ref := range preset.References {
		i, ref := i, ref
		g.Go(func() error {
			if err := sem.Acquire(gctx, 1); err != nil {
				return nil // cancelled
			}
			defer sem.Release(1)

			advice := RefAdvice{Label: ref.Label}
			cfg := ref.Config
			if strings.HasPrefix(cfg.ProviderName, "error:") {
				advice.Error = strings.TrimPrefix(cfg.ProviderName, "error:")
				mu.Lock()
				items[i] = advice
				mu.Unlock()
				return nil
			}
			if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
				advice.Error = "reference not configured"
				mu.Lock()
				items[i] = advice
				mu.Unlock()
				return nil
			}

			rctx, cancel := context.WithTimeout(gctx, timeout)
			if cfg.TimeoutSec > 0 {
				t2 := time.Duration(cfg.TimeoutSec) * time.Second
				if t2 < timeout {
					// Prefer the tighter per-provider timeout; release the wider parent timer.
					cancel()
					rctx, cancel = context.WithTimeout(gctx, t2)
				}
			}
			defer cancel()

			resp, err := r.CallRef(rctx, cfg, msgs)
			if err != nil {
				advice.Error = err.Error()
			} else if resp == nil || len(resp.Choices) == 0 {
				advice.Error = "empty response"
			} else {
				advice.Content = strings.TrimSpace(resp.Choices[0].Message.Content)
				if advice.Content == "" {
					advice.Content = strings.TrimSpace(resp.Choices[0].Message.ReasoningContent)
				}
				if advice.Content == "" {
					advice.Error = "empty content"
				}
			}
			mu.Lock()
			items[i] = advice
			if resp != nil && resp.Usage != nil {
				calls = append(calls, AccountedCall{Config: cfg, Usage: resp.Usage})
			}
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	out.Items = items
	out.Calls = calls
	out.Advice = FormatAdviceBlock(items)
	out.Duration = time.Since(start)
	okN := 0
	failN := 0
	for _, it := range items {
		if it.Error == "" && it.Content != "" {
			okN++
		} else {
			failN++
		}
	}
	out.RefOK = okN
	out.RefFail = failN
	ms := out.Duration.Milliseconds()
	if ms < 1 && len(items) > 0 {
		ms = 1
	}
	out.Progress = fmt.Sprintf("consulting %d models… (%d ok, %dms)", len(items), okN, ms)
	return out
}
