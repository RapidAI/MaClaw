package im

import (
	"encoding/base64"
	"testing"
)

func voiceTestData(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }

func pendingVoiceTestRouter(requestID string) (*MessageRouter, *PendingIMRequest) {
	pending := &PendingIMRequest{RequestID: requestID, ResponseCh: make(chan *AgentResponse, 1)}
	return &MessageRouter{pendingReqs: map[string]*PendingIMRequest{requestID: pending}}, pending
}

func TestAgentVoicePartsCommitInIndexOrder(t *testing.T) {
	router, pending := pendingVoiceTestRouter("voice-order")
	router.HandleAgentVoicePart("voice-order", AgentVoicePart{Index: 1, Total: 2, Part: VoicePart{Data: voiceTestData("part-2"), FileName: "2.wav", MimeType: "audio/wav"}})
	router.HandleAgentVoicePart("voice-order", AgentVoicePart{Index: 0, Total: 2, Part: VoicePart{Data: voiceTestData("part-1"), FileName: "1.wav", MimeType: "audio/wav"}})
	router.HandleAgentResponse("voice-order", &AgentResponse{Text: "完整结果"})

	resp := <-pending.ResponseCh
	if resp.Text != "完整结果" || len(resp.VoiceParts) != 2 {
		t.Fatalf("committed response=%#v", resp)
	}
	if resp.VoiceParts[0].Data != voiceTestData("part-1") || resp.VoiceParts[1].Data != voiceTestData("part-2") {
		t.Fatalf("voice order=%#v", resp.VoiceParts)
	}
}

func TestIncompleteAgentVoiceStreamFallsBackToTextOnly(t *testing.T) {
	router, pending := pendingVoiceTestRouter("voice-incomplete")
	router.HandleAgentVoicePart("voice-incomplete", AgentVoicePart{Index: 0, Total: 2, Part: VoicePart{Data: voiceTestData("part-1"), FileName: "1.wav", MimeType: "audio/wav"}})
	router.HandleAgentResponse("voice-incomplete", &AgentResponse{Text: "完整文字结果"})

	resp := <-pending.ResponseCh
	if resp.Text != "完整文字结果" || len(resp.VoiceParts) != 0 {
		t.Fatalf("incomplete stream must become text-only: %#v", resp)
	}
}

func TestInvalidAgentVoicePartPoisonsStream(t *testing.T) {
	router, pending := pendingVoiceTestRouter("voice-invalid")
	router.HandleAgentVoicePart("voice-invalid", AgentVoicePart{Index: 0, Total: 1, Part: VoicePart{Data: "", FileName: "1.wav", MimeType: "audio/wav"}})
	router.HandleAgentResponse("voice-invalid", &AgentResponse{Text: "文字仍可用"})

	resp := <-pending.ResponseCh
	if resp.Text != "文字仍可用" || len(resp.VoiceParts) != 0 {
		t.Fatalf("invalid stream must become text-only: %#v", resp)
	}
}

func TestAgentVoicePartEmitsSilentTimerHeartbeat(t *testing.T) {
	router, pending := pendingVoiceTestRouter("voice-heartbeat")
	pending.ProgressCh = make(chan string, 1)
	router.HandleAgentVoicePart("voice-heartbeat", AgentVoicePart{Index: 0, Total: 1, Part: VoicePart{
		Data: voiceTestData("part-1"), FileName: "1.wav", MimeType: "audio/wav",
	}})
	select {
	case got := <-pending.ProgressCh:
		if got != progressHeartbeat {
			t.Fatalf("heartbeat=%q", got)
		}
	default:
		t.Fatal("voice part must reset the live request timer")
	}
}

func TestConflictingDuplicateAgentVoicePartPoisonsStream(t *testing.T) {
	router, pending := pendingVoiceTestRouter("voice-duplicate")
	first := AgentVoicePart{Index: 0, Total: 1, Part: VoicePart{Data: voiceTestData("first"), FileName: "1.wav", MimeType: "audio/wav"}}
	router.HandleAgentVoicePart("voice-duplicate", first)
	first.Part.Data = voiceTestData("different")
	router.HandleAgentVoicePart("voice-duplicate", first)
	router.HandleAgentResponse("voice-duplicate", &AgentResponse{Text: "文字结果"})
	resp := <-pending.ResponseCh
	if len(resp.VoiceParts) != 0 || resp.Text != "文字结果" {
		t.Fatalf("conflicting duplicate must become text-only: %#v", resp)
	}
}
