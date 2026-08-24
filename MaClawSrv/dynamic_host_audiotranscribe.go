package main

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/asr"
)

// srvReviewedHostWAVEngine adapts the host ASR manager. It never imports the
// GUI asr tool name and never runs minutes/map-reduce.
type srvReviewedHostWAVEngine struct {
	models *srvAIModelManager
}

func (s srvReviewedHostWAVEngine) Ready() bool {
	if s.models == nil {
		return false
	}
	_, exists := asr.ModelFileStatus(s.models.modelPath(srvASRModelFilename))
	return exists
}

func (s srvReviewedHostWAVEngine) TranscribeWAV(ctx context.Context, wav []byte) (string, error) {
	if !s.Ready() || len(wav) == 0 {
		return "", fmt.Errorf("host_audio_transcribe_unavailable")
	}
	return s.models.transcribeWAV(ctx, corelib.AppConfig{}, wav)
}

func wireSrvReviewedHostSpeechTranscriber(executor *agentservice.CoreAgentExecutor, server *HTTPServer) {
	if executor == nil || server == nil || server.aiModels == nil {
		return
	}
	executor.SetReviewedHostSpeechTranscriber(agentservice.NewReviewedHostSpeechTranscriber(srvReviewedHostWAVEngine{models: server.aiModels}))
}
