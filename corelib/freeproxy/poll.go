package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type pollResult struct {
	Done bool   `json:"done"`
	Text string `json:"text"`
}

// pollResponse repeatedly evaluates pollJS until the response is complete.
// It calls onToken with incremental text deltas.
// pollJS must return a JSON object: {done: bool, text: string}.
func pollResponse(ctx context.Context, cdp *CDPClient, onToken func(string), pollJS string) (string, error) {
	var lastText string
	emptyCount := 0
	maxWait := 5 * time.Minute
	deadline := time.Now().Add(maxWait)
	pollInterval := 500 * time.Millisecond

	// Wait a bit for the response to start appearing
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(1 * time.Second):
	}

	for {
		if time.Now().After(deadline) {
			if lastText != "" {
				return lastText, nil
			}
			return "", fmt.Errorf("timeout waiting for response (%v)", maxWait)
		}

		select {
		case <-ctx.Done():
			if lastText != "" {
				return lastText, nil
			}
			return "", ctx.Err()
		default:
		}

		raw, err := cdp.Evaluate(ctx, pollJS)
		if err != nil {
			// Transient CDP error — retry
			time.Sleep(pollInterval)
			continue
		}

		var pr pollResult
		if err := json.Unmarshal(raw, &pr); err != nil {
			time.Sleep(pollInterval)
			continue
		}

		// Emit incremental tokens
		if len(pr.Text) > len(lastText) {
			delta := pr.Text[len(lastText):]
			onToken(delta)
			lastText = pr.Text
			emptyCount = 0
		} else {
			emptyCount++
		}

		if pr.Done {
			return pr.Text, nil
		}

		// If we've seen no new text for a while after getting some, assume done
		if lastText != "" && emptyCount > 20 {
			return lastText, nil
		}

		time.Sleep(pollInterval)
	}
}
