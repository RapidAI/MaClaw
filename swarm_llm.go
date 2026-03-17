package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// swarmCallLLM sends a prompt to the configured LLM and returns the response
// content. This is the shared helper used by TaskSplitter and FeedbackLoop.
func swarmCallLLM(cfg MaclawLLMConfig, prompt string, temperature float64, timeout time.Duration) ([]byte, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("LLM not configured")
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": temperature,
	})

	req, err := http.NewRequest("POST", cfg.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Extract content from OpenAI-compatible response
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return body, nil // return raw if not standard format
	}
	if len(result.Choices) > 0 {
		return []byte(result.Choices[0].Message.Content), nil
	}
	return body, nil
}
