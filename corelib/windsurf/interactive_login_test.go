package windsurf

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestInteractiveDevinProbe opens the Devin login page and checks the live
// exchange. It stays skipped unless DEVIN_INTERACTIVE=1, and it never prints
// the credential itself.
func TestInteractiveDevinProbe(t *testing.T) {
	if os.Getenv("DEVIN_INTERACTIVE") != "1" {
		t.Skip("set DEVIN_INTERACTIVE=1 to open the Devin login page")
	}
	listener, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	fmt.Println("AUTH_URL=" + listener.AuthURL())
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", listener.AuthURL()).Start()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	token, err := listener.Wait(ctx)
	if err != nil {
		t.Fatalf("login wait: %v", err)
	}
	fmt.Println("callback=" + tokenKind(token))

	session, err := ResolveToken(ctx, token)
	if err != nil {
		t.Fatalf("resolve: %s", scrubSecrets(err.Error(), token))
	}
	fmt.Printf("resolved kind=%s server=%s name=%q\n", tokenKind(session.APIKey), session.APIServerURL, session.Name)

	reportMint := func(label, server, key string) string {
		minted, mintErr := mintDevinSession(ctx, server, key)
		if mintErr != nil {
			fmt.Printf("mint %s: %s\n", label, scrubSecrets(mintErr.Error(), token, key))
			return ""
		}
		fmt.Printf("mint %s: ok %s\n", label, tokenKind(minted))
		return minted
	}
	minted := reportMint("resolved-host", session.APIServerURL, session.APIKey)
	if minted == "" && session.APIServerURL != SelfServeAPIServer {
		minted = reportMint("self-serve", SelfServeAPIServer, session.APIKey)
	}

	reportList := func(label, server, key string) {
		models, listErr := ListModels(ctx, server, key)
		if listErr != nil {
			fmt.Printf("list %s: %s\n", label, scrubSecrets(listErr.Error(), token, key, minted))
			return
		}
		fmt.Printf("list %s: ok count=%d first=%s\n", label, len(models), models[0].ID)
	}
	reportList("resolved-key", session.APIServerURL, session.APIKey)
	if minted != "" {
		reportList("minted-session", SelfServeAPIServer, minted)
	}
}

func tokenKind(token string) string {
	token = strings.TrimSpace(token)
	switch {
	case token == "":
		return "empty"
	case strings.HasPrefix(token, "sk-ws-"):
		return fmt.Sprintf("sk-ws len=%d", len(token))
	case strings.HasPrefix(token, devinSessionPrefix):
		return fmt.Sprintf("session len=%d", len(token))
	case strings.HasPrefix(token, "auth1_"):
		return fmt.Sprintf("auth1 len=%d", len(token))
	case strings.HasPrefix(token, "eyJ"):
		return fmt.Sprintf("jwt parts=%d len=%d", len(strings.Split(token, ".")), len(token))
	default:
		return fmt.Sprintf("other len=%d", len(token))
	}
}

func scrubSecrets(msg string, secrets ...string) string {
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) < 12 {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}
