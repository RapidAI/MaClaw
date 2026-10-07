package desktopd

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func readyMounts(t *testing.T, tenantID, userID string) string {
	t.Helper()
	mounts, err := desktop.PrivateMounts(tenantID, userID)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		parts = append(parts, mount.Volume+"="+mount.Target)
	}
	return strings.Join(parts, " ")
}

func TestRecreateReasonDecidesImageMigration(t *testing.T) {
	mounts := readyMounts(t, "tenant", "alice")
	state, err := desktop.StateImage("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	spec := func(image string) Spec {
		return Spec{TenantID: "tenant", UserID: "alice", Image: image, ShmSize: "1g"}
	}
	cases := []struct {
		name      string
		container containerState
		spec      Spec
		recreate  bool
	}{
		{"no container", containerState{}, spec("maclaw-gui:2"), false},
		{"legacy container still on legacy image", containerState{Exists: true, Shm: "1g", Mounts: mounts, ConfigImage: "maclaw-gui:1"}, spec("maclaw-gui:1"), false},
		{"legacy container from v1 state image stays on v1", containerState{Exists: true, Shm: "1g", Mounts: mounts, ConfigImage: state}, spec("maclaw-gui:1"), false},
		{"legacy container moves to v2", containerState{Exists: true, Shm: "1g", Mounts: mounts, ConfigImage: "maclaw-gui:1"}, spec("maclaw-gui:2"), true},
		{"legacy state-image container moves to v2", containerState{Exists: true, Shm: "1g", Mounts: mounts, ConfigImage: state}, spec("maclaw-gui:2"), true},
		{"labelled v2 container from its state image", containerState{Exists: true, Shm: "1g", Mounts: mounts, Image: "maclaw-gui:2", ConfigImage: state}, spec("maclaw-gui:2"), false},
		{"labelled container moves to a new tag", containerState{Exists: true, Shm: "1g", Mounts: mounts, Image: "maclaw-gui:2", ConfigImage: "maclaw-gui:2"}, spec("maclaw-gui:3"), true},
		{"label wins over config image", containerState{Exists: true, Shm: "1g", Mounts: mounts, Image: "maclaw-gui:2", ConfigImage: "maclaw-gui:1"}, spec("maclaw-gui:2"), false},
		{"shm changed", containerState{Exists: true, Shm: "512m", Mounts: mounts, Image: "maclaw-gui:2"}, spec("maclaw-gui:2"), true},
		{"shm case only", containerState{Exists: true, Shm: "1G", Mounts: mounts, Image: "maclaw-gui:2"}, spec("maclaw-gui:2"), false},
		{"private volumes missing", containerState{Exists: true, Shm: "1g", Image: "maclaw-gui:2"}, spec("maclaw-gui:2"), true},
	}
	for _, tc := range cases {
		reason := recreateReason(tc.container, tc.spec)
		if (reason != "") != tc.recreate {
			t.Errorf("%s: reason=%q want recreate=%v", tc.name, reason, tc.recreate)
		}
	}
}

func TestStateImageIsOnlyUsedForItsOwnBaseImage(t *testing.T) {
	cases := []struct {
		label, requested string
		usable           bool
	}{
		{"", "maclaw-gui:1", true},
		{"<no value>", "maclaw-gui:1", true},
		{"", "maclaw-gui:2", false},
		{"maclaw-gui:1", "maclaw-gui:2", false},
		{"maclaw-gui:2", "maclaw-gui:2", true},
		{"maclaw-gui:2", "maclaw-gui:3", false},
	}
	for _, tc := range cases {
		if got := stateImageUsable(tc.label, tc.requested); got != tc.usable {
			t.Errorf("label=%q requested=%q usable=%v want %v", tc.label, tc.requested, got, tc.usable)
		}
	}
}

func TestParseInspectReadsOldAndNewFormats(t *testing.T) {
	old := parseInspect("true|512m|a=/desktops b=/opt ")
	if !old.Exists || !old.Running || old.Shm != "512m" || old.Image != "" || old.ConfigImage != "" || containerImage(old) != desktop.LegacyImage {
		t.Fatalf("old=%#v", old)
	}
	current := parseInspect("false|1g|a=/desktops |maclaw-gui:2|maclaw-desktop-user-0123456789abcdef:state")
	if current.Running || current.Image != "maclaw-gui:2" || containerImage(current) != "maclaw-gui:2" || current.Mounts != "a=/desktops " {
		t.Fatalf("current=%#v", current)
	}
	missing := parseInspect("true||a=/desktops|<no value>|maclaw-gui:1")
	if missing.Shm != "" || missing.Image != "" || containerImage(missing) != "maclaw-gui:1" {
		t.Fatalf("missing=%#v", missing)
	}
}

// fakeDocker answers the docker calls a session makes for alice. stateLabel
// is the maclaw.image label of her state image ("missing" when there is none).
type fakeDocker struct {
	t          *testing.T
	inspect    string
	stateLabel string
	commands   []string
}

func (f *fakeDocker) run(_ context.Context, args ...string) (string, error) {
	line := strings.Join(args, " ")
	f.commands = append(f.commands, line)
	switch args[0] {
	case "inspect":
		if f.inspect == "" {
			return "", errString("Error: No such object")
		}
		return f.inspect, nil
	case "image":
		if strings.Contains(line, ":state") {
			if f.stateLabel == "missing" {
				return "", errString("Error: No such image")
			}
			return f.stateLabel, nil
		}
		return "sha256:base", nil
	case "commit":
		// The committed state carries the label keepUserLayer passed.
		for i, arg := range args {
			if arg == "--change" && i+1 < len(args) {
				value := strings.TrimPrefix(args[i+1], "LABEL "+desktop.ImageLabel+"=")
				f.stateLabel = strings.Trim(value, `"`)
			}
		}
		return "sha256:state", nil
	case "rmi":
		f.stateLabel = "missing"
		return "untagged", nil
	case "tag", "rm", "run", "stop", "update", "start", "volume", "cp":
		return "ok", nil
	case "exec":
		return `{"display":":20","cdp_port":19020}`, nil
	case "port":
		return "0.0.0.0:32768", nil
	}
	return "", errString("unexpected " + line)
}

func (f *fakeDocker) has(prefix, part string) bool {
	for _, line := range f.commands {
		if strings.HasPrefix(line, prefix) && strings.Contains(line, part) {
			return true
		}
	}
	return false
}

func TestV1DesktopMovesToV2AndKeepsPrivateVolumes(t *testing.T) {
	state, err := desktop.StateImage("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	prev := strings.TrimSuffix(state, ":state") + ":prev"
	name := "maclaw-desktop-" + mustKey(t, "tenant", "alice")
	// A container created by the old desktopd: no maclaw.image label, running
	// from her v1-based state image that has no label either.
	f := &fakeDocker{t: t, inspect: "true|1g|" + readyMounts(t, "tenant", "alice") + "||" + state, stateLabel: ""}
	svc := &Service{AdvertiseHost: "docker.example", Run: f.run}
	if _, err := svc.OpenSession(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", ShmSize: "1g"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.commands, "\n")
	if !f.has("stop ", name) || !f.has("rm -f", name) {
		t.Fatalf("v1 desktop was not replaced:\n%s", joined)
	}
	if !f.has("commit ", `LABEL maclaw.image="maclaw-gui:1"`) {
		t.Fatalf("old layer was not kept with its base image:\n%s", joined)
	}
	if !f.has("tag ", state+" "+prev) || !f.has("rmi ", state) {
		t.Fatalf("stale v1 state image was not set aside:\n%s", joined)
	}
	if strings.Contains(joined, "volume rm") || strings.Contains(joined, "rmi "+prev) {
		t.Fatalf("migration deleted user data:\n%s", joined)
	}
	ran := false
	for _, line := range f.commands {
		if !strings.HasPrefix(line, "run ") || !strings.Contains(line, "--name "+name) {
			continue
		}
		ran = true
		if !strings.Contains(line, " maclaw-gui:2 -c") || strings.Contains(line, state) {
			t.Fatalf("new desktop did not start from v2: %s", line)
		}
		if !strings.Contains(line, "--label maclaw.image=maclaw-gui:2") {
			t.Fatalf("new desktop is not labelled with its image: %s", line)
		}
		mounts, _ := desktop.PrivateMounts("tenant", "alice")
		for _, mount := range mounts {
			if !strings.Contains(line, mount.Volume+":"+mount.Target) {
				t.Fatalf("new desktop lost %s: %s", mount.Target, line)
			}
		}
	}
	if !ran {
		t.Fatalf("no new desktop:\n%s", joined)
	}
	for i, line := range f.commands {
		if strings.HasPrefix(line, "rm -f "+name) {
			flushed := false
			for _, before := range f.commands[:i] {
				if strings.Contains(before, " flush ") {
					flushed = true
				}
			}
			if !flushed {
				t.Fatal("desktop removed before the browser wrote the login")
			}
		}
	}
}

func TestSameImageTagReusesDesktopAndUserLayer(t *testing.T) {
	state, err := desktop.StateImage("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeDocker{t: t, inspect: "false|1g|" + readyMounts(t, "tenant", "alice") + "|maclaw-gui:2|" + state, stateLabel: "maclaw-gui:2"}
	svc := &Service{AdvertiseHost: "docker.example", Run: f.run}
	created, err := svc.Create(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", ShmSize: "1g"})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range f.commands {
		for _, bad := range []string{"rm ", "commit ", "tag ", "rmi ", "run "} {
			if strings.HasPrefix(line, bad) {
				t.Fatalf("same image rebuilt the desktop: %s", line)
			}
		}
	}
	if !f.has("start ", "maclaw-desktop-") || created.Image != state {
		t.Fatalf("desktop=%#v commands=%v", created, f.commands)
	}
}

func TestNewDesktopUsesMatchingStateImage(t *testing.T) {
	state, err := desktop.StateImage("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeDocker{t: t, stateLabel: "maclaw-gui:2"}
	svc := &Service{AdvertiseHost: "docker.example", Run: f.run}
	if _, err := svc.Create(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2"}); err != nil {
		t.Fatal(err)
	}
	if !f.has("run ", state+" -c") || !f.has("run ", "--label maclaw.image=maclaw-gui:2") || f.has("tag ", state) {
		t.Fatalf("matching state image was not used: %v", f.commands)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestScreenshotCapturesTheUsersDisplay(t *testing.T) {
	want := testPNG(t)
	var got []string
	svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
		got = append([]string(nil), args...)
		return base64.StdEncoding.EncodeToString(want) + "\n", nil
	}}
	data, err := svc.Screenshot(context.Background(), "tenant", "alice", "")
	if err != nil || !bytes.Equal(data, want) {
		t.Fatalf("data=%d bytes err=%v", len(data), err)
	}
	name := "maclaw-desktop-" + mustKey(t, "tenant", "alice")
	if len(got) < 6 || got[0] != "exec" || got[1] != "-e" || got[2] != "DISPLAY="+desktop.DefaultDisplay || got[3] != name || got[4] != "sh" {
		t.Fatalf("docker args=%v", got)
	}
	if script := got[len(got)-1]; !strings.Contains(script, "import -window root") || !strings.Contains(script, "scrot") || !strings.Contains(script, "base64 -w0") {
		t.Fatalf("script=%s", script)
	}
	if _, err := svc.Screenshot(context.Background(), "tenant", "alice", ":20; rm -rf /"); err == nil {
		t.Fatal("invalid display was accepted")
	}
	if _, err := svc.Screenshot(context.Background(), "tenant", "", ":20"); err == nil {
		t.Fatal("missing user was accepted")
	}
	bad := &Service{Run: func(context.Context, ...string) (string, error) {
		return base64.StdEncoding.EncodeToString([]byte("not a png")), nil
	}}
	if _, err := bad.Screenshot(context.Background(), "tenant", "alice", ":20"); err == nil {
		t.Fatal("non-PNG output was accepted")
	}
}

func TestScreenshotEndpointNeedsTokenAndReturnsPNG(t *testing.T) {
	want := testPNG(t)
	svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
		return base64.StdEncoding.EncodeToString(want), nil
	}}
	srv := httptest.NewServer(Handler(svc, "primary-token", ""))
	defer srv.Close()
	get := func(token string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/desktops/screenshot?tenant_id=tenant&user_id=alice", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	for _, token := range []string{"", "wrong"} {
		resp := get(token)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q status=%d", token, resp.StatusCode)
		}
	}
	resp := get("primary-token")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, want) {
		t.Fatalf("status=%d type=%q body=%d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/desktops/screenshot", strings.NewReader(`{"tenant_id":"tenant","user_id":"alice","display":"bad"}`))
	req.Header.Set("Authorization", "Bearer primary-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid display status=%d", resp.StatusCode)
	}
}

func TestV2ImageBuildKeepsTheDesktopContract(t *testing.T) {
	raw, err := os.ReadFile("image/Dockerfile.v2")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"ARG BASE_IMAGE=", "ARG APT_MIRROR=", "COPY desktop_supervisor.py /desktop_supervisor.py", `VOLUME ["/desktops"]`, "chromium", "fonts-noto-cjk", "xvfb", "xdotool", "x11vnc", "novnc", "python3-websockify", "imagemagick", "scrot", "xfce4-session", "fcitx5", "libclose_range_shim.so", "/etc/ld.so.preload"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Dockerfile.v2 missing %s", want)
		}
	}
	// /opt and /usr/local are per-user volumes that hide anything the image
	// puts there, and FROM maclaw-gui:1 would tie v2 to the old image.
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "FROM maclaw-gui") || strings.Contains(line, " /opt/") || strings.Contains(line, " /usr/local/") {
			t.Fatalf("Dockerfile.v2 line breaks the per-user volumes: %s", line)
		}
	}
	for _, file := range []string{"image/close_range_shim.c", "image/fcitx5-profile"} {
		if _, err := os.Stat(file); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMissingNewImageLeavesTheOldDesktopRunning(t *testing.T) {
	f := &fakeDocker{t: t, inspect: "true|1g|" + readyMounts(t, "tenant", "alice") + "||maclaw-gui:1", stateLabel: "missing"}
	svc := &Service{AdvertiseHost: "docker.example", Run: func(ctx context.Context, args ...string) (string, error) {
		line := strings.Join(args, " ")
		if strings.HasPrefix(line, "image inspect") && strings.HasSuffix(line, " maclaw-gui:2") {
			f.commands = append(f.commands, line)
			return "", errString("Error: No such image")
		}
		if args[0] == "pull" {
			f.commands = append(f.commands, line)
			return "", errString("pull access denied")
		}
		return f.run(ctx, args...)
	}}
	if _, err := svc.Create(context.Background(), Spec{TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:2", ShmSize: "1g"}); err == nil {
		t.Fatal("missing image was accepted")
	}
	for _, line := range f.commands {
		for _, bad := range []string{"stop ", "rm ", "commit ", "exec "} {
			if strings.HasPrefix(line, bad) {
				t.Fatalf("old desktop was touched before the new image existed: %s", line)
			}
		}
	}
}
