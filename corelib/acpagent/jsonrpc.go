package acpagent

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 message shapes used by ACP.

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

func newRPCError(code int, message string) *RPCError {
	return &RPCError{Code: code, Message: message}
}

func encodeLine(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// ACP: message MUST NOT contain embedded newlines.
	for _, b := range data {
		if b == '\n' || b == '\r' {
			return nil, fmt.Errorf("acpagent: encoded message contains newline")
		}
	}
	return append(data, '\n'), nil
}

func decodeRequest(line []byte) (Request, error) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, err
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return Request{}, fmt.Errorf("unsupported jsonrpc version %q", req.JSONRPC)
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	return req, nil
}

// IncomingMessage is one NDJSON JSON-RPC frame (request, notification, or response).
type IncomingMessage struct {
	Request  *Request
	Response *Response
}

// DecodeIncoming classifies a line as request/notification or response.
func DecodeIncoming(line []byte) (IncomingMessage, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(line, &env); err != nil {
		return IncomingMessage{}, err
	}
	if methodRaw, ok := env["method"]; ok && len(methodRaw) > 0 && string(methodRaw) != "null" {
		req, err := decodeRequest(line)
		if err != nil {
			return IncomingMessage{}, err
		}
		return IncomingMessage{Request: &req}, nil
	}
	if idRaw, ok := env["id"]; ok && hasRequestID(idRaw) {
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			return IncomingMessage{}, err
		}
		if resp.JSONRPC == "" {
			resp.JSONRPC = "2.0"
		}
		// Prefer typed result as raw for callers.
		if r, ok := env["result"]; ok {
			resp.Result = r
		}
		if e, ok := env["error"]; ok && len(e) > 0 && string(e) != "null" {
			var re RPCError
			if json.Unmarshal(e, &re) == nil {
				resp.Error = &re
			}
		}
		return IncomingMessage{Response: &resp}, nil
	}
	return IncomingMessage{}, fmt.Errorf("acpagent: unrecognized json-rpc line")
}

func hasRequestID(id json.RawMessage) bool {
	if len(id) == 0 {
		return false
	}
	s := string(id)
	return s != "null"
}
