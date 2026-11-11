// Package localrelay lets a cloud bot reach the computer where the person's
// MaClaw GUI runs: list and read files in folders the person allowed, write
// files there without overwriting anything, and open TCP connections to hosts
// on that computer's network (an intranet GPU server's ssh, a lab web UI).
//
// The GUI keeps one outbound WebSocket to Hub (no inbound port on the
// person's computer). Hub multiplexes that link with yamux and opens one
// stream per request. Each stream starts with a JSON request line from Hub and
// a JSON response line from the GUI; read, write and dial streams then carry
// raw bytes. The GUI enforces the folder and network policy and asks the
// person before anything new, so Hub and MaClawSrv never decide what may be
// touched on that computer.
package localrelay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Stream operations, sent by Hub as Request.Op.
const (
	// OpPing returns the GUI's policy summary (folders, switches, cap).
	OpPing = "ping"
	// OpList lists one allowed folder, or the allowed roots when Path is empty.
	OpList = "list"
	// OpCheckRead approves and stats one local file before a transfer.
	OpCheckRead = "check_read"
	// OpRead streams one local file (Response.Size bytes follow).
	OpRead = "read"
	// OpCheckWrite approves a local destination folder and name.
	OpCheckWrite = "check_write"
	// OpWrite receives Request.Size bytes and saves them under a new name.
	// The GUI answers twice: ready before the bytes, then the saved path.
	OpWrite = "write"
	// OpCheckDial approves a network target without connecting.
	OpCheckDial = "check_dial"
	// OpDial connects to Host:Port; the stream then carries the connection.
	OpDial = "dial"
)

// Error codes in Response.Code. The bot tool turns them into plain sentences.
const (
	CodeOffline  = "offline"
	CodeDenied   = "denied"
	CodeTimeout  = "timeout"
	CodeNotFound = "not_found"
	CodeTooLarge = "too_large"
	CodeInvalid  = "invalid"
	CodeDisabled = "disabled"
	CodeFailed   = "failed"
	CodeExpired  = "expired"
)

// MaxLine bounds one JSON header line on a stream.
const MaxLine = 1 << 20

// MaxListEntries bounds one folder listing.
const MaxListEntries = 500

// Request is the first line Hub writes on a stream.
type Request struct {
	ID      string `json:"id,omitempty"`
	Op      string `json:"op"`
	BotID   string `json:"bot_id,omitempty"`
	BotName string `json:"bot_name,omitempty"`
	RunID   string `json:"run_id,omitempty"`
	// Path is the local file (read, check_read), folder (list, write,
	// check_write; empty means the bot's own MaClaw folder).
	Path string `json:"path,omitempty"`
	// Name is the file name for write and check_write.
	Name string `json:"name,omitempty"`
	// Size is how many bytes follow a write request.
	Size int64  `json:"size,omitempty"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	// Remote is the cloud-side path of a transfer, shown to the person.
	Remote string `json:"remote,omitempty"`
}

// Entry is one row of a folder listing.
type Entry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir,omitempty"`
	Size    int64  `json:"size,omitempty"`
	ModTime string `json:"mtime,omitempty"`
	Write   bool   `json:"write,omitempty"`
}

// Summary is the GUI's policy as a bot may see it.
type Summary struct {
	FilesEnabled   bool    `json:"files_enabled"`
	NetworkEnabled bool    `json:"network_enabled"`
	Roots          []Entry `json:"roots,omitempty"`
	MaxTransferMB  int     `json:"max_transfer_mb,omitempty"`
	BotFolder      string  `json:"bot_folder,omitempty"`
	OS             string  `json:"os,omitempty"`
	// TunnelIdleMinutes closes an unused tunnel after this long.
	TunnelIdleMinutes int `json:"tunnel_idle_minutes,omitempty"`
}

// Response is the GUI's answer line.
type Response struct {
	OK        bool     `json:"ok"`
	Code      string   `json:"code,omitempty"`
	Error     string   `json:"error,omitempty"`
	Path      string   `json:"path,omitempty"`
	Name      string   `json:"name,omitempty"`
	Size      int64    `json:"size,omitempty"`
	Addr      string   `json:"addr,omitempty"`
	Entries   []Entry  `json:"entries,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Summary   *Summary `json:"summary,omitempty"`
}

// Err is a Response failure carried as a Go error.
type Err struct {
	Code    string
	Message string
}

func (e *Err) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// Fail builds a failed Response.
func Fail(code, format string, args ...any) *Response {
	return &Response{Code: code, Error: fmt.Sprintf(format, args...)}
}

// AsErr returns the failure of a Response as an error, nil when OK.
func (r *Response) AsErr() error {
	if r == nil {
		return &Err{Code: CodeFailed, Message: "no answer from the computer"}
	}
	if r.OK {
		return nil
	}
	code := r.Code
	if code == "" {
		code = CodeFailed
	}
	return &Err{Code: code, Message: r.Error}
}

// CodeOf extracts the code of an *Err, or CodeFailed.
func CodeOf(err error) string {
	var e *Err
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeFailed
}

// WriteLine writes v as one JSON line.
func WriteLine(w io.Writer, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(raw)+1 > MaxLine {
		return fmt.Errorf("localrelay: header line is too long")
	}
	raw = append(raw, '\n')
	_, err = w.Write(raw)
	return err
}

// ReadLine reads exactly one JSON line into v. It reads one byte at a time
// so the raw bytes after the line stay in r for the caller.
func ReadLine(r io.Reader, v any) error {
	var buf bytes.Buffer
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				break
			}
			if buf.Len() >= MaxLine {
				return fmt.Errorf("localrelay: header line is too long")
			}
			buf.WriteByte(one[0])
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) && buf.Len() == 0 {
				return io.ErrUnexpectedEOF
			}
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}
	return json.Unmarshal(buf.Bytes(), v)
}
