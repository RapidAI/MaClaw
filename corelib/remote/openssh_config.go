package remote

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OpenSSHDefaultsProbeHost is queried with ssh -G to learn global defaults.
// Host-specific ProxyJump and IdentityFile values are whatever differs from
// this probe, so a global Host * block is not applied to every short name.
const OpenSSHDefaultsProbeHost = "maclaw-openssh-default-probe"

const openSSHMaxJumps = 8

var (
	openSSHMu          sync.Mutex
	openSSHConfigQuery = queryOpenSSHConfig
	openSSHProbe       parsedOpenSSHConfig
	openSSHProbeErr    error
	openSSHProbeLoaded bool
)

// SetOpenSSHConfigQueryForTest replaces ssh -G for the duration of a test.
// The returned function restores the previous query.
func SetOpenSSHConfigQueryForTest(fn func(host, user string, port int) (string, error)) func() {
	openSSHMu.Lock()
	prev := openSSHConfigQuery
	openSSHConfigQuery = fn
	openSSHProbeLoaded = false
	openSSHMu.Unlock()
	return func() {
		openSSHMu.Lock()
		openSSHConfigQuery = prev
		openSSHProbeLoaded = false
		openSSHMu.Unlock()
	}
}

// ApplyOpenSSHClientConfig resolves an OpenSSH config alias such as gpu84.
// IP addresses and dotted hostnames are not looked up. Every returned config
// uses the same endpoint spelling as SSHHostID, including a bracketed IP.
// A matching alias rewrites HostName, fills an empty user, records IdentityFile,
// and builds the ProxyJump chain. Explicit user, port, key path, and password
// supplied by the caller stay in place.
func ApplyOpenSSHClientConfig(cfg SSHHostConfig) (SSHHostConfig, error) {
	// Alias lookup and the dial address share one spelling. An IP never reaches
	// ssh -G, but it still has to leave here in the form SSHHostID and JoinHostPort use.
	cfg.normalizeEndpoint()
	if cfg.OpenSSHChecked || !shouldConsultOpenSSH(cfg.Host) {
		return cfg, nil
	}
	resolved, err := applyOpenSSH(cfg, map[string]struct{}{})
	resolved.normalizeEndpoint()
	for i := range resolved.JumpHosts {
		resolved.JumpHosts[i].normalizeEndpoint()
	}
	return resolved, err
}

func applyOpenSSH(cfg SSHHostConfig, visited map[string]struct{}) (SSHHostConfig, error) {
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.User = strings.TrimSpace(cfg.User)
	cfg.KeyPath = strings.TrimSpace(cfg.KeyPath)
	cfg.AuthMethod = strings.TrimSpace(cfg.AuthMethod)
	if cfg.OpenSSHChecked || !shouldConsultOpenSSH(cfg.Host) {
		cfg.OpenSSHChecked = true
		return cfg, nil
	}
	key := strings.ToLower(cfg.Host)
	if _, seen := visited[key]; seen {
		return cfg, fmt.Errorf("跳板 %s 形成循环", cfg.Host)
	}
	visited[key] = struct{}{}

	probeCh := make(chan probeResult, 1)
	go func() {
		parsed, err := loadOpenSSHProbe()
		probeCh <- probeResult{parsed: parsed, err: err}
	}()
	text, err := currentOpenSSHQuery()(cfg.Host, cfg.User, cfg.Port)
	probeRes := <-probeCh
	if probeRes.err != nil {
		return cfg, fmt.Errorf("读取 OpenSSH 默认配置: %w", probeRes.err)
	}
	if err != nil {
		return cfg, fmt.Errorf("读取 OpenSSH 主机 %s: %w", cfg.Host, err)
	}
	probe := probeRes.parsed
	parsed := parseOpenSSHG(text)
	cfg.OpenSSHChecked = true

	hostname := strings.Trim(strings.TrimSpace(parsed.hostname), "[]")
	proxyJump := normalizeProxyJump(parsed.proxyJump)
	probeJump := normalizeProxyJump(probe.proxyJump)
	identityChanged := identityDelta(parsed.identityFiles, probe.identityFiles)
	hostChanged := hostname != "" && !strings.EqualFold(hostname, cfg.Host)
	jumpChanged := proxyJump != "" && proxyJump != probeJump
	if !hostChanged && !jumpChanged && !identityChanged {
		return cfg, nil
	}

	explicitKey := cfg.KeyPath != ""
	explicitPort := cfg.Port > 0
	if hostChanged {
		cfg.HostAlias = cfg.Host
		cfg.Host = hostname
		if cfg.Label == "" {
			cfg.Label = cfg.HostAlias
		}
	}
	if cfg.User == "" && parsed.user != "" {
		cfg.User = parsed.user
	}
	if !explicitPort && parsed.port > 0 {
		cfg.Port = parsed.port
	}
	if !explicitKey {
		// Defaults such as id_rsa are also listed by ssh -G. Offering every one
		// of them uses up the server's authentication attempts before the
		// alias's own IdentityFile.
		cfg.IdentityFiles = cleanPaths(identityFilesExcept(parsed.identityFiles, probe.identityFiles))
		if len(cfg.IdentityFiles) > 0 {
			cfg.KeyPath = cfg.IdentityFiles[0]
		}
		cfg.CertificateFiles = cleanPaths(parsed.certificateFiles)
		if !strings.EqualFold(cfg.AuthMethod, "password") {
			cfg.PreferAgent = true
		}
	}
	if cfg.KnownHostsPath == "" && len(cfg.KnownHostsFiles) == 0 && strings.TrimSpace(cfg.HostKeyFingerprint) == "" {
		if files := existingFiles(append(cleanPaths(parsed.knownHosts), defaultKnownHostsPath())); len(files) > 0 {
			cfg.KnownHostsFiles = files
			cfg.KnownHostsPath = files[0]
		}
	}
	if jumpChanged {
		hops, err := resolveProxyJump(proxyJump, visited)
		if err != nil {
			return cfg, err
		}
		cfg.JumpHosts = hops
	}
	return cfg, nil
}

func resolveProxyJump(spec string, visited map[string]struct{}) ([]SSHHostConfig, error) {
	var hops []SSHHostConfig
	for _, token := range strings.Split(spec, ",") {
		user, host, port := parseProxyJumpToken(token)
		if host == "" {
			continue
		}
		jumpCfg := SSHHostConfig{Host: host, User: user, Port: port}
		resolved, err := applyOpenSSH(jumpCfg, visited)
		if err != nil {
			return nil, fmt.Errorf("解析跳板 %s: %w", host, err)
		}
		nested := resolved.JumpHosts
		resolved.JumpHosts = nil
		hops = append(hops, nested...)
		hops = append(hops, resolved)
		if len(hops) > openSSHMaxJumps {
			return nil, fmt.Errorf("跳板层数超过 %d", openSSHMaxJumps)
		}
	}
	if len(hops) == 0 {
		return nil, fmt.Errorf("ProxyJump %q 没有可用跳板", spec)
	}
	return hops, nil
}

func shouldConsultOpenSSH(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\r\n\\/\"'") {
		return false
	}
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return false
	}
	if strings.Contains(host, ".") {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return false
	}
	return true
}

type parsedOpenSSHConfig struct {
	user             string
	hostname         string
	port             int
	proxyJump        string
	identityFiles    []string
	certificateFiles []string
	knownHosts       []string
}

func parseOpenSSHG(output string) parsedOpenSSHConfig {
	var parsed parsedOpenSSHConfig
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := parseOpenSSHConfigLine(line)
		if !ok {
			continue
		}
		switch key {
		case "user":
			parsed.user = value
		case "hostname":
			parsed.hostname = value
		case "port":
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				parsed.port = n
			}
		case "proxyjump":
			parsed.proxyJump = value
		case "identityfile":
			if value != "" {
				parsed.identityFiles = append(parsed.identityFiles, value)
			}
		case "certificatefile":
			if value != "" && !strings.EqualFold(value, "none") {
				parsed.certificateFiles = append(parsed.certificateFiles, value)
			}
		case "userknownhostsfile":
			for _, path := range strings.Fields(value) {
				if path != "" && !strings.EqualFold(path, "none") {
					parsed.knownHosts = append(parsed.knownHosts, path)
				}
			}
		}
	}
	return parsed
}

func parseOpenSSHConfigLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return strings.ToLower(line), "", true
	}
	return strings.ToLower(line[:i]), strings.Trim(strings.TrimSpace(line[i+1:]), `"'`), true
}

func parseProxyJumpToken(token string) (user, host string, port int) {
	token = strings.TrimSpace(token)
	token = strings.TrimPrefix(token, "ssh://")
	if at := strings.LastIndex(token, "@"); at >= 0 {
		user = token[:at]
		token = token[at+1:]
	}
	host = token
	if h, p, err := net.SplitHostPort(token); err == nil {
		if n, convErr := strconv.Atoi(p); convErr == nil && n > 0 {
			return user, strings.Trim(h, "[]"), n
		}
	}
	if strings.Count(token, ":") == 1 {
		parts := strings.SplitN(token, ":", 2)
		if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 && !strings.Contains(parts[0], ":") {
			return user, parts[0], n
		}
	}
	return user, strings.Trim(host, "[]"), 0
}

func normalizeProxyJump(value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "none") {
		return ""
	}
	return value
}

func identityDelta(hostFiles, probeFiles []string) bool {
	return len(identityFilesExcept(hostFiles, probeFiles)) > 0
}

func identityFilesExcept(hostFiles, probeFiles []string) []string {
	probeSet := make(map[string]struct{}, len(probeFiles))
	for _, path := range probeFiles {
		probeSet[strings.ToLower(normalizeIdentityPath(path))] = struct{}{}
	}
	var extra []string
	for _, path := range hostFiles {
		if _, ok := probeSet[strings.ToLower(normalizeIdentityPath(path))]; ok {
			continue
		}
		extra = append(extra, path)
	}
	return extra
}

type probeResult struct {
	parsed parsedOpenSSHConfig
	err    error
}

func normalizeIdentityPath(path string) string {
	path = expandHome(strings.Trim(strings.TrimSpace(path), `"'`))
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func cleanPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = normalizeIdentityPath(path)
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

func defaultKnownHostsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

func existingFiles(paths []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

func loadOpenSSHProbe() (parsedOpenSSHConfig, error) {
	openSSHMu.Lock()
	if openSSHProbeLoaded {
		parsed, err := openSSHProbe, openSSHProbeErr
		openSSHMu.Unlock()
		return parsed, err
	}
	query := openSSHConfigQuery
	openSSHMu.Unlock()

	text, err := query(OpenSSHDefaultsProbeHost, "", 0)
	parsed := parsedOpenSSHConfig{}
	if err == nil {
		parsed = parseOpenSSHG(text)
	}

	openSSHMu.Lock()
	defer openSSHMu.Unlock()
	if openSSHProbeLoaded {
		return openSSHProbe, openSSHProbeErr
	}
	openSSHProbe = parsed
	openSSHProbeErr = err
	openSSHProbeLoaded = true
	return parsed, err
}

func currentOpenSSHQuery() func(host, user string, port int) (string, error) {
	openSSHMu.Lock()
	defer openSSHMu.Unlock()
	if openSSHConfigQuery == nil {
		return queryOpenSSHConfig
	}
	return openSSHConfigQuery
}

func queryOpenSSHConfig(host, user string, port int) (string, error) {
	if strings.TrimSpace(host) == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\r\n") {
		return "", fmt.Errorf("拒绝解析 SSH 主机 %q", host)
	}
	sshBin, err := exec.LookPath("ssh")
	if err != nil {
		return "", err
	}
	args := []string{"-G"}
	if strings.TrimSpace(user) != "" {
		args = append(args, "-l", user)
	}
	if port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	args = append(args, "--", host)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, sshBin, args...)
	hideCommandWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return string(out), nil
}
