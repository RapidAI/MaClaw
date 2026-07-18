package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	vscodeACPExtensionID = "formulahendry.acp-client"
	maclawACPExtID       = "maclaw.maclaw-acp"
	vscodeACPAgentName   = "MaClaw GUI"
	bridgeBinaryBase     = "maclaw-acp-bridge"
	vscodeDownloadURL    = "https://code.visualstudio.com/Download"
)

// VSCodeACPLaunchResult is returned to the UI after configure + launch.
type VSCodeACPLaunchResult struct {
	OK           bool     `json:"ok"`
	Message      string   `json:"message"`
	Steps        []string `json:"steps"`
	Warnings     []string `json:"warnings,omitempty"`
	VSCodePath   string   `json:"vscodePath,omitempty"`
	BridgePath   string   `json:"bridgePath,omitempty"`
	SettingsPath string   `json:"settingsPath,omitempty"`
	ExtensionID  string   `json:"extensionId,omitempty"`
	GatewayReady bool     `json:"gatewayReady"`
	AgentName    string   `json:"agentName,omitempty"`
	// NeedVSCodeInstall is set when no VS Code CLI/install could be found;
	// the UI should prompt the user and open VSCodeDownloadURL on confirm.
	NeedVSCodeInstall bool   `json:"needVSCodeInstall,omitempty"`
	VSCodeDownloadURL string `json:"vscodeDownloadURL,omitempty"`
}

// LaunchVSCodeWithACP ensures the third-party gateway is on, resolves the ACP
// bridge binary, installs/configures the third-party VS Code ACP client
// extension, writes acp.agents for MaClaw GUI, and launches VS Code.
func (a *App) LaunchVSCodeWithACP() VSCodeACPLaunchResult {
	return a.launchVSCodeWithACPMode(true, false)
}

// PrepareVSCodeACP configures gateway + bridge + VS Code settings without launching.
func (a *App) PrepareVSCodeACP() VSCodeACPLaunchResult {
	return a.launchVSCodeWithACPMode(false, false)
}

// LaunchVSCodeWithACPExtension installs the bundled first-party VS Code
// extension (chat in the bottom panel) and launches VS Code.
func (a *App) LaunchVSCodeWithACPExtension() VSCodeACPLaunchResult {
	return a.launchVSCodeWithACPMode(true, true)
}

// PrepareVSCodeACPExtension installs the first-party extension without launching.
func (a *App) PrepareVSCodeACPExtension() VSCodeACPLaunchResult {
	return a.launchVSCodeWithACPMode(false, true)
}

// GetVSCodeACPStatus returns a lightweight readiness snapshot for the utilities card.
func (a *App) GetVSCodeACPStatus() map[string]any {
	vsPath, _ := findVSCodeCLI()
	bridge, _ := resolveACPBridgeBinary()
	settings := vscodeUserSettingsPath()
	ep := discoverGatewayFromAppConfig(a)
	modeB := a.GetACPHostStatus()
	return map[string]any{
		"vscodeFound":           strings.TrimSpace(vsPath) != "",
		"vscodePath":            vsPath,
		"bridgeFound":           strings.TrimSpace(bridge) != "",
		"bridgePath":            bridge,
		"settingsPath":          settings,
		"gatewayEnabled":        ep.Enabled,
		"tokenPresent":          ep.Token != "",
		"extensionId":           vscodeACPExtensionID,
		"firstPartyExtensionId": maclawACPExtID,
		"firstPartyVersion":     maclawACPVsixVersion(),
		"agentName":             vscodeACPAgentName,
		"modeB":                 modeB,
	}
}

func (a *App) launchVSCodeWithACPMode(doLaunch bool, firstParty bool) VSCodeACPLaunchResult {
	extensionID := vscodeACPExtensionID
	if firstParty {
		extensionID = maclawACPExtID
	}
	res := VSCodeACPLaunchResult{
		ExtensionID: extensionID,
		AgentName:   vscodeACPAgentName,
		Steps:       []string{},
		Warnings:    []string{},
	}

	// 0) VS Code must be installed before any gateway/bridge work — tell the
	//    user immediately (UI prompts and offers the download page).
	codeCLI, err := findVSCodeCLI()
	if err != nil {
		res.Message = "Visual Studio Code not found. Install VS Code, then retry."
		res.NeedVSCodeInstall = true
		res.VSCodeDownloadURL = vscodeDownloadURL
		return res
	}
	res.VSCodePath = codeCLI
	res.Steps = append(res.Steps, "detected VS Code CLI: "+codeCLI)

	// 1) Mode B — GUI AI assistant programming agent (preferred)
	a.ensureACPHost()
	// Brief settle for endpoint.json write + listener accept readiness.
	modeBReady := false
	for attempt := 0; attempt < 3; attempt++ {
		if host, port, _, ok := ReadACPHostEndpoint(); ok {
			// Probe process-local host when available.
			st := a.GetACPHostStatus()
			running, _ := st["running"].(bool)
			if running || ok {
				res.Steps = append(res.Steps, fmt.Sprintf("Mode B ACP host ready tcp://%s:%d (AI assistant programming agent)", host, port))
				modeBReady = true
				break
			}
		}
		if attempt < 2 {
			time.Sleep(150 * time.Millisecond)
			a.ensureACPHost()
		}
	}
	if !modeBReady {
		cfg, _ := a.LoadConfig()
		if cfg.IsAcpHostEnabled() {
			res.Warnings = append(res.Warnings,
				"Mode B ACP host failed to start (check acp_host_port conflict or logs); falling back to IM Gateway if available")
		} else {
			res.Warnings = append(res.Warnings, "Mode B disabled in settings; using IM Gateway path if available")
		}
	}

	// 2) Gateway (fallback attach path)
	if err := a.ensureGatewayForACPBridge(&res); err != nil {
		// If Mode B is up, gateway failure is non-fatal.
		if modeBReady {
			res.Warnings = append(res.Warnings, "gateway: "+err.Error())
		} else {
			res.Message = err.Error() + " (and Mode B host is not ready)"
			return res
		}
	} else {
		res.GatewayReady = true
	}

	// 3) Bridge binary
	bridgePath, err := ensureACPBridgeBinary(&res)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.BridgePath = bridgePath

	// 4) Install ACP extension.
	if firstParty {
		// The extension install is the ONLY VS Code-side step in this flow —
		// its failure must not report success.
		if err := installMaclawACPExtension(codeCLI); err != nil {
			res.Message = "install MaClaw VS Code extension: " + err.Error()
			return res
		}
		res.Steps = append(res.Steps, "ensured extension "+maclawACPExtID+"@"+maclawACPVsixVersion()+" (bottom panel)")
	} else {
		// Third-party client install stays best-effort (user can install it
		// from the Marketplace manually; settings are still written below).
		if err := installVSCodeACPExtension(codeCLI); err != nil {
			res.Warnings = append(res.Warnings, "extension install: "+err.Error()+" — install manually: "+vscodeACPExtensionID)
		} else {
			res.Steps = append(res.Steps, "ensured extension "+vscodeACPExtensionID)
		}
	}

	// 5) Write user settings acp.agents (third-party client only; the
	//    first-party extension resolves the bridge on its own).
	//    Bridge prefers Mode B discovery files; gateway env is fallback only.
	if !firstParty {
		ep := discoverGatewayFromAppConfig(a)
		settingsPath, err := writeVSCodeACPAgentSettings(bridgePath, ep)
		if err != nil {
			res.Message = "configure VS Code settings: " + err.Error()
			return res
		}
		res.SettingsPath = settingsPath
		if modeBReady {
			res.Steps = append(res.Steps, "configured acp.agents[\""+vscodeACPAgentName+"\"] → bridge (Mode B preferred)")
		} else {
			res.Steps = append(res.Steps, "configured acp.agents[\""+vscodeACPAgentName+"\"] → bridge (Gateway fallback)")
		}
	}

	// 6) Launch
	if doLaunch {
		folder := ""
		if cfg, err := a.LoadConfig(); err == nil {
			folder = strings.TrimSpace(cfg.WorkingDirectory)
			if folder == "" {
				folder = corelib.EffectiveWorkspaceDir()
			}
		}
		if err := launchVSCode(codeCLI, folder); err != nil {
			res.Message = "launch VS Code: " + err.Error()
			return res
		}
		res.Steps = append(res.Steps, "launched VS Code")
		if folder != "" {
			res.Steps = append(res.Steps, "opened folder: "+folder)
		}
	}

	res.OK = true
	if firstParty {
		if doLaunch {
			res.Message = "VS Code launched with MaClaw extension. Chat opens in the bottom panel 「MaClaw」."
		} else {
			res.Message = "MaClaw extension installed. Launch VS Code and open the bottom panel 「MaClaw」 chat."
		}
	} else if doLaunch {
		res.Message = "VS Code launched with MaClaw ACP bridge. Open ACP panel and select agent \"" + vscodeACPAgentName + "\"."
	} else {
		res.Message = "ACP configured. Launch VS Code and select agent \"" + vscodeACPAgentName + "\"."
	}
	return res
}

type gatewaySnap struct {
	Enabled bool
	Token   string
	Host    string
	Port    int
	BaseURL string
}

func discoverGatewayFromAppConfig(a *App) gatewaySnap {
	cfg, err := a.LoadConfig()
	if err != nil {
		return gatewaySnap{Host: "127.0.0.1", Port: 18777}
	}
	host := strings.TrimSpace(cfg.ThirdPartyGatewayHost)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	port := cfg.ThirdPartyGatewayPort
	if port <= 0 {
		port = 18777
	}
	return gatewaySnap{
		Enabled: cfg.ThirdPartyGatewayEnabled,
		Token:   strings.TrimSpace(cfg.ThirdPartyGatewayToken),
		Host:    host,
		Port:    port,
		BaseURL: fmt.Sprintf("http://%s:%d/api/im-gateway/v1", host, port),
	}
}

func (a *App) ensureGatewayForACPBridge(res *VSCodeACPLaunchResult) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	changed := false
	token := strings.TrimSpace(cfg.ThirdPartyGatewayToken)
	if token == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return fmt.Errorf("generate gateway token: %w", err)
		}
		token = hex.EncodeToString(raw)
		changed = true
	}
	if !cfg.ThirdPartyGatewayEnabled {
		changed = true
	}
	host := strings.TrimSpace(cfg.ThirdPartyGatewayHost)
	if host == "" {
		host = "127.0.0.1"
		changed = true
	}
	port := cfg.ThirdPartyGatewayPort
	if port <= 0 {
		port = 18777
		changed = true
	}
	if changed {
		if err := a.PatchConfig(func(c *corelib.AppConfig) {
			c.ThirdPartyGatewayEnabled = true
			c.ThirdPartyGatewayToken = token
			c.ThirdPartyGatewayHost = host
			c.ThirdPartyGatewayPort = port
			if c.ThirdPartyGatewayLocalMode == nil {
				c.SetThirdPartyGatewayLocal(true)
			}
		}); err != nil {
			return fmt.Errorf("enable third-party gateway: %w", err)
		}
		res.Steps = append(res.Steps, "enabled third-party gateway and ensured token")
	} else {
		res.Steps = append(res.Steps, "third-party gateway already enabled")
	}
	a.ensureThirdPartyGateway()
	// Wait until HTTP health responds (production gate for VS Code spawn).
	ep := discoverGatewayFromAppConfig(a)
	healthURL := strings.TrimRight(ep.BaseURL, "/") + "/health"
	deadline := time.Now().Add(8 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		st := a.GetThirdPartyGatewayStatus()
		if strings.EqualFold(st, gatewayConnectionStatusConnected.String()) {
			if err := probeGatewayHealth(healthURL, token); err == nil {
				res.Steps = append(res.Steps, "gateway health ok: "+healthURL)
				return nil
			} else {
				lastErr = err
			}
		}
		time.Sleep(150 * time.Millisecond)
		a.ensureThirdPartyGateway()
	}
	status := a.GetThirdPartyGatewayStatus()
	if lastErr != nil {
		return fmt.Errorf("gateway not healthy (status=%s): %w", status, lastErr)
	}
	return fmt.Errorf("gateway not ready (status=%s); check port %d is free and restart MaClaw", status, port)
}

func probeGatewayHealth(url, token string) error {
	ctxClient := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}
	resp, err := ctxClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) == nil {
		if ok, _ := payload["ok"].(bool); ok {
			return nil
		}
		// Some health responses use status field only.
		if st, _ := payload["status"].(string); st != "" {
			return nil
		}
	}
	return nil
}

func bridgeBinaryName() string {
	if runtime.GOOS == "windows" {
		return bridgeBinaryBase + ".exe"
	}
	return bridgeBinaryBase
}

func resolveACPBridgeBinary() (string, error) {
	name := bridgeBinaryName()
	candidates := []string{}

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), name))
	}
	candidates = append(candidates,
		filepath.Join(corelib.MaclawBaseDir(), "bin", name),
		filepath.Join(corelib.MaclawBaseDir(), name),
	)
	// Dev repo layouts relative to cwd
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "dist", name),
			filepath.Join(wd, name),
		)
	}
	if p, err := exec.LookPath(bridgeBinaryBase); err == nil {
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath(name); err == nil {
		candidates = append(candidates, p)
	}

	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("maclaw-acp-bridge not found (looked next to MaClaw, in %%MACLAW data/bin, dist/, PATH)")
}

func ensureACPBridgeBinary(res *VSCodeACPLaunchResult) (string, error) {
	// Preferred install locations for production: next to MaClaw.exe, then data/bin.
	stableTargets := []string{}
	if exe, err := os.Executable(); err == nil {
		stableTargets = append(stableTargets, filepath.Join(filepath.Dir(exe), bridgeBinaryName()))
	}
	binDir := filepath.Join(corelib.MaclawBaseDir(), "bin")
	stableTargets = append(stableTargets, filepath.Join(binDir, bridgeBinaryName()))

	if p, err := resolveACPBridgeBinary(); err == nil {
		// Copy into install dir / data bin so VS Code settings stay stable across restarts.
		installed := installBridgeCopy(p, stableTargets)
		if installed != "" {
			res.Steps = append(res.Steps, "bridge ready: "+installed)
			return installed, nil
		}
		res.Steps = append(res.Steps, "found bridge: "+p)
		return p, nil
	}

	// Try to build into MaclawBaseDir/bin when Go toolchain is available.
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin dir: %w", err)
	}
	outPath := filepath.Join(binDir, bridgeBinaryName())

	moduleRoot := findModuleRoot()
	if moduleRoot == "" {
		return "", fmt.Errorf("maclaw-acp-bridge binary missing; reinstall MaClaw or place %s next to MaClaw.exe / in %s", bridgeBinaryName(), binDir)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("maclaw-acp-bridge missing and `go` not on PATH; build with: go build -o %s ./cmd/maclaw-acp-bridge", outPath)
	}
	cmd := exec.Command(goBin, "build", "-o", outPath, "./cmd/maclaw-acp-bridge")
	cmd.Dir = moduleRoot
	cmd.Env = os.Environ()
	hideACPLaunchWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build maclaw-acp-bridge: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	// Also copy next to MaClaw.exe when possible.
	_ = installBridgeCopy(outPath, stableTargets)
	res.Steps = append(res.Steps, "built bridge: "+outPath)
	return outPath, nil
}

// installBridgeCopy copies src into the first writable target path (if different).
func installBridgeCopy(src string, targets []string) string {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		srcAbs = src
	}
	in, err := os.Open(srcAbs)
	if err != nil {
		return ""
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || info.IsDir() {
		return ""
	}
	for _, dst := range targets {
		if strings.TrimSpace(dst) == "" {
			continue
		}
		dstAbs, err := filepath.Abs(dst)
		if err != nil {
			dstAbs = dst
		}
		if strings.EqualFold(srcAbs, dstAbs) {
			return dstAbs
		}
		if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
			continue
		}
		// Skip copy only when destination is same size and not older than source.
		// Size-only equality could leave a stale bridge after a rebuild.
		if st, err := os.Stat(dstAbs); err == nil && !st.IsDir() &&
			st.Size() == info.Size() && !info.ModTime().After(st.ModTime()) {
			return dstAbs
		}
		tmp := dstAbs + ".tmp"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			continue
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(tmp)
			// rewind for next target
			_, _ = in.Seek(0, io.SeekStart)
			continue
		}
		if err := os.Rename(tmp, dstAbs); err != nil {
			_ = os.Remove(tmp)
			_, _ = in.Seek(0, io.SeekStart)
			continue
		}
		return dstAbs
	}
	return ""
}

func findModuleRoot() string {
	// Walk from executable, cwd, and common workspace.
	starts := []string{}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for i := 0; i < 8; i++ {
			if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
				// Confirm bridge cmd exists
				if _, err := os.Stat(filepath.Join(dir, "cmd", "maclaw-acp-bridge")); err == nil {
					return dir
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return ""
}

func findVSCodeCLI() (string, error) {
	names := []string{"code", "code.cmd", "code.bat"}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	// Common install locations
	var extras []string
	switch runtime.GOOS {
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		pf := os.Getenv("ProgramFiles")
		pf86 := os.Getenv("ProgramFiles(x86)")
		for _, root := range []string{local, pf, pf86} {
			if root == "" {
				continue
			}
			extras = append(extras,
				filepath.Join(root, "Programs", "Microsoft VS Code", "bin", "code.cmd"),
				filepath.Join(root, "Microsoft VS Code", "bin", "code.cmd"),
				filepath.Join(root, "Programs", "Microsoft VS Code Insiders", "bin", "code-insiders.cmd"),
			)
		}
	case "darwin":
		extras = append(extras,
			"/usr/local/bin/code",
			"/opt/homebrew/bin/code",
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
			"/Applications/Visual Studio Code - Insiders.app/Contents/Resources/app/bin/code",
		)
	default:
		extras = append(extras,
			"/usr/bin/code",
			"/usr/local/bin/code",
			filepath.Join(os.Getenv("HOME"), ".local", "bin", "code"),
		)
	}
	for _, p := range extras {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("VS Code CLI (`code`) not found")
}

func installVSCodeACPExtension(codeCLI string) error {
	ctxTimeout := 120 * time.Second
	cmd := exec.Command(codeCLI, "--install-extension", vscodeACPExtensionID, "--force")
	hideACPLaunchWindow(cmd)
	// Avoid stealing focus too long; still wait for install.
	done := make(chan error, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		if err != nil {
			done <- fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(ctxTimeout):
		_ = cmd.Process.Kill()
		return fmt.Errorf("timeout installing extension")
	}
}

// installMaclawACPExtension ensures the bundled first-party extension is
// installed in VS Code at the embedded version. It extracts the embedded VSIX
// under <MaclawBaseDir>/bin/vsix/ and only invokes `code --install-extension`
// when the bundled version is newer, so repeat launches are cheap and a
// manually installed newer build is never downgraded.
func installMaclawACPExtension(codeCLI string) error {
	if len(maclawACPVsix) == 0 {
		return fmt.Errorf("bundled VSIX asset is empty")
	}
	want := maclawACPVsixVersion()
	if want == "" {
		return fmt.Errorf("bundled VSIX version.txt is empty")
	}
	installed, err := installedVSCodeExtensionVersion(codeCLI, maclawACPExtID)
	if err == nil && installed != "" && semverCompare(installed, want) >= 0 {
		return nil
	}
	vsixPath, err := extractMaclawACPVsix(want)
	if err != nil {
		return err
	}
	ctxTimeout := 120 * time.Second
	cmd := exec.Command(codeCLI, "--install-extension", vsixPath, "--force")
	hideACPLaunchWindow(cmd)
	done := make(chan error, 1)
	go func() {
		out, err := cmd.CombinedOutput()
		if err != nil {
			done <- fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(ctxTimeout):
		_ = cmd.Process.Kill()
		return fmt.Errorf("timeout installing extension from %s", vsixPath)
	}
}

// semverCompare compares dotted numeric versions ("0.1.10" vs "0.1.9"),
// returning -1/0/1. Pre-release/build suffixes ("0.1.0-beta") are ignored —
// a bundled stable always replaces a prerelease of the same numbers.
func semverCompare(a, b string) int {
	strip := func(s string) string {
		s = strings.TrimSpace(s)
		if i := strings.IndexAny(s, "-+"); i >= 0 {
			s = s[:i]
		}
		return s
	}
	as := strings.Split(strip(a), ".")
	bs := strings.Split(strip(b), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y string
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		xi, xerr := strconv.Atoi(x)
		yi, yerr := strconv.Atoi(y)
		if xerr == nil && yerr == nil {
			if xi != yi {
				if xi < yi {
					return -1
				}
				return 1
			}
			continue
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// installedVSCodeExtensionVersion reports the installed version of extID
// ("publisher.name"), or "" when not installed. Output lines look like
// "maclaw.maclaw-acp@0.1.0".
func installedVSCodeExtensionVersion(codeCLI, extID string) (string, error) {
	cmd := exec.Command(codeCLI, "--list-extensions", "--show-versions")
	hideACPLaunchWindow(cmd)
	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := cmd.Output()
		done <- struct {
			out string
			err error
		}{string(out), err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return "", r.err
		}
		prefix := strings.ToLower(extID) + "@"
		for _, line := range strings.Split(r.out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), prefix) {
				return strings.TrimSpace(line[len(prefix):]), nil
			}
		}
		return "", nil
	case <-time.After(45 * time.Second):
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("timeout listing extensions")
	}
}

// extractMaclawACPVsix writes the embedded VSIX to a stable versioned path
// (staleness-proof by name), skipping the write when it already exists.
func extractMaclawACPVsix(version string) (string, error) {
	dir := filepath.Join(corelib.MaclawBaseDir(), "bin", "vsix")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create vsix dir: %w", err)
	}
	target := filepath.Join(dir, "maclaw-acp-"+version+".vsix")
	if st, err := os.Stat(target); err == nil && st.Size() == int64(len(maclawACPVsix)) {
		return target, nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, maclawACPVsix, 0o644); err != nil {
		return "", fmt.Errorf("write vsix: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("install vsix: %w", err)
	}
	// Best-effort cleanup of older versioned VSIX files.
	if olds, _ := filepath.Glob(filepath.Join(dir, "maclaw-acp-*.vsix")); len(olds) > 0 {
		for _, old := range olds {
			if !strings.EqualFold(old, target) {
				_ = os.Remove(old)
			}
		}
	}
	return target, nil
}

func vscodeUserSettingsPath() string {
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			home, _ := os.UserHomeDir()
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Code", "User", "settings.json")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "Code", "User", "settings.json")
	}
}

func writeVSCodeACPAgentSettings(bridgePath string, ep gatewaySnap) (string, error) {
	path := vscodeUserSettingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}

	// Absolute bridge path for reliable spawn
	absBridge, err := filepath.Abs(bridgePath)
	if err != nil {
		absBridge = bridgePath
	}

	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	settings := map[string]any{}
	if len(raw) > 0 {
		cleaned := stripJSONC(raw)
		if err := json.Unmarshal(cleaned, &settings); err != nil {
			// Backup corrupt/jsonc-heavy file and start fresh merge container
			backup := path + ".maclaw-bak-" + time.Now().Format("20060102-150405")
			_ = os.WriteFile(backup, raw, 0o644)
			settings = map[string]any{}
		}
	}

	agents, _ := settings["acp.agents"].(map[string]any)
	if agents == nil {
		// VS Code uses flat key "acp.agents"
		agents = map[string]any{}
	}
	env := map[string]any{}
	if ep.BaseURL != "" {
		env["MACLAW_GATEWAY_URL"] = ep.BaseURL
	}
	if ep.Token != "" {
		env["MACLAW_GATEWAY_TOKEN"] = ep.Token
	}
	agents[vscodeACPAgentName] = map[string]any{
		"command": absBridge,
		"args":    []any{},
		"env":     env,
	}
	settings["acp.agents"] = agents

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// stripJSONC removes // line comments and /* */ blocks for a best-effort parse of VS Code settings.
func stripJSONC(data []byte) []byte {
	s := string(data)
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	esc := false
	i := 0
	for i < len(s) {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == '/' && i+1 < len(s) {
			if s[i+1] == '/' {
				// line comment
				for i < len(s) && s[i] != '\n' {
					i++
				}
				continue
			}
			if s[i+1] == '*' {
				i += 2
				for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
					i++
				}
				if i+1 < len(s) {
					i += 2
				}
				continue
			}
		}
		// trailing commas before } or ] — leave for now; VS Code often avoids them
		b.WriteByte(c)
		i++
	}
	// Remove trailing commas: ,\s*}
	out := b.String()
	out = removeTrailingCommas(out)
	return []byte(out)
}

func removeTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			// peek next non-space
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // skip trailing comma
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func launchVSCode(codeCLI, folder string) error {
	args := []string{}
	if strings.TrimSpace(folder) != "" {
		if st, err := os.Stat(folder); err == nil && st.IsDir() {
			args = append(args, folder)
		}
	}
	// New window so user sees the result
	args = append([]string{"-n"}, args...)
	cmd := exec.Command(codeCLI, args...)
	hideACPLaunchWindow(cmd)
	// Detach so GUI doesn't wait; hide console (code.cmd is a batch launcher).
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}
