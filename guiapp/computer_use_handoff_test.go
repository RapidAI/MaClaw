package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

type countingInput struct {
	clicks  int
	types   int
	keys    int
	scrolls int
	drags   int
}

func (c *countingInput) Click(x, y int) error          { c.clicks++; return nil }
func (c *countingInput) RightClick(x, y int) error     { c.clicks++; return nil }
func (c *countingInput) DoubleClick(x, y int) error    { c.clicks++; return nil }
func (c *countingInput) Type(text string) error        { c.types++; return nil }
func (c *countingInput) KeyCombo(keys ...string) error { c.keys++; return nil }
func (c *countingInput) Scroll(x, y, deltaX, deltaY int) error {
	c.scrolls++
	return nil
}
func (c *countingInput) DragDrop(fromX, fromY, toX, toY int) error {
	c.drags++
	return nil
}

func installCountingComputerUse(t *testing.T, sess *computeruse.Session) *countingInput {
	t.Helper()
	resetComputerUseRuntimeForTest(t)
	in := &countingInput{}
	t.Cleanup(func() {
		computeruse.SetDesktopHandoffNotifier(nil)
		_ = computeruse.ResolveDesktopHandoff(false)
		globalComputerUse.mu.Lock()
		globalComputerUse.input = nil
		globalComputerUse.mu.Unlock()
	})
	globalComputerUse.mu.Lock()
	globalComputerUse.session = sess
	globalComputerUse.input = in
	globalComputerUse.mu.Unlock()
	return in
}

func TestComputerUsePayClickDoesNotInject(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{0, 0, 20, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	msg := cuHandleClick(map[string]interface{}{"ref": "e0"})
	if in.clicks != 0 {
		t.Fatalf("pay click injected %d: %s", in.clicks, msg)
	}
	if !strings.Contains(msg, "confirm_order") || strings.Contains(msg, "123456") {
		t.Fatalf("msg %s", msg)
	}
}

func TestComputerUsePixelPayClickUsesTheControlUnderThePoint(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
		{Name: "立即支付", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	msg := cuHandleClick(map[string]interface{}{"x": 30, "y": 20})
	if in.clicks != 0 || !strings.Contains(msg, "像素") {
		t.Fatalf("clicks=%d msg=%s", in.clicks, msg)
	}
}

func TestComputerUseSecretTypeDoesNotInject(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "支付密码", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	msg := cuHandleType(map[string]interface{}{"ref": "e0", "text": "123456"})
	if in.types != 0 || in.clicks != 0 {
		t.Fatalf("secret typed clicks=%d types=%d", in.clicks, in.types)
	}
	if strings.Contains(msg, "123456") || !strings.Contains(msg, "密码") {
		t.Fatalf("msg %s", msg)
	}
}

func TestComputerUseOCRPayClickDoesNotInject(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, []taskengine.OCRResult{
		{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}},
	}, "")
	in := installCountingComputerUse(t, sess)
	msg := cuHandleClick(map[string]interface{}{"x": 30, "y": 20})
	if in.clicks != 0 || !strings.Contains(msg, "confirm_order") || strings.Contains(msg, "123456") {
		t.Fatalf("clicks=%d msg=%s", in.clicks, msg)
	}
}

func TestComputerUsePayEnterDoesNotInject(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	msg := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if in.keys != 0 || !strings.Contains(msg, "confirm_order") || strings.Contains(msg, "123456") {
		t.Fatalf("keys=%d msg=%s", in.keys, msg)
	}
}

func TestComputerUseEnterInSearchStillSends(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 120, Height: 40, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
		{Name: "立即支付", BBox: [4]int{60, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	click := cuHandleClick(map[string]interface{}{"ref": "e0"})
	if in.clicks != 1 {
		t.Fatalf("search click %d %s", in.clicks, click)
	}
	sess.CommitObserve(computeruse.ScreenMeta{Width: 120, Height: 40, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
		{Name: "立即支付", BBox: [4]int{60, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	msg := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if in.keys != 1 || strings.Contains(msg, "blocked") {
		t.Fatalf("keys=%d msg=%s", in.keys, msg)
	}
}

func TestComputerUsePasswordFieldKeysDoNotInject(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "支付密码", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	click := cuHandleClick(map[string]interface{}{"ref": "e0"})
	if in.clicks != 1 {
		t.Fatalf("focus click %d %s", in.clicks, click)
	}
	typed := cuHandleType(map[string]interface{}{"text": "998877"})
	keyed := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if in.types != 0 || in.keys != 0 {
		t.Fatalf("types=%d keys=%d typed=%s keyed=%s", in.types, in.keys, typed, keyed)
	}
	if strings.Contains(typed, "998877") || strings.Contains(keyed, "998877") || !strings.Contains(typed, "密码") {
		t.Fatalf("typed=%s keyed=%s", typed, keyed)
	}
}

func TestComputerUsePayClickWaitsForThePerson(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{0, 0, 20, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	ready := make(chan struct{}, 1)
	computeruse.SetDesktopHandoffNotifier(func(n computeruse.DesktopHandoffNotice) {
		if n.Active {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	done := make(chan string, 1)
	go func() {
		done <- cuHandleClick(map[string]interface{}{"ref": "e0"})
	}()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("handoff was not shown")
	}
	if err := computeruse.ResolveDesktopHandoff(true); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-done:
		if in.clicks != 0 || !strings.Contains(msg, "computer_observe") || strings.Contains(msg, "123456") {
			t.Fatalf("clicks=%d msg=%s", in.clicks, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("click did not return after the person finished")
	}
}

func TestDesktopHandoffKeepsAManualPause(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	t.Cleanup(clearDesktopHandoffPause)
	manual := computeruse.NewSession(computeruse.DefaultConfig())
	manual.Pause()
	globalComputerUse.mu.Lock()
	globalComputerUse.session = manual
	globalComputerUse.mu.Unlock()
	other := computeruse.NewSession(computeruse.DefaultConfig())

	noteDesktopHandoff(computeruse.DesktopHandoffNotice{Active: true, Reason: "secret", Title: "支付密码"})
	if paused, _ := other.ControlState(); paused {
		t.Fatal("handoff paused a session that is not driving")
	}
	if paused, _ := manual.ControlState(); !paused {
		t.Fatal("handoff cleared a pause the person already set")
	}
	noteDesktopHandoff(computeruse.DesktopHandoffNotice{Active: false, Resume: true})
	if paused, _ := manual.ControlState(); !paused {
		t.Fatal("ending the handoff resumed a pause the person set")
	}
}

func TestDesktopHandoffResumesOnlyWhatItPaused(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	t.Cleanup(clearDesktopHandoffPause)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	globalComputerUse.mu.Lock()
	globalComputerUse.session = sess
	globalComputerUse.mu.Unlock()

	noteDesktopHandoff(computeruse.DesktopHandoffNotice{Active: true, Reason: "checkout", Title: "立即支付"})
	if paused, _ := sess.ControlState(); !paused {
		t.Fatal("handoff should pause the live session")
	}
	noteDesktopHandoff(computeruse.DesktopHandoffNotice{Active: false, Resume: true})
	if paused, _ := sess.ControlState(); paused {
		t.Fatal("handoff should resume the session it paused")
	}

	noteDesktopHandoff(computeruse.DesktopHandoffNotice{Active: true, Reason: "checkout", Title: "立即支付"})
	noteDesktopHandoff(computeruse.DesktopHandoffNotice{Active: false, Resume: false})
	if paused, _ := sess.ControlState(); !paused {
		t.Fatal("a hard stop must leave the pause for Stop to finish")
	}
}

// Keep the interface check local so a stub cannot drift from InputSimulator.
var _ guiautomation.InputSimulator = (*countingInput)(nil)
