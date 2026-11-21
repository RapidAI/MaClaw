package cua

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// This file holds one JSON-RPC conversation with one driver daemon. The
// named pipe lives in transport_windows.go; unix and tcp sockets, how a
// containerised host reaches its driver, work the same way.
//
// One runtime owns one pipe instance: the daemon creates an instance per
// client and a pipe is a byte stream, so at most one request is in flight
// and no two responses can interleave.
//
// # Wire dialect
//
// See dispatch.go: tool calls ride on the method "call" with top-level
// name/arguments, and answers carry content[] plus structuredContent.
// Unknown method names get an out-of-envelope {"ok":false,"error":"..."}.
type runtime struct {
	mu     sync.Mutex
	conn   net.Conn
	pipe   pipeTransport
	nextID atomic.Int64
	// closed is atomic so Close can retire the connection without waiting
	// for the in-flight read to drop mu. That read holds mu for the whole
	// frame, and the only way to unblock a synchronous pipe read is to
	// close the handle from outside that lock.
	closed atomic.Bool
}

// pipeTransport is one named-pipe instance.
type pipeTransport interface {
	write(b []byte) error
	readLine(ctx context.Context) ([]byte, error)
	// stop makes the connection unusable for new reads.
	stop()
	// closeHandle releases the OS handle.
	closeHandle()
}

// isSocketAddr reports whether addr is a unix:// or host:port target.
func isSocketAddr(addr string) bool { return strings.Contains(addr, ":") }

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
// response arrives. Notifications before that response are dropped: the
// driver never issues server->client requests on this transport.
func (rt *runtime) roundTrip(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed.Load() {
		return nil, ErrNoDriver
	}
	id := rt.nextID.Add(1)
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
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

// await reads frames until the one carrying id. The serve daemon's
// envelope is {"ok":true,"result":{...}} with no "id" field, so a frame
// without an id is the answer when it is not an error: one request is in
// flight per connection, so there is nothing else it could belong to.
func (rt *runtime) await(id int64, ctx context.Context) (json.RawMessage, error) {
	for {
		line, err := rt.readLine(ctx)
		if err != nil {
			return nil, fmt.Errorf("cua: read: %w", err)
		}
		var frame struct {
			ID     json.Number     `json:"id"`
			Result json.RawMessage `json:"result"`
			// The error field is shape-variable: a JSON-RPC error object, a
			// plain string, or the out-of-envelope {"ok":false,...} form.
			Error json.RawMessage `json:"error"`
			OK    bool            `json:"ok"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			return nil, fmt.Errorf("cua: unreadable frame: %w", err)
		}
		hasError := len(frame.Error) > 0 && string(frame.Error) != "null"
		if frame.ID == "" {
			if hasError {
				return nil, ErrDriver{Tool: "request", Detail: errText(frame.Error)}
			}
			if len(frame.Result) > 0 {
				return frame.Result, nil
			}
			continue
		}
		if hasError {
			return nil, ErrDriver{Tool: "request", Detail: errText(frame.Error)}
		}
		if frame.ID.String() != fmt.Sprint(id) {
			return nil, fmt.Errorf("cua: response id %s does not answer request %d", frame.ID, id)
		}
		if len(frame.Result) == 0 {
			return json.RawMessage("null"), nil
		}
		return frame.Result, nil
	}
}

// isMethodUnknown reports whether a failure means the method or tool name
// is not served by this daemon build.
func isMethodUnknown(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Unknown method") || strings.Contains(s, "Unknown tool") || strings.Contains(s, "unknown tool")
}

// errText renders an error field whether the daemon sent a plain string or
// the standard {code,message} object.
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
// array, structuredContent, isError) for CallDetail to decode.
//
// The serve daemon reads a tool call as {"method":"call","name":...,
// "args":{...}} with a top-level "args" field. A request using the MCP
// spelling "arguments" is accepted but arrives with no arguments at all: the
// tool then fails its own required-field checks ("Missing required integer
// field pid"), which is why the field name is pinned here.
func (rt *runtime) call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.closed.Load() {
		return nil, ErrNoDriver
	}
	id := rt.nextID.Add(1)
	body, err := requestEnvelope("call", tool, args, id)
	if err != nil {
		return nil, err
	}
	if err := rt.writeLine(body); err != nil {
		return nil, err
	}
	return rt.readResponse(ctx, id)
}

// requestEnvelope builds one request body with a fixed field order:
// jsonrpc, id, method, name, args.
func requestEnvelope(method, tool string, args map[string]any, id int64) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `{"jsonrpc":"2.0","id":%d,"method":%s`, id, quote(method))
	if tool != "" {
		buf.WriteString(`,"name":`)
		buf.WriteString(quote(tool))
	}
	if len(args) > 0 {
		encoded, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		buf.WriteString(`,"args":`)
		buf.Write(encoded)
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// quote renders a JSON string.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// readResponse reads frames until the answer to id. A notification (a
// method and no id, result, or error) is not that answer: returning it
// would make CallDetail treat a progress frame as the tool result. An
// id-less frame that carries a result or an error is the serve daemon's
// envelope, which has no id field.
func (rt *runtime) readResponse(ctx context.Context, id int64) (json.RawMessage, error) {
	for {
		line, err := rt.readLine(ctx)
		if err != nil {
			return nil, fmt.Errorf("cua: read: %w", err)
		}
		var frame struct {
			ID     json.Number     `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			return nil, fmt.Errorf("cua: unreadable frame: %w", err)
		}
		hasError := len(frame.Error) > 0 && string(frame.Error) != "null"
		if frame.Method != "" && frame.ID == "" && !hasError && len(frame.Result) == 0 {
			continue
		}
		if frame.ID != "" && frame.ID.String() != fmt.Sprint(id) {
			// The stream is no longer aligned with this request. Retire it
			// so the next call cannot treat the leftover frame as its own.
			rt.close()
			return nil, fmt.Errorf("cua: response id %s does not answer request %d", frame.ID, id)
		}
		return json.RawMessage(append([]byte(nil), line...)), nil
	}
}

// callEnvelope builds one tool-call request body with a deterministic field
// order: jsonrpc, id, method, name, arguments.
func callEnvelope(id int64, tool string, args map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `{"jsonrpc":"2.0","id":%d,"method":"call","name":`, id)
	name, err := json.Marshal(tool)
	if err != nil {
		return nil, err
	}
	buf.Write(name)
	if len(args) > 0 {
		encoded, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		buf.WriteString(`,"arguments":`)
		buf.Write(encoded)
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// liveness checks that the daemon answers at all. The serve daemon does not
// serve tools/list; its "Unknown method" envelope is an answer from a live
// daemon. A read or write failure is not: treating it as success is how a
// dead pipe used to pass Open.
func (rt *runtime) liveness(ctx context.Context) error {
	raw, err := rt.probe(ctx)
	if err != nil {
		return err
	}
	if !json.Valid(raw) {
		return fmt.Errorf("cua: driver liveness answer is not a protocol frame")
	}
	return nil
}

// probe sends the liveness request and reads one frame: the daemon answers
// an unserved method with an out-of-envelope error that carries no id, so
// awaiting a matching id would wait for a response that never comes.
func (rt *runtime) probe(ctx context.Context) (json.RawMessage, error) {
	rt.mu.Lock()
	if rt.closed.Load() {
		rt.mu.Unlock()
		return nil, ErrNoDriver
	}
	id := rt.nextID.Add(1)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/list"})
	if err != nil {
		rt.mu.Unlock()
		return nil, err
	}
	if err := rt.writeLine(body); err != nil {
		rt.mu.Unlock()
		return nil, err
	}
	// The read happens under the same lock: a pipe is one stream, and a
	// concurrent caller's response must not be consumed here.
	line, err := rt.readLine(ctx)
	rt.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("cua: read: %w", err)
	}
	return json.RawMessage(line), nil
}

func (rt *runtime) writeLine(b []byte) error { return rt.write(append(b, '\n')) }

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

// close releases the connection. It does not take mu: the in-flight read
// holds that lock, and closing the handle is what unblocks that read. The
// OS handle is released on a short timer so CloseHandle does not wait out
// a ReadFile still inside the kernel.
func (rt *runtime) close() {
	if !rt.closed.CompareAndSwap(false, true) {
		return
	}
	if rt.pipe != nil {
		rt.pipe.stop()
		p := rt.pipe
		time.AfterFunc(200*time.Millisecond, p.closeHandle)
	}
	if rt.conn != nil {
		_ = rt.conn.Close()
	}
}
