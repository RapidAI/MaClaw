package main

import (
	"context"
	"testing"
	"time"
)

func TestDesktopYOLOGateSerializes(t *testing.T) {
	release, ok := desktopAcquireYOLO(context.Background())
	if !ok {
		t.Fatal("first acquire")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, ok := desktopAcquireYOLO(ctx); ok {
		t.Fatal("second acquire should wait")
	}
	release()
	r2, ok := desktopAcquireYOLO(context.Background())
	if !ok {
		t.Fatal("acquire after release")
	}
	r2()
	t.Setenv("MACLAW_SCREEN_YOLO", "off")
	if desktopScreenYOLOEnabled() {
		t.Fatal("off")
	}
}
