package main

import (
	"context"
	"testing"
	"time"
)

func TestDesktopVisionSlotOneAtATime(t *testing.T) {
	old := desktopVisionWait
	desktopVisionWait = 50 * time.Millisecond
	defer func() { desktopVisionWait = old }()
	if !acquireDesktopVision(context.Background()) {
		t.Fatal("first acquire failed")
	}
	if acquireDesktopVision(context.Background()) {
		t.Fatal("second acquire must wait and give up while one runs")
	}
	releaseDesktopVision()
	if !acquireDesktopVision(context.Background()) {
		t.Fatal("acquire after release failed")
	}
	releaseDesktopVision()
}
