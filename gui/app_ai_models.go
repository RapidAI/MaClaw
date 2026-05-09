package main

import (
	"log"
	"time"
)

func boolPtr(v bool) *bool {
	return &v
}

// ensureConfiguredAIModels keeps the built-in local AI models available for a
// fresh install. Each model is downloaded before its feature is considered
// ready, and transient download failures are retried with backoff.
func (a *App) ensureConfiguredAIModels() {
	a.initTTSManager()

	backoff := 30 * time.Second
	for {
		pending := false
		startPreload := func(name string, preload func()) {
			pending = true
			log.Printf("[ai-models] %s model is missing; starting background preload", name)
			go preload()
		}

		cfg, err := a.LoadConfig()
		if err != nil {
			log.Printf("[ai-models] load config failed: %v", err)
			pending = true
		} else {
			if cfg.VectorSearchEnabled {
				if !a.vectorSearchReady() {
					startPreload("embedding", a.backgroundPreloadEmbeddingModel)
				}
			}

			if cfg.ASREnabled {
				if !modelStatusExists(a.CheckASRModel()) {
					startPreload("ASR", a.backgroundPreloadASRModel)
				}
			}

			if cfg.TTSEnabled {
				if !modelStatusExists(a.CheckTTSModel()) {
					startPreload("TTS", a.backgroundPreloadTTSModel)
				} else {
					a.initTTSManager()
				}
			}

			if a.GetScreenParsingEnabled() {
				if !modelStatusExists(a.CheckYOLOModel()) {
					startPreload("YOLO", a.backgroundPreloadYOLOModel)
				}
			}
		}

		if !pending {
			log.Println("[ai-models] all configured local AI models are downloaded and enabled")
			return
		}

		time.Sleep(backoff)
		if backoff < 10*time.Minute {
			backoff *= 2
			if backoff > 10*time.Minute {
				backoff = 10 * time.Minute
			}
		}
	}
}

func (a *App) vectorSearchConfiguredEnabled() bool {
	cfg, err := a.LoadConfig()
	return err == nil && cfg.VectorSearchEnabled
}

func (a *App) asrStillConfiguredEnabled() bool {
	cfg, err := a.LoadConfig()
	return err == nil && cfg.ASREnabled
}

func (a *App) ttsStillConfiguredEnabled() bool {
	cfg, err := a.LoadConfig()
	return err == nil && cfg.TTSEnabled
}

func (a *App) vectorSearchReady() bool {
	status := a.GetVectorSearchStatus()
	if !status.Enabled || !status.ModelExists {
		return false
	}
	if a.memoryStore == nil {
		return true
	}
	return status.EmbedderOK
}

func modelStatusExists(status map[string]interface{}) bool {
	exists, _ := status["exists"].(bool)
	return exists
}
