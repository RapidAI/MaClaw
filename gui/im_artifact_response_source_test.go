package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestAttachLocalPreviewMarksScreenshotResponseSource(t *testing.T) {
	resp := &IMAgentResponse{}
	attachLocalPreview(resp, `C:\tmp\screenshot.png`, "thumb")

	if resp.ResponseSource != "screenshot" {
		t.Fatalf("ResponseSource = %q, want screenshot", resp.ResponseSource)
	}
	if resp.LocalFilePath == "" || len(resp.LocalFilePaths) != 1 || resp.ThumbnailBase64 != "thumb" {
		t.Fatalf("preview fields not populated: %+v", resp)
	}
}

func TestAttachLocalPreviewPreservesExplicitResponseSource(t *testing.T) {
	resp := &IMAgentResponse{ResponseSource: "file_delivery"}
	attachLocalPreview(resp, `C:\tmp\report.pdf`, "")

	if resp.ResponseSource != "file_delivery" {
		t.Fatalf("ResponseSource = %q, want file_delivery", resp.ResponseSource)
	}
}

func TestAttachVoiceArtifactDoesNotChangeFileDeliverySource(t *testing.T) {
	resp := &IMAgentResponse{ResponseSource: "file_delivery"}
	attachVoiceArtifact(resp, "voice-data", "voice.wav", "audio/wav")

	if resp.ResponseSource != "file_delivery" {
		t.Fatalf("ResponseSource = %q, want file_delivery", resp.ResponseSource)
	}
	if resp.VoiceData == "" || resp.VoiceFileName != "voice.wav" || resp.VoiceMimeType != "audio/wav" {
		t.Fatalf("voice fields not populated: %+v", resp)
	}
}

func TestNormalizeArtifactResponseSourceInfersFileDelivery(t *testing.T) {
	resp := &IMAgentResponse{LocalFilePath: `C:\tmp\report.pdf`}
	normalizeArtifactResponseSource(resp)

	if resp.ResponseSource != "file_delivery" {
		t.Fatalf("ResponseSource = %q, want file_delivery", resp.ResponseSource)
	}
}

func TestNormalizeArtifactResponseSourceInfersScreenshot(t *testing.T) {
	resp := &IMAgentResponse{ThumbnailBase64: "thumb"}
	normalizeArtifactResponseSource(resp)

	if resp.ResponseSource != "screenshot" {
		t.Fatalf("ResponseSource = %q, want screenshot", resp.ResponseSource)
	}
}

func TestNormalizeArtifactResponseSourceOverridesAgentLoopArtifactSource(t *testing.T) {
	resp := &IMAgentResponse{ResponseSource: "agent_loop", LocalFilePath: `C:\tmp\report.pdf`}
	normalizeArtifactResponseSource(resp)

	if resp.ResponseSource != "file_delivery" {
		t.Fatalf("ResponseSource = %q, want file_delivery", resp.ResponseSource)
	}
}

func TestNormalizeArtifactResponseSourcePreservesNonAgentLoopExplicitSource(t *testing.T) {
	resp := &IMAgentResponse{ResponseSource: "ask_user", LocalFilePath: `C:\tmp\report.pdf`}
	normalizeArtifactResponseSource(resp)

	if resp.ResponseSource != "ask_user" {
		t.Fatalf("ResponseSource = %q, want ask_user", resp.ResponseSource)
	}
}

func TestNormalizeArtifactResponseSourceCanonicalizesKnownSource(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "underscore", raw: " File_Delivery ", want: "file_delivery"},
		{name: "kebab", raw: "file-delivery", want: "file_delivery"},
		{name: "camel", raw: "FileDelivery", want: "file_delivery"},
		{name: "agent loop", raw: "AgentLoop", want: "agent_loop"},
		{name: "ask user", raw: "ask-user", want: "ask_user"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &IMAgentResponse{ResponseSource: tc.raw}
			normalizeArtifactResponseSource(resp)

			if resp.ResponseSource != tc.want {
				t.Fatalf("ResponseSource = %q, want %q", resp.ResponseSource, tc.want)
			}
		})
	}
}

func TestNormalizeArtifactResponseSourceLeavesPlainTextUnchanged(t *testing.T) {
	resp := &IMAgentResponse{Text: "plain response", LocalFilePaths: []string{"  "}}
	normalizeArtifactResponseSource(resp)

	if resp.ResponseSource != "" {
		t.Fatalf("ResponseSource = %q, want empty", resp.ResponseSource)
	}
}

func TestHasVisibleIMResultIgnoresBlankLocalFilePaths(t *testing.T) {
	resp := &IMAgentResponse{LocalFilePaths: []string{"  "}}

	if hasVisibleIMResult(resp) {
		t.Fatalf("blank LocalFilePaths should not count as visible result: %+v", resp)
	}
}

func TestPopulateDesktopFileArtifactResponseMarksFileDeliverySource(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	resp := &IMAgentResponse{}
	handler := &IMMessageHandler{memory: agent.NewConversationMemory()}
	handler.populateDesktopFileArtifactResponse(resp, []pendingFile{{
		name:     "report.pdf",
		mimeType: "application/pdf",
		data:     "JVBERi0xLjQK",
	}})

	if resp.ResponseSource != "file_delivery" {
		t.Fatalf("ResponseSource = %q, want file_delivery", resp.ResponseSource)
	}
	if resp.LocalFilePath == "" || len(resp.LocalFilePaths) != 1 {
		t.Fatalf("local file paths not populated: %+v", resp)
	}
	base := filepath.Base(resp.LocalFilePath)
	if base != "report.pdf" && !strings.HasPrefix(base, "report_") {
		t.Fatalf("LocalFilePath = %q, want report.pdf or unique report_* basename", resp.LocalFilePath)
	}
	if _, err := os.Stat(resp.LocalFilePath); err != nil {
		t.Fatalf("expected saved file at %q: %v", resp.LocalFilePath, err)
	}
	// Desktop-only (no forwardIM) should still surface a clear status, not empty text.
	if resp.Text == "" || !strings.Contains(resp.Text, "send_to_im") {
		t.Fatalf("desktop-only delivery text should mention send_to_im, got %q", resp.Text)
	}
}

func TestPopulateDesktopFileArtifactResponseForwardsToIM(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var gotName, gotMime string
	var calls int
	handler := &IMMessageHandler{memory: agent.NewConversationMemory()}
	handler.imFileSender = func(b64Data, fileName, mimeType, message string) error {
		calls++
		gotName, gotMime = fileName, mimeType
		if b64Data == "" {
			t.Fatal("expected base64 payload")
		}
		return nil
	}
	resp := &IMAgentResponse{}
	handler.populateDesktopFileArtifactResponse(resp, []pendingFile{{
		name:      "shot.png",
		mimeType:  "image/png",
		data:      "iVBORw0KGgo=",
		forwardIM: true,
		message:   "请查收",
	}})
	if calls != 1 {
		t.Fatalf("imFileSender calls=%d, want 1", calls)
	}
	if gotName != "shot.png" || gotMime != "image/png" {
		t.Fatalf("forwarded name/mime = %q/%q", gotName, gotMime)
	}
	if !strings.Contains(resp.Text, "已向微信/IM 转发") {
		t.Fatalf("success text = %q", resp.Text)
	}
}

func TestPopulateDesktopFileArtifactResponseForwardErrorSurfaces(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	handler := &IMMessageHandler{memory: agent.NewConversationMemory()}
	handler.imFileSender = func(b64Data, fileName, mimeType, message string) error {
		return fmt.Errorf("没有可用的微信会话：请先在微信里给机器人发一条消息，再重试发送文件")
	}
	resp := &IMAgentResponse{}
	handler.populateDesktopFileArtifactResponse(resp, []pendingFile{{
		name:      "shot.png",
		mimeType:  "image/png",
		data:      "iVBORw0KGgo=",
		forwardIM: true,
	}})
	if !strings.Contains(resp.Text, "无法转发") || !strings.Contains(resp.Text, "微信会话") {
		t.Fatalf("forward error should surface, got %q", resp.Text)
	}
	if len(resp.LocalFilePaths) != 1 {
		t.Fatalf("file should still be saved locally, paths=%v", resp.LocalFilePaths)
	}
}

func TestUniqueLocalDeliveryPathAvoidsOverwrite(t *testing.T) {
	dir := t.TempDir()
	first := uniqueLocalDeliveryPath(dir, "a.png")
	if err := os.WriteFile(first, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := uniqueLocalDeliveryPath(dir, "a.png")
	if second == first {
		t.Fatalf("expected unique path when file exists, both %q", first)
	}
	if filepath.Ext(second) != ".png" {
		t.Fatalf("ext should be preserved: %q", second)
	}
}

func TestPopulateDesktopFileArtifactResponseUsesStructuredDeliveryMessage(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	resp := &IMAgentResponse{}
	handler := &IMMessageHandler{memory: agent.NewConversationMemory()}
	handler.populateDesktopFileArtifactResponse(resp, []pendingFile{{
		name:     "01-requirements.pdf",
		mimeType: "application/pdf",
		data:     "JVBERi0xLjQK",
		message:  "需求文档已生成",
	}})

	if resp.Text != "需求文档已生成" {
		t.Fatalf("Text = %q, want structured delivery message", resp.Text)
	}
	if resp.ResponseSource != "file_delivery" {
		t.Fatalf("ResponseSource = %q, want file_delivery", resp.ResponseSource)
	}
}

func TestHandleAgentLoopFileArtifactsMarksNonDesktopFileDeliverySource(t *testing.T) {
	resp := &IMAgentResponse{}
	handler := &IMMessageHandler{memory: agent.NewConversationMemory()}
	result := handler.handleAgentLoopFileArtifacts(
		"user",
		"web",
		[]pendingFile{{
			name:     "report.pdf",
			mimeType: "application/pdf",
			data:     "JVBERi0xLjQK",
		}},
		"",
		"",
		"",
		nil,
		true,
		func(r *IMAgentResponse) {
			resp = r
		},
	)

	if result.Response == nil {
		t.Fatal("expected response")
	}
	if result.Response != resp {
		t.Fatalf("telemetry response pointer mismatch")
	}
	if result.Response.ResponseSource != "file_delivery" {
		t.Fatalf("ResponseSource = %q, want file_delivery", result.Response.ResponseSource)
	}
	if result.Response.FileName != "report.pdf" || result.Response.FileMimeType != "application/pdf" || result.Response.FileData == "" {
		t.Fatalf("file fields not populated: %+v", result.Response)
	}
	if !result.PostStreamReturnPrepTime {
		t.Fatal("expected PostStreamReturnPrepTime")
	}
}

func TestHandleAgentLoopFileArtifactsUsesStructuredDeliveryMessage(t *testing.T) {
	handler := &IMMessageHandler{memory: agent.NewConversationMemory()}
	result := handler.handleAgentLoopFileArtifacts(
		"user",
		"web",
		[]pendingFile{{
			name:     "01-requirements.pdf",
			mimeType: "application/pdf",
			data:     "JVBERi0xLjQK",
			message:  "需求文档已生成",
		}},
		"",
		"",
		"",
		nil,
		true,
		func(r *IMAgentResponse) {},
	)

	if result.Response == nil {
		t.Fatal("expected response")
	}
	if result.Response.Text != "需求文档已生成" {
		t.Fatalf("Text = %q, want structured delivery message", result.Response.Text)
	}
}
