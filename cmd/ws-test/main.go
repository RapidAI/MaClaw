package main

// Standalone WebSocket test tool for OpenAI Responses API.
// Reads OAuth token from ~/.maclaw/config.json, dials wss://api.openai.com/v1/responses,
// sends a simple "hello" message, and prints the streamed response.
//
// Usage: go run cmd/ws-test/main.go

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type provider struct {
	Name             string `json:"name"`
	URL              string `json:"url"`
	Key              string `json:"key"`
	Model            string `json:"model"`
	AuthType         string `json:"auth_type"`
	WireAPI          string `json:"wire_api"`
	OAuthAccessToken string `json:"oauth_access_token"`
}

type config struct {
	MaclawLLMProviders       []provider `json:"maclaw_llm_providers"`
	MaclawLLMCurrentProvider string     `json:"maclaw_llm_current_provider"`
}

func main() {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".maclaw", "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("读取配置失败: %v (path=%s)", err, cfgPath)
	}

	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("解析配置失败: %v", err)
	}

	// 找当前 provider
	var p *provider
	for i := range cfg.MaclawLLMProviders {
		if cfg.MaclawLLMProviders[i].Name == cfg.MaclawLLMCurrentProvider {
			p = &cfg.MaclawLLMProviders[i]
			break
		}
	}
	if p == nil {
		log.Fatalf("未找到当前 provider: %s", cfg.MaclawLLMCurrentProvider)
	}

	fmt.Printf("Provider: %s\n", p.Name)
	fmt.Printf("URL: %s\n", p.URL)
	fmt.Printf("Model: %s\n", p.Model)
	fmt.Printf("AuthType: %s\n", p.AuthType)
	fmt.Printf("WireAPI: %s\n", p.WireAPI)
	fmt.Printf("Key length: %d\n", len(p.Key))
	fmt.Printf("OAuthAccessToken length: %d\n", len(p.OAuthAccessToken))

	// 选择 token：OAuth 用 exchanged API key (sk-...)，与 Codex CLI 行为一致.
	// OAuthAccessToken 是 Responses API 的原始 JWT；组织账单需要 Admin API Key.
	token := p.Key
	tokenSource := "Key (exchanged API key)"
	// 允许通过环境变量覆盖：USE_KEY=1 强制用 exchanged key
	if os.Getenv("USE_KEY") == "1" && p.Key != "" {
		token = p.Key
		tokenSource = "Key (forced via USE_KEY=1)"
	}
	fmt.Printf("Using: %s\n", tokenSource)
	if len(token) > 20 {
		fmt.Printf("Token prefix: %s...\n", token[:20])
	}

	// 构造 WebSocket URL
	baseURL := strings.TrimRight(p.URL, "/")
	wsURL := strings.Replace(baseURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/responses"
	fmt.Printf("\nWebSocket URL: %s\n", wsURL)

	// 构造 response.create frame
	model := p.Model
	if model == "" {
		model = "gpt-4o"
	}
	// 允许通过环境变量覆盖模型
	if envModel := os.Getenv("MODEL"); envModel != "" {
		model = envModel
	}
	fmt.Printf("Model for request: %s\n", model)

	// --- First: quick HTTP sanity check with same token ---
	fmt.Println("\n--- HTTP sanity check (POST /v1/responses) ---")
	httpURL := strings.TrimRight(p.URL, "/") + "/responses"
	httpBody := fmt.Sprintf(`{"model":"%s","store":false,"stream":false,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"say hi"}]}]}`, model)
	httpReq, _ := http.NewRequest("POST", httpURL, strings.NewReader(httpBody))
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "maclaw-ws-test")
	httpResp, httpErr := http.DefaultClient.Do(httpReq)
	if httpErr != nil {
		fmt.Printf("HTTP request failed: %v\n", httpErr)
	} else {
		respBody := make([]byte, 2048)
		n, _ := httpResp.Body.Read(respBody)
		httpResp.Body.Close()
		fmt.Printf("HTTP %d: %s\n", httpResp.StatusCode, string(respBody[:n]))
		if httpResp.StatusCode != 200 {
			fmt.Println(" HTTP Responses API also fails with this token — problem is token, not WebSocket")
		} else {
			fmt.Println("HTTP Responses API works — token is valid")
		}
	}

	// Dial
	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	headers.Set("User-Agent", "maclaw-ws-test")

	fmt.Println("\n--- Dialing WebSocket ---")
	conn, resp, err := dialer.Dial(wsURL, headers)
	if err != nil {
		if resp != nil {
			fmt.Printf("Handshake HTTP %d\n", resp.StatusCode)
			buf := make([]byte, 4096)
			n, _ := resp.Body.Read(buf)
			fmt.Printf("Body: %s\n", string(buf[:n]))
		}
		log.Fatalf("WebSocket dial failed: %v", err)
	}
	defer conn.Close()
	fmt.Println("WebSocket connected!")

	frame := map[string]interface{}{
		"type":  "response.create",
		"model": model,
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "input_text",
						"text": "Say hello in one sentence.",
					},
				},
			},
		},
		"store": false,
	}
	frameData, _ := json.Marshal(frame)
	fmt.Printf("\n--- Sending frame (%d bytes) ---\n", len(frameData))

	if err := conn.WriteMessage(websocket.TextMessage, frameData); err != nil {
		log.Fatalf("Send failed: %v", err)
	}

	// 读取响应
	fmt.Println("\n--- Reading response frames ---")
	var contentBuf strings.Builder
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("\nRead ended: %v\n", err)
			break
		}

		var base struct {
			Type string `json:"type"`
		}
		json.Unmarshal(msg, &base)

		switch base.Type {
		case "response.output_text.delta":
			var td struct {
				Delta string `json:"delta"`
			}
			json.Unmarshal(msg, &td)
			contentBuf.WriteString(td.Delta)
			fmt.Print(td.Delta)

		case "response.completed":
			var completed struct {
				Response struct {
					Usage *struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
						TotalTokens  int `json:"total_tokens"`
					} `json:"usage"`
				} `json:"response"`
			}
			json.Unmarshal(msg, &completed)
			fmt.Println()
			if completed.Response.Usage != nil {
				u := completed.Response.Usage
				fmt.Printf("\n--- Usage: input=%d output=%d total=%d ---\n",
					u.InputTokens, u.OutputTokens, u.TotalTokens)
			}
			fmt.Println("\nresponse.completed — WebSocket 协议验证成功!")
			return

		case "response.failed":
			fmt.Printf("\nresponse.failed: %s\n", string(msg))
			return

		case "error":
			fmt.Printf("\nerror frame: %s\n", string(msg))
			return

		default:
			// 其他事件类型只打印类型名
			fmt.Printf("[%s] ", base.Type)
		}
	}

	if contentBuf.Len() > 0 {
		fmt.Printf("\n\nPartial content: %s\n", contentBuf.String())
	}
}
