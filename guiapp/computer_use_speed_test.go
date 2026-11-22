package guiapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestRefuseStitchedCapture(t *testing.T) {
	if !refuseStitchedCapture(-1, 8_000_001) {
		t.Fatal("over-budget stitch should be refused")
	}
	if refuseStitchedCapture(-1, computerUseStitchPixelBudget) {
		t.Fatal("a stitch at the budget should be kept")
	}
	if refuseStitchedCapture(0, 20_000_000) {
		t.Fatal("a single monitor is not a stitch")
	}
	if refuseStitchedCapture(-1, 0) {
		t.Fatal("an unknown size should not refuse")
	}
}

func TestFinishActionAppendsRefresh(t *testing.T) {
	prev := computerUseAfterActionRefresh
	computerUseAfterActionRefresh = func(prefix, window string) string {
		if window != "" {
			t.Fatalf("window %q", window)
		}
		return prefix + "\nforeground crop"
	}
	t.Cleanup(func() { computerUseAfterActionRefresh = prev })
	got := cuFinishAction("clicked e1 via a11y")
	if got != "clicked e1 via a11y\nforeground crop" {
		t.Fatalf("got %q", got)
	}
	computerUseAfterActionRefresh = func(prefix, window string) string {
		return prefix + "|" + window
	}
	if got := cuFinishAction("focused", "蓝信"); got != "focused|蓝信" {
		t.Fatalf("window not forwarded: %q", got)
	}
	computerUseAfterActionRefresh = nil
	if got := cuFinishAction("pressed enter"); got != "pressed enter" {
		t.Fatalf("nil refresh got %q", got)
	}
}

func TestComputerUseExperienceReachesTheNextPlaybook(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	dir := t.TempDir()
	prev := computerUseExperienceDirForTest
	computerUseExperienceDirForTest = dir
	t.Cleanup(func() {
		computerUseExperienceDirForTest = prev
		computeruse.SetExperienceDir("")
	})
	beginComputerUseTask("sk-exp", "req-exp", "向蓝信中的龙军用户问好", nil)
	setComputerUseOwner("sk-exp")
	section := computerUsePlaybookSectionFor(true, "sk-exp")
	if !strings.Contains(section, "lanxin") || !strings.Contains(section, "search box") || !strings.Contains(section, "hint:") {
		t.Fatalf("playbook missing the lanxin hint: %s", section)
	}
	if strings.Contains(section, "龙军") || strings.Contains(section, "lessons from earlier") {
		t.Fatal(section)
	}
	line := computerUseRememberExperience("lanxin", computeruse.NoteSearchFind, true)
	if !strings.Contains(line, "worked:") {
		t.Fatalf("record did not return the sentence: %q", line)
	}
	again := computerUsePlaybookSectionFor(true, "sk-exp")
	if !strings.Contains(again, "worked:") || !strings.Contains(again, "computer_find") {
		t.Fatalf("learned note did not come back: %s", again)
	}
	if computerUseRememberExperience("", computeruse.NoteFindMissed, false) != "" {
		t.Fatal("an empty window stored a find miss on desktop")
	}
}

func TestComputerUseBeginKeepsTheOriginalGoal(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	ctx := &LoopContext{
		ComputerUseFresh:       true,
		ComputerUseRoutingText: "向蓝信中的龙军用户问好",
		Runtime: RuntimeContext{
			RequestID:    "chat-task-1-1",
			Conversation: RuntimeConversationRef{SessionKey: "sk-route"},
		},
	}
	maybeBeginComputerUseTask(ctx, "u1", "[系统续轮] 继续完成同一任务")
	st := computerUseTaskStateFor("sk-route")
	if st == nil || st.Goal != "向蓝信中的龙军用户问好" {
		t.Fatalf("goal %+v", st)
	}
	if strings.Contains(st.Goal, "续轮") {
		t.Fatal(st.Goal)
	}
}

func TestLightweightRefreshUsesTheOperatedWindow(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	globalComputerUse.mu.Lock()
	prevOCR := globalComputerUse.ocr
	globalComputerUse.ocr = nil
	globalComputerUse.turnVisionKnown = true
	globalComputerUse.turnVision = false
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.ocr = prevOCR
		globalComputerUse.turnVisionKnown = false
		globalComputerUse.turnVision = false
		globalComputerUse.mu.Unlock()
	})
	seedComputerUseObserve(t, "alpha", "Btn")
	if !cuSession().RefsValid() {
		t.Fatal("seed observe should leave refs valid")
	}
	prev := computerUseRefreshCapture
	var gotWindow string
	computerUseRefreshCapture = func(window string) (computerUseCapture, error) {
		gotWindow = window
		return computerUseCapture{
			PNG:    "crop",
			Meta:   computeruse.ScreenMeta{CropTitle: "蓝信", Width: 800, Height: 600, ScreenIndex: 0, ScaleFactor: 1},
			Width:  800,
			Height: 600,
		}, nil
	}
	t.Cleanup(func() { computerUseRefreshCapture = prev })
	computerUseForegroundTitleFn = func() string { return "蓝信" }
	computerUseFocusWindowFn = func(string) error {
		t.Fatal("named refresh must not focus another window")
		return nil
	}
	text := cuRefreshForeground("clicked e1", "蓝信")
	if gotWindow != "蓝信" {
		t.Fatalf("window %q", gotWindow)
	}
	if !strings.Contains(text, `crop of "蓝信"`) || !strings.Contains(text, "window just operated") {
		t.Fatalf("refresh text: %s", text)
	}
	if strings.Contains(text, "not the foreground window") || strings.Contains(text, "elements (") {
		t.Fatalf("refresh looked like a full observe: %s", text)
	}
	if cuSession().RefsValid() || cuSession().LabelsCurrent() {
		t.Fatal("a crop without OCR must not keep the previous refs or pay labels")
	}
	if name := cuSession().ElementNameAtScreen(10, 10); name != "" {
		t.Fatalf("stale element %q", name)
	}
	miss := cuDescribeRefresh("clicked", computerUseCapture{
		Meta:   computeruse.ScreenMeta{ScreenIndex: 0, Width: 2560, Height: 1440},
		Width:  2560,
		Height: 1440,
		PNG:    "monitor",
	})
	if strings.Contains(miss, "foreground window") && !strings.Contains(miss, "not the foreground window") {
		t.Fatalf("monitor fallback claimed the foreground: %s", miss)
	}
	if !strings.Contains(miss, "not the foreground window") || !strings.Contains(miss, "screen_index=0") || !strings.Contains(miss, "2560x1440") {
		t.Fatalf("monitor fallback: %s", miss)
	}
}

func TestStitchRefusalNamesTheCapturedScreen(t *testing.T) {
	cropped := computerUseStitchRefusalNote(6576, 2160, true, computerUseCapture{
		Meta: computeruse.ScreenMeta{CropTitle: "蓝信"},
	})
	if !strings.Contains(cropped, "foreground window") || !strings.Contains(cropped, "蓝信") {
		t.Fatal(cropped)
	}
	if strings.Contains(cropped, "not the foreground window") {
		t.Fatal(cropped)
	}
	fallback := computerUseStitchRefusalNote(6576, 2160, true, computerUseCapture{
		Meta:   computeruse.ScreenMeta{ScreenIndex: 0},
		Width:  2560,
		Height: 1440,
	})
	if strings.Contains(fallback, "This result is the foreground window") {
		t.Fatal(fallback)
	}
	if !strings.Contains(fallback, "not the foreground window") || !strings.Contains(fallback, "screen_index=0") || !strings.Contains(fallback, "2560x1440") {
		t.Fatal(fallback)
	}
}

func tinyComputerUsePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestSettleShotRebuildsClickGeometry(t *testing.T) {
	clearComputerUseSettle()
	t.Cleanup(clearComputerUseSettle)
	storeComputerUseSettle(tinyComputerUsePNG(t, 100, 50), computeruse.ScreenMeta{
		CropTitle: "蓝信", Width: 200, Height: 100, ScaleFactor: 2,
		OriginX: 100, OriginY: 50, ScreenIndex: 1,
	})
	cap, err := captureComputerUseRefresh("蓝信")
	if err != nil {
		t.Fatal(err)
	}
	if cap.Width != 100 || cap.Height != 50 || cap.Meta.Width != 100 || cap.Meta.Height != 50 {
		t.Fatalf("size %+v %dx%d", cap.Meta, cap.Width, cap.Height)
	}
	if cap.Meta.ScaleFactor != 1 || cap.Meta.OriginX != 100 || cap.Meta.OriginY != 50 || cap.Meta.CropTitle != "蓝信" || cap.Meta.ScreenIndex != 1 {
		t.Fatalf("meta %+v", cap.Meta)
	}
	sx, sy := computeruse.MapCaptureToScreen(cap.Meta, 50, 25)
	if sx != 150 || sy != 75 {
		t.Fatalf("click maps to %d,%d", sx, sy)
	}
}

func TestSettleShotMatchesOnlyTheOperatedWindow(t *testing.T) {
	clearComputerUseSettle()
	t.Cleanup(clearComputerUseSettle)
	storeComputerUseSettle("png-lanxin", computeruse.ScreenMeta{CropTitle: "蓝信", Width: 20, Height: 20})
	if _, ok := takeComputerUseSettle("记事本"); ok {
		t.Fatal("a different window reused the settle shot")
	}
	storeComputerUseSettle("png-empty", computeruse.ScreenMeta{Width: 20, Height: 20})
	if _, ok := takeComputerUseSettle("蓝信"); ok {
		t.Fatal("an empty crop title matched a window")
	}
	storeComputerUseSettle("png-lanxin", computeruse.ScreenMeta{CropTitle: "蓝信", Width: 20, Height: 20, ScaleFactor: 1})
	cap, err := captureComputerUseRefresh("蓝信")
	if err != nil || cap.PNG != "png-lanxin" || cap.Meta.CropTitle != "蓝信" {
		t.Fatalf("cap %+v err %v", cap, err)
	}
	if _, ok := takeComputerUseSettle("蓝信"); ok {
		t.Fatal("settle shot was reused twice")
	}
}

func TestVisionSearchLessonNeedsFocusTypeAndFewObserves(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	dir := t.TempDir()
	prevDir := computerUseExperienceDirForTest
	computerUseExperienceDirForTest = dir
	t.Cleanup(func() {
		computerUseExperienceDirForTest = prevDir
		computeruse.SetExperienceDir("")
		globalComputerUse.mu.Lock()
		globalComputerUse.turnVisionKnown = false
		globalComputerUse.turnVision = false
		globalComputerUse.mu.Unlock()
		resetComputerUseExperienceTrace()
	})
	setComputerUseTurnVision(true)
	resetComputerUseExperienceTrace()
	if computerUseRememberVisionSearch("蓝信") != "" {
		t.Fatal("type without focus was stored")
	}
	computerUseTraceFocus()
	computerUseTraceType()
	line := computerUseRememberVisionSearch("蓝信")
	if !strings.Contains(line, "worked:") || !strings.Contains(line, "search box") {
		t.Fatalf("vision search line: %q", line)
	}
	resetComputerUseExperienceTrace()
	computerUseTraceFocus()
	computerUseTraceType()
	for i := 0; i < computeruse.ExperienceSuccessObserveMax+1; i++ {
		computerUseTraceObserve()
	}
	if computerUseRememberVisionSearch("蓝信") != "" {
		t.Fatal("a long observe round stored a success")
	}
	resetComputerUseExperienceTrace()
	computerUseTraceFocus()
	line = computerUseRememberFind("蓝信", "", true)
	if !strings.Contains(line, "experience:") || !strings.Contains(line, "on-screen text") {
		t.Fatalf("find hit: %q", line)
	}
	if strings.Contains(line, "search box") {
		t.Fatalf("find without a prior type stored the search path: %q", line)
	}
	miss := computerUseRememberFind("完全未知的窗口", "", false)
	if miss != "" {
		t.Fatalf("unknown window miss: %q", miss)
	}
}

type labelOCR struct {
	calls int
	lines []taskengine.OCRResult
}

func (o *labelOCR) Recognize(string) ([]taskengine.OCRResult, error) {
	o.calls++
	return o.lines, nil
}

func (o *labelOCR) IsAvailable() bool { return true }

func installLabelOCR(t *testing.T, ocr taskengine.OCRProvider, sidecar *browser.NativeOCRProvider) {
	t.Helper()
	globalComputerUse.mu.Lock()
	prevOCR, prevSide := globalComputerUse.ocr, globalComputerUse.ocrSidecar
	globalComputerUse.ocr = ocr
	globalComputerUse.ocrSidecar = sidecar
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.ocr, globalComputerUse.ocrSidecar = prevOCR, prevSide
		globalComputerUse.mu.Unlock()
	})
}

func computerUseTestForeground(t *testing.T) string {
	t.Helper()
	title := strings.TrimSpace(accessibility.ForegroundWindowTitle())
	if title == "" {
		// This session has no foreground window. WindowTitlesMatch treats an
		// empty live title as unknown, so a fixed crop still passes same-window.
		return "计算机使用测试"
	}
	return title
}

func observeCount(sess *computeruse.Session) int {
	n := 0
	for _, a := range sess.Audit() {
		if a.Action == "observe" {
			n++
		}
	}
	return n
}

func TestRefreshPayLabelsBlockEnterUntilOCR(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	resetComputerUseExperienceTrace()
	ocr := &labelOCR{lines: []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}}}}
	installLabelOCR(t, ocr, nil)
	msg := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if _, _, observes := computerUseTraceSnapshot(); observes != 0 {
		t.Fatalf("label OCR counted as computer_observe: %d", observes)
	}
	if in.keys != 0 || ocr.calls != 1 || !strings.Contains(msg, "支付") {
		t.Fatalf("keys=%d calls=%d msg=%s", in.keys, ocr.calls, msg)
	}
	if sess.RefsValid() || observeCount(sess) != 1 {
		t.Fatalf("refs=%v observes=%d", sess.RefsValid(), observeCount(sess))
	}
}

func TestRefreshPayLabelsRefuseEnterWhenOCRIsEmpty(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: fg}, "crop")
	ocr := &labelOCR{}
	installLabelOCR(t, ocr, nil)
	msg := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if in.keys != 0 || ocr.calls != 1 || !strings.Contains(msg, "call computer_observe again") {
		t.Fatalf("keys=%d calls=%d msg=%s", in.keys, ocr.calls, msg)
	}
}

func TestRefreshPayLabelsDoNotColdLoadOCR(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: fg}, "crop")
	ocr := &labelOCR{lines: []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{0, 0, 40, 20}}}}
	sidecar := browser.NewNativeOCRProvider("", "", nil)
	if sidecar.Ready() {
		t.Fatal("an unloaded OCR provider reported ready")
	}
	installLabelOCR(t, ocr, sidecar)
	msg := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if ocr.calls != 0 || in.keys != 0 || !strings.Contains(msg, "call computer_observe again") {
		t.Fatalf("keys=%d calls=%d msg=%s", in.keys, ocr.calls, msg)
	}
}

func TestSearchEnterStillSendsAfterLabelOCR(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	click := cuHandleClick(map[string]interface{}{"ref": "e0"})
	if in.clicks != 1 {
		t.Fatalf("search click %d %s", in.clicks, click)
	}
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	ocr := &labelOCR{lines: []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{60, 0, 40, 20}}}}
	installLabelOCR(t, ocr, nil)
	msg := cuHandleKey(map[string]interface{}{"keys": "enter"})
	labels := strings.Join(sess.KeySurfaceLabels(fg), "\n")
	if in.keys != 1 || strings.Contains(msg, "blocked") || !strings.Contains(labels, "立即支付") {
		t.Fatalf("keys=%d msg=%s labels=%s", in.keys, msg, labels)
	}
}

func TestPixelClickReadsOCRLabelsAfterRefresh(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	resetComputerUseExperienceTrace()
	ocr := &labelOCR{lines: []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}}}}
	installLabelOCR(t, ocr, nil)
	msg := cuHandleClick(map[string]interface{}{"x": 30, "y": 20})
	if _, _, observes := computerUseTraceSnapshot(); observes != 0 {
		t.Fatalf("label OCR counted as computer_observe: %d", observes)
	}
	if in.clicks != 0 || ocr.calls != 1 || !strings.Contains(msg, "支付") || sess.RefsValid() {
		t.Fatalf("clicks=%d calls=%d refs=%v msg=%s", in.clicks, ocr.calls, sess.RefsValid(), msg)
	}
}

type localLabelOCR struct {
	ready bool
	calls int
	lines []browser.OCRResult
	err   error
}

func (o *localLabelOCR) Ready() bool { return o.ready }

func (o *localLabelOCR) Recognize(string) ([]browser.OCRResult, error) {
	o.calls++
	if o.err != nil {
		return nil, o.err
	}
	return o.lines, nil
}

type unavailableLabelOCR struct {
	calls int
}

func (o *unavailableLabelOCR) Recognize(string) ([]taskengine.OCRResult, error) {
	o.calls++
	return []taskengine.OCRResult{{Text: "should-not", BBox: [4]int{1, 1, 4, 4}}}, nil
}

func (o *unavailableLabelOCR) IsAvailable() bool { return false }

func TestReadLoadedCropLabelsUsesTheLocalEngine(t *testing.T) {
	local := &localLabelOCR{ready: true, lines: []browser.OCRResult{{Text: "搜索", BBox: [4]int{1, 2, 3, 4}}}}
	fallback := &labelOCR{lines: []taskengine.OCRResult{{Text: "should-not", BBox: [4]int{1, 1, 4, 4}}}}
	res, ok := readLoadedCropLabels(local, fallback, "png")
	if !ok || local.calls != 1 || fallback.calls != 0 || len(res) != 1 || res[0].Text != "搜索" {
		t.Fatalf("ok=%v local=%d fallback=%d res=%v", ok, local.calls, fallback.calls, res)
	}
	local.ready = false
	local.calls = 0
	if _, ok := readLoadedCropLabels(local, fallback, "png"); ok || local.calls != 0 || fallback.calls != 0 {
		t.Fatalf("unready local fell through: calls local=%d fallback=%d", local.calls, fallback.calls)
	}
	local.ready = true
	local.err = errors.New("boom")
	if _, ok := readLoadedCropLabels(local, fallback, "png"); ok || fallback.calls != 0 {
		t.Fatal("a local error used the vision wrapper")
	}
	local.err = nil
	local.lines = []browser.OCRResult{{Text: "立即支付", BBox: [4]int{0, 0, 0, 0}}}
	local.calls = 0
	if _, ok := readLoadedCropLabels(local, fallback, "png"); ok || local.calls != 1 || fallback.calls != 0 {
		t.Fatalf("zero-box local ok=%v calls=%d fallback=%d", ok, local.calls, fallback.calls)
	}
	res, ok = readLoadedCropLabels(nil, fallback, "png")
	if !ok || fallback.calls != 1 || res[0].Text != "should-not" {
		t.Fatalf("nil local ok=%v calls=%d res=%v", ok, fallback.calls, res)
	}
	off := &unavailableLabelOCR{}
	if _, ok := readLoadedCropLabels(nil, off, "png"); ok || off.calls != 0 {
		t.Fatal("an unavailable fallback ran")
	}
}

func TestPixelClickRefusesWhenOCRUnloaded(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	installLabelOCR(t, nil, nil)
	msg := cuHandleClick(map[string]interface{}{"x": 30, "y": 20})
	if in.clicks != 0 || !strings.Contains(msg, "call computer_observe again") || sess.LabelsCurrent() {
		t.Fatalf("clicks=%d current=%v msg=%s", in.clicks, sess.LabelsCurrent(), msg)
	}
	pay := cuHandleClick(map[string]interface{}{"x": 30, "y": 20, "target": "立即支付"})
	if in.clicks != 0 || !strings.Contains(pay, "支付") || strings.Contains(pay, "control labels are from before") {
		t.Fatalf("explicit pay click=%d msg=%s", in.clicks, pay)
	}
}

func TestRefreshPayLabelsRefuseEnterWhenOCRHasNoBox(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: fg}, "crop")
	ocr := &labelOCR{lines: []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{0, 0, 0, 0}}}}
	installLabelOCR(t, ocr, nil)
	msg := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if in.keys != 0 || ocr.calls != 1 || sess.LabelsCurrent() || !strings.Contains(msg, "call computer_observe again") {
		t.Fatalf("keys=%d calls=%d current=%v msg=%s", in.keys, ocr.calls, sess.LabelsCurrent(), msg)
	}
}

func TestRefreshFailureDropsPayLabels(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	prevHook := computerUseAfterActionRefresh
	prevCap := computerUseRefreshCapture
	computerUseAfterActionRefresh = cuRefreshForeground
	computerUseRefreshCapture = func(string) (computerUseCapture, error) {
		return computerUseCapture{}, errors.New("capture failed")
	}
	dir := t.TempDir()
	prevDir := computerUseExperienceDirForTest
	computerUseExperienceDirForTest = dir
	globalComputerUse.mu.Lock()
	prevPending := globalComputerUse.pendingModelImage
	prevLast := globalComputerUse.lastModelImage
	globalComputerUse.pendingModelImage = "old-pending"
	globalComputerUse.lastModelImage = "old-vision"
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		computerUseAfterActionRefresh = prevHook
		computerUseRefreshCapture = prevCap
		computerUseExperienceDirForTest = prevDir
		computeruse.SetExperienceDir("")
		globalComputerUse.mu.Lock()
		globalComputerUse.pendingModelImage = prevPending
		globalComputerUse.lastModelImage = prevLast
		globalComputerUse.mu.Unlock()
	})
	text := cuFinishAction("clicked e1")
	globalComputerUse.mu.Lock()
	pending := globalComputerUse.pendingModelImage
	last := globalComputerUse.lastModelImage
	globalComputerUse.mu.Unlock()
	if pending != "" || last != "" {
		t.Fatalf("failed refresh kept images pending=%q last=%q", pending, last)
	}
	if !strings.Contains(text, "refresh failed") || sess.LabelsCurrent() || sess.RefsValid() {
		t.Fatalf("current=%v refs=%v text=%s", sess.LabelsCurrent(), sess.RefsValid(), text)
	}
	msg := cuHandleKey(map[string]interface{}{"keys": "enter"})
	if in.keys != 0 || !strings.Contains(msg, "call computer_observe again") {
		t.Fatalf("keys=%d msg=%s", in.keys, msg)
	}
}

func TestTextRefreshListsOCRRefs(t *testing.T) {
	resetComputerUseRuntimeForTest(t)
	globalComputerUse.mu.Lock()
	prevPending := globalComputerUse.pendingModelImage
	prevLast := globalComputerUse.lastModelImage
	globalComputerUse.turnVisionKnown = true
	globalComputerUse.turnVision = false
	globalComputerUse.pendingModelImage = "old-pending"
	globalComputerUse.lastModelImage = "old-vision"
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.turnVisionKnown = false
		globalComputerUse.turnVision = false
		globalComputerUse.pendingModelImage = prevPending
		globalComputerUse.lastModelImage = prevLast
		globalComputerUse.mu.Unlock()
	})
	ocr := &labelOCR{lines: []taskengine.OCRResult{
		{Text: "龙军", BBox: [4]int{10, 10, 40, 20}},
		{Text: "立即支付", BBox: [4]int{0, 0, 0, 0}},
	}}
	installLabelOCR(t, ocr, nil)
	seedComputerUseObserve(t, "", "页面")
	prev := computerUseRefreshCapture
	computerUseRefreshCapture = func(string) (computerUseCapture, error) {
		return computerUseCapture{
			PNG:    "crop",
			Meta:   computeruse.ScreenMeta{CropTitle: "蓝信", Width: 80, Height: 40, ScaleFactor: 1},
			Width:  80,
			Height: 40,
		}, nil
	}
	t.Cleanup(func() { computerUseRefreshCapture = prev })
	computerUseForegroundTitleFn = func() string { return "蓝信" }
	computerUseFocusWindowFn = func(string) error {
		t.Fatal("named refresh must not focus another window")
		return nil
	}
	resetComputerUseExperienceTrace()
	text := cuRefreshForeground("clicked", "蓝信")
	globalComputerUse.mu.Lock()
	pending := globalComputerUse.pendingModelImage
	last := globalComputerUse.lastModelImage
	globalComputerUse.mu.Unlock()
	if pending != "" || last != "" {
		t.Fatalf("text refresh kept images pending=%q last=%q", pending, last)
	}
	if _, _, observes := computerUseTraceSnapshot(); observes != 0 {
		t.Fatalf("text refresh counted as computer_observe: %d", observes)
	}
	if !strings.Contains(text, "ocr_refs=1") || !strings.Contains(text, `"龙军"`) || strings.Contains(text, "elements (") {
		t.Fatalf("refresh text: %s", text)
	}
	if !cuSession().RefsValid() || !cuSession().LabelsCurrent() {
		t.Fatal("boxed OCR did not publish refs")
	}
	ocr.lines = []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{0, 0, 0, 0}}}
	again := cuRefreshForeground("clicked", "蓝信")
	if cuSession().RefsValid() || cuSession().LabelsCurrent() || !strings.Contains(again, "OCR returned no text") || strings.Contains(again, "elements (") {
		t.Fatalf("zero-box refresh refs=%v current=%v text=%s", cuSession().RefsValid(), cuSession().LabelsCurrent(), again)
	}
}

func TestMonitorFallbackIsNotAClickTarget(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	setComputerUseTurnVision(true)
	globalComputerUse.mu.Lock()
	prevPending := globalComputerUse.pendingModelImage
	prevLast := globalComputerUse.lastModelImage
	globalComputerUse.pendingModelImage = "stale-image"
	globalComputerUse.lastModelImage = "stale-image"
	globalComputerUse.mu.Unlock()
	groundCalls := 0
	prevGround := computerUseGrounderFn
	computerUseGrounderFn = func() (corelib.MaclawLLMConfig, bool) {
		groundCalls++
		return corelib.MaclawLLMConfig{}, false
	}
	t.Cleanup(func() {
		computerUseGrounderFn = prevGround
		globalComputerUse.mu.Lock()
		globalComputerUse.turnVisionKnown = false
		globalComputerUse.turnVision = false
		globalComputerUse.pendingModelImage = prevPending
		globalComputerUse.lastModelImage = prevLast
		globalComputerUse.mu.Unlock()
	})
	text := cuDescribeRefresh("clicked", computerUseCapture{
		PNG:    "monitor",
		Meta:   computeruse.ScreenMeta{ScreenIndex: 0, Width: 2560, Height: 1440},
		Width:  2560,
		Height: 1440,
	})
	if !strings.Contains(text, "not the foreground window") || !strings.Contains(text, "call computer_observe again") || strings.Contains(text, "click x,y") || strings.Contains(text, "screenshot: attached") {
		t.Fatalf("monitor refresh: %s", text)
	}
	globalComputerUse.mu.Lock()
	pending := globalComputerUse.pendingModelImage
	last := globalComputerUse.lastModelImage
	globalComputerUse.mu.Unlock()
	if pending != "" || last != "" || sess.LabelsCurrent() {
		t.Fatalf("pending=%q last=%q current=%v", pending, last, sess.LabelsCurrent())
	}
	msg := cuHandleClick(map[string]interface{}{"x": 30, "y": 20})
	if in.clicks != 0 || !strings.Contains(msg, "call computer_observe again") {
		t.Fatalf("clicks=%d msg=%s", in.clicks, msg)
	}
	named := cuHandleClick(map[string]interface{}{"target": "发送"})
	if groundCalls != 0 || in.clicks != 0 || !strings.Contains(named, "call computer_observe again") {
		t.Fatalf("ground=%d clicks=%d msg=%s", groundCalls, in.clicks, named)
	}
}

func TestUnlabeledCropRefusesScrollAndDrag(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	text := cuDescribeRefresh("clicked", computerUseCapture{
		PNG:    "monitor",
		Meta:   computeruse.ScreenMeta{ScreenIndex: 0, Width: 2560, Height: 1440},
		Width:  2560,
		Height: 1440,
	})
	if sess.LabelsCurrent() || !strings.Contains(text, "scroll, or drag") {
		t.Fatalf("monitor refresh current=%v text=%s", sess.LabelsCurrent(), text)
	}
	scroll := cuHandleScroll(map[string]interface{}{"x": 30, "y": 20, "delta_y": 3})
	drag := cuHandleDrag(map[string]interface{}{"from_x": 10, "from_y": 10, "to_x": 80, "to_y": 40})
	if in.scrolls != 0 || in.drags != 0 {
		t.Fatalf("scrolls=%d drags=%d scroll=%s drag=%s", in.scrolls, in.drags, scroll, drag)
	}
	if !strings.Contains(scroll, "call computer_observe again") || !strings.Contains(drag, "call computer_observe again") {
		t.Fatalf("scroll=%s drag=%s", scroll, drag)
	}
	sess.SetAllowPixelClick(false)
	center := cuHandleScroll(map[string]interface{}{})
	if in.scrolls != 0 || !strings.Contains(center, "call computer_observe again") {
		t.Fatalf("center scrolls=%d msg=%s", in.scrolls, center)
	}
}

func TestScrollOnCurrentCropDoesNotWaitForLabels(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	reads := 0
	computerUseCropLabelRead = func(string) ([]taskengine.OCRResult, bool) {
		reads++
		return []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}}}, true
	}
	center := cuHandleScroll(map[string]interface{}{})
	if in.scrolls != 1 || reads != 0 || strings.Contains(center, "call computer_observe again") {
		t.Fatalf("center scrolls=%d reads=%d msg=%s", in.scrolls, reads, center)
	}
	sess.SetAllowPixelClick(true)
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	again := cuHandleScroll(map[string]interface{}{"x": 30, "y": 20, "delta_y": -2})
	if in.scrolls != 2 || reads != 0 || strings.Contains(again, "call computer_observe again") || sess.LabelsCurrent() {
		t.Fatalf("restorable scrolls=%d reads=%d current=%v msg=%s", in.scrolls, reads, sess.LabelsCurrent(), again)
	}
}

func TestDragPayEndpointHandsOff(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	ocr := &labelOCR{lines: []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}}}}
	installLabelOCR(t, ocr, nil)
	pay := cuHandleDrag(map[string]interface{}{"from_x": 30, "from_y": 20, "to_x": 180, "to_y": 80})
	if in.drags != 0 || ocr.calls != 0 || !strings.Contains(pay, "支付") || strings.Contains(pay, "control labels are from before") {
		t.Fatalf("drags=%d calls=%d msg=%s", in.drags, ocr.calls, pay)
	}
	ref := cuHandleDrag(map[string]interface{}{"from_ref": "e0", "to_ref": "e1"})
	if in.drags != 0 || ocr.calls != 0 || !strings.Contains(ref, "支付") {
		t.Fatalf("drags=%d calls=%d msg=%s", in.drags, ocr.calls, ref)
	}
	safe := cuHandleDrag(map[string]interface{}{"from_x": 180, "from_y": 80, "to_x": 190, "to_y": 90})
	if in.drags != 1 || ocr.calls != 0 || strings.Contains(safe, "blocked") {
		t.Fatalf("drags=%d calls=%d msg=%s", in.drags, ocr.calls, safe)
	}
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	after := cuHandleDrag(map[string]interface{}{"from_x": 30, "from_y": 20, "to_x": 180, "to_y": 80})
	if in.drags != 1 || ocr.calls != 1 || !strings.Contains(after, "支付") {
		t.Fatalf("drags=%d calls=%d msg=%s", in.drags, ocr.calls, after)
	}
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	ocr.lines = []taskengine.OCRResult{{Text: "龙军", BBox: [4]int{10, 10, 20, 10}}}
	moved := cuHandleDrag(map[string]interface{}{"from_x": 180, "from_y": 80, "to_x": 190, "to_y": 90})
	if in.drags != 2 || ocr.calls != 2 || strings.Contains(moved, "blocked") || strings.Contains(moved, "control labels are from before") {
		t.Fatalf("drags=%d calls=%d msg=%s", in.drags, ocr.calls, moved)
	}
}

func TestRaiseBackgroundWindowBeforeAction(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: "蓝信"}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	last := sess.LastObserve()
	last.Elements[0].Window = "蓝信"
	in := installCountingComputerUse(t, sess)
	noteComputerUseOperatedWindow(42)
	title := "记事本"
	var id uint64 = 99
	var focused []string
	computerUseForegroundTitleFn = func() string { return title }
	computerUseForegroundIDFn = func() uint64 { return id }
	computerUseFocusWindowFn = func(got string) error {
		focused = append(focused, got)
		title = "蓝信"
		id = 42
		return nil
	}
	computerUseNamedWindowFn = func(hint string) (string, bool) { return hint, true }
	computerUsePolicyWindowHook = func(int, int) string { return "蓝信" }
	msg := cuHandleClick(map[string]interface{}{"ref": "e0"})
	if in.clicks != 1 || len(focused) != 1 || focused[0] != "蓝信" || !strings.Contains(msg, "raised") || strings.Contains(msg, "foreground window changed") {
		t.Fatalf("clicks=%d focused=%v msg=%s", in.clicks, focused, msg)
	}

	sess = computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: "蓝信"}, nil, nil, nil, "")
	installCountingComputerUse(t, sess)
	noteComputerUseOperatedWindow(42)
	title = "记事本"
	id = 42
	focused = nil
	computerUseForegroundTitleFn = func() string { return title }
	computerUseForegroundIDFn = func() uint64 { return id }
	computerUseFocusWindowFn = func(got string) error {
		focused = append(focused, got)
		title = "蓝信"
		id = 7
		return nil
	}
	computerUseNamedWindowFn = func(hint string) (string, bool) { return hint, true }
	// The halo followed a click onto the covering window. That is not
	// computer_focus, so the operated crop still comes forward.
	covered := cuRequireSameWindow(sess)
	if covered != "" || len(focused) != 1 || focused[0] != "蓝信" || computerUseRaisedNote != "蓝信" {
		t.Fatalf("covered focused=%v note=%q msg=%s", focused, computerUseRaisedNote, covered)
	}

	sess = computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: "蓝信"}, nil, nil, nil, "")
	installCountingComputerUse(t, sess)
	computerUseExplicitFocusTitle = "记事本"
	title = "记事本"
	id = 42
	focused = nil
	computerUseForegroundTitleFn = func() string { return title }
	computerUseForegroundIDFn = func() uint64 { return id }
	computerUseFocusWindowFn = func(got string) error {
		focused = append(focused, got)
		return nil
	}
	computerUseNamedWindowFn = func(hint string) (string, bool) { return hint, true }
	held := cuRequireSameWindow(sess)
	if len(focused) != 0 || !strings.Contains(held, "foreground window changed") || sess.RefsValid() {
		t.Fatalf("held focused=%v refs=%v msg=%s", focused, sess.RefsValid(), held)
	}

	sess = computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: "蓝信"}, nil, nil, nil, "")
	installCountingComputerUse(t, sess)
	noteComputerUseOperatedWindow(42)
	title = "User Account Control"
	id = 99
	focused = nil
	computerUseForegroundTitleFn = func() string { return title }
	computerUseForegroundIDFn = func() uint64 { return id }
	computerUseFocusWindowFn = func(got string) error {
		focused = append(focused, got)
		return nil
	}
	computerUseNamedWindowFn = func(hint string) (string, bool) { return hint, true }
	blocked := cuRequireSameWindow(sess)
	if len(focused) != 0 || !strings.Contains(blocked, "foreground window changed") {
		t.Fatalf("focused=%v msg=%s", focused, blocked)
	}
}

func TestReassertRaisesWhenTheCoverReturns(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: "蓝信"}, nil, nil, nil, "")
	installCountingComputerUse(t, sess)
	title := "记事本"
	focused := 0
	computerUseForegroundTitleFn = func() string { return title }
	computerUseFocusWindowFn = func(string) error {
		focused++
		title = "蓝信"
		return nil
	}
	computerUseNamedWindowFn = func(hint string) (string, bool) { return hint, true }
	if msg := cuRequireSameWindow(sess); msg != "" || focused != 1 || computerUseRaisedNote != "蓝信" {
		t.Fatalf("first focused=%d note=%q msg=%s", focused, computerUseRaisedNote, msg)
	}
	title = "记事本"
	if msg := cuReassertOperatedWindow(sess); msg != "" || focused != 2 || computerUseRaisedNote != "蓝信" {
		t.Fatalf("reassert focused=%d note=%q msg=%s", focused, computerUseRaisedNote, msg)
	}
	title = "记事本"
	computerUseFocusWindowFn = func(string) error { return errors.New("stayed back") }
	msg := cuReassertOperatedWindow(sess)
	if computerUseRaisedNote != "" || !strings.Contains(msg, "foreground window changed") {
		t.Fatalf("note=%q msg=%s", computerUseRaisedNote, msg)
	}
}

func TestCoveringClickKeepsTheOperatedWindow(t *testing.T) {
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 80, ScaleFactor: 1, CropTitle: "蓝信"}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	last := sess.LastObserve()
	last.Elements[0].Window = "蓝信"
	in := installCountingComputerUse(t, sess)
	noteComputerUseOperatedWindow(42)
	computerUseForegroundTitleFn = func() string { return "蓝信" }
	computerUseForegroundIDFn = func() uint64 { return 42 }
	computerUseFocusWindowFn = func(string) error {
		t.Fatal("click on the operated window must not raise")
		return nil
	}
	computerUseWindowAtPointFn = func(int, int) (uint64, string) { return 99, "Grok" }
	computerUsePolicyWindowHook = func(int, int) string { return "蓝信" }
	msg := cuHandleClick(map[string]interface{}{"ref": "e0"})
	if in.clicks != 1 || computerUseHaloTarget() != 42 || strings.Contains(msg, "foreground window changed") {
		t.Fatalf("clicks=%d halo=%d msg=%s", in.clicks, computerUseHaloTarget(), msg)
	}

	title := "记事本"
	var focused []string
	var captured []string
	computerUseForegroundTitleFn = func() string { return title }
	computerUseFocusWindowFn = func(got string) error {
		focused = append(focused, got)
		title = "蓝信"
		return nil
	}
	prevRefresh := computerUseAfterActionRefresh
	prevCap := computerUseRefreshCapture
	computerUseAfterActionRefresh = cuRefreshForeground
	computerUseRefreshCapture = func(window string) (computerUseCapture, error) {
		captured = append(captured, window)
		return computerUseCapture{
			PNG:    "crop",
			Meta:   computeruse.ScreenMeta{CropTitle: window, Width: 80, Height: 40, ScaleFactor: 1},
			Width:  80,
			Height: 40,
		}, nil
	}
	t.Cleanup(func() {
		computerUseAfterActionRefresh = prevRefresh
		computerUseRefreshCapture = prevCap
	})
	globalComputerUse.mu.Lock()
	globalComputerUse.turnVisionKnown = true
	globalComputerUse.turnVision = false
	prevOCR := globalComputerUse.ocr
	globalComputerUse.ocr = nil
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.turnVisionKnown = false
		globalComputerUse.turnVision = false
		globalComputerUse.ocr = prevOCR
		globalComputerUse.mu.Unlock()
	})
	computerUseExplicitFocusTitle = "蓝信"
	text := cuFinishAction("clicked")
	if len(focused) != 1 || focused[0] != "蓝信" || len(captured) != 1 || captured[0] != "蓝信" || !strings.Contains(text, "raised") || !strings.Contains(text, `crop of "蓝信"`) || computerUseExplicitFocusTitle != "" {
		t.Fatalf("focused=%v captured=%v hold=%q text=%s", focused, captured, computerUseExplicitFocusTitle, text)
	}

	title = "记事本"
	focused = nil
	computerUseFocusWindowFn = func(string) error { return errors.New("stayed back") }
	called := false
	computerUseRefreshCapture = func(string) (computerUseCapture, error) {
		called = true
		return computerUseCapture{}, nil
	}
	fail := cuFinishAction("clicked")
	if called || !strings.Contains(fail, "refresh failed") {
		t.Fatalf("called=%v text=%s", called, fail)
	}
	obs := computerUseObserveCaption(0, "", false, true, false)
	if obs.OK || !strings.Contains(obs.Message, "could not bring") || !strings.Contains(obs.Message, "蓝信") {
		t.Fatalf("observe ok=%v msg=%s", obs.OK, obs.Message)
	}
	if crop := cuSession().LastObserve(); crop == nil || crop.Meta.CropTitle != "蓝信" {
		t.Fatalf("crop changed: %+v", crop)
	}
}

func TestFocusRefusesSecurityWindow(t *testing.T) {
	cfg := computeruse.DefaultConfig()
	cfg.MaxSteps = 1
	sess := computeruse.NewSession(cfg)
	installCountingComputerUse(t, sess)
	computerUseNamedWindowFn = func(hint string) (string, bool) {
		if hint == "安全" {
			return "Windows 安全中心", true
		}
		return "", false
	}
	blocked := cuHandleFocus(map[string]interface{}{"window": "User Account Control"})
	resolved := cuHandleFocus(map[string]interface{}{"window": "安全"})
	if !strings.Contains(blocked, "security") || !strings.Contains(resolved, "security") {
		t.Fatalf("blocked=%s resolved=%s", blocked, resolved)
	}
	ok := cuHandleFocus(map[string]interface{}{"window": "cu-focus-policy-test"})
	if strings.Contains(ok, "security") || strings.Contains(ok, "max steps") || !strings.Contains(ok, "no visible window") {
		t.Fatalf("msg=%s", ok)
	}
}

func TestTypePayRefDoesNotFocusClick(t *testing.T) {
	fg := computerUseTestForeground(t)
	cfg := computeruse.DefaultConfig()
	cfg.MaxSteps = 1
	sess := computeruse.NewSession(cfg)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 240, Height: 80, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
		{Name: "搜索", BBox: [4]int{160, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	pay := cuHandleType(map[string]interface{}{"ref": "e0", "text": "确认购买"})
	if in.clicks != 0 || in.types != 0 || !strings.Contains(pay, "支付") || strings.Contains(pay, "确认购买") {
		t.Fatalf("clicks=%d types=%d msg=%s", in.clicks, in.types, pay)
	}
	ok := cuHandleType(map[string]interface{}{"ref": "e1", "text": "龙军"})
	if in.clicks != 1 || in.types != 1 || strings.Contains(ok, "blocked") {
		t.Fatalf("clicks=%d types=%d msg=%s", in.clicks, in.types, ok)
	}

	cfg = computeruse.DefaultConfig()
	cfg.MaxSteps = 1
	sess = computeruse.NewSession(cfg)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 240, Height: 80, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 80, 40}, Source: "accessibility", Patterns: []string{"value"}},
		{Name: "搜索", BBox: [4]int{160, 10, 40, 20}, Source: "accessibility", Patterns: []string{"value"}},
	}, []taskengine.OCRResult{
		{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}},
	}, "")
	in = installCountingComputerUse(t, sess)
	bridge := &countingTypeBridge{}
	globalComputerUse.mu.Lock()
	globalComputerUse.bridge = bridge
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.bridge = nil
		globalComputerUse.mu.Unlock()
	})
	under := cuHandleType(map[string]interface{}{"ref": "e0", "text": "确认购买"})
	if in.clicks != 0 || in.types != 0 || bridge.types != 0 || !strings.Contains(under, "支付") || strings.Contains(under, "确认购买") {
		t.Fatalf("clicks=%d types=%d semantic=%d msg=%s", in.clicks, in.types, bridge.types, under)
	}
	semanticOK := cuHandleType(map[string]interface{}{"ref": "e1", "text": "龙军"})
	if in.clicks != 0 || in.types != 0 || bridge.types != 1 || strings.Contains(semanticOK, "blocked") {
		t.Fatalf("clicks=%d types=%d semantic=%d msg=%s", in.clicks, in.types, bridge.types, semanticOK)
	}
}

func TestSelectPayRefDoesNotClick(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
		{Name: "龙军", BBox: [4]int{60, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	pay := cuHandleSelect(map[string]interface{}{"ref": "e0"})
	if in.clicks != 0 || !strings.Contains(pay, "支付") || strings.Contains(pay, "selected") {
		t.Fatalf("clicks=%d msg=%s", in.clicks, pay)
	}
	ok := cuHandleSelect(map[string]interface{}{"ref": "e1"})
	if in.clicks != 1 || strings.Contains(ok, "blocked") {
		t.Fatalf("clicks=%d msg=%s", in.clicks, ok)
	}
}

func TestPointerActionsHonorElementWindow(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "确认", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
		{Name: "列表", BBox: [4]int{60, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	last := sess.LastObserve()
	last.Elements[0].Window = "User Account Control"
	last.Elements[1].Window = fg
	in := installCountingComputerUse(t, sess)
	scroll := cuHandleScroll(map[string]interface{}{"ref": "e0"})
	drag := cuHandleDrag(map[string]interface{}{"from_ref": "e1", "to_ref": "e0"})
	sel := cuHandleSelect(map[string]interface{}{"ref": "e0"})
	typed := cuHandleType(map[string]interface{}{"ref": "e0", "text": "龙军"})
	if in.scrolls != 0 || in.drags != 0 || in.clicks != 0 || in.types != 0 {
		t.Fatalf("scrolls=%d drags=%d clicks=%d types=%d", in.scrolls, in.drags, in.clicks, in.types)
	}
	for _, msg := range []string{scroll, drag, sel, typed} {
		if !strings.Contains(msg, "security") {
			t.Fatalf("msg=%s", msg)
		}
	}
	ok := cuHandleScroll(map[string]interface{}{"ref": "e1"})
	if in.scrolls != 1 || strings.Contains(ok, "blocked") {
		t.Fatalf("scrolls=%d msg=%s", in.scrolls, ok)
	}
}

func TestPointerActionsRefuseLiveSecurityWindow(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.CommitObserve(computeruse.ScreenMeta{Width: 240, Height: 80, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "列表", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
		{Name: "确认", BBox: [4]int{160, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	last := sess.LastObserve()
	last.Elements[0].Window = fg
	last.Elements[1].Window = fg
	in := installCountingComputerUse(t, sess)
	computerUsePolicyWindowHook = func(x, _ int) string {
		if x >= 100 {
			return "User Account Control"
		}
		return fg
	}
	scroll := cuHandleScroll(map[string]interface{}{"ref": "e1"})
	drag := cuHandleDrag(map[string]interface{}{"from_ref": "e0", "to_ref": "e1"})
	click := cuHandleClick(map[string]interface{}{"ref": "e1"})
	sel := cuHandleSelect(map[string]interface{}{"ref": "e1"})
	typed := cuHandleType(map[string]interface{}{"ref": "e1", "text": "龙军"})
	if in.scrolls != 0 || in.drags != 0 || in.clicks != 0 || in.types != 0 {
		t.Fatalf("scrolls=%d drags=%d clicks=%d types=%d", in.scrolls, in.drags, in.clicks, in.types)
	}
	for _, msg := range []string{scroll, drag, click, sel, typed} {
		if !strings.Contains(msg, "security") {
			t.Fatalf("msg=%s", msg)
		}
	}
	ok := cuHandleScroll(map[string]interface{}{"ref": "e0"})
	if in.scrolls != 1 || strings.Contains(ok, "blocked") {
		t.Fatalf("scrolls=%d msg=%s", in.scrolls, ok)
	}
}

// countingTypeBridge records a semantic type without moving the pointer.
type countingTypeBridge struct {
	types int
}

func (b *countingTypeBridge) EnumElements(string) ([]accessibility.Element, error) {
	return nil, nil
}
func (b *countingTypeBridge) FindElement(string, string, string) (*accessibility.Element, error) {
	return nil, nil
}
func (b *countingTypeBridge) ClickElement(*accessibility.Element) error { return nil }
func (b *countingTypeBridge) TypeInElement(*accessibility.Element, string) error {
	b.types++
	return nil
}
func (b *countingTypeBridge) SelectElement(*accessibility.Element) error { return nil }
func (b *countingTypeBridge) ExpandElement(*accessibility.Element) error { return nil }
func (b *countingTypeBridge) ScrollElementIntoView(*accessibility.Element) error {
	return nil
}
func (b *countingTypeBridge) FocusElement(*accessibility.Element) error { return nil }
func (b *countingTypeBridge) GetValue(*accessibility.Element) (string, error) {
	return "", nil
}
func (b *countingTypeBridge) Close() {}

func TestTypeRefChecksLiveWindowBeforeFocus(t *testing.T) {
	fg := computerUseTestForeground(t)
	cfg := computeruse.DefaultConfig()
	cfg.MaxSteps = 1
	sess := computeruse.NewSession(cfg)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 240, Height: 80, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
		{Name: "姓名", BBox: [4]int{160, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	last := sess.LastObserve()
	last.Elements[0].Window = fg
	last.Elements[1].Window = fg
	in := installCountingComputerUse(t, sess)
	computerUsePolicyWindowHook = func(x, _ int) string {
		if x >= 100 {
			return "User Account Control"
		}
		return fg
	}
	blocked := cuHandleType(map[string]interface{}{"ref": "e1", "text": "龙军"})
	if in.clicks != 0 || in.types != 0 || !strings.Contains(blocked, "security") {
		t.Fatalf("clicks=%d types=%d msg=%s", in.clicks, in.types, blocked)
	}
	ok := cuHandleType(map[string]interface{}{"ref": "e0", "text": "龙军"})
	if in.clicks != 1 || in.types != 1 || strings.Contains(ok, "blocked") {
		t.Fatalf("clicks=%d types=%d msg=%s", in.clicks, in.types, ok)
	}

	cfg = computeruse.DefaultConfig()
	cfg.MaxSteps = 1
	sess = computeruse.NewSession(cfg)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 240, Height: 80, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{10, 10, 40, 20}, Source: "accessibility", Patterns: []string{"value"}},
		{Name: "姓名", BBox: [4]int{160, 10, 40, 20}, Source: "accessibility", Patterns: []string{"value"}},
	}, nil, "")
	last = sess.LastObserve()
	last.Elements[0].Window = fg
	last.Elements[1].Window = fg
	in = installCountingComputerUse(t, sess)
	bridge := &countingTypeBridge{}
	globalComputerUse.mu.Lock()
	globalComputerUse.bridge = bridge
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.bridge = nil
		globalComputerUse.mu.Unlock()
	})
	computerUsePolicyWindowHook = func(x, _ int) string {
		if x >= 100 {
			return "User Account Control"
		}
		return fg
	}
	semanticBlocked := cuHandleType(map[string]interface{}{"ref": "e1", "text": "龙军"})
	last.Elements[0].Window = "User Account Control"
	stored := cuHandleType(map[string]interface{}{"ref": "e0", "text": "龙军"})
	if in.clicks != 0 || in.types != 0 || bridge.types != 0 {
		t.Fatalf("clicks=%d types=%d semantic=%d live=%s stored=%s", in.clicks, in.types, bridge.types, semanticBlocked, stored)
	}
	if !strings.Contains(semanticBlocked, "security") || !strings.Contains(stored, "security") {
		t.Fatalf("live=%s stored=%s", semanticBlocked, stored)
	}
	last.Elements[0].Window = fg
	semanticOK := cuHandleType(map[string]interface{}{"ref": "e0", "text": "龙军"})
	if in.clicks != 0 || in.types != 0 || bridge.types != 1 || strings.Contains(semanticOK, "blocked") {
		t.Fatalf("clicks=%d types=%d semantic=%d msg=%s", in.clicks, in.types, bridge.types, semanticOK)
	}
}

func TestPayTargetHandsOffBeforeGrounding(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	sess.ApplyRefreshCapture(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, "crop")
	groundCalls := 0
	prevGround := computerUseGrounderFn
	computerUseGrounderFn = func() (corelib.MaclawLLMConfig, bool) {
		groundCalls++
		return corelib.MaclawLLMConfig{}, false
	}
	t.Cleanup(func() { computerUseGrounderFn = prevGround })
	msg := cuHandleClick(map[string]interface{}{"target": "立即支付"})
	if groundCalls != 0 || in.clicks != 0 || !strings.Contains(msg, "支付") || strings.Contains(msg, "control labels are from before") {
		t.Fatalf("ground=%d clicks=%d msg=%s", groundCalls, in.clicks, msg)
	}
}

func TestVisionRefreshOverlapsLabelOCR(t *testing.T) {
	fg := computerUseTestForeground(t)
	sess := computeruse.NewSession(computeruse.DefaultConfig())
	sess.SetAllowPixelClick(true)
	sess.CommitObserve(computeruse.ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1, CropTitle: fg}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, nil, "")
	in := installCountingComputerUse(t, sess)
	setComputerUseTurnVision(true)
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	var calls int
	var callsMu sync.Mutex
	cropPNG := tinyComputerUsePNG(t, 200, 100)
	computerUseCropLabelRead = func(png string) ([]taskengine.OCRResult, bool) {
		if png != cropPNG {
			return nil, false
		}
		callsMu.Lock()
		calls++
		n := calls
		callsMu.Unlock()
		once.Do(func() { close(started) })
		<-release
		if n != 1 {
			return nil, false
		}
		return []taskengine.OCRResult{{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}}}, true
	}
	prevCap := computerUseRefreshCapture
	computerUseRefreshCapture = func(string) (computerUseCapture, error) {
		return computerUseCapture{
			PNG:    cropPNG,
			Meta:   computeruse.ScreenMeta{CropTitle: fg, Width: 200, Height: 100, ScaleFactor: 1},
			Width:  200,
			Height: 100,
		}, nil
	}
	t.Cleanup(func() {
		computerUseRefreshCapture = prevCap
		select {
		case <-release:
		default:
			close(release)
		}
		computerUseCropLabelRead = nil
		globalComputerUse.mu.Lock()
		globalComputerUse.turnVisionKnown = false
		globalComputerUse.turnVision = false
		globalComputerUse.pendingModelImage = ""
		globalComputerUse.mu.Unlock()
	})
	text := cuRefreshForeground("clicked", fg)
	if !strings.Contains(text, "window just operated") {
		t.Fatalf("refresh: %s", text)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("label read did not start during the refresh")
	}
	callsMu.Lock()
	got := calls
	callsMu.Unlock()
	if got != 1 {
		t.Fatalf("reads during refresh: %d", got)
	}
	close(release)
	msg := cuHandleClick(map[string]interface{}{"x": 30, "y": 20})
	callsMu.Lock()
	got = calls
	callsMu.Unlock()
	if got != 1 || in.clicks != 0 || !strings.Contains(msg, "支付") {
		t.Fatalf("reads=%d clicks=%d msg=%s", got, in.clicks, msg)
	}
}
