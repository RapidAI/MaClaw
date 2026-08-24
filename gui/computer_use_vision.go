package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// computerUseVisionMaxEdge is the long-edge cap for screenshots sent to a
// vision chat model (Claude computer-use recommendation is 1568).
const computerUseVisionMaxEdge = 1568

// computerUseVisionMaxBytes is a decoded-size cap after the edge resize.
const computerUseVisionMaxBytes = 1_200_000

// computerUseVisionFn reports whether the current assistant model accepts
// images. Tests leave this false so observe stays on OmniParser/OCR.
var computerUseVisionFn = func() bool { return false }

func computerUseLLMSupportsVision() bool {
	globalComputerUse.mu.Lock()
	known := globalComputerUse.turnVisionKnown
	turn := globalComputerUse.turnVision
	globalComputerUse.mu.Unlock()
	if known {
		return turn
	}
	if computerUseVisionFn == nil {
		return false
	}
	return computerUseVisionFn()
}

func setComputerUseTurnVision(supports bool) {
	globalComputerUse.mu.Lock()
	globalComputerUse.turnVisionKnown = true
	globalComputerUse.turnVision = supports
	globalComputerUse.mu.Unlock()
}

func setPendingComputerUseModelImage(pngB64 string) {
	globalComputerUse.mu.Lock()
	globalComputerUse.pendingModelImage = strings.TrimSpace(pngB64)
	globalComputerUse.mu.Unlock()
}

func takePendingComputerUseModelImage() (string, bool) {
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	img := globalComputerUse.pendingModelImage
	globalComputerUse.pendingModelImage = ""
	if img == "" {
		return "", false
	}
	return img, true
}

func attachPendingComputerUseModelImage(result agent.ToolExecutionResult) agent.ToolExecutionResult {
	img, ok := takePendingComputerUseModelImage()
	if !ok {
		return result
	}
	result.ModelImages = append(result.ModelImages, agent.ToolModelImage{
		MIME:   "image/png",
		Base64: img,
	})
	return result
}

// prepareVisionScreenshot resizes a capture for the chat model and returns
// the (possibly smaller) PNG plus the image pixel size the model will see.
func prepareVisionScreenshot(pngB64 string) (out string, vw, vh int) {
	out = pngB64
	if w, h, ok := decodeImageSizeB64(pngB64); ok {
		vw, vh = w, h
	}
	resized, rw, rh, err := resizePNGBase64MaxEdge(pngB64, computerUseVisionMaxEdge)
	if err != nil {
		log.Printf("[computer-use] vision resize: %v", err)
		return out, vw, vh
	}
	out, vw, vh = resized, rw, rh
	if ExceedsImageSizeLimit(out, computerUseVisionMaxBytes) {
		if ds, err := remote.DownsizeScreenshotBase64(out, computerUseVisionMaxBytes); err == nil {
			out = ds
			if w, h, ok := decodeImageSizeB64(out); ok {
				vw, vh = w, h
			}
		}
	}
	return out, vw, vh
}

func resizePNGBase64MaxEdge(pngB64 string, maxEdge int) (string, int, int, error) {
	if maxEdge <= 0 {
		maxEdge = computerUseVisionMaxEdge
	}
	raw, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("decode: %w", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", 0, 0, fmt.Errorf("png: %w", err)
	}
	b := img.Bounds()
	origW, origH := b.Dx(), b.Dy()
	if origW <= 0 || origH <= 0 {
		return pngB64, origW, origH, nil
	}
	long := origW
	if origH > long {
		long = origH
	}
	if long <= maxEdge {
		return pngB64, origW, origH, nil
	}
	newW := origW * maxEdge / long
	newH := origH * maxEdge / long
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := b.Min.Y + y*origH/newH
		for x := 0; x < newW; x++ {
			srcX := b.Min.X + x*origW/newW
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", 0, 0, err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), newW, newH, nil
}
