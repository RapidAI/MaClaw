package remote

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHPool 管理到多台远程主机的 SSH 连接复用。
// 同一 hostID 的连接会被缓存，避免重复握手。
type SSHPool struct {
	mu    sync.Mutex
	conns map[string]*poolEntry
}

type poolEntry struct {
	client    *ssh.Client
	hostID    string
	createdAt time.Time
	refCount  int
}

// NewSSHPool 创建连接池。
func NewSSHPool() *SSHPool {
	return &SSHPool{
		conns: make(map[string]*poolEntry),
	}
}

// Acquire 获取或创建到指定主机的 SSH 连接。
func (p *SSHPool) Acquire(cfg SSHHostConfig) (*ssh.Client, error) {
	client, _, err := p.AcquireResolved(cfg)
	return client, err
}

// AcquireResolved acquires a connection and returns the effective host
// configuration used for it. When a caller supplies a host-key capture
// callback, the callback is invoked during this connection's SSH handshake
// and the returned configuration is upgraded to a concrete host-key pin.
//
// A capture request never reuses an unpinned pool entry: doing so would leave
// the new session without proof of which server key authenticated it. Once the
// handshake completes, the connection is entered into the pool under its final
// pinned identity so release, SFTP and reconnect all use the same key.
func (p *SSHPool) AcquireResolved(cfg SSHHostConfig) (*ssh.Client, SSHHostConfig, error) {
	resolved, err := ApplyOpenSSHClientConfig(cfg)
	if err != nil {
		return nil, cfg, err
	}
	cfg = resolved
	cfg.Defaults()
	if cfg.HostKeyFingerprintCapture != nil && strings.TrimSpace(cfg.HostKeyFingerprint) == "" {
		return p.acquireCaptured(cfg)
	}
	client, err := p.acquire(cfg)
	return client, cfg, err
}

func (p *SSHPool) acquireCaptured(cfg SSHHostConfig) (*ssh.Client, SSHHostConfig, error) {
	capture := cfg.HostKeyFingerprintCapture
	var capturedFingerprint string
	cfg.HostKeyFingerprintCapture = func(fingerprint string) {
		capturedFingerprint = fingerprint
		capture(fingerprint)
	}

	// A first-use capture must perform its own handshake. In particular, it
	// must not inherit a legacy/unpinned pooled client created by another call.
	client, err := dialSSH(cfg)
	if err != nil {
		return nil, cfg, fmt.Errorf("ssh dial %s: %w", cfg.SSHHostID(), err)
	}
	if strings.TrimSpace(capturedFingerprint) == "" {
		_ = client.Close()
		return nil, cfg, fmt.Errorf("ssh dial %s: host-key capture did not run", cfg.SSHHostID())
	}

	resolvedCfg := cfg
	resolvedCfg.HostKeyFingerprint = capturedFingerprint
	resolvedCfg.HostKeyFingerprintCapture = nil
	hostID := sshPoolKey(resolvedCfg)

	p.mu.Lock()
	if entry, ok := p.conns[hostID]; ok {
		entry.refCount++
		p.mu.Unlock()
		_ = client.Close()
		return entry.client, resolvedCfg, nil
	}
	p.conns[hostID] = &poolEntry{
		client:    client,
		hostID:    resolvedCfg.SSHHostID(),
		createdAt: time.Now(),
		refCount:  1,
	}
	p.mu.Unlock()

	go p.keepalive(hostID, client, resolvedCfg.KeepaliveInterval)
	return client, resolvedCfg, nil
}

func (p *SSHPool) acquire(cfg SSHHostConfig) (*ssh.Client, error) {
	cfg.Defaults()
	hostID := sshPoolKey(cfg)

	// 已被会话或传输引用的连接直接复用。
	// 对它做 keepalive 探测并在超时后 Close，会拆掉交互会话正在用的那条连接。
	// 断连交给连接池的 keepalive。
	p.mu.Lock()
	entry, found := p.conns[hostID]
	if found && entry.client != nil && entry.refCount > 0 {
		entry.refCount++
		client := entry.client
		p.mu.Unlock()
		return client, nil
	}
	var candidate *ssh.Client
	if found && entry.client != nil {
		candidate = entry.client
	}
	p.mu.Unlock()

	// 池里没有仍被引用的连接时，才在锁外探测。
	// 半开 TCP 上 SendRequest 可能阻塞 30-120 秒，必须限制等待时间。
	if candidate != nil {
		// 这条连接已经没有引用。探测没在时限内成功就丢掉它再拨号：
		// 未完成的 keepalive 占着连接锁，新会话挂上去会一直等。
		// 仍被会话引用的连接在上面直接复用，不会进到这里。
		ok, finished, _, _ := awaitSSHProbe(candidate, sshProbeWait)
		alive := finished && ok
		if alive {
			p.mu.Lock()
			if e, ok := p.conns[hostID]; ok && e.client == candidate {
				e.refCount++
				p.mu.Unlock()
				return candidate, nil
			}
			p.mu.Unlock()
			// entry 已被替换，走新建流程
		} else {
			// 空闲连接这次没有探测成功，丢掉再拨新的。
			p.mu.Lock()
			if e, ok := p.conns[hostID]; ok && e.client == candidate {
				delete(p.conns, hostID)
			}
			p.mu.Unlock()
			_ = candidate.Close()
		}
	}

	// 新建连接（在锁外）
	client, err := dialSSH(cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", cfg.SSHHostID(), err)
	}

	p.mu.Lock()
	// 再次检查是否有并发创建的连接
	if e, ok := p.conns[hostID]; ok {
		// 别人先建好了，关掉我们的，用已有的
		e.refCount++
		p.mu.Unlock()
		_ = client.Close()
		return e.client, nil
	}
	p.conns[hostID] = &poolEntry{
		client:    client,
		hostID:    cfg.SSHHostID(),
		createdAt: time.Now(),
		refCount:  1,
	}
	p.mu.Unlock()

	// 启动心跳
	go p.keepalive(hostID, client, cfg.KeepaliveInterval)

	return client, nil
}

// holdsClient reports whether client is still the pooled connection for cfg.
// A caller that released its own reference uses this to see whether another
// reference kept the client in the pool.
func (p *SSHPool) holdsClient(cfg SSHHostConfig, client *ssh.Client) bool {
	if p == nil || client == nil {
		return false
	}
	cfg.Defaults()
	hostID := sshPoolKey(cfg)
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.conns[hostID]
	return ok && entry.client == client
}

// Release 释放 caller 持有的那条连接。引用只对应当初 Acquire 到的 client。
// 池里的连接若已被替换，这里什么都不做，避免把新连接的计数减掉并关掉。
// 计数归零时才关闭，避免空闲连接泄漏。
func (p *SSHPool) Release(cfg SSHHostConfig, client *ssh.Client) {
	if client == nil {
		return
	}
	cfg.Defaults()
	hostID := sshPoolKey(cfg)

	var toClose *ssh.Client
	p.mu.Lock()
	if entry, ok := p.conns[hostID]; ok && entry.client == client {
		entry.refCount--
		if entry.refCount <= 0 {
			toClose = entry.client
			delete(p.conns, hostID)
		}
	}
	p.mu.Unlock()

	if toClose != nil {
		_ = toClose.Close()
	}
}

func sshPoolKey(cfg SSHHostConfig) string {
	base := cfg.SSHHostID()
	policy := "|legacy-unverified"
	if fingerprint := strings.TrimSpace(cfg.HostKeyFingerprint); fingerprint != "" {
		policy = "|fingerprint=" + fingerprint
	} else if len(cfg.KnownHostsFiles) > 0 {
		policy = "|known-hosts=" + strings.Join(cfg.KnownHostsFiles, ";")
	} else if knownHostsPath := strings.TrimSpace(cfg.KnownHostsPath); knownHostsPath != "" {
		policy = "|known-hosts=" + knownHostsPath
	}
	if len(cfg.JumpHosts) == 0 {
		return base + policy
	}
	// Close(hostID) matches the public ID plus "|". Jump identity stays after
	// that policy segment so a direct session and a jumped session do not share
	// a pooled connection.
	var jumps strings.Builder
	for _, hop := range cfg.JumpHosts {
		if jumps.Len() > 0 {
			jumps.WriteByte(',')
		}
		jumps.WriteString(hop.SSHHostID())
	}
	return base + policy + "|jump=" + jumps.String()
}

// Close closes all pooled connections for a public user@host:port ID,
// including entries isolated by host-key policy.
func (p *SSHPool) Close(hostID string) {
	p.mu.Lock()
	entries := make([]*poolEntry, 0, 1)
	for key, entry := range p.conns {
		if key == hostID || strings.HasPrefix(key, hostID+"|") {
			entries = append(entries, entry)
			delete(p.conns, key)
		}
	}
	p.mu.Unlock()
	for _, entry := range entries {
		if entry != nil && entry.client != nil {
			_ = entry.client.Close()
		}
	}
}

// CloseAll 关闭所有连接。
func (p *SSHPool) CloseAll() {
	p.mu.Lock()
	entries := make([]*poolEntry, 0, len(p.conns))
	for _, e := range p.conns {
		entries = append(entries, e)
	}
	p.conns = make(map[string]*poolEntry)
	p.mu.Unlock()

	for _, e := range entries {
		if e.client != nil {
			_ = e.client.Close()
		}
	}
}

// Stats 返回连接池状态。
func (p *SSHPool) Stats() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := make(map[string]int, len(p.conns))
	for _, e := range p.conns {
		// Pool entries are isolated by host-key policy internally, but stats are
		// externally observable. Aggregate by the public host ID so pinned
		// fingerprints and known_hosts paths never leak into UI or logs.
		stats[e.hostID] += e.refCount
	}
	return stats
}

// IsAlive 报告池里的这条连接还可以继续用。
// 与 SSHPTYSession.IsAlive 同一规则：传输错误或同一次请求超过
// sshProbeStuckAfter 返回 false；更短的未完成等待仍算存活。
func (p *SSHPool) IsAlive(cfg SSHHostConfig) bool {
	cfg.Defaults()
	hostID := sshPoolKey(cfg)

	p.mu.Lock()
	entry, found := p.conns[hostID]
	p.mu.Unlock()

	if !found || entry.client == nil {
		return false
	}
	ok, finished, running, _ := awaitSSHProbe(entry.client, sshProbeWait)
	return !sshProbeCountsAsFailure(finished, ok, running)
}

// Reconnect 强制断开旧连接并重新建立到指定主机的连接。
// 返回新的 ssh.Client。
func (p *SSHPool) Reconnect(cfg SSHHostConfig) (*ssh.Client, error) {
	resolved, err := ApplyOpenSSHClientConfig(cfg)
	if err != nil {
		return nil, err
	}
	cfg = resolved
	cfg.Defaults()
	hostID := sshPoolKey(cfg)

	// 清理旧连接
	p.mu.Lock()
	old, found := p.conns[hostID]
	if found {
		delete(p.conns, hostID)
	}
	p.mu.Unlock()
	if found && old.client != nil {
		_ = old.client.Close()
	}

	// 新建连接
	client, err := dialSSH(cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh reconnect %s: %w", cfg.SSHHostID(), err)
	}

	p.mu.Lock()
	// 再次检查是否有并发创建的连接
	if e, ok := p.conns[hostID]; ok {
		e.refCount++
		p.mu.Unlock()
		_ = client.Close()
		return e.client, nil
	}
	p.conns[hostID] = &poolEntry{
		client:    client,
		hostID:    cfg.SSHHostID(),
		createdAt: time.Now(),
		refCount:  1,
	}
	p.mu.Unlock()

	go p.keepalive(hostID, client, cfg.KeepaliveInterval)
	return client, nil
}

func (p *SSHPool) keepalive(hostID string, client *ssh.Client, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second // 缩短默认心跳间隔，更快检测断连
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failCount := 0
	for range ticker.C {
		// 同一条连接同时只发一个 keepalive。超时后丢掉的 SendRequest 会占着
		// 全局锁，后面的探测跟着超时，活连接也会被关掉。
		ok, finished, running, _ := awaitSSHProbe(client, sshProbeWait)
		if finished && ok {
			failCount = 0
			continue
		}
		if !sshProbeCountsAsFailure(finished, ok, running) {
			continue
		}
		failCount++
		// 三次真正的传输错误才关闭。一次请求超过 sshProbeStuckAfter 仍无应答，
		// 说明是半开连接，立刻关闭以便正在进行的下载从阻塞里返回。
		if finished && failCount < 3 {
			continue
		}
		p.mu.Lock()
		if entry, ok := p.conns[hostID]; ok && entry.client == client {
			delete(p.conns, hostID)
		}
		p.mu.Unlock()
		_ = client.Close()
		return
	}
}
