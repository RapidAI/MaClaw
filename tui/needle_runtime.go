package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/needledata"
	"github.com/RapidAI/CodeClaw/corelib/needleruntime"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

func (app *TUIApp) initNeedleRuntime() {
	if app == nil || !app.appConfig.LocalNeedleEnabled {
		return
	}
	modelPath := app.localNeedleModelPath()
	rt, err := needleruntime.New(needleruntime.Options{
		Enabled:   app.appConfig.LocalNeedleEnabled,
		ModelPath: modelPath,
		MinConf:   app.appConfig.LocalNeedleMinConfidence,
	})
	if err != nil {
		log.Printf("[needle] runtime init failed: %v", err)
		return
	}
	app.needleRuntime = rt
}

func (app *TUIApp) localNeedleModelPath() string {
	if app != nil && strings.TrimSpace(app.appConfig.LocalNeedleModelPath) != "" {
		return strings.TrimSpace(app.appConfig.LocalNeedleModelPath)
	}
	return resolveNeedleModelPath(needledata.DefaultModelDir(commands.ResolveDataDir()))
}

func resolveNeedleModelPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if _, err := os.Stat(filepath.Join(path, "manifest.json")); err == nil {
		return path
	}
	if _, err := os.Stat(filepath.Join(path, "collection.json")); err == nil {
		return path
	}
	return ""
}

func (app *TUIApp) predictNeedleWorkflowReview(text string) (*needledata.Decision, bool) {
	if app == nil {
		return nil, false
	}
	if app.needleRuntime == nil && app.appConfig.LocalNeedleEnabled {
		app.initNeedleRuntime()
	}
	if app.needleRuntime == nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	decision, ok, reason, err := app.needleRuntime.PredictDetailed(ctx, needleruntime.Request{
		Task:    needledata.EventWorkflowReview,
		Text:    text,
		Choices: []string{"confirm", "supplement", "skip", "cancel", "switch_task", "other"},
	})
	if err != nil {
		log.Printf("[needle] workflow review prediction failed: %v", err)
		return nil, false
	}
	if strings.TrimSpace(decision.Name) == "" {
		return nil, false
	}
	if !ok && strings.TrimSpace(reason) != "" {
		if decision.Arguments == nil {
			decision.Arguments = map[string]any{}
		}
		decision.Arguments["reject_reason"] = reason
	}
	return &decision, ok
}
