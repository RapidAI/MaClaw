package lobsterai

import (
	"io"
	"strings"
	"testing"
)

func readAllFilter(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	filter := newStreamErrorFilter(body)
	text, err := io.ReadAll(filter)
	if err != nil {
		t.Fatalf("read filtered stream: %v", err)
	}
	_ = filter.Close()
	return string(text)
}

func TestStreamErrorFilterTranslatesLeadingErrorFrame(t *testing.T) {
	upstream := "" +
		"data: {\"error\":{\"type\":\"proxy_error\",\"message\":\"quota gone\",\"code\":40201}}\n\n" +
		"data: {\"id\":\"1\",\"choices\":[]}\n\n" +
		"data: [DONE]\n\n"
	out := readAllFilter(t, io.NopCloser(strings.NewReader(upstream)))
	if !strings.Contains(out, `"error"`) || !strings.Contains(out, "40201") || !strings.Contains(out, "quota gone") {
		t.Fatalf("error chunk missing: %s", out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "data: [DONE]") {
		t.Fatalf("terminal [DONE] missing: %q", out)
	}
	// The regular frames after the error decision must not leak through.
	if strings.Contains(out, `"id":"1"`) {
		t.Fatalf("post-error frames leaked: %s", out)
	}
}

func TestStreamErrorFilterPassthroughIsByteExact(t *testing.T) {
	upstream := "" +
		"data: {\"id\":\"u1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":5}}\n" +
		"data: [DONE]\n\n"
	out := readAllFilter(t, io.NopCloser(strings.NewReader(upstream)))
	if out != upstream {
		t.Fatalf("passthrough not byte-exact:\n got %q\nwant %q", out, upstream)
	}
}

func TestStreamErrorFilterHandlesSplitFrames(t *testing.T) {
	// The error frame arrives byte-by-byte around chunk boundaries.
	source := strings.NewReader("data: {\"err" + "or\":{\"message\":\"model unknown\",\"code\":40300}}\n\ndata: [DONE]\n\n")
	slow := &slowReader{r: source, perRead: 3}
	out := readAllFilter(t, io.NopCloser(slow))
	if !strings.Contains(out, "40300") || !strings.Contains(out, "model unknown") {
		t.Fatalf("split error frame not translated: %s", out)
	}
}

func TestStreamErrorFiltersEOFOnlyError(t *testing.T) {
	upstream := "data: {\"error\":{\"message\":\"flip\",\"code\":1}}\n"
	out := readAllFilter(t, io.NopCloser(strings.NewReader(upstream)))
	if !strings.Contains(out, "flip") || !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("EOF error frame not translated: %s", out)
	}
}

func TestStreamErrorFilterCommentHeadDoesNotDecideEarly(t *testing.T) {
	// An SSE comment (or event line) at the head must NOT flip the filter to
	// passthrough before the real data frame arrives, or the error frame that
	// follows it would leak through un-translated.
	upstream := ": ping\n\ndata: {\"error\":{\"message\":\"late quota\",\"code\":40201}}\n\n"
	out := readAllFilter(t, io.NopCloser(strings.NewReader(upstream)))
	// The synthesized chunk wraps the upstream message in the standard error
	// envelope, so the assertion looks for the carried fields, not the frame.
	if !strings.Contains(out, "40201") || !strings.Contains(out, "late quota") {
		t.Fatalf("error frame after comment head not translated: %s", out)
	}
	if !strings.Contains(out, `"error":{`) {
		t.Fatalf("error envelope missing: %s", out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "data: [DONE]") {
		t.Fatalf("terminal [DONE] missing: %q", out)
	}
}

func TestStreamErrorFilterEmptyIsPassthroughEOF(t *testing.T) {
	out := readAllFilter(t, io.NopCloser(strings.NewReader("")))
	if out != "" {
		t.Fatalf("empty stream should stay empty: %q", out)
	}
}

// slowReader paces reads to force the filter over chunk boundaries.
type slowReader struct {
	r       io.Reader
	perRead int
}

func (s *slowReader) Read(p []byte) (int, error) {
	defer func() {
		if len(p) > s.perRead {
			p = p[:s.perRead]
		}
	}()
	buf := make([]byte, s.perRead)
	n, err := s.r.Read(buf)
	copy(p, buf[:n])
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}
