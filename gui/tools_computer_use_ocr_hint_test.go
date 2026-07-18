package main

import (
	"strings"
	"testing"
)

func TestAppendOCRFailureHint_Noop(t *testing.T) {
	in := "mode=text_primary\nelements (1):\n"
	if got := appendOCRFailureHint(in, false, "", 0, 1920, 1080); got != in {
		t.Fatalf("unexpected rewrite: %q", got)
	}
}

func TestAppendOCRFailureHint_Primary(t *testing.T) {
	got := appendOCRFailureHint("elements (0):\n", true, "pipe closed", 0, 1920, 1080)
	if !strings.Contains(got, "ocr_failed=true") {
		t.Fatalf("missing flag: %s", got)
	}
	if !strings.Contains(got, "ocr_error=pipe closed") {
		t.Fatalf("missing error: %s", got)
	}
	if !strings.Contains(got, "screen_index=0") {
		t.Fatalf("missing recovery hint: %s", got)
	}
	if strings.Contains(got, "Current capture used screen_index=-1") {
		t.Fatalf("should not claim stitched capture for screen 0: %s", got)
	}
}

func TestAppendOCRFailureHint_Stitched(t *testing.T) {
	got := appendOCRFailureHint("e0\n", true, "line1\nline2", -1, 9840, 3840)
	if !strings.Contains(got, "Current capture used screen_index=-1 (9840x3840)") {
		t.Fatalf("missing stitched detail: %s", got)
	}
	// Error newlines collapsed so output stays one-line for ocr_error.
	if strings.Contains(got, "ocr_error=line1\nline2") {
		t.Fatalf("ocr_error should not embed raw newlines: %q", got)
	}
	if !strings.Contains(got, "ocr_error=line1 line2") {
		t.Fatalf("expected collapsed error: %s", got)
	}
}
