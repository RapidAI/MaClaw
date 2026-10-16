package guiapp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The envelope contract spans a package boundary guiapp cannot import across
// (hubcenter/internal/skillmarket is internal). The round-trip test proves our
// encryptor and an independent re-implementation of the server algorithm agree,
// but if someone changed the *server* constants, both halves here could agree
// while disagreeing with the deployment.
//
// This test pins our client-side constants to the server source textually, so
// changing the server makes this fail and names the file to update.
func TestTokenBankEnvelopeParamsMatchServerSource(t *testing.T) {
	root := tokenBankRepoRoot(t)
	serverCrypto := filepath.Join(root, "hubcenter", "internal", "skillmarket", "crypto.go")
	serverHandlers := filepath.Join(root, "hubcenter", "internal", "httpapi", "token_bank_share_handlers.go")

	cryptoSource, err := os.ReadFile(serverCrypto)
	if err != nil {
		t.Fatalf("read %s: %v", serverCrypto, err)
	}
	handlerSource, err := os.ReadFile(serverHandlers)
	if err != nil {
		t.Fatalf("read %s: %v", serverHandlers, err)
	}

	checks := []struct {
		name    string
		source  string
		pattern string
		why     string
	}{
		{
			name:    "PBKDF2 iteration count",
			source:  string(cryptoSource),
			pattern: `pbkdf2Iter\s*=\s*_?100_?000`,
			why:     "tokenBankEnvelopeIter must equal the server's pbkdf2Iter",
		},
		{
			name:    "AES key length",
			source:  string(cryptoSource),
			pattern: `aesKeyLen\s*=\s*32`,
			why:     "tokenBankEnvelopeKeyLen must equal the server's aesKeyLen",
		},
		{
			name:    "salt owner domain separator",
			source:  string(handlerSource),
			pattern: `tokenBankEnvelopeSaltOwner\s*=\s*"` + regexp.QuoteMeta(tokenBankEnvelopeSaltOwner) + `"`,
			why:     "tokenBankEnvelopeSaltOwner must equal the server's salt owner",
		},
		{
			name:    "public key endpoint",
			source:  string(handlerSource) + readIfExists(t, filepath.Join(root, "hubcenter", "internal", "httpapi", "router.go")),
			pattern: `/api/v1/crypto/pubkey`,
			why:     "tokenBankPublicKey fetches this path",
		},
	}

	for _, check := range checks {
		re, err := regexp.Compile(check.pattern)
		if err != nil {
			t.Fatalf("bad pattern for %s: %v", check.name, err)
		}
		if !re.MatchString(check.source) {
			t.Errorf("%s: server source no longer matches %q (%s)", check.name, check.pattern, check.why)
		}
	}
}

// tokenBankRepoRoot walks up from the test working directory (guiapp/) to the
// module root so the relative paths above stay stable.
func tokenBankRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate the module root above %s", dir)
	return ""
}

func readIfExists(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// A sanity check on the walker itself: if the root discovery silently failed,
// every pattern check above would run against an empty string and pass.
func TestTokenBankRepoRootFindsModuleRoot(t *testing.T) {
	root := tokenBankRepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root %q has no go.mod: %v", root, err)
	}
	if !strings.HasSuffix(filepath.ToSlash(root), "/aicoder") {
		t.Logf("module root is %q (expected to end in /aicoder)", root)
	}
}
