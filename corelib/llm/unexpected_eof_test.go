package llm

import (
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestIsUnexpectedStreamEOF(t *testing.T) {
	wrapped := fmt.Errorf("[https://codegen.example/api/v1/chat/completions] SSE stream read: %w", io.ErrUnexpectedEOF)
	if !IsUnexpectedStreamEOF(wrapped) {
		t.Fatal("wrapped unexpected EOF should be recognized")
	}
	if IsUnexpectedStreamEOF(errors.New("SSE stream read: unexpected EOF")) {
		t.Fatal("the same words without io.ErrUnexpectedEOF are not a truncated frame")
	}
	if IsUnexpectedStreamEOF(io.EOF) {
		t.Fatal("clean EOF is a finished body")
	}
	if IsUnexpectedStreamEOF(errors.New("unexpected end of JSON input")) {
		t.Fatal("a JSON parse error is not a truncated stream frame")
	}
	if IsUnexpectedStreamEOF(nil) {
		t.Fatal("nil is not an unexpected EOF")
	}
}
