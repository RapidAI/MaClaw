package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// dialSSH 根据 SSHHostConfig 建立 SSH 连接。
// OpenSSH 别名（含 ProxyJump）在这里解析。直连使用 TCP keepalive，防止空闲连接被 NAT/防火墙断开。
func dialSSH(cfg SSHHostConfig) (*ssh.Client, error) {
	resolved, err := ApplyOpenSSHClientConfig(cfg)
	if err != nil {
		return nil, err
	}
	cfg = resolved
	if len(cfg.JumpHosts) > 0 {
		return dialSSHViaJump(cfg)
	}
	return dialSSHDirect(cfg)
}

func dialSSHDirect(cfg SSHHostConfig) (*ssh.Client, error) {
	cfg.Defaults()
	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, err
	}

	hostKeyCallback, err := sshHostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}
	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.ConnectTimeout,
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))

	// 先建立 TCP 连接并启用 TCP keepalive，再在其上建立 SSH
	tcpConn, err := net.DialTimeout("tcp", addr, cfg.ConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("tcp connect to %s: %w", addr, err)
	}

	// 启用 TCP 级别 keepalive，防止 NAT/防火墙超时断连
	if tc, ok := tcpConn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(15 * time.Second)
	}

	// 在 TCP 连接上建立 SSH
	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, sshCfg)
	if err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("ssh handshake to %s: %w", addr, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

// jumpLifetime closes the jump clients exactly once when the target connection ends.
type jumpLifetime struct {
	once sync.Once
	hops []io.Closer
}

func (j *jumpLifetime) Close() {
	if j == nil {
		return
	}
	j.once.Do(func() {
		for _, hop := range j.hops {
			if hop != nil {
				_ = hop.Close()
			}
		}
	})
}

type closeAfterConn struct {
	net.Conn
	life *jumpLifetime
}

func (c *closeAfterConn) Close() error {
	err := c.Conn.Close()
	if c.life != nil {
		c.life.Close()
	}
	return err
}

func dialSSHViaJump(cfg SSHHostConfig) (*ssh.Client, error) {
	life := &jumpLifetime{}
	var current *ssh.Client
	for _, hop := range cfg.JumpHosts {
		hop.JumpHosts = nil
		var next *ssh.Client
		var err error
		if current == nil {
			next, err = dialSSHDirect(hop)
		} else {
			next, err = dialSSHThrough(current, hop, nil)
		}
		if err != nil {
			life.Close()
			return nil, fmt.Errorf("ssh jump %s: %w", hop.SSHHostID(), err)
		}
		life.hops = append(life.hops, next)
		current = next
	}
	target := cfg
	target.JumpHosts = nil
	client, err := dialSSHThrough(current, target, life)
	if err != nil {
		life.Close()
		return nil, err
	}
	return client, nil
}

func dialSSHThrough(jump *ssh.Client, cfg SSHHostConfig, life *jumpLifetime) (*ssh.Client, error) {
	cfg.Defaults()
	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := sshHostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}
	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.ConnectTimeout,
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	raw, err := dialJumpTCP(jump, addr, cfg.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	var conn net.Conn = raw
	if life != nil {
		conn = &closeAfterConn{Conn: raw, life: life}
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh handshake to %s: %w", addr, err)
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func dialJumpTCP(jump *ssh.Client, addr string, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	type dialResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan dialResult, 1)
	go func() {
		conn, err := jump.Dial("tcp", addr)
		ch <- dialResult{conn: conn, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-ch:
		if result.err != nil {
			return nil, fmt.Errorf("jump dial %s: %w", addr, result.err)
		}
		return result.conn, nil
	case <-timer.C:
		go func() {
			result := <-ch
			if result.conn != nil {
				_ = result.conn.Close()
			}
		}()
		return nil, fmt.Errorf("jump dial %s: timeout after %s", addr, timeout)
	}
}

func knownHostFiles(cfg SSHHostConfig) []string {
	if len(cfg.KnownHostsFiles) > 0 {
		return cfg.KnownHostsFiles
	}
	if path := strings.TrimSpace(cfg.KnownHostsPath); path != "" {
		return []string{path}
	}
	return nil
}

func sshHostKeyFallback(cfg SSHHostConfig) ssh.HostKeyCallback {
	if capture := cfg.HostKeyFingerprintCapture; capture != nil {
		return func(_ string, _ net.Addr, key ssh.PublicKey) error {
			capture(ssh.FingerprintSHA256(key))
			return nil
		}
	}
	return ssh.InsecureIgnoreHostKey()
}

func sshHostKeyCallback(cfg SSHHostConfig) (ssh.HostKeyCallback, error) {
	if pinned := strings.TrimSpace(cfg.HostKeyFingerprint); pinned != "" {
		return func(_ string, _ net.Addr, key ssh.PublicKey) error {
			observed := ssh.FingerprintSHA256(key)
			if observed != pinned {
				return fmt.Errorf("SSH host key mismatch: expected %s, received %s", pinned, observed)
			}
			return nil
		}, nil
	}
	files := knownHostFiles(cfg)
	if len(files) > 0 {
		callback, err := knownhosts.New(files...)
		if err != nil {
			// A single unreadable or unparsable file must not block an alias that
			// OpenSSH itself can still log in to. Callers that pinned one file
			// keep the hard error.
			if strings.TrimSpace(cfg.HostAlias) == "" {
				return nil, fmt.Errorf("load SSH known_hosts: %w", err)
			}
			return sshHostKeyFallback(cfg), nil
		}
		capture := cfg.HostKeyFingerprintCapture
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			var mismatch error
			var firstErr error
			for _, name := range hostKeyCandidates(cfg, hostname) {
				err := callback(name, remote, key)
				if err == nil {
					if capture != nil {
						capture(ssh.FingerprintSHA256(key))
					}
					return nil
				}
				if firstErr == nil {
					firstErr = err
				}
				var keyErr *knownhosts.KeyError
				if errors.As(err, &keyErr) {
					if len(keyErr.Want) > 0 {
						mismatch = err
					}
					continue
				}
				return err
			}
			if mismatch != nil {
				return mismatch
			}
			// An OpenSSH alias may be recorded under a name this checker does not
			// try. A changed key still fails above. An unknown alias keeps the
			// tool's previous accept-on-first-use behavior so a working login
			// is not rejected solely because known_hosts used another spelling.
			if strings.TrimSpace(cfg.HostAlias) != "" {
				if capture != nil {
					capture(ssh.FingerprintSHA256(key))
				}
				return nil
			}
			return firstErr
		}, nil
	}
	if capture := cfg.HostKeyFingerprintCapture; capture != nil {
		return func(_ string, _ net.Addr, key ssh.PublicKey) error {
			capture(ssh.FingerprintSHA256(key))
			return nil
		}, nil
	}
	// Preserve existing SSH-tool behavior; security-sensitive callers such as
	// Virtual Repository pass an explicit fingerprint.
	return ssh.InsecureIgnoreHostKey(), nil
}

// buildAuthMethods 根据配置构建 SSH 认证方法列表。
// 优先级：用户指定的 AuthMethod 排第一，其他可用方法作为 fallback。
// OpenSSH 解析出的连接先出示配置里的身份文件，再出示 ssh-agent。
// 先把 agent 里的全部钥匙试完会占满服务器的认证次数，配置里的那把钥匙就排不上。
func buildAuthMethods(cfg SSHHostConfig) ([]ssh.AuthMethod, error) {
	if cfg.PreferAgent {
		return buildOpenSSHAuthMethods(cfg)
	}
	var methods []ssh.AuthMethod
	var primaryErr error

	// 1. 首选认证方式
	switch cfg.AuthMethod {
	case "password":
		if cfg.Password != "" {
			methods = append(methods, ssh.Password(cfg.Password))
			// 同时支持 keyboard-interactive（某些服务器用这种方式要求密码）
			methods = append(methods, ssh.KeyboardInteractive(
				func(user, instruction string, questions []string, echos []bool) ([]string, error) {
					answers := make([]string, len(questions))
					for i := range questions {
						answers[i] = cfg.Password
					}
					return answers, nil
				},
			))
		}
		// Password 为空时不报错，让 fallback 兜底（key/agent）

	case "agent":
		authMethod, err := sshAgentAuth()
		if err != nil {
			primaryErr = err
		} else {
			methods = append(methods, authMethod)
		}

	case "key":
		keyMethod, err := buildKeyAuth(cfg)
		if err != nil {
			primaryErr = err
		} else {
			methods = append(methods, keyMethod)
		}

	default:
		return nil, fmt.Errorf("unsupported auth method: %s", cfg.AuthMethod)
	}

	// 2. Fallback：如果首选不是密码但有密码，加密码作为备选
	if cfg.AuthMethod != "password" && cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}

	// 3. Fallback：如果首选不是密钥，尝试加密钥作为备选（静默失败）
	if cfg.AuthMethod != "key" {
		if keyMethod, err := buildKeyAuth(cfg); err == nil {
			methods = append(methods, keyMethod)
		}
	}

	// 4. Fallback：如果首选不是 agent，尝试加 agent 作为备选（静默失败）
	if cfg.AuthMethod != "agent" {
		if agentMethod, err := sshAgentAuth(); err == nil {
			methods = append(methods, agentMethod)
		}
	}

	if len(methods) == 0 {
		if primaryErr != nil {
			return nil, primaryErr
		}
		return nil, fmt.Errorf("no ssh auth method available")
	}

	return methods, nil
}

func buildOpenSSHAuthMethods(cfg SSHHostConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	fileMethod, fileSigners, _ := identityFileAuth(cfg)
	if len(fileSigners) > 0 {
		// Offer the agent copy of this key first. A one-hour certificate usually
		// lives there, while unrelated agent keys must not be sent.
		if agentMethod, err := sshAgentAuthMatching(fileSigners); err == nil {
			methods = append(methods, agentMethod)
		}
		methods = append(methods, fileMethod)
	} else if len(cfg.IdentityFiles) == 0 && strings.TrimSpace(cfg.KeyPath) == "" {
		if keyMethod, err := buildKeyAuth(cfg); err == nil {
			methods = append(methods, keyMethod)
		}
		if agentMethod, err := sshAgentAuth(); err == nil {
			methods = append(methods, agentMethod)
		}
	} else if agentMethod, err := sshAgentAuth(); err == nil {
		methods = append(methods, agentMethod)
	}
	if cfg.Password != "" {
		methods = append(methods, passwordAuthMethods(cfg.Password)...)
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no ssh auth method available")
	}
	return methods, nil
}

func passwordAuthMethods(password string) []ssh.AuthMethod {
	if password == "" {
		return nil
	}
	return []ssh.AuthMethod{
		ssh.Password(password),
		ssh.KeyboardInteractive(
			func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = password
				}
				return answers, nil
			},
		),
	}
}

// buildKeyAuth 构建密钥认证方法。
func buildKeyAuth(cfg SSHHostConfig) (ssh.AuthMethod, error) {
	keyPath := cfg.KeyPath
	if keyPath == "" {
		home, _ := os.UserHomeDir()
		keyPath = home + "/.ssh/id_rsa"
	}
	signer, err := signerFromKeyFile(keyPath, cfg.Passphrase, cfg.CertificateFiles)
	if err != nil {
		return nil, err
	}
	return ssh.PublicKeys(signer), nil
}

func buildIdentityFilesAuth(cfg SSHHostConfig) (ssh.AuthMethod, error) {
	method, signers, err := identityFileAuth(cfg)
	if err != nil {
		return nil, err
	}
	if len(signers) == 0 {
		return nil, fmt.Errorf("no ssh identity file available")
	}
	return method, nil
}

func identityFileAuth(cfg SSHHostConfig) (ssh.AuthMethod, []ssh.Signer, error) {
	paths := cfg.IdentityFiles
	if len(paths) == 0 && strings.TrimSpace(cfg.KeyPath) != "" {
		paths = []string{cfg.KeyPath}
	}
	var signers []ssh.Signer
	var firstErr error
	for _, path := range paths {
		signer, err := signerFromKeyFile(path, cfg.Passphrase, cfg.CertificateFiles)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		signers = append(signers, signer)
	}
	if len(signers) == 0 {
		if firstErr != nil {
			return nil, nil, firstErr
		}
		return nil, nil, fmt.Errorf("no ssh identity file available")
	}
	return ssh.PublicKeys(signers...), signers, nil
}

func signerFromKeyFile(path, passphrase string, certPaths []string) (ssh.Signer, error) {
	path = expandHome(strings.TrimSpace(path))
	keyData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ssh key %s: %w", path, err)
	}
	var signer ssh.Signer
	if passphrase != "" {
		// P0-4 (2026-09-08 review): wipe the passphrase bytes as soon as parse returns.
		passphraseSecret := BuildPassphraseSecret(passphrase)
		defer passphraseSecret.Zero()
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, passphraseSecret.Reveal())
	} else {
		signer, err = ssh.ParsePrivateKey(keyData)
	}
	SecureZeroPassword(keyData)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key %s: %w", path, err)
	}
	if certSigner, ok := signerWithCertificate(signer, path, certPaths); ok {
		return certSigner, nil
	}
	return signer, nil
}

func signerWithCertificate(signer ssh.Signer, keyPath string, certPaths []string) (ssh.Signer, bool) {
	candidates := make([]string, 0, len(certPaths)+1)
	candidates = append(candidates, keyPath+"-cert.pub")
	candidates = append(candidates, certPaths...)
	seen := make(map[string]struct{}, len(candidates))
	now := time.Now()
	for _, certPath := range candidates {
		certPath = expandHome(strings.TrimSpace(certPath))
		if certPath == "" {
			continue
		}
		if _, ok := seen[certPath]; ok {
			continue
		}
		seen[certPath] = struct{}{}
		data, err := os.ReadFile(certPath)
		if err != nil {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			continue
		}
		cert, ok := pub.(*ssh.Certificate)
		if !ok || !certificateUsable(cert, now) {
			continue
		}
		certSigner, err := ssh.NewCertSigner(cert, signer)
		if err != nil {
			continue
		}
		return certSigner, true
	}
	return nil, false
}

func certificateUsable(cert *ssh.Certificate, now time.Time) bool {
	if cert == nil {
		return false
	}
	unix := now.Unix()
	if unix < 0 {
		return false
	}
	if cert.ValidAfter != 0 && int64(cert.ValidAfter) > unix {
		return false
	}
	// OpenSSH writes forever as the maximum uint64. Zero is not a usable window.
	return cert.ValidBefore > uint64(unix)
}

func hostKeyCandidates(cfg SSHHostConfig, hostname string) []string {
	names := []string{hostname}
	alias := strings.TrimSpace(cfg.HostAlias)
	if alias == "" {
		return names
	}
	port := ""
	if _, parsedPort, err := net.SplitHostPort(hostname); err == nil {
		port = parsedPort
	}
	if port == "" && cfg.Port > 0 {
		port = strconv.Itoa(cfg.Port)
	}
	if port == "" {
		port = "22"
	}
	names = append(names, net.JoinHostPort(alias, port))
	return names
}

func rawPublicKey(pk ssh.PublicKey) ssh.PublicKey {
	if cert, ok := pk.(*ssh.Certificate); ok && cert != nil && cert.Key != nil {
		return cert.Key
	}
	return pk
}

func publicKeyFingerprints(signers []ssh.Signer) map[string]struct{} {
	out := make(map[string]struct{}, len(signers))
	for _, signer := range signers {
		if signer == nil || signer.PublicKey() == nil {
			continue
		}
		out[ssh.FingerprintSHA256(rawPublicKey(signer.PublicKey()))] = struct{}{}
	}
	return out
}

func matchingAgentSigners(agentSigners []ssh.Signer, want map[string]struct{}, now time.Time) []ssh.Signer {
	var matched []ssh.Signer
	for _, signer := range agentSigners {
		if signer == nil || signer.PublicKey() == nil {
			continue
		}
		if cert, ok := signer.PublicKey().(*ssh.Certificate); ok && !certificateUsable(cert, now) {
			continue
		}
		if _, ok := want[ssh.FingerprintSHA256(rawPublicKey(signer.PublicKey()))]; !ok {
			continue
		}
		matched = append(matched, signer)
	}
	return matched
}

func sshAgentAuthMatching(fileSigners []ssh.Signer) (ssh.AuthMethod, error) {
	client, err := dialSSHAgent()
	if err != nil {
		return nil, err
	}
	want := publicKeyFingerprints(fileSigners)
	return ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
		signers, err := client.Signers()
		if err != nil {
			// An agent error must not abort the identity-file method that follows.
			return nil, nil
		}
		return matchingAgentSigners(signers, want, time.Now()), nil
	}), nil
}

// sshAgentAuth 连接 ssh-agent 并返回认证方法。
// Windows 上使用 named pipe，Unix 上使用 SSH_AUTH_SOCK。
func sshAgentAuth() (ssh.AuthMethod, error) {
	client, err := dialSSHAgent()
	if err != nil {
		return nil, err
	}
	// 使用 PublicKeysCallback 而非直接获取 signers，
	// 这样 agent 连接的生命周期跟随 ssh.Client。
	return ssh.PublicKeysCallback(client.Signers), nil
}

func dialSSHAgent() (sshagent.Agent, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		if runtime.GOOS == "windows" {
			// Windows OpenSSH agent 使用 named pipe
			sock = `\\.\pipe\openssh-ssh-agent`
		} else {
			return nil, fmt.Errorf("SSH_AUTH_SOCK not set, ssh-agent not available")
		}
	}

	network := "unix"
	if runtime.GOOS == "windows" {
		network = "pipe" // Go 1.21+ 支持 named pipe dial
	}

	agentConn, err := net.Dial(network, sock)
	if err != nil {
		// Windows fallback: 尝试 unix dial（某些 WSL 场景）
		if runtime.GOOS == "windows" {
			agentConn, err = net.Dial("unix", sock)
		}
		if err != nil {
			return nil, fmt.Errorf("ssh-agent not available: %w", err)
		}
	}
	return sshagent.NewClient(agentConn), nil
}
