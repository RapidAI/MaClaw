package main

import (
	"strings"
	"testing"
)

func TestResolveSendFileForwardToIMStructured(t *testing.T) {
	// Structured boolean flag.
	if !resolveSendFileForwardToIM(map[string]interface{}{"forward_to_im": true}) {
		t.Fatal("forward_to_im bool true")
	}
	if !resolveSendFileForwardToIM(map[string]interface{}{"forward_to_im": "true"}) {
		t.Fatal("forward_to_im string true")
	}
	if resolveSendFileForwardToIM(map[string]interface{}{"forward_to_im": false}) {
		t.Fatal("forward_to_im false")
	}
	if resolveSendFileForwardToIM(nil) {
		t.Fatal("nil args should not forward")
	}

	// Structured destination enum — stable control plane.
	for _, dest := range []string{"im", "wechat", "weixin", "feishu", "lark", "qq", "dingtalk", "telegram", "微信", "飞书"} {
		if !resolveSendFileForwardToIM(map[string]interface{}{"destination": dest}) {
			t.Fatalf("destination=%s should forward", dest)
		}
	}
	for _, dest := range []string{"chat", "desktop", "local"} {
		if resolveSendFileForwardToIM(map[string]interface{}{"destination": dest}) {
			t.Fatalf("destination=%s should stay on desktop", dest)
		}
	}
	// Unknown destination alone does not guess.
	if resolveSendFileForwardToIM(map[string]interface{}{"destination": "somewhere"}) {
		t.Fatal("unknown destination alone must not forward")
	}
	// Unknown destination + explicit flag still honors the flag.
	if !resolveSendFileForwardToIM(map[string]interface{}{
		"destination":   "somewhere",
		"forward_to_im": true,
	}) {
		t.Fatal("unknown destination should fall through to forward_to_im")
	}
	// Explicit desktop destination wins over a true flag.
	if resolveSendFileForwardToIM(map[string]interface{}{
		"destination":   "desktop",
		"forward_to_im": true,
	}) {
		t.Fatal("explicit destination=desktop should not forward even if flag true")
	}
}

func TestApplySendFileForwardArgsNormalizesFlag(t *testing.T) {
	args := map[string]interface{}{"destination": "wechat", "path": "/tmp/a.png"}
	if !applySendFileForwardArgs(args) {
		t.Fatal("expected forward")
	}
	if v, ok := boolArgPresent(args, "forward_to_im"); !ok || !v {
		t.Fatalf("should normalize forward_to_im=true, got %#v", args["forward_to_im"])
	}
}

func TestForceSendFileToIMArgs(t *testing.T) {
	args := map[string]interface{}{"path": "/tmp/a.png"}
	forceSendFileToIMArgs(args)
	if !resolveSendFileForwardToIM(args) {
		t.Fatal("force should enable IM forward")
	}
	if dest, ok := sendFileDestinationArg(args); !ok || dest != "im" {
		t.Fatalf("destination should default to im, got %q ok=%v", dest, ok)
	}
	// Tool name wins over contradictory desktop destination.
	args2 := map[string]interface{}{"path": "/tmp/a.png", "destination": "desktop"}
	forceSendFileToIMArgs(args2)
	if !resolveSendFileForwardToIM(args2) {
		t.Fatal("force should override desktop destination")
	}
}

func TestIsSendFileFamilyTool(t *testing.T) {
	if !isSendFileFamilyTool("send_file") || !isSendFileFamilyTool("send_to_im") {
		t.Fatal("send_file and send_to_im are family")
	}
	if isSendFileFamilyTool("open") {
		t.Fatal("open is not send-file family")
	}
}

func TestClassifyAgentToolKindSendToIM(t *testing.T) {
	if classifyAgentToolKind("send_to_im") != agentToolKindSendFile {
		t.Fatal("send_to_im should classify as send-file kind")
	}
	if classifyAgentToolKind("send_file") != agentToolKindSendFile {
		t.Fatal("send_file should classify as send-file kind")
	}
	cat := classifyAgentToolKind("send_to_im").TraceCategory(toolExecutionResult{Outcome: toolOutcomeSucceeded})
	if cat != traceEvidenceCategoryFile {
		t.Fatalf("send_to_im trace category = %v, want file", cat)
	}
}

func TestDecodeToolPayloadBase64StripsWhitespace(t *testing.T) {
	// "hi" base64 is aGk=
	raw, err := decodeToolPayloadBase64("aG\nk=\n")
	if err != nil {
		t.Fatalf("decode with newlines: %v", err)
	}
	if string(raw) != "hi" {
		t.Fatalf("got %q want hi", raw)
	}
	raw, err = decodeToolPayloadBase64("aGk") // unpadded
	if err != nil {
		t.Fatalf("raw decode: %v", err)
	}
	if string(raw) != "hi" {
		t.Fatalf("got %q want hi", raw)
	}
}

func TestMediaTypeForProactiveFile(t *testing.T) {
	if mediaTypeForProactiveFile("application/octet-stream", "shot.png") != imMediaImage.String() {
		t.Fatal("png name should force image media type")
	}
	if mediaTypeForProactiveFile("image/jpeg", "x.bin") != imMediaImage.String() {
		t.Fatal("image mime should win")
	}
	if mediaTypeForProactiveFile("application/pdf", "doc.pdf") != imMediaFile.String() {
		t.Fatal("pdf should be file")
	}
}

func TestSniffProactiveMediaType(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	if sniffProactiveMediaType(png) != imMediaImage.String() {
		t.Fatal("png magic should sniff as image")
	}
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0, 0, 0, 0, 0}
	if sniffProactiveMediaType(jpeg) != imMediaImage.String() {
		t.Fatal("jpeg magic should sniff as image")
	}
	if sniffProactiveMediaType([]byte("not media!!")) != "" {
		t.Fatal("random bytes should not sniff")
	}
	id3 := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 0, 0, 0}
	if sniffProactiveMediaType(id3) != imMediaVoice.String() {
		t.Fatal("ID3 magic should sniff as voice")
	}
}

func TestBoolArgAcceptsJSONNumberAndString(t *testing.T) {
	if !boolArg(map[string]interface{}{"forward_to_im": "true"}, "forward_to_im", false) {
		t.Fatal("string true")
	}
	if !boolArg(map[string]interface{}{"forward_to_im": float64(1)}, "forward_to_im", false) {
		t.Fatal("json number 1")
	}
	if boolArg(map[string]interface{}{"forward_to_im": "false"}, "forward_to_im", true) {
		t.Fatal("string false should be false")
	}
}

func TestParseToolPayloadResultNoForwardMessageIsHonest(t *testing.T) {
	obs := parseToolPayloadResult("[file_base64|shot.png|image/png]AAAA")
	if obs.File == nil || obs.File.forwardIM {
		t.Fatalf("expected non-forward file payload, got %#v", obs.File)
	}
	if !strings.Contains(obs.ToolContent, "send_to_im") || !strings.Contains(obs.ToolContent, "微信") {
		t.Fatalf("tool content should recommend send_to_im for WeChat, got %q", obs.ToolContent)
	}
	obs2 := parseToolPayloadResult("[file_base64|shot.png|image/png|im]AAAA")
	if obs2.File == nil || !obs2.File.forwardIM {
		t.Fatalf("expected forwardIM file payload, got %#v", obs2.File)
	}
	if !strings.Contains(obs2.ToolContent, "IM") {
		t.Fatalf("forward tool content should mention IM, got %q", obs2.ToolContent)
	}
}
