package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type skillArtifactRegistryRecord struct {
	URI           string
	RunID         string
	OwnerID       string
	Skill         string
	ArtifactID    string
	Name          string
	Path          string
	MimeType      string
	SizeBytes     int64
	RemoteURL     string
	Checksum      string
	DownloadState string
	Status        string
	Presentation  string
	CreatedAt     string
	UpdatedAt     string
}

type SkillArtifactRegistryEntry struct {
	URI           string `json:"uri"`
	RunID         string `json:"run_id"`
	OwnerID       string `json:"owner_id,omitempty"`
	Skill         string `json:"skill,omitempty"`
	ArtifactID    string `json:"artifact_id"`
	Name          string `json:"name,omitempty"`
	MimeType      string `json:"mime_type,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	Checksum      string `json:"checksum,omitempty"`
	DownloadState string `json:"download_state,omitempty"`
	Status        string `json:"status,omitempty"`
	Presentation  string `json:"presentation,omitempty"`
	Available     bool   `json:"available"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

func (a *App) skillArtifactRegistryDBPath() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "skill_artifacts.db")
}

func openSkillArtifactRegistryDB(dbPath string) (*sql.DB, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("artifact registry db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("artifact registry mkdir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("artifact registry open: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("artifact registry pragma: %w", err)
		}
	}
	if err := ensureSkillArtifactRegistrySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSkillArtifactRegistrySchema(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("artifact registry db is nil")
	}
	schema := []string{
		`CREATE TABLE IF NOT EXISTS skill_run_artifacts (
			uri TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			owner_id TEXT,
			skill TEXT,
			artifact_id TEXT NOT NULL,
			name TEXT,
			path TEXT NOT NULL,
			mime_type TEXT,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			remote_url TEXT NOT NULL DEFAULT '',
			checksum TEXT NOT NULL DEFAULT '',
			download_state TEXT NOT NULL DEFAULT 'downloaded',
			status TEXT,
			presentation TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_skill_run_artifacts_run_artifact ON skill_run_artifacts(run_id, artifact_id)`,
		`CREATE INDEX IF NOT EXISTS idx_skill_run_artifacts_run ON skill_run_artifacts(run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_skill_run_artifacts_owner ON skill_run_artifacts(owner_id, updated_at)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("artifact registry schema: %w", err)
		}
	}
	for _, migration := range []string{
		`ALTER TABLE skill_run_artifacts ADD COLUMN owner_id TEXT`,
		`ALTER TABLE skill_run_artifacts ADD COLUMN skill TEXT`,
		`ALTER TABLE skill_run_artifacts ADD COLUMN remote_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE skill_run_artifacts ADD COLUMN checksum TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE skill_run_artifacts ADD COLUMN download_state TEXT NOT NULL DEFAULT 'downloaded'`,
	} {
		_, _ = db.Exec(migration)
	}
	return nil
}

func (a *App) registerSkillRunArtifacts(status *SkillRunStatus) {
	if a == nil || status == nil || len(status.Artifacts) == 0 {
		return
	}
	db, err := openSkillArtifactRegistryDB(a.skillArtifactRegistryDBPath())
	if err != nil {
		return
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, artifact := range status.Artifacts {
		record := skillArtifactRegistryRecord{
			URI:           strings.TrimSpace(artifact.URI),
			RunID:         strings.TrimSpace(status.RunID),
			OwnerID:       strings.TrimSpace(status.OwnerID),
			Skill:         strings.TrimSpace(status.Skill),
			ArtifactID:    strings.TrimSpace(artifact.ID),
			Name:          strings.TrimSpace(artifact.Name),
			Path:          strings.TrimSpace(artifact.Path),
			MimeType:      strings.TrimSpace(artifact.MimeType),
			SizeBytes:     artifact.SizeBytes,
			RemoteURL:     strings.TrimSpace(artifact.RemoteURL),
			Checksum:      strings.TrimSpace(artifact.Checksum),
			DownloadState: strings.TrimSpace(artifact.DownloadState),
			Status:        strings.TrimSpace(artifact.Status.String()),
			Presentation:  strings.TrimSpace(artifact.Presentation),
			UpdatedAt:     now,
		}
		if record.DownloadState == "" {
			if record.Path != "" {
				record.DownloadState = "downloaded"
			} else if record.RemoteURL != "" {
				record.DownloadState = "remote"
			}
		}
		if record.URI == "" {
			record.URI = skillRunArtifactURI(record.RunID, record.ArtifactID)
		}
		if record.URI == "" || record.RunID == "" || record.ArtifactID == "" || (record.Path == "" && record.RemoteURL == "") {
			continue
		}
		_, _ = db.ExecContext(context.Background(), `INSERT INTO skill_run_artifacts
			(uri, run_id, owner_id, skill, artifact_id, name, path, mime_type, size_bytes, remote_url, checksum, download_state, status, presentation, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(uri) DO UPDATE SET
				run_id=excluded.run_id,
				owner_id=excluded.owner_id,
				skill=excluded.skill,
				artifact_id=excluded.artifact_id,
				name=excluded.name,
				path=excluded.path,
				mime_type=excluded.mime_type,
				size_bytes=excluded.size_bytes,
				remote_url=excluded.remote_url,
				checksum=excluded.checksum,
				download_state=excluded.download_state,
				status=excluded.status,
				presentation=excluded.presentation,
				updated_at=excluded.updated_at`,
			record.URI, record.RunID, record.OwnerID, record.Skill, record.ArtifactID, record.Name, record.Path, record.MimeType, record.SizeBytes, record.RemoteURL, record.Checksum, record.DownloadState, record.Status, record.Presentation, now, record.UpdatedAt)
	}
}

func (a *App) lookupSkillArtifactPath(runID, artifactRef string) (string, error) {
	return a.lookupSkillArtifactPathForOwner("", runID, artifactRef)
}

func (a *App) GetSkillRunArtifact(runID, artifactRef string) (*SkillArtifactRegistryEntry, error) {
	return a.GetSkillRunArtifactForOwner("", runID, artifactRef)
}

const skillArtifactRegistrySelectColumns = `uri, run_id, COALESCE(owner_id, ''), COALESCE(skill, ''), artifact_id, COALESCE(name, ''), path, COALESCE(mime_type, ''), size_bytes, COALESCE(remote_url, ''), COALESCE(checksum, ''), COALESCE(download_state, ''), COALESCE(status, ''), COALESCE(presentation, ''), created_at, updated_at`

func (a *App) GetSkillRunArtifactForOwner(ownerID, runID, artifactRef string) (*SkillArtifactRegistryEntry, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	ownerID = strings.TrimSpace(ownerID)
	runID = strings.TrimSpace(runID)
	artifactRef = strings.TrimSpace(artifactRef)
	artifactID := skillRunArtifactIDFromRef(artifactRef)
	uri := artifactRef
	if uri == "" && runID != "" && artifactID != "" {
		uri = skillRunArtifactURI(runID, artifactID)
	}
	db, err := openSkillArtifactRegistryDB(a.skillArtifactRegistryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var row *sql.Row
	if strings.HasPrefix(strings.ToLower(uri), "artifact://") {
		row = db.QueryRowContext(context.Background(), `SELECT `+skillArtifactRegistrySelectColumns+` FROM skill_run_artifacts WHERE uri = ?`, uri)
	} else if runID != "" && artifactID != "" {
		row = db.QueryRowContext(context.Background(), `SELECT `+skillArtifactRegistrySelectColumns+` FROM skill_run_artifacts WHERE run_id = ? AND artifact_id = ?`, runID, artifactID)
	} else {
		return nil, fmt.Errorf("artifact reference is required")
	}
	record, err := scanSkillArtifactRegistryRow(row)
	if err != nil {
		return nil, err
	}
	if ownerID != "" && strings.TrimSpace(record.OwnerID) != "" && ownerID != strings.TrimSpace(record.OwnerID) {
		return nil, fmt.Errorf("artifact owner mismatch")
	}
	entry := skillArtifactRegistryEntryFromRecord(record)
	return &entry, nil
}

func (a *App) ListSkillRunArtifacts(runID string, limit int) ([]SkillArtifactRegistryEntry, error) {
	return a.ListSkillRunArtifactsForOwner("", runID, limit)
}

func (a *App) UpdateSkillRunArtifactCache(runID, artifactRef, localPath, checksum string) (*SkillArtifactRegistryEntry, error) {
	return a.UpdateSkillRunArtifactCacheForOwner("", runID, artifactRef, localPath, checksum)
}

func (a *App) UpdateSkillRunArtifactCacheForOwner(ownerID, runID, artifactRef, localPath, checksum string) (*SkillArtifactRegistryEntry, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	ownerID = strings.TrimSpace(ownerID)
	runID = strings.TrimSpace(runID)
	artifactRef = strings.TrimSpace(artifactRef)
	localPath = strings.TrimSpace(localPath)
	checksum = strings.TrimSpace(checksum)
	if localPath == "" {
		return nil, fmt.Errorf("local artifact path is required")
	}
	if info, err := os.Stat(localPath); err != nil || info.IsDir() {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("artifact path is a directory")
	}
	artifactID := skillRunArtifactIDFromRef(artifactRef)
	uri := artifactRef
	if uri == "" && runID != "" && artifactID != "" {
		uri = skillRunArtifactURI(runID, artifactID)
	}
	if uri == "" && (runID == "" || artifactID == "") {
		return nil, fmt.Errorf("artifact reference is required")
	}
	db, err := openSkillArtifactRegistryDB(a.skillArtifactRegistryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var artifactOwner string
	if strings.HasPrefix(strings.ToLower(uri), "artifact://") {
		err = db.QueryRowContext(context.Background(), `SELECT COALESCE(owner_id, '') FROM skill_run_artifacts WHERE uri = ?`, uri).Scan(&artifactOwner)
	} else {
		err = db.QueryRowContext(context.Background(), `SELECT COALESCE(owner_id, '') FROM skill_run_artifacts WHERE run_id = ? AND artifact_id = ?`, runID, artifactID).Scan(&artifactOwner)
	}
	if err != nil {
		return nil, err
	}
	if ownerID != "" && strings.TrimSpace(artifactOwner) != "" && ownerID != strings.TrimSpace(artifactOwner) {
		return nil, fmt.Errorf("artifact owner mismatch")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.HasPrefix(strings.ToLower(uri), "artifact://") {
		_, err = db.ExecContext(context.Background(), `UPDATE skill_run_artifacts SET path = ?, checksum = CASE WHEN ? <> '' THEN ? ELSE checksum END, download_state = 'downloaded', updated_at = ? WHERE uri = ?`, localPath, checksum, checksum, now, uri)
	} else {
		_, err = db.ExecContext(context.Background(), `UPDATE skill_run_artifacts SET path = ?, checksum = CASE WHEN ? <> '' THEN ? ELSE checksum END, download_state = 'downloaded', updated_at = ? WHERE run_id = ? AND artifact_id = ?`, localPath, checksum, checksum, now, runID, artifactID)
	}
	if err != nil {
		return nil, err
	}
	return a.GetSkillRunArtifactForOwner(ownerID, runID, artifactRef)
}

func (a *App) ListSkillRunArtifactsForOwner(ownerID, runID string, limit int) ([]SkillArtifactRegistryEntry, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	ownerID = strings.TrimSpace(ownerID)
	runID = strings.TrimSpace(runID)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	db, err := openSkillArtifactRegistryDB(a.skillArtifactRegistryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	baseQuery := `SELECT ` + skillArtifactRegistrySelectColumns + ` FROM skill_run_artifacts`
	var rows *sql.Rows
	if ownerID != "" && runID != "" {
		rows, err = db.QueryContext(context.Background(), baseQuery+` WHERE run_id = ? AND (owner_id = '' OR owner_id = ?) ORDER BY updated_at DESC LIMIT ?`, runID, ownerID, limit)
	} else if ownerID != "" {
		rows, err = db.QueryContext(context.Background(), baseQuery+` WHERE owner_id = '' OR owner_id = ? ORDER BY updated_at DESC LIMIT ?`, ownerID, limit)
	} else if runID != "" {
		rows, err = db.QueryContext(context.Background(), baseQuery+` WHERE run_id = ? ORDER BY updated_at DESC LIMIT ?`, runID, limit)
	} else {
		rows, err = db.QueryContext(context.Background(), baseQuery+` ORDER BY updated_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]SkillArtifactRegistryEntry, 0)
	for rows.Next() {
		record, err := scanSkillArtifactRegistryRows(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, skillArtifactRegistryEntryFromRecord(record))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (a *App) lookupSkillArtifactPathForOwner(ownerID, runID, artifactRef string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is nil")
	}
	ownerID = strings.TrimSpace(ownerID)
	runID = strings.TrimSpace(runID)
	artifactRef = strings.TrimSpace(artifactRef)
	artifactID := skillRunArtifactIDFromRef(artifactRef)
	uri := artifactRef
	if uri == "" && runID != "" && artifactID != "" {
		uri = skillRunArtifactURI(runID, artifactID)
	}
	db, err := openSkillArtifactRegistryDB(a.skillArtifactRegistryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	var path, artifactOwner string
	if strings.HasPrefix(strings.ToLower(uri), "artifact://") {
		err = db.QueryRowContext(context.Background(), `SELECT path, COALESCE(owner_id, '') FROM skill_run_artifacts WHERE uri = ?`, uri).Scan(&path, &artifactOwner)
	} else if runID != "" && artifactID != "" {
		err = db.QueryRowContext(context.Background(), `SELECT path, COALESCE(owner_id, '') FROM skill_run_artifacts WHERE run_id = ? AND artifact_id = ?`, runID, artifactID).Scan(&path, &artifactOwner)
	} else {
		return "", fmt.Errorf("artifact reference is required")
	}
	if err != nil {
		return "", err
	}
	if ownerID != "" && strings.TrimSpace(artifactOwner) != "" && ownerID != strings.TrimSpace(artifactOwner) {
		return "", fmt.Errorf("artifact owner mismatch")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("artifact path is empty")
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("artifact path is a directory")
	}
	return path, nil
}

func (a *App) CleanupSkillArtifactRegistry(maxAgeDays int, removeMissing bool) (map[string]int64, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	db, err := openSkillArtifactRegistryDB(a.skillArtifactRegistryDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	result := map[string]int64{"expired": 0, "missing": 0}
	if maxAgeDays > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -maxAgeDays).Format(time.RFC3339)
		expiredRows, err := db.QueryContext(context.Background(), `SELECT path FROM skill_run_artifacts WHERE updated_at < ?`, cutoff)
		if err != nil {
			return result, err
		}
		var expiredPaths []string
		for expiredRows.Next() {
			var path string
			if err := expiredRows.Scan(&path); err == nil {
				expiredPaths = append(expiredPaths, path)
			}
		}
		if err := expiredRows.Err(); err != nil {
			_ = expiredRows.Close()
			return result, err
		}
		_ = expiredRows.Close()
		res, err := db.ExecContext(context.Background(), `DELETE FROM skill_run_artifacts WHERE updated_at < ?`, cutoff)
		if err != nil {
			return result, err
		}
		if n, err := res.RowsAffected(); err == nil {
			result["expired"] = n
		}
		for _, path := range expiredPaths {
			a.removeExpiredSkillAppOutputFile(path)
		}
	}
	if removeMissing {
		rows, err := db.QueryContext(context.Background(), `SELECT uri, path, COALESCE(download_state, '') FROM skill_run_artifacts`)
		if err != nil {
			return result, err
		}
		type stale struct{ uri, path string }
		var staleRows []stale
		for rows.Next() {
			var item stale
			var downloadState string
			if err := rows.Scan(&item.uri, &item.path, &downloadState); err == nil {
				path := strings.TrimSpace(item.path)
				state := strings.ToLower(strings.TrimSpace(downloadState))
				if path == "" && state != "downloaded" && state != "local" {
					continue
				}
				if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
					staleRows = append(staleRows, item)
				}
			}
		}
		_ = rows.Close()
		for _, item := range staleRows {
			if _, err := db.ExecContext(context.Background(), `DELETE FROM skill_run_artifacts WHERE uri = ?`, item.uri); err == nil {
				result["missing"]++
			}
		}
	}
	return result, nil
}

func (a *App) removeExpiredSkillAppOutputFile(path string) {
	path = strings.TrimSpace(path)
	if path == "" || !a.isSkillAppOutputPath(path) {
		return
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[skill-artifacts] remove expired app output failed path=%q err=%v", path, err)
		return
	}
	parent := filepath.Dir(path)
	root := filepath.Clean(filepath.Join(a.GetDataDir(), "app-outputs"))
	if parent != root {
		_ = os.Remove(parent)
	}
}

func (a *App) isSkillAppOutputPath(path string) bool {
	if a == nil {
		return false
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	root := filepath.Clean(filepath.Join(a.GetDataDir(), "app-outputs"))
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(root, cleanPath)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (a *App) startSkillArtifactRegistryMaintenance(ctx context.Context) {
	if a == nil {
		return
	}
	go func() {
		runCleanup := func() {
			result, err := a.CleanupSkillArtifactRegistry(30, true)
			if err != nil {
				log.Printf("[skill-artifacts] cleanup failed: %v", err)
				return
			}
			if result["expired"] > 0 || result["missing"] > 0 {
				log.Printf("[skill-artifacts] cleanup removed expired=%d missing=%d", result["expired"], result["missing"])
			}
		}
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			runCleanup()
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runCleanup()
			}
		}
	}()
}

func scanSkillArtifactRegistryRow(row *sql.Row) (skillArtifactRegistryRecord, error) {
	var record skillArtifactRegistryRecord
	if row == nil {
		return record, fmt.Errorf("artifact registry row is nil")
	}
	err := row.Scan(&record.URI, &record.RunID, &record.OwnerID, &record.Skill, &record.ArtifactID, &record.Name, &record.Path, &record.MimeType, &record.SizeBytes, &record.RemoteURL, &record.Checksum, &record.DownloadState, &record.Status, &record.Presentation, &record.CreatedAt, &record.UpdatedAt)
	return record, err
}

func scanSkillArtifactRegistryRows(rows *sql.Rows) (skillArtifactRegistryRecord, error) {
	var record skillArtifactRegistryRecord
	if rows == nil {
		return record, fmt.Errorf("artifact registry rows is nil")
	}
	err := rows.Scan(&record.URI, &record.RunID, &record.OwnerID, &record.Skill, &record.ArtifactID, &record.Name, &record.Path, &record.MimeType, &record.SizeBytes, &record.RemoteURL, &record.Checksum, &record.DownloadState, &record.Status, &record.Presentation, &record.CreatedAt, &record.UpdatedAt)
	return record, err
}

func skillArtifactRegistryEntryFromRecord(record skillArtifactRegistryRecord) SkillArtifactRegistryEntry {
	return SkillArtifactRegistryEntry{
		URI:           strings.TrimSpace(record.URI),
		RunID:         strings.TrimSpace(record.RunID),
		OwnerID:       strings.TrimSpace(record.OwnerID),
		Skill:         strings.TrimSpace(record.Skill),
		ArtifactID:    strings.TrimSpace(record.ArtifactID),
		Name:          strings.TrimSpace(record.Name),
		MimeType:      strings.TrimSpace(record.MimeType),
		SizeBytes:     record.SizeBytes,
		RemoteURL:     strings.TrimSpace(record.RemoteURL),
		Checksum:      strings.TrimSpace(record.Checksum),
		DownloadState: strings.TrimSpace(record.DownloadState),
		Status:        strings.TrimSpace(record.Status),
		Presentation:  strings.TrimSpace(record.Presentation),
		Available:     skillArtifactRegistryPathAvailable(record.Path),
		CreatedAt:     strings.TrimSpace(record.CreatedAt),
		UpdatedAt:     strings.TrimSpace(record.UpdatedAt),
	}
}

func skillArtifactRegistryPathAvailable(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
