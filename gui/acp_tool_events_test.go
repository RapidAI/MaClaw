package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAcpToolEventToUpdateStart(t *testing.T) {
	u := acpToolEventToUpdate(ACPToolEvent{
		Phase:      "start",
		ToolCallID: "tc_1",
		Name:       "write_file",
		ArgsJSON:   `{"path":"hello.go","content":"package main"}`,
	})
	if u["sessionUpdate"] != "tool_call" {
		t.Fatalf("sessionUpdate=%v", u["sessionUpdate"])
	}
	if u["status"] != "in_progress" {
		t.Fatalf("status=%v", u["status"])
	}
	if u["kind"] != "edit" {
		t.Fatalf("kind=%v", u["kind"])
	}
	if u["toolCallId"] != "tc_1" {
		t.Fatalf("id=%v", u["toolCallId"])
	}
	locs, _ := u["locations"].([]map[string]any)
	if len(locs) == 0 {
		// may be []map after construction
		raw, _ := json.Marshal(u["locations"])
		if !strings.Contains(string(raw), "hello.go") {
			t.Fatalf("locations=%s", raw)
		}
	}
	title, _ := u["title"].(string)
	if !strings.Contains(title, "write_file") {
		t.Fatalf("title=%q", title)
	}
}

func TestAcpToolEventToUpdateEnd(t *testing.T) {
	u := acpToolEventToUpdate(ACPToolEvent{
		Phase:      "end",
		ToolCallID: "tc_1",
		Name:       "bash",
		OK:         true,
		Result:     "ok",
	})
	if u["sessionUpdate"] != "tool_call_update" {
		t.Fatalf("sessionUpdate=%v", u["sessionUpdate"])
	}
	if u["status"] != "completed" {
		t.Fatalf("status=%v", u["status"])
	}
	if u["kind"] != "execute" {
		t.Fatalf("kind=%v", u["kind"])
	}
}

func TestAcpPathsFromToolArgs(t *testing.T) {
	paths := acpPathsFromToolArgs("write_file", `{"path":"src/a.go"}`)
	if len(paths) != 1 || paths[0] != "src/a.go" {
		t.Fatalf("%v", paths)
	}
}

func TestAcpToolSinkRegistry(t *testing.T) {
	var got []string
	clear := globalACPToolSinks.set("acp-test-1", func(ev ACPToolEvent) {
		got = append(got, ev.Phase+":"+ev.Name)
	})
	emitACPToolEventForRequest("acp-test-1", ACPToolEvent{Phase: "start", Name: "read_file"})
	emitACPToolEventForRequest("other", ACPToolEvent{Phase: "start", Name: "nope"})
	clear()
	emitACPToolEventForRequest("acp-test-1", ACPToolEvent{Phase: "end", Name: "read_file"})
	if len(got) != 1 || got[0] != "start:read_file" {
		t.Fatalf("got=%v", got)
	}
}
