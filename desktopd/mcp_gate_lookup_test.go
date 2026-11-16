package desktopd

import (
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func populatedGateScreens(count int) (*gateScreens, string) {
	gates := &gateScreens{}
	last := ""
	for i := 0; i < count; i++ {
		token := fmt.Sprintf("%032x", i)
		gates.remember(token, gateScreen{TenantID: "tenant", UserID: "user", BotID: fmt.Sprintf("bot-%d", i), Display: fmt.Sprintf(":%d", i)})
		last = token
	}
	return gates, last
}

func TestGateScreensLookupFindsExactScreenAmongMany(t *testing.T) {
	gates, token := populatedGateScreens(1000)
	screen, ok := gates.lookup(token)
	if !ok || screen.Display != ":999" || screen.BotID != "bot-999" {
		t.Fatalf("lookup = %#v, ok=%t", screen, ok)
	}
	if _, ok := gates.lookup("ffffffffffffffffffffffffffffffff"); ok {
		t.Fatal("unknown gate token was accepted")
	}
}

func TestTokenForDesktopUserKeepsEachScreen(t *testing.T) {
	gates := &gateScreens{}
	gates.remember("aaaaaaaaaaaaaaaa", gateScreen{TenantID: "tenant-a", UserID: "alice", Display: ":20"})
	gates.remember("bbbbbbbbbbbbbbbb", gateScreen{TenantID: "tenant-a", UserID: "alice", BotID: "bot_1", Display: ":21"})
	gates.remember("cccccccccccccccc", gateScreen{TenantID: "tenant-a", UserID: "bob", Display: ":20"})
	owner, ok := gates.tokenForDesktopUser("tenant-a", "alice")
	if !ok || owner != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("alice screen = %q ok=%t", owner, ok)
	}
	bot, ok := gates.tokenForDesktopUser("tenant-a", desktop.BotDesktopUser("alice", "bot_1"))
	if !ok || bot != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("bot screen = %q ok=%t", bot, ok)
	}
	other, ok := gates.tokenForDesktopUser("tenant-a", "bob")
	if !ok || other != "cccccccccccccccc" {
		t.Fatalf("bob screen = %q ok=%t", other, ok)
	}
}

func BenchmarkGateScreensLookup(b *testing.B) {
	gates, token := populatedGateScreens(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := gates.lookup(token); !ok {
			b.Fatal("gate token not found")
		}
	}
}
