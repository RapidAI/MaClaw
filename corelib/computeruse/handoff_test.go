package computeruse

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestDesktopHandoffWaitsUntilThePersonFinishes(t *testing.T) {
	t.Cleanup(func() {
		SetDesktopHandoffNotifier(nil)
		_ = ResolveDesktopHandoff(false)
	})
	var got DesktopHandoffNotice
	SetDesktopHandoffNotifier(func(n DesktopHandoffNotice) {
		if n.Active {
			got = n
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- OfferDesktopHandoff("secret", "请输入支付密码")
	}()
	time.Sleep(30 * time.Millisecond)
	if !got.Active || got.Reason != "secret" || got.Title != "请输入支付密码" {
		t.Fatalf("notice %+v", got)
	}
	if strings.Contains(got.Title, "123456") {
		t.Fatal(got.Title)
	}
	if err := ResolveDesktopHandoff(true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrDesktopHandoffDone) || strings.Contains(err.Error(), "123456") {
			t.Fatalf("done err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handoff did not return")
	}
}

func TestDesktopHandoffCancelDoesNotResumeWhenSuppressed(t *testing.T) {
	t.Cleanup(func() {
		SetDesktopHandoffNotifier(nil)
		_ = ResolveDesktopHandoff(false)
	})
	var last DesktopHandoffNotice
	SetDesktopHandoffNotifier(func(n DesktopHandoffNotice) { last = n })
	done := make(chan error, 1)
	go func() {
		done <- OfferDesktopHandoff("checkout", "立即支付")
	}()
	time.Sleep(30 * time.Millisecond)
	SetDesktopHandoffResume(false)
	if err := ResolveDesktopHandoff(false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrDesktopHandoffCancelled) {
			t.Fatalf("cancel err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not return")
	}
	time.Sleep(20 * time.Millisecond)
	if last.Active || last.Resume {
		t.Fatalf("end notice %+v", last)
	}
}

func TestDesktopHandoffOffDoesNotWait(t *testing.T) {
	SetDesktopHandoffNotifier(nil)
	if err := OfferDesktopHandoff("secret", "密码"); !errors.Is(err, ErrDesktopHandoffOff) {
		t.Fatal(err)
	}
	if DesktopHandoffEnabled() {
		t.Fatal("notifier nil must disable the wait")
	}
}

func TestDesktopHandoffTimeout(t *testing.T) {
	old := desktopHandoffWaitCap
	desktopHandoffWaitCap = 40 * time.Millisecond
	t.Cleanup(func() {
		desktopHandoffWaitCap = old
		SetDesktopHandoffNotifier(nil)
		_ = ResolveDesktopHandoff(false)
	})
	SetDesktopHandoffNotifier(func(DesktopHandoffNotice) {})
	err := OfferDesktopHandoff("secret", "验证码")
	if !errors.Is(err, ErrDesktopHandoffTimeout) {
		t.Fatal(err)
	}
}

func TestElementNameAtScreenPrefersTheSmallerControl(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
		{Name: "立即支付", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	if got := s.ElementNameAtScreen(30, 20); got != "立即支付" {
		t.Fatalf("pay hit %q", got)
	}
	if got := s.ElementNameAtScreen(180, 80); got != "页面" {
		t.Fatalf("page hit %q", got)
	}
	if got := s.ElementNameAtScreen(400, 400); got != "" {
		t.Fatalf("miss %q", got)
	}
}

func TestOCRTextAtScreenIgnoresAParagraph(t *testing.T) {
	s := NewSession(DefaultConfig())
	longLine := strings.Repeat("商品说明", 30) + "立即支付"
	s.CommitObserve(ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
	}, []taskengine.OCRResult{
		{Text: "立即支付", BBox: [4]int{10, 10, 40, 20}},
		{Text: longLine, BBox: [4]int{0, 0, 200, 100}},
	}, "")
	if got := s.OCRTextAtScreen(30, 20); got != "立即支付" {
		t.Fatalf("ocr %q", got)
	}
	if got := s.ElementNameAtScreen(30, 20); got != "页面" {
		t.Fatalf("element name must stay the control, got %q", got)
	}
	if got := s.OCRTextAtScreen(400, 400); got != "" {
		t.Fatalf("miss %q", got)
	}
}

func TestKeySurfaceLabelsStayInTheForegroundWindow(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 400, Height: 200, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "保存", BBox: [4]int{0, 0, 80, 40}},
		{Name: "立即支付", BBox: [4]int{200, 0, 80, 40}},
	}, []taskengine.OCRResult{
		{Text: "去支付", BBox: [4]int{210, 10, 40, 16}},
		{Text: "搜索", BBox: [4]int{10, 10, 30, 16}},
	}, "")
	s.mu.Lock()
	s.last.Elements[0].Window = "记事本"
	s.last.Elements[1].Window = "收银台"
	s.mu.Unlock()
	labels := strings.Join(s.KeySurfaceLabels("记事本"), "\n")
	if strings.Contains(labels, "立即支付") || strings.Contains(labels, "去支付") || strings.Contains(labels, "搜索") {
		t.Fatalf("other window or text inside a control leaked: %q", labels)
	}
	if !strings.Contains(labels, "保存") {
		t.Fatalf("foreground labels %q", labels)
	}
	s.NoteActuated("搜索", "记事本", "edit")
	s.CommitObserve(ScreenMeta{Width: 40, Height: 20, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "保存", BBox: [4]int{0, 0, 10, 10}},
	}, nil, "")
	if s.ActuatedName() != "搜索" {
		t.Fatal("observe must not forget the control the agent clicked")
	}
	if name, kind := s.ActuatedInWindow("收银台"); name != "" || kind != "" {
		t.Fatalf("other window still has the caret %q %q", name, kind)
	}
	if name, kind := s.ActuatedInWindow("无标题 - 记事本"); name != "搜索" || kind != "edit" {
		t.Fatalf("same window lost the caret %q %q", name, kind)
	}
	s.ResetControl()
	if s.ActuatedName() != "" {
		t.Fatal("reset must clear the actuated control")
	}
}

func TestKeySurfaceLabelsIgnoreChatTextInsideAPane(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 200, Height: 120, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "会话", BBox: [4]int{0, 0, 200, 100}},
	}, []taskengine.OCRResult{
		{Text: "支付", BBox: [4]int{20, 30, 24, 16}},
		{Text: "立即支付", BBox: [4]int{0, 100, 80, 20}},
	}, "")
	labels := strings.Join(s.KeySurfaceLabels(""), "\n")
	if strings.Contains(labels, "支付") && !strings.Contains(labels, "立即支付") {
		t.Fatalf("chat text became a pay label: %q", labels)
	}
	if strings.Contains(strings.ReplaceAll(labels, "立即支付", ""), "支付") {
		t.Fatalf("chat text leaked: %q", labels)
	}
	if !strings.Contains(labels, "立即支付") || !strings.Contains(labels, "会话") {
		t.Fatalf("labels %q", labels)
	}
}

func TestKeySurfaceLabelsSkipADifferentCrop(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: "收银台"}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{0, 0, 40, 20}},
	}, nil, "")
	if labels := s.KeySurfaceLabels("记事本"); labels != nil {
		t.Fatalf("crop %v", labels)
	}
	if labels := s.KeySurfaceLabels("收银台"); !strings.Contains(strings.Join(labels, " "), "立即支付") {
		t.Fatalf("same crop %v", labels)
	}
}

func TestKeySurfaceLabelsSplitSameAppWindows(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 400, Height: 200, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{0, 0, 80, 30}},
		{Name: "回复", BBox: [4]int{200, 0, 80, 30}},
	}, nil, "")
	s.mu.Lock()
	s.last.Elements[0].Window = "订单 - Google Chrome"
	s.last.Elements[1].Window = "收件箱 - Google Chrome"
	s.mu.Unlock()
	labels := strings.Join(s.KeySurfaceLabels("收件箱 - Google Chrome"), "\n")
	if strings.Contains(labels, "立即支付") {
		t.Fatalf("cashier leaked into the other chrome window: %q", labels)
	}
	if !strings.Contains(labels, "回复") {
		t.Fatalf("inbox labels %q", labels)
	}
	s.NoteActuated("搜索", "订单 - Google Chrome", "edit")
	if name, kind := s.ActuatedInWindow("收件箱 - Google Chrome"); name != "" || kind != "" {
		t.Fatalf("search caret followed the other chrome window %q %q", name, kind)
	}
	s.NoteActuated("支付密码", "", "edit")
	if name, kind := s.ActuatedInWindow("收件箱 - Google Chrome"); name != "支付密码" || kind != "edit" {
		t.Fatalf("unattributed caret must still apply, got %q %q", name, kind)
	}
}

func TestKeySurfaceLabelsFollowTheSameWindowID(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 400, Height: 80, ScaleFactor: 1, CropTitle: "订单 (1) - Google Chrome"}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{0, 0, 80, 30}},
		{Name: "回复", BBox: [4]int{200, 0, 80, 30}},
	}, nil, "")
	s.mu.Lock()
	s.last.Elements[0].Window = "订单 (1) - Google Chrome"
	s.last.Elements[0].WindowID = 7
	s.last.Elements[1].Window = "无标题 - 记事本"
	s.last.Elements[1].WindowID = 8
	s.mu.Unlock()
	// The cashier title gained a count. It is still window 7.
	labels := strings.Join(s.KeySurfaceLabelsFor("订单 (2) - Google Chrome", 7), "\n")
	if !strings.Contains(labels, "立即支付") || strings.Contains(labels, "回复") {
		t.Fatalf("same window lost its pay label: %q", labels)
	}
	// Two notepads can share a title. The other hwnd must not inherit the button.
	s.mu.Lock()
	s.last.Meta.CropTitle = "无标题 - 记事本"
	s.last.Elements[0].Window = "无标题 - 记事本"
	s.last.Elements[1].Window = "无标题 - 记事本"
	s.mu.Unlock()
	labels = strings.Join(s.KeySurfaceLabelsFor("无标题 - 记事本", 8), "\n")
	if strings.Contains(labels, "立即支付") || !strings.Contains(labels, "回复") {
		t.Fatalf("same title crossed windows: %q", labels)
	}
	s.NoteActuatedID("搜索", "订单 (1) - Google Chrome", "edit", 7)
	if name, kind := s.ActuatedInWindowID("订单 (2) - Google Chrome", 7); name != "搜索" || kind != "edit" {
		t.Fatalf("title change dropped the caret %q %q", name, kind)
	}
	if name, kind := s.ActuatedInWindowID("订单 (2) - Google Chrome", 9); name != "" || kind != "" {
		t.Fatalf("caret followed a different window %q %q", name, kind)
	}
	if name, kind := s.ActuatedInWindowID("无标题 - 记事本", 8); name != "" || kind != "" {
		t.Fatalf("caret followed the other notepad %q %q", name, kind)
	}
}

func TestActuatedForKeyDropsAStaleSearchExemption(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.NoteActuatedID("搜索", "搜索 - Google Chrome", "edit", 7)
	name, kind := s.ActuatedForKey("订单 - Google Chrome", 7)
	if name != "" || kind != "" {
		t.Fatalf("search exemption followed the cashier %q %q", name, kind)
	}
	if !s.ActuatedDocumentCurrent("搜索 - Google Chrome") {
		t.Fatal("the search page is still the recorded document")
	}
	s.NoteActuatedID("支付密码", "登录 - Google Chrome", "edit", 7)
	name, kind = s.ActuatedForKey("订单 - Google Chrome", 7)
	if name != "" || kind != "" {
		t.Fatalf("password caret followed the next page %q %q", name, kind)
	}
	s.NoteActuatedID("支付密码", "订单 (1) - Google Chrome", "edit", 7)
	name, kind = s.ActuatedForKey("订单 (2) - Google Chrome", 7)
	if name != "支付密码" || kind != "edit" {
		t.Fatalf("password caret must stay across a count change %q %q", name, kind)
	}
	s.NoteActuatedID("搜索", "收件箱 (1) - Google Chrome", "edit", 7)
	name, kind = s.ActuatedForKey("收件箱 (2) - Google Chrome", 7)
	if name != "搜索" || kind != "edit" {
		t.Fatalf("search exemption dropped on an unread count %q %q", name, kind)
	}
	s.NoteActuatedID("搜索", "记事本", "edit", 0)
	name, kind = s.ActuatedForKey("无标题 - 记事本", 0)
	if name != "搜索" || kind != "edit" {
		t.Fatalf("bare app name lost the caret %q %q", name, kind)
	}
}

func TestObserveDocumentStaleBlocksTheSameWindowOnly(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: "搜索 - Google Chrome"}, nil, []taskengine.UIElement{
		{Name: "搜索", BBox: [4]int{0, 0, 40, 20}},
	}, nil, "")
	s.mu.Lock()
	s.last.Elements[0].Window = "搜索 - Google Chrome"
	s.last.Elements[0].WindowID = 7
	s.mu.Unlock()
	if crop, stale := s.ObserveDocumentStale("订单 - Google Chrome", 7); !stale || crop != "搜索 - Google Chrome" {
		t.Fatalf("navigation stale=%v crop=%q", stale, crop)
	}
	if _, stale := s.ObserveDocumentStale("收件箱 - Google Chrome", 8); stale {
		t.Fatal("another window must keep its own keys")
	}
	s.CommitObserve(ScreenMeta{Width: 200, Height: 40, ScaleFactor: 1, CropTitle: "订单 - Google Chrome"}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{0, 0, 40, 20}},
		{Name: "回复", BBox: [4]int{80, 0, 40, 20}},
	}, nil, "")
	s.mu.Lock()
	s.last.Elements[0].Window = "订单 - Google Chrome"
	s.last.Elements[0].WindowID = 7
	s.last.Elements[1].Window = "收件箱 - Google Chrome"
	s.last.Elements[1].WindowID = 8
	s.mu.Unlock()
	if _, stale := s.ObserveDocumentStale("收件箱 - Google Chrome", 8); stale {
		t.Fatal("a window in the margin is not the cropped document")
	}
	if _, stale := s.ObserveDocumentStale("订单确认 - Google Chrome", 7); !stale {
		t.Fatal("the cropped window navigated")
	}
	s.mu.Lock()
	s.last.Meta.CropTitle = "收件箱 (1) - Google Chrome"
	s.mu.Unlock()
	if _, stale := s.ObserveDocumentStale("收件箱 (2) - Google Chrome", 7); stale {
		t.Fatal("an unread count is not a new document")
	}
}

func TestKeySurfaceLabelsSkipSameAppCrop(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 80, Height: 40, ScaleFactor: 1, CropTitle: "订单 - Google Chrome"}, nil, []taskengine.UIElement{
		{Name: "立即支付", BBox: [4]int{0, 0, 40, 20}},
	}, nil, "")
	if labels := s.KeySurfaceLabels("收件箱 - Google Chrome"); labels != nil {
		t.Fatalf("same-app crop kept pay labels: %v", labels)
	}
}
