package browser

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBrowserRecorderRedactsTypedTextAndURLBeforePersistence(t *testing.T) {
	recorder := NewBrowserRecorder(func() (*Session, error) { return nil, errors.New("no session") }, nil)
	recorder.recording = true
	recorder.startTime = time.Now()

	recorder.RecordStep("type", "input[name=password]", "SECRET_PASSWORD", "https://example.com/login?token=SECRET_TOKEN#frag", [2]int{})

	if len(recorder.steps) != 1 {
		t.Fatalf("steps len = %d", len(recorder.steps))
	}
	step := recorder.steps[0]
	serialized := step.Text + "\n" + step.URL
	for _, leaked := range []string{"SECRET_PASSWORD", "SECRET_TOKEN", "token=", "#frag"} {
		if strings.Contains(serialized, leaked) {
			t.Fatalf("recorded step leaked %q: %#v", leaked, step)
		}
	}
	if !step.Redacted || step.TextLen != len([]rune("SECRET_PASSWORD")) {
		t.Fatalf("recorded step missing redaction metadata: %#v", step)
	}
	if step.URL != "https://example.com/login" {
		t.Fatalf("safe URL = %q", step.URL)
	}
}

func TestBrowserRecorderRecordsTypeContentFormat(t *testing.T) {
	recorder := NewBrowserRecorder(func() (*Session, error) { return nil, errors.New("no session") }, nil)
	recorder.recording = true
	recorder.startTime = time.Now()

	recorder.RecordStep("type", "[contenteditable=true]", "# Title", "https://example.com/editor", [2]int{}, "markdown")

	if len(recorder.steps) != 1 {
		t.Fatalf("steps len = %d", len(recorder.steps))
	}
	if recorder.steps[0].ContentFormat != BrowserContentFormatMarkdown {
		t.Fatalf("content format = %q", recorder.steps[0].ContentFormat)
	}
}

func TestFlowReplayerNormalizesClickAtToStableClick(t *testing.T) {
	replayer := NewFlowReplayer(nil, nil, nil)
	step := replayer.recordedStepToStepSpec(RecordedStep{Action: "click_at", Selector: "button.submit", Coords: [2]int{10, 20}}, nil)

	if step.Action != "click" {
		t.Fatalf("action = %q, want click", step.Action)
	}
	if step.Params["selector"] != "button.submit" {
		t.Fatalf("selector = %q", step.Params["selector"])
	}
	if step.Params["fallback_x"] != "10" || step.Params["fallback_y"] != "20" {
		t.Fatalf("fallback coords not preserved: %#v", step.Params)
	}
}

func TestFlowReplayerPreservesRecordedTypeContentFormat(t *testing.T) {
	replayer := NewFlowReplayer(nil, nil, nil)
	step := replayer.recordedStepToStepSpec(RecordedStep{
		Action:        "type",
		Selector:      "[contenteditable=true]",
		Text:          "# Title",
		ContentFormat: "markdown",
	}, nil)

	if step.Action != "type" {
		t.Fatalf("action = %q", step.Action)
	}
	if step.Params["content_format"] != BrowserContentFormatMarkdown {
		t.Fatalf("content_format = %q", step.Params["content_format"])
	}
}
