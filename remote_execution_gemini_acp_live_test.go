//go:build manual
// +build manual

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestGeminiACPLiveHandshake is a manual integration test that launches
// the real Gemini CLI with --experimental-acp and tests the full ACP flow:
// initialize → session/new → session/prompt.
// Run with: go test -tags manual -run TestGeminiACPLiveHandshake -v -count=1 .
func TestGeminiACPLiveHandshake(t *testing.T) {
	geminiPath, err := exec.LookPath("gemini")
	if err != nil {
		t.Skipf("gemini not found in PATH: %v", err)
	}
	t.Logf("Using gemini at: %s", geminiPath)

	cmd := exec.Command(geminiPath, "--experimental-acp")
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			t.Logf("[stderr] %s", scanner.Text())
		}
	}()

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	msgCh := make(chan map[string]interface{}, 64)
	go func() {
		defer close(msgCh)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			t.Logf("[recv] %s", line)
			var msg map[string]interface{}
			if json.Unmarshal([]byte(line), &msg) == nil {
				msgCh <- msg
			}
		}
	}()

	sendRequest := func(id int, method string, params interface{}) {
		req := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  method,
			"params":  params,
		}
		data, _ := json.Marshal(req)
		t.Logf("[send] %s", string(data))
		data = append(data, '\n')
		if _, err := stdin.Write(data); err != nil {
			t.Fatalf("write %s: %v", method, err)
		}
	}

	waitForResponse := func(id int, timeout time.Duration) map[string]interface{} {
		deadline := time.After(timeout)
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					t.Fatalf("stdout closed while waiting for response id=%d", id)
				}
				if msgID, ok := msg["id"]; ok {
					if idFloat, ok := msgID.(float64); ok && int(idFloat) == id {
						return msg
					}
				}
			case <-deadline:
				t.Fatalf("timeout waiting for response id=%d", id)
				return nil
			}
		}
	}

	// Step 1: initialize
	t.Log("=== Step 1: initialize ===")
	sendRequest(1, "initialize", map[string]interface{}{
		"protocolVersion": 1,
		"clientCapabilities": map[string]interface{}{
			"fs":       map[string]interface{}{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]interface{}{
			"name":    "cceasy-test",
			"version": "1.0.0",
		},
	})

	resp1 := waitForResponse(1, 30*time.Second)
	if errObj, ok := resp1["error"]; ok {
		t.Fatalf("initialize error: %v", errObj)
	}
	t.Log("initialize OK")

	// Step 2: session/new
	t.Log("=== Step 2: session/new ===")
	sendRequest(2, "session/new", map[string]interface{}{
		"cwd":        os.TempDir(),
		"mcpServers": []interface{}{},
	})

	resp2 := waitForResponse(2, 30*time.Second)
	if errObj, ok := resp2["error"]; ok {
		t.Fatalf("session/new error: %v", errObj)
	}
	result, _ := resp2["result"].(map[string]interface{})
	sid, _ := result["sessionId"].(string)
	if sid == "" {
		t.Fatalf("session/new: missing sessionId")
	}
	t.Logf("session/new OK: sessionId=%s", sid)

	// Step 3: session/prompt
	t.Log("=== Step 3: session/prompt ===")
	sendRequest(3, "session/prompt", map[string]interface{}{
		"sessionId": sid,
		"prompt": []map[string]interface{}{
			{"type": "text", "text": "Say hello in one short sentence."},
		},
	})

	// Wait for prompt response — Gemini can take a while on first prompt
	resp3 := waitForResponse(3, 180*time.Second)
	if errObj, ok := resp3["error"]; ok {
		t.Fatalf("session/prompt error: %v", errObj)
	}
	promptResult, _ := resp3["result"].(map[string]interface{})
	stopReason, _ := promptResult["stopReason"].(string)
	t.Logf("session/prompt OK: stopReason=%s", stopReason)

	fmt.Println("All steps passed!")
}
