package acpagent

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

)

func TestDiscoverModeBStalePID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dir)
	_ = os.MkdirAll(filepath.Join(dir, "acp"), 0o700)
	// Impossible PID on most systems.
	epj, _ := json.Marshal(map[string]any{
		"host": "127.0.0.1", "port": 19999, "pid": 2147483646, "agent": "test",
	})
	_ = os.WriteFile(filepath.Join(dir, "acp", "endpoint.json"), epj, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "acp", "token"), []byte("tok\n"), 0o600)
	if _, ok := DiscoverModeB(); ok {
		// processAlive may return true for weird PIDs on some platforms; only
		// fail when we are sure the PID is dead.
		if !processAlive(2147483646) {
			t.Fatal("expected DiscoverModeB false for dead PID")
		}
	}
}

func TestModeBReadLoopUpdateWithIDStillFansOut(t *testing.T) {
	// Misbehaving peers may attach an id to session/update; must still fan-out
	// as an update and must NOT be treated as a reverse request.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	token := "tok-update-id"
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		conn := NewConn(c, c)
		// initialize
		req, err := conn.ReadRequest()
		if err != nil {
			return
		}
		_ = conn.WriteResponse(req.ID, InitializeResult{
			ProtocolVersion:   1,
			AgentCapabilities: DefaultAgentCapabilities(),
			AgentInfo:         ImplementationInfo{Name: "test"},
		}, nil)
		// Wait for session/new so the client has registered handlers after Dial.
		req, err = conn.ReadRequest()
		if err != nil {
			return
		}
		_ = conn.WriteResponse(req.ID, SessionNewResult{SessionID: "s1"}, nil)
		// session/update with spurious id (notification-shaped body + id).
		line, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      99,
			"method":  "session/update",
			"params": map[string]any{
				"sessionId": "s1",
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]any{"type": "text", "text": "x"},
				},
			},
		})
		_, _ = c.Write(append(line, '\n'))
		time.Sleep(300 * time.Millisecond)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	ep := ModeBEndpoint{Host: "127.0.0.1", Port: port, Token: token}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := DialModeB(ctx, ep)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	reverseHits := 0
	cli.SetReverseHandler(func(req Request) (any, *RPCError) {
		reverseHits++
		return map[string]any{}, nil
	})
	gotUpdate := make(chan struct{}, 1)
	cli.SetUpdateHandler(func(p SessionUpdateParams) {
		select {
		case gotUpdate <- struct{}{}:
		default:
		}
	})
	if _, err := cli.SessionNew(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gotUpdate:
	case <-time.After(2 * time.Second):
		t.Fatal("session/update with id did not fan-out")
	}
	if reverseHits != 0 {
		t.Fatalf("session/update must not hit reverse handler, hits=%d", reverseHits)
	}
}

func TestModeBRoundTripLocal(t *testing.T) {
	// Minimal fake Mode B host: initialize + session/new + prompt
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	token := "test-token-modeb"
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		conn := NewConn(c, c)
		// initialize
		req, err := conn.ReadRequest()
		if err != nil {
			return
		}
		_ = conn.WriteResponse(req.ID, InitializeResult{
			ProtocolVersion:   1,
			AgentCapabilities: DefaultAgentCapabilities(),
			AgentInfo:         ImplementationInfo{Name: "test"},
		}, nil)
		// session/new
		req, err = conn.ReadRequest()
		if err != nil {
			return
		}
		_ = conn.WriteResponse(req.ID, SessionNewResult{SessionID: "s1"}, nil)
		// session/prompt
		req, err = conn.ReadRequest()
		if err != nil {
			return
		}
		_ = conn.WriteNotification("session/update", SessionUpdateParams{
			SessionID: "s1",
			Update: map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "hi"},
			},
		})
		_ = conn.WriteResponse(req.ID, SessionPromptResult{StopReason: StopEndTurn}, nil)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	// write discovery under temp MACLAW_DATA_DIR
	dir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dir)
	_ = os.MkdirAll(filepath.Join(dir, "acp"), 0o700)
	epj, _ := json.Marshal(map[string]any{
		"host": "127.0.0.1", "port": port, "pid": os.Getpid(), "agent": "test",
	})
	_ = os.WriteFile(filepath.Join(dir, "acp", "endpoint.json"), epj, 0o600)
	_ = os.WriteFile(filepath.Join(dir, "acp", "token"), []byte(token+"\n"), 0o600)

	// DiscoverModeB uses MaclawBaseDir OR MACLAW_DATA_DIR — we set env in DiscoverModeB
	// Force by calling DialModeB with explicit endpoint
	ep := ModeBEndpoint{Host: "127.0.0.1", Port: port, Token: token}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := DialModeB(ctx, ep)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	gotUpdate := make(chan struct{}, 1)
	cli.SetUpdateHandler(func(p SessionUpdateParams) {
		select {
		case gotUpdate <- struct{}{}:
		default:
		}
	})
	sid, err := cli.SessionNew(ctx, t.TempDir())
	if err != nil || sid != "s1" {
		t.Fatalf("session new: sid=%q err=%v", sid, err)
	}
	stop, err := cli.SessionPrompt(ctx, sid, []ContentBlock{{Type: "text", Text: "ping"}})
	if err != nil {
		t.Fatal(err)
	}
	if stop != StopEndTurn {
		t.Fatalf("stop=%q", stop)
	}
	select {
	case <-gotUpdate:
	case <-time.After(2 * time.Second):
		t.Fatal("no session/update")
	}
}

// SessionSteer round-trips params and the accepted flag against a fake
// Mode B host, for both accepted and rejected outcomes.
func TestModeBSessionSteerRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	token := "test-token-modeb-steer"
	seen := make(chan SessionSteerParams, 2)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		conn := NewConn(c, c)
		for {
			req, err := conn.ReadRequest()
			if err != nil {
				return
			}
			switch req.Method {
			case "initialize":
				_ = conn.WriteResponse(req.ID, InitializeResult{
					ProtocolVersion:   1,
					AgentCapabilities: DefaultAgentCapabilities(),
					AgentInfo:         ImplementationInfo{Name: "test"},
				}, nil)
			case "session/new":
				_ = conn.WriteResponse(req.ID, SessionNewResult{SessionID: "s1"}, nil)
			case "session/steer":
				var params SessionSteerParams
				_ = json.Unmarshal(req.Params, &params)
				seen <- params
				_ = conn.WriteResponse(req.ID, SessionSteerResult{Accepted: params.Text == "ok-text"}, nil)
			}
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	ep := ModeBEndpoint{Host: "127.0.0.1", Port: port, Token: token}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := DialModeB(ctx, ep)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	sid, err := cli.SessionNew(ctx, t.TempDir())
	if err != nil || sid != "s1" {
		t.Fatalf("session new: sid=%q err=%v", sid, err)
	}

	accepted, err := cli.SessionSteer(ctx, sid, "ok-text")
	if err != nil || !accepted {
		t.Fatalf("steer ok-text: accepted=%v err=%v", accepted, err)
	}
	got := <-seen
	if got.SessionID != "s1" || got.Text != "ok-text" {
		t.Fatalf("host saw %+v", got)
	}

	rejected, err := cli.SessionSteer(ctx, sid, "bad-text")
	if err != nil {
		t.Fatal(err)
	}
	if rejected {
		t.Fatal("expected accepted=false for bad-text")
	}
}
