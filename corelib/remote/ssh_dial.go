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
// 优先级：用户指定的 AuthMethod 排第一，其他可用方法作为 fallback。
// 这样即使首选方式失败，SSH 库会自动尝试下一个。
func buildAuthMethods(cfg SSHHostConfig) ([]ssh.AuthMethod, error) {
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

// buildKeyAuth 构建密钥认证方法。
func buildKeyAuth(cfg SSHHostConfig) (ssh.AuthMethod, error) {
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
	return ssh.PublicKeys(signer), nil
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
