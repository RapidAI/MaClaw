package main

import (
	"fmt"
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
	if !strings.Contains(obs2.ToolContent, "forward_to_im") && !strings.Contains(obs2.ToolContent, "IM") {
		t.Fatalf("forward tool content should mention IM delivery, got %q", obs2.ToolContent)
	}
}

func TestMaterializeToolFilePayloadForwardsOnSharedPath(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var calls int
	var gotName string
	h := &IMMessageHandler{}
	h.imFileSender = func(b64Data, fileName, mimeType, message string) error {
		calls++
		gotName = fileName
		if b64Data == "" || mimeType == "" {
			t.Fatalf("expected payload mime, got mime=%q b64_len=%d", mimeType, len(b64Data))
		}
		return nil
	}
	// Minimal valid-looking PNG base64 header (same style as tool payloads).
	// Leading whitespace must still be recognized (models/tool wrappers).
	raw := "\n[file_base64|shot.png|image/png|im]iVBORw0KGgo="
	got := h.materializeToolFilePayloadIfNeeded(raw)
	if !got.Handled || !got.Forwarded {
		t.Fatalf("expected handled+forwarded, got %#v", got)
	}
	if calls != 1 {
		t.Fatalf("imFileSender calls=%d, want 1 (shared path must forward)", calls)
	}
	if gotName != "shot.png" {
		t.Fatalf("name=%q", gotName)
	}
	if !strings.Contains(got.Text, "已向微信/IM 转发") && !strings.Contains(got.Text, "Forwarded") {
		t.Fatalf("materialize text should report success, got %q", got.Text)
	}
	if len(got.LocalPaths) != 1 {
		t.Fatalf("expected local path, got %v", got.LocalPaths)
	}
	// Non-file payload passes through.
	plain := h.materializeToolFilePayloadIfNeeded("ok done")
	if plain.Handled || plain.Text != "ok done" {
		t.Fatalf("non-file should pass through, got %#v", plain)
	}
}

func TestMaterializeToolFilePayloadReportsForwardFailure(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	h := &IMMessageHandler{}
	h.imFileSender = func(b64Data, fileName, mimeType, message string) error {
		return fmt.Errorf("没有可用的微信会话：请先在微信里给机器人发一条消息，再重试发送文件")
	}
	got := h.materializeToolFilePayloadIfNeeded("[file_base64|shot.png|image/png|im]iVBORw0KGgo=")
	if !got.Handled || got.Forwarded {
		t.Fatalf("expected handled but not forwarded, got %#v", got)
	}
	if !strings.Contains(got.Text, "无法转发") || !strings.Contains(got.Text, "微信会话") {
		t.Fatalf("failure should surface honestly, got %q", got.Text)
	}
}

func TestMaterializeToolFilePayloadEmptyData(t *testing.T) {
	h := &IMMessageHandler{}
	h.imFileSender = func(b64Data, fileName, mimeType, message string) error {
		t.Fatal("should not forward empty payload")
		return nil
	}
	got := h.materializeToolFilePayloadIfNeeded("[file_base64|empty.png|image/png|im]")
	if !got.Handled || got.Forwarded {
		t.Fatalf("expected handled without forward, got %#v", got)
	}
	if !strings.Contains(got.Text, "为空") {
		t.Fatalf("empty payload text = %q", got.Text)
	}
}

func TestAppendUniqueStrings(t *testing.T) {
	got := appendUniqueStrings(nil, "a", "b", "a", " ", "b", "c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
}

func TestLocalizeIMProactiveCaption(t *testing.T) {
	// Images: bare caption (no long screenshot filename).
	zhImg := localizeIMProactiveCaption("zh", "屏幕截图 2026-06-19.png", "image/png")
	if zhImg != "请查收图片" {
		t.Fatalf("zh image caption = %q, want bare 请查收图片", zhImg)
	}
	zhFile := localizeIMProactiveCaption("zh", "report.pdf", "application/pdf")
	if !strings.Contains(zhFile, "请查收文件") || !strings.Contains(zhFile, "report.pdf") {
		t.Fatalf("zh file caption = %q", zhFile)
	}
	enImg := localizeIMProactiveCaption("en", "shot.png", "image/png")
	if enImg != "Please find the image" {
		t.Fatalf("en image caption = %q", enImg)
	}
	// Bare caption when name missing.
	if bare := localizeIMProactiveCaption("zh", "", "image/png"); bare != "请查收图片" {
		t.Fatalf("zh bare image = %q", bare)
	}
	// Legacy English bot instruction must be replaced.
	got := resolveIMProactiveCaption("zh", "Please send shot.png to the user.", "shot.png", "image/png")
	if strings.Contains(got, "Please send") || got != "请查收图片" {
		t.Fatalf("legacy instruction not replaced: %q", got)
	}
	// Previously auto-generated zh caption re-localized when GUI is en.
	re := resolveIMProactiveCaption("en", "请查收图片：shot.png", "shot.png", "image/png")
	if re != "Please find the image" {
		t.Fatalf("auto caption should re-localize to en bare, got %q", re)
	}
	// Explicit workflow message kept.
	keep := resolveIMProactiveCaption("zh", "需求文档已生成，请确认。", "a.pdf", "application/pdf")
	if keep != "需求文档已生成，请确认。" {
		t.Fatalf("workflow message should be kept, got %q", keep)
	}
	// Empty message → fresh localized caption.
	empty := resolveIMProactiveCaption("zh", "", "shot.png", "image/png")
	if empty != "请查收图片" {
		t.Fatalf("empty message caption = %q", empty)
	}
}

func TestPopulateDesktopFileArtifactUsesLocalizedCaption(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var gotCaption string
	h := &IMMessageHandler{app: &App{CurrentLanguage: "zh"}}
	h.imFileSender = func(b64Data, fileName, mimeType, message string) error {
		gotCaption = message
		return nil
	}
	resp := &IMAgentResponse{}
	n := h.populateDesktopFileArtifactResponse(resp, []pendingFile{{
		name:      "屏幕截图.png",
		mimeType:  "image/png",
		data:      "iVBORw0KGgo=",
		forwardIM: true,
		message:   "Please send 屏幕截图.png to the user.",
	}})
	if n != 1 {
		t.Fatalf("forwarded=%d", n)
	}
	if gotCaption != "请查收图片" {
		t.Fatalf("caption should be bare zh image form, got %q", gotCaption)
	}
}
