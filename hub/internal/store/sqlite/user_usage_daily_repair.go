package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Client heartbeats report lifetime cumulative token totals. Until first
// snapshots were treated as a baseline, a new machine id (or an empty
// snapshot table) wrote that lifetime total into user_usage_daily for
// "today", so daily/weekly/monthly rankings showed all-time usage.
//
// A daily row that still holds most of any one machine snapshot and is
// implausibly large for a single day is that dump, not period usage.
const (
	lifetimeDumpMinTokens               int64 = 10_000_000
	lifetimeDumpRatioDen                int64 = 10
	userUsageDailyLifetimeDumpRepairKey       = "repair.user_usage_daily.lifetime_dumps.v1"
)

func applyUserUsageDailyLifetimeDumpRepairOnce(db *sql.DB) error {
	if db == nil {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("repair usage dumps: begin: %w", err)
	}
	defer tx.Rollback()

	var applied string
	err = tx.QueryRow(`SELECT value_json FROM system_settings WHERE key = ?`, userUsageDailyLifetimeDumpRepairKey).Scan(&applied)
	if err == nil && strings.TrimSpace(applied) != "" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("repair usage dumps: read marker: %w", err)
	}
	if err := repairUserUsageDailyLifetimeDumps(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`,
		userUsageDailyLifetimeDumpRepairKey,
		`"applied"`,
		time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("repair usage dumps: write marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("repair usage dumps: commit: %w", err)
	}
	return nil
}

func repairUserUsageDailyLifetimeDumps(db sqlExecer) error {
	if db == nil {
		return nil
	}
	// Compare each daily row against any single machine snapshot for the same
	// user. Summing snapshots first would miss two machines that dumped on
	// different days (each dump is ~50% of the combined lifetime).
	_, err := db.ExecContext(context.Background(), `
		UPDATE user_usage_daily
		   SET input_tokens = 0,
		       output_tokens = 0,
		       cached_input_tokens = 0,
		       cache_write_tokens = 0
		 WHERE (input_tokens + output_tokens) >= ?
		   AND EXISTS (
		         SELECT 1
		           FROM session_token_usage_snapshots s
		          WHERE s.tenant_id = user_usage_daily.tenant_id
		            AND trim(s.user_id) <> ''
		            AND (s.input_tokens + s.output_tokens) >= ?
		            AND (
		                  s.user_id = user_usage_daily.user_id
		               OR (
		                    trim(user_usage_daily.user_id) = ''
		                AND lower(user_usage_daily.user_email) = (
		                        SELECT lower(email)
		                          FROM users
		                         WHERE users.tenant_id = s.tenant_id
		                           AND users.id = s.user_id
		                    )
		                  )
		                )
		            AND (user_usage_daily.input_tokens + user_usage_daily.output_tokens)
		                >= (s.input_tokens + s.output_tokens)
		                 - (s.input_tokens + s.output_tokens) / ?
		       )`,
		lifetimeDumpMinTokens, lifetimeDumpMinTokens, lifetimeDumpRatioDen)
	if err != nil {
		return fmt.Errorf("repair usage dumps: %w", err)
	}
	return nil
}
