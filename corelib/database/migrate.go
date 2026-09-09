package database

import (
	"fmt"
	"strings"
)

// SecretWriter persists a plaintext password into the host secret provider and
// returns the secret_ref that should replace it. Hosts must not log secret.
type SecretWriter func(profileID, secret string) (secretRef string, err error)

// MigratePlaintextSecrets writes leftover Profile.Password values into the
// secret provider and clears them. Any failure is returned and the original
// slice is left unchanged so the caller can fail-closed.
func MigratePlaintextSecrets(profiles []Profile, write SecretWriter) ([]Profile, error) {
	if write == nil {
		for _, profile := range profiles {
			if strings.TrimSpace(profile.Password) != "" {
				return nil, fmt.Errorf("authentication: plaintext password on profile %s cannot be migrated: no secret writer", profile.ID)
			}
		}
		return profiles, nil
	}
	out := append([]Profile(nil), profiles...)
	for i := range out {
		secret := strings.TrimSpace(out[i].Password)
		if secret == "" {
			continue
		}
		id := strings.TrimSpace(out[i].ID)
		if id == "" {
			return nil, fmt.Errorf("authentication: plaintext password on a profile without id")
		}
		ref, err := write(id, secret)
		if err != nil {
			return nil, fmt.Errorf("authentication: migrate profile %s: %w", id, err)
		}
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return nil, fmt.Errorf("authentication: migrate profile %s: empty secret_ref", id)
		}
		out[i].SecretRef = ref
		out[i].Password = ""
		if out[i].SchemaVersion <= 0 {
			out[i].SchemaVersion = 1
		} else {
			out[i].SchemaVersion++
		}
	}
	return out, nil
}

// PlaintextSecretError is the stable fail-closed reason used when a profile
// still carries a password after migration should have cleared it.
func PlaintextSecretError(profiles []Profile) error {
	for _, profile := range profiles {
		if strings.TrimSpace(profile.Password) != "" {
			return fmt.Errorf("plaintext password present on profile %s; migrate to secret_ref before enabling the database tool", profile.ID)
		}
	}
	return nil
}
