//go:build windows

package guiapp

import (
	"context"
	"strings"
	"testing"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
)

func TestAcceptanceConsoleStructSizes(t *testing.T) {
	if unsafe.Sizeof(acceptanceInputRecord{}) != 20 {
		t.Fatalf("INPUT_RECORD size = %d, want 20", unsafe.Sizeof(acceptanceInputRecord{}))
	}
	if unsafe.Sizeof(acceptanceCharInfo{}) != 4 {
		t.Fatalf("CHAR_INFO size = %d, want 4", unsafe.Sizeof(acceptanceCharInfo{}))
	}
	if unsafe.Sizeof(acceptanceSmallRect{}) != 8 {
		t.Fatalf("SMALL_RECT size = %d, want 8", unsafe.Sizeof(acceptanceSmallRect{}))
	}
	if unsafe.Sizeof(acceptanceConsoleScreenBufferInfo{}) != 22 {
		t.Fatalf("CONSOLE_SCREEN_BUFFER_INFO size = %d, want 22", unsafe.Sizeof(acceptanceConsoleScreenBufferInfo{}))
	}
	key := acceptanceInputRecord{EventType: 0x0001, KeyDown: 1, Repeat: 1, VK: 0x25, Control: 0x0100}
	if key.Control != 0x0100 || key.Rune != 0 {
		t.Fatal("arrow record must set ENHANCED_KEY and leave UnicodeChar empty")
	}
}

func TestAcceptanceExtendedKeyFlag(t *testing.T) {
	flags, err := guiautomation.KeyComboFlagWords("left")
	if err != nil || len(flags) != 2 {
		t.Fatalf("left inputs: %v %v", err, flags)
	}
	if flags[0] != 0x0001 || flags[1] != (0x0001|0x0002) {
		t.Fatalf("arrow flags down=%#x up=%#x", flags[0], flags[1])
	}
	letter, err := guiautomation.KeyComboFlagWords("z")
	if err != nil || len(letter) != 2 || letter[0] != 0 || letter[1] != 0x0002 {
		t.Fatalf("letter flags %v %v", letter, err)
	}
	for _, name := range []string{"up", "right", "down", "insert", "delete", "home", "end", "pageup", "pagedown"} {
		got, err := guiautomation.KeyComboFlagWords(name)
		if err != nil || len(got) != 2 || got[0]&0x0001 == 0 || got[1]&0x0001 == 0 {
			down, up := uint32(0), uint32(0)
			if len(got) > 0 {
				down = got[0]
			}
			if len(got) > 1 {
				up = got[1]
			}
			t.Fatalf("%s flags err=%v down=%#x up=%#x", name, err, down, up)
		}
	}
}

func TestAcceptanceDesktopClickUsesOneObservation(t *testing.T) {
	lockAcceptanceHooks(t)
	exe := `C:\build\tetris.exe`
	sess := &realDesktopSession{exe: exe}
	clicks := 0
	var gotX, gotY int
	acceptanceDesktopClickHook = func(x, y int) error {
		clicks++
		gotX, gotY = x, y
		return nil
	}
	read := func(obs desktopObservation) {
		t.Helper()
		reads := 0
		acceptanceReadForeground = func() (desktopObservation, bool) {
			reads++
			return obs, true
		}
		err := sess.Click(context.Background(), 1, 10, 12)
		if reads != 1 {
			t.Fatalf("foreground reads=%d obs=%+v err=%v", reads, obs, err)
		}
		if clicks != 0 || err == nil {
			t.Fatalf("sent a click clicks=%d err=%v obs=%+v", clicks, err, obs)
		}
	}
	read(desktopObservation{Image: `C:\other\notepad.exe`, Title: "Notepad", HasBounds: true, Left: 0, Top: 0, Right: 400, Bottom: 300})
	read(desktopObservation{Title: "Game", HasBounds: true, Left: 0, Top: 0, Right: 400, Bottom: 300})
	read(desktopObservation{Image: exe, HasBounds: false, Title: "Tetris"})
	outsideErr := error(nil)
	reads := 0
	acceptanceReadForeground = func() (desktopObservation, bool) {
		reads++
		return desktopObservation{Image: exe, HasBounds: true, Left: 0, Top: 0, Right: 8, Bottom: 8}, true
	}
	outsideErr = sess.Click(context.Background(), 1, 10, 12)
	if reads != 1 || clicks != 0 || outsideErr == nil || !strings.Contains(outsideErr.Error(), "窗口之外") {
		t.Fatalf("outside reads=%d clicks=%d err=%v", reads, clicks, outsideErr)
	}
	reads = 0
	acceptanceReadForeground = func() (desktopObservation, bool) {
		reads++
		return desktopObservation{Image: exe, Title: "Shop", Labels: []string{"立即支付"}, HasBounds: true, Left: 0, Top: 0, Right: 100, Bottom: 80}, true
	}
	payErr := sess.Click(context.Background(), 1, 10, 12)
	if reads != 1 || clicks != 0 || payErr == nil {
		t.Fatalf("pay reads=%d clicks=%d err=%v", reads, clicks, payErr)
	}
	reads = 0
	acceptanceReadForeground = func() (desktopObservation, bool) {
		reads++
		return desktopObservation{Image: exe, Title: "Tetris", HasBounds: true, Left: 0, Top: 0, Right: 100, Bottom: 80}, true
	}
	if err := sess.Click(context.Background(), 1, 10, 12); err != nil || clicks != 1 || reads != 1 || gotX != 10 || gotY != 12 {
		t.Fatalf("inside err=%v clicks=%d reads=%d point=%d,%d", err, clicks, reads, gotX, gotY)
	}
	payDetail, payReason, payOK := acceptanceDesktopFaultDetail(payErr)
	if !payOK || payDetail != "handoff_refused" || !strings.Contains(payReason, "秘密") || strings.Contains(payReason, "点击失败") {
		t.Fatalf("pay fault detail=%s reason=%q", payDetail, payReason)
	}
	reads = 0
	acceptanceReadForeground = func() (desktopObservation, bool) {
		reads++
		return desktopObservation{Image: exe, Class: "wailsWindow", Title: "Tetris", HasBounds: true, Left: 0, Top: 0, Right: 100, Bottom: 80}, true
	}
	maclawErr := sess.Click(context.Background(), 1, 10, 12)
	maclawDetail, maclawReason, maclawOK := acceptanceDesktopFaultDetail(maclawErr)
	if reads != 1 || clicks != 1 || !maclawOK || maclawDetail != "handoff_refused" || maclawReason != acceptanceReasonMaClaw {
		t.Fatalf("maclaw reads=%d clicks=%d detail=%s reason=%q err=%v", reads, clicks, maclawDetail, maclawReason, maclawErr)
	}
	acceptanceReadForeground = func() (desktopObservation, bool) {
		return desktopObservation{Image: exe, Title: "Tetris", HasBounds: true, Left: 0, Top: 0, Right: 100, Bottom: 80}, true
	}
	keyErr := sess.Key(context.Background(), 1, "left")
	if keyErr == nil || !strings.Contains(keyErr.Error(), "refusing to send input under test") {
		t.Fatalf("key under test: %v", keyErr)
	}
	acceptanceReadForeground = func() (desktopObservation, bool) {
		return desktopObservation{Image: exe, Title: "请输入密码", HasBounds: true, Left: 0, Top: 0, Right: 100, Bottom: 80}, true
	}
	secretKey := sess.Key(context.Background(), 1, "left")
	secretDetail, secretReason, secretOK := acceptanceDesktopFaultDetail(secretKey)
	if !secretOK || secretDetail != "handoff_refused" || !strings.Contains(secretReason, "秘密") || strings.Contains(secretKey.Error(), "按键失败") {
		t.Fatalf("secret key detail=%s reason=%q err=%v", secretDetail, secretReason, secretKey)
	}
}

func acceptanceDesktopFaultDetail(err error) (string, string, bool) {
	detail, reason := acceptanceDesktopDriveError(err, "click_failed")
	if detail == "click_failed" {
		return detail, reason, false
	}
	return detail, reason, true
}

func TestAcceptancePasswordEditClass(t *testing.T) {
	if !acceptancePasswordEditClass("Edit", acceptanceESPassword) {
		t.Fatal("edit password style was not a password control")
	}
	if !acceptancePasswordEditClass("RICHEDIT50W", acceptanceESPassword|0x0004) {
		t.Fatal("rich edit password style was not a password control")
	}
	if acceptancePasswordEditClass("Edit", 0) || acceptancePasswordEditClass("Button", acceptanceESPassword) {
		t.Fatal("a plain edit or a button was treated as a password control")
	}
}
