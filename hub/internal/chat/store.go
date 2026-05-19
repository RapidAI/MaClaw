package chat

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// Store provides SQLite persistence for the chat module.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store and runs migrations.
func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("chat store migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS chat_channels (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL DEFAULT 'tenant_default',
			type        INTEGER NOT NULL,
			name        TEXT,
			avatar_url  TEXT,
			created_by  TEXT NOT NULL,
			created_at  INTEGER NOT NULL,
			last_seq    INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS chat_members (
			channel_id TEXT NOT NULL,
			tenant_id  TEXT NOT NULL DEFAULT 'tenant_default',
			user_id    TEXT NOT NULL,
			role       INTEGER NOT NULL DEFAULT 0,
			mute       INTEGER NOT NULL DEFAULT 0,
			nickname   TEXT,
			joined_at  INTEGER NOT NULL,
			read_seq   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(channel_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id           TEXT PRIMARY KEY,
			channel_id   TEXT NOT NULL,
			tenant_id    TEXT NOT NULL DEFAULT 'tenant_default',
			seq          INTEGER NOT NULL,
			sender_id    TEXT NOT NULL,
			content      TEXT NOT NULL DEFAULT '',
			msg_type     INTEGER NOT NULL DEFAULT 0,
			attachments  TEXT NOT NULL DEFAULT '[]',
			created_at   INTEGER NOT NULL,
			client_msg_id TEXT,
			recalled     INTEGER NOT NULL DEFAULT 0,
			edited_at    INTEGER,
			UNIQUE(channel_id, seq)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_msg_ch_seq ON chat_messages(channel_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_channels_tenant ON chat_channels(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_members_tenant_user ON chat_members(tenant_id, user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_tenant_ch_seq ON chat_messages(tenant_id, channel_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_files_tenant ON chat_files(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_voice_calls_tenant ON chat_voice_calls(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_push_tokens_tenant_user ON chat_push_tokens(tenant_id, user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_msg_dedup ON chat_messages(channel_id, client_msg_id)`,
		`CREATE TABLE IF NOT EXISTS chat_files (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL DEFAULT 'tenant_default',
			uploader_id TEXT NOT NULL,
			channel_id  TEXT NOT NULL,
			filename    TEXT NOT NULL,
			mime_type   TEXT NOT NULL,
			size        INTEGER NOT NULL,
			has_thumb   INTEGER NOT NULL DEFAULT 0,
			created_at  INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS chat_voice_calls (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL DEFAULT 'tenant_default',
			channel_id TEXT,
			caller_id  TEXT NOT NULL,
			call_type  INTEGER NOT NULL,
			status     INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			started_at INTEGER,
			ended_at   INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS chat_voice_participants (
			call_id   TEXT NOT NULL,
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			user_id   TEXT NOT NULL,
			joined_at INTEGER,
			left_at   INTEGER,
			PRIMARY KEY(call_id, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS chat_push_tokens (
			tenant_id  TEXT NOT NULL DEFAULT 'tenant_default',
			user_id    TEXT NOT NULL,
			platform   TEXT NOT NULL,
			token      TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(user_id, platform)
		)`,
		`CREATE TABLE IF NOT EXISTS chat_presence (
			tenant_id TEXT NOT NULL DEFAULT 'tenant_default',
			user_id   TEXT PRIMARY KEY,
			status    INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:40], err)
		}
	}
	for _, stmt := range []string{
		`ALTER TABLE chat_channels ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE chat_members ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE chat_messages ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE chat_files ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE chat_voice_calls ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE chat_voice_participants ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE chat_push_tokens ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
		`ALTER TABLE chat_presence ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'tenant_default'`,
	} {
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	return nil
}

// 閳光偓閳光偓 Channels 閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓

func (s *Store) CreateChannel(ch *Channel) error {
	tenantID := store.NormalizeTenantID(ch.TenantID)
	ch.TenantID = tenantID
	_, err := s.db.Exec(
		`INSERT INTO chat_channels (id, tenant_id, type, name, avatar_url, created_by, created_at, last_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		ch.ID, tenantID, ch.Type, ch.Name, ch.AvatarURL, ch.CreatedBy, ch.CreatedAt.UnixMilli(),
	)
	return err
}

func (s *Store) GetChannel(channelID string) (*Channel, error) {
	var ch Channel
	var createdAt int64
	err := s.db.QueryRow(
		`SELECT id, tenant_id, type, name, avatar_url, created_by, created_at, last_seq
		 FROM chat_channels WHERE id = ?`, channelID,
	).Scan(&ch.ID, &ch.TenantID, &ch.Type, &ch.Name, &ch.AvatarURL, &ch.CreatedBy, &createdAt, &ch.LastSeq)
	if err != nil {
		return nil, err
	}
	ch.CreatedAt = time.UnixMilli(createdAt)
	return &ch, nil
}
func (s *Store) GetChannelsForUser(userID string) ([]Channel, error) {
	return s.GetChannelsForTenantUser("", userID)
}

func (s *Store) GetChannelsForTenantUser(tenantID, userID string) ([]Channel, error) {
	tenantID = store.NormalizeTenantID(tenantID)
	rows, err := s.db.Query(
		`SELECT c.id, c.tenant_id, c.type, c.name, c.avatar_url, c.created_by, c.created_at, c.last_seq
		 FROM chat_channels c
		 JOIN chat_members m ON m.channel_id = c.id
		 WHERE c.tenant_id = ? AND m.tenant_id = ? AND m.user_id = ?
		 ORDER BY c.last_seq DESC`, tenantID, tenantID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		var createdAt int64
		if err := rows.Scan(&ch.ID, &ch.TenantID, &ch.Type, &ch.Name, &ch.AvatarURL, &ch.CreatedBy, &createdAt, &ch.LastSeq); err != nil {
			return nil, err
		}
		ch.CreatedAt = time.UnixMilli(createdAt)
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// 閳光偓閳光偓 Members 閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓

func (s *Store) AddMember(m *Member) error {
	tenantID := store.DefaultTenantID
	if ch, err := s.GetChannel(m.ChannelID); err == nil && ch != nil {
		tenantID = store.NormalizeTenantID(ch.TenantID)
	}
	m.TenantID = tenantID
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO chat_members (channel_id, tenant_id, user_id, role, mute, nickname, joined_at, read_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		m.ChannelID, tenantID, m.UserID, m.Role, boolToInt(m.Mute), m.Nickname, m.JoinedAt.UnixMilli(),
	)
	return err
}

func (s *Store) GetMembers(channelID string) ([]Member, error) {
	rows, err := s.db.Query(
		`SELECT channel_id, tenant_id, user_id, role, mute, nickname, joined_at, read_seq
		 FROM chat_members WHERE channel_id = ?`, channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		var muteInt int
		var joinedAt int64
		if err := rows.Scan(&m.ChannelID, &m.TenantID, &m.UserID, &m.Role, &muteInt, &m.Nickname, &joinedAt, &m.ReadSeq); err != nil {
			return nil, err
		}
		m.Mute = muteInt != 0
		m.JoinedAt = time.UnixMilli(joinedAt)
		members = append(members, m)
	}
	return members, rows.Err()
}

func (s *Store) IsMember(channelID, userID string) (bool, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM chat_members WHERE channel_id = ? AND user_id = ?`,
		channelID, userID,
	).Scan(&count)
	return count > 0, err
}

// 閳光偓閳光偓 Messages 閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓

// InsertMessage stores a message and atomically increments the channel seq.
// Returns the assigned seq. If client_msg_id already exists, returns the existing message.
func (s *Store) InsertMessage(msg *Message) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Check dedup.
	if msg.ClientMsgID != "" {
		var existingSeq int64
		err := tx.QueryRow(
			`SELECT seq FROM chat_messages WHERE channel_id = ? AND client_msg_id = ?`,
			msg.ChannelID, msg.ClientMsgID,
		).Scan(&existingSeq)
		if err == nil {
			return existingSeq, nil // Idempotent: already exists.
		}
	}

	// Increment channel seq.
	var seq int64
	err = tx.QueryRow(
		`UPDATE chat_channels SET last_seq = last_seq + 1 WHERE id = ? RETURNING last_seq`,
		msg.ChannelID,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("increment seq: %w", err)
	}

	attachJSON, _ := json.Marshal(msg.Attachments)
	tenantID := store.DefaultTenantID
	if ch, err := s.GetChannel(msg.ChannelID); err == nil && ch != nil {
		tenantID = store.NormalizeTenantID(ch.TenantID)
	}

	_, err = tx.Exec(
		`INSERT INTO chat_messages (id, channel_id, tenant_id, seq, sender_id, content, msg_type, attachments, created_at, client_msg_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ChannelID, tenantID, seq, msg.SenderID, msg.Content, msg.MsgType,
		string(attachJSON), msg.CreatedAt.UnixMilli(), msg.ClientMsgID,
	)
	if err != nil {
		return 0, err
	}

	return seq, tx.Commit()
}

// GetMessagesAfter returns messages with seq > afterSeq (ascending).
func (s *Store) GetMessagesAfter(channelID string, afterSeq int64, limit int) ([]Message, error) {
	return s.queryMessages(
		`SELECT id, channel_id, seq, sender_id, content, msg_type, attachments, created_at, client_msg_id, recalled, edited_at
		 FROM chat_messages WHERE channel_id = ? AND seq > ? ORDER BY seq ASC LIMIT ?`,
		channelID, afterSeq, limit,
	)
}

// GetMessagesBefore returns messages with seq < beforeSeq (descending, then reversed).
func (s *Store) GetMessagesBefore(channelID string, beforeSeq int64, limit int) ([]Message, error) {
	return s.queryMessages(
		`SELECT id, channel_id, seq, sender_id, content, msg_type, attachments, created_at, client_msg_id, recalled, edited_at
		 FROM chat_messages WHERE channel_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?`,
		channelID, beforeSeq, limit,
	)
}

func (s *Store) queryMessages(query string, args ...any) ([]Message, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var attachJSON string
		var createdAt int64
		var editedAt sql.NullInt64
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Seq, &m.SenderID, &m.Content,
			&m.MsgType, &attachJSON, &createdAt, &m.ClientMsgID, &m.Recalled, &editedAt); err != nil {
			return nil, err
		}
		m.CreatedAt = time.UnixMilli(createdAt)
		if editedAt.Valid {
			t := time.UnixMilli(editedAt.Int64)
			m.EditedAt = &t
		}
		_ = json.Unmarshal([]byte(attachJSON), &m.Attachments)
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// 閳光偓閳光偓 Read Receipts 閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓

func (s *Store) UpdateReadSeq(channelID, userID string, seq int64) error {
	_, err := s.db.Exec(
		`UPDATE chat_members SET read_seq = MAX(read_seq, ?) WHERE channel_id = ? AND user_id = ?`,
		seq, channelID, userID,
	)
	return err
}

// 閳光偓閳光偓 Presence 閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓

func (s *Store) SetPresence(userID string, online bool) error {
	return s.SetPresenceForTenant(store.DefaultTenantID, userID, online)
}

func (s *Store) SetPresenceForTenant(tenantID, userID string, online bool) error {
	status := 0
	if online {
		status = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO chat_presence (tenant_id, user_id, status, last_seen) VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET status = excluded.status, last_seen = excluded.last_seen`,
		store.NormalizeTenantID(tenantID), userID, status, time.Now().UnixMilli(),
	)
	return err
}

// 閳光偓閳光偓 Push Tokens 閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓

func (s *Store) UpsertPushToken(userID, platform, token string) error {
	return s.UpsertPushTokenForTenant(store.DefaultTenantID, userID, platform, token)
}

func (s *Store) UpsertPushTokenForTenant(tenantID, userID, platform, token string) error {
	_, err := s.db.Exec(
		`INSERT INTO chat_push_tokens (tenant_id, user_id, platform, token, updated_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, platform) DO UPDATE SET token = excluded.token, updated_at = excluded.updated_at`,
		store.NormalizeTenantID(tenantID), userID, platform, token, time.Now().UnixMilli(),
	)
	return err
}

func (s *Store) GetPushTokens(userID string) ([]PushToken, error) {
	return s.GetPushTokensForTenant(store.DefaultTenantID, userID)
}

func (s *Store) GetPushTokensForTenant(tenantID, userID string) ([]PushToken, error) {
	rows, err := s.db.Query(
		`SELECT tenant_id, user_id, platform, token, updated_at FROM chat_push_tokens WHERE tenant_id = ? AND user_id = ?`, store.NormalizeTenantID(tenantID), userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []PushToken
	for rows.Next() {
		var t PushToken
		var updatedAt int64
		if err := rows.Scan(&t.TenantID, &t.UserID, &t.Platform, &t.Token, &updatedAt); err != nil {
			return nil, err
		}
		t.UpdatedAt = time.UnixMilli(updatedAt)
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// 閳光偓閳光偓 Voice Calls 閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓閳光偓

func (s *Store) CreateVoiceCall(call *VoiceCall) error {
	tenantID := store.NormalizeTenantID(call.TenantID)
	if tenantID == store.DefaultTenantID && strings.TrimSpace(call.ChannelID) != "" {
		if ch, err := s.GetChannel(call.ChannelID); err == nil && ch != nil {
			tenantID = store.NormalizeTenantID(ch.TenantID)
		}
	}
	call.TenantID = tenantID
	_, err := s.db.Exec(
		`INSERT INTO chat_voice_calls (id, tenant_id, channel_id, caller_id, call_type, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		call.ID, tenantID, call.ChannelID, call.CallerID, call.CallType, call.Status, call.CreatedAt.UnixMilli(),
	)
	return err
}

func (s *Store) GetVoiceCall(callID string) (*VoiceCall, error) {
	var call VoiceCall
	var createdAt int64
	var startedAt, endedAt sql.NullInt64
	err := s.db.QueryRow(
		`SELECT id, tenant_id, channel_id, caller_id, call_type, status, created_at, started_at, ended_at
		 FROM chat_voice_calls WHERE id = ?`, callID,
	).Scan(&call.ID, &call.TenantID, &call.ChannelID, &call.CallerID, &call.CallType, &call.Status, &createdAt, &startedAt, &endedAt)
	if err != nil {
		return nil, err
	}
	call.CreatedAt = time.UnixMilli(createdAt)
	if startedAt.Valid {
		t := time.UnixMilli(startedAt.Int64)
		call.StartedAt = &t
	}
	if endedAt.Valid {
		t := time.UnixMilli(endedAt.Int64)
		call.EndedAt = &t
	}
	return &call, nil
}
func (s *Store) UpdateCallStatus(callID string, status CallStatus) error {
	now := time.Now().UnixMilli()
	switch status {
	case CallActive:
		_, err := s.db.Exec(
			`UPDATE chat_voice_calls SET status = ?, started_at = ? WHERE id = ?`,
			status, now, callID,
		)
		return err
	case CallEnded, CallMissed, CallRejected:
		_, err := s.db.Exec(
			`UPDATE chat_voice_calls SET status = ?, ended_at = ? WHERE id = ?`,
			status, now, callID,
		)
		return err
	default:
		_, err := s.db.Exec(`UPDATE chat_voice_calls SET status = ? WHERE id = ?`, status, callID)
		return err
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
