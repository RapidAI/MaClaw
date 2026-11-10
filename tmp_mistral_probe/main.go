// Temporary debug probe: dumps the exact chat/completions request body the
// maclaw GUI path builds for a Mistral config, then (optionally, with
// MISTRAL_SEND=1) posts it to the real API and prints status + reply.
// MISTRAL_AGENT=1 switches to the real agent-loop shape: stream+tools.
package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	corelib "github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func agentTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "bash",
				"description": "Run a shell command. Use the built-in ssh tool instead of invoking ssh/scp/rsync through bash.",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command":     map[string]interface{}{"type": "string", "description": "Shell command to execute"},
						"working_dir": map[string]interface{}{"type": "string", "description": "Working directory (optional)"},
						"timeout":     map[string]interface{}{"type": "integer", "description": "Timeout seconds, default 600, range 240-600", "minimum": 240, "maximum": 600},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "FileRead",
				"description": "按行读取 UTF-8 文本文件。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path":              map[string]interface{}{"type": "string", "description": "要读取的文件路径。"},
						"start_line":        map[string]interface{}{"type": "integer", "description": "起始行号，1-based，默认 1。"},
						"end_line":          map[string]interface{}{"type": "integer", "description": "结束行号，1-based 且包含该行。"},
						"lines":             map[string]interface{}{"type": "integer", "description": "未提供 end_line 时读取的行数，默认 200。"},
						"show_line_numbers": map[string]interface{}{"type": "boolean", "description": "是否显示行号，默认 true。"},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "ripgrep",
				"description": "在项目文件中递归搜索文本/正则表达式。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"pattern":   map[string]interface{}{"type": "string", "description": "要搜索的正则表达式。"},
						"path":      map[string]interface{}{"type": "string", "description": "搜索范围。"},
						"glob":      map[string]interface{}{"type": "string", "description": "可选文件过滤 glob。"},
						"max_results": map[string]interface{}{"type": "integer", "description": "最多返回匹配数。"},
					},
					"required": []string{"pattern"},
				},
			},
		},
	}
}

func main() {
	agent := os.Getenv("MISTRAL_AGENT") == "1"
	model := os.Getenv("MISTRAL_MODEL")
	if model == "" {
		model = "mistral-small-latest"
	}
	cfg := corelib.MaclawLLMConfig{
		URL:             "https://api.mistral.ai/v1",
		Key:             os.Getenv("MISTRAL_KEY"),
		Model:           model,
		Protocol:        "openai",
		ProviderName:    "Mistral AI",
		TimeoutSec:      30,
		MaxOutputTokens: 4096,
	}
	var messages []interface{}
	var opts llm.OpenAIChatRequestOptions
	if agent {
		// Real agent-loop shape (loop.go doLLMRequestStream):
		// Stream=true, Tools set, no tool_choice/parallel (provider default).
		messages = []interface{}{
			map[string]string{"role": "system", "content": "You are a coding agent working in the user's workspace. Use tools to accomplish the task. Be concise."},
			map[string]string{"role": "user", "content": "List the Go files in the current directory and report the total count."},
		}
		opts = llm.OpenAIChatRequestOptions{
			Stream: true, Tools: agentTools(), ExplicitToolReplacement: true,
		}
	} else {
		// GUI TestLLMConfig shape.
		messages = []interface{}{
			map[string]string{"role": "system", "content": "You are testing whether this MaClawSrv LLM configuration works. Reply with exactly: CONFIG_OK"},
			map[string]string{"role": "user", "content": "Return CONFIG_OK"},
		}
	}

	endpoint, body, err := llm.BuildOpenAIChatRequestData(cfg, messages, opts)
	if err != nil {
		fmt.Println("build error:", err)
		os.Exit(1)
	}
	fmt.Println("ENDPOINT:", endpoint)
	fmt.Println("BODY_HDR:", string(body[:min(400, len(body))]), "...(len", len(body), ")")

	if os.Getenv("MISTRAL_SEND") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		fmt.Println("req error:", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("send error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	fmt.Println("HTTP:", resp.StatusCode)
	if strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		shown := 0
		for scanner.Scan() && shown < 12 {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				shown++
				if len(line) > 400 {
					line = line[:400]
				}
				fmt.Println("SSE:", line)
			}
		}
		return
	}
	data, _ := io.ReadAll(resp.Body)
	fmt.Println("RESP:", truncateStr(string(data), 1200))
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
