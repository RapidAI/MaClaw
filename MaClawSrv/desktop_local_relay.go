package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"crypto/sha256"
	"encoding/hex"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/localrelay"
)

// The local_computer tool reaches the person's own computer, where their
// MaClaw GUI runs: files in folders they allowed, and hosts on that
// computer's network (an intranet GPU server, a lab web UI). Hub relays each
// request over the GUI's outbound link; the GUI enforces the folders and
// asks the person before anything new. Bytes never pass through this
// process: a small helper in the cloud desktop connects to Hub with a
// one-file or one-tunnel token and streams them directly.

const localRelayToolName = "local_computer"

//go:embed localrelay_helper/maclaw_relay.py
var localRelayHelperSource string

var localRelayHelperSum = func() string {
	sum := sha256.Sum256([]byte(localRelayHelperSource))
	return hex.EncodeToString(sum[:])[:12]
}()

// localRelayHome is the desktop home; tests point it at a temp folder.
var localRelayHome = desktopHomeDir

func localRelayDir() string   { return path.Join(localRelayHome, ".maclaw", "relay") }
func localRelayStateDir() string { return path.Join(localRelayDir(), "state") }
func localRelayHelper() string {
	return path.Join(localRelayDir(), "maclaw_relay_"+localRelayHelperSum+".py")
}

// localRelayPost is the Hub call; tests replace it.
var localRelayPost = localRelayPostHub

// localRelayBash runs one shell command in the cloud desktop; tests replace it.
var localRelayBash = func(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
	if !desktopHubExecutesTool() {
		return "", fmt.Errorf("the cloud desktop is not reachable through Hub on this server, so the local relay cannot run")
	}
	return runDesktopHubTool(ctx, scope, "bash", map[string]any{"command": command})
}

type localRelayReply struct {
	OK        bool                `json:"ok"`
	Code      string              `json:"code"`
	Error     string              `json:"error"`
	Online    bool                `json:"online"`
	Machine   string              `json:"machine"`
	BotName   string              `json:"bot_name"`
	Summary   *localrelay.Summary `json:"summary"`
	Path      string              `json:"path"`
	Entries   []localrelay.Entry  `json:"entries"`
	Truncated bool                `json:"truncated"`
	ID        string              `json:"id"`
	Token     string              `json:"token"`
	LocalPath string              `json:"local_path"`
	Folder    string              `json:"folder"`
	Name      string              `json:"name"`
	Size      int64               `json:"size"`
	MaxBytes  int64               `json:"max_bytes"`
	Addr      string              `json:"addr"`
	Kind      string              `json:"kind"`
	IdleMin   int                 `json:"idle_minutes"`
	Closed    bool                `json:"closed"`
	Grants    []localrelay.Grant  `json:"grants"`
}

func localRelayPostHub(ctx context.Context, body map[string]any) (localRelayReply, error) {
	client := desktopHubClientFromEnv()
	if client == nil {
		return localRelayReply{}, fmt.Errorf("this server is not connected to Hub, so the person's computer cannot be reached")
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/api/v1/desktop-services/relay", bytes.NewReader(raw))
	if err != nil {
		return localRelayReply{}, err
	}
	req.Header.Set("Authorization", "Bearer "+client.token)
	req.Header.Set("Content-Type", "application/json")
	// An approval waits for the person (up to 10 minutes on the GUI side).
	resp, err := (&http.Client{Timeout: 12 * time.Minute}).Do(req)
	if err != nil {
		return localRelayReply{}, fmt.Errorf("Hub is unreachable: %v", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return localRelayReply{}, fmt.Errorf("this server's Hub does not have the local relay yet (it needs the newer Hub build)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return localRelayReply{}, fmt.Errorf("%s", desktopRejectedMessage(payload))
	}
	var out localRelayReply
	if err := json.Unmarshal(payload, &out); err != nil {
		return localRelayReply{}, fmt.Errorf("Hub returned an unreadable relay answer")
	}
	return out, nil
}

// localRelayHubURL is Hub as the cloud desktop reaches it. MACLAW_RELAY_HUB_URL
// wins; otherwise MACLAW_HUB_URL, with a loopback host replaced by the Docker
// bridge gateway (MACLAW_RELAY_CONTAINER_HOST, default 172.17.0.1) because
// the desktop container's 127.0.0.1 is itself.
func localRelayHubURL() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("MACLAW_RELAY_HUB_URL")), "/"); v != "" {
		return v
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv(desktopHubURLEnv)), "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "localhost", "::1", "0.0.0.0":
		gw := strings.TrimSpace(os.Getenv("MACLAW_RELAY_CONTAINER_HOST"))
		if gw == "" {
			gw = "172.17.0.1"
		}
		if p := u.Port(); p != "" {
			u.Host = gw + ":" + p
		} else {
			u.Host = gw
		}
	}
	return strings.TrimRight(u.String(), "/")
}

type localRelayModule struct{}

func (localRelayModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{ModuleID: "maclawsrv.localrelay", Version: "1.0.0", HeadlessSupport: true}
}

func localRelayAvailable(request agentruntime.TurnRequest) bool {
	if os.Getenv("MACLAW_LOCAL_RELAY") == "off" {
		return false
	}
	return desktopTurnUsesContainer(request) && (desktopHubConfigured() || localRelayTestEnabled)
}

// localRelayTestEnabled offers the tool without Hub (tests).
var localRelayTestEnabled bool

var localRelayActions = []string{"status", "list", "upload", "download", "transfer_status", "tunnel_open", "socks_open", "tunnel_close", "tunnel_list"}

func (localRelayModule) Tools(_ context.Context, request agentruntime.TurnRequest) ([]agentruntime.ToolDefinition, error) {
	if !localRelayAvailable(request) {
		return nil, nil
	}
	return []agentruntime.ToolDefinition{{
		Name: localRelayToolName,
		Description: "Reach the person's OWN computer (where their MaClaw app runs) and the network it sits on, relayed through that app. " +
			"Use it when the person mentions a file on their computer, wants a result saved to their computer, or wants you to work on a machine in their office/home network (an intranet GPU server, a NAS, a lab web page) that this cloud desktop cannot reach directly. " +
			"action=status first: whether the app is connected, which folders are allowed, size limit. action=list path= lists a folder (empty path lists the allowed folders). " +
			"action=upload local_path= copies one file from their computer into this cloud desktop (remote_path= optional; default is this run's project folder). " +
			"action=download remote_path= copies one file from this cloud desktop to their computer (local_folder= optional, default their MaClaw/<bot> folder; an existing file is never overwritten, a new name is chosen). Folders: zip/tar them first. " +
			"action=tunnel_open host= port= makes 127.0.0.1:<port> in this desktop reach that host on their network, so ssh/scp/rsync/curl work normally (e.g. ssh -p <port> user@127.0.0.1). action=socks_open starts a SOCKS5 proxy for many hosts. action=tunnel_list, tunnel_close id=. " +
			"The person approves new folders and hosts on their computer, so a call may wait for them; a refusal or timeout is final for that request: tell them, do not retry silently. " +
			"Report exactly what the result says (saved path, size); if the app is offline, say so and continue what you can without it.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":       map[string]any{"type": "string", "enum": localRelayActions},
				"path":         map[string]any{"type": "string", "description": "list: a folder on the person's computer (full path or ~/...)."},
				"local_path":   map[string]any{"type": "string", "description": "upload: the file on the person's computer (full path or ~/...)."},
				"remote_path":  map[string]any{"type": "string", "description": "upload: destination in this cloud desktop (file or folder ending in /). download: the file in this cloud desktop to send."},
				"local_folder": map[string]any{"type": "string", "description": "download: destination folder on the person's computer; empty = their MaClaw/<bot> folder."},
				"name":         map[string]any{"type": "string", "description": "download: file name to save as (default: the remote file's name)."},
				"host":         map[string]any{"type": "string", "description": "tunnel_open: host name or IP on the person's network."},
				"port":         map[string]any{"type": "integer", "description": "tunnel_open: TCP port on that host (22 for ssh)."},
				"local_port":   map[string]any{"type": "integer", "description": "tunnel_open/socks_open: port to listen on in this desktop (default: a free one)."},
				"id":           map[string]any{"type": "string", "description": "transfer_status/tunnel_close: the id from an earlier result."},
			},
			"required": []string{"action"},
		},
	}}, nil
}

func (m localRelayModule) InvokeTool(ctx context.Context, request agentruntime.TurnRequest, name string, args map[string]any) (string, error) {
	if strings.TrimSpace(name) != localRelayToolName {
		return "", fmt.Errorf("unknown tool %s", name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	scope, _, err := desktopScopeForTurn(request)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(scope.RunID) == "" {
		scope.RunID = strings.TrimSpace(request.Scope.RunID)
	}
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	if botPhaseFromTurn(request) == "plan" {
		switch action {
		case "status", "list", "tunnel_list", "transfer_status":
		default:
			return "", fmt.Errorf("plan step: only status, list, tunnel_list and transfer_status run now; do %s in the execute step", action)
		}
	}
	started := time.Now()
	out, err := localRelayRun(ctx, scope, action, args)
	log.Printf("[local-relay] run=%s action=%s ms=%d err=%v", scope.RunID, action, time.Since(started).Milliseconds(), err)
	return out, err
}

func localRelayBase(scope agentruntime.Scope, op string) map[string]any {
	return map[string]any{
		"tenant_id":   strings.TrimSpace(scope.TenantID),
		"user_id":     strings.TrimSpace(scope.UserID),
		"instance_id": strings.TrimSpace(scope.InstanceID),
		"run_id":      strings.TrimSpace(scope.RunID),
		"op":          op,
	}
}

func localRelayCall(ctx context.Context, scope agentruntime.Scope, op string, extra map[string]any) (localRelayReply, error) {
	body := localRelayBase(scope, op)
	for k, v := range extra {
		body[k] = v
	}
	reply, err := localRelayPost(ctx, body)
	if err != nil {
		return reply, err
	}
	if !reply.OK {
		msg := strings.TrimSpace(reply.Error)
		if msg == "" {
			msg = "the person's computer refused the request"
		}
		return reply, &localrelay.Err{Code: reply.Code, Message: msg}
	}
	return reply, nil
}

func localRelayIntArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	return 0
}

func localRelayRun(ctx context.Context, scope agentruntime.Scope, action string, args map[string]any) (string, error) {
	switch action {
	case "status":
		return localRelayStatus(ctx, scope)
	case "list":
		return localRelayList(ctx, scope, desktopArg(args, "path"))
	case "upload":
		return localRelayUpload(ctx, scope, desktopArg(args, "local_path"), desktopArg(args, "remote_path"))
	case "download":
		return localRelayDownload(ctx, scope, desktopArg(args, "remote_path"), desktopArg(args, "local_folder"), desktopArg(args, "name"))
	case "transfer_status":
		return localRelayTransferStatus(ctx, scope, desktopArg(args, "id"))
	case "tunnel_open":
		return localRelayTunnel(ctx, scope, localrelay.KindForward, desktopArg(args, "host"), localRelayIntArg(args, "port"), localRelayIntArg(args, "local_port"))
	case "socks_open":
		return localRelayTunnel(ctx, scope, localrelay.KindSocks, "", 0, localRelayIntArg(args, "local_port"))
	case "tunnel_close":
		return localRelayClose(ctx, scope, desktopArg(args, "id"))
	case "tunnel_list":
		return localRelayTunnels(ctx, scope)
	}
	return "", fmt.Errorf("action must be one of %s", strings.Join(localRelayActions, ", "))
}

func localRelayStatus(ctx context.Context, scope agentruntime.Scope) (string, error) {
	r, err := localRelayCall(ctx, scope, "status", nil)
	if err != nil {
		return "", err
	}
	if !r.Online {
		return "", localrelay.ErrOffline
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The person's MaClaw app is connected")
	if s := r.Summary; s != nil {
		if s.OS != "" {
			fmt.Fprintf(&b, " (%s)", s.OS)
		}
		b.WriteString(".\n")
		if s.FilesEnabled {
			fmt.Fprintf(&b, "Files: on, up to %d MB per file. Your own folder there: %s\nAllowed folders (anything else asks the person):\n", s.MaxTransferMB, s.BotFolder)
			for _, root := range s.Roots {
				mode := "read"
				if root.Write {
					mode = "read+write"
				}
				fmt.Fprintf(&b, "- %s (%s)\n", root.Name, mode)
			}
		} else {
			b.WriteString("Files: turned off by the person.\n")
		}
		if s.NetworkEnabled {
			fmt.Fprintf(&b, "Network relay: on (new hosts ask the person first; idle tunnels close after %d min).\n", s.TunnelIdleMinutes)
		} else {
			b.WriteString("Network relay: turned off by the person.\n")
		}
	} else {
		b.WriteString(".\n")
	}
	if len(r.Grants) > 0 {
		b.WriteString(localRelayGrantLines(r.Grants))
	}
	return strings.TrimSpace(b.String()), nil
}

func localRelayList(ctx context.Context, scope agentruntime.Scope, p string) (string, error) {
	r, err := localRelayCall(ctx, scope, "list", map[string]any{"path": p})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if strings.TrimSpace(p) == "" {
		b.WriteString("Allowed folders on the person's computer:\n")
		for _, e := range r.Entries {
			mode := "read"
			if e.Write {
				mode = "read+write"
			}
			fmt.Fprintf(&b, "%s (%s)\n", e.Name, mode)
		}
		return strings.TrimSpace(b.String()), nil
	}
	fmt.Fprintf(&b, "%s (%d entries%s):\n", r.Path, len(r.Entries), map[bool]string{true: ", first 500 shown", false: ""}[r.Truncated])
	for _, e := range r.Entries {
		if e.Dir {
			fmt.Fprintf(&b, "[dir] %s\n", e.Name)
			continue
		}
		fmt.Fprintf(&b, "%s  %s  %s\n", e.Name, localrelay.HumanSize(e.Size), e.ModTime)
	}
	return strings.TrimSpace(b.String()), nil
}

// localRelayRemotePath resolves a cloud-desktop path; a relative one is in
// the run's project folder.
func localRelayRemotePath(scope agentruntime.Scope, p string) (string, error) {
	p = strings.TrimSpace(p)
	dir := strings.HasSuffix(p, "/")
	if p != "" && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~") {
		if wd := desktopRunWorkDir(scope); wd != "" {
			p = path.Join(wd, p)
		}
	}
	if p == "" {
		return "", nil
	}
	resolved, err := desktop.ContainerPath(p)
	if err != nil {
		return "", err
	}
	if dir {
		resolved += "/"
	}
	return resolved, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// localRelayScript installs the helper when it is missing, stores the token
// in a private file, and runs the helper. The token never appears in a
// result, a log line, or the process list.
func localRelayScript(id, token, run string) string {
	helper := localRelayHelper()
	var b strings.Builder
	fmt.Fprintf(&b, "set -e; umask 077; mkdir -p %s %s\n", shellQuote(localRelayDir()), shellQuote(localRelayStateDir()))
	fmt.Fprintf(&b, "if [ ! -f %s ]; then cat > %s.tmp <<'MACLAW_RELAY_EOF'\n%s\nMACLAW_RELAY_EOF\nmv %s.tmp %s; fi\n", shellQuote(helper), shellQuote(helper), strings.TrimRight(localRelayHelperSource, "\n"), shellQuote(helper), shellQuote(helper))
	if token != "" {
		fmt.Fprintf(&b, "printf '%%s' %s > %s\n", shellQuote(token), shellQuote(localRelayTokenFile(id)))
	}
	b.WriteString(run)
	return b.String()
}

func localRelayTokenFile(id string) string { return path.Join(localRelayStateDir(), id+".token") }
func localRelayStateFile(id string) string { return path.Join(localRelayStateDir(), id+".json") }

var localRelayResultRe = regexp.MustCompile(`(?m)^MACLAW_RELAY_RESULT (\{.*\})\s*$`)

type localRelayState struct {
	State string `json:"state"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
	Path  string `json:"path"`
	Local string `json:"local"`
	Port  int    `json:"port"`
	PID   int    `json:"pid"`
	Code  string `json:"code"`
	Error string `json:"error"`
	Conns int    `json:"conns"`
}

func parseLocalRelayResult(out string) (localRelayState, bool) {
	m := localRelayResultRe.FindAllStringSubmatch(out, -1)
	if len(m) == 0 {
		return localRelayState{}, false
	}
	var st localRelayState
	if json.Unmarshal([]byte(m[len(m)-1][1]), &st) != nil {
		return localRelayState{}, false
	}
	return st, true
}

var localRelayIDRe = regexp.MustCompile(`^lr_[0-9a-f]{6,32}$`)

func localRelayHelperRun(ctx context.Context, scope agentruntime.Scope, id, token, args string, wait int) (localRelayState, error) {
	if !localRelayIDRe.MatchString(id) {
		return localRelayState{}, fmt.Errorf("Hub returned an invalid relay id")
	}
	run := fmt.Sprintf("python3 %s %s --url %s --token-file %s --state %s --wait %d --detach\n",
		shellQuote(localRelayHelper()), args, shellQuote(localRelayHubURL()), shellQuote(localRelayTokenFile(id)), shellQuote(localRelayStateFile(id)), wait)
	out, err := localRelayBash(ctx, scope, localRelayScript(id, token, run))
	st, ok := parseLocalRelayResult(out)
	if !ok {
		if err != nil {
			return st, fmt.Errorf("the relay helper did not start in the cloud desktop: %v", err)
		}
		return st, fmt.Errorf("the relay helper did not report a result in the cloud desktop: %s", localRelayTail(out))
	}
	return st, nil
}

func localRelayTail(out string) string {
	out = strings.TrimSpace(out)
	if len(out) > 400 {
		out = "…" + out[len(out)-400:]
	}
	return out
}

func localRelayUpload(ctx context.Context, scope agentruntime.Scope, local, remote string) (string, error) {
	if strings.TrimSpace(local) == "" {
		return "", fmt.Errorf("local_path is required: the file on the person's computer")
	}
	dest, err := localRelayRemotePath(scope, remote)
	if err != nil {
		return "", err
	}
	r, err := localRelayCall(ctx, scope, "grant_to_cloud", map[string]any{"path": local, "remote": dest})
	if err != nil {
		return "", err
	}
	name := localrelay.SafeName(r.Name)
	if name == "" {
		name = "file"
	}
	switch {
	case dest == "":
		wd := desktopRunWorkDir(scope)
		if wd == "" {
			wd = path.Join(localRelayHome, "local-files")
		}
		dest = path.Join(wd, name)
	case strings.HasSuffix(dest, "/"):
		dest = path.Join(dest, name)
	}
	st, err := localRelayHelperRun(ctx, scope, r.ID, r.Token, "get --out "+shellQuote(dest), 75)
	if err != nil {
		return "", err
	}
	return localRelayTransferText(r.ID, "upload", r.LocalPath, st)
}

func localRelayDownload(ctx context.Context, scope agentruntime.Scope, remote, folder, name string) (string, error) {
	src, err := localRelayRemotePath(scope, remote)
	if err != nil || src == "" || strings.HasSuffix(src, "/") {
		return "", fmt.Errorf("remote_path must name one file in this cloud desktop (zip a folder first)")
	}
	if strings.TrimSpace(name) == "" {
		name = path.Base(src)
	}
	r, err := localRelayCall(ctx, scope, "grant_to_local", map[string]any{"path": folder, "name": name, "remote": src})
	if err != nil {
		return "", err
	}
	st, err := localRelayHelperRun(ctx, scope, r.ID, r.Token, fmt.Sprintf("put --file %s --max %d", shellQuote(src), r.MaxBytes), 75)
	if err != nil {
		return "", err
	}
	return localRelayTransferText(r.ID, "download", src, st)
}

func localRelayTransferText(id, kind, from string, st localRelayState) (string, error) {
	switch st.State {
	case "done":
		if kind == "upload" {
			return fmt.Sprintf("Copied %s from the person's computer to %s in this cloud desktop (%s).", firstDesktopText(st.Local, from), st.Path, localrelay.HumanSize(st.Total)), nil
		}
		return fmt.Sprintf("Saved %s to the person's computer as %s (%s).", from, st.Path, localrelay.HumanSize(st.Total)), nil
	case "running", "starting":
		return fmt.Sprintf("Still transferring (%s of %s so far). It continues in the background: call action=transfer_status id=%s to check; do not report it as finished yet.", localrelay.HumanSize(st.Done), localrelay.HumanSize(st.Total), id), nil
	}
	msg := strings.TrimSpace(st.Error)
	if msg == "" {
		msg = "the transfer failed"
	}
	return "", &localrelay.Err{Code: firstDesktopText(st.Code, localrelay.CodeFailed), Message: "transfer failed: " + msg}
}

func localRelayReadState(ctx context.Context, scope agentruntime.Scope, id string) (localRelayState, error) {
	if !localRelayIDRe.MatchString(id) {
		return localRelayState{}, fmt.Errorf("id must be the lr_… id from an earlier result")
	}
	out, err := localRelayBash(ctx, scope, fmt.Sprintf("printf 'MACLAW_RELAY_RESULT '; cat %s 2>/dev/null || echo '{\"state\":\"unknown\"}'; echo", shellQuote(localRelayStateFile(id))))
	st, ok := parseLocalRelayResult(out)
	if !ok {
		if err == nil {
			err = errors.New(localRelayTail(out))
		}
		return st, fmt.Errorf("could not read the transfer state: %v", err)
	}
	return st, nil
}

func localRelayTransferStatus(ctx context.Context, scope agentruntime.Scope, id string) (string, error) {
	st, err := localRelayReadState(ctx, scope, id)
	if err != nil {
		return "", err
	}
	if st.State == "unknown" {
		return "", fmt.Errorf("no transfer or tunnel %s in this cloud desktop", id)
	}
	if st.State == "listening" {
		return fmt.Sprintf("Tunnel %s is listening on 127.0.0.1:%d (%d connections so far).%s", id, st.Port, st.Conns, localRelayErrNote(st)), nil
	}
	if st.State == "closed" {
		return fmt.Sprintf("Tunnel %s is closed: %s", id, st.Error), nil
	}
	return localRelayTransferText(id, map[bool]string{true: "upload", false: "download"}[st.Local != ""], "", st)
}

func localRelayErrNote(st localRelayState) string {
	if strings.TrimSpace(st.Error) == "" {
		return ""
	}
	return " Last connection error: " + st.Error
}

func localRelayTunnel(ctx context.Context, scope agentruntime.Scope, kind, host string, port, localPort int) (string, error) {
	if localPort < 0 || localPort > 65535 {
		return "", fmt.Errorf("local_port is out of range")
	}
	op := "grant_socks"
	extra := map[string]any{}
	if kind == localrelay.KindForward {
		if strings.TrimSpace(host) == "" || port <= 0 || port > 65535 {
			return "", fmt.Errorf("host and port are required (for ssh, port=22)")
		}
		op = "grant_forward"
		extra["host"], extra["port"] = strings.TrimSpace(host), port
	}
	r, err := localRelayCall(ctx, scope, op, extra)
	if err != nil {
		return "", err
	}
	mode := "socks"
	if kind == localrelay.KindForward {
		mode = "forward"
	}
	st, err := localRelayHelperRun(ctx, scope, r.ID, r.Token, fmt.Sprintf("%s --port %d", mode, localPort), 15)
	if err != nil {
		_, _ = localRelayCall(ctx, scope, "close", map[string]any{"id": r.ID})
		return "", err
	}
	if st.State != "listening" || st.Port == 0 {
		_, _ = localRelayCall(ctx, scope, "close", map[string]any{"id": r.ID})
		msg := firstDesktopText(st.Error, "it did not start listening")
		return "", fmt.Errorf("the tunnel could not start in the cloud desktop: %s", msg)
	}
	idle := ""
	if r.IdleMin > 0 {
		idle = fmt.Sprintf(" It closes after %d idle minutes.", r.IdleMin)
	}
	if kind == localrelay.KindForward {
		target := fmt.Sprintf("%s:%d", strings.TrimSpace(host), port)
		return fmt.Sprintf("Tunnel %s open: 127.0.0.1:%d in this cloud desktop reaches %s on the person's network (through their MaClaw app, resolved there as %s).%s\n"+
			"ssh -p %d -o HostKeyAlias=%s USER@127.0.0.1 ; scp -P %d FILE USER@127.0.0.1:PATH ; rsync -e 'ssh -p %d' ... ; curl --noproxy '*' http://127.0.0.1:%d/\n"+
			"Use action=bash for these (long jobs with background=true). Close it with action=tunnel_close id=%s when done.",
			r.ID, st.Port, target, r.Addr, idle, st.Port, strings.TrimSpace(host), st.Port, st.Port, st.Port, r.ID), nil
	}
	return fmt.Sprintf("SOCKS5 proxy %s open on 127.0.0.1:%d in this cloud desktop; connections go out from the person's computer (each new host asks them once).%s\n"+
		"curl --socks5-hostname 127.0.0.1:%d http://HOST:PORT/ ; ssh -o ProxyCommand=\"python3 %s stdio %%h %%p --url %s --token-file %s\" USER@HOST\n"+
		"Close it with action=tunnel_close id=%s when done.",
		r.ID, st.Port, idle, st.Port, localRelayHelper(), localRelayHubURL(), localRelayTokenFile(r.ID), r.ID), nil
}

func localRelayClose(ctx context.Context, scope agentruntime.Scope, id string) (string, error) {
	if !localRelayIDRe.MatchString(id) {
		return "", fmt.Errorf("id must be the lr_… id from tunnel_open or tunnel_list")
	}
	r, err := localRelayCall(ctx, scope, "close", map[string]any{"id": id})
	if err != nil {
		return "", err
	}
	stateFile := localRelayStateFile(id)
	_, berr := localRelayBash(ctx, scope, fmt.Sprintf("p=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get(\"pid\",0))' %s 2>/dev/null || echo 0); [ \"$p\" -gt 1 ] 2>/dev/null && kill \"$p\" 2>/dev/null; rm -f %s %s; echo closed",
		shellQuote(stateFile), shellQuote(localRelayTokenFile(id)), shellQuote(stateFile)))
	text := fmt.Sprintf("Tunnel %s closed", id)
	if !r.Closed {
		text = fmt.Sprintf("Tunnel %s was already closed on Hub", id)
	}
	if berr != nil {
		return text + fmt.Sprintf("; stopping its listener in the cloud desktop failed: %v", berr), nil
	}
	return text + "; its listener in the cloud desktop is stopped.", nil
}

func localRelayTunnels(ctx context.Context, scope agentruntime.Scope) (string, error) {
	r, err := localRelayCall(ctx, scope, "grants", nil)
	if err != nil {
		return "", err
	}
	if len(r.Grants) == 0 {
		return "No open tunnels or pending transfers.", nil
	}
	return strings.TrimSpace(localRelayGrantLines(r.Grants)), nil
}

func localRelayGrantLines(grants []localrelay.Grant) string {
	var b strings.Builder
	b.WriteString("Open relays:\n")
	for _, g := range grants {
		switch g.Kind {
		case localrelay.KindForward:
			fmt.Fprintf(&b, "- %s tunnel to %s:%d, %d connections, %s out / %s in\n", g.ID, g.Host, g.Port, g.Uses, localrelay.HumanSize(g.BytesUp), localrelay.HumanSize(g.BytesDown))
		case localrelay.KindSocks:
			fmt.Fprintf(&b, "- %s SOCKS proxy, %d connections, last %s\n", g.ID, g.Uses, firstDesktopText(g.LastTarget, "none"))
		default:
			fmt.Fprintf(&b, "- %s pending %s of %s\n", g.ID, g.Kind, firstDesktopText(g.Name, g.LocalPath))
		}
	}
	return b.String()
}
