package longhorizon

// Task state persistence lives in {root}/horizon/horizon.db (see
// store_sqlite.go): one row per task with indexed owner/resumable/recency
// columns for field retrieval, and a JSON payload carrying the full state.
// task_state.json under per-task directories is only the pre-migration file:
// the first open imports every readable one and removes it.

import (
	"fmt"
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
	return saveTaskStateDB(root, state)
}

func LoadTaskState(root, taskID string) (*TaskState, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("longhorizon: missing store root")
	}
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("longhorizon: missing task id")
	}
	return loadTaskStateDB(root, taskID)
}

func FindIncompleteTask(root, ownerID string) (*TaskState, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(ownerID) == "" {
		return nil, nil
	}
	return findIncompleteTaskDB(root, ownerID)
}
