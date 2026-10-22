package main

import (
	"context"
	"os"
	"runtime/debug"
	"strings"
)

// One YOLO pass at the 1280 input size needs about 1 GB of scratch, so
// screenshots from parallel steps or bots run it one at a time.
var desktopYOLOSem = make(chan struct{}, 1)

// desktopScreenYOLOEnabled lets MACLAW_SCREEN_YOLO=off skip the detector.
func desktopScreenYOLOEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("MACLAW_SCREEN_YOLO")), "off")
}

// desktopAcquireYOLO waits for the detector slot. The release hands the
// scratch buffers back to the OS so one pass does not pin a gigabyte.
func desktopAcquireYOLO(ctx context.Context) (func(), bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case desktopYOLOSem <- struct{}{}:
	case <-ctx.Done():
		return nil, false
	}
	return func() {
		debug.FreeOSMemory()
		debug.FreeOSMemory()
		<-desktopYOLOSem
	}, true
}
