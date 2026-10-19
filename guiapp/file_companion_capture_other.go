//go:build !windows && !darwin && !linux

package guiapp

import (
	"context"
	"image"
	"os"
)

func captureCompanionWebViewPNG(ctx context.Context, _ image.Rectangle) ([]byte, error) {
	fileCompanionCaptureViewCalls.Add(1)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, errCompanionWebViewUnbound
}

func captureProcessAudio(ctx context.Context) ([]byte, bool) {
	fileCompanionCaptureAudioCalls.Add(1)
	act := companionProcessAudioActivate(uint32(os.Getpid()))
	if act.StreamFlags&audclntStreamFlagsLoopback != 0 || !act.IncludeProcessTree || act.TargetPID == 0 {
		return nil, true
	}
	if ctx.Err() != nil || !companionCaptureViewBound() {
		return nil, true
	}
	return nil, true
}
