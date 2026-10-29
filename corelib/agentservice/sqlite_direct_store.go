package agentservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func sqliteOne[T any](ctx context.Context, q sqlq, query string, notFound error, args ...any) (T, error) {
	var out T
	var payload []byte
	err := q.QueryRowContext(ctx, query, args...).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return out, notFound
	}
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

func sqliteMany[T any](ctx context.Context, q sqlq, query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return nil, err
		}
		var value T
		if err := json.Unmarshal(payload, &value); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, value)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	return out, nil
}

func messageClientID(v Message) string {
	if v.Role != MessageRoleUser || len(v.Metadata) == 0 {
		return ""
	}
	return strings.TrimSpace(v.Metadata["client_message_id"])
}

func sessionClientKey(v Session) string {
	if len(v.Metadata) == 0 {
		return ""
	}
	return strings.TrimSpace(v.Metadata["client_session_key"])
}

func sqliteMustExist(ctx context.Context, q sqlq, query string, notFound error, args ...any) error {
	found, err := sqliteFound(ctx, q, query, args...)
	if err != nil {
		return err
	}
	if !found {
		return notFound
	}
	return nil
}

func sqliteFound(ctx context.Context, q sqlq, query string, args ...any) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, query, args...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func sqliteSaveMapRow(ctx context.Context, q sqlq, table, key, tenantID, userID, instanceID string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT OR REPLACE INTO `+table+`(k, tenant_id, user_id, instance_id, v) VALUES(?,?,?,?,?)`, key, tenantID, userID, instanceID, payload)
	return err
}

func (s *SQLiteStore) SaveTenant(v Tenant) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		return sqliteSaveMapRow(ctx, q, "as_tenants", v.ID, v.ID, "", "", v)
	}, func(m *MemoryStore) error { return m.SaveTenant(v) })
}

func (s *SQLiteStore) GetTenant(id string) (Tenant, error) {
	var out Tenant
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[Tenant](ctx, q, `SELECT v FROM as_tenants WHERE k=?`, ErrTenantNotFound, id)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetTenant(id); return err })
	return out, err
}

func (s *SQLiteStore) ListTenants() ([]Tenant, error) {
	var out []Tenant
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteMany[Tenant](ctx, q, `SELECT v FROM as_tenants`)
		if err == nil {
			sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListTenants(); return err })
	return out, err
}

func (s *SQLiteStore) DeleteTenant(id string) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		if err := sqliteMustExist(ctx, q, `SELECT 1 FROM as_tenants WHERE k=?`, ErrTenantNotFound, id); err != nil {
			return err
		}
		if err := sqliteDeleteRuns(ctx, q, `tenant_id=?`, []any{id}, ""); err != nil {
			return err
		}
		if err := sqliteDeleteSessions(ctx, q, `tenant_id=?`, []any{id}); err != nil {
			return err
		}
		for _, table := range []string{"as_instances", "as_user_configs", "as_credentials", "as_users", "as_tenants"} {
			if _, err := q.ExecContext(ctx, `DELETE FROM `+table+` WHERE tenant_id=?`, id); err != nil {
				return err
			}
		}
		return nil
	}, func(m *MemoryStore) error { return m.DeleteTenant(id) })
}

func (s *SQLiteStore) SaveUser(v User) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		return sqliteSaveMapRow(ctx, q, "as_users", composite(v.TenantID, v.ID), v.TenantID, v.ID, "", v)
	}, func(m *MemoryStore) error { return m.SaveUser(v) })
}

func (s *SQLiteStore) GetUser(t, u string) (User, error) {
	var out User
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[User](ctx, q, `SELECT v FROM as_users WHERE k=?`, ErrUserNotFound, composite(t, u))
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetUser(t, u); return err })
	return out, err
}

func (s *SQLiteStore) ListUsers(t string) ([]User, error) {
	var out []User
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteMany[User](ctx, q, `SELECT v FROM as_users WHERE tenant_id=?`, t)
		if err == nil {
			sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListUsers(t); return err })
	return out, err
}

func (s *SQLiteStore) DeleteUser(t, u string) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		key := composite(t, u)
		if err := sqliteMustExist(ctx, q, `SELECT 1 FROM as_users WHERE k=?`, ErrUserNotFound, key); err != nil {
			return err
		}
		if err := sqliteDeleteRuns(ctx, q, `tenant_id=? AND user_id=?`, []any{t, u}, ""); err != nil {
			return err
		}
		if err := sqliteDeleteSessions(ctx, q, `tenant_id=? AND user_id=?`, []any{t, u}); err != nil {
			return err
		}
		for _, table := range []string{"as_instances", "as_user_configs", "as_credentials", "as_users"} {
			if _, err := q.ExecContext(ctx, `DELETE FROM `+table+` WHERE tenant_id=? AND user_id=?`, t, u); err != nil {
				return err
			}
		}
		return nil
	}, func(m *MemoryStore) error { return m.DeleteUser(t, u) })
}

func (s *SQLiteStore) SaveCredential(v Credential) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		stored := normalizeStoredCredential(v, "")
		lookupKey := credentialLookupKey(stored)
		if lookupKey == "" {
			return ErrCredentialNotFound
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM as_credentials WHERE credential_id=?`, stored.ID); err != nil {
			return err
		}
		payload, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `INSERT OR REPLACE INTO as_credentials(k, tenant_id, user_id, instance_id, credential_id, v) VALUES(?,?,?,?,?,?)`, lookupKey, stored.TenantID, stored.UserID, "", stored.ID, payload)
		return err
	}, func(m *MemoryStore) error { return m.SaveCredential(v) })
}

func (s *SQLiteStore) GetCredential(t, u, id string) (Credential, error) {
	var out Credential
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[Credential](ctx, q, `SELECT v FROM as_credentials WHERE tenant_id=? AND user_id=? AND credential_id=? LIMIT 1`, ErrCredentialNotFound, t, u, id)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetCredential(t, u, id); return err })
	return out, err
}

func (s *SQLiteStore) ListCredentials(t, u string) ([]Credential, error) {
	var out []Credential
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteMany[Credential](ctx, q, `SELECT v FROM as_credentials WHERE tenant_id=? AND user_id=?`, t, u)
		if err == nil {
			sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListCredentials(t, u); return err })
	return out, err
}

func (s *SQLiteStore) GetCredentialByAPIKey(key string) (Credential, error) {
	var out Credential
	err := s.read(func(ctx context.Context, q sqlq) error {
		lookupKey := credentialLookupKey(Credential{APIKey: key})
		if lookupKey == "" {
			return ErrCredentialNotFound
		}
		var err error
		out, err = sqliteOne[Credential](ctx, q, `SELECT v FROM as_credentials WHERE k=?`, ErrCredentialNotFound, lookupKey)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetCredentialByAPIKey(key); return err })
	return out, err
}

func (s *SQLiteStore) SaveUserConfig(v UserConfig) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		return sqliteSaveMapRow(ctx, q, "as_user_configs", composite(v.TenantID, v.UserID), v.TenantID, v.UserID, "", v)
	}, func(m *MemoryStore) error { return m.SaveUserConfig(v) })
}

func (s *SQLiteStore) GetUserConfig(t, u string) (UserConfig, error) {
	var out UserConfig
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[UserConfig](ctx, q, `SELECT v FROM as_user_configs WHERE k=?`, ErrUserConfigNotFound, composite(t, u))
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetUserConfig(t, u); return err })
	return out, err
}

func (s *SQLiteStore) SaveInstance(v Instance) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		return sqliteSaveMapRow(ctx, q, "as_instances", v.ID, v.TenantID, v.UserID, v.ID, v)
	}, func(m *MemoryStore) error { return m.SaveInstance(v) })
}

func (s *SQLiteStore) GetInstance(t, u, id string) (Instance, error) {
	var out Instance
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[Instance](ctx, q, `SELECT v FROM as_instances WHERE k=? AND tenant_id=? AND user_id=?`, ErrInstanceNotFound, id, t, u)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetInstance(t, u, id); return err })
	return out, err
}

func (s *SQLiteStore) ListInstances(t, u string) ([]Instance, error) {
	var out []Instance
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteMany[Instance](ctx, q, `SELECT v FROM as_instances WHERE tenant_id=? AND user_id=?`, t, u)
		if err == nil {
			sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListInstances(t, u); return err })
	return out, err
}

func (s *SQLiteStore) DeleteInstance(t, u, id string) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		if err := sqliteMustExist(ctx, q, `SELECT 1 FROM as_instances WHERE k=? AND tenant_id=? AND user_id=?`, ErrInstanceNotFound, id, t, u); err != nil {
			return err
		}
		if err := sqliteDeleteRuns(ctx, q, `tenant_id=? AND user_id=? AND instance_id=?`, []any{t, u, id}, ""); err != nil {
			return err
		}
		if err := sqliteDeleteSessions(ctx, q, `tenant_id=? AND user_id=? AND instance_id=?`, []any{t, u, id}); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `DELETE FROM as_instances WHERE k=?`, id)
		return err
	}, func(m *MemoryStore) error { return m.DeleteInstance(t, u, id) })
}

func (s *SQLiteStore) SaveSession(v Session) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		if key := sessionClientKey(v); key != "" {
			found, err := sqliteFound(ctx, q, `SELECT 1 FROM as_sessions WHERE tenant_id=? AND user_id=? AND instance_id=? AND client_session_key=? AND k<>? LIMIT 1`, v.TenantID, v.UserID, v.InstanceID, key, v.ID)
			if err != nil {
				return err
			}
			if found {
				return ErrAlreadyExists
			}
		}
		payload, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `INSERT OR REPLACE INTO as_sessions(k, tenant_id, user_id, instance_id, client_session_key, v) VALUES(?,?,?,?,?,?)`, v.ID, v.TenantID, v.UserID, v.InstanceID, sessionClientKey(v), payload)
		return err
	}, func(m *MemoryStore) error { return m.SaveSession(v) })
}

func (s *SQLiteStore) GetSession(t, u, i, id string) (Session, error) {
	var out Session
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[Session](ctx, q, `SELECT v FROM as_sessions WHERE k=? AND tenant_id=? AND user_id=? AND instance_id=?`, ErrSessionNotFound, id, t, u, i)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetSession(t, u, i, id); return err })
	return out, err
}

func (s *SQLiteStore) ListSessions(t, u, i string) ([]Session, error) {
	var out []Session
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteMany[Session](ctx, q, `SELECT v FROM as_sessions WHERE tenant_id=? AND user_id=? AND instance_id=?`, t, u, i)
		if err == nil {
			sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListSessions(t, u, i); return err })
	return out, err
}

func (s *SQLiteStore) DeleteSession(t, u, i, id string) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		if err := sqliteMustExist(ctx, q, `SELECT 1 FROM as_sessions WHERE k=? AND tenant_id=? AND user_id=? AND instance_id=?`, ErrSessionNotFound, id, t, u, i); err != nil {
			return err
		}
		if err := sqliteDeleteRuns(ctx, q, `tenant_id=? AND user_id=? AND instance_id=? AND session_id=?`, []any{t, u, i, id}, ""); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM as_messages WHERE session_id=?`, id); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `DELETE FROM as_sessions WHERE k=?`, id)
		return err
	}, func(m *MemoryStore) error { return m.DeleteSession(t, u, i, id) })
}

func sqliteDeleteSessions(ctx context.Context, q sqlq, where string, args []any) error {
	rows, err := q.QueryContext(ctx, `SELECT k FROM as_sessions WHERE `+where, args...)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := q.ExecContext(ctx, `DELETE FROM as_messages WHERE session_id=?`, id); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM as_sessions WHERE k=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func sqliteDeleteRuns(ctx context.Context, q sqlq, where string, args []any, sessionID string) error {
	query := `SELECT k, tenant_id, user_id FROM as_runs WHERE ` + where
	if sessionID != "" {
		query += ` AND session_id=?`
		args = append(append([]any{}, args...), sessionID)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	type item struct{ key, tenantID, userID string }
	var items []item
	for rows.Next() {
		var current item
		if err := rows.Scan(&current.key, &current.tenantID, &current.userID); err != nil {
			rows.Close()
			return err
		}
		items = append(items, current)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, current := range items {
		if _, err := q.ExecContext(ctx, `DELETE FROM as_run_events WHERE scope=?`, runEventScope(current.tenantID, current.userID, current.key)); err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM as_runs WHERE k=?`, current.key); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) SaveMessage(v Message) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		return sqliteSaveMessage(ctx, q, v)
	}, func(m *MemoryStore) error { return m.SaveMessage(v) })
}

func sqliteSaveMessage(ctx context.Context, q sqlq, v Message) error {
	clientID := messageClientID(v)
	if clientID != "" {
		taken, err := sqliteClientMessageTaken(ctx, q, v, clientID)
		if err != nil {
			return err
		}
		if taken {
			return ErrAlreadyExists
		}
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var idx int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(idx), -1)+1 FROM as_messages WHERE session_id=?`, v.SessionID).Scan(&idx); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO as_messages(session_id, idx, id, client_message_id, v) VALUES(?,?,?,?,?)`, v.SessionID, idx, v.ID, clientID, payload)
	return err
}

func sqliteClientMessageTaken(ctx context.Context, q sqlq, v Message, clientID string) (bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT v FROM as_messages WHERE session_id=? AND client_message_id=?`, v.SessionID, clientID)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return false, err
		}
		var existing Message
		if err := json.Unmarshal(payload, &existing); err != nil {
			rows.Close()
			return false, err
		}
		if existing.Role == MessageRoleUser && existing.TenantID == v.TenantID && existing.UserID == v.UserID && existing.InstanceID == v.InstanceID {
			rows.Close()
			return true, nil
		}
	}
	if err := closeRows(rows); err != nil {
		return false, err
	}
	return false, nil
}

func (s *SQLiteStore) GetMessage(sessionID, messageID string) (Message, error) {
	var out Message
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[Message](ctx, q, `SELECT v FROM as_messages WHERE session_id=? AND id=? ORDER BY idx LIMIT 1`, ErrMessageNotFound, sessionID, messageID)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetMessage(sessionID, messageID); return err })
	return out, err
}

func (s *SQLiteStore) ListMessages(id string) ([]Message, error) {
	var out []Message
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteMany[Message](ctx, q, `SELECT v FROM as_messages WHERE session_id=? ORDER BY idx`, id)
		if len(out) == 0 {
			out = nil
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListMessages(id); return err })
	return out, err
}

func (s *SQLiteStore) DeleteMessages(id string, ids []string) (int, error) {
	var deleted int
	err := s.write(func(ctx context.Context, q sqlq) error {
		deleted = 0
		want := make(map[string]struct{}, len(ids))
		for _, messageID := range ids {
			messageID = strings.TrimSpace(messageID)
			if messageID != "" {
				want[messageID] = struct{}{}
			}
		}
		for messageID := range want {
			result, err := q.ExecContext(ctx, `DELETE FROM as_messages WHERE session_id=? AND id=?`, id, messageID)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			deleted += int(n)
		}
		return nil
	}, func(m *MemoryStore) error { var err error; deleted, err = m.DeleteMessages(id, ids); return err })
	return deleted, err
}

func (s *SQLiteStore) SaveRun(v Run) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		return sqliteSaveRun(ctx, q, v)
	}, func(m *MemoryStore) error { return m.SaveRun(v) })
}

func sqliteSaveRun(ctx context.Context, q sqlq, v Run) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT OR REPLACE INTO as_runs(k, tenant_id, user_id, instance_id, session_id, user_message_id, v) VALUES(?,?,?,?,?,?,?)`, v.ID, v.TenantID, v.UserID, v.InstanceID, v.SessionID, v.UserMessageID, payload)
	return err
}

func (s *SQLiteStore) GetRun(t, u, i, id string) (Run, error) {
	var out Run
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[Run](ctx, q, `SELECT v FROM as_runs WHERE k=? AND tenant_id=? AND user_id=? AND instance_id=?`, ErrRunNotFound, id, t, u, i)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetRun(t, u, i, id); return err })
	return out, err
}

func (s *SQLiteStore) ListRuns(t, u, i string) ([]Run, error) {
	var out []Run
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteMany[Run](ctx, q, `SELECT v FROM as_runs WHERE tenant_id=? AND user_id=? AND instance_id=?`, t, u, i)
		if err == nil {
			sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListRuns(t, u, i); return err })
	return out, err
}

func (s *SQLiteStore) GetRunByUserMessageID(t, u, i, id string) (Run, error) {
	var out Run
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteOne[Run](ctx, q, `SELECT v FROM as_runs WHERE tenant_id=? AND user_id=? AND instance_id=? AND user_message_id=? ORDER BY k LIMIT 1`, ErrRunNotFound, t, u, i, id)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.GetRunByUserMessageID(t, u, i, id); return err })
	return out, err
}

func (s *SQLiteStore) SaveAuditEvent(v AuditEvent) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		payload, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var idx int64
		if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(idx), -1)+1 FROM as_audit_events`).Scan(&idx); err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `INSERT INTO as_audit_events(idx, tenant_id, user_id, v) VALUES(?,?,?,?)`, idx, v.TenantID, v.UserID, payload)
		return err
	}, func(m *MemoryStore) error { return m.SaveAuditEvent(v) })
}

func (s *SQLiteStore) ListAuditEvents(t, u string) ([]AuditEvent, error) {
	var out []AuditEvent
	err := s.read(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteMany[AuditEvent](ctx, q, `SELECT v FROM as_audit_events WHERE (?='' OR tenant_id=?) AND (?='' OR user_id=?) ORDER BY idx`, t, t, u, u)
		if err == nil {
			sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListAuditEvents(t, u); return err })
	return out, err
}

func (s *SQLiteStore) DeleteAuditEvents(t, u string) (int, error) {
	var deleted int
	err := s.write(func(ctx context.Context, q sqlq) error {
		result, err := q.ExecContext(ctx, `DELETE FROM as_audit_events WHERE tenant_id=? AND user_id=?`, t, u)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		deleted = int(n)
		return err
	}, func(m *MemoryStore) error { var err error; deleted, err = m.DeleteAuditEvents(t, u); return err })
	return deleted, err
}

func (s *SQLiteStore) SaveRunAdmission(message Message, run Run) error {
	return s.SaveRunAdmissionWithEvents(message, run, nil)
}

func (s *SQLiteStore) SaveRunAdmissionWithEvents(message Message, run Run, events []RunEvent) error {
	return s.writeRunLifecycle(message, &run, events, true)
}

func (s *SQLiteStore) SaveRunCompletion(message Message, run Run) error {
	return s.SaveRunCompletionWithEvents(message, run, nil)
}

func (s *SQLiteStore) SaveRunCompletionWithEvents(message Message, run Run, events []RunEvent) error {
	return s.writeRunLifecycle(message, &run, events, true)
}

func (s *SQLiteStore) SaveRunTerminalWithEvents(run Run, events []RunEvent) error {
	return s.writeRunLifecycle(Message{}, &run, events, false)
}

func (s *SQLiteStore) writeRunLifecycle(message Message, run *Run, events []RunEvent, saveMessage bool) error {
	return s.write(func(ctx context.Context, q sqlq) error {
		if saveMessage {
			if err := sqliteSaveMessage(ctx, q, message); err != nil {
				return err
			}
		}
		if err := sqliteSaveRun(ctx, q, *run); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := sqliteAppendRunEvent(ctx, q, event); err != nil {
				return err
			}
		}
		return nil
	}, func(m *MemoryStore) error {
		if saveMessage {
			if err := m.SaveMessage(message); err != nil {
				return err
			}
		}
		if err := m.SaveRun(*run); err != nil {
			return err
		}
		for _, event := range events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) CommitRunLifecycle(mutation RunLifecycleMutation) error {
	if err := validateRunLifecycleMutation(mutation); err != nil {
		return err
	}
	return s.write(func(ctx context.Context, q sqlq) error {
		switch mutation.Phase {
		case RunLifecyclePhaseAdmission, RunLifecyclePhaseCompletion:
			if err := sqliteSaveMessage(ctx, q, *mutation.Message); err != nil {
				return err
			}
			if err := sqliteSaveRun(ctx, q, mutation.Run); err != nil {
				return err
			}
		case RunLifecyclePhaseTerminal:
			if err := sqliteSaveRun(ctx, q, mutation.Run); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported run lifecycle phase %q", mutation.Phase)
		}
		for _, event := range mutation.Events {
			if _, err := sqliteAppendRunEvent(ctx, q, event); err != nil {
				return err
			}
		}
		return nil
	}, func(m *MemoryStore) error {
		switch mutation.Phase {
		case RunLifecyclePhaseAdmission, RunLifecyclePhaseCompletion:
			if err := m.SaveMessage(*mutation.Message); err != nil {
				return err
			}
			if err := m.SaveRun(mutation.Run); err != nil {
				return err
			}
		case RunLifecyclePhaseTerminal:
			if err := m.SaveRun(mutation.Run); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported run lifecycle phase %q", mutation.Phase)
		}
		for _, event := range mutation.Events {
			if _, err := m.Append(event); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLiteStore) Append(event RunEvent) (RunEvent, error) {
	var out RunEvent
	err := s.write(func(ctx context.Context, q sqlq) error {
		var err error
		out, err = sqliteAppendRunEvent(ctx, q, event)
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.Append(event); return err })
	return out, err
}

func sqliteAppendRunEvent(ctx context.Context, q sqlq, event RunEvent) (RunEvent, error) {
	scope := runEventScope(event.TenantID, event.UserID, event.RunID)
	allocated := event.Sequence == 0
	if allocated {
		if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0)+1 FROM as_run_events WHERE scope=?`, scope).Scan(&event.Sequence); err != nil {
			return RunEvent{}, err
		}
	}
	var err error
	event, err = normalizeRunEvent(event)
	if err != nil {
		return RunEvent{}, err
	}
	var existing RunEvent
	var found bool
	// A sequence allocated in this transaction cannot already exist. Look up
	// the primary key and the id index separately so the insert path does not
	// scan with OR.
	if !allocated {
		existing, found, err = sqliteFindRunEvent(ctx, q, `SELECT v FROM as_run_events WHERE scope=? AND seq=?`, scope, event.Sequence)
		if err != nil {
			return RunEvent{}, err
		}
	}
	byID, foundID, err := sqliteFindRunEvent(ctx, q, `SELECT v FROM as_run_events WHERE scope=? AND id=? ORDER BY seq LIMIT 1`, scope, event.ID)
	if err != nil {
		return RunEvent{}, err
	}
	if foundID && (!found || byID.Sequence < existing.Sequence) {
		existing, found = byID, true
	}
	if found {
		return cloneRunEvent(existing), nil
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return RunEvent{}, err
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO as_run_events(scope, seq, tenant_id, user_id, run_id, id, v) VALUES(?,?,?,?,?,?,?)`, scope, event.Sequence, event.TenantID, event.UserID, event.RunID, event.ID, payload); err != nil {
		return RunEvent{}, err
	}
	return event, nil
}

func sqliteFindRunEvent(ctx context.Context, q sqlq, query string, args ...any) (RunEvent, bool, error) {
	var event RunEvent
	var payload []byte
	err := q.QueryRowContext(ctx, query, args...).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return RunEvent{}, false, nil
	}
	if err != nil {
		return RunEvent{}, false, err
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return RunEvent{}, false, err
	}
	return event, true, nil
}

func (s *SQLiteStore) SequenceForID(t, u, r, id string) (uint64, bool, error) {
	var sequence uint64
	var found bool
	err := s.read(func(ctx context.Context, q sqlq) error {
		event, ok, err := sqliteFindRunEvent(ctx, q, `SELECT v FROM as_run_events WHERE scope=? AND id=? ORDER BY seq LIMIT 1`, runEventScope(t, u, r), id)
		if err != nil {
			return err
		}
		found, sequence = ok, event.Sequence
		return nil
	}, func(m *MemoryStore) error {
		var err error
		sequence, found, err = m.SequenceForID(t, u, r, id)
		return err
	})
	return sequence, found, err
}

func (s *SQLiteStore) ListAfter(t, u, r string, after uint64, limit int) ([]RunEvent, error) {
	var out []RunEvent
	err := s.read(func(ctx context.Context, q sqlq) error {
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		var err error
		out, err = sqliteMany[RunEvent](ctx, q, `SELECT v FROM as_run_events WHERE scope=? AND seq>? ORDER BY seq LIMIT ?`, runEventScope(t, u, r), after, limit)
		for i := range out {
			out[i] = cloneRunEvent(out[i])
		}
		return err
	}, func(m *MemoryStore) error { var err error; out, err = m.ListAfter(t, u, r, after, limit); return err })
	return out, err
}

func (s *SQLiteStore) Snapshot() (Store, error) {
	if s == nil {
		return nil, ErrServiceClosed
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.ensureOpenLocked(); err != nil {
		return nil, err
	}
	ctx := context.Background()
	if s.legacy {
		m, err := loadLegacyMemory(ctx, s.db)
		if err != nil {
			return nil, err
		}
		return m.Snapshot()
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	m, err := loadAllRows(ctx, conn)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, err
	}
	committed = true
	return newMemoryStoreFromState(m.snapshot()), nil
}

func (s *SQLiteStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

var (
	_ Store                        = (*SQLiteStore)(nil)
	_ StoreSnapshotter             = (*SQLiteStore)(nil)
	_ RunEventStore                = (*SQLiteStore)(nil)
	_ RunAdmissionEventStore       = (*SQLiteStore)(nil)
	_ RunCompletionEventStore      = (*SQLiteStore)(nil)
	_ RunTerminalEventStore        = (*SQLiteStore)(nil)
	_ RunLifecycleTransactionStore = (*SQLiteStore)(nil)
)
