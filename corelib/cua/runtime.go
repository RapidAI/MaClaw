package cua

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

// This file holds one JSON-RPC conversation with one driver daemon. The
// named pipe lives in transport_windows.go; unix and tcp sockets, how a
// containerised host reaches its driver, work the same way.
//
// The daemon creates one pipe instance per client, so one runtime owns
// exactly one instance: one request at a time, no multiplexing, no
// interleaved responses. A unix/tcp socket gets the same one-at-a-time
// rule, which is the safe one for a single-client stdio-style bridge.
//
// # Wire dialect
//
// See dispatch.go: tool calls ride on the method "call" with top-level
// name/arguments, and answers carry content[] plus structuredContent.
// Unknown method names get an out-of-envelope {"ok":false,"error":"..."}
// object.
type runtime struct {
	mu     sync.Mutex
	conn   net.Conn
	pipe   pipeTransport
	nextID atomic.Int64
	closed bool
}

// pipeTransport is one named-pipe instance.
type pipeTransport interface {
	write(b []byte) error
	readLine(ctx context.Context) ([]byte, error)
	// stop makes in-flight and future reads fail.
	stop()
	// closeHandle releases the OS handle.
	closeHandle()
}

// isSocketAddr reports whether addr is a unix:// or host:port target.
func isSocketAddr(addr string) bool {
	return strings.Contains(addr, ":")
}

func newRuntime(addr string) (*runtime, error) {
	if !isSocketAddr(addr) {
		p, err := dialPipe(addr)
		if err != nil {
			return nil, err
		}
		return &runtime{pipe: p}, nil
	}
	network, target := "tcp", addr
	if strings.HasPrefix(addr, "unix://") {
		network, target = "unix", strings.TrimPrefix(addr, "unix://")
	}
	conn, err := net.Dial(network, target)
	if err != nil {
		return nil, fmt.Errorf("cua: dial %q: %w", addr, err)
	}
	return &runtime{conn: conn}, nil
}

// roundTrip sends one JSON-RPC request and reads frames until the matching
// response id arrives. Notifications between the write and the response are
// dropped: the driver never issues server->client requests on this
// transport.
func (rt *runtime) roundTrip(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrNoDriver
	}
	id := rt.nextID.Add(1)
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

// await reads lines until the response for id. A response whose "result" is
// null or absent maps to the JSON literal "null".
func (rt *runtime) await(id int64, ctx context.Context) (json.RawMessage, error) {
	idStr := fmt.Sprint(id)
	for {
		line, err := rt.readLine(ctx)
		if err != nil {
			return nil, fmt.Errorf("cua: read: %w", err)
		}
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		var probe struct {
			ID     json.Number `json:"id"`
			Result json.RawMessage
			// The error field is shape-variable across server builds: a
			// JSON-RPC error object, a plain string, or the out-of-envelope
			// {"ok":false,"error":...} object.
			Error json.RawMessage `json:"error"`
			OK    bool            `json:"ok"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return nil, fmt.Errorf("cua: unreadable frame: %w", err)
		}
		if probe.ID == "" {
			// The out-of-envelope error has no id:
			// {"ok":false,"error":"Unknown method: ...","exit_code":65}.
			if !probe.OK && len(probe.Error) > 0 && string(probe.Error) != "null" {
				return nil, fmt.Errorf("cua: %s", errText(probe.Error))
			}
			continue // a notification or a frame we cannot classify
		}
		if len(probe.Error) > 0 && string(probe.Error) != "null" {
			return nil, ErrDriver{Tool: "request", Detail: errText(probe.Error)}
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

// isMethodUnknown reports whether a failure means the method or tool name
// is not on this server build.
func isMethodUnknown(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Unknown method") || strings.Contains(s, "Unknown tool") || strings.Contains(s, "unknown tool")
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

// call runs one tool call and returns its whole result envelope (content
// array, structuredContent, isError) for CallDetail to decode. The tool
// name and its arguments ride in top-level fields beside "method".
func (rt *runtime) call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return nil, ErrNoDriver
	}
	id := rt.nextID.Add(1)
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "call",
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
// Driver. The serve daemon does not expose tools/list; its "unknown
// method" envelope still proves the daemon is alive, and in that case the
// check passes with a bare tool call instead.
func (rt *runtime) listTools(ctx context.Context) error {
	_, err := rt.roundTrip(ctx, "tools/list", nil)
	if err == nil {
		return nil
	}
	if isMethodUnknown(err) {
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
	if rt.conn == nil {
		return ErrNoDriver
	}
	if n, err := rt.conn.Write(b); err != nil {
		return err
	} else if n != len(b) {
		return fmt.Errorf("cua: short write")
	}
	return nil
}

func (rt *runtime) readLine(ctx context.Context) ([]byte, error) {
	if rt.pipe != nil {
		return rt.pipe.readLine(ctx)
	}
	if rt.conn == nil {
		return nil, ErrNoDriver
	}
	return readLineConn(ctx, rt.conn)
}

// close releases the connection. Subsequent calls fail with ErrNoDriver.
func (rt *runtime) close() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed {
		return
	}
	rt.closed = true
	if rt.pipe != nil {
		rt.pipe.stop()
		rt.pipe.closeHandle()
	}
	if rt.conn != nil {
		_ = rt.conn.Close()
	}
}
