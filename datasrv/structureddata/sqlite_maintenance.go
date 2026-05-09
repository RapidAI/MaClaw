package structureddata

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *SQLiteStore) RunMaintenance(ctx context.Context, in RunMaintenanceInput, now time.Time) (*MaintenanceResult, error) {
	tasks := normalizeMaintenanceTasks(in.Tasks)
	result := &MaintenanceResult{Engine: "sqlite", Valid: true, StartedAt: now, Tasks: []MaintenanceTaskResult{}}
	for _, task := range tasks {
		item := MaintenanceTaskResult{Task: task, Status: "ok", StartedAt: time.Now().UTC()}
		switch task {
		case "integrity_check":
			message, valid, err := s.runIntegrityCheck(ctx)
			item.Message = message
			if !valid {
				item.Status = "failed"
				result.Valid = false
			}
			if err != nil {
				item.Status = "failed"
				item.Message = err.Error()
				result.Valid = false
			}
		case "optimize":
			if _, err := s.db.ExecContext(ctx, `PRAGMA optimize`); err != nil {
				item.Status = "failed"
				item.Message = err.Error()
				result.Valid = false
			} else {
				item.Message = "optimize completed"
			}
		case "vacuum":
			if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
				item.Status = "failed"
				item.Message = err.Error()
				result.Valid = false
			} else {
				item.Message = "vacuum completed"
			}
		default:
			item.Status = "failed"
			item.Message = "unsupported task"
			result.Valid = false
		}
		item.FinishedAt = time.Now().UTC()
		item.DurationMS = item.FinishedAt.Sub(item.StartedAt).Milliseconds()
		result.Tasks = append(result.Tasks, item)
	}
	result.FinishedAt = time.Now().UTC()
	return result, nil
}

func normalizeMaintenanceTasks(tasks []string) []string {
	if len(tasks) == 0 {
		return []string{"integrity_check", "optimize"}
	}
	seen := map[string]struct{}{}
	out := []string{}
	for _, task := range tasks {
		task = strings.ToLower(strings.TrimSpace(task))
		switch task {
		case "integrity", "check":
			task = "integrity_check"
		}
		if task == "" {
			continue
		}
		if _, ok := seen[task]; ok {
			continue
		}
		seen[task] = struct{}{}
		out = append(out, task)
	}
	if len(out) == 0 {
		return []string{"integrity_check", "optimize"}
	}
	return out
}

func (s *SQLiteStore) runIntegrityCheck(ctx context.Context) (string, bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	messages := []string{}
	for rows.Next() {
		var message string
		if err := rows.Scan(&message); err != nil {
			return "", false, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if len(messages) == 0 {
		return "no result", false, nil
	}
	message := strings.Join(messages, "; ")
	return message, strings.EqualFold(strings.TrimSpace(message), "ok"), nil
}

func maintenanceTaskList(tasks []MaintenanceTaskResult) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, fmt.Sprintf("%s:%s", task.Task, task.Status))
	}
	return out
}
