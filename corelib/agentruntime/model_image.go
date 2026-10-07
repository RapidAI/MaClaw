package agentruntime

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// Runtime module tools return text only (Module.InvokeTool). A module tool
// that also wants a vision-capable model to see an image, such as the
// MaClawSrv desktop screenshot, appends AttachModelImage's marker to its
// text. The host strips every marker with ExtractModelImages before the text
// reaches the model, history, or logs, and passes the images on as
// agent.ToolExecutionResult.ModelImages; models without vision just get the
// text.
//
// The marker carries a random per-process nonce. Page text, file contents,
// or command output a tool echoes cannot know it, so they cannot smuggle an
// image (or hide text) through this channel.

// MaxModelImageBase64 bounds one attached image (about 6MB decoded).
const MaxModelImageBase64 = 8 << 20

var modelImageMarker = func() string {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic("agentruntime: model image nonce: " + err.Error())
	}
	return "\x00[maclaw-model-image " + hex.EncodeToString(nonce[:]) + " "
}()

const modelImageEnd = "]\x00"

// AttachModelImage returns text with one image attached for the model.
// Unsupported MIME types or invalid data leave text unchanged.
func AttachModelImage(text, mime, base64Data string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	base64Data = strings.TrimSpace(base64Data)
	if !modelImageMIME(mime) || !modelImageData(base64Data) {
		return text
	}
	return text + modelImageMarker + mime + " " + base64Data + modelImageEnd
}

// ExtractModelImages removes every attachment AttachModelImage added and
// returns the remaining text and the images in order.
func ExtractModelImages(text string) (string, []agent.ToolModelImage) {
	if !strings.Contains(text, modelImageMarker) {
		return text, nil
	}
	var out strings.Builder
	var images []agent.ToolModelImage
	rest := text
	for {
		start := strings.Index(rest, modelImageMarker)
		if start < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:start])
		body := rest[start+len(modelImageMarker):]
		end := strings.Index(body, modelImageEnd)
		if end < 0 {
			// Truncated attachment: drop it rather than leak base64 into text.
			break
		}
		mime, data, ok := strings.Cut(body[:end], " ")
		if ok && modelImageMIME(mime) && modelImageData(data) {
			images = append(images, agent.ToolModelImage{MIME: mime, Base64: data})
		}
		rest = body[end+len(modelImageEnd):]
	}
	return out.String(), images
}

func modelImageMIME(mime string) bool {
	switch mime {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}

func modelImageData(data string) bool {
	if data == "" || len(data) > MaxModelImageBase64 || len(data)%4 != 0 {
		return false
	}
	for i := 0; i < len(data); i++ {
		c := data[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=') {
			return false
		}
	}
	return true
}
