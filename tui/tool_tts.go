package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

// newTTSHandler returns a TTS tool handler for the TUI.
// Long text is split into semantic chunks, synthesized, concatenated, then played.
func newTTSHandler(app *TUIApp) agent.ToolHandler {
	return func(args map[string]interface{}) string {
		text, _ := args["text"].(string)
		if text == "" {
			return "缺少 text 参数"
		}

		if app.ttsManager == nil {
			return "语音合成不可用（TTS 模型未加载）。请确认 TTS 模型已下载。"
		}

		chunks := tts.PrepareSpeechChunks(text, tts.MaxLongFormSpeechRunes, 0)
		if len(chunks) == 0 {
			return "文本清理后为空，无法合成语音"
		}

		// Synthesize pre-split parts (semantic chunks + silence gaps).
		wav, nChunks, err := tts.SynthesizeSpeechParts(app.ttsManager, chunks)
		if err != nil {
			log.Printf("[tts-tool] synthesize error: %v", err)
			return fmt.Sprintf("语音合成失败: %v", err)
		}

		// Write to temp file and play.
		tmpDir := os.TempDir()
		tmpFile := filepath.Join(tmpDir, "maclaw_tts_output.wav")
		if err := os.WriteFile(tmpFile, wav, 0o644); err != nil {
			return fmt.Sprintf("写入临时文件失败: %v", err)
		}

		// Play using system default.
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", tmpFile)
		case "darwin":
			cmd = exec.Command("afplay", tmpFile)
		default:
			cmd = exec.Command("xdg-open", tmpFile)
		}
		if err := cmd.Start(); err != nil {
			return fmt.Sprintf("语音已合成到 %s，但播放失败: %v", tmpFile, err)
		}
		go cmd.Wait()

		totalRunes := 0
		for _, c := range chunks {
			totalRunes += utf8.RuneCountInString(c)
		}
		if nChunks <= 1 {
			return fmt.Sprintf("语音已合成并播放（%d 字符 → %s）", totalRunes, tmpFile)
		}
		return fmt.Sprintf("语音已分段合成并播放（%d 段 / %d 字符 → %s）", nChunks, totalRunes, tmpFile)
	}
}

// initTUITTSManager creates a TTS manager if the model file exists.
// Returns nil if the model is not downloaded (TTS will be unavailable).
func initTUITTSManager() *tts.Manager {
	dir, err := embeddingModelsDir()
	if err != nil {
		return nil
	}
	modelPath := filepath.Join(dir, tts.TTSModelFilename)
	if _, err := os.Stat(modelPath); err != nil {
		return nil // model not downloaded
	}
	return tts.NewManager(modelPath)
}

// embeddingModelsDir returns the directory for embedding/TTS models.
// Shared with GUI: <MaclawBaseDir>/models/
func embeddingModelsDir() (string, error) {
	dir := embedding.DefaultModelsDir()
	if dir == "" {
		return "", fmt.Errorf("cannot determine models directory")
	}
	return dir, os.MkdirAll(dir, 0o755)
}
