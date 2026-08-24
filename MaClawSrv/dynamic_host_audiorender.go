package main

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

// srvReviewedHostSpeechRenderer adapts the host TTS manager. It never
// imports GUI tts / tts_render / tts_local and never plays or sends.
type srvReviewedHostSpeechRenderer struct {
	models *srvAIModelManager
}

func (s srvReviewedHostSpeechRenderer) Ready() bool {
	if s.models == nil {
		return false
	}
	exists, _ := modelFileReady(s.models.modelPath(tts.TTSModelFilename))
	return exists
}

func (s srvReviewedHostSpeechRenderer) RenderSpeech(ctx context.Context, text string) ([]byte, error) {
	if !s.Ready() || s.models == nil {
		return nil, fmt.Errorf("host_audio_render_unavailable")
	}
	wav, _, err := s.models.synthesizeText(ctx, corelib.AppConfig{}, text)
	if err != nil || len(wav) == 0 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("host_audio_render_unavailable")
	}
	return wav, nil
}

func wireSrvReviewedHostSpeechSynthesizer(executor *agentservice.CoreAgentExecutor, server *HTTPServer) {
	if executor == nil || server == nil || server.aiModels == nil {
		return
	}
	executor.SetReviewedHostSpeechSynthesizer(srvReviewedHostSpeechRenderer{models: server.aiModels})
}
