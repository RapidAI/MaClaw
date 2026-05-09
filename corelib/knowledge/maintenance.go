package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *SQLiteStore) Maintain(ctx context.Context, vacuum bool) MaintenanceResult {
	result := MaintenanceResult{StartedAt: time.Now().UTC()}
	defer func() {
		result.CompletedAt = time.Now().UTC()
	}()
	if s == nil || s.db == nil {
		result.Errors = append(result.Errors, "knowledge store is not open")
		return result
	}
	var integrity string
	if err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		result.Errors = append(result.Errors, "integrity_check: "+err.Error())
	} else {
		result.IntegrityCheck = integrity
		result.IntegrityOK = strings.EqualFold(strings.TrimSpace(integrity), "ok")
	}
	for _, table := range []string{"document_nodes_fts", "knowledge_cards_fts", "knowledge_facts_fts"} {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s(%s) VALUES('optimize')`, table, table)); err != nil {
			result.Errors = append(result.Errors, table+": "+err.Error())
			continue
		}
		result.OptimizedFTS = append(result.OptimizedFTS, table)
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		appendMaintenanceOperationError(&result, "wal_checkpoint", err)
	} else {
		result.Checkpointed = true
	}
	if vacuum {
		if _, err := s.db.ExecContext(ctx, `VACUUM`); err != nil {
			appendMaintenanceOperationError(&result, "vacuum", err)
		} else {
			result.Vacuumed = true
		}
	}
	return result
}

func appendMaintenanceOperationError(result *MaintenanceResult, operation string, err error) {
	if result == nil || err == nil {
		return
	}
	message := operation + ": " + err.Error()
	if IsSQLiteLockedError(err) {
		result.Warnings = append(result.Warnings, message)
		return
	}
	result.Errors = append(result.Errors, message)
}
