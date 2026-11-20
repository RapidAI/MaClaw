package guiapp

import (
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
)

// Light-blue rim around the top-level window Computer Use is driving.
// The glow starts computerUseHaloGap pixels outside the window rectangle.
// That gap is wider than computerUseCropMargin, so a cropped observe shot
// does not contain the rim. MaClaw's own windows are left unmarked: the pet,
// the main window, and this overlay are not the app being operated.
const (
	computerUseHaloGap    = 12
	computerUseHaloBand   = 22
	computerUseHaloRadius = 12
	// computerUseHaloPeak is the straight alpha of the brightest rim pixel.
	computerUseHaloPeak = 188
)

var (
	computerUseHaloMu           sync.Mutex
	computerUseHaloID           uint64
	computerUseHaloForceLive    bool
	computerUseHaloSkipAffinity bool
	computerUseHaloOwnsWindow   = func(id uint64) bool {
		return accessibility.WindowOwnedBySelf(id)
	}
)

func computerUseHaloTarget() uint64 {
	computerUseHaloMu.Lock()
	defer computerUseHaloMu.Unlock()
	return computerUseHaloID
}

func computerUseHaloLive() bool {
	computerUseHaloMu.Lock()
	force := computerUseHaloForceLive
	computerUseHaloMu.Unlock()
	if force {
		return true
	}
	return !testingComputerUseHalo()
}

func computerUseHaloSkipCaptureExclusion() bool {
	computerUseHaloMu.Lock()
	defer computerUseHaloMu.Unlock()
	return computerUseHaloSkipAffinity
}

func computerUseHaloWindowIsOwn(id uint64) bool {
	computerUseHaloMu.Lock()
	fn := computerUseHaloOwnsWindow
	computerUseHaloMu.Unlock()
	if fn == nil {
		return false
	}
	return fn(id)
}

func computerUseHaloForgetTarget(id uint64) {
	computerUseHaloMu.Lock()
	if computerUseHaloID == id {
		computerUseHaloID = 0
	}
	computerUseHaloMu.Unlock()
}

// noteComputerUseOperatedWindow marks id as the app being driven.
// A zero id, one of the rim strips, or a window in this process keeps the
// previous target, so a glance back at MaClaw does not move the rim.
func noteComputerUseOperatedWindow(id uint64) {
	if id == 0 || computerUseHaloIsOverlay(id) {
		return
	}
	if computerUseHaloWindowIsOwn(id) {
		return
	}
	computerUseHaloMu.Lock()
	computerUseHaloID = id
	computerUseHaloMu.Unlock()
	presentComputerUseWindowHalo()
}

func noteComputerUseNamedWindow(title string) {
	var id uint64
	if strings.TrimSpace(title) != "" {
		id = accessibility.NamedWindowID(title)
	}
	if id == 0 {
		id = accessibility.ForegroundWindowID()
	}
	noteComputerUseOperatedWindow(id)
}

func noteComputerUsePointWindow(x, y int) {
	id := accessibility.WindowIDAtPoint(x, y)
	if id == 0 {
		id = accessibility.ForegroundWindowID()
	}
	noteComputerUseOperatedWindow(id)
}

// hideComputerUseWindowHalo removes the rim. Stop, reset, done, and a turn
// that drops the Computer Use session all come through here.
func hideComputerUseWindowHalo() {
	computerUseHaloMu.Lock()
	computerUseHaloID = 0
	computerUseHaloMu.Unlock()
	hideComputerUseWindowHaloPlatform()
}
