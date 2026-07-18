package acpagent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Conn is a bidirectional NDJSON JSON-RPC connection (stdio-oriented).
// Writes are serialized. Reads are sequential (one consumer).
type Conn struct {
	in  *bufio.Reader
	out io.Writer

	writeMu sync.Mutex
}

// NewConn wraps r/w for NDJSON ACP framing.
// Only valid ACP messages may be written to out; logs go elsewhere (stderr).
func NewConn(r io.Reader, w io.Writer) *Conn {
	return &Conn{
		in:  bufio.NewReaderSize(r, 1024*1024),
		out: w,
	}
}

// ReadRequest reads the next NDJSON line as a JSON-RPC request or notification.
// Prefer ReadMessage when the peer may also send responses to agent-initiated calls.
func (c *Conn) ReadRequest() (Request, error) {
	msg, err := c.ReadMessage()
	if err != nil {
		return Request{}, err
	}
	if msg.Request == nil {
		return Request{}, fmt.Errorf("acpagent: expected request, got response")
	}
	return *msg.Request, nil
}

// ReadMessage reads the next NDJSON line as request/notification or response.
func (c *Conn) ReadMessage() (IncomingMessage, error) {
	line, err := c.in.ReadBytes('\n')
	if err != nil {
		return IncomingMessage{}, err
	}
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	if len(line) == 0 {
		return IncomingMessage{}, fmt.Errorf("empty line")
	}
	return DecodeIncoming(line)
}

// WriteResponse writes a JSON-RPC response line.
func (c *Conn) WriteResponse(id jsonRaw, result any, rpcErr *RPCError) error {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
		Error:   rpcErr,
	}
	if rpcErr != nil {
		resp.Result = nil
	}
	return c.write(resp)
}

// WriteRequest writes a JSON-RPC request line (agent → client reverse calls).
func (c *Conn) WriteRequest(id jsonRaw, method string, params any) error {
	type wire struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  any             `json:"params,omitempty"`
	}
	return c.write(wire{
		JSONRPC: "2.0",
		ID:      json.RawMessage(id),
		Method:  method,
		Params:  params,
	})
}

// WriteNotification writes a JSON-RPC notification (no id).
func (c *Conn) WriteNotification(method string, params any) error {
	n := Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.write(n)
}

func (c *Conn) write(v any) error {
	line, err := encodeLine(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.out.Write(line)
	return err
}

// jsonRaw is an alias used by WriteResponse to avoid exporting encoding/json in signature noise.
type jsonRaw = []byte
