package codingruntime

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strconv"
	"strings"
)

// RemoteTarget is the non-secret identity that a host freezes before it starts
// a remote coding attempt. Display labels, mutable DNS aliases and credentials
// are intentionally absent: recovery must bind to the canonical login target,
// remote working directory and a configured host-key pin.
type RemoteTarget struct {
	Host               string
	User               string
	Port               int
	WorkDir            string
	HostKeyFingerprint string
}

// NormalizeRemoteTarget validates and canonicalizes a remote coding target.
// It is deliberately transport-neutral; the SSH host configuration and its
// credentials remain owned by each host adapter.
func NormalizeRemoteTarget(target RemoteTarget) (RemoteTarget, error) {
	target.Host = strings.ToLower(strings.TrimSpace(target.Host))
	target.User = strings.TrimSpace(target.User)
	target.WorkDir = strings.TrimSpace(target.WorkDir)
	target.HostKeyFingerprint = strings.TrimSpace(target.HostKeyFingerprint)
	if target.Port == 0 {
		target.Port = 22
	}
	if target.Host == "" || target.User == "" {
		return RemoteTarget{}, fmt.Errorf("remote coding target requires host and user")
	}
	if target.Port < 1 || target.Port > 65535 {
		return RemoteTarget{}, fmt.Errorf("remote coding target port must be between 1 and 65535")
	}
	if !strings.HasPrefix(target.WorkDir, "/") {
		return RemoteTarget{}, fmt.Errorf("remote coding workdir must be an absolute POSIX path")
	}
	target.WorkDir = path.Clean(target.WorkDir)
	if target.WorkDir == "/" {
		return RemoteTarget{}, fmt.Errorf("remote coding workdir must not be the filesystem root")
	}
	if target.HostKeyFingerprint == "" {
		return RemoteTarget{}, fmt.Errorf("remote coding target requires a pinned SSH host key fingerprint")
	}
	return target, nil
}

// Identity returns a stable, non-secret target binding suitable for
// PolicySnapshot.RemoteTarget and WorkspaceProbe.HostKey. It must not be used
// as a credential or as a replacement for SSH host-key verification.
func (target RemoteTarget) Identity() (string, error) {
	normalized, err := NormalizeRemoteTarget(target)
	if err != nil {
		return "", err
	}
	canonical := normalized.User + "@" + normalized.Host + ":" + strconv.Itoa(normalized.Port) + "\n" + normalized.WorkDir + "\n" + normalized.HostKeyFingerprint
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}
