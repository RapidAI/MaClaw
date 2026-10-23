package browser

import (
	"encoding/json"
	"testing"
)

func TestParseFileChooserOpened(t *testing.T) {
	n, m := parseFileChooserOpened(json.RawMessage(`{"frameId":"F","mode":"selectMultiple","backendNodeId":42}`))
	if n != 42 || m != "selectMultiple" {
		t.Fatalf("got %d %s", n, m)
	}
	if !isCriticalCDPEvent("Page.fileChooserOpened") {
		t.Fatal("chooser event may be dropped")
	}
	s := &BrowserAgentSession{}
	s.onFileChooserOpened(json.RawMessage(`{"backendNodeId":1}`)) // unarmed: no-op, no panic
	if ok, _ := s.WaitFileChooser(0); ok {
		t.Fatal("unarmed wait")
	}
}
