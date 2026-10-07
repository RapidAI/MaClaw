package guiapp

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/zalando/go-keyring"
)

// remoteSSHPasswordKeyringService is the OS credential-manager service for
// remote coding SSH passwords. WebView localStorage is wiped often enough
// (profile resets, quota, crashed LevelDB flushes) that it cannot be the
// only copy of a password the user asked us to remember.
const remoteSSHPasswordKeyringService = "MaClaw Remote SSH"

// shouldRememberRemoteSSHPassword reports whether a successful connect may
// keep the password. Incident diagnosis stays explicit: the operator types
// that password for the current investigation only.
func shouldRememberRemoteSSHPassword(kind codingRequestKind) bool {
	return kind != codingRequestInquiry
}

// canonicalRemoteSSHPasswordKey is the shared identity for the OS keyring and
// the frontend vault. Host is case-insensitive and bracket-unwrapped; user
// keeps its case; an invalid port becomes 22.
func canonicalRemoteSSHPasswordKey(host, user string, port int) (string, bool) {
	host = strings.ToLower(normalizeSSHHostInput(host))
	user = sanitizeTaskMetadataTagValue(user)
	if host == "" || user == "" {
		return "", false
	}
	if port <= 0 || port >= 65536 {
		port = 22
	}
	return fmt.Sprintf("%s@%s:%d", user, host, port), true
}

func rememberRemoteSSHPassword(host, user, password string, port int) error {
	key, ok := canonicalRemoteSSHPasswordKey(host, user, port)
	if !ok {
		return nil
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return nil
	}
	if err := keyring.Set(remoteSSHPasswordKeyringService, key, password); err != nil {
		return fmt.Errorf("save remote SSH password: %w", err)
	}
	return nil
}

func recallRemoteSSHPassword(host, user string, port int) (string, error) {
	key, ok := canonicalRemoteSSHPasswordKey(host, user, port)
	if !ok {
		return "", nil
	}
	password, err := keyring.Get(remoteSSHPasswordKeyringService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(password), nil
}

func forgetRemoteSSHPassword(host, user string, port int) error {
	key, ok := canonicalRemoteSSHPasswordKey(host, user, port)
	if !ok {
		return nil
	}
	if err := keyring.Delete(remoteSSHPasswordKeyringService, key); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete remote SSH password: %w", err)
	}
	return nil
}

// RememberRemoteSSHPassword stores an SSH password in the OS keyring.
// Empty identity or password is a no-op. Diagnosis tasks must not call this.
func (a *App) RememberRemoteSSHPassword(host, user, password string, port int) error {
	if err := rememberRemoteSSHPassword(host, user, password, port); err != nil {
		log.Printf("[remote-ssh-password] remember failed: %v", err)
		return err
	}
	return nil
}

// RecallRemoteSSHPassword returns a previously remembered SSH password.
// A missing entry is an empty string so the reconnect form can stay quiet.
func (a *App) RecallRemoteSSHPassword(host, user string, port int) string {
	password, err := recallRemoteSSHPassword(host, user, port)
	if err != nil {
		log.Printf("[remote-ssh-password] recall failed: %v", err)
		return ""
	}
	return password
}

// ForgetRemoteSSHPassword removes one remembered SSH password.
func (a *App) ForgetRemoteSSHPassword(host, user string, port int) error {
	if err := forgetRemoteSSHPassword(host, user, port); err != nil {
		log.Printf("[remote-ssh-password] forget failed: %v", err)
		return err
	}
	return nil
}
