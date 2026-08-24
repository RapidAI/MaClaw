package agent

import "testing"

// Every unsuccessful read carries a stable envelope naming its class, and the
// system prompt instructs the model to obey specific classes. Hosts used to
// re-derive that judgement themselves, and mostly forgot to, so a refusal was
// served as a document page.
func TestDocumentReadFailureNamesTheClassTheEnvelopeCarries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result string
		class  string
	}{
		{"encrypted", "读取失败（error_class=encrypted）\n# path: a.docx\n", "encrypted"},
		{"malformed", "读取失败（error_class=malformed）\n# path: a.docx\n", "malformed"},
		{"timeout", "读取失败（error_class=timeout）\n# path: a.docx\n", "timeout"},
		{"unavailable", "读取失败（error_class=unavailable）\n# path: a.docx\n", "unavailable"},
		{"input too large", "读取失败（error_class=input_too_large）\n# path: a.docx\n", "input_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			class, failed := DocumentReadFailure(tc.result)
			if !failed {
				t.Fatalf("%q was reported as a document page", tc.result)
			}
			if class != tc.class {
				t.Fatalf("class = %q, want %q", class, tc.class)
			}
		})
	}
}

// The cases above are hand-written envelopes, which only prove the parser
// works on what the test author believed the format to be. These drive the
// real formatters instead, because the "class sits on the first line" rule the
// parser depends on is a convention those three functions establish and
// nothing enforces. A reworded or restructured envelope has to fail here, or
// it silently turns every failed read back into a document page.
func TestDocumentReadFailureRecognisesWhatTheFormattersActuallyEmit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		emit  func() string
		class string
	}{
		{"unavailable", func() string { return formatOfficeReadUnavailable("a.docx") }, "unavailable"},
		{"invalid path", func() string { return formatOfficeReadInvalidPath("a", "路径是目录，请指定具体文件路径") }, "invalid_path"},
		{"input too large", func() string {
			return formatOfficeReadFailure("a.docx", "docx", errOfficeReadInputTooLarge)
		}, "input_too_large"},
		{"output too large", func() string {
			return formatOfficeReadFailure("a.docx", "docx", errOfficeReadOutputTooLarge)
		}, "output_too_large"},
		{"encrypted", func() string {
			return formatOfficeReadFailure("a.docx", "docx", errOfficeReadEncryptedContainer)
		}, "encrypted"},
		{"source changed", func() string {
			return formatOfficeReadFailure("a.docx", "docx", errOfficeReadSourceChanged)
		}, "source_changed"},
		{"timed out", func() string {
			return formatOfficeReadFailure("a.docx", "docx", errOfficeReadTimedOut)
		}, "timeout"},
		{"extract error", func() string { return formatOfficeReadFailure("a.docx", "docx", nil) }, "extract_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envelope := tc.emit()
			class, failed := DocumentReadFailure(envelope)
			if !failed {
				t.Fatalf("an envelope this package emits was read as a document page: %q", envelope)
			}
			if class != tc.class {
				t.Fatalf("class = %q, want %q; envelope=%q", class, tc.class, envelope)
			}
		})
	}
}

// The opposite mistake would be worse than the one being fixed: turning real
// pages into failures would make every readable document unreadable.
func TestDocumentReadFailureLeavesRealPagesAlone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result string
	}{
		{"page", "# path: a.docx\n# format: docx\n# truncated: false\nbody text"},
		{"end of document", "已到文档末尾（offset=10, total_chars=5）。没有更多内容。\n# path: a.docx\n"},
		{"body mentioning the marker later", "# path: a.docx\nbody discussing error_class=encrypted in prose"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if class, failed := DocumentReadFailure(tc.result); failed {
				t.Fatalf("a readable page was reported as a %q failure", class)
			}
		})
	}
}

// An envelope whose class cannot be read still says the read failed, and
// believing the marker over the parse is what keeps an unrecognised envelope
// from being served as a document.
func TestDocumentReadFailureTrustsTheMarkerWhenTheClassIsUnreadable(t *testing.T) {
	class, failed := DocumentReadFailure("读取失败（error_class=）\n# path: a.docx\n")
	if !failed {
		t.Fatal("an envelope with an unreadable class was reported as a page")
	}
	if class != "unknown" {
		t.Fatalf("class = %q, want a placeholder that names nothing specific", class)
	}
}
