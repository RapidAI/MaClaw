package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type digitalAssetRepo struct {
	db     *sql.DB
	readDB *sql.DB
	batch  *writeBatcher
}

// NewDigitalAssetRepository constructs a DigitalAssetRepository from DB handles.
func NewDigitalAssetRepository(writeDB, readDB *sql.DB) store.DigitalAssetRepository {
	if readDB == nil {
		readDB = writeDB
	}
	return &digitalAssetRepo{db: writeDB, readDB: readDB, batch: nil}
}

const digitalAssetLibraryCols = `id, tenant_id, name, description, status, sync_enabled,
	acl_mode, acl_departments_json, acl_users_json, content_rev, content_hash, store_path,
	source_count, card_count, byte_size, library_kind, accepts_submissions,
	created_by, updated_by, created_at, updated_at, deleted_at`

func (r *digitalAssetRepo) CreateLibrary(ctx context.Context, lib *store.DigitalAssetLibrary) error {
	if lib == nil {
		return errors.New("digital asset library is nil")
	}
	id := strings.TrimSpace(lib.ID)
	if id == "" {
		return errors.New("library id is required")
	}
	tenantID := normalizeTenantID(lib.TenantID)
	name := strings.TrimSpace(lib.Name)
	if name == "" {
		return errors.New("library name is required")
	}
	status := strings.TrimSpace(lib.Status)
	if status == "" {
		status = store.DigitalAssetStatusActive
	}
	aclMode := strings.TrimSpace(lib.ACLMode)
	if aclMode == "" {
		aclMode = store.DigitalAssetACLAllMembers
	}
	depts := strings.TrimSpace(lib.ACLDepartmentsJSON)
	if depts == "" {
		depts = "[]"
	}
	users := strings.TrimSpace(lib.ACLUsersJSON)
	if users == "" {
		users = "[]"
	}
	syncEnabled := 1
	if !lib.SyncEnabled {
		syncEnabled = 0
	}
	kind := strings.TrimSpace(lib.LibraryKind)
	if kind == "" {
		kind = store.DigitalAssetLibraryKindBusiness
	}
	accepts := 1
	if !lib.AcceptsSubmissions {
		accepts = 0
	}
	return execWrite(ctx, r.batch, r.db, `INSERT INTO digital_asset_libraries (
		id, tenant_id, name, description, status, sync_enabled,
		acl_mode, acl_departments_json, acl_users_json, content_rev, content_hash, store_path,
		source_count, card_count, byte_size, library_kind, accepts_submissions,
		created_by, updated_by, created_at, updated_at, deleted_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, name, strings.TrimSpace(lib.Description), status, syncEnabled,
		aclMode, depts, users, lib.ContentRev, strings.TrimSpace(lib.ContentHash), strings.TrimSpace(lib.StorePath),
		lib.SourceCount, lib.CardCount, lib.ByteSize, kind, accepts,
		strings.TrimSpace(lib.CreatedBy), strings.TrimSpace(lib.UpdatedBy),
		lib.CreatedAt.UTC().Format(time.RFC3339), lib.UpdatedAt.UTC().Format(time.RFC3339),
		nullableTimeString(lib.DeletedAt),
	)
}

func (r *digitalAssetRepo) GetLibrary(ctx context.Context, tenantID, libraryID string) (*store.DigitalAssetLibrary, error) {
	row := r.readDB.QueryRowContext(ctx,
		`SELECT `+digitalAssetLibraryCols+` FROM digital_asset_libraries WHERE tenant_id = ? AND id = ?`,
		normalizeTenantID(tenantID), strings.TrimSpace(libraryID),
	)
	lib, err := scanDigitalAssetLibrary(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return lib, nil
}

func (r *digitalAssetRepo) ListLibraries(ctx context.Context, filter store.DigitalAssetLibraryFilter) ([]*store.DigitalAssetLibrary, int, error) {
	tenantID := normalizeTenantID(filter.TenantID)
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	where := []string{"tenant_id = ?"}
	args := []any{tenantID}
	if filter.IncludeDeleted {
		// no status filter by default
	} else if status := strings.TrimSpace(filter.Status); status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	} else {
		where = append(where, "status <> ?")
		args = append(args, store.DigitalAssetStatusDeleted)
	}
	if kw := strings.TrimSpace(filter.Keyword); kw != "" {
		where = append(where, "(name LIKE ? OR description LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like)
	}
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		where = append(where, "library_kind = ?")
		args = append(args, kind)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := r.readDB.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM digital_asset_libraries WHERE `+whereSQL, args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT `+digitalAssetLibraryCols+` FROM digital_asset_libraries WHERE `+whereSQL+
			` ORDER BY updated_at DESC LIMIT ? OFFSET ?`, queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]*store.DigitalAssetLibrary, 0, limit)
	for rows.Next() {
		lib, err := scanDigitalAssetLibrary(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, lib)
	}
	return items, total, rows.Err()
}

func (r *digitalAssetRepo) UpdateLibrary(ctx context.Context, lib *store.DigitalAssetLibrary) error {
	if lib == nil {
		return errors.New("digital asset library is nil")
	}
	syncEnabled := 1
	if !lib.SyncEnabled {
		syncEnabled = 0
	}
	depts := strings.TrimSpace(lib.ACLDepartmentsJSON)
	if depts == "" {
		depts = "[]"
	}
	users := strings.TrimSpace(lib.ACLUsersJSON)
	if users == "" {
		users = "[]"
	}
	aclMode := strings.TrimSpace(lib.ACLMode)
	if aclMode == "" {
		aclMode = store.DigitalAssetACLAllMembers
	}
	status := strings.TrimSpace(lib.Status)
	if status == "" {
		status = store.DigitalAssetStatusActive
	}
	kind := strings.TrimSpace(lib.LibraryKind)
	if kind == "" {
		kind = store.DigitalAssetLibraryKindBusiness
	}
	accepts := 1
	if !lib.AcceptsSubmissions {
		accepts = 0
	}
	return execWrite(ctx, r.batch, r.db, `UPDATE digital_asset_libraries SET
		name = ?, description = ?, status = ?, sync_enabled = ?,
		acl_mode = ?, acl_departments_json = ?, acl_users_json = ?,
		content_rev = ?, content_hash = ?, store_path = ?,
		source_count = ?, card_count = ?, byte_size = ?,
		library_kind = ?, accepts_submissions = ?,
		updated_by = ?, updated_at = ?, deleted_at = ?
		WHERE tenant_id = ? AND id = ?`,
		strings.TrimSpace(lib.Name), strings.TrimSpace(lib.Description), status, syncEnabled,
		aclMode, depts, users,
		lib.ContentRev, strings.TrimSpace(lib.ContentHash), strings.TrimSpace(lib.StorePath),
		lib.SourceCount, lib.CardCount, lib.ByteSize, kind, accepts,
		strings.TrimSpace(lib.UpdatedBy), lib.UpdatedAt.UTC().Format(time.RFC3339), nullableTimeString(lib.DeletedAt),
		normalizeTenantID(lib.TenantID), strings.TrimSpace(lib.ID),
	)
}

func (r *digitalAssetRepo) SoftDeleteLibrary(ctx context.Context, tenantID, libraryID string, deletedAt time.Time, updatedBy string) error {
	return execWrite(ctx, r.batch, r.db, `UPDATE digital_asset_libraries SET
		status = ?, deleted_at = ?, updated_by = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ? AND status <> ?`,
		store.DigitalAssetStatusDeleted,
		deletedAt.UTC().Format(time.RFC3339),
		strings.TrimSpace(updatedBy),
		deletedAt.UTC().Format(time.RFC3339),
		normalizeTenantID(tenantID),
		strings.TrimSpace(libraryID),
		store.DigitalAssetStatusDeleted,
	)
}

func (r *digitalAssetRepo) ArchiveLibrary(ctx context.Context, tenantID, libraryID string, updatedAt time.Time, updatedBy string) error {
	return execWrite(ctx, r.batch, r.db, `UPDATE digital_asset_libraries SET
		status = ?, updated_by = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ? AND status = ?`,
		store.DigitalAssetStatusArchived,
		strings.TrimSpace(updatedBy),
		updatedAt.UTC().Format(time.RFC3339),
		normalizeTenantID(tenantID),
		strings.TrimSpace(libraryID),
		store.DigitalAssetStatusActive,
	)
}

func (r *digitalAssetRepo) InsertChangelog(ctx context.Context, row *store.DigitalAssetChangelog) error {
	if row == nil {
		return errors.New("changelog is nil")
	}
	status := strings.TrimSpace(row.PackageStatus)
	if status == "" {
		status = "pending"
	}
	payload := strings.TrimSpace(row.PayloadJSON)
	if payload == "" {
		payload = "{}"
	}
	return execWrite(ctx, r.batch, r.db, `INSERT INTO digital_asset_changelog (
		tenant_id, library_id, rev, op, package_status, package_ref, package_sha256, package_bytes,
		payload_json, content_hash, error_message, created_at, ready_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalizeTenantID(row.TenantID), strings.TrimSpace(row.LibraryID), row.Rev,
		strings.TrimSpace(row.Op), status, strings.TrimSpace(row.PackageRef),
		strings.TrimSpace(row.PackageSHA256), row.PackageBytes, payload,
		strings.TrimSpace(row.ContentHash), strings.TrimSpace(row.ErrorMessage),
		row.CreatedAt.UTC().Format(time.RFC3339), nullableTimeString(row.ReadyAt),
	)
}

func (r *digitalAssetRepo) UpdateChangelogPackage(ctx context.Context, tenantID, libraryID string, rev int64, status, ref, sha256 string, bytes int64, contentHash, errMsg string, readyAt *time.Time) error {
	return execWrite(ctx, r.batch, r.db, `UPDATE digital_asset_changelog SET
		package_status = ?, package_ref = ?, package_sha256 = ?, package_bytes = ?,
		content_hash = ?, error_message = ?, ready_at = ?
		WHERE tenant_id = ? AND library_id = ? AND rev = ?`,
		strings.TrimSpace(status), strings.TrimSpace(ref), strings.TrimSpace(sha256), bytes,
		strings.TrimSpace(contentHash), strings.TrimSpace(errMsg), nullableTimeString(readyAt),
		normalizeTenantID(tenantID), strings.TrimSpace(libraryID), rev,
	)
}

func (r *digitalAssetRepo) ListChangelogSince(ctx context.Context, tenantID, libraryID string, sinceRev int64, readyOnly bool, limit int) ([]*store.DigitalAssetChangelog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT tenant_id, library_id, rev, op, package_status, package_ref, package_sha256, package_bytes,
		payload_json, content_hash, error_message, created_at, ready_at
		FROM digital_asset_changelog
		WHERE tenant_id = ? AND library_id = ? AND rev > ?`
	args := []any{normalizeTenantID(tenantID), strings.TrimSpace(libraryID), sinceRev}
	if readyOnly {
		query += ` AND package_status = 'ready'`
	}
	query += ` ORDER BY rev ASC LIMIT ?`
	args = append(args, limit)

	rows, err := r.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*store.DigitalAssetChangelog, 0, limit)
	for rows.Next() {
		item, err := scanDigitalAssetChangelog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *digitalAssetRepo) GetChangelog(ctx context.Context, tenantID, libraryID string, rev int64) (*store.DigitalAssetChangelog, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT tenant_id, library_id, rev, op, package_status, package_ref, package_sha256, package_bytes,
		payload_json, content_hash, error_message, created_at, ready_at
		FROM digital_asset_changelog WHERE tenant_id = ? AND library_id = ? AND rev = ?`,
		normalizeTenantID(tenantID), strings.TrimSpace(libraryID), rev,
	)
	item, err := scanDigitalAssetChangelog(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

func (r *digitalAssetRepo) LatestReadyRev(ctx context.Context, tenantID, libraryID string) (int64, error) {
	var rev sql.NullInt64
	err := r.readDB.QueryRowContext(ctx,
		`SELECT MAX(rev) FROM digital_asset_changelog WHERE tenant_id = ? AND library_id = ? AND package_status = 'ready'`,
		normalizeTenantID(tenantID), strings.TrimSpace(libraryID),
	).Scan(&rev)
	if err != nil {
		return 0, err
	}
	if !rev.Valid {
		return 0, nil
	}
	return rev.Int64, nil
}

func (r *digitalAssetRepo) CreateJob(ctx context.Context, job *store.DigitalAssetImportJob) error {
	if job == nil {
		return errors.New("import job is nil")
	}
	id := strings.TrimSpace(job.ID)
	if id == "" {
		return errors.New("job id is required")
	}
	progress := strings.TrimSpace(job.ProgressJSON)
	if progress == "" {
		progress = "{}"
	}
	status := strings.TrimSpace(job.Status)
	if status == "" {
		status = "queued"
	}
	return execWrite(ctx, r.batch, r.db, `INSERT INTO digital_asset_import_jobs (
		id, tenant_id, library_id, kind, status, progress_json, error_message, created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, normalizeTenantID(job.TenantID), strings.TrimSpace(job.LibraryID),
		strings.TrimSpace(job.Kind), status, progress, strings.TrimSpace(job.ErrorMessage),
		strings.TrimSpace(job.CreatedBy),
		job.CreatedAt.UTC().Format(time.RFC3339), job.UpdatedAt.UTC().Format(time.RFC3339),
	)
}

func (r *digitalAssetRepo) GetJob(ctx context.Context, tenantID, jobID string) (*store.DigitalAssetImportJob, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT id, tenant_id, library_id, kind, status, progress_json, error_message, created_by, created_at, updated_at
		FROM digital_asset_import_jobs WHERE tenant_id = ? AND id = ?`,
		normalizeTenantID(tenantID), strings.TrimSpace(jobID),
	)
	job, err := scanDigitalAssetImportJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return job, nil
}

func (r *digitalAssetRepo) UpdateJob(ctx context.Context, job *store.DigitalAssetImportJob) error {
	if job == nil {
		return errors.New("import job is nil")
	}
	progress := strings.TrimSpace(job.ProgressJSON)
	if progress == "" {
		progress = "{}"
	}
	return execWrite(ctx, r.batch, r.db, `UPDATE digital_asset_import_jobs SET
		library_id = ?, kind = ?, status = ?, progress_json = ?, error_message = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ?`,
		strings.TrimSpace(job.LibraryID), strings.TrimSpace(job.Kind), strings.TrimSpace(job.Status),
		progress, strings.TrimSpace(job.ErrorMessage), job.UpdatedAt.UTC().Format(time.RFC3339),
		normalizeTenantID(job.TenantID), strings.TrimSpace(job.ID),
	)
}

func (r *digitalAssetRepo) CountRunningJobs(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := r.readDB.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM digital_asset_import_jobs WHERE tenant_id = ? AND status IN ('queued','running')`,
		normalizeTenantID(tenantID),
	).Scan(&n)
	return n, err
}

func (r *digitalAssetRepo) FailStaleRunningJobs(ctx context.Context, tenantID string, before time.Time, errMsg string) (int, error) {
	if before.IsZero() {
		return 0, nil
	}
	msg := strings.TrimSpace(errMsg)
	if msg == "" {
		msg = "import job timed out (stale)"
	}
	tenant := normalizeTenantID(tenantID)
	cutoff := before.UTC().Format(time.RFC3339)
	var n int
	if err := r.readDB.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM digital_asset_import_jobs
		 WHERE tenant_id = ? AND status IN ('queued','running') AND updated_at < ?`,
		tenant, cutoff,
	).Scan(&n); err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// Keep a minimal progress marker so admin history shows why the job ended.
	progress := `{"phase":"failed","percent":0,"message":"stale job reclaimed"}`
	if err := execWrite(ctx, r.batch, r.db, `UPDATE digital_asset_import_jobs SET
		status = 'failed',
		error_message = ?,
		progress_json = ?,
		updated_at = ?
		WHERE tenant_id = ? AND status IN ('queued','running') AND updated_at < ?`,
		msg, progress, now, tenant, cutoff,
	); err != nil {
		return 0, err
	}
	return n, nil
}

func (r *digitalAssetRepo) ListJobs(ctx context.Context, tenantID, libraryID string, limit int) ([]*store.DigitalAssetImportJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.readDB.QueryContext(ctx, `SELECT id, tenant_id, library_id, kind, status, progress_json, error_message, created_by, created_at, updated_at
		FROM digital_asset_import_jobs
		WHERE tenant_id = ? AND library_id = ?
		ORDER BY created_at DESC
		LIMIT ?`,
		normalizeTenantID(tenantID), strings.TrimSpace(libraryID), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.DigitalAssetImportJob, 0, limit)
	for rows.Next() {
		job, err := scanDigitalAssetImportJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (r *digitalAssetRepo) UpsertSyncCursor(ctx context.Context, cur *store.DigitalAssetSyncCursor) error {
	if cur == nil {
		return errors.New("sync cursor is nil")
	}
	return execWrite(ctx, r.batch, r.db, `INSERT INTO digital_asset_sync_cursors (
		tenant_id, library_id, user_id, device_id, last_rev, last_sync_at, last_status
	) VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(tenant_id, library_id, user_id, device_id) DO UPDATE SET
		last_rev = excluded.last_rev,
		last_sync_at = excluded.last_sync_at,
		last_status = excluded.last_status`,
		normalizeTenantID(cur.TenantID), strings.TrimSpace(cur.LibraryID),
		strings.TrimSpace(cur.UserID), strings.TrimSpace(cur.DeviceID),
		cur.LastRev, cur.LastSyncAt.UTC().Format(time.RFC3339), strings.TrimSpace(cur.LastStatus),
	)
}

func (r *digitalAssetRepo) GetSyncCursor(ctx context.Context, tenantID, libraryID, userID, deviceID string) (*store.DigitalAssetSyncCursor, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT tenant_id, library_id, user_id, device_id, last_rev, last_sync_at, last_status
		FROM digital_asset_sync_cursors WHERE tenant_id = ? AND library_id = ? AND user_id = ? AND device_id = ?`,
		normalizeTenantID(tenantID), strings.TrimSpace(libraryID), strings.TrimSpace(userID), strings.TrimSpace(deviceID),
	)
	var cur store.DigitalAssetSyncCursor
	var lastSync string
	if err := row.Scan(&cur.TenantID, &cur.LibraryID, &cur.UserID, &cur.DeviceID, &cur.LastRev, &lastSync, &cur.LastStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	cur.LastSyncAt = mustParseTime(lastSync)
	return &cur, nil
}

type digitalAssetScanner interface {
	Scan(dest ...any) error
}

func scanDigitalAssetLibrary(row digitalAssetScanner) (*store.DigitalAssetLibrary, error) {
	var lib store.DigitalAssetLibrary
	var syncEnabled int
	var accepts int
	var createdAt, updatedAt string
	var deletedAt sql.NullString
	if err := row.Scan(
		&lib.ID, &lib.TenantID, &lib.Name, &lib.Description, &lib.Status, &syncEnabled,
		&lib.ACLMode, &lib.ACLDepartmentsJSON, &lib.ACLUsersJSON, &lib.ContentRev, &lib.ContentHash, &lib.StorePath,
		&lib.SourceCount, &lib.CardCount, &lib.ByteSize, &lib.LibraryKind, &accepts,
		&lib.CreatedBy, &lib.UpdatedBy,
		&createdAt, &updatedAt, &deletedAt,
	); err != nil {
		return nil, err
	}
	lib.SyncEnabled = syncEnabled != 0
	lib.AcceptsSubmissions = accepts != 0
	if strings.TrimSpace(lib.LibraryKind) == "" {
		lib.LibraryKind = store.DigitalAssetLibraryKindBusiness
	}
	lib.CreatedAt = mustParseTime(createdAt)
	lib.UpdatedAt = mustParseTime(updatedAt)
	if deletedAt.Valid && strings.TrimSpace(deletedAt.String) != "" {
		t := mustParseTime(deletedAt.String)
		lib.DeletedAt = &t
	}
	return &lib, nil
}

func scanDigitalAssetChangelog(row digitalAssetScanner) (*store.DigitalAssetChangelog, error) {
	var item store.DigitalAssetChangelog
	var createdAt string
	var readyAt sql.NullString
	if err := row.Scan(
		&item.TenantID, &item.LibraryID, &item.Rev, &item.Op, &item.PackageStatus,
		&item.PackageRef, &item.PackageSHA256, &item.PackageBytes, &item.PayloadJSON,
		&item.ContentHash, &item.ErrorMessage, &createdAt, &readyAt,
	); err != nil {
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	if readyAt.Valid && strings.TrimSpace(readyAt.String) != "" {
		t := mustParseTime(readyAt.String)
		item.ReadyAt = &t
	}
	return &item, nil
}

func scanDigitalAssetImportJob(row digitalAssetScanner) (*store.DigitalAssetImportJob, error) {
	var job store.DigitalAssetImportJob
	var createdAt, updatedAt string
	if err := row.Scan(
		&job.ID, &job.TenantID, &job.LibraryID, &job.Kind, &job.Status,
		&job.ProgressJSON, &job.ErrorMessage, &job.CreatedBy, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	job.CreatedAt = mustParseTime(createdAt)
	job.UpdatedAt = mustParseTime(updatedAt)
	return &job, nil
}

func (r *digitalAssetRepo) CreateSubmission(ctx context.Context, row *store.DigitalAssetSubmission) error {
	if row == nil {
		return errors.New("digital asset submission is nil")
	}
	id := strings.TrimSpace(row.ID)
	if id == "" {
		return errors.New("submission id is required")
	}
	status := strings.TrimSpace(row.Status)
	if status == "" {
		status = store.DigitalAssetSubmissionSubmitted
	}
	return execWrite(ctx, r.batch, r.db, `INSERT INTO digital_asset_submissions (
		id, tenant_id, library_id, submitter_user_id, submitter_email, kind, status,
		title, summary, package_ref, package_sha256, package_bytes, item_count, source_share_id,
		reviewer_user_id, review_note, reviewed_at, import_job_id, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, normalizeTenantID(row.TenantID), strings.TrimSpace(row.LibraryID),
		strings.TrimSpace(row.SubmitterUserID), strings.TrimSpace(row.SubmitterEmail),
		strings.TrimSpace(row.Kind), status,
		strings.TrimSpace(row.Title), strings.TrimSpace(row.Summary),
		strings.TrimSpace(row.PackageRef), strings.TrimSpace(row.PackageSHA256),
		row.PackageBytes, row.ItemCount, strings.TrimSpace(row.SourceShareID),
		strings.TrimSpace(row.ReviewerUserID), strings.TrimSpace(row.ReviewNote),
		nullableTimeString(row.ReviewedAt), strings.TrimSpace(row.ImportJobID),
		row.CreatedAt.UTC().Format(time.RFC3339), row.UpdatedAt.UTC().Format(time.RFC3339),
	)
}

func (r *digitalAssetRepo) GetSubmission(ctx context.Context, tenantID, submissionID string) (*store.DigitalAssetSubmission, error) {
	row := r.readDB.QueryRowContext(ctx,
		`SELECT id, tenant_id, library_id, submitter_user_id, submitter_email, kind, status,
			title, summary, package_ref, package_sha256, package_bytes, item_count, source_share_id,
			reviewer_user_id, review_note, reviewed_at, import_job_id, created_at, updated_at
		 FROM digital_asset_submissions WHERE tenant_id = ? AND id = ?`,
		normalizeTenantID(tenantID), strings.TrimSpace(submissionID),
	)
	item, err := scanDigitalAssetSubmission(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

func (r *digitalAssetRepo) ListSubmissions(ctx context.Context, filter store.DigitalAssetSubmissionFilter) ([]*store.DigitalAssetSubmission, int, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	where := []string{"tenant_id = ?"}
	args := []any{normalizeTenantID(filter.TenantID)}
	if id := strings.TrimSpace(filter.LibraryID); id != "" {
		where = append(where, "library_id = ?")
		args = append(args, id)
	}
	if id := strings.TrimSpace(filter.SubmitterUserID); id != "" {
		where = append(where, "submitter_user_id = ?")
		args = append(args, id)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		where = append(where, "kind = ?")
		args = append(args, kind)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.readDB.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM digital_asset_submissions WHERE `+whereSQL, args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, tenant_id, library_id, submitter_user_id, submitter_email, kind, status,
			title, summary, package_ref, package_sha256, package_bytes, item_count, source_share_id,
			reviewer_user_id, review_note, reviewed_at, import_job_id, created_at, updated_at
		 FROM digital_asset_submissions WHERE `+whereSQL+` ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
		queryArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*store.DigitalAssetSubmission, 0, limit)
	for rows.Next() {
		item, err := scanDigitalAssetSubmission(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *digitalAssetRepo) UpdateSubmission(ctx context.Context, row *store.DigitalAssetSubmission) error {
	if row == nil {
		return errors.New("digital asset submission is nil")
	}
	return execWrite(ctx, r.batch, r.db, `UPDATE digital_asset_submissions SET
		library_id = ?, kind = ?, status = ?, title = ?, summary = ?,
		package_ref = ?, package_sha256 = ?, package_bytes = ?, item_count = ?,
		source_share_id = ?, reviewer_user_id = ?, review_note = ?, reviewed_at = ?,
		import_job_id = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ?`,
		strings.TrimSpace(row.LibraryID), strings.TrimSpace(row.Kind), strings.TrimSpace(row.Status),
		strings.TrimSpace(row.Title), strings.TrimSpace(row.Summary),
		strings.TrimSpace(row.PackageRef), strings.TrimSpace(row.PackageSHA256),
		row.PackageBytes, row.ItemCount, strings.TrimSpace(row.SourceShareID),
		strings.TrimSpace(row.ReviewerUserID), strings.TrimSpace(row.ReviewNote),
		nullableTimeString(row.ReviewedAt), strings.TrimSpace(row.ImportJobID),
		row.UpdatedAt.UTC().Format(time.RFC3339),
		normalizeTenantID(row.TenantID), strings.TrimSpace(row.ID),
	)
}

func scanDigitalAssetSubmission(row digitalAssetScanner) (*store.DigitalAssetSubmission, error) {
	var item store.DigitalAssetSubmission
	var reviewedAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&item.ID, &item.TenantID, &item.LibraryID, &item.SubmitterUserID, &item.SubmitterEmail,
		&item.Kind, &item.Status, &item.Title, &item.Summary, &item.PackageRef, &item.PackageSHA256,
		&item.PackageBytes, &item.ItemCount, &item.SourceShareID, &item.ReviewerUserID, &item.ReviewNote,
		&reviewedAt, &item.ImportJobID, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	item.CreatedAt = mustParseTime(createdAt)
	item.UpdatedAt = mustParseTime(updatedAt)
	if reviewedAt.Valid && strings.TrimSpace(reviewedAt.String) != "" {
		t := mustParseTime(reviewedAt.String)
		item.ReviewedAt = &t
	}
	return &item, nil
}

// Ensure compile-time interface compliance.
var _ store.DigitalAssetRepository = (*digitalAssetRepo)(nil)
