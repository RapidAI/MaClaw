package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// TestOpenCodeFreeBeijingWeatherPDFLive checks the root-cause plan before it
// is treated as product behavior: the free model only calls OpenCode tools,
// so those tools have to run and the loop has to come back with a PDF.
func TestOpenCodeFreeBeijingWeatherPDFLive(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("OPENCODE_LIVE=1 is not set")
	}
	sandbox, err := os.MkdirTemp("", "opencode-weather-pdf-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sandbox)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	messages := []interface{}{map[string]interface{}{
		"role": "user",
		"content": "北京天气，生成pdf。工作目录是 " + sandbox +
			" 。用 websearch 或 webfetch 获取今天的北京天气，再用 write 或 shell 把 PDF 写到该目录，文件名 beijing-weather.pdf。不要访问其它目录。",
	}}
	var trace []string
	for turn := 1; turn <= 6; turn++ {
		body, err := json.Marshal(map[string]interface{}{
			"model":    "deepseek-v4.1-flash-free",
			"messages": messages,
		})
		if err != nil {
			t.Fatal(err)
		}
		callCtx, callCancel := context.WithTimeout(ctx, 70*time.Second)
		resp, err := CompleteOpenCodeFreeClient(callCtx, body)
		callCancel()
		if err != nil {
			t.Fatalf("turn %d: %v", turn, err)
		}
		if resp == nil || len(resp.Choices) == 0 {
			t.Fatalf("turn %d: empty response", turn)
		}
		msg := resp.Choices[0].Message
		names := make([]string, 0, len(msg.ToolCalls))
		for _, call := range msg.ToolCalls {
			names = append(names, call.Function.Name)
		}
		trace = append(trace, fmt.Sprintf("turn %d tools=%v content_len=%d", turn, names, len(strings.TrimSpace(msg.Content))))
		t.Log(trace[len(trace)-1])
		if len(msg.ToolCalls) == 0 {
			break
		}
		assistant := map[string]interface{}{
			"role":       "assistant",
			"content":    msg.Content,
			"tool_calls": msg.ToolCalls,
		}
		messages = append(messages, assistant)
		for _, call := range msg.ToolCalls {
			result := runOpenCodeProofTool(ctx, sandbox, call.Function.Name, call.Function.Arguments)
			if len(result) > 4000 {
				result = result[:4000]
			}
			t.Logf("  %s -> %s", call.Function.Name, oneLine(result, 180))
			messages = append(messages, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": call.ID,
				"content":      result,
			})
		}
		if pdf := findPDF(sandbox); pdf != "" {
			info, err := os.Stat(pdf)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() < 500 {
				t.Fatalf("pdf %s is only %d bytes", pdf, info.Size())
			}
			t.Logf("pdf=%s bytes=%d", pdf, info.Size())
			return
		}
	}
	t.Fatalf("no pdf in %s\n%s", sandbox, strings.Join(trace, "\n"))
}

func runOpenCodeProofTool(ctx context.Context, sandbox, name, argsJSON string) string {
	var args map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &args) != nil {
		args = map[string]interface{}{}
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "websearch":
		query, _ := args["query"].(string)
		results, err := websearch.SearchCtx(ctx, query, 5)
		if err != nil {
			return "search failed: " + err.Error()
		}
		var b strings.Builder
		for i, item := range results {
			fmt.Fprintf(&b, "%d. %s\n%s\n%s\n\n", i+1, item.Title, item.URL, item.Snippet)
		}
		if b.Len() == 0 {
			return "no results"
		}
		return b.String()
	case "webfetch":
		rawURL, _ := args["url"].(string)
		return proofFetch(ctx, rawURL)
	case "write":
		path, _ := args["filePath"].(string)
		if path == "" {
			path, _ = args["path"].(string)
		}
		content, _ := args["content"].(string)
		target, err := confineProofPath(sandbox, path)
		if err != nil {
			return err.Error()
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err.Error()
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err.Error()
		}
		return "wrote " + target
	case "read":
		path, _ := args["filePath"].(string)
		if path == "" {
			path, _ = args["path"].(string)
		}
		target, err := confineProofPath(sandbox, path)
		if err != nil {
			return err.Error()
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return err.Error()
		}
		if len(data) > 4000 {
			data = data[:4000]
		}
		return string(data)
	case "shell":
		command, _ := args["command"].(string)
		return proofShell(ctx, sandbox, command)
	default:
		return "tool " + name + " is not available in this proof; use websearch, webfetch, write, read, or shell"
	}
}

func proofFetch(ctx context.Context, rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "https://") && !strings.HasPrefix(rawURL, "http://") {
		return "unsupported url"
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err.Error()
	}
	req.Header.Set("User-Agent", "maclaw-opencode-proof")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 24*1024))
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("status %d\n%s", resp.StatusCode, string(body))
}

func proofShell(ctx context.Context, sandbox, command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "empty command"
	}
	runCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "cmd", "/c", command)
	cmd.Dir = sandbox
	out, err := cmd.CombinedOutput()
	text := string(out)
	if len(text) > 4000 {
		text = text[:4000]
	}
	if err != nil {
		return text + "\n" + err.Error()
	}
	if strings.TrimSpace(text) == "" {
		return "exit 0"
	}
	return text
}

func confineProofPath(sandbox, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("missing path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(sandbox, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(sandbox, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		path = filepath.Join(sandbox, filepath.Base(path))
	}
	return path, nil
}

func findPDF(root string) string {
	var found string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".pdf") && info.Size() > 0 {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func oneLine(text string, n int) string {
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > n {
		return text[:n]
	}
	return text
}
