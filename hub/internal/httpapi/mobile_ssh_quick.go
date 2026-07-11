package httpapi

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

type mobileSSHQuickConnectResult struct {
	ProfileID     string
	Label         string
	Host          string
	Port          int
	Username      string
	AssistantHint string
	Message       string
}

// MobileSSHQuickConnectHandler is the zero-management path for Mobile AI:
// user only provides host / username / password; Hub stores profile + vault
// so the assistant can enable the ssh tool without any further management UI.
//
//	POST /api/mobile/ssh/quick-connect
//	body: {host, username, password, port?, label?}
func MobileSSHQuickConnectHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var body struct {
			Host     string `json:"host"`
			Port     int    `json:"port"`
			Username string `json:"username"`
			Password string `json:"password"`
			Label    string `json:"label"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		result, err := mobileSSHQuickConnectStore(principal, body.Host, body.Username, body.Password, body.Port, body.Label)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"profile_id":     result.ProfileID,
			"label":          result.Label,
			"host":           result.Host,
			"port":           result.Port,
			"username":       result.Username,
			"has_secret":     true,
			"assistant_hint": result.AssistantHint,
			"message":        result.Message,
		})
	}
}

// mobileSSHQuickConnectStore registers profile + vault for hub_exec / AI ssh.
func mobileSSHQuickConnectStore(
	principal *auth.ViewerPrincipal,
	hostRaw, usernameRaw, passwordRaw string,
	port int,
	labelRaw string,
) (mobileSSHQuickConnectResult, error) {
	if principal == nil {
		return mobileSSHQuickConnectResult{}, fmt.Errorf("principal required")
	}
	host := sanitizeMobileServerProfileText(hostRaw, 255)
	username := sanitizeMobileServerProfileText(usernameRaw, 128)
	password := strings.TrimSpace(passwordRaw)
	if host == "" || username == "" || password == "" {
		return mobileSSHQuickConnectResult{}, fmt.Errorf("host, username and password are required")
	}
	// Strip scheme if user pasted a URL.
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimSpace(host)
	if port == 0 {
		// host:port form
		if h, p, err := net.SplitHostPort(host); err == nil {
			host = h
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		} else {
			port = 22
		}
	}
	if port < 1 || port > 65535 {
		return mobileSSHQuickConnectResult{}, fmt.Errorf("port must be 1-65535")
	}
	label := sanitizeMobileServerProfileText(labelRaw, 128)
	if label == "" {
		label = username + "@" + host
		if port != 22 {
			label = fmt.Sprintf("%s@%s:%d", username, host, port)
		}
	}
	profileID := mobileSSHQuickProfileID(host, port, username)
	ownerID := mobilePrincipalOwnerID(principal)
	tenantID := strings.TrimSpace(principal.TenantID)
	now := time.Now().UTC()

	profileKey := tenantID + "\x00" + ownerID + "\x00" + "mobile-quick" + "\x00" + profileID
	rec := mobileServerProfileRecord{
		ProfileID:       profileID,
		TenantID:        tenantID,
		OwnerID:         ownerID,
		SourceMachineID: "mobile-quick",
		Name:            label,
		Host:            host,
		Port:            port,
		Username:        username,
		AuthMode:        "password",
		Tag:             "quick",
		Note:            "mobile AI quick connect",
		UpdatedAt:       now,
	}
	mobileServerProfiles.Lock()
	mobileServerProfiles.profiles[profileKey] = rec
	mobileServerProfiles.Unlock()

	enc, err := mobileSSHVaultEncrypt(password)
	if err != nil {
		return mobileSSHQuickConnectResult{}, fmt.Errorf("failed to store password")
	}
	vaultKey := mobileSSHVaultMapKey(tenantID, ownerID, profileID)
	mobileSSHVault.Lock()
	mobileSSHVault.secrets[vaultKey] = mobileSSHVaultRecord{
		TenantID:        tenantID,
		OwnerID:         ownerID,
		ProfileID:       profileID,
		AuthMode:        "password",
		EncryptedSecret: enc,
		UpdatedAt:       now,
	}
	mobileSSHVault.Unlock()
	go mobilePersistState()

	return mobileSSHQuickConnectResult{
		ProfileID:     profileID,
		Label:         label,
		Host:          host,
		Port:          port,
		Username:      username,
		AssistantHint: fmt.Sprintf("SSH ready. Use ssh tool with label=%q (connect then exec). Do not ask the user for the password again.", label),
		Message:       "服务器已接入，可直接让 AI 助手操作，无需再管理档案。",
	}, nil
}

func mobileSSHQuickProfileID(host string, port int, username string) string {
	raw := strings.ToLower(strings.TrimSpace(host)) + "\x00" +
		fmt.Sprintf("%d", port) + "\x00" +
		strings.ToLower(strings.TrimSpace(username))
	sum := sha1.Sum([]byte(raw))
	return "q" + hex.EncodeToString(sum[:8])
}

// mobileParseQuickSSHFromText extracts host/user/password from free-form user text
// so chat like "查状态 www.example.com root secret" can enable ssh in the same turn.
func mobileParseQuickSSHFromText(text string) (host, user, pass string, port int, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", "", 0, false
	}
	// Prefer explicit user@host password
	// e.g. root@10.0.0.1 mypass  or root@host:22 pass
	reAt := regexp.MustCompile(`(?i)\b([a-zA-Z0-9._-]+)@([a-zA-Z0-9._-]+(?:\.[a-zA-Z0-9._-]+)*)(?::(\d{1,5}))?\s+(\S{4,})`)
	if m := reAt.FindStringSubmatch(text); len(m) == 5 {
		user = m[1]
		host = m[2]
		pass = m[4]
		port = 22
		if m[3] != "" {
			if n, err := strconv.Atoi(m[3]); err == nil {
				port = n
			}
		}
		if mobileLooksLikeHost(host) && mobileLooksLikeUser(user) && mobileLooksLikePassword(pass) {
			return host, user, pass, port, true
		}
	}

	// Labeled Chinese/English forms:
	// "主机 10.0.0.1 用户 root 密码 secret" / "host: a.com user: u pass: p"
	if h, u, p, pt, found := mobileParseLabeledSSHCreds(text); found {
		return h, u, p, pt, true
	}

	// Token scan: find host, then next user-like token (skip Chinese labels), then password.
	fields := strings.Fields(text)
	for i := 0; i < len(fields); i++ {
		h := strings.Trim(fields[i], "，。；;,:：\"'")
		if !mobileLooksLikeHost(h) {
			continue
		}
		port = 22
		if hh, pp, err := net.SplitHostPort(h); err == nil {
			h = hh
			if n, err := strconv.Atoi(pp); err == nil {
				port = n
			}
		}
		// Find user after host, skipping label junk (用户/密码/账号…).
		for j := i + 1; j < len(fields); j++ {
			u := strings.Trim(fields[j], "，。；;,:：\"'")
			if mobileIsSSHCredLabel(u) {
				continue
			}
			if !mobileLooksLikeUser(u) {
				continue
			}
			// Find password after user.
			for k := j + 1; k < len(fields); k++ {
				p := strings.Trim(fields[k], "，。；;,:：\"'")
				if mobileIsSSHCredLabel(p) {
					continue
				}
				if !mobileLooksLikePassword(p) {
					continue
				}
				// Prefer last non-label password-like token near user (common: … root MyPass123 请查).
				// Stop at first solid candidate after user.
				return h, u, p, port, true
			}
			break
		}
	}
	return "", "", "", 0, false
}

// mobileParseLabeledSSHCreds handles "IP/主机 … 用户/user … 密码/pass …" free text.
func mobileParseLabeledSSHCreds(text string) (host, user, pass string, port int, ok bool) {
	// Capture host after 主机/服务器/host/ip keywords.
	reHost := regexp.MustCompile(`(?i)(?:主机|服务器|地址|host|ip|hostname)\s*[:：=]?\s*([a-zA-Z0-9._:-]+)`)
	reUser := regexp.MustCompile(`(?i)(?:用户名?|账号|帐户|user(?:name)?|login)\s*[:：=]?\s*([a-zA-Z0-9._-]+)`)
	rePass := regexp.MustCompile(`(?i)(?:密码|口令|password|passwd|pwd)\s*[:：=]?\s*(\S{4,})`)
	hm := reHost.FindStringSubmatch(text)
	um := reUser.FindStringSubmatch(text)
	pm := rePass.FindStringSubmatch(text)
	if len(hm) < 2 || len(um) < 2 || len(pm) < 2 {
		return "", "", "", 0, false
	}
	host = strings.Trim(hm[1], "，。；;,\"'")
	user = strings.Trim(um[1], "，。；;,\"'")
	pass = strings.Trim(pm[1], "，。；;,\"'")
	port = 22
	if hh, pp, err := net.SplitHostPort(host); err == nil {
		host = hh
		if n, err := strconv.Atoi(pp); err == nil {
			port = n
		}
	}
	if !mobileLooksLikeHost(host) || !mobileLooksLikeUser(user) || !mobileLooksLikePassword(pass) {
		return "", "", "", 0, false
	}
	return host, user, pass, port, true
}

func mobileIsSSHCredLabel(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.Trim(s, "：:=")
	switch s {
	case "主机", "服务器", "地址", "host", "ip", "hostname",
		"用户", "用户名", "账号", "帐户", "user", "username", "login",
		"密码", "口令", "password", "passwd", "pwd",
		"端口", "port":
		return true
	}
	return false
}

func mobileLooksLikeHost(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	if s == "" || len(s) > 255 {
		return false
	}
	// Must look like domain or IP, not pure Chinese sentence.
	if net.ParseIP(s) != nil {
		return true
	}
	if strings.Contains(s, ".") {
		// domain-like
		for _, r := range s {
			if unicode.Is(unicode.Han, r) {
				return false
			}
		}
		parts := strings.Split(s, ".")
		if len(parts) < 2 {
			return false
		}
		return true
	}
	return false
}

func mobileLooksLikeUser(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.IsSpace(r) {
			return false
		}
	}
	// common junk
	switch strings.ToLower(s) {
	case "http", "https", "ssh", "ftp", "status", "server":
		return false
	}
	return true
}

func mobileLooksLikePassword(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 4 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// mobileRedactPasswordInText replaces password occurrences so they never enter the LLM prompt.
func mobileRedactPasswordInText(text, password string) string {
	password = strings.TrimSpace(password)
	if password == "" || text == "" {
		return text
	}
	return strings.ReplaceAll(text, password, "***")
}

// mobileMaybeAutoRegisterSSHFromUserText registers SSH from chat credentials in-process
// and returns redacted user text, label, password (for history redaction) if successful.
func mobileMaybeAutoRegisterSSHFromUserText(
	principal *auth.ViewerPrincipal,
	userText string,
) (redactedText string, label string, password string, registered bool) {
	host, user, pass, port, ok := mobileParseQuickSSHFromText(userText)
	if !ok {
		return userText, "", "", false
	}
	result, err := mobileSSHQuickConnectStore(principal, host, user, pass, port, "")
	if err != nil {
		// Still redact so the password does not enter the LLM prompt.
		return mobileRedactPasswordInText(userText, pass), "", pass, false
	}
	redacted := mobileRedactPasswordInText(userText, pass)
	// Nudge the model: credentials already stored; use label-based ssh.
	nudge := fmt.Sprintf(
		"\n\n[system: SSH credentials for %s were saved server-side. Password redacted. "+
			"You MUST use the ssh tool with label=%q — connect then exec. "+
			"Do not claim SSH is unavailable. Do not ask for the password again.]",
		result.Label, result.Label,
	)
	return redacted + nudge, result.Label, pass, true
}
