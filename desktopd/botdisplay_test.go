package desktopd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// fakeBotDocker answers the docker calls of a session on an existing,
// running container whose supervisor knows one bot display (:23).
func fakeBotDocker(calls *[]string) func(context.Context, ...string) (string, error) {
	return func(_ context.Context, args ...string) (string, error) {
		line := strings.Join(args, " ")
		*calls = append(*calls, line)
		switch args[0] {
		case "image":
			return "sha256:img", nil
		case "inspect":
			mounts, _ := desktop.PrivateMounts("tenant", "alice")
			parts := []string{}
			for _, m := range mounts {
				parts = append(parts, m.Volume+"="+m.Target)
			}
			return "true|1g|" + strings.Join(parts, " ") + "|maclaw-gui:2|maclaw-gui:2|", nil
		case "exec":
			switch {
			case strings.Contains(line, "desktop_supervisor.py ensure"):
				return `{"display":":23","cdp_port":19020,"token":"` + strings.Repeat("b", 32) + `"}`, nil
			case strings.Contains(line, "desktop_supervisor.py display"):
				return `{"display":":23","running":true}`, nil
			case strings.Contains(line, "desktop_supervisor.py bots-busy"):
				return `{"busy":["x"]}`, nil
			}
			return `{"ok":true}`, nil
		case "port":
			if strings.Contains(line, "6080") {
				return "0.0.0.0:40001", nil
			}
			return "0.0.0.0:40000", nil
		}
		return "", nil
	}
}

func TestBotSessionUsesTheBotKeyAndTheOwnersContainer(t *testing.T) {
	var calls []string
	svc := &Service{AdvertiseHost: "docker.example", Run: fakeBotDocker(&calls)}
	h := Handler(svc, "tok", "", nil, nil)
	body := `{"tenant_id":"tenant","user_id":"alice","bot_id":"bot_1","image":"maclaw-gui:2","memory":"3g","cpus":"1.5","shm_size":"1g"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/desktops/session", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var session Session
	_ = json.Unmarshal(rec.Body.Bytes(), &session)
	botKey, _ := desktop.BotKey("tenant", "alice", "bot_1")
	owner, _ := desktop.UserKey("tenant", "alice")
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "exec maclaw-desktop-"+owner+" python3 /desktop_supervisor.py ensure "+botKey) {
		t.Fatalf("bot ensure not in the owner's container:\n%s", joined)
	}
	token := strings.Repeat("b", 32)
	wantCDP := "https://desktop:" + token + "@docker.example/v1/desktops/tunnel/" + owner + "/cdp"
	if session.Display != ":23" || session.CDP != wantCDP || !strings.Contains(session.Novnc, "/v1/desktops/tunnel/"+owner+"/vnc/vnc.html") || strings.Contains(session.CDP, ":40000") || strings.Contains(session.Novnc, ":40001") {
		t.Fatalf("session %#v", session)
	}
}

func TestBotRequestsUseTheServerDerivedDisplay(t *testing.T) {
	var calls []string
	svc := &Service{AdvertiseHost: "docker.example", Run: fakeBotDocker(&calls)}
	h := Handler(svc, "tok", "", nil, nil)
	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer tok")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	// The caller claims the owner's display; the bot's own display is used.
	if rec := post("/v1/desktops/app", `{"tenant_id":"tenant","user_id":"alice","bot_id":"bot_1","display":":20","args":["key","Return"]}`); rec.Code != 200 {
		t.Fatalf("app %d %s", rec.Code, rec.Body.String())
	}
	var appCall string
	for _, call := range calls {
		if strings.Contains(call, " /desktop_supervisor.py command ") {
			appCall = call
		}
	}
	app := commandPayload(t, strings.Fields(appCall))
	if app.Op != "click" || app.Display != ":23" || strings.Contains(appCall, "DISPLAY=") {
		t.Fatalf("app did not run on the bot display: %+v\n%s", app, appCall)
	}
	// Bot control ops get the derived key; a client key is dropped.
	calls = nil
	if rec := post("/v1/desktops/ctl", `{"tenant_id":"tenant","user_id":"alice","bot_id":"bot_1","request":{"op":"takeover_begin","reason":"login","key":"`+strings.Repeat("c", 32)+`"}}`); rec.Code != 200 {
		t.Fatalf("ctl %d %s", rec.Code, rec.Body.String())
	}
	botKey, _ := desktop.BotKey("tenant", "alice", "bot_1")
	if len(calls) < 2 {
		t.Fatalf("calls %v", calls)
	}
	last := calls[len(calls)-1]
	ctlReq := commandPayload(t, strings.Fields(last))
	raw, err := base64.StdEncoding.DecodeString(ctlReq.Body)
	if err != nil || ctlReq.Display != ":23" || !strings.Contains(string(raw), `"key":"`+botKey+`"`) || strings.Contains(string(raw), strings.Repeat("c", 32)) {
		t.Fatalf("ctl request %s err=%v", raw, err)
	}
	// Without a bot, bot ops are refused and the old display rules hold.
	if rec := post("/v1/desktops/ctl", `{"tenant_id":"tenant","user_id":"alice","request":{"op":"bot_state"}}`); rec.Code != 400 {
		t.Fatalf("owner bot_state %d", rec.Code)
	}
	if rec := post("/v1/desktops/app", `{"tenant_id":"tenant","user_id":"alice","bot_id":"bad id","args":["key","a"]}`); rec.Code != 400 {
		t.Fatalf("bad bot id %d", rec.Code)
	}
}

func TestOwnerStopKeepsTheContainerWhileABotIsBusy(t *testing.T) {
	var calls []string
	svc := &Service{AdvertiseHost: "docker.example", Run: fakeBotDocker(&calls)}
	// An owner with no isolated bot keeps the old stop (no extra exec).
	if _, err := svc.Stop(context.Background(), "tenant", "alice"); err != nil || strings.Contains(strings.Join(calls, "\n"), "bots-busy") {
		t.Fatalf("plain stop %v %v", err, calls)
	}
	calls = nil
	svc.noteBotOwner(Spec{TenantID: "tenant", UserID: "alice"})
	out, err := svc.Stop(context.Background(), "tenant", "alice")
	if err != nil || out.Status != "stopped" {
		t.Fatalf("stop %v %#v", err, out)
	}
	joined := strings.Join(calls, "\n")
	owner, _ := desktop.UserKey("tenant", "alice")
	if strings.Contains(joined, "stop --time") || strings.Contains(joined, "\nstop ") || !strings.Contains(joined, "desktop_supervisor.py stop "+owner) {
		t.Fatalf("container stopped under a busy bot:\n%s", joined)
	}
	// A bot's own stop leaves its display to the idle timer.
	calls = nil
	out, err = svc.Stop(context.Background(), "tenant", desktop.BotDesktopUser("alice", "bot_1"))
	if err != nil || out.Status != "idle" || len(calls) != 0 {
		t.Fatalf("bot stop %v %#v %v", err, out, calls)
	}
}

func base64Decode(s string) (string, error) {
	raw, err := base64StdDecode(s)
	return string(raw), err
}

var base64StdDecode = base64.StdEncoding.DecodeString
