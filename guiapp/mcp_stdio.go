package guiapp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const mcpStdioUnavailable = "MaClaw GUI 未运行，或 MCP 服务器未开启。请打开 MaClaw，进入专业功能 → 工具 → MCP服务器，打开开关后再试。"
const mcpStdioUnauthorized = "MCP API key 不正确。请在 MaClaw 的专业功能 → 工具 → MCP服务器 核对当前密钥后再试。"
const mcpStdioReadFailed = "读取 MCP 输入失败。"

func isMCPStdioCommand(args []string) bool {
	return len(args) > 1 && args[1] == "mcp"
}

// runMCPStdioProxy forwards one JSON-RPC message per stdin line to the GUI's
// loopback MCP server. The API key stays in the local apikey file, so the
// pasted stdio client config contains no secret.
func runMCPStdioProxy(in io.Reader, out io.Writer) int {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	endpoint, key, ok := readMCPHostDiscovery()
	if !ok {
		writeMCPStdioError(out, nil, mcpStdioUnavailable)
		return 1
	}
	client := &http.Client{Timeout: 35 * time.Minute}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), mcpBodyLimit)
	for scanner.Scan() {
		line := append([]byte(nil), bytes.TrimSpace(scanner.Bytes())...)
		if len(line) == 0 {
			continue
		}
		if fresh, freshKey, freshOK := readMCPHostDiscovery(); freshOK {
			endpoint = fresh
			key = freshKey
		}
		notification := mcpStdioIsNotification(line)
		body, status, err := mcpStdioPost(client, endpoint.URL, key, line)
		if err != nil || (status != http.StatusOK && status != http.StatusAccepted) {
			if !notification {
				message := mcpStdioUnavailable
				if err == nil {
					message = mcpStdioStatusError(status)
				}
				writeMCPStdioError(out, mcpStdioID(line), message)
			}
			continue
		}
		if status == http.StatusAccepted || notification {
			continue
		}
		if len(bytes.TrimSpace(body)) == 0 {
			continue
		}
		if _, err := out.Write(append(bytes.TrimSpace(body), '\n')); err != nil {
			return 1
		}
	}
	if err := scanner.Err(); err != nil {
		writeMCPStdioError(out, nil, mcpStdioReadFailed)
		return 1
	}
	return 0
}

func mcpStdioStatusError(status int) string {
	if status == http.StatusUnauthorized {
		return mcpStdioUnauthorized
	}
	return mcpStdioUnavailable
}

func mcpStdioPost(client *http.Client, endpoint, key string, line []byte) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(line))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, mcpBodyLimit))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		body = mcpStdioSSEData(body)
	}
	return body, resp.StatusCode, nil
}

func mcpStdioSSEData(body []byte) []byte {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			return []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return body
}

func mcpStdioIsNotification(line []byte) bool {
	var head struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(line, &head) != nil {
		return false
	}
	return mcpIsNotification(head.ID)
}

func mcpStdioID(line []byte) json.RawMessage {
	var head struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(line, &head) != nil || mcpIsNotification(head.ID) {
		return json.RawMessage("null")
	}
	return head.ID
}

func writeMCPStdioError(out io.Writer, id json.RawMessage, message string) {
	if out == nil {
		return
	}
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32000,
			"message": message,
		},
	})
	if err != nil {
		raw = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"MCP server unavailable"}}`)
	}
	fmt.Fprintf(out, "%s\n", raw)
}
