package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type testCase struct {
	Name       string
	Method     string
	URL        string
	HeaderMode string
	Body       any
}

type result struct {
	Name       string
	Method     string
	URL        string
	HeaderMode string
	Status     string
	Body       string
	Err        string
}

func main() {
	baseURL := flag.String("base", "", "Base URL to probe, for example https://ark.cn-beijing.volces.com/api/plan/v3")
	model := flag.String("model", "Auto", "Model name to send in probe payloads")
	timeout := flag.Duration("timeout", 20*time.Second, "HTTP timeout")
	flag.Parse()

	apiKey := strings.TrimSpace(os.Getenv("VOLCENGINE_API_KEY"))
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "VOLCENGINE_API_KEY is required")
		os.Exit(2)
	}
	if strings.TrimSpace(*baseURL) == "" {
		fmt.Fprintln(os.Stderr, "--base is required")
		os.Exit(2)
	}

	base := strings.TrimRight(strings.TrimSpace(*baseURL), "/")
	client := &http.Client{Timeout: *timeout}

	tests := []testCase{
		{
			Name:       "openai-models-bearer",
			Method:     http.MethodGet,
			URL:        base + "/models",
			HeaderMode: "bearer",
		},
		{
			Name:       "openai-models-x-api-key",
			Method:     http.MethodGet,
			URL:        base + "/models",
			HeaderMode: "x-api-key",
		},
		{
			Name:       "responses-bearer",
			Method:     http.MethodPost,
			URL:        base + "/responses",
			HeaderMode: "bearer",
			Body: map[string]any{
				"model":  *model,
				"input":  "hello",
				"stream": false,
			},
		},
		{
			Name:       "responses-x-api-key",
			Method:     http.MethodPost,
			URL:        base + "/responses",
			HeaderMode: "x-api-key",
			Body: map[string]any{
				"model":  *model,
				"input":  "hello",
				"stream": false,
			},
		},
		{
			Name:       "chat-bearer",
			Method:     http.MethodPost,
			URL:        base + "/chat/completions",
			HeaderMode: "bearer",
			Body: map[string]any{
				"model": *model,
				"messages": []map[string]any{
					{"role": "user", "content": "hello"},
				},
				"stream": false,
			},
		},
		{
			Name:       "chat-x-api-key",
			Method:     http.MethodPost,
			URL:        base + "/chat/completions",
			HeaderMode: "x-api-key",
			Body: map[string]any{
				"model": *model,
				"messages": []map[string]any{
					{"role": "user", "content": "hello"},
				},
				"stream": false,
			},
		},
		{
			Name:       "anthropic-models",
			Method:     http.MethodGet,
			URL:        base + "/models",
			HeaderMode: "anthropic",
		},
		{
			Name:       "anthropic-messages",
			Method:     http.MethodPost,
			URL:        base + "/messages",
			HeaderMode: "anthropic",
			Body: map[string]any{
				"model":      *model,
				"max_tokens": 32,
				"messages": []map[string]any{
					{"role": "user", "content": "hello"},
				},
			},
		},
	}

	results := make([]result, 0, len(tests))
	for _, tc := range tests {
		results = append(results, runTest(client, apiKey, tc))
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runTest(client *http.Client, apiKey string, tc testCase) result {
	res := result{
		Name:       tc.Name,
		Method:     tc.Method,
		URL:        tc.URL,
		HeaderMode: tc.HeaderMode,
	}

	var bodyReader io.Reader
	if tc.Body != nil {
		data, err := json.Marshal(tc.Body)
		if err != nil {
			res.Err = err.Error()
			return res
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(tc.Method, tc.URL, bodyReader)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	req.Header.Set("User-Agent", "volcengine-probe/1.0")
	if tc.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	switch tc.HeaderMode {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "x-api-key":
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("x-api-key", apiKey)
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		res.Err = "unknown header mode"
		return res
	}

	httpRes, err := client.Do(req)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	defer httpRes.Body.Close()

	res.Status = httpRes.Status
	body, _ := io.ReadAll(io.LimitReader(httpRes.Body, 2048))
	res.Body = strings.TrimSpace(string(body))
	return res
}
