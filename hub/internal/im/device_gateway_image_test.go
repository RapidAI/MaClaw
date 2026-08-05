package im

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func encodedGatewayTestImage(t *testing.T, format string, width, height int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff})
	var output bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&output, img)
	case "jpeg":
		err = jpeg.Encode(&output, img, nil)
	default:
		t.Fatalf("unsupported test image format %q", format)
	}
	if err != nil {
		t.Fatalf("encode test %s: %v", format, err)
	}
	return base64.StdEncoding.EncodeToString(output.Bytes())
}

func TestAdaptDeviceGatewayReplyValidatesRGB565ImageShape(t *testing.T) {
	capabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{
			MimeTypes: []string{coreim.ThirdPartyRGB565MIME}, MaxWidth: 64, MaxHeight: 64,
		},
	}})
	valid := map[string]any{
		"type": "image", "mime_type": coreim.ThirdPartyRGB565MIME,
		"width": 64, "height": 32, "data": base64.StdEncoding.EncodeToString(make([]byte, 64*32*2)),
	}
	if !adaptDeviceGatewayReply(valid, capabilities) {
		t.Fatal("valid RGB565 image was rejected")
	}

	smallerCapabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{
			MimeTypes: []string{coreim.ThirdPartyRGB565MIME}, MaxWidth: 32, MaxHeight: 16,
		},
	}})
	if adaptDeviceGatewayReply(valid, smallerCapabilities) {
		t.Fatal("image exceeding the client's declared dimensions was accepted")
	}
	invalid := cloneDeviceMessage(valid)
	invalid["height"] = 64
	if adaptDeviceGatewayReply(invalid, capabilities) {
		t.Fatal("mismatched RGB565 byte length was accepted")
	}
	wrongSize := cloneDeviceMessage(valid)
	wrongSize["sizeBytes"] = 1
	if adaptDeviceGatewayReply(wrongSize, capabilities) {
		t.Fatal("RGB565 image with mismatched declared size was accepted")
	}
	oversizedBase64 := cloneDeviceMessage(valid)
	oversizedBase64["data"] = base64.StdEncoding.EncodeToString(make([]byte, 64*32*2+1))
	if adaptDeviceGatewayReply(oversizedBase64, capabilities) {
		t.Fatal("oversized RGB565 payload was accepted")
	}
}

func TestAdaptDeviceGatewayReplyValidatesEncodedImages(t *testing.T) {
	capabilities := agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{
			MimeTypes: []string{"image/png"}, MaxWidth: 64, MaxHeight: 64,
		},
	}})
	validData := encodedGatewayTestImage(t, "png", 64, 32)
	valid := map[string]any{"type": "image", "mime_type": "image/png", "image_data": validData}
	if !adaptDeviceGatewayReply(valid, capabilities) {
		t.Fatal("valid PNG image was rejected")
	}
	if valid["width"] != 64 || valid["height"] != 32 {
		t.Fatalf("normalized dimensions = %vx%v", valid["width"], valid["height"])
	}
	if valid["sizeBytes"] != len(mustDecodeGatewayImage(t, validData)) {
		t.Fatalf("normalized sizeBytes = %v", valid["sizeBytes"])
	}

	tests := []struct {
		name  string
		reply map[string]any
	}{
		{name: "invalid base64", reply: map[string]any{"type": "image", "mime_type": "image/png", "data": "%%%"}},
		{name: "empty data", reply: map[string]any{"type": "image", "mime_type": "image/png"}},
		{name: "jpeg disguised as png", reply: map[string]any{"type": "image", "mime_type": "image/png", "data": encodedGatewayTestImage(t, "jpeg", 32, 16)}},
		{name: "too wide", reply: map[string]any{"type": "image", "mime_type": "image/png", "data": encodedGatewayTestImage(t, "png", 65, 32)}},
		{name: "declared size mismatch", reply: map[string]any{"type": "image", "mime_type": "image/png", "data": validData, "sizeBytes": 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if adaptDeviceGatewayReply(test.reply, capabilities) {
				t.Fatal("invalid encoded image was accepted")
			}
		})
	}
}

func mustDecodeGatewayImage(t *testing.T, encoded string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
