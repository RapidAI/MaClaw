package guiapp

import "testing"

func TestDesktopBotCreateBodyOmitsServerWithoutAComputer(t *testing.T) {
	with := desktopBotCreateBody("Bot", "", "#336699", "circle", true, "dn_a", true)
	if with["desktop"] != true || with["node_id"] != "dn_a" {
		t.Fatalf("computer bot = %#v", with)
	}
	cloud := desktopBotCreateBody("Bot", "", "", "", true, "  ", true)
	if cloud["desktop"] != true || cloud["node_id"] != "" {
		t.Fatalf("cloud bot = %#v", cloud)
	}
	plain := desktopBotCreateBody("Bot", "", "", "", false, "dn_a", true)
	if plain["desktop"] != false {
		t.Fatalf("desktop = %#v", plain["desktop"])
	}
	if _, ok := plain["node_id"]; ok {
		t.Fatalf("bot without a computer picked a server: %#v", plain)
	}
}
