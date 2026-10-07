package desktopd

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestBrowserRestoresTheLoggedInProfile(t *testing.T) {
	raw, err := os.ReadFile("image/desktop_supervisor.py")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "about:blank") {
		t.Fatal("a blank start page replaces the logged-in tabs")
	}
	if !strings.Contains(text, "--restore-last-session") || !strings.Contains(text, "--user-data-dir=%s") || !strings.Contains(text, "--profile-directory=Default") || !strings.Contains(text, "--disable-sync") || !strings.Contains(text, "ThirdPartyStoragePartitioning") {
		t.Fatal("the browser does not restore this user's profile")
	}
	running := text[strings.Index(text, "def desktop_running"):]
	runningNext := strings.Index(running, "\ndef ")
	if runningNext < 0 || !strings.Contains(running[:runningNext], "browser_pid(") || !strings.Contains(running[:runningNext], "--user-data-dir=") {
		t.Fatal("a reused process id would hide the logged-in browser")
	}
	alive := text[strings.Index(text, "def pid_alive"):]
	aliveNext := strings.Index(alive, "\ndef ")
	browser := text[strings.Index(text, "def browser_pid"):]
	browserNext := strings.Index(browser, "\ndef ")
	if aliveNext < 0 || browserNext < 0 || !strings.Contains(alive[:aliveNext], `state != "Z"`) || !strings.Contains(browser[:browserNext], "pid_alive(") {
		t.Fatal("a zombie browser is treated as the logged-in window")
	}
	stop := text[strings.Index(text, "def stop_desktop"):]
	next := strings.Index(stop, "\ndef ")
	if next < 0 || !strings.Contains(stop[:next], "flush_browser(key)") {
		t.Fatal("stopping the desktop does not write the website login first")
	}
	claim := text[strings.Index(text, "def claim_flush"):]
	claimNext := strings.Index(claim, "\ndef ")
	if claimNext < 0 || !strings.Contains(claim[:claimNext], "O_EXCL") || !strings.Contains(claim[:claimNext], "getpid") {
		t.Fatal("a second flush signals the browser again and drops the login write")
	}
	flush := text[strings.Index(text, "def flush_browser("):]
	flushNext := strings.Index(flush, "\ndef ")
	if flushNext < 0 || !strings.Contains(flush[:flushNext], "desktop_lock()") {
		t.Fatal("a new browser starts while the website login is still being written")
	}
	locked := text[strings.Index(text, "def flush_browser_locked"):]
	lockedNext := strings.Index(locked, "\ndef ")
	if lockedNext < 0 || !strings.Contains(locked[:lockedNext], "claim_flush(") || !strings.Contains(locked[:lockedNext], "flush_owner_gone(") || !strings.Contains(locked[:lockedNext], "mark_flush_signaled(") {
		t.Fatal("flush signals the browser on every call, or a dead flush blocks the login write")
	}
	owner := text[strings.Index(text, "def flush_owner_gone"):]
	ownerNext := strings.Index(owner, "\ndef ")
	if ownerNext < 0 || !strings.Contains(owner[:ownerNext], "signaled") {
		t.Fatal("a finished flush signals the browser again and drops the login write")
	}
	persistAt := strings.Index(locked[:lockedNext], "persist_website_login(")
	captureAt := strings.Index(locked[:lockedNext], "capture_page_login(")
	killAt := strings.Index(locked[:lockedNext], "os.kill")
	if persistAt < 0 || captureAt < 0 || killAt < 0 || persistAt > captureAt || captureAt > killAt {
		t.Fatal("the browser exits before the website login is saved in this profile")
	}
	saved := text[strings.Index(text, "def persistent_cookie"):]
	savedNext := strings.Index(saved, "\ndef ")
	if savedNext < 0 || !strings.Contains(saved[:savedNext], `__Host-`) || !strings.Contains(saved[:savedNext], `params["url"]`) || !strings.Contains(saved[:savedNext], `params["partitionKey"]`) {
		t.Fatal("a host-only login cookie is dropped when the desktop stops")
	}
	retry := text[strings.Index(text, "def save_session_cookie"):]
	retryNext := strings.Index(retry, "\ndef ")
	if retryNext < 0 || !strings.Contains(retry[:retryNext], `result.get("success")`) || !strings.Contains(retry[:retryNext], `retry["url"]`) || !strings.Contains(retry[:retryNext], "cookie_page_url") || strings.Index(retry[:retryNext], "except") < 0 || strings.Index(retry[:retryNext], "except") > strings.Index(retry[:retryNext], `retry["url"]`) {
		t.Fatal("a rejected login cookie is not written back into this profile")
	}
	pageURL := text[strings.Index(text, "def cookie_page_url"):]
	pageNext := strings.Index(pageURL, "\ndef ")
	if pageNext < 0 || !strings.Contains(pageURL[:pageNext], "quote(") {
		t.Fatal("a login cookie path that is not a plain URL is dropped on exit")
	}
	persist := text[strings.Index(text, "def persist_website_login"):]
	persistNext := strings.Index(persist, "\ndef ")
	call := text[strings.Index(text, "def call(self, method, params)"):]
	callNext := strings.Index(call, "\ndef ")
	if persistNext < 0 || callNext < 0 || strings.Contains(persist[:persistNext], "Network.enable") || strings.Contains(call[:callNext], "range(20)") || !strings.Contains(call[:callNext], "deadline") {
		t.Fatal("browser events hide the cookie reply and the exit deletes the login")
	}
	read := text[strings.Index(text, "def ws_read"):]
	readNext := strings.Index(read, "\ndef ")
	if readNext < 0 || strings.Contains(read[:readNext], "range(32)") || !strings.Contains(read[:readNext], "ws_client_frame(data, 0x8A)") || !strings.Contains(read[:readNext], "4096") || !strings.Contains(read[:readNext], "pending") {
		t.Fatal("a long cookie list is cut off and the exit deletes the login")
	}
	enter := text[strings.Index(text, "def __enter__"):]
	exitAt := strings.Index(enter, "def __exit__")
	localWS := text[strings.Index(text, "def local_cdp_ws"):]
	localNext := strings.Index(localWS, "\nclass ")
	if exitAt < 0 || localNext < 0 || !strings.Contains(enter[:exitAt], "local_cdp_ws") || !strings.Contains(localWS[:localNext], "127.0.0.1") || !strings.Contains(enter[:exitAt], "self.pending") {
		t.Fatal("the browser address Chrome advertised is not the one that holds the website login")
	}
	if !strings.Contains(call[:callNext], "ws_read(self.sock, self.pending)") {
		t.Fatal("the cookie reply that arrives with the debugger handshake is discarded, and the exit deletes the login")
	}
	if !strings.Contains(call[:callNext], "watch_document") || !strings.Contains(call[:callNext], "document_replaced") {
		t.Fatal("the page already open is treated as the reloaded login")
	}
	start := text[strings.Index(text, "def start_desktop"):]
	startNext := strings.Index(start, "\ndef ")
	if startNext < 0 || !strings.Contains(start[:startNext], "clear_flush_flag") || !strings.Contains(start[:startNext], "keep_website_login") || !strings.Contains(start[:startNext], "restore_page_login") || !strings.Contains(start[:startNext], "browser_pid(profile)") || strings.Contains(start[:startNext], "chrome.pid") {
		t.Fatal("a new browser inherits a flush or drops the website login")
	}
	apply := text[strings.Index(text, "def apply_page_login"):]
	applyNext := strings.Index(apply, "\ndef ")
	scriptAt := strings.Index(apply[:applyNext], "addScriptToEvaluateOnNewDocument")
	reloadAt := strings.Index(apply[:applyNext], "Page.reload")
	removeAt := strings.Index(apply[:applyNext], "removeScriptToEvaluateOnNewDocument")
	navigateAt := strings.Index(apply[:applyNext], "Page.navigate")
	opened := text[strings.Index(text, "def open_same_browser_tab"):]
	openedNext := strings.Index(opened, "\ndef ")
	if scriptAt < 0 || reloadAt < 0 || removeAt < 0 || navigateAt < 0 || openedNext < 0 || scriptAt > reloadAt || reloadAt > removeAt || strings.Contains(apply[:removeAt], "if not wait_for_new_document") || !strings.Contains(apply[:navigateAt], "discard_buffered_cdp") || !strings.Contains(apply[:navigateAt], "Page.getFrameTree") || !strings.Contains(apply[:navigateAt], "baseline_loader") || !strings.Contains(apply[:reloadAt], "session or local or navigate") || !strings.Contains(apply[:navigateAt], "open_same_browser_tab") || !strings.Contains(opened[:openedNext], "Target.createTarget") || !strings.Contains(opened[:openedNext], "targetId") || strings.Contains(opened[:openedNext], "about:blank") {
		t.Fatal("the website reads the new page before this tab's login is put back")
	}
	wait := text[strings.Index(text, "def wait_for_new_document"):]
	waitNext := strings.Index(wait, "\ndef ")
	navigated := text[strings.Index(text, "def main_document_navigated"):]
	navigatedNext := strings.Index(navigated, "\ndef ")
	if waitNext < 0 || navigatedNext < 0 || !strings.Contains(wait[:waitNext], "saw_document") || !strings.Contains(wait[:waitNext], "main_document_navigated") || !strings.Contains(wait[:waitNext], "document_replaced") || !strings.Contains(navigated[:navigatedNext], "parentId") || !strings.Contains(navigated[:navigatedNext], "page_origin") {
		t.Fatal("a blank frame or an embedded frame is treated as the logged-in page")
	}
	restore := text[strings.Index(text, "def restore_page_login"):]
	restoreNext := strings.Index(restore, "\ndef ")
	if restoreNext < 0 || strings.Index(restore[:restoreNext], "apply_page_login(") > strings.Index(restore[:restoreNext], "path.unlink") || !strings.Contains(restore[:restoreNext], "if not applied") || !strings.Contains(restore[:restoreNext], "if not pages or not settled") || !strings.Contains(restore[:restoreNext], "stable >= 8") || !strings.Contains(restore[:restoreNext], "origins.issubset") || !strings.Contains(restore[:restoreNext], "left") {
		t.Fatal("a failed restore deletes the tab login")
	}
	capture := text[strings.Index(text, "def capture_page_login"):]
	captureNext := strings.Index(capture, "\ndef ")
	capAt, breakAt := -1, -1
	if captureNext >= 0 {
		capAt = strings.Index(capture[:captureNext], "len(saved) >= 8")
		breakAt = strings.Index(capture[:captureNext], "break")
	}
	if captureNext < 0 || capAt < 0 || breakAt < capAt || !strings.Contains(capture[capAt:breakAt], "unread = True") || !strings.Contains(capture[:captureNext], "if unread") || !strings.Contains(capture[:captureNext], "merge_unread_page_login") || strings.Index(capture[:captureNext], "merge_unread_page_login") > strings.Index(capture[:captureNext], "write_text") {
		t.Fatal("a tab whose login could not be read is treated as logged out")
	}
	listing := text[strings.Index(text, "def http_json_any"):]
	listingNext := strings.Index(listing, "\ndef ")
	if listingNext < 0 || !strings.Contains(listing[:listingNext], "TimeoutError") || !strings.Contains(listing[:listingNext], "socket.timeout") || !strings.Contains(listing[:listingNext], "http_message_body") {
		t.Fatal("a finished browser listing is dropped before the tab login is saved")
	}
	chunked := text[strings.Index(text, "def http_message_body"):]
	chunkedNext := strings.Index(chunked, "\ndef ")
	if chunkedNext < 0 || !strings.Contains(chunked[:chunkedNext], "chunked") || !strings.Contains(text, "def unchunk_http") {
		t.Fatal("a chunked browser listing is not the tab login")
	}
	if !strings.Contains(text, "restore_on_startup") {
		t.Fatal("stopping the desktop deletes the website session login")
	}
	keep := text[strings.Index(text, "def keep_website_login"):]
	keepNext := strings.Index(keep, "\ndef ")
	if keepNext < 0 || strings.Contains(keep[:keepNext], "exit_type") || strings.Contains(keep[:keepNext], "exited_cleanly") {
		t.Fatal("marking the last exit clean drops the logged-in tabs")
	}
	if !strings.Contains(keep[:keepNext], "block_third_party_cookies") || !strings.Contains(keep[:keepNext], "cookie_controls_mode") || !strings.Contains(keep[:keepNext], `defaults["cookies"] = 1`) {
		t.Fatal("the browser drops the cookies set while the person logged in")
	}
	body := stop[:next]
	if strings.Contains(body, "for pid in pids.values()") {
		t.Fatal("stopping the desktop signals the browser again and drops the login write")
	}
	if !strings.Contains(body, "browser_gone") || !strings.Contains(body, "xvfb") {
		t.Fatal("stopping the desktop closes the display before the login is written")
	}
	proxy := text[strings.Index(text, "def ensure_proxy"):]
	proxyNext := strings.Index(proxy, "\ndef ")
	if proxyNext < 0 || strings.Contains(proxy[:proxyNext], "start_desktop") || strings.Contains(proxy[:proxyNext], "flush_browser") {
		t.Fatal("restoring the browser connection restarts Chromium and drops the login")
	}
	runningBranch := text[strings.Index(text, "if not desktop_running"):]
	runningEnd := strings.Index(runningBranch, "ensure_watch")
	if runningEnd < 0 || !strings.Contains(runningBranch[:runningEnd], "ensure_proxy") {
		t.Fatal("a dead browser connection does not come back while the login stays open")
	}
}

func TestDesktopImageKeepsTheBrowserAndTheDomainIsAPIOnly(t *testing.T) {
	image, err := os.ReadFile("image/Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	text := string(image)
	for _, want := range []string{"COPY desktop_supervisor.py", `VOLUME ["/desktops"]`, "chromium", "fonts-noto-cjk", "xvfb", "xdotool"} {
		if !strings.Contains(text, want) {
			t.Fatalf("desktop image missing %s", want)
		}
	}
	vhost, err := os.ReadFile("deploy/nginx-dockerd.conf.example")
	if err != nil {
		t.Fatal(err)
	}
	conf := string(vhost)
	if !strings.Contains(conf, "proxy_pass http://127.0.0.1:18081;") {
		t.Fatal("dockerd domain does not proxy the API")
	}
	for _, blocked := range []string{"19020", "6080", "websockify"} {
		if strings.Contains(conf, blocked) {
			t.Fatalf("dockerd domain exposes %s", blocked)
		}
	}
}

func TestSessionUsesAdvertisedHostNotContainerIP(t *testing.T) {
	var runArgs []string
	svc := &Service{AdvertiseHost: "docker.example", Run: func(_ context.Context, args ...string) (string, error) {
		runArgs = append(runArgs, strings.Join(args, " "))
		switch args[0] {
		case "image":
			if strings.Contains(strings.Join(args, " "), "maclaw-desktop-user-") {
				return "", errString("Error: No such image")
			}
			return "sha256:img", nil
		case "inspect":
			return "", errString("Error: No such object: maclaw-desktop")
		case "run":
			return "container", nil
		case "exec":
			return `{"display":":20","cdp_port":19020}`, nil
		case "port":
			if strings.Contains(strings.Join(args, " "), "6080") {
				return "0.0.0.0:6081", nil
			}
			return "0.0.0.0:32768", nil
		default:
			return "", errString("unexpected " + args[0])
		}
	}}
	session, err := svc.OpenSession(context.Background(), Spec{
		TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:1",
		Memory: "4g", CPUs: "2", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.CDP != "http://docker.example:32768" || session.Display != ":20" {
		t.Fatalf("session=%#v", session)
	}
	if session.Novnc != "http://docker.example:6081/vnc.html?autoconnect=1&resize=scale" {
		t.Fatalf("novnc=%q", session.Novnc)
	}
	if strings.Contains(session.CDP, "172.") {
		t.Fatal("cdp url used a container address")
	}
	joined := strings.Join(runArgs, "\n")
	mounts, err := desktop.PrivateMounts("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--memory 4g", "--cpus 2", "--shm-size 1g", "--publish 19020/tcp", "--publish 6080/tcp", "HOME=/home/desktop", "MACLAW_DESKTOP_KEY=", "--entrypoint sh", "desktop_supervisor.py flush", "sleep infinity"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("docker run missing %s\n%s", want, joined)
		}
	}
	for _, mount := range mounts {
		if !strings.Contains(joined, mount.Volume+":"+mount.Target) {
			t.Fatalf("docker run missing private mount %s:%s\n%s", mount.Volume, mount.Target, joined)
		}
	}
	bob, err := desktop.PrivateMounts("tenant", "bob")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(joined, bob[0].Volume) {
		t.Fatal("alice desktop mounted bob's data")
	}
}

func TestSameDesktopStaysUpWhenShmCaseDiffers(t *testing.T) {
	mounts, err := desktop.PrivateMounts("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		parts = append(parts, mount.Volume+"="+mount.Target)
	}
	var commands []string
	svc := &Service{AdvertiseHost: "docker.example", Run: func(_ context.Context, args ...string) (string, error) {
		commands = append(commands, strings.Join(args, " "))
		switch args[0] {
		case "image":
			return "sha256:img", nil
		case "inspect":
			return "true|512M|" + strings.Join(parts, " "), nil
		case "update":
			return "ok", nil
		case "exec":
			return `{"display":":20","cdp_port":19020}`, nil
		case "port":
			return "0.0.0.0:32768", nil
		default:
			return "", errString("unexpected " + args[0])
		}
	}}
	if _, err := svc.OpenSession(context.Background(), Spec{
		TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:1",
		Memory: "2500m", CPUs: "1.5", ShmSize: "512m",
	}); err != nil {
		t.Fatal(err)
	}
	for _, line := range commands {
		if strings.HasPrefix(line, "rm ") || strings.HasPrefix(line, "commit ") {
			t.Fatalf("shm case rebuilt the desktop:\n%s", strings.Join(commands, "\n"))
		}
	}
}

func TestRecreateKeepsTheUsersSoftwareAndDropsNothingShared(t *testing.T) {
	var commands []string
	svc := &Service{AdvertiseHost: "docker.example", Run: func(_ context.Context, args ...string) (string, error) {
		commands = append(commands, strings.Join(args, " "))
		switch args[0] {
		case "image":
			if strings.Contains(strings.Join(args, " "), ":state") && !contains(commands[:len(commands)-1], "commit ") {
				return "", errString("Error: No such image")
			}
			if strings.Contains(strings.Join(args, " "), desktop.ImageLabel) {
				// State images committed before the label existed.
				return "", nil
			}
			return "sha256:img", nil
		case "inspect":
			return "true|512m", nil
		case "commit":
			state := args[len(args)-1]
			if len(args) != 5 || args[2] != "LABEL "+desktop.ImageLabel+`="maclaw-gui:1"` || !strings.HasPrefix(state, "maclaw-desktop-user-") || !strings.HasSuffix(state, ":state") {
				return "", errString("bad commit " + strings.Join(args, " "))
			}
			return "sha256:state", nil
		case "volume", "rm", "run", "cp", "stop":
			return "ok", nil
		case "exec":
			return `{"display":":20","cdp_port":19020}`, nil
		case "port":
			return "0.0.0.0:32768", nil
		default:
			return "", errString("unexpected " + args[0])
		}
	}}
	if _, err := svc.OpenSession(context.Background(), Spec{
		TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:1",
		Memory: "2500m", CPUs: "1.5", ShmSize: "1g",
	}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	state, err := desktop.StateImage("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joined, " maclaw-desktop-"+mustKey(t, "tenant", "alice")+" "+state) {
		t.Fatalf("user layer was not kept:\n%s", joined)
	}
	ran := false
	for _, line := range commands {
		if strings.HasPrefix(line, "run ") && strings.Contains(line, state+" -c") && strings.Contains(line, "sleep infinity") {
			ran = true
		}
	}
	if !ran {
		t.Fatal("recreated desktop did not use the user's image")
	}
	if strings.Contains(joined, "volume rm") || strings.Contains(joined, "maclaw-desktops\n") {
		t.Fatal("private data was deleted")
	}
	flushed := false
	stopped := false
	for _, line := range commands {
		if strings.Contains(line, " flush ") {
			flushed = true
		}
		if strings.HasPrefix(line, "stop ") && strings.Contains(line, "--time "+desktopStopGrace) {
			if !flushed {
				t.Fatal("recreate stopped the desktop before the browser was asked to write the login")
			}
			stopped = true
		}
		if strings.HasPrefix(line, "rm ") && !stopped {
			t.Fatal("recreate removed the desktop before the login was written")
		}
	}
	if !stopped {
		t.Fatal("recreate did not stop the desktop before removing it")
	}
}

func TestExistingDesktopWithoutPrivateDataIsRecreated(t *testing.T) {
	var commands []string
	svc := &Service{AdvertiseHost: "docker.example", Run: func(_ context.Context, args ...string) (string, error) {
		commands = append(commands, strings.Join(args, " "))
		switch args[0] {
		case "image":
			if strings.Contains(strings.Join(args, " "), ":state") && !contains(commands[:len(commands)-1], "commit ") {
				return "", errString("Error: No such image")
			}
			if strings.Contains(strings.Join(args, " "), desktop.ImageLabel) {
				// State images committed before the label existed.
				return "", nil
			}
			return "sha256:img", nil
		case "inspect":
			return "true|1g|", nil
		case "volume", "commit", "rm", "run", "cp", "stop":
			return "ok", nil
		case "exec":
			return `{"display":":20","cdp_port":19020}`, nil
		case "port":
			return "0.0.0.0:32768", nil
		case "update":
			return "", errString("updated a desktop that has no private volume")
		default:
			return "", errString("unexpected " + args[0])
		}
	}}
	if _, err := svc.OpenSession(context.Background(), Spec{
		TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:1",
		Memory: "2500m", CPUs: "1.5", ShmSize: "1g",
	}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	mounts, err := desktop.PrivateMounts("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	key := mustKey(t, "tenant", "alice")
	if !strings.Contains(joined, "commit ") || !strings.Contains(joined, "cp maclaw-desktop-"+key+":/desktops/"+key) || !strings.Contains(joined, mounts[0].Volume+":/desktops") {
		t.Fatalf("desktop was not moved onto the user's volumes:\n%s", joined)
	}
	if strings.Contains(joined, "cp maclaw-desktop-"+key+":/desktops/.") || strings.Contains(joined, "cp maclaw-desktop-"+key+":/desktops ") {
		t.Fatal("shared desktop tree was copied")
	}
	landed := false
	for _, line := range commands {
		if strings.Contains(line, "maclaw-migrate-"+key+":/mnt/desktops/"+key) && (strings.Contains(line, key+"/.") || strings.Contains(line, key+"\\.")) {
			landed = true
		}
	}
	if !landed {
		t.Fatalf("profile was not copied into /desktops/%s:\n%s", key, joined)
	}
}

func TestSharedVolumeIsReplacedWithTheUsersVolume(t *testing.T) {
	var commands []string
	svc := &Service{AdvertiseHost: "docker.example", Run: func(_ context.Context, args ...string) (string, error) {
		commands = append(commands, strings.Join(args, " "))
		switch args[0] {
		case "image":
			if strings.Contains(strings.Join(args, " "), ":state") && !contains(commands[:len(commands)-1], "commit ") {
				return "", errString("Error: No such image")
			}
			if strings.Contains(strings.Join(args, " "), desktop.ImageLabel) {
				// State images committed before the label existed.
				return "", nil
			}
			return "sha256:img", nil
		case "inspect":
			return "true|1g|maclaw-desktops-shared=/desktops maclaw-home-shared=/home/desktop maclaw-opt-shared=/opt maclaw-local-shared=/usr/local", nil
		case "volume", "commit", "rm", "run", "cp", "stop":
			return "ok", nil
		case "exec":
			return `{"display":":20","cdp_port":19020}`, nil
		case "port":
			return "0.0.0.0:32768", nil
		case "update":
			return "", errString("kept a shared volume")
		default:
			return "", errString("unexpected " + args[0])
		}
	}}
	if _, err := svc.OpenSession(context.Background(), Spec{
		TenantID: "tenant", UserID: "alice", Image: "maclaw-gui:1",
		Memory: "2500m", CPUs: "1.5", ShmSize: "1g",
	}); err != nil {
		t.Fatal(err)
	}
	mounts, err := desktop.PrivateMounts("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(commands, "\n")
	for _, line := range commands {
		if strings.HasPrefix(line, "run ") && strings.Contains(line, "maclaw-desktops-shared") {
			t.Fatal("recreated desktop still used the shared volume")
		}
	}
	if !strings.Contains(joined, mounts[0].Volume+":/desktops") {
		t.Fatalf("user volume was not mounted:\n%s", joined)
	}
}

func TestStopLeavesTheUsersDesktopData(t *testing.T) {
	var commands []string
	svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
		commands = append(commands, strings.Join(args, " "))
		if args[0] == "exec" {
			return "", errString("flush failed")
		}
		if args[0] == "stop" {
			return "stopped", nil
		}
		return "", errString("unexpected " + args[0])
	}}
	if _, err := svc.Stop(context.Background(), "tenant", "alice"); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.Contains(commands[0], " flush ") || !strings.Contains(commands[1], "--time "+desktopStopGrace) || !strings.Contains(commands[1], "maclaw-desktop-") || !strings.HasPrefix(commands[1], "stop ") {
		t.Fatalf("commands=%v", commands)
	}
	if strings.Contains(strings.Join(commands, "\n"), " rm") {
		t.Fatal("stop removed the user's desktop")
	}
}

func TestStopWritesTheLoginAfterTheCallerDisconnects(t *testing.T) {
	var commands []string
	svc := &Service{Run: func(ctx context.Context, args ...string) (string, error) {
		if ctx.Err() != nil {
			t.Errorf("login write was cancelled on %s", args[0])
		}
		commands = append(commands, strings.Join(args, " "))
		if args[0] == "exec" || args[0] == "stop" {
			return "ok", nil
		}
		return "", errString("unexpected " + args[0])
	}}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Stop(parent, "tenant", "alice"); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.Contains(commands[0], " flush ") || !strings.HasPrefix(commands[1], "stop ") {
		t.Fatalf("commands=%v", commands)
	}
}

func TestSessionWaitsUntilTheLoginStopFinishes(t *testing.T) {
	mounts, err := desktop.PrivateMounts("tenant", "alice")
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		parts = append(parts, mount.Volume+"="+mount.Target)
	}
	inspect := "true|" + desktop.DefaultShmSize + "|" + strings.Join(parts, " ") + "|" + desktop.DefaultImage + "|" + desktop.DefaultImage
	var mu sync.Mutex
	var order []string
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	svc := &Service{AdvertiseHost: "docker.example", Run: func(_ context.Context, args ...string) (string, error) {
		name := args[0]
		if name == "exec" && len(args) >= 2 && args[len(args)-2] == "flush" {
			name = "flush"
		}
		if name == "exec" && len(args) >= 2 && args[len(args)-2] == "ensure" {
			name = "ensure"
		}
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
		switch name {
		case "flush":
			once.Do(func() { close(entered) })
			<-release
			return "", nil
		case "stop", "update", "image":
			return "sha256:abc", nil
		case "inspect":
			return inspect, nil
		case "ensure":
			return `{"display":":20","cdp_port":19020}`, nil
		case "port":
			return "0.0.0.0:32768", nil
		default:
			return "", errString("unexpected " + name)
		}
	}}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	stopDone := make(chan error, 1)
	go func() {
		_, err := svc.Stop(context.Background(), "tenant", "alice")
		stopDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not start writing the login")
	}
	sessionDone := make(chan error, 1)
	go func() {
		_, err := svc.OpenSession(context.Background(), Spec{TenantID: "tenant", UserID: "alice"})
		sessionDone <- err
	}()
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	during := append([]string(nil), order...)
	mu.Unlock()
	if len(during) != 1 || during[0] != "flush" {
		t.Fatalf("the next session opened while the login was still being written: %v", during)
	}
	close(release)
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}
	if err := <-sessionDone; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	flushAt, ensureAt := -1, -1
	for i, name := range got {
		if name == "flush" && flushAt < 0 {
			flushAt = i
		}
		if name == "ensure" {
			ensureAt = i
		}
	}
	if flushAt < 0 || ensureAt < 0 || ensureAt < flushAt {
		t.Fatalf("session started before the login stop finished: %v", got)
	}
}

func TestBatchedAppCommandReachesXdotool(t *testing.T) {
	var got []string
	svc := &Service{Run: func(_ context.Context, args ...string) (string, error) {
		got = append([]string(nil), args...)
		return "ok", nil
	}}
	args := make([]string, 0, 96)
	for i := 0; i < 16; i++ {
		args = append(args, "mousemove", "--sync", "10", "20", "click", "1")
	}
	out, err := svc.App(context.Background(), "tenant", "alice", ":20", args)
	if err != nil || out != "ok" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if len(got) < 5 || got[0] != "exec" || got[len(got)-1] != "1" {
		t.Fatalf("docker args=%v", got)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "xdotool mousemove") || strings.Count(joined, "click") != 16 {
		t.Fatalf("batch was split: %s", joined)
	}
}

func contains(lines []string, part string) bool {
	for _, line := range lines {
		if strings.Contains(line, part) {
			return true
		}
	}
	return false
}

func mustKey(t *testing.T, tenantID, userID string) string {
	t.Helper()
	key, err := desktop.UserKey(tenantID, userID)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

type errString string

func (e errString) Error() string { return string(e) }
