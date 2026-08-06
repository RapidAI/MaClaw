package im

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestAdaptResponseForTextOnlyClientDropsUnsupportedMedia(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", &remoteGatewayTestSender{}, nil, nil)
	target := UserTarget{PlatformUID: "thirdparty:pet-a:default"}
	plugin.SetClientCapabilities("tenant-a", target.PlatformUID, agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text"}, Text: &agent.ClientTextCapabilities{MaxChars: 5},
	}})
	ctx := WithTenant(context.Background(), "tenant-a")
	resp, caps, ok := adaptResponseForTarget(ctx, plugin, target, &GenericResponse{Body: "上海天气晴朗", ImageKey: "image", FileData: "file", FileName: "x.pdf", VoiceData: "voice", VoiceFileName: "x.wav"})
	if !ok || resp.ImageKey != "" || resp.FileData != "" || resp.VoiceData != "" {
		t.Fatalf("adapted response=%#v ok=%t", resp, ok)
	}
	if caps.SupportsImage || caps.SupportsFile || caps.SupportsVoice || caps.MaxTextLength != 5 {
		t.Fatalf("effective capabilities=%#v", caps)
	}
	if got := truncateAtLine("上海天气晴朗", caps.MaxTextLength); got != "上海天气…" {
		t.Fatalf("Unicode truncation=%q", got)
	}
}

func TestAdaptResponsePromotesAudioFileForAudioOnlyClient(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", &remoteGatewayTestSender{}, nil, nil)
	target := UserTarget{PlatformUID: "thirdparty:pet-a:default"}
	plugin.SetClientCapabilities("tenant-a", target.PlatformUID, agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"audio"},
		Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/mpeg"}, Playback: true,
			DeliveryModes: []string{"url"}, MaxDownloadBytes: 1024,
		},
	}})
	data := base64.StdEncoding.EncodeToString([]byte("mp3"))
	resp, caps, ok := adaptResponseForTarget(WithTenant(context.Background(), "tenant-a"), plugin, target, &GenericResponse{
		FileData: data, FileName: "answer.mp3", FileMimeType: "audio/mpeg", PendingVoiceParts: 4,
	})
	if !ok || !caps.SupportsVoice || caps.SupportsFile {
		t.Fatalf("audio-only capabilities=%#v ok=%t", caps, ok)
	}
	if resp.FileData != "" || resp.VoiceData != data || resp.VoiceFileName != "answer.mp3" || resp.VoiceMimeType != "audio/mpeg" {
		t.Fatalf("audio file was not promoted to voice: %#v", resp)
	}
	if resp.PendingVoiceParts != 0 {
		t.Fatalf("stale deferred TTS count=%d, want 0", resp.PendingVoiceParts)
	}
}

func TestSendResponseRoutesAudioFileThroughVoiceForAudioOnlyThirdParty(t *testing.T) {
	sender := &remoteGatewayTestSender{}
	plugin := NewRemoteGatewayPlugin("thirdparty", sender, nil, nil)
	plugin.ClaimGatewayForTenant("tenant-a", "machine-a", "user-a")
	target := UserTarget{PlatformUID: "thirdparty:pet-a:default"}
	plugin.SetClientCapabilities("tenant-a", target.PlatformUID, agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"audio"},
		Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true,
			DeliveryModes: []string{"inline"}, MaxInlineBytes: 1024,
		},
	}})
	data := base64.StdEncoding.EncodeToString([]byte("wav"))
	(&Adapter{}).sendResponse(WithTenant(context.Background(), "tenant-a"), plugin, target, &GenericResponse{
		FileData: data, FileName: "answer.wav", FileMimeType: "audio/wav",
	})
	if len(sender.messages) != 2 {
		t.Fatalf("gateway messages=%d, want result surface plus voice", len(sender.messages))
	}
	first := sender.messages[0]["payload"].(map[string]any)["payload"].(map[string]any)
	second := sender.messages[1]["payload"].(map[string]any)["payload"].(map[string]any)
	if first["reply_type"] != "text" || second["reply_type"] != "voice" || second["file_data"] != data {
		t.Fatalf("audio delivery order/payload = %#v then %#v", first, second)
	}
}

func TestAdaptResponseHonorsOutputCombinationAndPreference(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", &remoteGatewayTestSender{}, nil, nil)
	target := UserTarget{PlatformUID: "thirdparty:screen:default"}
	plugin.SetClientCapabilities("tenant-a", target.PlatformUID, agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text", "image"}, Preferred: []string{"image", "text"},
		Combinations: [][]string{{"image"}, {"text"}},
		Text:         &agent.ClientTextCapabilities{}, Image: &agent.ClientImageCapabilities{MimeTypes: []string{"image/png"}},
	}})
	resp, _, ok := adaptResponseForTarget(WithTenant(context.Background(), "tenant-a"), plugin, target, &GenericResponse{Body: "caption", ImageKey: "image"})
	if !ok || resp.ImageKey == "" || resp.Body != "" {
		t.Fatalf("preferred response=%#v", resp)
	}
}

func TestAdaptResponseNeverLeaksTextToImageOnlyClient(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", &remoteGatewayTestSender{}, nil, nil)
	target := UserTarget{PlatformUID: "thirdparty:frame:default"}
	plugin.SetClientCapabilities("tenant-a", target.PlatformUID, agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image"}, Image: &agent.ClientImageCapabilities{MimeTypes: []string{"image/png"}},
	}})
	resp, _, ok := adaptResponseForTarget(WithTenant(context.Background(), "tenant-a"), plugin, target, &GenericResponse{Body: "not displayable", ImageKey: "image"})
	if !ok || resp.ImageKey == "" || resp.ToFallbackText() != "" {
		t.Fatalf("image-only response leaked text: %#v", resp)
	}
}

func TestAdaptResponseKeepsRichestDeclaredCombination(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", &remoteGatewayTestSender{}, nil, nil)
	target := UserTarget{PlatformUID: "thirdparty:multimodal:default"}
	plugin.SetClientCapabilities("tenant-a", target.PlatformUID, agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text", "image", "audio"}, Preferred: []string{"text", "image", "audio"},
		Combinations: [][]string{{"text"}, {"image"}, {"audio"}, {"text", "image"}},
		Text:         &agent.ClientTextCapabilities{}, Image: &agent.ClientImageCapabilities{}, Audio: &agent.ClientAudioCapabilities{Playback: true},
	}})
	resp, _, ok := adaptResponseForTarget(WithTenant(context.Background(), "tenant-a"), plugin, target, &GenericResponse{
		Body: "caption", ImageKey: "image", VoiceData: "voice", VoiceFileName: "voice.wav",
	})
	if !ok || resp.Body == "" || resp.ImageKey == "" || resp.VoiceData != "" {
		t.Fatalf("richest supported combination not selected: %#v", resp)
	}
}
