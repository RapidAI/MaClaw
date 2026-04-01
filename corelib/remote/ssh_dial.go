package remote

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
)

// dialSSH 根据 SSHHostConfig 建立 SSH 连接。
// 使用 TCP keepalive 防止空闲连接被中间网络设备（NAT/防火墙）断开。
func dialSSH(cfg SSHHostConfig) (*ssh.Client, error) {
	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, err
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: 生产环境应使用 known_hosts 校验
		Timeout:         cfg.ConnectTimeout,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

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

// buildAuthMethods 根据配置构建 SSH 认证方法列表。
func buildAuthMethods(cfg SSHHostConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	switch cfg.AuthMethod {
	case "password":
		if cfg.Password == "" {
			return nil, fmt.Errorf("password auth requires password")
		}
		methods = append(methods, ssh.Password(cfg.Password))

	case "agent":
		authMethod, err := sshAgentAuth()
		if err != nil {
			return nil, err
		}
		methods = append(methods, authMethod)

	case "key", "":
		keyPath := cfg.KeyPath
		if keyPath == "" {
			home, _ := os.UserHomeDir()
			keyPath = home + "/.ssh/id_rsa"
		}
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read ssh key %s: %w", keyPath, err)
		}
		var signer ssh.Signer
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyData)
		}
		if err != nil {
			return nil, fmt.Errorf("parse ssh key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))

	default:
		return nil, fmt.Errorf("unsupported auth method: %s", cfg.AuthMethod)
	}

	return methods, nil
}

// sshAgentAuth 连接 ssh-agent 并返回认证方法。
// Windows 上使用 named pipe，Unix 上使用 SSH_AUTH_SOCK。
func sshAgentAuth() (ssh.AuthMethod, error) {
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

	agentClient := sshagent.NewClient(agentConn)
	// 使用 PublicKeysCallback 而非直接获取 signers，
	// 这样 agentConn 的生命周期跟随 ssh.Client 连接。
	return ssh.PublicKeysCallback(agentClient.Signers), nil
}
