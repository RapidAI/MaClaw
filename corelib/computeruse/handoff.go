package computeruse

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// Desktop handoff. A pay click or a password field stops injection and waits
// until the person finishes on the screen. The model cannot approve it, and
// the queued click or keystrokes are not sent.

var (
	// ErrDesktopHandoffOff means no UI is waiting, so the caller refuses at once.
	ErrDesktopHandoffOff = errors.New("desktop handoff is off")
	// ErrDesktopHandoffDone means the person finished the step on the screen.
	ErrDesktopHandoffDone = errors.New("用户已在屏幕上完成这一步。先 computer_observe。不要重试刚才的点击或输入，也不要在对话里索要密码或验证码。")
	// ErrDesktopHandoffCancelled means the person cancelled the step.
	ErrDesktopHandoffCancelled = errors.New("用户取消了屏幕上的这一步。不要重试刚才的点击或输入，也不要索要密码或验证码。")
	// ErrDesktopHandoffTimeout means the person did not finish in time.
	ErrDesktopHandoffTimeout = errors.New("等待用户操作屏幕超时。不要重试刚才的点击或输入，也不要索要密码或验证码。")
	// ErrDesktopHandoffNone means nothing is waiting.
	ErrDesktopHandoffNone = errors.New("没有等待中的屏幕操作")
)

// desktopHandoffWaitCap bounds one handoff. Tests shrink it.
var desktopHandoffWaitCap = 15 * time.Minute

// DesktopHandoffNotice is what the desktop UI shows while the person works.
// Active false ends the wait. Resume tells the UI to let the agent continue.
type DesktopHandoffNotice struct {
	Active bool
	Resume bool
	Reason string
	Title  string
}

type desktopHandoff struct {
	done     chan struct{}
	gen      uint64
	closed   bool
	ended    bool
	resume   bool
	finished bool
}

var (
	desktopHandoffMu   sync.Mutex
	desktopHandoffFn   func(DesktopHandoffNotice)
	desktopHandoffWait *desktopHandoff
	desktopHandoffGen  uint64
)

// SetDesktopHandoffNotifier installs the UI hook. Nil turns the wait off.
// The hook must return quickly and must not call OfferDesktopHandoff.
func SetDesktopHandoffNotifier(fn func(DesktopHandoffNotice)) {
	desktopHandoffMu.Lock()
	desktopHandoffFn = fn
	desktopHandoffMu.Unlock()
}

// DesktopHandoffEnabled reports whether a pay or secret step will wait.
func DesktopHandoffEnabled() bool {
	desktopHandoffMu.Lock()
	defer desktopHandoffMu.Unlock()
	return desktopHandoffFn != nil
}

// OfferDesktopHandoff shows the handoff and waits. The reason is "secret" or
// "checkout". title is a window or control name, never the secret itself.
func OfferDesktopHandoff(reason, title string) error {
	desktopHandoffMu.Lock()
	fn := desktopHandoffFn
	if fn == nil {
		desktopHandoffMu.Unlock()
		return ErrDesktopHandoffOff
	}
	if desktopHandoffWait != nil {
		w := desktopHandoffWait
		desktopHandoffMu.Unlock()
		return waitDesktopHandoff(w)
	}
	desktopHandoffGen++
	w := &desktopHandoff{done: make(chan struct{}), gen: desktopHandoffGen, resume: true}
	desktopHandoffWait = w
	desktopHandoffMu.Unlock()

	fn(DesktopHandoffNotice{
		Active: true,
		Reason: reason,
		Title:  clipHandoffTitle(title),
	})
	return waitDesktopHandoff(w)
}

func waitDesktopHandoff(w *desktopHandoff) error {
	timer := time.NewTimer(desktopHandoffWaitCap)
	defer timer.Stop()
	defer endDesktopHandoff(w)
	select {
	case <-w.done:
		desktopHandoffMu.Lock()
		finished := w.finished
		desktopHandoffMu.Unlock()
		if finished {
			return ErrDesktopHandoffDone
		}
		return ErrDesktopHandoffCancelled
	case <-timer.C:
		finishDesktopHandoff(w, false)
		return ErrDesktopHandoffTimeout
	}
}

func endDesktopHandoff(w *desktopHandoff) {
	if w == nil {
		return
	}
	desktopHandoffMu.Lock()
	defer desktopHandoffMu.Unlock()
	if w.ended || desktopHandoffGen != w.gen {
		return
	}
	w.ended = true
	if desktopHandoffFn == nil {
		return
	}
	// The notifier must not call OfferDesktopHandoff or ResolveDesktopHandoff.
	desktopHandoffFn(DesktopHandoffNotice{Active: false, Resume: w.resume})
}

func finishDesktopHandoff(w *desktopHandoff, finished bool) {
	if w == nil {
		return
	}
	desktopHandoffMu.Lock()
	defer desktopHandoffMu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	w.finished = finished
	close(w.done)
	if desktopHandoffWait == w {
		desktopHandoffWait = nil
	}
}

// ResolveDesktopHandoff records the person's decision.
// done true means they finished. False means they cancelled.
func ResolveDesktopHandoff(done bool) error {
	desktopHandoffMu.Lock()
	w := desktopHandoffWait
	desktopHandoffMu.Unlock()
	if w == nil {
		return ErrDesktopHandoffNone
	}
	finishDesktopHandoff(w, done)
	return nil
}

// SetDesktopHandoffResume chooses whether ending this handoff lets the agent
// continue. Stop sets it false so a hard stop is not cleared afterwards.
func SetDesktopHandoffResume(on bool) {
	desktopHandoffMu.Lock()
	defer desktopHandoffMu.Unlock()
	if desktopHandoffWait != nil {
		desktopHandoffWait.resume = on
	}
}

func clipHandoffTitle(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	r := []rune(s)
	if len(r) > 80 {
		return string(r[:80])
	}
	return string(r)
}
