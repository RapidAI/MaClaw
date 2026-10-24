package browser

import (
	"encoding/json"
	"testing"
	"time"
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

func TestFileChooserBlockSwallowsAndCounts(t *testing.T) {
	s := &BrowserAgentSession{}
	st := s.chooserState()
	st.mu.Lock()
	st.block, st.handled = true, make(chan error, 1)
	st.mu.Unlock()
	s.onFileChooserOpened(json.RawMessage(`{"backendNodeId":7,"mode":"selectSingle"}`))
	ok, err := s.WaitFileChooser(time.Second)
	if !ok || err != ErrFileChooserBlocked {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if n := s.FileChooserBlocked(); n != 1 {
		t.Fatalf("blocked=%d", n)
	}
	if n := s.FileChooserBlocked(); n != 0 {
		t.Fatalf("not cleared: %d", n)
	}
	s.DisarmFileChooser()
	s.onFileChooserOpened(json.RawMessage(`{"backendNodeId":7}`))
	if s.FileChooserBlocked() != 0 {
		t.Fatal("counted while disarmed")
	}
}
