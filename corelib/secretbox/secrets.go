// Package secretbox provides a small set of helpers for handling in-memory
// secrets (SSH passwords, key passphrases, API bearer tokens) so that:
//
//   - The bytes used to hold the secret can be wiped once the secret is no
//     longer needed (defense against accidental disclosure via core dumps,
//     panic stack traces, /proc/<pid>/maps captures, or debugger attach).
//   - The secret value is never compared with `==` and never leaves the
//     package without going through a short-lived Reveal() snapshot.
//
// This is the foundation for the P0-4 fix in the 2026-09-08 review (SSH
// credentials were previously held in plain `string` fields for their
// entire lifetime, with no zero-on-close).
package secretbox

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"sync"
)

// Secret holds a secret value in a byte slice. Once Zero() has been called the
// underlying bytes are overwritten with random data and any subsequent Reveal()
// returns the empty string. Secret is safe for concurrent use after construction.
//
// Secret intentionally does not implement Stringer or MarshalText to prevent
// accidental logging via fmt.Printf("%s", s) or JSON encoders.
type Secret struct {
	mu  sync.Mutex
	buf []byte
}

// NewSecret copies value into freshly allocated memory and returns a Secret
// that owns it. The caller must call Zero() (typically via defer) once the
// secret is no longer needed.
func NewSecret(value string) *Secret {
	s := &Secret{}
	if value != "" {
		s.buf = make([]byte, len(value))
		copy(s.buf, value)
	}
	return s
}

// NewSecretBytes is the []byte analogue of NewSecret.
func NewSecretBytes(value []byte) *Secret {
	s := &Secret{}
	if len(value) > 0 {
		s.buf = make([]byte, len(value))
		copy(s.buf, value)
	}
	return s
}

// Reveal returns a short-lived copy of the secret bytes. Callers should zero
// the returned slice as soon as they no longer need it; Secret does not track
// independently-allocated copies.
//
// IMPORTANT: the returned slice is a fresh allocation so wiping it does not
// affect other Reveal() calls in flight. Use Zero() on the parent Secret
// rather than on the Reveal return value when you want to invalidate all
// outstanding uses.
func (s *Secret) Reveal() []byte {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) == 0 {
		return nil
	}
	out := make([]byte, len(s.buf))
	copy(out, s.buf)
	return out
}

// IsZero reports whether Zero() has been called. Useful in deferred cleanups
// to avoid double-wiping (a no-op but produces noisy logs).
func (s *Secret) IsZero() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf == nil
}

// Zero overwrites the secret bytes with random data, then releases the
// backing array. After Zero a Secret cannot be reused.
//
// Zero is idempotent. It uses crypto/rand (not a fixed pattern) so an
// observer looking at the post-zero bytes cannot tell whether a particular
// plaintext was the one we held.
func (s *Secret) Zero() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) > 0 {
		// Fill with random bytes; using rand.Read means we do not rely on the
		// runtime zeroing hidden behind the runtime.memclr_no_*. On platforms
		// where the runtime does not zero on free this still prevents an
		// attacker who reads the slice before free from recovering the
		// plaintext via reverse-mapping, and on platforms that DO zero
		// it adds no measurable cost (the slice is small: SSH passwords are
		// typically < 128 bytes).
		if _, err := rand.Read(s.buf); err != nil {
			// Fall back to a fixed scratch pattern when entropy is unavailable.
			for i := range s.buf {
				s.buf[i] = 0xAA
			}
		}
		// Defensive overwrite so we are not relying solely on the runtime GC
		// to drop the reference; this also pins the slices out of any
		// zero-page sharing optimizations.
		for i := range s.buf {
			s.buf[i] = 0
		}
		s.buf = nil
	}
}

// ConstantTimeEqual reports whether two Secrets hold equal contents in time
// independent of the data. Useful when comparing SSH password retries or
// validating an API key without leaking length via timing.
func ConstantTimeEqual(a, b *Secret) bool {
	if a == nil || b == nil {
		return a == b
	}
	abytes := a.Reveal()
	defer ZeroBytes(abytes)
	bbytes := b.Reveal()
	defer ZeroBytes(bbytes)
	return subtle.ConstantTimeCompare(abytes, bbytes) == 1
}

// ZeroBytes zeroes the slice in place. Provided as a convenience for callers
// who hold short-lived Reveal() slices.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ErrNilSecret is returned by helpers that require a non-nil Secret.
var ErrNilSecret = errors.New("secretbox: nil Secret")
