package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// fileAsyncJobStore is the MaClawSrv adapter for the shared JobStore
// contract. It owns file encoding and crash-safe replacement; the manager owns
// lifecycle transitions, tenant isolation, cancellation, and retention.
type fileAsyncJobStore struct {
	path string
}

var _ agentruntime.JobStore = (*fileAsyncJobStore)(nil)

func (s *fileAsyncJobStore) Load(ctx context.Context) ([]agentruntime.Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || strings.TrimSpace(s.path) == "" {
		return nil, fmt.Errorf("job store path is required")
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		// A missing snapshot in an existing directory is a valid first start.
		// A missing/unusable parent is a storage outage and must not be
		// mistaken for an empty repository (notably on Windows, where a file in
		// the parent chain can also surface as ERROR_PATH_NOT_FOUND).
		if parentErr := validateMissingJobStoreParent(s.path); parentErr != nil {
			return nil, parentErr
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var snapshot asyncJobSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	if err := validateAsyncJobSnapshot(snapshot.Items); err != nil {
		return nil, err
	}
	items := make([]agentruntime.Job, len(snapshot.Items))
	copy(items, snapshot.Items)
	return items, nil
}

func validateMissingJobStoreParent(path string) error {
	parent := filepath.Dir(path)
	for {
		info, err := os.Stat(parent)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("job store parent is not a directory")
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return err
		}
		parent = next
	}
}

func (s *fileAsyncJobStore) Replace(ctx context.Context, items []agentruntime.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("job store path is required")
	}
	if err := validateAsyncJobSnapshot(items); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(asyncJobSnapshot{Items: append([]asyncJobView(nil), items...)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := writeDurableJobFile(tmp, payload); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func validateAsyncJobSnapshot(items []agentruntime.Job) error {
	idempotencyOwners := make(map[string]string)
	for i := range items {
		job := items[i]
		if err := agentruntime.ValidateJobRetryState(job.RetryPolicy, job.Attempt, job.NextAttemptAt); err != nil {
			return fmt.Errorf("job %q retry state: %w", job.ID, err)
		}
		if job.Checkpoint != nil {
			if err := agentruntime.ValidateJobCheckpoint(*job.Checkpoint); err != nil {
				return fmt.Errorf("job %q checkpoint: %w", job.ID, err)
			}
		}
		if err := agentruntime.ValidateJobIdempotencyState(job.IdempotencyDigest, job.RequestDigest, job.IdempotentReplay); err != nil {
			return fmt.Errorf("job %q idempotency state: %w", job.ID, err)
		}
		if job.IdempotencyDigest != "" {
			if owner, exists := idempotencyOwners[job.IdempotencyDigest]; exists {
				return fmt.Errorf("job %q duplicates idempotency identity owned by %q: %w", job.ID, owner, agentruntime.ErrInvalidJobIdempotency)
			}
			idempotencyOwners[job.IdempotencyDigest] = job.ID
		}
	}
	return nil
}

// writeDurableJobFile flushes the replacement payload before rename. A plain
// os.WriteFile can report success while data is still only in the OS cache;
// flushing here keeps the "admitted" state meaningful across a sudden
// process/host restart.
func writeDurableJobFile(path string, payload []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
