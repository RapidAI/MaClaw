package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/steering"
)

// initSteeringStore initializes the steering file store.
// User-level: ~/.maclaw/steering/
// Project-level: <workingDir>/.maclaw/steering/ (if a working directory is set)
//
// Called during app startup. Creates default files on first run.
func (a *App) initSteeringStore() {
	homeDir := a.GetUserHomeDir()
	userDir := filepath.Join(homeDir, ".maclaw", "steering")

	// Ensure default steering files exist.
	if err := steering.EnsureDefaults(userDir); err != nil {
		log.Printf("[steering] EnsureDefaults: %v", err)
	}

	// Project-level directory: derived from the current working directory.
	// This may be empty if no project is open.
	projectDir := ""
	if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, ".maclaw", "steering")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			projectDir = candidate
		}
	}

	a.steeringStore = steering.NewStore(userDir, projectDir)
	if err := a.steeringStore.Load(); err != nil {
		log.Printf("[steering] initial load: %v", err)
	}

	log.Printf("[steering] initialized (user=%s, project=%s, files=%d)",
		userDir, projectDir, a.steeringStore.FileCount())
}
