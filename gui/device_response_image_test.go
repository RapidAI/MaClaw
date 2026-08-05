package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func TestPrepareDeviceResponseImagePreservesAspectRatioAndRGB565WireOrder(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 128; x++ {
			source.Set(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, source); err != nil {
		t.Fatal(err)
	}
	capabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{
			MimeTypes: []string{coreim.ThirdPartyRGB565MIME}, MaxWidth: 64, MaxHeight: 64,
		},
	}})
	prepared, err := prepareDeviceResponseImage(base64.StdEncoding.EncodeToString(pngData.Bytes()), capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Width != 64 || prepared.Height != 32 {
		t.Fatalf("dimensions = %dx%d, want 64x32", prepared.Width, prepared.Height)
	}
	pixels, err := base64.StdEncoding.DecodeString(prepared.Data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pixels) != 64*32*2 {
		t.Fatalf("pixel bytes = %d", len(pixels))
	}
	if pixels[0] != 0xf8 || pixels[1] != 0x00 {
		t.Fatalf("first red pixel = %02x%02x, want f800", pixels[0], pixels[1])
	}
}

func TestProportionalImageSizeDoesNotEnlarge(t *testing.T) {
	width, height := proportionalImageSize(32, 16, &agent.ClientImageCapabilities{MaxWidth: 64, MaxHeight: 64})
	if width != 32 || height != 16 {
		t.Fatalf("dimensions = %dx%d, want 32x16", width, height)
	}
}

func TestPrepareDeviceResponseImageRejectsOversizedSourceBeforeDecode(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	raw := encoded.Bytes()
	// A PNG decoder reads dimensions from IHDR. Recompute that chunk's CRC so
	// DecodeConfig accepts the header without attempting a full pixel decode.
	binary.BigEndian.PutUint32(raw[16:20], deviceResponseImageMaxSourceDimension+1)
	binary.BigEndian.PutUint32(raw[29:33], crc32.ChecksumIEEE(raw[12:29]))

	capabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{
			MimeTypes: []string{coreim.ThirdPartyRGB565MIME}, MaxWidth: 64, MaxHeight: 64,
		},
	}})
	_, err := prepareDeviceResponseImage(base64.StdEncoding.EncodeToString(raw), capabilities)
	if err == nil || !strings.Contains(err.Error(), "source dimensions are too large") {
		t.Fatalf("error = %v, want oversized source rejection", err)
	}
}

func TestPrepareDeviceResponseImageDoesNotMislabelNonPNGData(t *testing.T) {
	// A valid JPEG demonstrates that a
	// PNG-only client must never receive other bytes labelled image/png.
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	capabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{MimeTypes: []string{"image/png"}},
	}})
	_, err := prepareDeviceResponseImage(base64.StdEncoding.EncodeToString(encoded.Bytes()), capabilities)
	if err == nil || !strings.Contains(err.Error(), "requires PNG") {
		t.Fatalf("error = %v, want non-PNG rejection", err)
	}
}

func TestPrepareDeviceResponseImageConvertsJPEGForRGB565Client(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	capabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{
			MimeTypes: []string{coreim.ThirdPartyRGB565MIME}, MaxWidth: 64, MaxHeight: 64,
		},
	}})
	prepared, err := prepareDeviceResponseImage(base64.StdEncoding.EncodeToString(encoded.Bytes()), capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Width != 64 || prepared.Height != 32 || prepared.MIMEType != coreim.ThirdPartyRGB565MIME {
		t.Fatalf("prepared JPEG = %dx%d %q, want 64x32 RGB565", prepared.Width, prepared.Height, prepared.MIMEType)
	}
	if pixels, err := base64.StdEncoding.DecodeString(prepared.Data); err != nil || len(pixels) != 64*32*2 {
		t.Fatalf("RGB565 bytes = %d, err=%v", len(pixels), err)
	}
}

func TestPrepareDeviceResponseImageResizesPNGForPNGOnlyClient(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	capabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{
			MimeTypes: []string{"image/png"}, MaxWidth: 64, MaxHeight: 64,
		},
	}})
	prepared, err := prepareDeviceResponseImage(base64.StdEncoding.EncodeToString(encoded.Bytes()), capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Width != 64 || prepared.Height != 32 {
		t.Fatalf("dimensions = %dx%d, want 64x32", prepared.Width, prepared.Height)
	}
	raw, err := base64.StdEncoding.DecodeString(prepared.Data)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 64 || config.Height != 32 {
		t.Fatalf("encoded PNG = %dx%d, want 64x32", config.Width, config.Height)
	}
}

func TestPrepareDeviceResponseImageDoesNotEnlargePNGForPNGOnlyClient(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	input := base64.StdEncoding.EncodeToString(encoded.Bytes())
	capabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{
			MimeTypes: []string{"image/png"}, MaxWidth: 64, MaxHeight: 64,
		},
	}})
	prepared, err := prepareDeviceResponseImage(input, capabilities)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Width != 32 || prepared.Height != 16 || prepared.Data != input {
		t.Fatalf("small PNG was changed: dimensions=%dx%d sameData=%v", prepared.Width, prepared.Height, prepared.Data == input)
	}
}
