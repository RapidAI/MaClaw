// Package protocol defines the MaClaw Buddy host <-> device wire protocol.
//
// The device is an ESP32-S3 companion display reached over USB-Serial/JTAG.
// Frames are single-line UTF-8 JSON terminated by '\n'. The constants and
// limits in this file are mirrored by protocol/spec.json; protocol_test.go
// fails when the two drift apart.
package protocol

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	// Version is the wire protocol version exchanged in hello messages.
	Version = 2
	// MaxLineBytes is the maximum encoded frame size including the newline.
	MaxLineBytes = 1024
	// MaxCurrent is the maximum number of in-progress tasks in one state frame.
	MaxCurrent = 5
	// MaxTitleBytes is the maximum UTF-8 byte length of a task title.
	MaxTitleBytes = 64

	// HeartbeatInterval is how often the host sends heartbeat frames.
	HeartbeatInterval = 3
	// OfflineAfter is how long the device waits without any host frame before
	// it treats the host as offline.
	OfflineAfter = 10
	// StaleAfter is how old a state snapshot may be before the host marks it
	// stale for the device UI.
	StaleAfter = 60
)

// MessageType classifies a protocol frame.
type MessageType string

const (
	TypeHello      MessageType = "hello"
	TypeState      MessageType = "state"
	TypeAmbient    MessageType = "ambient"
	TypePet        MessageType = "pet"
	TypeHeartbeat  MessageType = "heartbeat"
	TypeAssetChunk MessageType = "asset_chunk"
	TypeClock      MessageType = "clock"
	TypeStateAck   MessageType = "state_ack"
	TypeLog        MessageType = "log"
)

var (
	// ErrNotJSON reports a line that does not start with a JSON object. The
	// device console is expected to be silent, but boot ROM chatter and a
	// misconfigured build must never break the host parser.
	ErrNotJSON = errors.New("buddy: line is not a JSON object")
	// ErrLineTooLong reports an over-long frame. It is recoverable: the
	// reader discards the remainder of the line.
	ErrLineTooLong = errors.New("buddy: line exceeds limit")
)

// Counts is the task census shown on the device. Every bucket is computed by
// the GUI so the device never re-derives status from its own vocabulary.
type Counts struct {
	Total      int `json:"total"`
	InProgress int `json:"in_progress"`
	Pending    int `json:"pending"`
	Paused     int `json:"paused"`
	Done       int `json:"done"`
	Failed     int `json:"failed"`
}

// Sum returns the total implied by the buckets.
func (c Counts) Sum() int {
	return c.InProgress + c.Pending + c.Paused + c.Done + c.Failed
}

// Validate enforces the protocol invariant total == sum of buckets.
func (c Counts) Validate() error {
	for name, v := range map[string]int{
		"total":       c.Total,
		"in_progress": c.InProgress,
		"pending":     c.Pending,
		"paused":      c.Paused,
		"done":        c.Done,
		"failed":      c.Failed,
	} {
		if v < 0 {
			return fmt.Errorf("buddy: counts.%s must be >= 0 (got %d)", name, v)
		}
	}
	if got := c.Sum(); got != c.Total {
		return fmt.Errorf("buddy: counts invariant broken: total=%d sum=%d", c.Total, got)
	}
	return nil
}

// TaskItem is one in-progress task row.
type TaskItem struct {
	ID       string  `json:"id,omitempty"`
	Title    string  `json:"title"`
	Progress float64 `json:"progress,omitempty"`
}

// Weather mirrors guiapp.deviceAmbientWeather exactly.
type Weather struct {
	Summary      string `json:"summary"`
	TemperatureC int    `json:"temperatureC"`
	Location     string `json:"location"`
}

// Display describes the device panel in the device hello frame.
type Display struct {
	Width  int  `json:"w"`
	Height int  `json:"h"`
	Round  bool `json:"round,omitempty"`
}

// Pet carries an optional emotion hint.
type Pet struct {
	State string `json:"state"`
}

// Message is the union of every protocol frame. Only the fields relevant to
// Type are populated; unknown fields from future peers are ignored by encoding/json.
type Message struct {
	Type    MessageType `json:"type"`
	Proto   int         `json:"proto,omitempty"`
	Ts      int64       `json:"ts,omitempty"`
	Seq     uint64      `json:"seq,omitempty"`
	Stale   bool        `json:"stale,omitempty"`
	Counts  *Counts     `json:"counts,omitempty"`
	Current []TaskItem  `json:"current,omitempty"`
	Weather *Weather    `json:"weather,omitempty"`
	Pet     *Pet        `json:"pet,omitempty"`
	Clock   *Clock      `json:"clock,omitempty"`

	// hello
	FullResend bool     `json:"full_resend,omitempty"`
	Host       string   `json:"host,omitempty"`
	Ver        string   `json:"ver,omitempty"`
	FW         string   `json:"fw,omitempty"`
	Board      string   `json:"board,omitempty"`
	Display    *Display `json:"disp,omitempty"`

	// state_ack
	Applied       bool   `json:"applied,omitempty"`
	Page          string `json:"page,omitempty"`
	MissingGlyphs int    `json:"missing_glyphs,omitempty"`

	// log
	Level string `json:"level,omitempty"`
	Msg   string `json:"msg,omitempty"`

	// device heartbeat
	UptimeS uint32 `json:"uptime_s,omitempty"`
	HeapMin uint32 `json:"heap_min,omitempty"`

	// device hello: touch controller present (best-effort probe)
	Touch bool `json:"touch,omitempty"`
}

// Clock carries a pre-formatted local date so the device needs no timezone or
// calendar logic. The host refreshes it on every tick.
type Clock struct {
	Date    string `json:"date"`
	Weekday string `json:"weekday"`
}

// Snapshot is the payload the GUI pushes into the in-process buddy service.
// It is separate from Message: it is an internal hand-off, not a wire frame.
type Snapshot struct {
	Counts    Counts     `json:"counts"`
	Current   []TaskItem `json:"current"`
	UpdatedAt int64      `json:"updated_at"`
}

// Sanitize applies wire limits in place: titles are truncated on rune
// boundaries, the current list is capped, and progress is clamped.
func (m *Message) Sanitize() {
	if m.Counts != nil && m.Counts.Total < 0 {
		m.Counts.Total = 0
	}
	if len(m.Current) > MaxCurrent {
		m.Current = m.Current[:MaxCurrent]
	}
	for i := range m.Current {
		m.Current[i].Title = TruncateTitle(m.Current[i].Title, MaxTitleBytes)
		if m.Current[i].Progress < -1 {
			m.Current[i].Progress = -1
		}
		if m.Current[i].Progress > 1 {
			m.Current[i].Progress = 1
		}
	}
	if m.Weather != nil {
		m.Weather.Summary = TruncateTitle(m.Weather.Summary, 32)
		m.Weather.Location = TruncateTitle(m.Weather.Location, 32)
	}
}

// TruncateTitle caps s at maxBytes without splitting a UTF-8 rune.
func TruncateTitle(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// Encode serialises one frame including the trailing newline and enforces the
// line limit.
func Encode(m *Message) ([]byte, error) {
	if m == nil {
		return nil, errors.New("buddy: nil message")
	}
	if m.Type == "" {
		return nil, errors.New("buddy: message type is required")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if len(b)+1 > MaxLineBytes {
		return nil, fmt.Errorf("buddy: encoded frame %d bytes exceeds %d", len(b)+1, MaxLineBytes)
	}
	return append(b, '\n'), nil
}

// DecodeLine parses one frame without its terminator.
func DecodeLine(line []byte) (*Message, error) {
	line = trimEOL(line)
	if len(line) > MaxLineBytes {
		return nil, ErrLineTooLong
	}
	if len(line) == 0 || line[0] != '{' {
		return nil, ErrNotJSON
	}
	var m Message
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, fmt.Errorf("buddy: decode frame: %w", err)
	}
	if m.Type == "" {
		return nil, errors.New("buddy: frame has no type")
	}
	return &m, nil
}

// LineReader reads newline-delimited frames with a hard size limit. Oversized
// lines are discarded after their newline so one bad producer cannot wedge the
// stream.
type LineReader struct {
	br       *bufio.Reader
	Max      int
	Overflow int
	NonJSON  int
}

// NewLineReader wraps r. max <= 0 falls back to MaxLineBytes.
func NewLineReader(r io.Reader, max int) *LineReader {
	if max <= 0 {
		max = MaxLineBytes
	}
	return &LineReader{br: bufio.NewReaderSize(r, max+2), Max: max}
}

// Next returns the next line without its terminator. It returns io.EOF when
// the stream ends cleanly.
func (lr *LineReader) Next() ([]byte, error) {
	for {
		line, err := lr.br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			if len(line) > lr.Max {
				if err := lr.discardLine(); err != nil && !errors.Is(err, io.EOF) {
					return nil, err
				}
				lr.Overflow++
				return nil, ErrLineTooLong
			}
			// The buffer is sized max+2, so a non-newline overflow means the
			// line is longer than the limit: drop the remainder.
			if err := lr.discardLine(); err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			lr.Overflow++
			return nil, ErrLineTooLong
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		line = trimEOL(line)
		if len(line) > lr.Max {
			lr.Overflow++
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue
		}
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue
		}
		if line[0] != '{' {
			lr.NonJSON++
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue
		}
		return line, nil
	}
}

func (lr *LineReader) discardLine() error {
	for {
		_, err := lr.br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return err
	}
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// SeqGate drops duplicate or out-of-order state frames. A device restarts with
// seq 0, so callers must gate reset to 0 explicitly.
type SeqGate struct {
	last uint64
	seen bool
}

// Accept reports whether seq should be applied.
func (g *SeqGate) Accept(seq uint64) bool {
	if !g.seen {
		g.seen = true
		g.last = seq
		return true
	}
	if seq <= g.last {
		return false
	}
	g.last = seq
	return true
}

// Reset clears the gate so the next frame is accepted regardless of seq.
func (g *SeqGate) Reset() {
	g.seen = false
	g.last = 0
}

// Validate checks the fields required by the message type.
func (m *Message) Validate() error {
	switch m.Type {
	case TypeState:
		if m.Counts == nil {
			return errors.New("buddy: state requires counts")
		}
		if err := m.Counts.Validate(); err != nil {
			return err
		}
		if len(m.Current) > MaxCurrent {
			return fmt.Errorf("buddy: state has %d current tasks, max %d", len(m.Current), MaxCurrent)
		}
		for i, t := range m.Current {
			if strings.TrimSpace(t.Title) == "" {
				return fmt.Errorf("buddy: state.current[%d] has empty title", i)
			}
			if len(t.Title) > MaxTitleBytes {
				return fmt.Errorf("buddy: state.current[%d] title %d bytes exceeds %d", i, len(t.Title), MaxTitleBytes)
			}
		}
	case TypeAmbient:
		if m.Weather == nil {
			return errors.New("buddy: ambient requires weather")
		}
		if strings.TrimSpace(m.Weather.Summary) == "" {
			return errors.New("buddy: ambient.weather.summary is required")
		}
	case TypeClock:
		if m.Clock == nil || strings.TrimSpace(m.Clock.Date) == "" {
			return errors.New("buddy: clock requires date")
		}
	case TypeHello:
		if m.Proto == 0 {
			return errors.New("buddy: hello requires proto")
		}
	case TypeHeartbeat, TypePet, TypeAssetChunk, TypeStateAck, TypeLog:
		// No mandatory payload fields.
	default:
		return fmt.Errorf("buddy: unknown message type %q", m.Type)
	}
	return nil
}
