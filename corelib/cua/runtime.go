package cua

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// runtime is one JSON-RPC 2.0 conversation with one driver daemon.
//
// The daemon creates one pipe instance per client, so one runtime owns
// exactly one instance: one request at a time, no multiplexing, no
// interleaved responses. A unix/tcp socket (a non-pipe deployment) gets
// the same one-at-a-time rule, which is the safe one for a single-client
// stdio-style bridge.
//
// # Wire dialect
//
// The daemon answers:
//
//   - tools/list  -> {"result":{"tools":[...]}}
//   - tools/call  -> {"result":{"content":[...],"structuredContent":{...}}}
//     or {"error":{"code":...,"message":"..."}}
//
// Unknown method names get an out-of-envelope {"ok":false,"error":"..."}
// object. Tool names are bare; an unknown tool name is a JSON-RPC error
// on the tools/call method, which is the retryable case.
type runtime struct {
	mu     sync.Mutex
	conn   net.Conn // unix / tcp transports
	pipe   *pipeConn
	nextID atomic.Int64
	closed bool
}

func newRuntime(addr string) (*runtime, error) {
	switch {
	case isPipeAddr(addr):
		p, err := dialPipe(addr)
		if err != nil {
			return nil, err
		}
		return &runtime{pipe: p}, nil
	case len(addr) > 7 && addr[:7] == "unix://":
		conn, err := net.Dial("unix", addr[7:])
		if err != nil {
			return nil, fmt.Errorf("cua: dial %q: %w", addr, err)
		}
		return &runtime{conn: conn}, nil
	default:
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("cua: dial %q: %w", addr, err)
		}
		return &runtime{conn: conn}, nil
	}
}

// isPipeAddr reports whether addr names a Windows pipe: the full UNC form
// (\\.\pipe\name) or a bare pipe name.
func isPipeAddr(addr string) bool {
	if len(addr) >= 4 && addr[:2] == `\\` {
		return true
	}
	return !strings.Contains(addr, ":")
}

// roundTrip sends one JSON-RPC request and reads frames until the matching
// response id arrives. Notifications between the write and the response are
// dropped: the driver never issues server->client requests on this
// transport.
func (rt *runtime) roundTrip(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := rt.nextID.Add(1)
	if dl, ok := ctx.Deadline(); ok && rt.conn != nil {
		_ = rt.conn.SetDeadline(dl)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrNoDriver
	}

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := rt.writeLine(body); err != nil {
		return nil, err
	}
	return rt.await(id, ctx)
}

// await reads lines until the response for id. Returns (result, err).
// A response whose "result" is null or absent maps to the JSON literal
// "null".
func (rt *runtime) await(id int64, ctx context.Context) (json.RawMessage, error) {
	idStr := fmt.Sprint(id)
	for {
		line, err := rt.readLine()
		if err != nil {
			return nil, fmt.Errorf("cua: read: %w", err)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var probe struct {
			ID     json.Number `json:"id"`
			Result json.RawMessage
			// The error field is shape-variable across server builds:
			// a JSON-RPC error object, a plain string, or the out-of-
			// envelope {"ok":false,"error":...} object. Decode to
			// RawMessage and normalize after.
			Error json.RawMessage `json:"error"`
			// The out-of-envelope object's top-level fields, when the
			// frame is not JSON-RPC at all.
			OK bool `json:"ok"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return nil, fmt.Errorf("cua: unreadable frame: %w", err)
		}
		if probe.ID == "" {
			// The out-of-envelope error has no id: {"ok":false,
			// "error":"Unknown method: ...", "exit_code":65}.
			if probe.OK == false && len(probe.Error) > 0 && string(probe.Error) != "null" {
				return nil, fmt.Errorf("cua: %s", errText(probe.Error))
			}
			continue // a notification or a frame we cannot classify
		}
		if len(probe.Error) > 0 && string(probe.Error) != "null" {
			return nil, fmt.Errorf("cua: %s", errText(probe.Error))
		}
		if probe.ID.String() != idStr {
			return nil, fmt.Errorf("cua: unexpected response id %s", probe.ID)
		}
		if len(probe.Result) == 0 {
			return json.RawMessage("null"), nil
		}
		return probe.Result, nil
	}
}

// isMethodUnknown reports whether an await failure means the method or tool
// name is not on this server build - the retryable case.
func isMethodUnknown(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Unknown method") || strings.Contains(s, "Unknown tool") || strings.Contains(s, "unknown tool")
}

// call runs one tool call. The OpenBot build serves tool calls through
// the method "call" with the tool name in a top-level "name" field and
// the arguments in a top-level "arguments" object (both beside
// "method", not inside "params"):
//
//	{"jsonrpc":"2.0","id":N,"method":"call","name":"<tool>","arguments":{...}}
//
// A tool name the server does not know comes back as a JSON-RPC error
// ("Unknown tool: <name>"); when the bare name misses and the tool is
// registered under its "computer." prefix, the retry succeeds. The
// "unknown method" case is terminal: it means the call method itself is
// absent, which no tool-name retry can fix.
func (rt *runtime) call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	res, err := rt.toolCall(ctx, "call", tool, args)
	if err != nil {
		if !strings.HasPrefix(tool, "computer.") && isMethodUnknown(err) {
			return rt.toolCall(ctx, "call", "computer."+tool, args)
		}
		return nil, err
	}
	// MCP tool-call results are content arrays; some builds return the
	// tool value directly. Both shapes are handled.
	var wrapped struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(res, &wrapped) != nil || len(wrapped.Content) == 0 {
		return res, nil
	}
	if wrapped.IsError && len(wrapped.Content) > 0 {
		return nil, fmt.Errorf("cua: %s: %s", tool, wrapped.Content[0].Text)
	}
	return json.RawMessage(wrapped.Content[0].Text), nil
}

// toolCall sends one tool invocation through method m. The tool name and
// arguments ride in top-level fields beside "method" (the OpenBot
// dialect); an MCP-standard server that wants them inside "params" is a
// deployment decision, not a protocol one - the host picks the dialect
// when it spawns the daemon.
func (rt *runtime) toolCall(ctx context.Context, m, tool string, args map[string]any) (json.RawMessage, error) {
	id := rt.nextID.Add(1)
	if dl, ok := ctx.Deadline(); ok && rt.conn != nil {
		_ = rt.conn.SetDeadline(dl)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrNoDriver
	}
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  m,
		"name":    tool,
	}
	if len(args) > 0 {
		req["arguments"] = args
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := rt.writeLine(body); err != nil {
		return nil, err
	}
	return rt.await(id, ctx)
}

// listTools performs the liveness check Open() runs before handing out a
// Driver. It tries tools/list first; the current OpenBot build does not
// expose it, and its "unknown method" envelope is a live-daemon signal,
// not a transport fault - in that case the check passes with the bare
// tool-call probe instead.
func (rt *runtime) listTools(ctx context.Context) error {
	_, err := rt.roundTrip(ctx, "tools/list", nil)
	if err == nil {
		return nil
	}
	if isMethodUnknown(err) {
		// The daemon answered with its unknown-method envelope: it is
		// alive. Probe with the no-argument tool call that every build
		// has.
		_, err = rt.call(ctx, "health_report", map[string]any{})
	}
	return err
}

func (rt *runtime) writeLine(b []byte) error {
	return rt.write(append(b, '\n'))
}

func (rt *runtime) write(b []byte) error {
	if rt.pipe != nil {
		return rt.pipe.write(b)
	}
	if n, err := rt.conn.Write(b); err != nil {
		return err
	} else if n != len(b) {
		return fmt.Errorf("cua: short write")
	}
	return nil
}

func (rt *runtime) readLine() ([]byte, error) {
	if rt.pipe != nil {
		return rt.pipe.readLine()
	}
	if rt.conn == nil {
		return nil, ErrNoDriver
	}
	return readLineConn(rt.conn)
}

func (rt *runtime) close() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return
	}
	rt.closed = true
	if rt.pipe != nil {
		rt.pipe.stopSignal()
		procCloseHandle.Call(rt.pipe.h)
	}
	if rt.conn != nil {
		rt.conn.Close()
	}
}

// dialPipe opens one instance of a named-pipe daemon. The daemon creates
// the instance on demand, so the client must use CREATE_ALWAYS:
// OPEN_EXISTING fails with "The parameter is incorrect" while the
// instance count is zero.
func dialPipe(path string) (*pipeConn, error) {
	name := windows.StringToUTF16(pipePath(path))
	ret, _, _ := procCreateFileW.Call(
		uintptr(unsafe.Pointer(&name[0])),
		0x80000000|0x40000000, // GENERIC_READ|GENERIC_WRITE
		0x7,                   // share read/write/delete
		0, 1,                  // CREATE_ALWAYS
		0, 0)
	if ret == ^uintptr(0) {
		return nil, fmt.Errorf("cua: CreateFileW(%s): win32 error", pipePath(path))
	}
	return &pipeConn{h: ret}, nil
}

// pipePath normalizes a pipe address to the \\.\pipe\<name> form that
// CreateFileW wants.
func pipePath(path string) string {
	if len(path) >= 2 && path[:2] == `\\` {
		return path
	}
	return `\\.\pipe\` + strings.TrimLeft(path, `\/`)
}

// errText renders a JSON-RPC error field whether the server sent a plain
// string or the standard {code,message} object.
func errText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return fmt.Sprintf("[%d] %s", obj.Code, obj.Message)
	}
	return string(raw)
}
