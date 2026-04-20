package main

import (
	"strings"
	"testing"
)

func TestStripHistoryAttachments_OpenAIMultimodal(t *testing.T) {
	msg := map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": "图中有什么？\n\n[用户选择的本地文件路径]\nC:\\Users\\test\\logo.png\n请直接使用这些路径。",
			},
			map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": "data:image/png;base64,iVBORw0KGgo..."},
			},
		},
	}

	result := stripHistoryAttachments(msg)
	rm := result.(map[string]interface{})
	blocks := rm["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}

	textBlock := blocks[0].(map[string]interface{})
	text := textBlock["text"].(string)
	if strings.Contains(text, "[用户选择的本地文件路径]") {
		t.Error("expected file path prefix to be replaced")
	}
	if !strings.Contains(text, filePathPromptPrefixHistorical) {
		t.Error("expected historical file path prefix")
	}

	imgBlock := blocks[1].(map[string]interface{})
	if imgBlock["type"] != "text" {
		t.Errorf("expected image block replaced with text, got type=%v", imgBlock["type"])
	}
	if imgBlock["text"] != "[之前上传了 1 张图片]" {
		t.Errorf("unexpected placeholder: %v", imgBlock["text"])
	}
}

func TestStripHistoryAttachments_MultipleImages(t *testing.T) {
	msg := map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "看看这些图"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,AAA"}},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,BBB"}},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,CCC"}},
		},
	}

	result := stripHistoryAttachments(msg)
	rm := result.(map[string]interface{})
	blocks := rm["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	placeholder := blocks[1].(map[string]interface{})
	if placeholder["text"] != "[之前上传了 3 张图片]" {
		t.Errorf("expected merged placeholder for 3 images, got: %v", placeholder["text"])
	}
}

func TestStripHistoryAttachments_AnthropicMultimodal(t *testing.T) {
	msg := map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "看看这张图"},
			map[string]interface{}{
				"type":   "image",
				"source": map[string]interface{}{"type": "base64", "media_type": "image/jpeg", "data": "base64data..."},
			},
		},
	}

	result := stripHistoryAttachments(msg)
	rm := result.(map[string]interface{})
	blocks := rm["content"].([]interface{})
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[1].(map[string]interface{})["text"] != "[之前上传了 1 张图片]" {
		t.Errorf("unexpected placeholder: %v", blocks[1])
	}
}

func TestStripHistoryAttachments_PlainTextWithFilePaths(t *testing.T) {
	msg := map[string]interface{}{
		"role":    "user",
		"content": "图中有什么？\n\n[用户选择的本地文件路径]\nC:\\test\\img.png\n请直接使用这些路径。",
	}

	result := stripHistoryAttachments(msg)
	rm := result.(map[string]interface{})
	text := rm["content"].(string)
	if strings.Contains(text, "[用户选择的本地文件路径]") {
		t.Error("expected file path prefix to be replaced")
	}
	if !strings.Contains(text, filePathPromptPrefixHistorical) {
		t.Error("expected historical prefix")
	}
}

func TestStripHistoryAttachments_NoOpForAssistant(t *testing.T) {
	msg := map[string]interface{}{"role": "assistant", "content": "这是一张logo图片。"}
	result := stripHistoryAttachments(msg)
	if result.(map[string]interface{})["content"] != "这是一张logo图片。" {
		t.Error("assistant messages should not be modified")
	}
}

func TestStripHistoryAttachments_NoOpForPlainText(t *testing.T) {
	msg := map[string]interface{}{"role": "user", "content": "你好，帮我写个函数"}
	result := stripHistoryAttachments(msg)
	if result.(map[string]interface{})["content"] != "你好，帮我写个函数" {
		t.Error("plain text without attachments should not be modified")
	}
}

func TestStripHistoryAttachments_PreservesNonImageBlocks(t *testing.T) {
	msg := map[string]interface{}{
		"role":    "user",
		"content": []interface{}{map[string]interface{}{"type": "text", "text": "普通文本消息"}},
	}
	result := stripHistoryAttachments(msg)
	blocks := result.(map[string]interface{})["content"].([]interface{})
	if blocks[0].(map[string]interface{})["text"] != "普通文本消息" {
		t.Error("non-image blocks should not be modified")
	}
}

func TestStripHistoryAttachments_IMAttachmentDescriptions(t *testing.T) {
	msg := map[string]interface{}{
		"role":    "user",
		"content": "帮我看看\n\n[附件: report.pdf → 已保存到 /tmp/report.pdf]",
	}
	result := stripHistoryAttachments(msg)
	text := result.(map[string]interface{})["content"].(string)
	if strings.Contains(text, "[附件:") {
		t.Error("expected [附件: replaced")
	}
	if !strings.Contains(text, "[之前的附件:") {
		t.Error("expected historical attachment marker")
	}
}

func TestStripHistoryAttachments_IMImageFallback(t *testing.T) {
	msg := map[string]interface{}{
		"role":    "user",
		"content": "看看\n\n[用户发送了图片 photo.jpg，已保存到 /tmp/photo.jpg，当前模型不支持图片理解]",
	}
	result := stripHistoryAttachments(msg)
	text := result.(map[string]interface{})["content"].(string)
	if strings.Contains(text, "[用户发送了图片") {
		t.Error("expected image fallback replaced")
	}
	if !strings.Contains(text, "[之前发送的图片") {
		t.Error("expected historical image fallback marker")
	}
}

func TestStripHistoryAttachments_IMVoice(t *testing.T) {
	msg := map[string]interface{}{
		"role":    "user",
		"content": "听听\n\n[语音: voice.ogg → 已转换为WAV并保存到 /tmp/voice.wav，请使用ASR工具进行语音识别]",
	}
	result := stripHistoryAttachments(msg)
	text := result.(map[string]interface{})["content"].(string)
	if strings.Contains(text, "[语音:") {
		t.Error("expected voice marker replaced")
	}
	if !strings.Contains(text, "[之前的语音:") {
		t.Error("expected historical voice marker")
	}
}

func TestStripHistoryAttachments_MixedAttachments(t *testing.T) {
	msg := map[string]interface{}{
		"role": "user",
		"content": "处理这些文件\n\n" +
			"[用户选择的本地文件路径]\nC:\\docs\\report.pdf\n请直接使用这些路径。\n\n" +
			"[附件: extra.docx → 已保存到 /tmp/extra.docx]",
	}
	result := stripHistoryAttachments(msg)
	text := result.(map[string]interface{})["content"].(string)
	if strings.Contains(text, "[用户选择的本地文件路径]") {
		t.Error("file path prefix should be replaced")
	}
	if strings.Contains(text, "[附件:") {
		t.Error("attachment marker should be replaced")
	}
	if !strings.Contains(text, filePathPromptPrefixHistorical) {
		t.Error("expected historical file path prefix")
	}
	if !strings.Contains(text, "[之前的附件:") {
		t.Error("expected historical attachment marker")
	}
}
