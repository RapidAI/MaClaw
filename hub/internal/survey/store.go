package survey

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrAlreadySubmitted is returned when UNIQUE blocks insert and allow_update is false.
var ErrAlreadySubmitted = errors.New("already submitted")

// Store is the tenant-scoped survey authority (Hub SQLite).
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) InitSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS surveys (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			short_code TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			settings_json TEXT NOT NULL,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			published_at TEXT,
			UNIQUE(tenant_id, short_code)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_surveys_tenant_status ON surveys(tenant_id, status)`,
		`CREATE TABLE IF NOT EXISTS survey_questions (
			survey_id TEXT NOT NULL,
			question_id TEXT NOT NULL,
			position INTEGER NOT NULL,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			required INTEGER NOT NULL DEFAULT 0,
			config_json TEXT NOT NULL DEFAULT '{}',
			PRIMARY KEY(survey_id, question_id)
		)`,
		`CREATE TABLE IF NOT EXISTS survey_bindings (
			survey_id TEXT NOT NULL,
			platform TEXT NOT NULL,
			group_id TEXT NOT NULL,
			group_name TEXT NOT NULL DEFAULT '',
			bound_at TEXT NOT NULL,
			PRIMARY KEY(survey_id, platform, group_id)
		)`,
		`CREATE TABLE IF NOT EXISTS survey_responses (
			id TEXT PRIMARY KEY,
			survey_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			platform TEXT NOT NULL,
			respondent_key TEXT NOT NULL,
			respondent_name TEXT NOT NULL DEFAULT '',
			group_id TEXT NOT NULL DEFAULT '',
			answers_json TEXT NOT NULL,
			submitted_at TEXT NOT NULL,
			UNIQUE(survey_id, platform, respondent_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_survey_responses_survey ON survey_responses(survey_id)`,
		`CREATE TABLE IF NOT EXISTS survey_sessions (
			session_key TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			survey_id TEXT NOT NULL,
			platform TEXT NOT NULL,
			user_id TEXT NOT NULL,
			user_name TEXT NOT NULL DEFAULT '',
			group_id TEXT NOT NULL DEFAULT '',
			phase TEXT NOT NULL,
			cursor INTEGER NOT NULL DEFAULT 0,
			answers_json TEXT NOT NULL DEFAULT '{}',
			expires_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, session_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_survey_sessions_expires ON survey_sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_survey_sessions_survey ON survey_sessions(survey_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("survey schema: %w", err)
		}
	}
	return nil
}

func (s *Store) Create(ctx context.Context, tenantID, createdBy string, in CreateInput) (*Survey, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("tenant required")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("title required")
	}
	qs := NormalizeQuestions(in.Questions)
	if len(qs) > 0 {
		if err := ValidateDraftQuestions(qs); err != nil {
			return nil, err
		}
	}
	if err := ValidateSettingsIn(in.Settings); err != nil {
		return nil, err
	}
	salt, err := NewAnonymitySalt()
	if err != nil {
		return nil, err
	}
	settings := Settings{
		Anonymous:     in.Settings.Anonymous,
		AllowUpdate:   in.Settings.AllowUpdate,
		AllowP2P:      in.Settings.AllowP2P,
		Deadline:      in.Settings.Deadline,
		TargetCount:   in.Settings.TargetCount,
		AnonymitySalt: salt,
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	var code string
	for i := 0; i < ShortCodeRetries; i++ {
		c, err := GenerateShortCode()
		if err != nil {
			return nil, err
		}
		code = c
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		settingsJSON, _ := json.Marshal(settings)
		_, err = tx.ExecContext(ctx, `INSERT INTO surveys(id,tenant_id,short_code,title,description,status,settings_json,created_by,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			id, tenantID, code, title, strings.TrimSpace(in.Description), StatusDraft, string(settingsJSON), createdBy,
			now.Format(time.RFC3339), now.Format(time.RFC3339))
		if err != nil {
			_ = tx.Rollback()
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				continue
			}
			return nil, err
		}
		if err := insertQuestions(ctx, tx, id, qs); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return s.Get(ctx, tenantID, id)
	}
	return nil, fmt.Errorf("failed to allocate short code")
}

func insertQuestions(ctx context.Context, tx *sql.Tx, surveyID string, qs []Question) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM survey_questions WHERE survey_id=?`, surveyID); err != nil {
		return err
	}
	for _, q := range qs {
		cfg := map[string]any{"options": q.Options}
		if q.Min != nil {
			cfg["min"] = *q.Min
		}
		if q.Max != nil {
			cfg["max"] = *q.Max
		}
		if q.MaxLength != nil {
			cfg["max_length"] = *q.MaxLength
		}
		raw, _ := json.Marshal(cfg)
		req := 0
		if q.Required {
			req = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO survey_questions(survey_id,question_id,position,type,title,required,config_json)
			VALUES(?,?,?,?,?,?,?)`, surveyID, q.ID, q.Position, q.Type, q.Title, req, string(raw)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Get(ctx context.Context, tenantID, id string) (*Survey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,tenant_id,short_code,title,description,status,settings_json,created_by,created_at,updated_at,published_at
		FROM surveys WHERE id=? AND tenant_id=?`, id, tenantID)
	sv, err := scanSurvey(row)
	if err != nil {
		return nil, err
	}
	qs, err := s.loadQuestions(ctx, id)
	if err != nil {
		return nil, err
	}
	sv.Questions = qs
	bs, err := s.loadBindings(ctx, id)
	if err != nil {
		return nil, err
	}
	sv.Bindings = bs
	return sv, nil
}

func (s *Store) GetByCode(ctx context.Context, tenantID, code string) (*Survey, error) {
	code, err := NormalizeShortCode(code)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT id,tenant_id,short_code,title,description,status,settings_json,created_by,created_at,updated_at,published_at
		FROM surveys WHERE tenant_id=? AND short_code=?`, tenantID, code)
	sv, err := scanSurvey(row)
	if err != nil {
		return nil, err
	}
	qs, err := s.loadQuestions(ctx, sv.ID)
	if err != nil {
		return nil, err
	}
	sv.Questions = qs
	bs, err := s.loadBindings(ctx, sv.ID)
	if err != nil {
		return nil, err
	}
	sv.Bindings = bs
	return sv, nil
}

func (s *Store) List(ctx context.Context, tenantID, status string) ([]Survey, error) {
	q := `SELECT id,tenant_id,short_code,title,description,status,settings_json,created_by,created_at,updated_at,published_at FROM surveys WHERE tenant_id=?`
	args := []any{tenantID}
	status = strings.TrimSpace(status)
	if status != "" && status != "all" {
		switch status {
		case StatusDraft, StatusPublished, StatusClosed, StatusArchived:
			q += ` AND status=?`
			args = append(args, status)
		default:
			return nil, fmt.Errorf("invalid status filter")
		}
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Survey
	for rows.Next() {
		sv, err := scanSurveyRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachListCounts(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// attachListCounts fills BindingCount / QuestionCount / ResponseCount for list payloads.
func (s *Store) attachListCounts(ctx context.Context, list []Survey) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]string, len(list))
	index := map[string]int{}
	for i := range list {
		ids[i] = list[i].ID
		index[list[i].ID] = i
	}
	// SQLite: build placeholders
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	inClause := strings.Join(ph, ",")
	countInto := func(sql string, set func(i int, n int)) error {
		rows, err := s.db.QueryContext(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			if i, ok := index[id]; ok {
				set(i, n)
			}
		}
		return rows.Err()
	}
	if err := countInto(`SELECT survey_id, COUNT(*) FROM survey_questions WHERE survey_id IN (`+inClause+`) GROUP BY survey_id`, func(i, n int) {
		list[i].QuestionCount = n
	}); err != nil {
		return err
	}
	if err := countInto(`SELECT survey_id, COUNT(*) FROM survey_bindings WHERE survey_id IN (`+inClause+`) GROUP BY survey_id`, func(i, n int) {
		list[i].BindingCount = n
	}); err != nil {
		return err
	}
	return countInto(`SELECT survey_id, COUNT(*) FROM survey_responses WHERE survey_id IN (`+inClause+`) GROUP BY survey_id`, func(i, n int) {
		list[i].ResponseCount = n
	})
}

func (s *Store) Update(ctx context.Context, tenantID, id string, in UpdateInput) (*Survey, error) {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sv.Status != StatusDraft {
		return nil, fmt.Errorf("only draft surveys can be edited")
	}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return nil, fmt.Errorf("title required")
		}
		sv.Title = t
	}
	if in.Description != nil {
		sv.Description = strings.TrimSpace(*in.Description)
	}
	if in.Settings != nil {
		if err := ValidateSettingsIn(*in.Settings); err != nil {
			return nil, err
		}
		// preserve salt
		salt := sv.Settings.AnonymitySalt
		sv.Settings.Anonymous = in.Settings.Anonymous
		sv.Settings.AllowUpdate = in.Settings.AllowUpdate
		sv.Settings.AllowP2P = in.Settings.AllowP2P
		sv.Settings.Deadline = in.Settings.Deadline
		sv.Settings.TargetCount = in.Settings.TargetCount
		sv.Settings.AnonymitySalt = salt
	}
	now := time.Now().UTC()
	settingsJSON, _ := json.Marshal(sv.Settings)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE surveys SET title=?, description=?, settings_json=?, updated_at=? WHERE id=? AND tenant_id=?`,
		sv.Title, sv.Description, string(settingsJSON), now.Format(time.RFC3339), id, tenantID); err != nil {
		return nil, err
	}
	if in.Questions != nil {
		qs := NormalizeQuestions(*in.Questions)
		if err := ValidateDraftQuestions(qs); err != nil {
			return nil, err
		}
		if err := insertQuestions(ctx, tx, id, qs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) Bind(ctx context.Context, tenantID, id string, bindings []Binding) error {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if sv.Status != StatusDraft && sv.Status != StatusPublished {
		return fmt.Errorf("cannot bind in status %s", sv.Status)
	}
	if len(bindings) == 0 {
		return fmt.Errorf("bindings required")
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, b := range bindings {
		if b.Platform != PlatformLansenger {
			return fmt.Errorf("only lansenger bindings allowed")
		}
		if strings.TrimSpace(b.GroupID) == "" {
			return fmt.Errorf("group_id required")
		}
		if b.BoundAt.IsZero() {
			b.BoundAt = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO survey_bindings(survey_id,platform,group_id,group_name,bound_at)
			VALUES(?,?,?,?,?) ON CONFLICT(survey_id,platform,group_id) DO UPDATE SET group_name=excluded.group_name`,
			id, b.Platform, b.GroupID, b.GroupName, b.BoundAt.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE surveys SET updated_at=? WHERE id=?`, now.Format(time.RFC3339), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Unbind(ctx context.Context, tenantID, id, platform, groupID string) error {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if sv.Status == StatusClosed || sv.Status == StatusArchived {
		return fmt.Errorf("cannot unbind in status %s", sv.Status)
	}
	if sv.Status == StatusPublished {
		count := 0
		for _, b := range sv.Bindings {
			if !(b.Platform == platform && b.GroupID == groupID) {
				count++
			}
		}
		if count == 0 {
			return fmt.Errorf("已发布问卷至少保留一个群绑定")
		}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM survey_bindings WHERE survey_id=? AND platform=? AND group_id=?`, id, platform, groupID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE surveys SET updated_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), id)
	return nil
}

func (s *Store) Publish(ctx context.Context, tenantID, id string) (*Survey, error) {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sv.Status != StatusDraft && sv.Status != StatusClosed {
		// closed uses Reopen
		if sv.Status != StatusDraft {
			return nil, fmt.Errorf("cannot publish from status %s", sv.Status)
		}
	}
	if sv.Status == StatusClosed {
		return nil, fmt.Errorf("use reopen for closed surveys")
	}
	if err := ValidateDraftQuestions(sv.Questions); err != nil {
		return nil, err
	}
	if len(sv.Bindings) < 1 {
		return nil, fmt.Errorf("请先绑定至少一个蓝信群")
	}
	if strings.TrimSpace(sv.Settings.AnonymitySalt) == "" {
		return nil, fmt.Errorf("missing anonymity salt")
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE surveys SET status=?, published_at=?, updated_at=? WHERE id=? AND tenant_id=?`,
		StatusPublished, now.Format(time.RFC3339), now.Format(time.RFC3339), id, tenantID); err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) Close(ctx context.Context, tenantID, id string) (*Survey, error) {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sv.Status != StatusPublished {
		return nil, fmt.Errorf("only published can close")
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE surveys SET status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		StatusClosed, now.Format(time.RFC3339), id, tenantID); err != nil {
		return nil, err
	}
	// Drop in-flight sessions so IM stops treating replies as survey answers.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM survey_sessions WHERE survey_id=?`, id)
	return s.Get(ctx, tenantID, id)
}

func (s *Store) Reopen(ctx context.Context, tenantID, id string) (*Survey, error) {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sv.Status != StatusClosed {
		return nil, fmt.Errorf("only closed can reopen")
	}
	if len(sv.Bindings) < 1 {
		return nil, fmt.Errorf("cannot reopen without bindings")
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE surveys SET status=?, published_at=COALESCE(published_at,?), updated_at=? WHERE id=? AND tenant_id=?`,
		StatusPublished, now.Format(time.RFC3339), now.Format(time.RFC3339), id, tenantID); err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) Archive(ctx context.Context, tenantID, id string) (*Survey, error) {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sv.Status != StatusDraft && sv.Status != StatusClosed {
		return nil, fmt.Errorf("only draft or closed can archive")
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE surveys SET status=?, updated_at=? WHERE id=? AND tenant_id=?`,
		StatusArchived, now.Format(time.RFC3339), id, tenantID); err != nil {
		return nil, err
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Store) Delete(ctx context.Context, tenantID, id string) error {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if sv.Status != StatusDraft && sv.Status != StatusArchived {
		return fmt.Errorf("only draft or archived can delete")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM survey_sessions WHERE survey_id=?`,
		`DELETE FROM survey_responses WHERE survey_id=?`,
		`DELETE FROM survey_bindings WHERE survey_id=?`,
		`DELETE FROM survey_questions WHERE survey_id=?`,
		`DELETE FROM surveys WHERE id=? AND tenant_id=?`,
	} {
		if q == `DELETE FROM surveys WHERE id=? AND tenant_id=?` {
			if _, err := tx.ExecContext(ctx, q, id, tenantID); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) Duplicate(ctx context.Context, tenantID, id, createdBy string) (*Survey, error) {
	sv, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	// Do not carry a past deadline into the new draft (would publish into already-closed collect window).
	var deadline *time.Time
	if sv.Settings.Deadline != nil && sv.Settings.Deadline.After(time.Now().UTC()) {
		deadline = sv.Settings.Deadline
	}
	in := CreateInput{
		Title:       sv.Title + " (copy)",
		Description: sv.Description,
		Questions:   sv.Questions,
		Settings: SettingsIn{
			Anonymous:   sv.Settings.Anonymous,
			AllowUpdate: sv.Settings.AllowUpdate,
			AllowP2P:    sv.Settings.AllowP2P,
			Deadline:    deadline,
			TargetCount: sv.Settings.TargetCount,
		},
	}
	return s.Create(ctx, tenantID, createdBy, in)
}

func (s *Store) SubmitResponse(ctx context.Context, tenantID string, resp *Response, allowUpdate bool) error {
	if resp.ID == "" {
		resp.ID = uuid.NewString()
	}
	if resp.SubmittedAt.IsZero() {
		resp.SubmittedAt = time.Now().UTC()
	}
	// try insert
	_, err := s.db.ExecContext(ctx, `INSERT INTO survey_responses(id,survey_id,tenant_id,platform,respondent_key,respondent_name,group_id,answers_json,submitted_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		resp.ID, resp.SurveyID, tenantID, resp.Platform, resp.RespondentKey, resp.RespondentName, resp.GroupID,
		string(resp.Answers), resp.SubmittedAt.Format(time.RFC3339))
	if err == nil {
		return nil
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unique") {
		return err
	}
	if !allowUpdate {
		return ErrAlreadySubmitted
	}
	// UPSERT
	_, err = s.db.ExecContext(ctx, `UPDATE survey_responses SET respondent_name=?, group_id=?, answers_json=?, submitted_at=?
		WHERE survey_id=? AND platform=? AND respondent_key=?`,
		resp.RespondentName, resp.GroupID, string(resp.Answers), resp.SubmittedAt.Format(time.RFC3339),
		resp.SurveyID, resp.Platform, resp.RespondentKey)
	return err
}

func (s *Store) HasResponse(ctx context.Context, surveyID, platform, respondentKey string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM survey_responses WHERE survey_id=? AND platform=? AND respondent_key=? LIMIT 1`,
		surveyID, platform, respondentKey).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// surveyOwned is a cheap ownership check (no questions/bindings load).
func (s *Store) surveyOwned(ctx context.Context, tenantID, surveyID string) error {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM surveys WHERE id=? AND tenant_id=? LIMIT 1`, surveyID, tenantID).Scan(&one)
	if err != nil {
		return err
	}
	return nil
}

// IsAnonymous returns the anonymous flag without loading questions/bindings.
func (s *Store) IsAnonymous(ctx context.Context, tenantID, surveyID string) (bool, error) {
	var settingsJSON string
	err := s.db.QueryRowContext(ctx, `SELECT settings_json FROM surveys WHERE id=? AND tenant_id=?`, surveyID, tenantID).Scan(&settingsJSON)
	if err != nil {
		return false, err
	}
	var st Settings
	if err := json.Unmarshal([]byte(settingsJSON), &st); err != nil {
		return false, err
	}
	return st.Anonymous, nil
}

func (s *Store) ListResponses(ctx context.Context, tenantID, surveyID string) ([]Response, error) {
	// verify tenant owns survey (avoid full Get)
	if err := s.surveyOwned(ctx, tenantID, surveyID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,survey_id,tenant_id,platform,respondent_key,respondent_name,group_id,answers_json,submitted_at
		FROM survey_responses WHERE survey_id=? ORDER BY submitted_at ASC`, surveyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Response
	for rows.Next() {
		var r Response
		var submitted string
		var answers string
		if err := rows.Scan(&r.ID, &r.SurveyID, &r.TenantID, &r.Platform, &r.RespondentKey, &r.RespondentName, &r.GroupID, &answers, &submitted); err != nil {
			return nil, err
		}
		r.Answers = json.RawMessage(answers)
		r.SubmittedAt, _ = time.Parse(time.RFC3339, submitted)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SaveSession(ctx context.Context, sess *Session) error {
	raw, _ := json.Marshal(sess.Answers)
	_, err := s.db.ExecContext(ctx, `INSERT INTO survey_sessions(session_key,tenant_id,survey_id,platform,user_id,user_name,group_id,phase,cursor,answers_json,expires_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(tenant_id,session_key) DO UPDATE SET
			survey_id=excluded.survey_id, platform=excluded.platform, user_id=excluded.user_id, user_name=excluded.user_name,
			group_id=excluded.group_id, phase=excluded.phase, cursor=excluded.cursor, answers_json=excluded.answers_json,
			expires_at=excluded.expires_at, updated_at=excluded.updated_at`,
		sess.SessionKey, sess.TenantID, sess.SurveyID, sess.Platform, sess.UserID, sess.UserName, sess.GroupID,
		sess.Phase, sess.Cursor, string(raw), sess.ExpiresAt.UTC().Format(time.RFC3339), sess.UpdatedAt.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) GetSession(ctx context.Context, tenantID, sessionKey string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT session_key,tenant_id,survey_id,platform,user_id,user_name,group_id,phase,cursor,answers_json,expires_at,updated_at
		FROM survey_sessions WHERE tenant_id=? AND session_key=?`, tenantID, sessionKey)
	var sess Session
	var answers, exp, upd string
	err := row.Scan(&sess.SessionKey, &sess.TenantID, &sess.SurveyID, &sess.Platform, &sess.UserID, &sess.UserName, &sess.GroupID,
		&sess.Phase, &sess.Cursor, &answers, &exp, &upd)
	if err != nil {
		return nil, err
	}
	sess.Answers = JSONToAnswers(json.RawMessage(answers))
	sess.ExpiresAt, _ = time.Parse(time.RFC3339, exp)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339, upd)
	return &sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, tenantID, sessionKey string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM survey_sessions WHERE tenant_id=? AND session_key=?`, tenantID, sessionKey)
	return err
}

func (s *Store) CleanupExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM survey_sessions WHERE expires_at < ?`, now.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) ListPublishedForGroup(ctx context.Context, tenantID, platform, groupID string) ([]Survey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id,s.tenant_id,s.short_code,s.title,s.description,s.status,s.settings_json,s.created_by,s.created_at,s.updated_at,s.published_at
		FROM surveys s
		JOIN survey_bindings b ON b.survey_id=s.id
		WHERE s.tenant_id=? AND s.status=? AND b.platform=? AND b.group_id=?
		ORDER BY s.updated_at DESC`, tenantID, StatusPublished, platform, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Survey
	for rows.Next() {
		sv, err := scanSurveyRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sv)
	}
	return out, rows.Err()
}

func (s *Store) loadQuestions(ctx context.Context, surveyID string) ([]Question, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT question_id,position,type,title,required,config_json FROM survey_questions WHERE survey_id=? ORDER BY position`, surveyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		var q Question
		var req int
		var cfg string
		if err := rows.Scan(&q.ID, &q.Position, &q.Type, &q.Title, &req, &cfg); err != nil {
			return nil, err
		}
		q.Required = req != 0
		var m map[string]json.RawMessage
		_ = json.Unmarshal([]byte(cfg), &m)
		if raw, ok := m["options"]; ok {
			_ = json.Unmarshal(raw, &q.Options)
		}
		if raw, ok := m["min"]; ok {
			var v int
			if json.Unmarshal(raw, &v) == nil {
				q.Min = &v
			}
		}
		if raw, ok := m["max"]; ok {
			var v int
			if json.Unmarshal(raw, &v) == nil {
				q.Max = &v
			}
		}
		if raw, ok := m["max_length"]; ok {
			var v int
			if json.Unmarshal(raw, &v) == nil {
				q.MaxLength = &v
			}
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) loadBindings(ctx context.Context, surveyID string) ([]Binding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT platform,group_id,group_name,bound_at FROM survey_bindings WHERE survey_id=?`, surveyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		var b Binding
		var at string
		if err := rows.Scan(&b.Platform, &b.GroupID, &b.GroupName, &at); err != nil {
			return nil, err
		}
		b.BoundAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, b)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSurvey(row scanner) (*Survey, error) {
	var sv Survey
	var settings, created, updated string
	var published sql.NullString
	if err := row.Scan(&sv.ID, &sv.TenantID, &sv.ShortCode, &sv.Title, &sv.Description, &sv.Status, &settings, &sv.CreatedBy, &created, &updated, &published); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(settings), &sv.Settings)
	sv.CreatedAt, _ = time.Parse(time.RFC3339, created)
	sv.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	if published.Valid && published.String != "" {
		t, _ := time.Parse(time.RFC3339, published.String)
		sv.PublishedAt = &t
	}
	return &sv, nil
}

func scanSurveyRows(rows *sql.Rows) (*Survey, error) {
	return scanSurvey(rows)
}
