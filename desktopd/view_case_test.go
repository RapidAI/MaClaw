package desktopd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// peekDocker answers screen and display for already-running isolated
// screens inside one owner's container. It never has to start anything.
func peekDocker(t *testing.T, calls *[]string, screens map[string]struct {
	display, token, profile string
	running                 bool
}) func(context.Context, ...string) (string, error) {
	t.Helper()
	return func(_ context.Context, args ...string) (string, error) {
		line := strings.Join(args, " ")
		*calls = append(*calls, line)
		switch {
		case strings.Contains(line, " run ") || strings.Contains(line, " rm ") || strings.Contains(line, " start ") || strings.Contains(line, "desktop_supervisor.py ensure") || strings.Contains(line, " restore"):
			t.Fatalf("peek started or changed the desktop: %s", line)
		case args[0] == "exec" && strings.Contains(line, "desktop_supervisor.py screen"):
			key := args[len(args)-1]
			screen, ok := screens[key]
			if !ok {
				return "", errDocker("screen missing")
			}
			return `{"display":"` + screen.display + `","cdp":"http://127.0.0.1:18021","profile":"` + screen.profile + `","token":"` + screen.token + `"}`, nil
		case args[0] == "exec" && strings.Contains(line, "desktop_supervisor.py display"):
			key := args[len(args)-1]
			screen, ok := screens[key]
			if !ok {
				return `{"display":":0","running":false}`, nil
			}
			if screen.running {
				return `{"display":"` + screen.display + `","running":true}`, nil
			}
			return `{"display":"` + screen.display + `","running":false}`, nil
		}
		t.Fatalf("unexpected docker %s", line)
		return "", nil
	}
}

func errDocker(msg string) error { return dockerExecError(msg) }

type dockerExecError string

func (e dockerExecError) Error() string { return string(e) }

func TestPeekSessionIsolatedScreensHaveOwnURLsAndNeverStart(t *testing.T) {
	ownerKey, err := desktop.UserKey("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	bot1, err := desktop.BotKey("tenant", "alice", "bot_1")
	if err != nil {
		t.Fatal(err)
	}
	bot2, err := desktop.BotKey("tenant", "alice", "bot_2")
	if err != nil {
		t.Fatal(err)
	}
	token1 := strings.Repeat("a", 32)
	token2 := strings.Repeat("b", 32)
	screens := map[string]struct {
		display, token, profile string
		running                 bool
	}{
		bot1: {display: ":23", token: token1, profile: "/desktops/" + ownerKey + "/screens/23/profile", running: true},
		bot2: {display: ":24", token: token2, profile: "/desktops/" + ownerKey + "/screens/24/profile", running: true},
	}
	var calls []string
	svc := &Service{AdvertiseHost: "docker.example", Run: peekDocker(t, &calls, screens)}

	first, err := svc.PeekSession(context.Background(), Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1"})
	if err != nil {
		t.Fatal(err)
	}
	want1 := "https://desktop:" + token1 + "@docker.example/v1/desktops/tunnel/" + ownerKey + "/vnc/vnc.html?autoconnect=1&resize=scale"
	if first.Novnc != want1 || first.Display != ":23" || !strings.Contains(first.CDP, token1) || strings.Contains(first.Novnc, token2) {
		t.Fatalf("bot_1 view %#v", first)
	}

	// The same container, a different isolated screen: a different token,
	// still the owner's tunnel, never Bot 1's URL.
	second, err := svc.PeekSession(context.Background(), Spec{TenantID: "tenant", UserID: desktop.BotDesktopUser("alice", "bot_2")})
	if err != nil {
		t.Fatal(err)
	}
	want2 := "https://desktop:" + token2 + "@docker.example/v1/desktops/tunnel/" + ownerKey + "/vnc/vnc.html?autoconnect=1&resize=scale"
	if second.Novnc != want2 || second.Display != ":24" || second.Novnc == first.Novnc {
		t.Fatalf("bot_2 view %#v (bot_1 was %#v)", second, first)
	}

	// A slept screen stays empty.
	screens[bot1] = struct {
		display, token, profile string
		running                 bool
	}{display: ":23", token: token1, profile: "/desktops/" + ownerKey + "/screens/23/profile", running: false}
	asleep, err := svc.PeekSession(context.Background(), Spec{TenantID: "tenant", UserID: "alice", BotID: "bot_1"})
	if err != nil || asleep.Novnc != "" {
		t.Fatalf("slept screen %#v err=%v", asleep, err)
	}

	joined := strings.Join(calls, "\n")
	for _, bad := range []string{" run ", " rm ", " start ", " ensure ", "restore"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("peek woke the desktop via %q:\n%s", bad, joined)
		}
	}
}

func TestPeekSessionHTTPDoesNotStartAndReturnsPerBotURL(t *testing.T) {
	ownerKey, err := desktop.UserKey("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	bot2, err := desktop.BotKey("tenant", "alice", "bot_2")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("c", 32)
	screens := map[string]struct {
		display, token, profile string
		running                 bool
	}{
		bot2: {display: ":38", token: token, profile: "/desktops/" + ownerKey + "/screens/38/profile", running: true},
	}
	var calls []string
	svc := &Service{AdvertiseHost: "docker.example", Run: peekDocker(t, &calls, screens)}
	h := Handler(svc, "tok", "", nil, nil)

	// Isolated bot, no prior session: the body is the desktop user Hub
	// already uses, and the answer is that bot's own page.
	body := `{"tenant_id":"tenant","user_id":"` + desktop.BotDesktopUser("alice", "bot_2") + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/desktops/view", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var session Session
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	want := "https://desktop:" + token + "@docker.example/v1/desktops/tunnel/" + ownerKey + "/vnc/vnc.html?autoconnect=1&resize=scale"
	if session.Novnc != want || session.Display != ":38" {
		t.Fatalf("view %#v", session)
	}
	if strings.Contains(strings.Join(calls, "\n"), "ensure") {
		t.Fatal("HTTP view started the desktop")
	}

	// A screen that was never opened stays empty, still 200.
	req = httptest.NewRequest(http.MethodPost, "/v1/desktops/view", strings.NewReader(`{"tenant_id":"tenant","user_id":"alice","bot_id":"bot_missing"}`))
	req.Header.Set("Authorization", "Bearer tok")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("missing screen status %d %s", rec.Code, rec.Body.String())
	}
	session = Session{}
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil || session.Novnc != "" {
		t.Fatalf("missing screen %#v err=%v", session, err)
	}
}
