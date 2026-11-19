package skillmarket

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const problemReportColumns = "id, reporter_user_id, reporter_contact, os_version, gui_version, description, status, admin_note, diagnostics_path, screenshot_paths, origin_url, archived_at, created_at, updated_at, public_download_token"

func (s *Store) CreateProblemReport(ctx context.Context, report *ProblemReport) error {
	paths, err := json.Marshal(report.ScreenshotPaths)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO sm_problem_reports (`+problemReportColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, report.ID, report.ReporterUserID, report.ReporterContact, report.OSVersion, report.GUIVersion, report.Description, report.Status, report.AdminNote, report.DiagnosticsPath, string(paths), report.OriginURL, fmtTime(report.ArchivedAt), fmtTime(report.CreatedAt), fmtTime(report.UpdatedAt), report.PublicDownloadToken)
	if err == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM sm_problem_report_tombstones WHERE id = ?`, report.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) GetProblemReport(ctx context.Context, id string) (*ProblemReport, error) {
	return scanProblemReport(s.readDB.QueryRowContext(ctx, `SELECT `+problemReportColumns+` FROM sm_problem_reports WHERE id = ?`, id))
}

// NewProblemReportDownloadToken is the unguessable path segment for the public ZIP URL.
func NewProblemReportDownloadToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// GetProblemReportByDownloadToken finds the report that owns a public ZIP link.
func (s *Store) GetProblemReportByDownloadToken(ctx context.Context, token string) (*ProblemReport, error) {
	token = strings.TrimSpace(token)
	if len(token) != 64 {
		return nil, ErrNotFound
	}
	return scanProblemReport(s.readDB.QueryRowContext(ctx, `SELECT `+problemReportColumns+` FROM sm_problem_reports WHERE public_download_token = ?`, token))
}

// EnsureProblemReportDownloadToken assigns a public token to a report that
// was stored before the link existed. A second caller keeps the winner.
func (s *Store) EnsureProblemReportDownloadToken(ctx context.Context, id string) (string, error) {
	current, err := s.GetProblemReport(ctx, id)
	if err != nil {
		return "", err
	}
	if token := strings.TrimSpace(current.PublicDownloadToken); len(token) == 64 {
		return token, nil
	}
	token, err := NewProblemReportDownloadToken()
	if err != nil {
		return "", err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sm_problem_reports SET public_download_token=? WHERE id=? AND public_download_token=''`, token, id)
	if err != nil {
		return "", err
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == 0 {
		again, againErr := s.GetProblemReport(ctx, id)
		if againErr != nil {
			return "", againErr
		}
		return strings.TrimSpace(again.PublicDownloadToken), nil
	}
	s.emitSync(ctx)
	return token, nil
}

func (s *Store) ListProblemReports(ctx context.Context, reporterUserID, status string, offset, limit int) ([]ProblemReport, int, error) {
	where, args := " WHERE 1=1", []any{}
	if reporterUserID != "" {
		where += " AND reporter_user_id = ?"
		args = append(args, reporterUserID)
	}
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	var total int
	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sm_problem_reports`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.readDB.QueryContext(ctx, `SELECT `+problemReportColumns+` FROM sm_problem_reports`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []ProblemReport{}
	for rows.Next() {
		item, err := scanProblemReportRows(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (s *Store) UpdateProblemReport(ctx context.Context, id, status, adminNote string, archivedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sm_problem_reports SET status=?, admin_note=?, archived_at=?, updated_at=? WHERE id=?`, status, adminNote, fmtTime(archivedAt), time.Now().UTC().Format(timeFmt), id)
	if err == nil {
		if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected == 0 {
			return ErrNotFound
		}
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) DeleteProblemReport(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(timeFmt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO sm_problem_report_tombstones (id, deleted_at) VALUES (?, ?)
		ON CONFLICT(id) DO UPDATE SET deleted_at=excluded.deleted_at`, id, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sm_problem_reports WHERE id=?`, id); err == nil {
		err = tx.Commit()
	}
	if err == nil {
		s.emitSync(ctx)
	}
	return err
}

func (s *Store) ArchiveUnprocessedProblemReports(ctx context.Context, before time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE sm_problem_reports SET status='archived', admin_note=CASE WHEN admin_note='' THEN 'Automatically archived after 100 days without handling.' ELSE admin_note END, archived_at=?, updated_at=? WHERE status='pending' AND created_at < ?`, before.UTC().Format(timeFmt), time.Now().UTC().Format(timeFmt), before.UTC().Format(timeFmt))
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	if n > 0 {
		s.emitSync(ctx)
	}
	return n, nil
}

type problemReportScanner interface{ Scan(...any) error }

func scanProblemReport(row problemReportScanner) (*ProblemReport, error) {
	var item ProblemReport
	var screenshots, archivedAt, createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.ReporterUserID, &item.ReporterContact, &item.OSVersion, &item.GUIVersion, &item.Description, &item.Status, &item.AdminNote, &item.DiagnosticsPath, &screenshots, &item.OriginURL, &archivedAt, &createdAt, &updatedAt, &item.PublicDownloadToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(screenshots), &item.ScreenshotPaths)
	item.ArchivedAt = parseTime(archivedAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return &item, nil
}
func scanProblemReportRows(row *sql.Rows) (*ProblemReport, error) { return scanProblemReport(row) }
