package longhorizon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func TaskDir(root, taskID string) string {
	return filepath.Join(root, "horizon", taskID)
}

func SaveTaskState(root string, state *TaskState) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("longhorizon: missing store root")
	}
	if state == nil || strings.TrimSpace(state.TaskID) == "" {
		return fmt.Errorf("longhorizon: missing task id")
	}
	state.Carryover = ClipCarryover(state.Carryover)
	state.UserGoal = Clip(state.UserGoal, GoalCap)
	state.UpdatedAt = time.Now().UTC()
	dir := TaskDir(root, state.TaskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "task_state.json.tmp")
	final := filepath.Join(dir, "task_state.json")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

func LoadTaskState(root, taskID string) (*TaskState, error) {
	path := filepath.Join(TaskDir(root, taskID), "task_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state TaskState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	state.Carryover = ClipCarryover(state.Carryover)
	return &state, nil
}

func FindIncompleteTask(root, ownerID string) (*TaskState, error) {
	base := filepath.Join(root, "horizon")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var newest *TaskState
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, err := LoadTaskState(root, entry.Name())
		if err != nil || state == nil {
			continue
		}
		if state.Policy.OwnerID != ownerID {
			continue
		}
		if !Resumable(state) {
			continue
		}
		if newest == nil || state.UpdatedAt.After(newest.UpdatedAt) {
			newest = state
		}
	}
	return newest, nil
}
