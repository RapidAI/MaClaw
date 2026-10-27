package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSpecMatchesConstants(t *testing.T) {
	raw, err := os.ReadFile("spec.json")
	if err != nil {
		t.Fatalf("read spec.json: %v", err)
	}
	var spec struct {
		Proto         int `json:"proto"`
		MaxLineBytes  int `json:"max_line_bytes"`
		MaxCurrent    int `json:"max_current_tasks"`
		MaxTitleBytes int `json:"max_title_bytes"`
		HeartbeatMS   int `json:"heartbeat_ms"`
		OfflineMS     int `json:"offline_ms"`
		StaleMS       int `json:"stale_ms"`
		Display       struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"display"`
		Types struct {
			HostToDevice []string `json:"host_to_device"`
			DeviceToHost []string `json:"device_to_host"`
		} `json:"types"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec.json: %v", err)
	}
	if spec.Proto != Version {
		t.Errorf("spec proto=%d, Version=%d", spec.Proto, Version)
	}
	if spec.MaxLineBytes != MaxLineBytes {
		t.Errorf("spec max_line_bytes=%d, MaxLineBytes=%d", spec.MaxLineBytes, MaxLineBytes)
	}
	if spec.MaxCurrent != MaxCurrent {
		t.Errorf("spec max_current_tasks=%d, MaxCurrent=%d", spec.MaxCurrent, MaxCurrent)
	}
	if spec.MaxTitleBytes != MaxTitleBytes {
		t.Errorf("spec max_title_bytes=%d, MaxTitleBytes=%d", spec.MaxTitleBytes, MaxTitleBytes)
	}
	if spec.HeartbeatMS != HeartbeatInterval*1000 {
		t.Errorf("spec heartbeat_ms=%d, HeartbeatInterval=%ds", spec.HeartbeatMS, HeartbeatInterval)
	}
	if spec.OfflineMS != OfflineAfter*1000 {
		t.Errorf("spec offline_ms=%d, OfflineAfter=%ds", spec.OfflineMS, OfflineAfter)
	}
	if spec.StaleMS != StaleAfter*1000 {
		t.Errorf("spec stale_ms=%d, StaleAfter=%ds", spec.StaleMS, StaleAfter)
	}
	if spec.Display.Width != 360 || spec.Display.Height != 360 {
		t.Errorf("spec display=%dx%d, want 360x360", spec.Display.Width, spec.Display.Height)
	}
	known := map[MessageType]bool{
		TypeHello: true, TypeState: true, TypeAmbient: true, TypePet: true,
		TypeHeartbeat: true, TypeAssetChunk: true, TypeClock: true, TypeCelebrate: true, TypeSound: true,
		TypeStateAck: true, TypeLog: true,
	}
	listed := map[MessageType]bool{}
	for _, name := range append(append([]string{}, spec.Types.HostToDevice...), spec.Types.DeviceToHost...) {
		if !known[MessageType(name)] {
			t.Errorf("spec lists unknown type %q", name)
		}
		listed[MessageType(name)] = true
	}
	for missing := range known {
		if !listed[missing] {
			t.Errorf("spec does not list known type %q", missing)
		}
	}
}

func TestCountsValidate(t *testing.T) {
	ok := Counts{Total: 7, InProgress: 2, Pending: 1, Paused: 0, Done: 4, Failed: 0}
	if err := ok.Validate(); err != nil {
		t.Fatalf("expected valid counts: %v", err)
	}
	bad := ok
	bad.Total = 8
	if err := bad.Validate(); err == nil {
		t.Fatal("expected invariant failure")
	}
	neg := ok
	neg.Done = -1
	neg.Total = 2
	if err := neg.Validate(); err == nil {
		t.Fatal("expected negative bucket failure")
	}
}

func TestTruncateTitleKeepsRunes(t *testing.T) {
	// 30 CJK runes = 90 bytes; cap at 64 bytes must land on a rune boundary.
	title := strings.Repeat("会", 30)
	got := TruncateTitle(title, MaxTitleBytes)
	if len(got) > MaxTitleBytes {
		t.Fatalf("truncated to %d bytes", len(got))
	}
	if len(got)%3 != 0 {
		t.Fatalf("cut inside a rune: %d bytes", len(got))
	}
	if !strings.HasPrefix(title, got) {
		t.Fatal("truncation did not preserve prefix")
	}
}

func TestSanitizeClampsAndCaps(t *testing.T) {
	m := &Message{Type: TypeState, Current: make([]TaskItem, 0, 9)}
	for i := 0; i < 9; i++ {
		m.Current = append(m.Current, TaskItem{Title: strings.Repeat("任", 40), Progress: 5})
	}
	m.Sanitize()
	if len(m.Current) != MaxCurrent {
		t.Fatalf("current=%d, want %d", len(m.Current), MaxCurrent)
	}
	for _, item := range m.Current {
		if len(item.Title) > MaxTitleBytes {
			t.Fatalf("title %d bytes", len(item.Title))
		}
		if item.Progress != 1 {
			t.Fatalf("progress=%v, want 1", item.Progress)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	m := &Message{
		Type:  TypeState,
		Proto: Version,
		Ts:    1769900000,
		Seq:   42,
		Counts: &Counts{
			Total: 7, InProgress: 2, Pending: 1, Done: 4,
		},
		Current: []TaskItem{
			{ID: "a", Title: "整理会议纪要并导出 Word", Progress: 0.62},
			{ID: "b", Title: "文档导入：季度报表.xlsx", Progress: -1},
		},
	}
	m.Sanitize()
	if err := m.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	line, err := Encode(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if line[len(line)-1] != '\n' {
		t.Fatal("frame missing newline")
	}
	got, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != TypeState || got.Seq != m.Seq || got.Counts == nil || got.Counts.Total != 7 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if len(got.Current) != 2 || got.Current[0].Title != m.Current[0].Title {
		t.Fatalf("current mismatch: %+v", got.Current)
	}
}

func TestEncodeRejectsOversize(t *testing.T) {
	m := &Message{Type: TypeLog, Msg: strings.Repeat("x", MaxLineBytes)}
	if _, err := Encode(m); err == nil {
		t.Fatal("expected oversize error")
	}
}

func TestDecodeRejectsNonJSON(t *testing.T) {
	if _, err := DecodeLine([]byte("I (718) esp_image: segment 5\n")); !errors.Is(err, ErrNotJSON) {
		t.Fatalf("err=%v, want ErrNotJSON", err)
	}
	if _, err := DecodeLine([]byte("  {\"type\":\"log\"}\n")); !errors.Is(err, ErrNotJSON) {
		t.Fatalf("leading space should not be accepted: %v", err)
	}
}

func TestLineReaderSkipsNoiseAndOversize(t *testing.T) {
	big := strings.Repeat("y", MaxLineBytes+50)
	stream := "I (1) boot: hello\n" +
		`{"type":"heartbeat","ts":1}` + "\n" +
		big + "\n" +
		`{"type":"state_ack","applied":true,"page":"tasks"}` + "\n"
	lr := NewLineReader(strings.NewReader(stream), MaxLineBytes)

	line, err := lr.Next()
	if err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if !bytes.Contains(line, []byte("heartbeat")) {
		t.Fatalf("unexpected first frame %s", line)
	}
	if _, err := lr.Next(); !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("oversize line: err=%v, want ErrLineTooLong", err)
	}
	line, err = lr.Next()
	if err != nil {
		t.Fatalf("after oversize: %v", err)
	}
	if !bytes.Contains(line, []byte("state_ack")) {
		t.Fatalf("expected stream to resync, got %s", line)
	}
	if lr.NonJSON != 1 {
		t.Fatalf("NonJSON=%d, want 1", lr.NonJSON)
	}
	if lr.Overflow != 1 {
		t.Fatalf("Overflow=%d, want 1", lr.Overflow)
	}
	if _, err := lr.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestSeqGate(t *testing.T) {
	var g SeqGate
	for _, tc := range []struct {
		seq  uint64
		want bool
	}{{5, true}, {5, false}, {4, false}, {6, true}, {7, true}} {
		if got := g.Accept(tc.seq); got != tc.want {
			t.Fatalf("Accept(%d)=%v, want %v", tc.seq, got, tc.want)
		}
	}
	g.Reset()
	if !g.Accept(0) {
		t.Fatal("after reset seq 0 must be accepted")
	}
}

func TestValidateByType(t *testing.T) {
	cases := []struct {
		name string
		msg  *Message
		ok   bool
	}{
		{"state ok", &Message{Type: TypeState, Counts: &Counts{Total: 1, InProgress: 1}}, true},
		{"state no counts", &Message{Type: TypeState}, false},
		{"ambient ok", &Message{Type: TypeAmbient, Weather: &Weather{Summary: "晴"}}, true},
		{"ambient no weather", &Message{Type: TypeAmbient}, false},
		{"clock ok", &Message{Type: TypeClock, Clock: &Clock{Date: "10月27日", Weekday: "周一"}}, true},
		{"hello ok", &Message{Type: TypeHello, Proto: Version}, true},
		{"hello no proto", &Message{Type: TypeHello}, false},
		{"unknown", &Message{Type: "nope"}, false},
	}
	for _, tc := range cases {
		err := tc.msg.Validate()
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

// TestReplayFixture locks the parser against recorded traffic.
func TestReplayFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/replay.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var want []bool
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i, line := range lines {
		var entry struct {
			Valid bool   `json:"valid"`
			Line  string `json:"line"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("fixture line %d: %v", i, err)
		}
		want = append(want, entry.Valid)
		msg, err := DecodeLine([]byte(entry.Line))
		if entry.Valid {
			if err != nil {
				t.Errorf("line %d should parse: %v", i, err)
				continue
			}
			if verr := msg.Validate(); verr != nil {
				t.Errorf("line %d should validate: %v", i, verr)
			}
			continue
		}
		if err == nil && msg.Validate() == nil {
			t.Errorf("line %d should be rejected", i)
		}
	}
	if len(want) == 0 {
		t.Fatal("empty fixture")
	}
}
