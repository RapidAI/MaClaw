// Package secretbox helper shims for the SSH credential code path.
//
// P0-4 (2026-09-08 review) called for zero-on-close semantics for SSH
// credentials. Switching SSHHostConfig from `Password string` to a Secret type
// would have rippled to many call sites; this file provides the realistic
// minimum:
//
//   - We zero all local []byte copies of passwords/passphrases we create on
//     the fly (the helper works on byte slices, which Go lets us overwrite).
//   - We zero the temporary buffer used to send the password to the sudo
//     prompt via PTY.
//   - We expose BuildPassphraseSecret so buildKeyAuth can hold the passphrase
//     in a wipeable buffer for the duration of key parsing.
//
// What this does NOT solve: SSHHostConfig.Password / .Passphrase remain plain
// Go strings held in config for the lifetime of the process. Go strings are
// immutable and frequently copied, so they cannot be reliably wiped. Closing
// that gap requires changing SSHHostConfig to carry *secretbox.Secret, which
// touches every SSH call site and belongs in its own change.
package remote

import (
	"github.com/RapidAI/CodeClaw/corelib/secretbox"
)

// SecureZeroPassword wipes the local []byte copy of cfg.Password that this
// package extracts during auth-method construction. Note that cfg.Password
// itself is a Go string and cannot be safely zeroed in place because the
// string header may be interned; the helper therefore operates on a
// freshly-allocated copy and is a defense-in-depth measure, not a guarantee.
//
// Always prefer building SSH credentials through BuildPasswordSecret on the
// caller side and only passing *secretbox.Secret pointers to dialSSH.
func SecureZeroPassword(passwordCopy []byte) {
	secretbox.ZeroBytes(passwordCopy)
}

// BuildPassphraseSecret is BuildPasswordSecret's analogue for SSH key
// passphrases.
func BuildPassphraseSecret(passphrase string) *secretbox.Secret {
	return secretbox.NewSecret(passphrase)
}
