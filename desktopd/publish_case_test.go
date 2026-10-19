package desktopd

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopBotPublishCases(t *testing.T) {
	t.Run("published_port_binds_loopback", func(t *testing.T) {
		if got, err := ParsePublishBind(""); err != nil || got != "127.0.0.1" {
			t.Fatalf("default bind %q %v", got, err)
		}
		for _, raw := range []string{"0.0.0.0", "8.8.8.8", "::1", "100.64.0.1"} {
			if _, err := ParsePublishBind(raw); err == nil {
				t.Fatalf("%s was accepted", raw)
			}
		}
		if got, err := ParsePublishBind("10.1.2.3"); err != nil || got != "10.1.2.3" {
			t.Fatalf("private bind %q %v", got, err)
		}

		token := strings.Repeat("ab", 16)
		ownerKey, err := desktop.UserKey("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		mounts, err := desktop.PrivateMounts("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		mountText := make([]string, 0, len(mounts))
		for _, mount := range mounts {
			mountText = append(mountText, mount.Volume+"="+mount.Target)
		}
		inspectLine := "true|1g|" + strings.Join(mountText, " ") + "|maclaw-gui:2|maclaw-gui:2|"
		var gatePath, gateHost, gateAuth string
		var gateHits int
		gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gateHits++
			gatePath = r.URL.Path
			gateHost = r.Host
			gateAuth = r.Header.Get("Authorization")
			if r.URL.RawQuery != "" {
				gatePath += "?" + r.URL.RawQuery
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer gate.Close()
		gatePort := gate.Listener.Addr().(*net.TCPAddr).Port

		exists := false
		var calls []string
		fixed := time.Date(2026, 10, 18, 4, 2, 0, 0, time.UTC)
		root := t.TempDir()
		svc := &Service{
			AdvertiseHost: "docker.example",
			ComputerDir:   root,
			Now:           func() time.Time { return fixed },
			Run: func(_ context.Context, args ...string) (string, error) {
				line := strings.Join(args, " ")
				calls = append(calls, line)
				switch args[0] {
				case "image":
					if strings.Contains(line, ":state") {
						return "", errString("Error: No such image")
					}
					return "sha256:img", nil
				case "inspect":
					if !exists {
						return "", errString("Error: No such object")
					}
					return inspectLine, nil
				case "run":
					exists = true
					return "container", nil
				case "update":
					return "ok", nil
				case "exec":
					return `{"display":":20","cdp_port":19020,"token":"` + token + `"}`, nil
				case "port":
					if !strings.Contains(line, "maclaw-desktop-"+ownerKey) {
						return "", errString("Error: No such container")
					}
					if strings.Contains(line, "6080") {
						return "0.0.0.0:6081", nil
					}
					return "0.0.0.0:" + strconv.Itoa(gatePort), nil
				default:
					return "", errString("unexpected " + args[0])
				}
			},
		}
		spec := Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", Memory: "2g", CPUs: "1", ShmSize: "1g"}
		session, err := svc.OpenSession(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		wantCDP := "https://desktop:" + token + "@docker.example/v1/desktops/tunnel/" + ownerKey + "/cdp"
		wantNovnc := "https://desktop:" + token + "@docker.example/v1/desktops/tunnel/" + ownerKey + "/vnc/vnc.html?autoconnect=1&resize=scale"
		if session.CDP != wantCDP || session.Novnc != wantNovnc || session.Display != ":20" {
			t.Fatalf("session %#v", session)
		}
		parsed, err := url.Parse(session.CDP)
		if err != nil {
			t.Fatal(err)
		}
		password, _ := parsed.User.Password()
		if password != token || strings.Contains(session.CDP, strconv.Itoa(gatePort)) || strings.Contains(session.CDP, "6081") || strings.Contains(session.CDP, "172.") || strings.Contains(session.Novnc, "6081") {
			t.Fatalf("session exposed a published address %#v", session)
		}
		runs, rms := 0, 0
		for _, call := range calls {
			if strings.HasPrefix(call, "run ") {
				runs++
				if !strings.Contains(call, "--publish 127.0.0.1::19020/tcp") || !strings.Contains(call, "--publish 127.0.0.1::6080/tcp") {
					t.Fatalf("publish %s", call)
				}
				if strings.Contains(call, "--publish 19020/tcp") || strings.Contains(call, "--publish 6080/tcp") || strings.Contains(call, "0.0.0.0:") {
					t.Fatalf("publish is still on every interface: %s", call)
				}
			}
			if strings.HasPrefix(call, "rm ") {
				rms++
			}
		}
		if runs != 1 || rms != 0 {
			t.Fatalf("runs=%d rm=%d", runs, rms)
		}

		svc.PublicURL = "https://public.example/desk"
		again, err := svc.OpenSession(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		if again.CDP != "https://desktop:"+token+"@public.example/desk/v1/desktops/tunnel/"+ownerKey+"/cdp" {
			t.Fatalf("public url %#v", again)
		}
		runs = 0
		rms = 0
		for _, call := range calls {
			if strings.HasPrefix(call, "run ") {
				runs++
			}
			if strings.HasPrefix(call, "rm ") {
				rms++
			}
		}
		if runs != 1 || rms != 0 {
			t.Fatalf("second open recreated the container runs=%d rm=%d", runs, rms)
		}

		meta, err := svc.ReadComputerMeta("tenant", "alice")
		if err != nil {
			t.Fatal(err)
		}
		if meta.LastActivity != fixed.Format(time.RFC3339Nano) {
			t.Fatalf("activity %s", meta.LastActivity)
		}
		before := len(calls)
		svc.Now = func() time.Time { return fixed.Add(time.Hour) }
		h := Handler(svc, "api-token", "", nil, nil)
		req := httptest.NewRequest(http.MethodGet, "/v1/desktops/tunnel/"+ownerKey+"/cdp/json/version?x=1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent || gatePath != "/json/version?x=1" || gateAuth != "Bearer "+token || gateHost != "127.0.0.1:"+strconv.Itoa(gatePort) {
			t.Fatalf("status=%d path=%q auth=%q host=%q body=%s", rec.Code, gatePath, gateAuth, gateHost, rec.Body.String())
		}
		if len(calls) != before {
			t.Fatalf("tunnel called docker: %d", len(calls)-before)
		}
		open, err := svc.ReadComputerMeta("tenant", "alice")
		if err != nil || open.LastActivity != meta.LastActivity {
			t.Fatalf("tunnel refreshed activity %q", open.LastActivity)
		}

		hits := gateHits
		bare := httptest.NewRequest(http.MethodGet, "/v1/desktops/tunnel/"+ownerKey+"/cdp/json/version", nil)
		bareRec := httptest.NewRecorder()
		h.ServeHTTP(bareRec, bare)
		if bareRec.Code != http.StatusNoContent || gateAuth != "" || gateHits != hits+1 {
			t.Fatalf("missing api token was refused status=%d auth=%q hits=%d", bareRec.Code, gateAuth, gateHits)
		}

		hits = gateHits
		climbed := httptest.NewRequest(http.MethodGet, "/v1/desktops/tunnel/"+ownerKey+"/cdp/json/version", nil)
		climbed.URL.Path = "/v1/desktops/tunnel/" + ownerKey + "/cdp/../secret"
		climbRec := httptest.NewRecorder()
		h.ServeHTTP(climbRec, climbed)
		if climbRec.Code != http.StatusNotFound || gateHits != hits {
			t.Fatalf("climb status=%d hits=%d", climbRec.Code, gateHits)
		}

		other := httptest.NewRequest(http.MethodGet, "/v1/desktops/tunnel/"+strings.Repeat("0", 16)+"/cdp/json/version", nil)
		otherRec := httptest.NewRecorder()
		hits = gateHits
		h.ServeHTTP(otherRec, other)
		if otherRec.Code != http.StatusBadGateway || gateHits != hits {
			t.Fatalf("other user status=%d hits=%d body=%s", otherRec.Code, gateHits, otherRec.Body.String())
		}

		var badCalls []string
		bad := &Service{AdvertiseHost: "docker.example", PublishBind: "0.0.0.0", Run: func(_ context.Context, args ...string) (string, error) {
			line := strings.Join(args, " ")
			badCalls = append(badCalls, line)
			switch args[0] {
			case "image":
				if strings.Contains(line, ":state") {
					return "", errString("Error: No such image")
				}
				return "sha256:img", nil
			case "inspect":
				return "", errString("Error: No such object")
			default:
				return "", errString("unexpected " + args[0])
			}
		}}
		if _, err := bad.OpenSession(context.Background(), spec); err == nil {
			t.Fatal("0.0.0.0 bind opened a session")
		}
		for _, call := range badCalls {
			if strings.HasPrefix(call, "run ") {
				t.Fatalf("0.0.0.0 published: %s", call)
			}
		}

		var privateRun string
		private := &Service{AdvertiseHost: "docker.example", PublishBind: "10.1.2.3", Run: func(_ context.Context, args ...string) (string, error) {
			line := strings.Join(args, " ")
			if args[0] == "run" {
				privateRun = line
				return "container", nil
			}
			switch args[0] {
			case "image":
				if strings.Contains(line, ":state") {
					return "", errString("Error: No such image")
				}
				return "sha256:img", nil
			case "inspect":
				return "", errString("Error: No such object")
			case "exec":
				return `{"display":":20","cdp_port":19020,"token":"` + token + `"}`, nil
			case "port":
				return "10.1.2.3:32768", nil
			default:
				return "", errString("unexpected " + args[0])
			}
		}}
		privateSession, err := private.OpenSession(context.Background(), spec)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(privateRun, "--publish 10.1.2.3::19020/tcp") || !strings.Contains(privateRun, "--publish 10.1.2.3::6080/tcp") || strings.Contains(privateRun, "--publish 19020/tcp") {
			t.Fatalf("private publish %s", privateRun)
		}
		if strings.Contains(privateSession.CDP, "32768") || strings.Contains(privateSession.CDP, "10.1.2.3") {
			t.Fatalf("private session used the published address %s", privateSession.CDP)
		}
	})
}
