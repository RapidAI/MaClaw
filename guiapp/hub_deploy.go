package guiapp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// A Hub installed from the tools page. The SSH password is not stored.
// AdminPassword is the initial management-console password created on that
// machine; the person who started the install needs it to log in.

const (
	hubDeployFile   = "hub_deploys.json"
	hubDeployEvent  = "hub-deploys-changed"
	hubDeployListen = 9399

	hubDeployStatusDeploying   = "deploying"
	hubDeployStatusRegistering = "registering"
	hubDeployStatusReady       = "ready"
	hubDeployStatusFailed      = "failed"
)

var hubDeployEmail = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// HubDeployRequest is the tools-page form. Email is the Hub Center owner.
type HubDeployRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// HubDeployRecord is one Hub this GUI asked an agent to install.
type HubDeployRecord struct {
	ID            string `json:"id"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	User          string `json:"user"`
	HostKey       string `json:"host_key,omitempty"`
	Email         string `json:"email"`
	PublicURL     string `json:"public_url"`
	CenterURLs    string `json:"center_urls"`
	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password,omitempty"`
	HubID         string `json:"hub_id,omitempty"`
	Pending       bool   `json:"pending,omitempty"`
	Status        string `json:"status"`
	Message       string `json:"message,omitempty"`
	TaskPath      string `json:"task_path,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	JobStartedAt  string `json:"job_started_at,omitempty"`
}

var hubDeployMu sync.Mutex

// One install per record. The claim is taken before the record is saved, so a
// list that runs in between cannot treat a live install as a crashed one.
var hubDeployJobs sync.Map

func claimHubDeployJob(id string) bool {
	_, loaded := hubDeployJobs.LoadOrStore(id, struct{}{})
	return !loaded
}

func releaseHubDeployJob(id string) { hubDeployJobs.Delete(id) }

func hubDeployJobLive(id string) bool {
	_, ok := hubDeployJobs.Load(id)
	return ok
}

func (a *App) hubDeployPath() string {
	dir := ""
	if a != nil {
		dir = a.GetDataDir()
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".maclaw")
	}
	return filepath.Join(dir, hubDeployFile)
}

func (a *App) loadHubDeploys() ([]HubDeployRecord, error) {
	raw, err := os.ReadFile(a.hubDeployPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []HubDeployRecord
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *App) saveHubDeploys(records []HubDeployRecord) error {
	if records == nil {
		records = []HubDeployRecord{}
	}
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	path := a.hubDeployPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ListHubDeploys returns installs started on this computer. The SSH password
// is not among the fields.
func (a *App) ListHubDeploys() ([]HubDeployRecord, error) {
	hubDeployMu.Lock()
	records, err := a.loadHubDeploys()
	if err != nil {
		hubDeployMu.Unlock()
		return nil, err
	}
	records, repaired := settleHubDeployJobs(records)
	if repaired {
		err = a.saveHubDeploys(records)
	}
	hubDeployMu.Unlock()
	if err != nil {
		return nil, err
	}
	if repaired {
		a.emitEvent(hubDeployEvent, "")
	}
	return records, nil
}

// ClearHubDeployResult drops one finished install from the list. A running
// install stays.
func (a *App) ClearHubDeployResult(id string) error {
	hubDeployMu.Lock()
	defer hubDeployMu.Unlock()
	records, err := a.loadHubDeploys()
	if err != nil {
		return err
	}
	next := records[:0]
	for _, rec := range records {
		if rec.ID == id && !hubDeployBusy(rec.Status) {
			continue
		}
		next = append(next, rec)
	}
	if err := a.saveHubDeploys(next); err != nil {
		return err
	}
	a.emitEvent(hubDeployEvent, "")
	return nil
}

func hubDeployBusy(status string) bool {
	return status == hubDeployStatusDeploying || status == hubDeployStatusRegistering
}

// settleHubDeployJobs marks a busy record failed when this process has no
// install for it. A crash or a killed app leaves that record behind, and the
// tools page then refuses another install on the same machine.
func settleHubDeployJobs(records []HubDeployRecord) ([]HubDeployRecord, bool) {
	changed := false
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range records {
		if hubDeployBusy(records[i].Status) && !hubDeployJobLive(records[i].ID) {
			records[i].Status = hubDeployStatusFailed
			records[i].Message = "上次部署已中断，请重新开始"
			records[i].JobStartedAt = ""
			records[i].UpdatedAt = now
			changed = true
		}
	}
	return records, changed
}

func (a *App) listLocalHubDeployJobs() []LocalDesktopJob {
	hubDeployMu.Lock()
	records, err := a.loadHubDeploys()
	repaired := false
	if err == nil {
		records, repaired = settleHubDeployJobs(records)
		if repaired {
			err = a.saveHubDeploys(records)
		}
	}
	hubDeployMu.Unlock()
	if err != nil {
		return nil
	}
	if repaired {
		a.emitEvent(hubDeployEvent, "")
	}
	out := make([]LocalDesktopJob, 0)
	for _, rec := range records {
		if !hubDeployBusy(rec.Status) {
			continue
		}
		started := rec.JobStartedAt
		if started == "" {
			started = rec.UpdatedAt
		}
		out = append(out, LocalDesktopJob{
			ID:        rec.ID,
			Title:     "部署 Hub · " + rec.User + "@" + rec.Host,
			Status:    rec.Status,
			StartedAt: started,
		})
	}
	return out
}

// DeployHub sends the assistant agent to install a Hub from source and lets
// that Hub register itself with Hub Center.
func (a *App) DeployHub(req HubDeployRequest) (*HubDeployRecord, error) {
	req.Host = strings.TrimSpace(req.Host)
	req.User = strings.TrimSpace(req.User)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Port <= 0 {
		req.Port = 22
	}
	if req.Host == "" || req.User == "" || req.Password == "" || req.Email == "" {
		return nil, fmt.Errorf("请填写 IP、用户名、密码和注册邮箱")
	}
	if !hubDeployEmail.MatchString(req.Email) {
		return nil, fmt.Errorf("注册邮箱无效")
	}
	if req.Port > 65535 || strings.ContainsAny(req.Host, " /@\\") || strings.ContainsAny(req.User, " :/@\\'\"") {
		return nil, fmt.Errorf("SSH 地址或用户名无效")
	}
	docHub, _, hubErr := a.getHubCredentials()
	docHub = strings.TrimRight(strings.TrimSpace(docHub), "/")
	if docHub == "" {
		if hubErr != nil {
			return nil, fmt.Errorf("还没有连接 Hub，读不到安装文档: %v", hubErr)
		}
		return nil, fmt.Errorf("还没有连接 Hub，读不到安装文档")
	}
	conn, err := dialDesktopNodeSSH(req.Host, req.Port, req.User, req.Password, "")
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %v", err)
	}
	privilege, privErr := conn.ensurePrivilege()
	hostKey := conn.hostKey
	if privErr == nil {
		// A previous install leaves 200 in this file. The probe would treat
		// that as this run's admin setup and finish before the new password exists.
		forgetCtx, forgetCancel := context.WithTimeout(context.Background(), 20*time.Second)
		_, _ = conn.run(forgetCtx, "rm -f /opt/maclaw-hub/data/admin-setup.result", privilege, req.Password)
		forgetCancel()
	}
	conn.Close()
	if privErr != nil {
		return nil, privErr
	}
	adminPassword, err := hubDeployAdminPassword()
	if err != nil {
		return nil, err
	}
	idRaw := make([]byte, 4)
	if _, err := rand.Read(idRaw); err != nil {
		return nil, err
	}
	centers := hubDeployCenterURLs(a)
	rec := HubDeployRecord{
		ID:            "hd_" + hex.EncodeToString(idRaw),
		Host:          req.Host,
		Port:          req.Port,
		User:          req.User,
		HostKey:       hostKey,
		Email:         req.Email,
		PublicURL:     hubDeployPublicURL(req.Host),
		CenterURLs:    strings.Join(centers, "\n"),
		AdminUser:     "admin",
		AdminPassword: adminPassword,
		Status:        hubDeployStatusDeploying,
		Message:       "agent 正在安装 Hub 并注册到 Hub Center",
		JobStartedAt:  time.Now().UTC().Format(time.RFC3339),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if !claimHubDeployJob(rec.ID) {
		return nil, fmt.Errorf("这台机器正在部署 Hub")
	}
	hubDeployMu.Lock()
	records, err := a.loadHubDeploys()
	if err != nil {
		hubDeployMu.Unlock()
		releaseHubDeployJob(rec.ID)
		return nil, err
	}
	records, repaired := settleHubDeployJobs(records)
	for _, old := range records {
		if old.Host == rec.Host && old.Port == rec.Port && hubDeployBusy(old.Status) {
			if repaired {
				if err := a.saveHubDeploys(records); err != nil {
					hubDeployMu.Unlock()
					releaseHubDeployJob(rec.ID)
					return nil, err
				}
			}
			hubDeployMu.Unlock()
			releaseHubDeployJob(rec.ID)
			if repaired {
				a.emitEvent(hubDeployEvent, "")
			}
			return nil, fmt.Errorf("这台机器正在部署 Hub")
		}
	}
	records = append(records, rec)
	err = a.saveHubDeploys(records)
	hubDeployMu.Unlock()
	if err != nil {
		releaseHubDeployJob(rec.ID)
		return nil, err
	}
	a.emitEvent(hubDeployEvent, "")
	go a.runHubDeploy(rec, req.Password, privilege, docHub)
	out := rec
	return &out, nil
}

func hubDeployAdminPassword() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hubDeployPublicURL(host string) string {
	return "http://" + net.JoinHostPort(host, fmt.Sprintf("%d", hubDeployListen))
}

func hubDeployCenterURLs(a *App) []string {
	if a != nil {
		if cfg, err := a.loadConfigSnapshot(); err == nil {
			seeds := cfg.HubCenterBaseURLs(remote.DefaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs)
			if public := hubDeployPublicCenterURLs(seeds); len(public) > 0 {
				return public
			}
		}
	}
	return append([]string(nil), remote.DefaultRemoteHubCenterURLs...)
}

// hubDeployPublicCenterURLs keeps the desktop's enrollment centers. A loopback
// address is this computer, not a center the new Hub can reach.
func hubDeployPublicCenterURLs(seeds []string) []string {
	out := make([]string, 0, len(seeds))
	seen := make(map[string]struct{}, len(seeds))
	for _, value := range seeds {
		value = strings.TrimSpace(value)
		if value == "" || hubDeployLoopbackCenter(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func hubDeployLoopbackCenter(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

func (a *App) runHubDeploy(rec HubDeployRecord, password string, privilege desktopNodePrivilege, docHub string) {
	defer releaseHubDeployJob(rec.ID)
	label := "maclaw-hub-" + strings.TrimPrefix(rec.ID, "hd_")
	registerEphemeralSSHHost(corelib.SSHHostEntry{
		Label: label, Host: rec.Host, Port: rec.Port, User: rec.User,
		AuthMethod: "password", Password: password,
	})
	defer unregisterEphemeralSSHHost(label)
	display := fmt.Sprintf("〔Hub 部署〕在 %s@%s 上安装 Hub 并注册", rec.User, rec.Host)
	_, err := RunOpsAgent(a, OpsAgentJob{
		Name:    "部署 Hub · " + rec.User + "@" + rec.Host,
		Display: display,
		Prompt:  hubDeployPrompt(rec, password, privilege, docHub),
		Marker:  "HUB_INSTALL",
		Budget:  90 * time.Minute,
		Every:   45 * time.Second,
		Log:     func(step, detail string) { a.hubDeployMessage(rec.ID, step+": "+detail) },
		OnTask: func(projectPath string) {
			a.updateHubDeploy(rec.ID, func(r *HubDeployRecord) { r.TaskPath = projectPath })
		},
		OnWaiting: func(string) {
			a.updateHubDeploy(rec.ID, func(r *HubDeployRecord) {
				r.Status = hubDeployStatusRegistering
				r.Message = "agent 已结束，等待 Hub 注册"
			})
		},
		Probe: func(agentDone bool, verdict string) error {
			if agentDone && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(verdict)), "FAILED") {
				return errors.New(strings.TrimSpace(verdict))
			}
			err := a.probeHubDeploy(rec, password, privilege, agentDone)
			// Register runs once at process start. A last_error written while
			// the agent is still working can be cleared by a restart. After
			// the agent finishes, that error is the result.
			if err != nil && !agentDone && strings.HasPrefix(err.Error(), "注册失败") {
				return fmt.Errorf("Hub 正在注册: %s", err.Error())
			}
			return err
		},
		Fatal: func(err error) bool {
			return err != nil && !strings.Contains(err.Error(), "正在")
		},
	})
	if err != nil {
		a.updateHubDeploy(rec.ID, func(r *HubDeployRecord) {
			r.Status = hubDeployStatusFailed
			r.Message = OpsScrub(err.Error())
			r.JobStartedAt = ""
		})
		return
	}
	a.updateHubDeploy(rec.ID, func(r *HubDeployRecord) {
		r.Status = hubDeployStatusReady
		r.Message = hubDeployReadyMessage(r.Pending, r.AdminPassword)
		r.JobStartedAt = ""
	})
}

func hubDeployReadyMessage(pending bool, adminPassword string) string {
	message := "Hub 已安装并完成注册"
	if pending {
		message = "已提交 Hub Center，等待确认"
	}
	if strings.TrimSpace(adminPassword) == "" {
		message += "。管理台账号之前已经创建，请用当时的密码登录"
	}
	return message
}

func (a *App) probeHubDeploy(rec HubDeployRecord, password string, privilege desktopNodePrivilege, agentDone bool) error {
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return fmt.Errorf("Hub 正在安装，SSH 暂不可用")
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:9399/healthz", nil)
	if err != nil {
		return fmt.Errorf("Hub 正在启动")
	}
	resp, err := conn.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("Hub 正在启动")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok":true`) || !strings.Contains(string(body), `"service":"maclaw-hub"`) {
		return fmt.Errorf("Hub 正在启动")
	}
	out, _ := conn.run(ctx, hubDeployRegisterCommand(), privilege, password)
	hubID, pending, parsedErr := hubDeployRegistrationResult(lastLine(out))
	if parsedErr != nil {
		return parsedErr
	}
	// Registration finishes before the admin account exists. Ending the job
	// there publishes a password that cannot log in yet, and a re-run gets
	// 409 while still showing the new password.
	adminCtx, adminCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer adminCancel()
	adminOut, _ := conn.run(adminCtx, hubDeployAdminResultCommand(), privilege, password)
	started, _ := time.Parse(time.RFC3339, rec.JobStartedAt)
	adminCode := hubDeployAdminCodeFresh(lastLine(adminOut), rec.ID, started)
	if adminCode == "" && !agentDone {
		a.updateHubDeploy(rec.ID, func(r *HubDeployRecord) {
			r.HubID = hubID
			r.Pending = pending
		})
		return fmt.Errorf("Hub 正在完成安装")
	}
	a.updateHubDeploy(rec.ID, func(r *HubDeployRecord) {
		r.HubID = hubID
		r.Pending = pending
		if adminCode == "409" {
			r.AdminPassword = ""
		}
	})
	return nil
}

// hubDeployRegistrationResult reads the one-line JSON printed by the probe.
// A disabled hub is final. A last_error without hub_id can still be cleared
// by restarting the process, so the caller treats that as retryable until
// the agent has finished.
func hubDeployRegistrationResult(line string) (string, bool, error) {
	var parsed struct {
		Registered bool   `json:"registered"`
		Pending    bool   `json:"pending"`
		Disabled   bool   `json:"disabled"`
		HubID      string `json:"hub_id"`
		LastError  string `json:"last_error"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(line)), &parsed) != nil {
		return "", false, fmt.Errorf("Hub 正在注册")
	}
	if parsed.Disabled {
		msg := strings.TrimSpace(parsed.LastError)
		if msg == "" {
			msg = "hub has been disabled by Hub Center"
		}
		return "", false, fmt.Errorf("Hub 已被停用: %s", msg)
	}
	if parsed.HubID != "" && (parsed.Registered || parsed.Pending) {
		return parsed.HubID, parsed.Pending && !parsed.Registered, nil
	}
	if strings.TrimSpace(parsed.LastError) != "" {
		return "", false, fmt.Errorf("注册失败: %s", parsed.LastError)
	}
	return "", false, fmt.Errorf("Hub 正在注册")
}

func hubDeployAdminCodeFresh(line, mark string, started time.Time) string {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) >= 3 && mark != "" && fields[2] == mark && (fields[1] == "200" || fields[1] == "409") {
		return fields[1]
	}
	if len(fields) != 2 || (fields[1] != "200" && fields[1] != "409") {
		return ""
	}
	sec, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return ""
	}
	if !started.IsZero() && sec < started.Unix() {
		return ""
	}
	return fields[1]
}

func hubDeployAdminResultCommand() string {
	script := `import os
path="/opt/maclaw-hub/data/admin-setup.result"
if not os.path.isfile(path):
    print("0 missing")
    raise SystemExit(0)
st=os.stat(path)
raw=open(path).read(80).strip().splitlines()
code=raw[0] if raw else "missing"
print("%d %s" % (int(st.st_mtime), code))
`
	return `python3 -c "import base64; exec(base64.b64decode('` + base64.StdEncoding.EncodeToString([]byte(script)) + `'))"`
}

func hubDeployRegisterCommand() string {
	script := `import json,os,sqlite3,sys
path="/opt/maclaw-hub/data/maclaw-hub.db"
if not os.path.isfile(path):
    sys.exit(1)
con=sqlite3.connect(path, timeout=5)
con.execute("pragma query_only=ON")
row=con.execute("select value_json from system_settings where key='center_registration'").fetchone()
if not row:
    sys.exit(1)
d=json.loads(row[0])
hub=d.get("hub_id") or ""
disabled=bool(d.get("disabled"))
registered=bool(d.get("registered"))
pending=bool(d.get("pending_confirmation"))
print(json.dumps({"registered":registered,"pending":pending,"disabled":disabled,"hub_id":hub,"last_error":d.get("last_error") or ""}))
sys.exit(0 if (not disabled and hub and (registered or pending)) else 1)
`
	return `python3 -c "import base64; exec(base64.b64decode('` + base64.StdEncoding.EncodeToString([]byte(script)) + `'))"`
}

func (a *App) updateHubDeploy(id string, fn func(*HubDeployRecord)) {
	hubDeployMu.Lock()
	defer hubDeployMu.Unlock()
	records, err := a.loadHubDeploys()
	if err != nil {
		return
	}
	for i := range records {
		if records[i].ID != id {
			continue
		}
		fn(&records[i])
		records[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = a.saveHubDeploys(records)
		a.emitEvent(hubDeployEvent, "")
		return
	}
}

func (a *App) hubDeployMessage(id, message string) {
	message = OpsScrub(strings.TrimSpace(message))
	if message == "" {
		return
	}
	a.updateHubDeploy(id, func(r *HubDeployRecord) { r.Message = message })
}

func hubDeployPrompt(rec HubDeployRecord, password string, privilege desktopNodePrivilege, docHub string) string {
	var b strings.Builder
	b.WriteString("用 ssh 登录下面这台 Linux 服务器，按安装文档安装 Hub，并让 Hub 自己向 Hub Center 注册。\n\n")
	fmt.Fprintf(&b, "服务器：\n- 地址：%s\n- 端口：%d\n- 用户：%s\n- 密码：%s\n- 提权：%s\n\n",
		rec.Host, rec.Port, rec.User, password, desktopNodePrivilegeLine(privilege, "部署"))
	fmt.Fprintf(&b, "安装文档：%s/docs/hub-install\n", docHub)
	fmt.Fprintf(&b, "HUB_PUBLIC_URL=%s\n", rec.PublicURL)
	fmt.Fprintf(&b, "OWNER_EMAIL=%s\n", rec.Email)
	fmt.Fprintf(&b, "ADMIN_USER=%s\n", rec.AdminUser)
	fmt.Fprintf(&b, "ADMIN_PASSWORD=%s\n", rec.AdminPassword)
	fmt.Fprintf(&b, "ADMIN_SETUP_MARK=%s\n", rec.ID)
	urls := make([]string, 0)
	for _, line := range strings.Split(rec.CenterURLs, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			urls = append(urls, line)
		}
	}
	fmt.Fprintf(&b, "CENTER_URLS='%s'\n", strings.Join(urls, " "))
	b.WriteString("\n文档里的这些变量用上面的值，不要用文档里的示例地址。CENTER_URLS 里多个地址用空格隔开，写配置时每个地址单独一条。不要在回复里复述密码或 hub_secret。\n")
	b.WriteString("完成后最后一行输出 `HUB_INSTALL: OK`；失败则输出 `HUB_INSTALL: FAILED <原因>`。\n")
	return b.String()
}
