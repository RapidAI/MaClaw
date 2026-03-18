package main

import (
	"fmt"
	"net/http"
	"time"
)

// swarmCallLLM sends a prompt to the configured LLM and returns the response
// content. This is the shared helper used by TaskSplitter and FeedbackLoop.
func swarmCallLLM(cfg MaclawLLMConfig, prompt string, temperature float64, timeout time.Duration) ([]byte, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("LLM not configured")
	}

	messages := []interface{}{
		map[string]string{"role": "user", "content": prompt},
	}

	client := &http.Client{Timeout: timeout}
	result, err := doSimpleLLMRequest(cfg, messages, client, timeout)
	if err != nil {
		return nil, err
	}
	return []byte(result.Content), nil
}
