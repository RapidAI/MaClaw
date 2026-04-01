package remote

import (
	"fmt"
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
	cfg.Defaults()
	hostID := cfg.SSHHostID()

	// 先尝试从池中取出候选连接（不持锁做网络 I/O）
	p.mu.Lock()
	entry, found := p.conns[hostID]
	var candidate *ssh.Client
	if found {
		candidate = entry.client
	}
	p.mu.Unlock()

	// 在锁外检查连接是否存活
	if candidate != nil {
		_, _, err := candidate.SendRequest("keepalive@openssh.com", true, nil)
		if err == nil {
			p.mu.Lock()
			if e, ok := p.conns[hostID]; ok && e.client == candidate {
				e.refCount++
				p.mu.Unlock()
				return candidate, nil
			}
			p.mu.Unlock()
			// entry 已被替换，走新建流程
		} else {
			// 连接已断，清理
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
		return nil, fmt.Errorf("ssh dial %s: %w", hostID, err)
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
		hostID:    hostID,
		createdAt: time.Now(),
		refCount:  1,
	}
	p.mu.Unlock()

	// 启动心跳
	go p.keepalive(hostID, client, cfg.KeepaliveInterval)

	return client, nil
}

// Release 释放连接引用。当引用计数归零时主动关闭连接，避免空闲连接泄漏。
func (p *SSHPool) Release(cfg SSHHostConfig) {
	cfg.Defaults()
	hostID := cfg.SSHHostID()

	var toClose *ssh.Client
	p.mu.Lock()
	if entry, ok := p.conns[hostID]; ok {
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

// Close 关闭指定主机的连接。
func (p *SSHPool) Close(hostID string) {
	p.mu.Lock()
	entry, ok := p.conns[hostID]
	if ok {
		delete(p.conns, hostID)
	}
	p.mu.Unlock()
	if ok && entry.client != nil {
		_ = entry.client.Close()
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
	for id, e := range p.conns {
		stats[id] = e.refCount
	}
	return stats
}

// IsAlive 检查指定主机的连接是否存活。
func (p *SSHPool) IsAlive(cfg SSHHostConfig) bool {
	cfg.Defaults()
	hostID := cfg.SSHHostID()

	p.mu.Lock()
	entry, found := p.conns[hostID]
	p.mu.Unlock()

	if !found || entry.client == nil {
		return false
	}
	_, _, err := entry.client.SendRequest("keepalive@openssh.com", true, nil)
	return err == nil
}

// Reconnect 强制断开旧连接并重新建立到指定主机的连接。
// 返回新的 ssh.Client。
func (p *SSHPool) Reconnect(cfg SSHHostConfig) (*ssh.Client, error) {
	cfg.Defaults()
	hostID := cfg.SSHHostID()

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
		return nil, fmt.Errorf("ssh reconnect %s: %w", hostID, err)
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
		hostID:    hostID,
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
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		if err != nil {
			failCount++
			// 容忍 2 次瞬时失败，连续 3 次才判定断连（约 45s）
			if failCount >= 3 {
				p.mu.Lock()
				if entry, ok := p.conns[hostID]; ok && entry.client == client {
					delete(p.conns, hostID)
				}
				p.mu.Unlock()
				_ = client.Close()
				return
			}
		} else {
			failCount = 0
		}
	}
}
