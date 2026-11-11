package skill

// github_token.go provides a single function to resolve the GitHub API token.
//
// Resolution order:
//  1. GITHUB_TOKEN environment variable (user override)
//  2. Connected GitHub account token (registered via RegisterGitHubTokenProvider
//     — the guiapp wires this to the OAuth credential store)
//  3. Built-in default token (base64-encoded 3 times, for rate limit avoidance)
//
// All GitHub API callers in the codebase should use ResolveGitHubToken()
// instead of reading the environment variable directly.

import (
	"encoding/base64"
	"os"
	"sync/atomic"
)

// defaultGitHubTokenEncoded is the built-in GitHub token, base64-encoded 3 times.
// This token is used for GitHub API rate limit avoidance (unauthenticated
// requests are limited to 10 req/min for search, authenticated to 30 req/min).
const defaultGitHubTokenEncoded = "V2pKb2QxZ3hjREJPVmtZeVVXNXNUV0ZZVmtOaFZFSktWbXBuTWxsWVNrOVNhbWhYWTI1a1ZsRlVUbXBWZWtaUVlsWk9TR1IzUFQwPQ=="

// tokenProvider supplies the connected-account token. Registered by the app
// shell (guiapp) so corelib stays free of credential-store imports.
var tokenProvider atomic.Value // stores func() string

// RegisterGitHubTokenProvider installs a resolver invoked between the env var
// and the built-in fallback. Passing nil removes it. Not required to be called
// — callers without a provider simply fall through to the built-in token.
func RegisterGitHubTokenProvider(fn func() string) {
	if fn == nil {
		tokenProvider.Store(func() string { return "" })
		return
	}
	tokenProvider.Store(fn)
}

func connectedAccountToken() string {
	if fn, ok := tokenProvider.Load().(func() string); ok && fn != nil {
		return fn()
	}
	return ""
}

// ResolveGitHubToken returns the GitHub API token to use.
// Priority: GITHUB_TOKEN env var > connected GitHub account > built-in default token.
// Returns empty string if none is available.
func ResolveGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	if token := connectedAccountToken(); token != "" {
		return token
	}
	return decodeBuiltinToken()
}

func decodeBuiltinToken() string {
	decoded := defaultGitHubTokenEncoded
	for i := 0; i < 3; i++ {
		bytes, err := base64.StdEncoding.DecodeString(decoded)
		if err != nil {
			return ""
		}
		decoded = string(bytes)
	}
	return decoded
}
