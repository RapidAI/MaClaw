package cua

import (
	"context"
	"encoding/json"
	"fmt"
)

// # Wire dialect of the serve daemon (probed against cua-driver 0.52.9)
//
// Unlike the MCP transport an embedded host connects to, `cua-driver serve`
// answers tool calls through the method "call" with the tool name in a
// top-level "name" field and the arguments in a top-level "arguments"
// object, both beside "method":
//
//	-> {"jsonrpc":"2.0","id":1,"method":"call","name":"list_windows","arguments":{}}
//	<- {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"..."}],
//	                                   "structuredContent":{...},"isError":false}}
//
// A failure is a JSON-RPC error {"code":E,"message":"..."} or isError:true
// with the detail in content[0].text. tools/list is not served; its
// "Unknown method" envelope still proves the daemon is alive. Every tool
// also rejects an unknown argument field with an error that names the fields
// it expects, so schema drift is reported, never guessed.

// ErrDriver is a tool call the driver refused or failed. Detail carries the
// driver's own words, which hosts render to a model or a person.
type ErrDriver struct {
	Tool   string
	Detail string
	Cause  error
}

func (e ErrDriver) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("cua %s: %s (%v)", e.Tool, e.Detail, e.Cause)
	}
	return fmt.Sprintf("cua %s: %s", e.Tool, e.Detail)
}

func (e ErrDriver) Unwrap() error { return e.Cause }

// CallDetail runs one tool and returns the structured result together with
// the human-readable text the driver produced alongside it. Tools answer
// with structuredContent when they have one; the text carries the driver's
// notes (truncation, invalidated snapshots, refusals) and is the only
// answer for tools that have no structure.
func (d *Driver) CallDetail(ctx context.Context, tool string, args map[string]any) (structured map[string]any, text string, err error) {
	raw, err := d.rt.call(ctx, tool, args)
	if err != nil {
		return nil, "", err
	}
	content, structured, isError, parseErr := decodeToolResult(raw)
	if parseErr != nil {
		return nil, "", ErrDriver{Tool: tool, Detail: parseErr.Error()}
	}
	if len(content) > 0 {
		text = content[0].Text
	}
	if isError {
		if text == "" {
			text = "the driver reported an error"
		}
		return structured, text, ErrDriver{Tool: tool, Detail: text}
	}
	return structured, text, nil
}

// contentBlock is one MCP content entry.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// decodeToolResult reads a tool answer in either shape the daemon uses:
// the MCP wrapper {"content":[...],"structuredContent":{...},"isError":...}
// at the top level, or the same wrapper inside {"ok":true,"result":{...}}.
// Answers without a wrapper are the tool value itself.
func decodeToolResult(raw json.RawMessage) (content []contentBlock, structured map[string]any, isError bool, err error) {
	var direct struct {
		Content           []contentBlock `json:"content"`
		StructuredContent map[string]any `json:"structuredContent"`
		IsError           bool           `json:"isError"`
	}
	if err := json.Unmarshal(raw, &direct); err == nil {
		if len(direct.Content) > 0 || direct.StructuredContent != nil {
			return direct.Content, direct.StructuredContent, direct.IsError, nil
		}
	}
	var envelope struct {
		OK     *bool           `json:"ok"`
		Error  json.RawMessage `json:"error"`
		Result struct {
			Content           []contentBlock `json:"content"`
			StructuredContent map[string]any `json:"structuredContent"`
			IsError           bool           `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil {
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			return nil, nil, false, fmt.Errorf("%s", errText(envelope.Error))
		}
		if envelope.OK != nil && !*envelope.OK {
			return nil, nil, false, fmt.Errorf("the driver reported an error")
		}
		if len(envelope.Result.Content) > 0 || envelope.Result.StructuredContent != nil {
			return envelope.Result.Content, envelope.Result.StructuredContent, envelope.Result.IsError, nil
		}
	}
	var anyValue map[string]any
	if json.Unmarshal(raw, &anyValue) == nil && len(anyValue) > 0 {
		return nil, anyValue, false, nil
	}
	return nil, nil, false, fmt.Errorf("unreadable result")
}
