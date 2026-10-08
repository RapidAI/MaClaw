package lobsterai

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// streamFilterHead caps how much of the stream the filter examines before it
// replaces the source with a synthesized error body. The upstream error frame
// always rides the head of the stream (measured well under 200 bytes), so the
// cap only guards against a pathological stream.
const streamFilterHead = 512 * 1024

// newStreamErrorFilter wraps an SSE body so a leading error frame becomes one
// OpenAI error chunk plus [DONE]. Any other stream passes through byte-exact:
// the frames the filter already consumed are replayed before raw source reads.
// 业务错误帧只出现在 HTTP 200 流的首部（实测），其后的内容在错误判定下被丢弃。
func newStreamErrorFilter(body io.ReadCloser) io.ReadCloser {
	return &streamErrorFilter{source: bufio.NewReaderSize(body, 16*1024), body: body}
}

type streamErrorFilter struct {
	source *bufio.Reader
	body   io.ReadCloser
	// head holds bytes consumed during classification that must replay to the
	// caller before raw source reads resume (passthrough case).
	head []byte
	// tail holds the synthesized error body (error case); nil for passthrough.
	tail    []byte
	decided bool
}

func (f *streamErrorFilter) Read(p []byte) (int, error) {
	if !f.decided {
		if err := f.decide(); err != nil {
			return 0, err
		}
	}
	// Error decision: deliver the synthesized body, then EOF. The real source
	// is closed with the remainder unread.
	if f.tail != nil {
		taken := copy(p, f.tail)
		f.tail = f.tail[taken:]
		if len(f.tail) == 0 {
			f.tail = nil
			f.closeSource()
		}
		return taken, nil
	}
	// Passthrough: replay consumed head bytes, then hand reads to the source.
	if len(f.head) > 0 {
		taken := copy(p, f.head)
		f.head = f.head[taken:]
		return taken, nil
	}
	return f.source.Read(p)
}

func (f *streamErrorFilter) Close() error {
	f.head = nil
	f.tail = nil
	return f.body.Close()
}

func (f *streamErrorFilter) closeSource() {
	_ = f.body.Close()
}

// decide classifies the leading frames. It keeps reading until one completed
// "data:" line is decidable (an error object flips to the synthesized error
// body; any other JSON frame confirms passthrough). SSE comment/event lines
// alone never trigger the decision — the real error frame may follow them —
// and EOF finishes the stream with a whole-head verdict.
func (f *streamErrorFilter) decide() error {
	f.decided = true
	buf := make([]byte, 16*1024)
	var head []byte
	// surveys the completed "data:" lines in head. 1 = error frame found,
	// 0 = normal frame found, -1 = not decidable yet.
	survey := func() int {
		lines := strings.Split(string(head), "\n")
		for i := 0; i < len(lines)-1; i++ { // the last fragment may still grow
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "data:"))
			if text == "" || text == "[DONE]" {
				continue
			}
			var probe map[string]any
			if json.Unmarshal([]byte(text), &probe) != nil {
				continue // SSE event/comment lines are not JSON frames
			}
			if frame := sseErrorFrame([]byte("data: " + text + "\n")); frame != "" {
				f.adoptError(frame)
				return 1
			}
			return 0
		}
		return -1
	}
	for len(head) < streamFilterHead {
		n, err := f.source.Read(buf)
		head = append(head, buf[:n]...)
		if verdict := survey(); verdict >= 0 {
			if verdict == 1 {
				return nil // adoptError already rewired the stream
			}
			f.head = append(f.head, head...)
			return nil
		}
		if err != nil {
			// EOF: the whole head is the last word (a lone error frame
			// without a trailing blank line still translates).
			if frame := sseErrorFrame(head); frame != "" {
				f.adoptError(frame)
				return nil
			}
			f.head = append(f.head, head...)
			return nil
		}
	}
	if frame := sseErrorFrame(head); frame != "" {
		f.adoptError(frame)
		return nil
	}
	f.head = append(f.head, head...)
	return nil
}

// adoptError swaps the stream for the synthesized OpenAI error body.
func (f *streamErrorFilter) adoptError(frame string) {
	f.head = nil
	payload, err := json.Marshal(map[string]any{"error": map[string]any{"message": frame, "type": "upstream_error"}})
	if err != nil {
		payload = []byte(`{"error":{"message":"upstream error","type":"upstream_error"}}`)
	}
	f.tail = []byte("data: " + string(payload) + "\n\ndata: [DONE]\n\n")
}
