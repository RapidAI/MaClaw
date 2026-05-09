package compute

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

// ProviderStore provides SQLite-backed persistence for compute providers
// and center-provider assignments. API keys are encrypted at rest using
// AES-256-GCM via the crypto helpers in this package.
type ProviderStore struct {
	db     *sql.DB
	encKey []byte // 32-byte AES-256 key
}

// NewProviderStore creates a new ProviderStore.
// encKey must be exactly 32 bytes (AES-256).
func NewProviderStore(db *sql.DB, encKey []byte) *ProviderStore {
	return &ProviderStore{db: db, encKey: encKey}
}

// CreateTable creates the compute_providers and center_provider_assignments
// tables if they do not already exist.
func (s *ProviderStore) CreateTable(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS compute_providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			api_key_enc BLOB NOT NULL,
			api_key_nonce BLOB NOT NULL,
			protocol TEXT NOT NULL DEFAULT 'openai',
			user_agent TEXT NOT NULL DEFAULT 'openclaw',
			compute_type TEXT NOT NULL DEFAULT 'general',
			model TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			description TEXT NOT NULL DEFAULT '',
			input_price_per_mtoken REAL NOT NULL DEFAULT 0.0,
			output_price_per_mtoken REAL NOT NULL DEFAULT 0.0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS center_provider_assignments (
			center_id TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (center_id, provider_id)
		)`,
		`CREATE TABLE IF NOT EXISTS compute_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create compute tables: %w", err)
		}
	}
	return nil
}

// generateID returns a random 16-byte hex string suitable for use as a primary key.
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CreateProvider inserts a new ComputeProvider. The APIKey field is encrypted
// before storage. The ID, CreatedAt, and UpdatedAt fields are set automatically.
func (s *ProviderStore) CreateProvider(ctx context.Context, p *ComputeProvider) error {
	id, err := generateID()
	if err != nil {
		return err
	}

	enc, nonce, err := EncryptAPIKey(p.APIKey, s.encKey)
	if err != nil {
		return fmt.Errorf("encrypt api key: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	p.ID = id
	p.CreatedAt = now
	p.UpdatedAt = now

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO compute_providers
			(id, name, base_url, api_key_enc, api_key_nonce, protocol, user_agent,
			 compute_type, model, enabled, priority, description,
			 input_price_per_mtoken, output_price_per_mtoken, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.BaseURL, enc, nonce, p.Protocol, p.UserAgent,
		p.ComputeType, p.Model, boolToInt(p.Enabled), p.Priority, p.Description,
		p.InputPricePerMToken, p.OutputPricePerMToken, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

// GetProvider retrieves a single ComputeProvider by ID.
// The APIKey is decrypted before returning. Returns nil, nil if not found.
func (s *ProviderStore) GetProvider(ctx context.Context, id string) (*ComputeProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, base_url, api_key_enc, api_key_nonce, protocol, user_agent,
		        compute_type, model, enabled, priority, description,
		        input_price_per_mtoken, output_price_per_mtoken, created_at, updated_at
		 FROM compute_providers WHERE id = ?`, id)
	return s.scanProvider(row)
}

// ListProviders returns all ComputeProvider records with decrypted API keys.
func (s *ProviderStore) ListProviders(ctx context.Context) ([]*ComputeProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, base_url, api_key_enc, api_key_nonce, protocol, user_agent,
		        compute_type, model, enabled, priority, description,
		        input_price_per_mtoken, output_price_per_mtoken, created_at, updated_at
		 FROM compute_providers ORDER BY priority DESC, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanProviders(rows)
}

// UpdateProvider updates an existing ComputeProvider. If APIKey is non-empty,
// it is re-encrypted; otherwise the existing encrypted key is preserved.
func (s *ProviderStore) UpdateProvider(ctx context.Context, p *ComputeProvider) error {
	now := time.Now().UTC().Format(time.RFC3339)
	p.UpdatedAt = now

	if p.APIKey != "" {
		enc, nonce, err := EncryptAPIKey(p.APIKey, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt api key: %w", err)
		}
		_, err = s.db.ExecContext(ctx,
			`UPDATE compute_providers SET
				name = ?, base_url = ?, api_key_enc = ?, api_key_nonce = ?,
				protocol = ?, user_agent = ?, compute_type = ?, model = ?,
				enabled = ?, priority = ?, description = ?,
				input_price_per_mtoken = ?, output_price_per_mtoken = ?, updated_at = ?
			 WHERE id = ?`,
			p.Name, p.BaseURL, enc, nonce,
			p.Protocol, p.UserAgent, p.ComputeType, p.Model,
			boolToInt(p.Enabled), p.Priority, p.Description,
			p.InputPricePerMToken, p.OutputPricePerMToken, p.UpdatedAt,
			p.ID,
		)
		return err
	}

	// APIKey empty: keep existing encrypted key.
	_, err := s.db.ExecContext(ctx,
		`UPDATE compute_providers SET
			name = ?, base_url = ?,
			protocol = ?, user_agent = ?, compute_type = ?, model = ?,
			enabled = ?, priority = ?, description = ?,
			input_price_per_mtoken = ?, output_price_per_mtoken = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.BaseURL,
		p.Protocol, p.UserAgent, p.ComputeType, p.Model,
		boolToInt(p.Enabled), p.Priority, p.Description,
		p.InputPricePerMToken, p.OutputPricePerMToken, p.UpdatedAt,
		p.ID,
	)
	return err
}

// DeleteProvider removes a ComputeProvider by ID, cleans up any
// center_provider_assignments referencing it, and marks affected centers for
// compute sync so they do not keep using a stale local routing snapshot.
func (s *ProviderStore) DeleteProvider(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT center_id FROM center_provider_assignments WHERE provider_id = ?`, id)
	if err != nil {
		return err
	}

	var centerIDs []string
	for rows.Next() {
		var centerID string
		if err := rows.Scan(&centerID); err != nil {
			rows.Close()
			return err
		}
		centerIDs = append(centerIDs, centerID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM center_provider_assignments WHERE provider_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM compute_providers WHERE id = ?`, id); err != nil {
		return err
	}

	for _, centerID := range centerIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO compute_settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			"force_sync_"+centerID, "true"); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ToggleProvider flips the enabled state of a provider.
func (s *ProviderStore) ToggleProvider(ctx context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE compute_providers SET enabled = 1 - enabled, updated_at = ? WHERE id = ?`,
		now, id)
	return err
}

// ListEnabledProviders returns only enabled ComputeProvider records with decrypted API keys.
func (s *ProviderStore) ListEnabledProviders(ctx context.Context) ([]*ComputeProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, base_url, api_key_enc, api_key_nonce, protocol, user_agent,
		        compute_type, model, enabled, priority, description,
		        input_price_per_mtoken, output_price_per_mtoken, created_at, updated_at
		 FROM compute_providers WHERE enabled = 1 ORDER BY priority DESC, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanProviders(rows)
}

// ListAssignedProviders returns the enabled providers assigned to a specific center.
// If no assignments exist for the center, it falls back to returning all enabled providers.
func (s *ProviderStore) ListAssignedProviders(ctx context.Context, centerID string) ([]*ComputeProvider, error) {
	// Check if any assignments exist for this center.
	assignments, err := s.ListAssignments(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if len(assignments) == 0 {
		return s.ListEnabledProviders(ctx)
	}

	// Return only the assigned providers that are also enabled.
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id, p.name, p.base_url, p.api_key_enc, p.api_key_nonce, p.protocol, p.user_agent,
		        p.compute_type, p.model, p.enabled, p.priority, p.description,
		        p.input_price_per_mtoken, p.output_price_per_mtoken, p.created_at, p.updated_at
		 FROM compute_providers p
		 INNER JOIN center_provider_assignments a ON p.id = a.provider_id
		 WHERE a.center_id = ? AND p.enabled = 1
		 ORDER BY p.priority DESC, p.created_at`, centerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanProviders(rows)
}

// AssignProvider creates an assignment between a center and a provider.
func (s *ProviderStore) AssignProvider(ctx context.Context, centerID, providerID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO center_provider_assignments (center_id, provider_id, created_at)
		 VALUES (?, ?, ?)`,
		centerID, providerID, time.Now().UTC().Format(time.RFC3339))
	return err
}

// UnassignProvider removes an assignment between a center and a provider.
func (s *ProviderStore) UnassignProvider(ctx context.Context, centerID, providerID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM center_provider_assignments WHERE center_id = ? AND provider_id = ?`,
		centerID, providerID)
	return err
}

// ListAssignments returns the provider IDs assigned to a center.
func (s *ProviderStore) ListAssignments(ctx context.Context, centerID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider_id FROM center_provider_assignments WHERE center_id = ?`, centerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// --- helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// scanProvider reads a single row into a ComputeProvider, decrypting the API key.
func (s *ProviderStore) scanProvider(row *sql.Row) (*ComputeProvider, error) {
	var p ComputeProvider
	var enc, nonce []byte
	var enabled int

	err := row.Scan(
		&p.ID, &p.Name, &p.BaseURL, &enc, &nonce,
		&p.Protocol, &p.UserAgent, &p.ComputeType, &p.Model,
		&enabled, &p.Priority, &p.Description,
		&p.InputPricePerMToken, &p.OutputPricePerMToken,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	p.Enabled = enabled != 0

	apiKey, err := DecryptAPIKey(enc, nonce, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt api key for provider %s: %w", p.ID, err)
	}
	p.APIKey = apiKey
	p.HasAPIKey = apiKey != ""

	return &p, nil
}

// scanProviders reads multiple rows into a slice of ComputeProvider.
func (s *ProviderStore) scanProviders(rows *sql.Rows) ([]*ComputeProvider, error) {
	var result []*ComputeProvider
	for rows.Next() {
		var p ComputeProvider
		var enc, nonce []byte
		var enabled int

		if err := rows.Scan(
			&p.ID, &p.Name, &p.BaseURL, &enc, &nonce,
			&p.Protocol, &p.UserAgent, &p.ComputeType, &p.Model,
			&enabled, &p.Priority, &p.Description,
			&p.InputPricePerMToken, &p.OutputPricePerMToken,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}

		p.Enabled = enabled != 0

		apiKey, err := DecryptAPIKey(enc, nonce, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt api key for provider %s: %w", p.ID, err)
		}
		p.APIKey = apiKey
		p.HasAPIKey = apiKey != ""

		result = append(result, &p)
	}
	return result, rows.Err()
}

// --- compute_settings helpers ---

// GetSetting reads a value from the compute_settings table.
// Returns empty string if the key does not exist.
func (s *ProviderStore) GetSetting(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM compute_settings WHERE key = ?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return val, err
}

// SetSetting upserts a key-value pair in the compute_settings table.
func (s *ProviderStore) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO compute_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// GetComputePermission returns the compute self-management permission for a center.
func (s *ProviderStore) GetComputePermission(ctx context.Context, centerID string) (bool, error) {
	val, err := s.GetSetting(ctx, "compute_permission_"+centerID)
	if err != nil {
		return false, err
	}
	return val == "true", nil
}

// SetComputePermission sets the compute self-management permission for a center.
func (s *ProviderStore) SetComputePermission(ctx context.Context, centerID string, enabled bool) error {
	val := "false"
	if enabled {
		val = "true"
	}
	return s.SetSetting(ctx, "compute_permission_"+centerID, val)
}

// GetForceSync returns the force_sync flag for a center.
func (s *ProviderStore) GetForceSync(ctx context.Context, centerID string) (bool, error) {
	val, err := s.GetSetting(ctx, "force_sync_"+centerID)
	if err != nil {
		return false, err
	}
	return val == "true", nil
}

// SetForceSync sets the force_sync flag for a center.
func (s *ProviderStore) SetForceSync(ctx context.Context, centerID string, val bool) error {
	v := "false"
	if val {
		v = "true"
	}
	return s.SetSetting(ctx, "force_sync_"+centerID, v)
}

// ClearForceSync removes the force_sync flag for a center.
func (s *ProviderStore) ClearForceSync(ctx context.Context, centerID string) error {
	return s.SetSetting(ctx, "force_sync_"+centerID, "false")
}
