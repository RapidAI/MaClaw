package remote

import (
	"net"
	"strconv"
	"strings"
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
	// KnownHostsFiles 是 OpenSSH 会查阅的全部 known_hosts。非空时覆盖
	// KnownHostsPath，避免只命中系统文件而漏掉用户自己的主机记录。
	KnownHostsFiles []string `json:"known_hosts_files,omitempty"`
	// HostKeyFingerprint pins one SHA256 host-key fingerprint for callers that
	// maintain a dedicated trust store.
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	// HostKeyFingerprintCapture receives the fingerprint presented by a server
	// during this connection's SSH handshake. It is runtime-only and lets a
	// caller establish a first-use pin without a second, unbound connection.
	HostKeyFingerprintCapture func(string) `json:"-"`
	// HostAlias is the OpenSSH config name when Host was rewritten from an alias
	// such as gpu84 to the config HostName. Host-key checks try both names.
	HostAlias string `json:"host_alias,omitempty"`
	// JumpHosts are dialed in order before Host. ApplyOpenSSHClientConfig fills
	// this from ProxyJump so the session can be reconnected without reading
	// ssh config again.
	JumpHosts []SSHHostConfig `json:"jump_hosts,omitempty"`
	// IdentityFiles are OpenSSH identity files in the order the client offers them.
	IdentityFiles []string `json:"identity_files,omitempty"`
	// CertificateFiles are OpenSSH certificate files. A sibling "<key>-cert.pub"
	// is also tried when it exists.
	CertificateFiles []string `json:"certificate_files,omitempty"`
	// PreferAgent adds ssh-agent after the configured identity file. The file is
	// offered first so unrelated agent keys do not exhaust the server's auth
	// attempts. The agent still supplies a certificate that has not been written
	// beside the key yet.
	PreferAgent bool `json:"prefer_agent,omitempty"`
	// OpenSSHChecked marks that ApplyOpenSSHClientConfig already ran. Reconnect
	// and the pool must not resolve the alias a second time.
	OpenSSHChecked bool `json:"-"`
	// ConnectTimeout 连接超时，默认 10s。
	ConnectTimeout time.Duration `json:"connect_timeout,omitempty"`
	// KeepaliveInterval 心跳间隔，默认 15s（更快检测 NAT/防火墙断连与半开连接）。
	KeepaliveInterval time.Duration `json:"keepalive_interval,omitempty"`
}

// normalizeEndpoint trims the login and writes one IP spelling.
// Bracketed and expanded IPv6 are the same host. A hostname is only trimmed:
// its letters are the name the user typed and the OpenSSH alias that is looked up.
func (c *SSHHostConfig) normalizeEndpoint() {
	if c == nil {
		return
	}
	c.Host = canonicalSSHHost(c.Host)
	c.User = strings.TrimSpace(c.User)
	c.Label = strings.TrimSpace(c.Label)
}

// canonicalSSHHost unwraps a bracketed IP and returns its canonical form.
// Anything that is not an IP is returned trimmed, brackets included.
func canonicalSSHHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	inner := host
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		inner = strings.TrimSpace(host[1 : len(host)-1])
	}
	if ip := net.ParseIP(inner); ip != nil {
		return ip.String()
	}
	return host
}

// SSHHostID 返回用于连接池索引的唯一标识。
// The id is the normalized endpoint, so a connect that has not called Defaults
// still names the same session as one that has.
func (c SSHHostConfig) SSHHostID() string {
	c.normalizeEndpoint()
	port := c.Port
	if port == 0 {
		port = 22
	}
	return c.User + "@" + c.Host + ":" + strconv.Itoa(port)
}

// Defaults 填充默认值。
func (c *SSHHostConfig) Defaults() {
	c.normalizeEndpoint()
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
		if c.Password != "" {
			c.AuthMethod = "password"
		} else if c.KeyPath != "" {
			c.AuthMethod = "key"
		} else {
			// 都没指定时用 key 作为默认（与 OpenSSH 行为一致）；
			// buildAuthMethods 会自动 fallback 到 password/agent
			c.AuthMethod = "key"
		}
	}
}

// SSHSessionSpec 描述在远程主机上启动一个 SSH 交互会话的参数。
type SSHSessionSpec struct {
	// HostConfig 目标主机配置。
	HostConfig SSHHostConfig `json:"host_config"`
	// InitialCommand 是写入交互 shell 的第一行（如 "cd /app && bash"），
	// 不是替代 shell 的一次性进程；命令结束后会话仍然可 exec。
	InitialCommand string `json:"initial_command,omitempty"`
	// Env 额外环境变量。
	Env map[string]string `json:"env,omitempty"`
	// Cols 终端列数。
	Cols int `json:"cols,omitempty"`
	// Rows 终端行数。
	Rows int `json:"rows,omitempty"`
	// SessionID 由调用方指定或自动生成。
	SessionID string `json:"session_id,omitempty"`
	// OwnerID is the conversation principal that opened this session.
	// Binding, reuse, and situation listing select by this id.
	// Empty means the session was opened with no named principal, and a
	// named conversation does not inherit it.
	OwnerID string `json:"owner_id,omitempty"`
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
