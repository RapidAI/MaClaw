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
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent map[string]any `json:"structuredContent"`
		IsError           bool           `json:"isError"`
	}
	if json.Unmarshal(raw, &out) != nil {
		// A build that returns the tool value directly.
		var direct map[string]any
		if json.Unmarshal(raw, &direct) == nil {
			return direct, "", nil
		}
		return nil, "", ErrDriver{Tool: tool, Detail: "unreadable result"}
	}
	if len(out.Content) > 0 {
		text = out.Content[0].Text
	}
	if out.IsError {
		if text == "" {
			text = "the driver reported an error"
		}
		return out.StructuredContent, text, ErrDriver{Tool: tool, Detail: text}
	}
	return out.StructuredContent, text, nil
}
