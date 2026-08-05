package im

import "testing"

func TestFormatStatusIconMark(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"ok", "[OK]"},
		{"OK", "[OK]"},
		{"error", "[ERR]"},
		{"warning", "[!!]"},
		{"busy", "[..]"},
		{"info", ""},
		{"[i]", ""},
		{"[I]", ""},
		{"offline", "[--]"},
		{"[OK]", "[OK]"},
		{"", ""},
		{"unknown-token", ""},
	}
	for _, tc := range cases {
		if got := FormatStatusIconMark(tc.in); got != tc.want {
			t.Fatalf("FormatStatusIconMark(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToFallbackTextUsesASCIIStatusMarks(t *testing.T) {
	r := &GenericResponse{
		StatusIcon: "ok",
		Title:      "Done",
		Body:       "All good",
		Fields:     []ResponseField{{Label: "Device", Value: "Mac"}},
	}
	got := r.ToFallbackText()
	want := "[OK] Done\nAll good\nDevice: Mac"
	if got != want {
		t.Fatalf("ToFallbackText() = %q, want %q", got, want)
	}
	// Must not dump raw semantic token.
	if containsStr(got, "ok Done") {
		t.Fatalf("fallback still contains raw token: %q", got)
	}
}

func TestFormatCardFallbackPrefersExplicitFallback(t *testing.T) {
	card := OutgoingMessage{
		FallbackText: "custom",
		StatusIcon:   "error",
		Title:        "x",
	}
	if got := FormatCardFallback(card); got != "custom" {
		t.Fatalf("FormatCardFallback = %q, want custom", got)
	}
	// Explicit fallback still strips line-leading decorative pictographs.
	rocket := "\U0001F680"
	card2 := OutgoingMessage{FallbackText: rocket + " custom"}
	if got := FormatCardFallback(card2); got != "custom" {
		t.Fatalf("FormatCardFallback with pictograph = %q, want custom", got)
	}
}

func TestExplicitFallbackStripsOnlyLeadingInfoMark(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"[i] Notice", "Notice"},
		{"[I]\nNotice", "Notice"},
		{"[i]", ""},
		{"[I/O] status", "[I/O] status"},
		{"prefix [i] content", "prefix [i] content"},
	}
	for _, tc := range cases {
		r := &GenericResponse{FallbackText: tc.in}
		if got := r.ToFallbackText(); got != tc.want {
			t.Fatalf("ToFallbackText(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := FormatCardFallback(r.ToOutgoingMessage()); got != tc.want {
			t.Fatalf("FormatCardFallback(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatCardFallbackBuildsFromFields(t *testing.T) {
	card := OutgoingMessage{
		StatusIcon: "warning",
		Title:      "Need confirm",
		Body:       "SSH password",
	}
	got := FormatCardFallback(card)
	if got != "[!!] Need confirm\nSSH password" {
		t.Fatalf("FormatCardFallback = %q", got)
	}
}

func TestToFallbackTextStripsLineLeadingPictographs(t *testing.T) {
	rocket := "\U0001F680"
	r := &GenericResponse{
		StatusIcon: "ok",
		Title:      rocket + " Done",
		Body:       rocket + " Deployed.\n### " + "\U0001F3AF" + " Next",
	}
	got := r.ToFallbackText()
	want := "[OK] Done\nDeployed.\n### Next"
	if got != want {
		t.Fatalf("ToFallbackText() = %q, want %q", got, want)
	}
	if containsStr(got, rocket) {
		t.Fatalf("fallback still contains leading pictograph: %q", got)
	}
}

func TestToOutgoingMessageCleansBodyForAllChannels(t *testing.T) {
	rocket := "\U0001F680"
	r := &GenericResponse{
		StatusIcon: "info",
		Title:      rocket + " Notice",
		Body:       rocket + " Body text",
		Fields:     []ResponseField{{Label: "K", Value: rocket + " V"}},
	}
	out := r.ToOutgoingMessage()
	if out.Title != "Notice" || out.Body != "Body text" || out.Fields[0].Value != "V" {
		t.Fatalf("ToOutgoingMessage cleaned fields = title=%q body=%q field=%q", out.Title, out.Body, out.Fields[0].Value)
	}
	if out.StatusIcon != "info" {
		t.Fatalf("StatusIcon should stay semantic, got %q", out.StatusIcon)
	}
	if out.FallbackText != "Notice\nBody text\nK: V" {
		t.Fatalf("FallbackText should not expose the info marker: %q", out.FallbackText)
	}
	// Fallback must match a direct ToFallbackText (single normalize path).
	if out.FallbackText != r.ToFallbackText() {
		t.Fatalf("ToOutgoingMessage FallbackText diverged from ToFallbackText:\n%q\nvs\n%q", out.FallbackText, r.ToFallbackText())
	}
}

func TestGenericResponseNeverRendersInternalFields(t *testing.T) {
	r := &GenericResponse{
		Body: "answer",
		Fields: []ResponseField{
			{Label: "Result", Value: "ok"},
			{Label: "Route model", Value: "private", Internal: true},
		},
	}
	if got := r.ToFallbackText(); got != "answer\nResult: ok" {
		t.Fatalf("ToFallbackText() = %q, want internal field removed", got)
	}
	out := r.ToOutgoingMessage()
	if len(out.Fields) != 1 || out.Fields[0].Label != "Result" || out.FallbackText != "answer\nResult: ok" {
		t.Fatalf("ToOutgoingMessage() exposed internal fields: %#v", out)
	}
}

func TestToOutgoingMessageNilSafe(t *testing.T) {
	var r *GenericResponse
	out := r.ToOutgoingMessage()
	if out.Title != "" || out.Body != "" || out.FallbackText != "" {
		t.Fatalf("nil receiver should yield empty message, got %+v", out)
	}
	if r.ToFallbackText() != "" {
		t.Fatalf("nil ToFallbackText should be empty")
	}
}
