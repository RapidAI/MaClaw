package hopjit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type SQLitePersistence struct {
	db *sql.DB
}

func NewSQLitePersistence(path string) (*SQLitePersistence, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("hopjit sqlite path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLitePersistence{db: db}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS hopjit_snapshots (
		run_id TEXT PRIMARY KEY,
		state_seq INTEGER NOT NULL,
		snapshot_json TEXT NOT NULL,
		updated_at_ns INTEGER NOT NULL
	)`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLitePersistence) SaveSnapshot(ctx context.Context, snapshot EngineSnapshot) error {
	if s == nil || s.db == nil {
		return errors.New("hopjit sqlite persistence is unavailable")
	}
	if snapshot.RunID == "" || snapshot.StateSeq == 0 {
		return errors.New("hopjit snapshot identity and positive state sequence are required")
	}
	encoded, err := jsonMarshalSnapshot(snapshot)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT INTO hopjit_snapshots(run_id, state_seq, snapshot_json, updated_at_ns)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(run_id) DO UPDATE SET state_seq=excluded.state_seq, snapshot_json=excluded.snapshot_json, updated_at_ns=excluded.updated_at_ns
		WHERE excluded.state_seq > hopjit_snapshots.state_seq`, snapshot.RunID, snapshot.StateSeq, string(encoded), time.Now().UTC().UnixNano())
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrStaleSnapshot
	}
	return tx.Commit()
}

func (s *SQLitePersistence) LoadSnapshot(ctx context.Context, runID string) (EngineSnapshot, error) {
	if s == nil || s.db == nil {
		return EngineSnapshot{}, errors.New("hopjit sqlite persistence is unavailable")
	}
	var encoded string
	err := s.db.QueryRowContext(ctx, "SELECT snapshot_json FROM hopjit_snapshots WHERE run_id=?", runID).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return EngineSnapshot{}, ErrSnapshotNotFound
	}
	if err != nil {
		return EngineSnapshot{}, err
	}
	return jsonUnmarshalSnapshot([]byte(encoded))
}

func (s *SQLitePersistence) Exists(ctx context.Context, runID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, errors.New("hopjit sqlite persistence is unavailable")
	}
	var one int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM hopjit_snapshots WHERE run_id=?", runID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *SQLitePersistence) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func jsonMarshalSnapshot(snapshot EngineSnapshot) ([]byte, error) {
	clone, err := CloneSnapshot(snapshot)
	if err != nil {
		return nil, fmt.Errorf("clone hopjit snapshot: %w", err)
	}
	return json.Marshal(clone)
}

func jsonUnmarshalSnapshot(encoded []byte) (EngineSnapshot, error) {
	var snapshot EngineSnapshot
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return EngineSnapshot{}, err
	}
	return snapshot, nil
}
