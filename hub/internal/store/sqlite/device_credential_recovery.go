package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// DeviceCredentialIdentityRestorer writes the minimum GUI identity chain used
// by a recovered hardware credential. All validation and inserts happen in one
// transaction: an identity conflict or write failure leaves the new Hub DB
// untouched, so it never exposes a partial recovered identity.
type DeviceCredentialIdentityRestorer struct {
	provider *Provider
}

func NewDeviceCredentialIdentityRestorer(provider *Provider) *DeviceCredentialIdentityRestorer {
	return &DeviceCredentialIdentityRestorer{provider: provider}
}

func (r *DeviceCredentialIdentityRestorer) RestoreDeviceCredentialIdentities(ctx context.Context, tenants []store.Tenant, users []store.User, machines []store.Machine) error {
	return r.restore(ctx, tenants, users, machines, "", "")
}

// RestoreDeviceCredentialSnapshot extends identity recovery with the actual
// device credential setting in the same SQLite transaction. This closes the
// last partial-state window in a reinstall: either GUI identity and ESP bearer
// mapping both become durable, or neither does.
func (r *DeviceCredentialIdentityRestorer) RestoreDeviceCredentialSnapshot(ctx context.Context, tenants []store.Tenant, users []store.User, machines []store.Machine, credentialKey, credentialJSON string) error {
	if strings.TrimSpace(credentialKey) == "" || strings.TrimSpace(credentialJSON) == "" {
		return fmt.Errorf("recovered device credential snapshot is empty")
	}
	return r.restore(ctx, tenants, users, machines, credentialKey, credentialJSON)
}

func (r *DeviceCredentialIdentityRestorer) restore(ctx context.Context, tenants []store.Tenant, users []store.User, machines []store.Machine, credentialKey, credentialJSON string) error {
	// Older Hub Center backups predate identity recovery and therefore contain
	// only the hardware credential payload. They still need to be written to
	// the new database: otherwise the gateway works until its next restart and
	// then loses the recovered binding again.
	if len(tenants) == 0 && len(users) == 0 && len(machines) == 0 && strings.TrimSpace(credentialKey) == "" {
		return nil
	}
	if r == nil || r.provider == nil || r.provider.Write == nil {
		return fmt.Errorf("credential identity recovery store is unavailable")
	}
	tx, err := r.provider.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, tenant := range tenants {
		if err := verifyRecoveredTenant(ctx, tx, tenant); err != nil {
			return err
		}
	}
	for _, user := range users {
		if err := verifyRecoveredUser(ctx, tx, user); err != nil {
			return err
		}
	}
	for _, machine := range machines {
		if err := verifyRecoveredMachine(ctx, tx, machine); err != nil {
			return err
		}
	}
	for _, tenant := range tenants {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id = ?)`, tenant.ID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tenants (id, slug, name, status, primary_domain, settings_json, created_by_admin_id, created_at, updated_at, deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, tenant.ID, tenant.Slug, tenant.Name, tenant.Status, tenant.PrimaryDomain, tenant.SettingsJSON, tenant.CreatedByAdminID, tenant.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), tenant.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"), nullableTimeString(tenant.DeletedAt)); err != nil {
			return err
		}
	}
	for _, user := range users {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, user.ID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, tenant_id, email, sn, status, enrollment_status, smart_route, email_verified, email_verified_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, user.ID, normalizeTenantID(user.TenantID), user.Email, user.SN, user.Status, user.EnrollmentStatus, boolToInt(user.SmartRoute), boolToInt(user.EmailVerified), timeStringOrEmpty(user.EmailVerifiedAt), user.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), user.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
			return err
		}
		identityType, identityValue := normalizeUserIdentityFromAccount(user.Email)
		if identityType != "" && identityValue != "" {
			identityVerified := user.EmailVerified || identityType == "phone"
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_identities (id, tenant_id, user_id, type, value, verified, verified_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, user.ID+"_"+identityType, normalizeTenantID(user.TenantID), user.ID, identityType, identityValue, boolToInt(identityVerified), timeStringOrEmpty(user.EmailVerifiedAt), user.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), user.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
				return err
			}
		}
	}
	for _, machine := range machines {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM machines WHERE id = ?)`, machine.ID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO machines (id, tenant_id, user_id, client_id, name, platform, hostname, arch, app_version, heartbeat_sec, machine_token_hash, status, last_seen_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, machine.ID, normalizeTenantID(machine.TenantID), machine.UserID, machine.ClientID, machine.Name, machine.Platform, machine.Hostname, machine.Arch, machine.AppVersion, machine.HeartbeatSec, machine.MachineTokenHash, machine.Status, nullableTimeString(machine.LastSeenAt), machine.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), machine.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
			return err
		}
	}
	if credentialKey != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`, credentialKey, credentialJSON, time.Now().UTC().Format("2006-01-02T15:04:05Z07:00")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func verifyRecoveredTenant(ctx context.Context, tx *sql.Tx, tenant store.Tenant) error {
	var existing store.Tenant
	var createdAt, updatedAt string
	var deletedAt sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id, slug, name, status, primary_domain, settings_json, created_by_admin_id, created_at, updated_at, deleted_at FROM tenants WHERE id = ?`, tenant.ID).Scan(&existing.ID, &existing.Slug, &existing.Name, &existing.Status, &existing.PrimaryDomain, &existing.SettingsJSON, &existing.CreatedByAdminID, &createdAt, &updatedAt, &deletedAt)
	if err == nil {
		if !sameTenantIdentity(existing, tenant, deletedAt.Valid) {
			return fmt.Errorf("recovered tenant %q conflicts with local identity", tenant.ID)
		}
		return nil
	}
	if !errorsIsNoRows(err) {
		return err
	}
	var slugOwner string
	err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE slug = ?`, tenant.Slug).Scan(&slugOwner)
	if err == nil && strings.TrimSpace(slugOwner) != strings.TrimSpace(tenant.ID) {
		return fmt.Errorf("recovered tenant slug %q conflicts with local identity", tenant.Slug)
	}
	if err != nil && !errorsIsNoRows(err) {
		return err
	}
	return nil
}

func verifyRecoveredUser(ctx context.Context, tx *sql.Tx, user store.User) error {
	var existing store.User
	err := tx.QueryRowContext(ctx, `SELECT id, tenant_id, email FROM users WHERE id = ?`, user.ID).Scan(&existing.ID, &existing.TenantID, &existing.Email)
	if err == nil {
		if existing.TenantID != normalizeTenantID(user.TenantID) || !strings.EqualFold(existing.Email, user.Email) {
			return fmt.Errorf("recovered user %q conflicts with local identity", user.ID)
		}
		return nil
	}
	if !errorsIsNoRows(err) {
		return err
	}
	var emailOwner string
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE tenant_id = ? AND lower(email) = lower(?)`, normalizeTenantID(user.TenantID), user.Email).Scan(&emailOwner)
	if err == nil && strings.TrimSpace(emailOwner) != strings.TrimSpace(user.ID) {
		return fmt.Errorf("recovered user email %q conflicts with local identity", user.Email)
	}
	if err != nil && !errorsIsNoRows(err) {
		return err
	}
	return nil
}

func verifyRecoveredMachine(ctx context.Context, tx *sql.Tx, machine store.Machine) error {
	var existing store.Machine
	err := tx.QueryRowContext(ctx, `SELECT id, tenant_id, user_id, client_id, machine_token_hash FROM machines WHERE id = ?`, machine.ID).Scan(&existing.ID, &existing.TenantID, &existing.UserID, &existing.ClientID, &existing.MachineTokenHash)
	if err == nil {
		if existing.TenantID != normalizeTenantID(machine.TenantID) || existing.UserID != machine.UserID || existing.ClientID != machine.ClientID || existing.MachineTokenHash != machine.MachineTokenHash {
			return fmt.Errorf("recovered machine %q conflicts with local identity", machine.ID)
		}
		return nil
	}
	if !errorsIsNoRows(err) {
		return err
	}
	var clientOwner string
	err = tx.QueryRowContext(ctx, `SELECT id FROM machines WHERE user_id = ? AND client_id = ?`, machine.UserID, machine.ClientID).Scan(&clientOwner)
	if err == nil && strings.TrimSpace(clientOwner) != strings.TrimSpace(machine.ID) {
		return fmt.Errorf("recovered machine client %q conflicts with local identity", machine.ClientID)
	}
	if err != nil && !errorsIsNoRows(err) {
		return err
	}
	return nil
}

func sameTenantIdentity(existing, recovered store.Tenant, existingDeleted bool) bool {
	// tenant_default is automatically seeded into every fresh SQLite database.
	// Its operational metadata belongs to the new Hub and must not block recovery
	// of a GUI machine token merely because the old Hub had renamed/configured it.
	if strings.TrimSpace(existing.ID) == store.DefaultTenantID && strings.TrimSpace(recovered.ID) == store.DefaultTenantID {
		return true
	}
	return existing.ID == recovered.ID && existing.Slug == recovered.Slug && existing.Name == recovered.Name && existing.Status == recovered.Status && existing.PrimaryDomain == recovered.PrimaryDomain && existing.SettingsJSON == recovered.SettingsJSON && existing.CreatedByAdminID == recovered.CreatedByAdminID && existingDeleted == (recovered.DeletedAt != nil)
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }

func timeStringOrEmpty(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02T15:04:05Z07:00")
}
