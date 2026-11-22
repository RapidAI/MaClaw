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
	// The fixture desktop has a focused editor. A test that wants the
	// no-caret refusal replaces this. Production reads the GUI thread.
	computerUseAcceptsTextFn = func() bool { return true }
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

type denyingInput struct {
	countingInput
}

func (d *denyingInput) Click(int, int) error {
	d.clicks++
	return guiautomation.ErrInputDenied
}

func TestExhaustedBudgetRefusesBeforeStaleLabels(t *testing.T) {
	fg := computerUseTestForeground(t)
	cfg := computeruse.DefaultConfig()
	cfg.MaxSteps = 1
	sess := computeruse.NewSession(cfg)
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	computerUseFocusWindowFn = func(string) error {
		t.Fatal("a spent budget must not focus a window")
		return nil
	}
	if err := sess.BeginAction("click", "spend"); err != nil {
		t.Fatal(err)
	}
	if err := sess.BeginAction("click", "over"); err == nil {
		t.Fatal("expected the budget to be spent")
	}
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	msg := cuHandleClick(map[string]interface{}{"x": 20, "y": 20})
	if in.clicks != 0 || !strings.Contains(msg, "max steps exceeded (1)") || strings.Contains(msg, "computer_observe again") {
		t.Fatalf("clicks=%d msg=%s", in.clicks, msg)
	}
	start := time.Now()
	wait := cuHandleWait(map[string]interface{}{"ms": 30000})
	if time.Since(start) > 2*time.Second || !strings.Contains(wait, "max steps exceeded") || strings.Contains(wait, "computer_observe again") {
		t.Fatalf("wait %s after %s", wait, time.Since(start))
	}
	if obs := cuGuardDisabled("computer_observe"); !strings.Contains(obs, "max steps exceeded") || strings.Contains(obs, "computer_observe again") {
		t.Fatalf("observe %s", obs)
	}
	if done := cuGuardDisabled("computer_done"); strings.Contains(done, "max steps") {
		t.Fatalf("done was budget-blocked: %s", done)
	}
}

func TestInputDenialStopsTheNextAction(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	installCountingComputerUse(t, sess)
	computerUseFocusWindowFn = func(string) error {
		t.Fatal("input denial must not focus a window")
		return nil
	}
	computerUsePolicyWindowHook = func(int, int) string { return fg }
	denied := &denyingInput{}
	globalComputerUse.mu.Lock()
	globalComputerUse.input = denied
	globalComputerUse.mu.Unlock()
	first := cuHandleClick(map[string]interface{}{"x": 20, "y": 20})
	if denied.clicks != 1 || !strings.Contains(first, "refused input") || !strings.Contains(first, "computer_done") || strings.Contains(first, "computer_observe again") {
		t.Fatalf("clicks=%d msg=%s", denied.clicks, first)
	}
	if !sess.InputDenied() {
		t.Fatal("the denial did not latch")
	}
	start := time.Now()
	second := cuHandleClick(map[string]interface{}{"x": 20, "y": 20})
	wait := cuHandleWait(map[string]interface{}{"ms": 30000})
	if time.Since(start) > 2*time.Second || denied.clicks != 1 {
		t.Fatalf("clicks=%d after %s click=%s wait=%s", denied.clicks, time.Since(start), second, wait)
	}
	if strings.Contains(second, "computer_observe again") || strings.Contains(wait, "computer_observe again") || !strings.Contains(second, "refused input") {
		t.Fatalf("click=%s wait=%s", second, wait)
	}
	done := cuHandleDone("could not type")
	if strings.Contains(done, "computer_observe") || !strings.Contains(done, "computer_done:") {
		t.Fatalf("done=%s", done)
	}
	if !sess.InputDenied() {
		t.Fatal("computer_done cleared the latch")
	}
}

func TestTypeWithoutKeyboardFocusDoesNotSend(t *testing.T) {
	fg := computerUseTestForeground(t)
	cfg := computeruse.DefaultConfig()
	cfg.MaxSteps = 1
	sess := computeruse.NewSession(cfg)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	computerUseFocusWindowFn = func(string) error {
		t.Fatal("a missing text caret must not focus a window")
		return nil
	}
	computerUseAcceptsTextFn = func() bool { return false }
	missed := cuHandleType(map[string]interface{}{"text": "码卡龙现在很擅长给蓝信中的用户发消息了"})
	paste := cuHandleKey(map[string]interface{}{"keys": "ctrl+v"})
	plain := cuHandleKey(map[string]interface{}{"keys": "ctrl+shift+v"})
	ins := cuHandleKey(map[string]interface{}{"keys": "shift+insert"})
	if in.types != 0 || in.keys != 0 || in.clicks != 0 {
		t.Fatalf("types=%d keys=%d clicks=%d missed=%s paste=%s plain=%s insert=%s", in.types, in.keys, in.clicks, missed, paste, plain, ins)
	}
	if !strings.Contains(missed, "text field") || !strings.Contains(paste, "text field") || !strings.Contains(plain, "text field") || !strings.Contains(ins, "text field") {
		t.Fatalf("missed=%s paste=%s plain=%s insert=%s", missed, paste, plain, ins)
	}
	if sess.InputDenied() {
		t.Fatal("a missing caret is not an input denial")
	}
	computerUseAcceptsTextFn = func() bool { return true }
	typed := cuHandleType(map[string]interface{}{"text": "码卡龙现在很擅长给蓝信中的用户发消息了"})
	if in.types != 1 || strings.Contains(typed, "text field") || strings.Contains(typed, "max steps") {
		t.Fatalf("types=%d msg=%s", in.types, typed)
	}

	enterSess := computeruse.NewSession(computeruse.DefaultConfig())
	enterSess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	enterIn := &countingInput{}
	globalComputerUse.mu.Lock()
	globalComputerUse.session = enterSess
	globalComputerUse.input = enterIn
	globalComputerUse.mu.Unlock()
	computerUseAcceptsTextFn = func() bool { return false }
	enter := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if strings.Contains(enter, "text field") {
		t.Fatalf("enter is not a paste: %s", enter)
	}
	if enterIn.keys > 1 {
		t.Fatalf("keys=%d enter=%s", enterIn.keys, enter)
	}
}

func TestRefTypeWithoutKeyboardFocusDoesNotPaste(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	computerUseFocusWindowFn = func(string) error {
		t.Fatal("a ref type must not focus a window")
		return nil
	}
	computerUseAcceptsTextFn = func() bool { return false }
	msg := cuHandleType(map[string]interface{}{"ref": "e0", "text": "龙军"})
	if in.clicks != 1 || in.types != 0 || !strings.Contains(msg, "text field") || sess.InputDenied() {
		t.Fatalf("clicks=%d types=%d denied=%v msg=%s", in.clicks, in.types, sess.InputDenied(), msg)
	}
}
