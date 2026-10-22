package llm

import (
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
)

type resetAfterReader struct {
	r io.Reader
}

func (x *resetAfterReader) Read(p []byte) (int, error) {
	n, err := x.r.Read(p)
	if err == io.EOF {
		return n, syscall.ECONNRESET
	}
	return n, err
}

func TestParseSSEStreamResetMidToolCallReturnsNoResponse(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"desktop\",\"arguments\":\"{\\\"action\\\": \\\"script\\\", \\\"script\\\": \\\"click\"}}]}}]}\n\n"
	resp, err := parseSSEStream(&resetAfterReader{r: strings.NewReader(body)}, nil)
	if err == nil || !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("want reset error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("a cut with only reasoning and a half tool call must return no response so it is retried, got %+v", resp.Choices)
	}
}

func TestParseSSEStreamResetAfterTextKeepsPartial(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"
	resp, err := parseSSEStream(&resetAfterReader{r: strings.NewReader(body)}, nil)
	if err == nil || resp == nil || resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("partial text must be kept: resp=%v err=%v", resp, err)
	}
}
