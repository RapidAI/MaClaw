package main

import (
	"context"
	"log"

	"github.com/RapidAI/CodeClaw/corelib"
)

// IWorkerGoalWatchService is kept as a small compatibility shell when the
// iWorkerCenter integration is not linked into the desktop build. The App still
// owns startup/shutdown hooks, so these no-op methods keep the rest of the GUI
// buildable without silently starting a half-configured watcher.
type IWorkerGoalWatchService struct{}

func (s *IWorkerGoalWatchService) Stop() {}

func (a *App) startIWorkerGoalWatchIfConfigured(_ corelib.AppConfig) {
	if a == nil {
		return
	}
	a.iworkerGoalWatchMu.Lock()
	defer a.iworkerGoalWatchMu.Unlock()
	if a.iworkerGoalWatch == nil {
		a.iworkerGoalWatch = &IWorkerGoalWatchService{}
	}
	log.Printf("[iworker-goalwatch] integration unavailable in this build; watcher disabled")
}

func (a *App) stopIWorkerGoalWatch() {
	if a == nil {
		return
	}
	a.iworkerGoalWatchMu.Lock()
	defer a.iworkerGoalWatchMu.Unlock()
	if a.iworkerGoalWatch != nil {
		a.iworkerGoalWatch.Stop()
		a.iworkerGoalWatch = nil
	}
}

func (s *IWorkerGoalWatchService) Start(context.Context) bool { return false }
