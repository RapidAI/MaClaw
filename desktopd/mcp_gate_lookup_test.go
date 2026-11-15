package desktopd

import (
	"fmt"
	"testing"
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

func BenchmarkGateScreensLookup(b *testing.B) {
	gates, token := populatedGateScreens(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := gates.lookup(token); !ok {
			b.Fatal("gate token not found")
		}
	}
}
