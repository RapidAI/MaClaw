package tool

import (
	"path/filepath"
	"testing"
)

func TestTrustedInputPlanIDAndAttachmentSourceID(t *testing.T) {
	if got := TrustedInputPlanID(" turn-1 "); got != "input:turn-1" {
		t.Fatalf("TrustedInputPlanID=%q", got)
	}
	if got := TrustedAttachmentSourceID(2, "dir/report.pdf", "application/pdf", " media-9 "); got != "media-9" {
		t.Fatalf("channel handle must win: %q", got)
	}
	want := "attachment:2:" + filepath.Base("dir/report.pdf") + ":application/pdf"
	if got := TrustedAttachmentSourceID(2, "dir/report.pdf", "application/pdf", "  "); got != want {
		t.Fatalf("fallback sourceID=%q want=%q", got, want)
	}
}

func TestUniqueTrustedInputCountMissingAmbiguousAndExact(t *testing.T) {
	if err := UniqueTrustedInputCount(1); err != nil {
		t.Fatalf("exact one must pass: %v", err)
	}
	if err := UniqueTrustedInputCount(0); err == nil || err.Error() != TrustedInputMissing {
		t.Fatalf("missing=%v", err)
	}
	if err := UniqueTrustedInputCount(2); err == nil || err.Error() != TrustedInputAmbiguous {
		t.Fatalf("ambiguous=%v", err)
	}
	if !IsTrustedInputMissingOrAmbiguous(UniqueTrustedInputCount(0)) || !IsTrustedInputMissingOrAmbiguous(UniqueTrustedInputCount(3)) {
		t.Fatal("classifier must recognize both uniqueness failures")
	}
	if IsTrustedInputMissingOrAmbiguous(nil) {
		t.Fatal("nil is not a uniqueness failure")
	}
}
