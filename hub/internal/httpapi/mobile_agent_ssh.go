package httpapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// mobileAgentSSHHosts builds preconfigured SSH host labels for the Mobile AI
// assistant from the viewer's Hub server profiles + vault secrets (hub_exec).
//
// Secrets never leave Hub process memory except as temporary key files under
// the per-user agent data directory (private_key auth). Password auth injects
// the vault secret into SSHHostEntry.Password for label-based connect only.
//
// AllowDirectSSH stays false on the mobile CoreAgentExecutor: the model can
// only connect via these labels, not invent arbitrary host/password targets.
func mobileAgentSSHHosts(principal *auth.ViewerPrincipal, materializeDir string) []corelib.SSHHostEntry {
	if principal == nil {
		return nil
	}
	// HubSSHExec is currently enabled for all mobile plans (hub_exec path).
	mobileEnsureStateLoaded()
	ownerID := mobilePrincipalOwnerID(principal)
	tenantID := strings.TrimSpace(principal.TenantID)
	if ownerID == "" {
		return nil
	}
	userID := strings.TrimSpace(principal.UserID)

	type profileVault struct {
		profile mobileServerProfileRecord
		vault   mobileSSHVaultRecord
	}
	var candidates []profileVault

	mobileServerProfiles.Lock()
	for _, rec := range mobileServerProfiles.profiles {
		own := strings.TrimSpace(rec.OwnerID)
		if own != ownerID && (userID == "" || own != userID) {
			continue
		}
		if tenantID != "" && strings.TrimSpace(rec.TenantID) != "" &&
			strings.TrimSpace(rec.TenantID) != tenantID {
			continue
		}
		if strings.TrimSpace(rec.Host) == "" || strings.TrimSpace(rec.Username) == "" {
			continue
		}
		candidates = append(candidates, profileVault{profile: rec})
	}
	mobileServerProfiles.Unlock()
	if len(candidates) == 0 {
		return nil
	}

	out := make([]corelib.SSHHostEntry, 0, len(candidates))
	seenLabel := make(map[string]struct{}, len(candidates))

	mobileSSHVault.Lock()
	defer mobileSSHVault.Unlock()

	ownerKeys := []string{ownerID}
	if userID != "" && userID != ownerID {
		ownerKeys = append(ownerKeys, userID)
	}

	for _, c := range candidates {
		var vault mobileSSHVaultRecord
		ok := false
		for _, oid := range ownerKeys {
			key := mobileSSHVaultMapKey(c.profile.TenantID, oid, c.profile.ProfileID)
			if rec, found := mobileSSHVault.secrets[key]; found {
				vault, ok = rec, true
				break
			}
			if tenantID != "" && c.profile.TenantID != tenantID {
				if rec, found := mobileSSHVault.secrets[mobileSSHVaultMapKey(tenantID, oid, c.profile.ProfileID)]; found {
					vault, ok = rec, true
					break
				}
			}
		}
		if !ok {
			// Fallback: scan vault for matching profile+owner.
			for _, rec := range mobileSSHVault.secrets {
				if rec.ProfileID != c.profile.ProfileID {
					continue
				}
				if rec.OwnerID == ownerID || (userID != "" && rec.OwnerID == userID) {
					vault = rec
					ok = true
					break
				}
			}
		}
		if !ok || strings.TrimSpace(vault.EncryptedSecret) == "" {
			continue
		}
		entry, err := mobileAgentSSHHostFromVault(c.profile, vault, materializeDir)
		if err != nil || entry == nil {
			continue
		}
		labelKey := strings.ToLower(entry.Label)
		if _, dup := seenLabel[labelKey]; dup {
			// Disambiguate duplicate display names with profile id.
			entry.Label = entry.Label + "-" + sanitizePathSegment(c.profile.ProfileID)
			labelKey = strings.ToLower(entry.Label)
			if _, still := seenLabel[labelKey]; still {
				continue
			}
		}
		seenLabel[labelKey] = struct{}{}
		out = append(out, *entry)
	}
	return out
}

func mobileAgentSSHHostFromVault(
	profile mobileServerProfileRecord,
	vault mobileSSHVaultRecord,
	materializeDir string,
) (*corelib.SSHHostEntry, error) {
	secret := mobileSSHVaultDecrypt(vault.EncryptedSecret)
	if secret == "" {
		return nil, fmt.Errorf("empty vault secret")
	}
	label := strings.TrimSpace(profile.Name)
	if label == "" {
		label = strings.TrimSpace(profile.ProfileID)
	}
	if label == "" {
		label = strings.TrimSpace(profile.Host)
	}
	port := profile.Port
	if port <= 0 {
		port = 22
	}
	entry := &corelib.SSHHostEntry{
		Label: label,
		Host:  strings.TrimSpace(profile.Host),
		Port:  port,
		User:  strings.TrimSpace(profile.Username),
	}
	switch mobileNormalizeVaultAuthMode(vault.AuthMode) {
	case "private_key":
		entry.AuthMethod = "key"
		keyPath, err := mobileMaterializeSSHPrivateKey(materializeDir, profile.ProfileID, secret)
		if err != nil {
			return nil, err
		}
		entry.KeyPath = keyPath
		if pass := mobileSSHVaultDecrypt(vault.EncryptedPassphrase); pass != "" {
			entry.Passphrase = pass
		}
	default:
		entry.AuthMethod = "password"
		entry.Password = secret
	}
	return entry, nil
}

func mobileMaterializeSSHPrivateKey(dir, profileID, pem string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("materialize dir required for private key")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// Fixed path per profile so reconnects reuse the same file (0600).
	name := sanitizePathSegment(profileID)
	if name == "" || name == "_unknown" {
		name = "key"
	}
	path := filepath.Join(dir, name+".pem")
	// Always rewrite so rotated vault secrets take effect.
	if err := os.WriteFile(path, []byte(pem), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// mobileAgentSSHSystemHint documents available Linux hosts + live sessions
// (same process-scoped manager style as desktop GUI).
func mobileAgentSSHSystemHint(hosts []corelib.SSHHostEntry, liveSessions []remote.SSHSessionSummary) string {
	if len(hosts) == 0 {
		return strings.TrimSpace(`
Linux SSH / hub_exec status:
- The ssh tool is NOT enabled yet for this account (no vault hosts).
- User setup is intentionally simple — no profile management UI:
  1) Paste in chat: host/IP, username, password (e.g. "查状态 10.0.0.1 root MyPass123" or "root@10.0.0.1 MyPass123"). Hub auto-registers and enables ssh in the same turn; or
  2) Tap「连服务器」and enter only IP, username, password.
- After credentials are stored, the ssh tool appears with a label like user@host. Use ssh(action=connect, label=...) then exec.
- Do NOT invent host credentials. Do NOT claim tools are permanently unavailable — ask for host/user/password in one message if missing.
`)
	}
	var b strings.Builder
	b.WriteString("Linux SSH session management (same kernel as MaClaw GUI — process-scoped per user):\n")
	b.WriteString("- Labels only (credentials already in Hub vault). Never pass password/host/user overrides.\n")
	b.WriteString("- Session rules (MUST follow):\n")
	b.WriteString("  1) If a LIVE session exists for the target host below, SKIP connect — only exec with that session_id.\n")
	b.WriteString("  2) Otherwise: connect ONCE with label=... then exec with the returned session_id.\n")
	b.WriteString("  3) Never reconnect because banner/preview looks short — use exec for real command output.\n")
	b.WriteString("  4) Simple status check: at most 1 connect + 1-2 exec (e.g. uptime; free -h; df -h /), then answer in the user's language. No loops.\n")
	b.WriteString("  5) Optional: ssh(action=list) to refresh sessions; close when done.\n")
	b.WriteString("- Available labels:\n")
	for _, h := range hosts {
		auth := strings.TrimSpace(h.AuthMethod)
		if auth == "" {
			auth = "key/password"
		}
		fmt.Fprintf(&b, "  - label=%q host=%s user=%s port=%d auth=%s\n",
			h.Label, h.Host, h.User, h.Port, auth)
	}
	if len(liveSessions) > 0 {
		b.WriteString("- LIVE sessions (reuse these session_id values now):\n")
		for _, s := range liveSessions {
			label := strings.TrimSpace(s.HostLabel)
			if label == "" {
				label = strings.TrimSpace(s.HostID)
			}
			status := strings.TrimSpace(s.Status)
			if status == "" {
				status = "running"
			}
			fmt.Fprintf(&b, "  - session_id=%q host=%q status=%s\n", s.SessionID, label, status)
			if tail := strings.TrimSpace(s.LastOutput); tail != "" {
				// Keep tiny so prompts stay small.
				if len([]rune(tail)) > 120 {
					r := []rune(tail)
					tail = string(r[:120]) + "…"
				}
				fmt.Fprintf(&b, "    last_output: %s\n", strings.ReplaceAll(tail, "\n", " "))
			}
		}
	} else {
		b.WriteString("- LIVE sessions: none yet — connect once when you need the server.\n")
	}
	return strings.TrimSpace(b.String())
}
