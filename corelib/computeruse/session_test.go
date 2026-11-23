package computeruse

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestSession_ObserveClickRefLifecycle(t *testing.T) {
	s := NewSession(DefaultConfig())
	els := []taskengine.UIElement{
		{Name: "element_0", BBox: [4]int{0, 0, 10, 10}, Source: "yolo", Interactable: true, Confidence: 0.9},
	}
	ocr := []taskengine.OCRResult{{Text: "OK", BBox: [4]int{1, 1, 8, 8}}}
	obs := s.CommitObserve(ScreenMeta{Width: 100, Height: 100, ScaleFactor: 1}, []string{"App"}, els, ocr, "fake-png-b64")
	if obs.TextForModel == "" || strings.Contains(obs.TextForModel, "fake-png") {
		t.Fatalf("bad TextForModel: %q", obs.TextForModel)
	}
	if !s.RefsValid() {
		t.Fatal("refs should be valid after observe")
	}
	if s.LastValidObserve() == nil {
		t.Fatal("LastValidObserve should return a snapshot after observe")
	}
	x, y, el, err := s.ResolveClickRef("e0")
	if err != nil {
		t.Fatal(err)
	}
	if x != 5 || y != 5 || el.Name != "OK" {
		t.Fatalf("got %d,%d name=%q", x, y, el.Name)
	}
	if err := s.BeginAction("click", "e0"); err != nil {
		t.Fatal(err)
	}
	s.RecordAction("click", "e0", true, "", true)
	if s.RefsValid() {
		t.Fatal("refs should be stale after click")
	}
	if s.LastValidObserve() != nil {
		t.Fatal("LastValidObserve must be nil after refs are invalidated")
	}
	if _, _, _, err := s.ResolveClickRef("e0"); err == nil {
		t.Fatal("expected stale after invalidate")
	}
}

func TestSession_MaxSteps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxSteps = 2
	s := NewSession(cfg)
	if err := s.BeginAction("a", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginAction("b", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginAction("c", ""); err == nil {
		t.Fatal("expected max steps error")
	}
	before := s.Policy().StepCount()
	max, exhausted := s.StepsExhausted()
	if !exhausted || max != 2 || s.Policy().StepCount() != before {
		t.Fatalf("exhausted=%v max=%d steps=%d before=%d", exhausted, max, s.Policy().StepCount(), before)
	}
	s.NoteInputDenied()
	if !s.InputDenied() {
		t.Fatal("input denial did not latch")
	}
	s.ResetControl()
	if s.InputDenied() {
		t.Fatal("reset kept the input denial")
	}
	if _, exhausted := s.StepsExhausted(); !exhausted {
		t.Fatal("reset cleared the step budget")
	}
	s.NoteInputDenied()
	s.ClearInputDenied()
	if s.InputDenied() {
		t.Fatal("clear kept the input denial")
	}
	if _, exhausted := s.StepsExhausted(); !exhausted {
		t.Fatal("clearing the denial reset the step budget")
	}
	s.Pause()
	s.NoteInputDenied()
	s.ResetSteps()
	if !s.InputDenied() {
		t.Fatal("reset steps cleared the input denial")
	}
	paused, stopped := s.ControlState()
	if !paused || stopped {
		t.Fatalf("reset steps changed pause=%v stopped=%v", paused, stopped)
	}
	if s.Policy().StepCount() != 0 {
		t.Fatalf("steps=%d", s.Policy().StepCount())
	}
	if _, exhausted := s.StepsExhausted(); exhausted {
		t.Fatal("reset steps left the budget spent")
	}
	if err := s.BeginAction("after-reset", ""); err == nil {
		t.Fatal("a paused session accepted a step after the counter reset")
	}
	if s.Policy().StepCount() != 0 {
		t.Fatal("a refused step incremented the counter")
	}
}

func TestWindowHungLatchClearsForAnotherAttempt(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.NoteWindowHung()
	if !s.WindowHung() {
		t.Fatal("window health failure did not latch")
	}
	s.ClearWindowHung()
	if s.WindowHung() {
		t.Fatal("a new attempt kept the old window failure")
	}
	s.NoteWindowHung()
	s.ResetControl()
	if s.WindowHung() {
		t.Fatal("reset kept the window failure")
	}
}

func TestPolicy_BlockedWindow(t *testing.T) {
	p := NewPolicy(DefaultConfig())
	if err := p.AllowClickAt(1, 1, "User Account Control"); err == nil {
		t.Fatal("expected UAC block")
	}
	if err := p.AllowClickAt(1, 1, "Notepad"); err != nil {
		t.Fatal(err)
	}
}

func TestPolicy_TargetApps(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TargetApps = []string{"Notepad", "记事本"}
	p := NewPolicy(cfg)
	if err := p.AllowClickAt(0, 0, "Chrome - Google"); err == nil {
		t.Fatal("expected outside target")
	}
	if err := p.AllowClickAt(0, 0, "Untitled - Notepad"); err != nil {
		t.Fatal(err)
	}
}

func TestPlaybookBeforePerceptionNamesBothResults(t *testing.T) {
	p := PlaybookBeforePerception()
	for _, want := range []string{"screenshot", "ref=eN", "computer_find", "computer_done", "search box", "foreground-window crop", "refresh failed", "screen_index=-1"} {
		if !strings.Contains(p, want) {
			t.Fatalf("playbook missing %q: %s", want, p)
		}
	}
	if strings.Contains(p, "Screenshots are NOT sent") || strings.Contains(p, "computer_use") {
		t.Fatal(p)
	}
	if strings.Contains(p, "After every click, type, key, scroll, focus, select, or drag, call computer_observe again") {
		t.Fatal(p)
	}
}

func TestPlaybookMentionsTextOnly(t *testing.T) {
	if !strings.Contains(Playbook(), "text-only") {
		t.Fatal(Playbook())
	}
}

func TestPlaybookVisionUsesScreenshotClicks(t *testing.T) {
	p := PlaybookVision()
	if !strings.Contains(p, "vision") || !strings.Contains(p, "screenshot") {
		t.Fatal(p)
	}
	if strings.Contains(p, "text-only") {
		t.Fatal("vision playbook should not claim text-only")
	}
	if strings.Contains(p, "call computer_observe again so you see the new screen") {
		t.Fatal(p)
	}
	if !strings.Contains(p, "foreground-window crop") || !strings.Contains(p, "screen_index=-1") {
		t.Fatal(p)
	}
}

func TestSession_ApplyPerceptionModeEnablesPixelClick(t *testing.T) {
	s := NewSession(DefaultConfig())
	if s.AllowPixelClick() {
		t.Fatal("text-primary default forbids pixel click")
	}
	s.ApplyPerceptionMode(true)
	if s.Config().Mode != ObserveVisionAssist || !s.AllowPixelClick() {
		t.Fatalf("vision mode=%s pixel=%v", s.Config().Mode, s.AllowPixelClick())
	}
	s.ApplyPerceptionMode(false)
	if s.Config().Mode != ObserveTextPrimary || s.AllowPixelClick() {
		t.Fatalf("text mode=%s pixel=%v", s.Config().Mode, s.AllowPixelClick())
	}
}

func TestMapVisionClick(t *testing.T) {
	meta := ScreenMeta{Width: 1920, Height: 1080, VisionWidth: 960, VisionHeight: 540}
	x, y := MapVisionClick(meta, 100, 50)
	if x != 200 || y != 100 {
		t.Fatalf("got %d,%d", x, y)
	}
	same := ScreenMeta{Width: 800, Height: 600, VisionWidth: 800, VisionHeight: 600}
	x, y = MapVisionClick(same, 10, 20)
	if x != 10 || y != 20 {
		t.Fatalf("1:1 got %d,%d", x, y)
	}
}

func TestSession_PauseResumeStop(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.Pause()
	if err := s.BeginAction("click", "x"); err == nil {
		t.Fatal("expected pause block")
	}
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginAction("click", "x"); err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if err := s.BeginAction("type", "y"); err == nil {
		t.Fatal("expected stop block")
	}
	if err := s.Resume(); err == nil {
		t.Fatal("resume after stop should fail")
	}
	s.ResetControl()
	if err := s.BeginAction("key", "z"); err != nil {
		t.Fatal(err)
	}
	paused, stopped := s.ControlState()
	if paused || stopped {
		t.Fatalf("after reset paused=%v stopped=%v", paused, stopped)
	}
}
