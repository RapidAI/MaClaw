package guiapp

import "testing"

func TestDesktopNodeDirectBase(t *testing.T) {
	for _, tc := range []struct {
		addr, host string
		port       int
		want       string
	}{
		// A wildcard bind is reached through the address the GUI SSH'd to.
		{":18081", "10.0.0.8", 18081, "http://10.0.0.8:18081"},
		{"0.0.0.0:18081", "10.0.0.8", 18081, "http://10.0.0.8:18081"},
		{"[::]:18081", "desk.local", 18081, "http://desk.local:18081"},
		// A loopback bind answers only on that machine: the LAN address
		// would hand out a URL nothing can dial.
		{"127.0.0.1:18081", "10.0.0.8", 18081, "http://127.0.0.1:18081"},
		{"localhost:18081", "10.0.0.8", 18081, "http://127.0.0.1:18081"},
		{"[::1]:18081", "10.0.0.8", 18081, "http://127.0.0.1:18081"},
		// A specific LAN bind is advertised as itself: it may differ from
		// the SSH target (jump host, alias, second interface).
		{"10.0.0.5:18081", "10.0.0.8", 18081, "http://10.0.0.5:18081"},
		// An empty or unreadable env line falls back to the SSH target.
		{"", "10.0.0.8", 18081, "http://10.0.0.8:18081"},
		{"garbage", "10.0.0.8", 18081, "http://10.0.0.8:18081"},
		// An IPv6 SSH target keeps its brackets, bracketed or not, and a
		// stray port on the SSH target is dropped (SSH port ≠ desktopd port).
		{":18081", "fd00::8", 18081, "http://[fd00::8]:18081"},
		{":18081", "[fd00::8]", 18081, "http://[fd00::8]:18081"},
		{"", "10.0.0.8:2222", 18081, "http://10.0.0.8:18081"},
		// A different port follows it.
		{":18082", "10.0.0.8", 18082, "http://10.0.0.8:18082"},
	} {
		if got := desktopNodeDirectBase(tc.addr, tc.host, tc.port); got != tc.want {
			t.Fatalf("desktopNodeDirectBase(%q, %q, %d) = %q, want %q", tc.addr, tc.host, tc.port, got, tc.want)
		}
	}
}

func TestDesktopdURLParts(t *testing.T) {
	if host, port := desktopdURLHostPort("http://10.0.0.9:18082"), desktopdURLPort("http://10.0.0.9:18082"); host != "10.0.0.9:18082" || port != 18082 {
		t.Fatalf("host=%q port=%d", host, port)
	}
	// A normalized URL without an explicit port reads the desktopd default.
	if port := desktopdURLPort("http://10.0.0.9"); port != desktopNodeDesktopdPort {
		t.Fatalf("default port=%d", port)
	}
	// https without an explicit port dials 443, not the desktopd default.
	if port := desktopdURLPort("https://10.0.0.9"); port != 443 {
		t.Fatalf("https port=%d", port)
	}
	if host := desktopdURLHostPort(""); host != "" {
		t.Fatalf("empty host=%q", host)
	}
}
