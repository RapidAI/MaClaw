package main

import (
	"strings"
	"testing"
)

// A computerless bot's undelivered attachment claim is corrected without a
// computer for the file to stay on.
func TestNoDesktopAttachClaimCorrectionIsNeutral(t *testing.T) {
	out := correctNoDesktopAttachClaim("结果见附件。", 0)
	if !strings.Contains(out, noDesktopAttachCorrection) || strings.Contains(out, "电脑") {
		t.Fatalf("correction = %q", out)
	}
	if out := correctNoDesktopAttachClaim("结果见附件。", 1); out != "结果见附件。" {
		t.Fatalf("attached reply changed: %q", out)
	}
	// An already-corrected reply is not corrected again.
	if again := correctNoDesktopAttachClaim(out, 0); again != out {
		t.Fatalf("second pass = %q", again)
	}
}
