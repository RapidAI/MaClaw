package main

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLocalMCPClientConcurrentRequestsDoNotSerializeOnResponse(t *testing.T) {
	reqReader, reqWriter := io.Pipe()
	respReader, respWriter := io.Pipe()

	client := NewLocalMCPClient(corelib.LocalMCPServerEntry{ID: "test", Name: "test"})
	client.stdin = reqWriter
	client.stdout = bufio.NewReader(respReader)
	client.pending = make(map[int64]chan localMCPPendingResponse)
	client.running = true

	go client.readLoop()
	t.Cleanup(func() {
		client.Stop()
		_ = reqReader.Close()
		_ = reqWriter.Close()
		_ = respReader.Close()
		_ = respWriter.Close()
	})

	seen := make(chan jsonRPCRequest, 2)
	go func() {
		scanner := bufio.NewScanner(reqReader)
		for scanner.Scan() {
			var req jsonRPCRequest
			if err := json.Unmarshal(scanner.Bytes(), &req); err == nil {
				seen <- req
			}
		}
	}()

	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, toolName := range []string{"first", "second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			result, err := client.sendRequest("tools/call", map[string]interface{}{"name": name})
			if err != nil {
				results <- "error:" + err.Error()
				return
			}
			results <- string(result)
		}(toolName)
	}

	firstReq := waitForLocalMCPTestRequest(t, seen)
	secondReq := waitForLocalMCPTestRequest(t, seen)
	if firstReq.ID == secondReq.ID {
		t.Fatalf("request ids should differ: %d", firstReq.ID)
	}

	// Respond out of order. Correct routing must follow JSON-RPC id, not request order.
	writeLocalMCPTestResponse(t, respWriter, secondReq.ID, "second-response")
	writeLocalMCPTestResponse(t, respWriter, firstReq.ID, "first-response")

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent requests did not both complete")
	}

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		got[<-results] = true
	}
	if !got[`"first-response"`] || !got[`"second-response"`] {
		t.Fatalf("responses not routed by id: %#v", got)
	}
}

func waitForLocalMCPTestRequest(t *testing.T, requests <-chan jsonRPCRequest) jsonRPCRequest {
	t.Helper()
	select {
	case req := <-requests:
		return req
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for request; client likely serialized on response")
		return jsonRPCRequest{}
	}
}

func writeLocalMCPTestResponse(t *testing.T, w io.Writer, id int64, result string) {
	t.Helper()
	resp := jsonRPCResponse{JSONRPC: "2.0", ID: id}
	resp.Result, _ = json.Marshal(result)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("write response: %v", err)
	}
}
