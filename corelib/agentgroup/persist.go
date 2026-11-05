package agentgroup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const runsFileName = "agent_group_runs.json"

type runsFile struct {
	Version int   `json:"version"`
	Runs    []Run `json:"runs"`
}

// FilePersist returns a PersistFunc that writes runs under dataDir.
func FilePersist(dataDir string) PersistFunc {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil
	}
	path := filepath.Join(dataDir, runsFileName)
	return func(runs []Run) error {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		// Cap history so the file stays small.
		if len(runs) > 100 {
			runs = runs[:100]
		}
		raw, err := json.MarshalIndent(runsFile{Version: 1, Runs: runs}, "", "  ")
		if err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, raw, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
}

// LoadRunsFile reads previously persisted runs.
func LoadRunsFile(dataDir string) ([]Run, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, fmt.Errorf("data dir is required")
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, runsFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file runsFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	return file.Runs, nil
}

// PlannerWithFallback tries primary; on empty/error uses fallback (usually DefaultPlan).
func PlannerWithFallback(primary Planner, fallback Planner) Planner {
	if fallback == nil {
		fallback = DefaultPlan
	}
	return func(goal string, bots []Bot) []SubTask {
		if primary != nil {
			if tasks := primary(goal, bots); len(tasks) > 0 {
				return tasks
			}
		}
		return fallback(goal, bots)
	}
}
