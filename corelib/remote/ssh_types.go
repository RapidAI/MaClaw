package remote

import (
	"strconv"
	"time"
)

// SSHHostConfig 描述一个 SSH 远程主机的连接配置。
type SSHHostConfig struct {
	// Host 是远程主机地址（IP 或域名）。
	Host string `json:"host"`
	// Port 是 SSH 端口，默认 22。
	Port int `json:"port,omitempty"`
	// User 是登录用户名。
	User string `json:"user"`
	// AuthMethod 认证方式: "key", "password", "agent"。
	AuthMethod string `json:"auth_method"`
	// KeyPath 私钥文件路径（auth_method=key 时使用）。
	KeyPath string `json:"key_path,omitempty"`
	// Password 密码（auth_method=password 时使用，不建议明文存储）。
	Password string `json:"password,omitempty"`
	// Passphrase 私钥密码（可选）。
	Passphrase string `json:"passphrase,omitempty"`
	// Label 用户可读的主机标签，如 "prod-web-01"。
	Label string `json:"label,omitempty"`
	// KnownHostsPath 可选的 known_hosts 文件路径。
	KnownHostsPath string `json:"known_hosts_path,omitempty"`
	// ConnectTimeout 连接超时，默认 10s。
	ConnectTimeout time.Duration `json:"connect_timeout,omitempty"`
	// KeepaliveInterval 心跳间隔，默认 15s。
	KeepaliveInterval time.Duration `json:"keepalive_interval,omitempty"`
}

// SSHHostID 返回用于连接池索引的唯一标识。
func (c SSHHostConfig) SSHHostID() string {
	port := c.Port
	if port == 0 {
		port = 22
	}
	return c.User + "@" + c.Host + ":" + strconv.Itoa(port)
}

// Defaults 填充默认值。
func (c *SSHHostConfig) Defaults() {
	if c.Port == 0 {
		c.Port = 22
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 10 * time.Second
	}
	if c.KeepaliveInterval == 0 {
		c.KeepaliveInterval = 15 * time.Second
	}
	if c.AuthMethod == "" {
		c.AuthMethod = "key"
	}
}

// SSHSessionSpec 描述在远程主机上启动一个 SSH 交互会话的参数。
type SSHSessionSpec struct {
	// HostConfig 目标主机配置。
	HostConfig SSHHostConfig `json:"host_config"`
	// InitialCommand 连接后立即执行的命令（可选，如 "cd /app && bash"）。
	InitialCommand string `json:"initial_command,omitempty"`
	// Env 额外环境变量。
	Env map[string]string `json:"env,omitempty"`
	// Cols 终端列数。
	Cols int `json:"cols,omitempty"`
	// Rows 终端行数。
	Rows int `json:"rows,omitempty"`
	// SessionID 由调用方指定或自动生成。
	SessionID string `json:"session_id,omitempty"`
}

// SSHSessionSummary 是 SSH 会话的摘要信息，用于 UI 展示。
type SSHSessionSummary struct {
	SessionID  string `json:"session_id"`
	HostID     string `json:"host_id"`
	HostLabel  string `json:"host_label"`
	Status     string `json:"status"`
	LastOutput string `json:"last_output,omitempty"`
	UpdatedAt  int64  `json:"updated_at"`
}

