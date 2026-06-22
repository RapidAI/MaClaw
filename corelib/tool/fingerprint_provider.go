package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// FingerprintProvider computes a fingerprint for a tool's current configuration state.
// Returning "" means "no fingerprint available, skip check".
type FingerprintProvider interface {
	ComputeFingerprint(toolName string) string
}

// computeFingerprint generates a SHA-256 truncated to 16 hex chars from JSON-marshaled fields.
// Go 1.12+ guarantees json.Marshal outputs map keys in sorted order, so the
// result is deterministic regardless of map iteration order.
// Returns "" if the fields map is nil/empty or JSON marshaling fails.
func computeFingerprint(fields map[string]interface{}) string {
	if len(fields) == 0 {
		return ""
	}

	data, err := json.Marshal(fields)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:8]) // first 8 bytes = 16 hex chars
}

// ---------------------------------------------------------------------------
// ConfigFingerprintProvider
// ---------------------------------------------------------------------------

// ConfigFingerprintProvider fingerprints tool config fields from AppConfig.
// The ConfigFieldsFunc returns a map of relevant config fields for the given tool.
// Typical fields include LLM endpoint, model, API key hash, timeout, etc.
type ConfigFingerprintProvider struct {
	ConfigFieldsFunc func(toolName string) map[string]interface{}
}

// ComputeFingerprint returns a fingerprint based on the tool's config fields.
// Returns "" if ConfigFieldsFunc is nil or returns no/empty fields.
func (p *ConfigFingerprintProvider) ComputeFingerprint(toolName string) string {
	if p.ConfigFieldsFunc == nil {
		return ""
	}
	fields := p.ConfigFieldsFunc(toolName)
	if len(fields) == 0 {
		return ""
	}
	return computeFingerprint(fields)
}

// ---------------------------------------------------------------------------
// SkillFingerprintProvider
// ---------------------------------------------------------------------------

// SkillFingerprintProvider fingerprints skill version + directory mtime.
// The SkillInfoFunc returns a map with "version" and/or "mtime" fields
// for the given tool/skill name.
type SkillFingerprintProvider struct {
	SkillInfoFunc func(toolName string) map[string]interface{}
}

// ComputeFingerprint returns a fingerprint based on skill version and directory mtime.
// Returns "" if SkillInfoFunc is nil or returns no/empty fields.
func (p *SkillFingerprintProvider) ComputeFingerprint(toolName string) string {
	if p.SkillInfoFunc == nil {
		return ""
	}
	fields := p.SkillInfoFunc(toolName)
	if len(fields) == 0 {
		return ""
	}
	return computeFingerprint(fields)
}

// ---------------------------------------------------------------------------
// SSHFingerprintProvider
// ---------------------------------------------------------------------------

// SSHFingerprintProvider fingerprints SSH host config (host:port:user:keypath).
// The SSHConfigFunc returns a map with SSH connection fields for the given tool.
type SSHFingerprintProvider struct {
	SSHConfigFunc func(toolName string) map[string]interface{}
}

// ComputeFingerprint returns a fingerprint based on SSH host configuration.
// Returns "" if SSHConfigFunc is nil or returns no/empty fields.
func (p *SSHFingerprintProvider) ComputeFingerprint(toolName string) string {
	if p.SSHConfigFunc == nil {
		return ""
	}
	fields := p.SSHConfigFunc(toolName)
	if len(fields) == 0 {
		return ""
	}
	return computeFingerprint(fields)
}

// ---------------------------------------------------------------------------
// Helper: FormatSSHScopeToken
// ---------------------------------------------------------------------------

// FormatSSHScopeToken builds a scope token string for SSH host invalidation events.
// Format: "host:user@host:port"
func FormatSSHScopeToken(user, host string, port int) string {
	return fmt.Sprintf("host:%s@%s:%d", user, host, port)
}

// ---------------------------------------------------------------------------
// Convenience accessor types for wiring and testing
// ---------------------------------------------------------------------------

// StaticSSHHostConfig is a convenience type that produces SSH fingerprint fields
// from statically known values. Useful for testing and simple wiring scenarios.
type StaticSSHHostConfig struct {
	Host    string
	Port    int
	User    string
	KeyPath string
}

// SSHFields returns the SSH configuration fields as a map suitable for
// fingerprint computation.
func (c *StaticSSHHostConfig) SSHFields() map[string]interface{} {
	if c.Host == "" {
		return nil
	}
	fields := map[string]interface{}{
		"host": c.Host,
		"port": c.Port,
		"user": c.User,
	}
	if c.KeyPath != "" {
		fields["key_path"] = c.KeyPath
	}
	return fields
}

// StaticSkillInfo is a convenience type that produces skill fingerprint fields
// from statically known values.
type StaticSkillInfo struct {
	Version  string
	SkillDir string
}

// SkillFields returns a map with version and mtime suitable for fingerprint computation.
// Returns nil if both version is empty and mtime cannot be determined.
func (s *StaticSkillInfo) SkillFields() map[string]interface{} {
	var mtime int64
	if s.SkillDir != "" {
		if info, err := os.Stat(s.SkillDir); err == nil {
			mtime = info.ModTime().Unix()
		}
	}

	if s.Version == "" && mtime == 0 {
		return nil
	}

	fields := map[string]interface{}{}
	if s.Version != "" {
		fields["version"] = s.Version
	}
	if mtime != 0 {
		fields["mtime"] = mtime
	}
	return fields
}

// NewSkillFingerprintProviderFromStatic creates a SkillFingerprintProvider backed
// by a static skill info resolver function. The resolver maps tool names to
// StaticSkillInfo instances.
func NewSkillFingerprintProviderFromStatic(resolver func(toolName string) *StaticSkillInfo) *SkillFingerprintProvider {
	return &SkillFingerprintProvider{
		SkillInfoFunc: func(toolName string) map[string]interface{} {
			info := resolver(toolName)
			if info == nil {
				return nil
			}
			return info.SkillFields()
		},
	}
}

// NewSSHFingerprintProviderFromStatic creates an SSHFingerprintProvider backed
// by a static SSH config resolver function. The resolver provides the current
// SSH host configuration.
func NewSSHFingerprintProviderFromStatic(resolver func(toolName string) *StaticSSHHostConfig) *SSHFingerprintProvider {
	return &SSHFingerprintProvider{
		SSHConfigFunc: func(toolName string) map[string]interface{} {
			cfg := resolver(toolName)
			if cfg == nil {
				return nil
			}
			return cfg.SSHFields()
		},
	}
}

// NewConfigFingerprintProviderFromFields creates a ConfigFingerprintProvider
// backed by a simple fields-per-tool lookup function.
func NewConfigFingerprintProviderFromFields(lookup func(toolName string) map[string]interface{}) *ConfigFingerprintProvider {
	return &ConfigFingerprintProvider{
		ConfigFieldsFunc: lookup,
	}
}

// MakeSkillInfoFields is a helper that builds a skill fingerprint fields map
// from a version string and a directory mtime.
func MakeSkillInfoFields(version string, dirMtime time.Time) map[string]interface{} {
	if version == "" && dirMtime.IsZero() {
		return nil
	}
	fields := map[string]interface{}{}
	if version != "" {
		fields["version"] = version
	}
	if !dirMtime.IsZero() {
		fields["mtime"] = dirMtime.Unix()
	}
	return fields
}
