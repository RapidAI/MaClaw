package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
)

func TestComputerUseLLMSupportsVisionTurnOverride(t *testing.T) {
	prevFn := computerUseVisionFn
	t.Cleanup(func() {
		computerUseVisionFn = prevFn
		globalComputerUse.mu.Lock()
		globalComputerUse.turnVisionKnown = false
		globalComputerUse.turnVision = false
		globalComputerUse.pendingModelImage = ""
		globalComputerUse.mu.Unlock()
	})
	computerUseVisionFn = func() bool { return false }
	if computerUseLLMSupportsVision() {
		t.Fatal("default fn false")
	}
	setComputerUseTurnVision(true)
	if !computerUseLLMSupportsVision() {
		t.Fatal("turn override should win")
	}
	setComputerUseTurnVision(false)
	if computerUseLLMSupportsVision() {
		t.Fatal("turn override false")
	}
}

func TestAttachPendingComputerUseModelImage(t *testing.T) {
	t.Cleanup(func() { setPendingComputerUseModelImage("") })
	setPendingComputerUseModelImage("pngdata")
	got := attachPendingComputerUseModelImage(agent.ToolExecutionResult{Result: "ok"})
	if len(got.ModelImages) != 1 || got.ModelImages[0].Base64 != "pngdata" {
		t.Fatalf("images=%#v", got.ModelImages)
	}
	got2 := attachPendingComputerUseModelImage(agent.ToolExecutionResult{Result: "ok"})
	if len(got2.ModelImages) != 0 {
		t.Fatal("pending image must be consumed once")
	}
}

func TestResizePNGBase64MaxEdgeNoopWhenSmall(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	got, w, h, err := resizePNGBase64MaxEdge(b64, 1568)
	if err != nil {
		t.Fatal(err)
	}
	if got != b64 || w != 4 || h != 4 {
		t.Fatalf("noop failed w=%d h=%d changed=%v", w, h, got != b64)
	}
}

func TestApplyPerceptionModeOnSession(t *testing.T) {
	s := computeruse.NewSession(computeruse.DefaultConfig())
	s.ApplyPerceptionMode(true)
	if !s.AllowPixelClick() {
		t.Fatal("vision should allow pixel click")
	}
}
