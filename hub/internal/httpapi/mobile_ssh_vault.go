package httpapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// Hub-side SSH credential vault (Phase C): secrets never leave Hub encrypted
// storage; Mobile only learns has_secret. Used by exec_mode=hub_exec.

const (
	mobileSSHVaultEnvKey = "MACLAW_HUB_SSH_VAULT_KEY"
	mobileSSHExecDesktop = "desktop_exec"
	mobileSSHExecHub     = "hub_exec"
)

var mobileSSHVault = struct {
	sync.Mutex
	// key: tenant\x00owner\x00profileID
	secrets map[string]mobileSSHVaultRecord
}{
	secrets: make(map[string]mobileSSHVaultRecord),
}

type mobileSSHVaultRecord struct {
	TenantID            string
	OwnerID             string
	ProfileID           string
	AuthMode            string // password | private_key
	EncryptedSecret     string
	EncryptedPassphrase string // optional for private_key
	UpdatedAt           time.Time
}

func mobileSSHVaultMapKey(tenantID, ownerID, profileID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(ownerID) + "\x00" + strings.TrimSpace(profileID)
}

func mobileSSHVaultKey() []byte {
	seed := strings.TrimSpace(os.Getenv(mobileSSHVaultEnvKey))
	if seed == "" {
		seed = "maclaw-hub-default-ssh-vault-key"
	}
	h := sha256.Sum256([]byte("maclaw-ssh-vault-v1:" + seed))
	return h[:]
}

func mobileSSHVaultEncrypt(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", fmt.Errorf("empty secret")
	}
	key := mobileSSHVaultKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func mobileSSHVaultDecrypt(enc string) string {
	enc = strings.TrimSpace(enc)
	if enc == "" {
		return ""
	}
	data, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return ""
	}
	key := mobileSSHVaultKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	ns := gcm.NonceSize()
	if len(data) < ns+gcm.Overhead()+1 {
		return ""
	}
	plain, err := gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func mobileNormalizeSSHExecMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case mobileSSHExecHub, "hub", "server":
		return mobileSSHExecHub
	default:
		return mobileSSHExecDesktop
	}
}

func mobileNormalizeVaultAuthMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "key", "private_key", "private-key":
		return "private_key"
	default:
		return "password"
	}
}

// MobileSSHVaultListHandler lists vault metadata for the current viewer (no secrets).
//
//	GET /api/mobile/ssh/vault
func MobileSSHVaultListHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		tenantID := strings.TrimSpace(principal.TenantID)
		items := make([]map[string]any, 0)
		mobileSSHVault.Lock()
		for _, rec := range mobileSSHVault.secrets {
			if rec.OwnerID != ownerID {
				continue
			}
			// Tenant match when both sides non-empty.
			if tenantID != "" && rec.TenantID != "" && rec.TenantID != tenantID {
				continue
			}
			items = append(items, map[string]any{
				"profile_id": rec.ProfileID,
				"has_secret": true,
				"auth_mode":  rec.AuthMode,
				"updated_at": rec.UpdatedAt.UTC().Format(time.RFC3339),
			})
		}
		mobileSSHVault.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"items": items,
			"count": len(items),
		})
	}
}

// MobileSSHVaultHandler manages encrypted SSH secrets for hub_exec.
//
//	PUT/POST /api/mobile/ssh/vault/{profileId}  body: {auth_mode, secret, passphrase?}
//	GET      /api/mobile/ssh/vault/{profileId}  → {has_secret, auth_mode, updated_at}
//	DELETE   /api/mobile/ssh/vault/{profileId}
func MobileSSHVaultHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		profileID := sanitizeMobileServerProfileText(r.PathValue("profileId"), 128)
		if profileID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "profile id is required")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		tenantID := strings.TrimSpace(principal.TenantID)
		key := mobileSSHVaultMapKey(tenantID, ownerID, profileID)

		switch r.Method {
		case http.MethodGet:
			mobileSSHVault.Lock()
			rec, ok := mobileSSHVault.secrets[key]
			mobileSSHVault.Unlock()
			if !ok {
				writeJSON(w, http.StatusOK, map[string]any{
					"profile_id": profileID,
					"has_secret": false,
					"auth_mode":  "",
				})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"profile_id": profileID,
				"has_secret": true,
				"auth_mode":  rec.AuthMode,
				"updated_at": rec.UpdatedAt.UTC().Format(time.RFC3339),
			})
		case http.MethodPut, http.MethodPost:
			var body struct {
				AuthMode   string `json:"auth_mode"`
				Secret     string `json:"secret"`
				Passphrase string `json:"passphrase"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
				return
			}
			secret := strings.TrimSpace(body.Secret)
			if secret == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "secret is required")
				return
			}
			if len(secret) > 64*1024 {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "secret too large")
				return
			}
			enc, err := mobileSSHVaultEncrypt(secret)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "VAULT_ENCRYPT_FAILED", "failed to store secret")
				return
			}
			passEnc := ""
			if p := strings.TrimSpace(body.Passphrase); p != "" {
				passEnc, err = mobileSSHVaultEncrypt(p)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "VAULT_ENCRYPT_FAILED", "failed to store passphrase")
					return
				}
			}
			now := time.Now().UTC()
			rec := mobileSSHVaultRecord{
				TenantID:            tenantID,
				OwnerID:             ownerID,
				ProfileID:           profileID,
				AuthMode:            mobileNormalizeVaultAuthMode(body.AuthMode),
				EncryptedSecret:     enc,
				EncryptedPassphrase: passEnc,
				UpdatedAt:           now,
			}
			mobileKnowledgePurgeState.RLock()
			if !mobileOwnerWriteAllowedLocked(tenantID, ownerID) {
				mobileKnowledgePurgeState.RUnlock()
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
				return
			}
			mobileSSHVault.Lock()
			mobileSSHVault.secrets[key] = rec
			mobileSSHVault.Unlock()
			mobileKnowledgePurgeState.RUnlock()
			go mobilePersistState()
			writeJSON(w, http.StatusOK, map[string]any{
				"profile_id": profileID,
				"has_secret": true,
				"auth_mode":  rec.AuthMode,
				"updated_at": now.Format(time.RFC3339),
				"status":     "stored",
			})
		case http.MethodDelete:
			mobileSSHVault.Lock()
			delete(mobileSSHVault.secrets, key)
			mobileSSHVault.Unlock()
			go mobilePersistState()
			writeJSON(w, http.StatusOK, map[string]any{
				"profile_id": profileID,
				"has_secret": false,
				"status":     "deleted",
			})
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET, PUT, POST, or DELETE")
		}
	}
}

func mobileSSHVaultLookup(tenantID, ownerID, profileID string) (mobileSSHVaultRecord, bool) {
	key := mobileSSHVaultMapKey(tenantID, ownerID, profileID)
	mobileSSHVault.Lock()
	defer mobileSSHVault.Unlock()
	rec, ok := mobileSSHVault.secrets[key]
	return rec, ok
}

func mobileFindServerProfile(tenantID, ownerID, profileID string) (mobileServerProfileRecord, bool) {
	profileID = strings.TrimSpace(profileID)
	mobileServerProfiles.Lock()
	defer mobileServerProfiles.Unlock()
	var found mobileServerProfileRecord
	var ok bool
	// Prefer most recently updated matching profile for this owner.
	for _, rec := range mobileServerProfiles.profiles {
		if rec.TenantID != tenantID || rec.OwnerID != ownerID || rec.ProfileID != profileID {
			continue
		}
		if !ok || rec.UpdatedAt.After(found.UpdatedAt) {
			found = rec
			ok = true
		}
	}
	return found, ok
}
