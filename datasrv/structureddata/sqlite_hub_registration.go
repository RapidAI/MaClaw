package structureddata

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *SQLiteStore) GetHubRegistration(ctx context.Context) (*hubRegistrationRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, hub_base_url, platform_id, platform_name, callback_base_url, virtual_mail_domain, public_key_pem, private_key_pem, callback_secret, registered, last_registered_at, last_synced_at, last_error, created_at, updated_at FROM hub_registration WHERE id = 'default'`)
	record, err := scanHubRegistration(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (s *SQLiteStore) SaveHubRegistration(ctx context.Context, record hubRegistrationRecord) (*hubRegistrationRecord, error) {
	if strings.TrimSpace(record.ID) == "" {
		record.ID = "default"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO hub_registration(id, hub_base_url, platform_id, platform_name, callback_base_url, virtual_mail_domain, public_key_pem, private_key_pem, callback_secret, registered, last_registered_at, last_synced_at, last_error, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET hub_base_url = excluded.hub_base_url, platform_id = excluded.platform_id, platform_name = excluded.platform_name, callback_base_url = excluded.callback_base_url, virtual_mail_domain = excluded.virtual_mail_domain, public_key_pem = excluded.public_key_pem, private_key_pem = excluded.private_key_pem, callback_secret = excluded.callback_secret, registered = excluded.registered, last_registered_at = excluded.last_registered_at, last_synced_at = excluded.last_synced_at, last_error = excluded.last_error, updated_at = excluded.updated_at`,
		record.ID, record.HubBaseURL, record.PlatformID, record.PlatformName, record.CallbackBaseURL, record.VirtualMailDomain, record.PublicKeyPEM, record.PrivateKeyPEM, record.CallbackSecret, boolInt(record.Registered), formatOptionalTime(record.LastRegisteredAt), formatOptionalTime(record.LastSyncedAt), record.LastError, formatTime(record.CreatedAt), formatTime(record.UpdatedAt))
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func scanHubRegistration(scanner interface{ Scan(dest ...any) error }) (hubRegistrationRecord, error) {
	var record hubRegistrationRecord
	var registered int
	var lastRegisteredAt, lastSyncedAt, createdAt, updatedAt string
	if err := scanner.Scan(&record.ID, &record.HubBaseURL, &record.PlatformID, &record.PlatformName, &record.CallbackBaseURL, &record.VirtualMailDomain, &record.PublicKeyPEM, &record.PrivateKeyPEM, &record.CallbackSecret, &registered, &lastRegisteredAt, &lastSyncedAt, &record.LastError, &createdAt, &updatedAt); err != nil {
		return record, err
	}
	record.Registered = intBool(registered)
	if strings.TrimSpace(lastRegisteredAt) != "" {
		parsed := parseTime(lastRegisteredAt)
		record.LastRegisteredAt = &parsed
	}
	if strings.TrimSpace(lastSyncedAt) != "" {
		parsed := parseTime(lastSyncedAt)
		record.LastSyncedAt = &parsed
	}
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}
