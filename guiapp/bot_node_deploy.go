package guiapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

// A private bot node installed from the tools page. It registers to the Hub
// this desktop is already connected to: after desktopd is healthy, botnode
// exchanges the admin's one-time code and writes node_id. The SSH password
// and the enroll code are not stored.

const (
	botNodeFile  = "bot_nodes.json"
	botNodeEvent = "bot-nodes-changed"

	botNodeStatusDeploying   = "deploying"
	botNodeStatusRegistering = "registering"
	botNodeStatusReady       = "ready"
	botNodeStatusFailed      = "failed"
)

// BotNodeDeployRequest is the tools-page form. EnrollCode is the 16-hex
// code an admin generates under Bot 节点. It is used once, after desktopd
// is healthy, to register this machine to the connected Hub.
type BotNodeDeployRequest struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password"`
	CanSudo    bool   `json:"can_sudo"`
	WithSrv    bool   `json:"with_srv"`
	EnrollCode string `json:"enroll_code"`
}

// BotNodeEnrollRequest retries registration with a new code. A code lasts
// three hours and is spent on the first enroll attempt. An expired or used
// code must not be retried.
type BotNodeEnrollRequest struct {
	ID         string `json:"id"`
	Password   string `json:"password"`
	EnrollCode string `json:"enroll_code"`
}

// BotNodeRecord is one private node this GUI asked an agent to install.
type BotNodeRecord struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	User         string `json:"user"`
	HostKey      string `json:"host_key,omitempty"`
	CanSudo      bool   `json:"can_sudo,omitempty"`
	WithSrv      bool   `json:"with_srv,omitempty"`
	HubURL       string `json:"hub_url,omitempty"`
	NodeID       string `json:"node_id,omitempty"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	TaskPath     string `json:"task_path,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	JobStartedAt string `json:"job_started_at,omitempty"`
}

var botNodeMu sync.Mutex

var botNodeJobs sync.Map

func claimBotNodeJob(id string) bool {
	_, loaded := botNodeJobs.LoadOrStore(id, struct{}{})
	return !loaded
}

func releaseBotNodeJob(id string) { botNodeJobs.Delete(id) }

func botNodeJobLive(id string) bool {
	_, ok := botNodeJobs.Load(id)
	return ok
}

func (a *App) botNodePath() string {
	dir := ""
	if a != nil {
		dir = a.GetDataDir()
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".maclaw")
	}
	return filepath.Join(dir, botNodeFile)
}

func (a *App) loadBotNodes() ([]BotNodeRecord, error) {
	raw, err := os.ReadFile(a.botNodePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var out []BotNodeRecord
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *App) saveBotNodes(records []BotNodeRecord) error {
	if records == nil {
		records = []BotNodeRecord{}
	}
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	path := a.botNodePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ListBotNodes returns private-node installs. A record left busy by a crash
// is marked failed so the same machine can be started again.
func (a *App) ListBotNodes() ([]BotNodeRecord, error) {
	botNodeMu.Lock()
	records, err := a.loadBotNodes()
	if err != nil {
		botNodeMu.Unlock()
		return nil, err
	}
	records, repaired := settleBotNodeJobs(records)
	if repaired {
		err = a.saveBotNodes(records)
	}
	botNodeMu.Unlock()
	if err != nil {
		return nil, err
	}
	if repaired {
		a.emitEvent(botNodeEvent, "")
	}
	return records, nil
}

// ClearBotNodeResult drops one finished install from the list.
func (a *App) ClearBotNodeResult(id string) error {
	botNodeMu.Lock()
	records, err := a.loadBotNodes()
	if err != nil {
		botNodeMu.Unlock()
		return err
	}
	next := records[:0]
	for _, rec := range records {
		if rec.ID == id && !botNodeBusy(rec.Status) {
			continue
		}
		next = append(next, rec)
	}
	err = a.saveBotNodes(next)
	botNodeMu.Unlock()
	if err != nil {
		return err
	}
	a.emitEvent(botNodeEvent, "")
	return nil
}

func botNodeBusy(status string) bool {
	return status == botNodeStatusDeploying || status == botNodeStatusRegistering
}

func botNodeHostBusy(records []BotNodeRecord, exceptID, host string, port int) bool {
	for _, old := range records {
		if old.ID != exceptID && old.Host == host && old.Port == port && botNodeBusy(old.Status) {
			return true
		}
	}
	return false
}

func settleBotNodeJobs(records []BotNodeRecord) ([]BotNodeRecord, bool) {
	changed := false
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range records {
		if botNodeBusy(records[i].Status) && !botNodeJobLive(records[i].ID) {
			records[i].Status = botNodeStatusFailed
			records[i].Message = "上次部署已中断，请重新开始"
			records[i].JobStartedAt = ""
			records[i].UpdatedAt = now
			changed = true
		}
	}
	return records, changed
}

func (a *App) listLocalBotNodeJobs() []LocalDesktopJob {
	botNodeMu.Lock()
	records, err := a.loadBotNodes()
	repaired := false
	if err == nil {
		records, repaired = settleBotNodeJobs(records)
		if repaired {
			err = a.saveBotNodes(records)
		}
	}
	botNodeMu.Unlock()
	if err != nil {
		return nil
	}
	if repaired {
		a.emitEvent(botNodeEvent, "")
	}
	out := make([]LocalDesktopJob, 0)
	for _, rec := range records {
		if !botNodeBusy(rec.Status) {
			continue
		}
		started := rec.JobStartedAt
		if started == "" {
			started = rec.UpdatedAt
		}
		out = append(out, LocalDesktopJob{
			ID:        rec.ID,
			Title:     "部署 Bot 节点 · " + rec.User + "@" + rec.Host,
			Status:    rec.Status,
			StartedAt: started,
		})
	}
	return out
}

// DeployBotNode installs desktopd (and optionally MaClawSrv) and registers
// the machine to the connected Hub with the admin's enroll code.
func (a *App) DeployBotNode(req BotNodeDeployRequest) (*BotNodeRecord, error) {
	req.Host = strings.TrimSpace(req.Host)
	req.User = strings.TrimSpace(req.User)
	req.Name = strings.TrimSpace(req.Name)
	if req.Port <= 0 {
		req.Port = 22
	}
	code, err := botNodeEnrollCode(req.EnrollCode)
	if err != nil {
		return nil, err
	}
	if req.Host == "" || req.User == "" || req.Password == "" {
		return nil, fmt.Errorf("请填写 IP、用户名、密码和注册码")
	}
	if req.Port > 65535 || strings.ContainsAny(req.Host, " /@\\") || strings.ContainsAny(req.User, " :/@\\'\"") {
		return nil, fmt.Errorf("SSH 地址或用户名无效")
	}
	if utf8.RuneCountInString(req.Name) > 80 {
		return nil, fmt.Errorf("名称不能超过 80 个字")
	}
	if req.Name == "" {
		req.Name = req.User + "@" + req.Host
	}
	docHub, err := a.botNodeHubURL()
	if err != nil {
		return nil, err
	}
	conn, err := dialDesktopNodeSSH(req.Host, req.Port, req.User, req.Password, "")
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %v", err)
	}
	privilege, privErr := chooseBotNodePrivilege(conn, req.CanSudo)
	hostKey := conn.hostKey
	conn.Close()
	if privErr != nil {
		return nil, privErr
	}
	idRaw := make([]byte, 4)
	if _, err := rand.Read(idRaw); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rec := BotNodeRecord{
		ID:           "bn_" + hex.EncodeToString(idRaw),
		Name:         req.Name,
		Host:         req.Host,
		Port:         req.Port,
		User:         req.User,
		HostKey:      hostKey,
		CanSudo:      req.CanSudo,
		WithSrv:      req.WithSrv,
		HubURL:       docHub,
		Status:       botNodeStatusDeploying,
		Message:      "agent 正在安装，desktopd 健康后再向 Hub 注册",
		JobStartedAt: now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := a.insertBotNode(rec); err != nil {
		return nil, err
	}
	go a.runBotNode(rec, req.Password, privilege, code, false)
	out := rec
	return &out, nil
}

// RegisterBotNode sends a new enroll code after desktopd is already installed.
// The previous code is not reused.
func (a *App) RegisterBotNode(req BotNodeEnrollRequest) (*BotNodeRecord, error) {
	code, err := botNodeEnrollCode(req.EnrollCode)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Password) == "" {
		return nil, fmt.Errorf("请填写 SSH 密码和新的注册码")
	}
	docHub, err := a.botNodeHubURL()
	if err != nil {
		return nil, err
	}
	botNodeMu.Lock()
	records, err := a.loadBotNodes()
	if err != nil {
		botNodeMu.Unlock()
		return nil, err
	}
	var rec BotNodeRecord
	found := false
	for _, old := range records {
		if old.ID == strings.TrimSpace(req.ID) {
			rec = old
			found = true
			break
		}
	}
	botNodeMu.Unlock()
	if !found {
		return nil, fmt.Errorf("找不到这台 Bot 节点")
	}
	if rec.NodeID != "" {
		return nil, fmt.Errorf("这台机器已经注册到 Hub")
	}
	if botNodeBusy(rec.Status) && botNodeJobLive(rec.ID) {
		return nil, fmt.Errorf("这台机器正在安装 Bot 节点")
	}
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, req.Password, rec.HostKey)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %v", err)
	}
	privilege, privErr := chooseBotNodePrivilege(conn, rec.CanSudo)
	conn.Close()
	if privErr != nil {
		return nil, privErr
	}
	if !claimBotNodeJob(rec.ID) {
		return nil, fmt.Errorf("这台机器正在安装 Bot 节点")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	botNodeMu.Lock()
	records, err = a.loadBotNodes()
	if err != nil {
		botNodeMu.Unlock()
		releaseBotNodeJob(rec.ID)
		return nil, err
	}
	records, settled := settleBotNodeJobs(records)
	updated := false
	for i := range records {
		if records[i].ID != rec.ID {
			continue
		}
		if records[i].NodeID != "" {
			if settled {
				_ = a.saveBotNodes(records)
			}
			botNodeMu.Unlock()
			releaseBotNodeJob(rec.ID)
			if settled {
				a.emitEvent(botNodeEvent, "")
			}
			return nil, fmt.Errorf("这台机器已经注册到 Hub")
		}
		if botNodeHostBusy(records, rec.ID, rec.Host, rec.Port) {
			if settled {
				_ = a.saveBotNodes(records)
			}
			botNodeMu.Unlock()
			releaseBotNodeJob(rec.ID)
			if settled {
				a.emitEvent(botNodeEvent, "")
			}
			return nil, fmt.Errorf("这台机器正在安装 Bot 节点")
		}
		records[i].Status = botNodeStatusRegistering
		records[i].Message = "正在用新注册码向 Hub 注册"
		records[i].HubURL = docHub
		records[i].JobStartedAt = now
		records[i].UpdatedAt = now
		rec = records[i]
		updated = true
		break
	}
	if !updated {
		botNodeMu.Unlock()
		releaseBotNodeJob(rec.ID)
		return nil, fmt.Errorf("找不到这台 Bot 节点")
	}
	err = a.saveBotNodes(records)
	botNodeMu.Unlock()
	if err != nil {
		releaseBotNodeJob(rec.ID)
		return nil, err
	}
	a.emitEvent(botNodeEvent, "")
	go a.runBotNode(rec, req.Password, privilege, code, true)
	out := rec
	return &out, nil
}

func (a *App) insertBotNode(rec BotNodeRecord) error {
	if !claimBotNodeJob(rec.ID) {
		return fmt.Errorf("这台机器正在安装 Bot 节点")
	}
	botNodeMu.Lock()
	records, err := a.loadBotNodes()
	if err != nil {
		botNodeMu.Unlock()
		releaseBotNodeJob(rec.ID)
		return err
	}
	records, repaired := settleBotNodeJobs(records)
	for _, old := range records {
		if old.ID != rec.ID && old.Host == rec.Host && old.Port == rec.Port && botNodeBusy(old.Status) {
			if repaired {
				if err := a.saveBotNodes(records); err != nil {
					botNodeMu.Unlock()
					releaseBotNodeJob(rec.ID)
					return err
				}
			}
			botNodeMu.Unlock()
			releaseBotNodeJob(rec.ID)
			if repaired {
				a.emitEvent(botNodeEvent, "")
			}
			return fmt.Errorf("这台机器正在安装 Bot 节点")
		}
	}
	records = append(records, rec)
	err = a.saveBotNodes(records)
	botNodeMu.Unlock()
	if err != nil {
		releaseBotNodeJob(rec.ID)
		return err
	}
	a.emitEvent(botNodeEvent, "")
	return nil
}

func (a *App) botNodeHubURL() (string, error) {
	docHub, _, hubErr := a.getHubCredentials()
	docHub = strings.TrimRight(strings.TrimSpace(docHub), "/")
	if docHub == "" {
		if hubErr != nil {
			return "", fmt.Errorf("还没有连接 Hub，读不到安装文档: %v", hubErr)
		}
		return "", fmt.Errorf("还没有连接 Hub，读不到安装文档")
	}
	return docHub, nil
}

func botNodeEnrollCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 16 || strings.Trim(code, "0123456789ABCDEF") != "" {
		return "", fmt.Errorf("注册码无效。向 Hub 管理员要 16 位注册码")
	}
	return code, nil
}

func chooseBotNodePrivilege(conn *desktopNodeSSH, canSudo bool) (desktopNodePrivilege, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := conn.exec(ctx, "id -u", ""); err == nil && strings.TrimSpace(out) == "0" {
		return desktopNodePrivilege{root: true}, nil
	}
	if !canSudo {
		return desktopNodePrivilege{}, fmt.Errorf("登录用户不是 root。这台机器可以 sudo 时请勾选")
	}
	if out, err := conn.exec(ctx, "sudo -S -p '' id -u", conn.password+"\n"); err == nil && strings.TrimSpace(lastLine(out)) == "0" {
		return desktopNodePrivilege{sudo: true}, nil
	}
	return desktopNodePrivilege{}, fmt.Errorf("这个密码不能 sudo 到 root")
}

func (a *App) runBotNode(rec BotNodeRecord, password string, privilege desktopNodePrivilege, code string, enrollOnly bool) {
	defer releaseBotNodeJob(rec.ID)
	label := "maclaw-botnode-" + strings.TrimPrefix(rec.ID, "bn_")
	registerEphemeralSSHHost(corelib.SSHHostEntry{
		Label: label, Host: rec.Host, Port: rec.Port, User: rec.User,
		AuthMethod: "password", Password: password,
	})
	defer unregisterEphemeralSSHHost(label)
	display := botNodeDisplay(rec.User, rec.Host)
	var registeredID string
	_, err := RunOpsAgent(a, OpsAgentJob{
		Name:    "部署 Bot 节点 · " + rec.User + "@" + rec.Host,
		Display: display,
		Prompt:  botNodePrompt(rec, password, privilege, code, enrollOnly),
		Marker:  "BOTNODE_INSTALL",
		Budget:  90 * time.Minute,
		Every:   45 * time.Second,
		Log:     func(step, detail string) { a.botNodeMessage(rec.ID, step+": "+detail) },
		OnTask: func(projectPath string) {
			a.updateBotNode(rec.ID, func(r *BotNodeRecord) { r.TaskPath = projectPath })
		},
		OnWaiting: func(string) {
			a.updateBotNode(rec.ID, func(r *BotNodeRecord) {
				r.Status = botNodeStatusRegistering
				r.Message = "agent 已结束，等待向 Hub 注册"
			})
		},
		Probe: func(agentDone bool, verdict string) error {
			if agentDone && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(verdict)), "FAILED") {
				return errors.New(strings.TrimSpace(verdict))
			}
			id, err := a.probeBotNode(rec, password, privilege, agentDone)
			if err == nil {
				registeredID = id
			}
			return err
		},
		Fatal: func(err error) bool {
			return err != nil && !strings.Contains(err.Error(), "正在")
		},
	})
	if err != nil {
		a.updateBotNode(rec.ID, func(r *BotNodeRecord) {
			r.Status = botNodeStatusFailed
			r.Message = OpsScrub(err.Error())
			r.JobStartedAt = ""
		})
		return
	}
	a.updateBotNode(rec.ID, func(r *BotNodeRecord) {
		r.Status = botNodeStatusReady
		if registeredID != "" {
			r.NodeID = registeredID
		}
		r.Message = "已安装并注册到 Hub"
		r.JobStartedAt = ""
	})
}

func botNodeDisplay(user, host string) string {
	return fmt.Sprintf("〔Bot 节点〕在 %s@%s 上安装并注册到 Hub", user, host)
}

func (a *App) probeBotNode(rec BotNodeRecord, password string, privilege desktopNodePrivilege, agentDone bool) (string, error) {
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return "", fmt.Errorf("Bot 节点正在安装，SSH 暂不可用")
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, _ := conn.run(ctx, botNodeProbeCommand(), privilege, password)
	nodeID, enrollLeft, err := botNodeProbeDecision(lastLine(out), rec.WithSrv, rec.HubURL, agentDone)
	if err != nil || !enrollLeft {
		return nodeID, err
	}
	// The code is spent once node_id exists. Leave it out of the env file.
	cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cleanCancel()
	_, _ = conn.run(cleanCtx, "sed -i '/^BOTNODE_ENROLL_CODE=/d' /etc/maclaw/botnode.env", privilege, password)
	return nodeID, nil
}

func botNodeProbeDecision(line string, withSrv bool, hubURL string, agentDone bool) (string, bool, error) {
	var parsed struct {
		Desktopd   bool   `json:"desktopd"`
		NodeID     string `json:"node_id"`
		HubURL     string `json:"hub_url"`
		Botnode    bool   `json:"botnode"`
		EnrollLeft bool   `json:"enroll_left"`
		Srv        bool   `json:"srv"`
		SrvToken   bool   `json:"srv_token"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(line)), &parsed) != nil || !parsed.Desktopd {
		return "", false, fmt.Errorf("Bot 节点正在安装")
	}
	nodeID := strings.TrimSpace(parsed.NodeID)
	if nodeID == "" {
		if agentDone {
			return "", false, fmt.Errorf("注册失败: 还没有向 Hub 注册")
		}
		return "", false, fmt.Errorf("Bot 节点正在注册")
	}
	want := strings.TrimRight(strings.TrimSpace(hubURL), "/")
	got := strings.TrimRight(strings.TrimSpace(parsed.HubURL), "/")
	if want != "" && got != "" && !strings.EqualFold(want, got) {
		return "", false, fmt.Errorf("注册失败: 节点连的不是当前 Hub")
	}
	// systemctl restart returns while the unit is still activating. That is
	// not a failed registration; ending the job here would drop SSH mid-restart.
	if !parsed.Botnode || (withSrv && (!parsed.Srv || !parsed.SrvToken)) {
		return "", false, fmt.Errorf("Bot 节点正在收尾")
	}
	if !agentDone {
		if parsed.EnrollLeft {
			return "", false, fmt.Errorf("Bot 节点正在注册")
		}
		return "", false, fmt.Errorf("Bot 节点正在收尾")
	}
	return nodeID, parsed.EnrollLeft, nil
}

// botNodeProbeScript checks a finished install with POSIX tools. The host
// does not need Python; the desktop's python3 is inside the maclaw-gui image.
const botNodeProbeScript = `token=$(sed -n 's/^DESKTOPD_TOKEN=//p' /etc/maclaw/desktopd.env 2>/dev/null | head -n 1 | tr -d '"' | tr -d "'")
desktopd=false
if [ -n "$token" ]; then
  body=$(curl -fsS -m 5 -H "Authorization: Bearer $token" http://127.0.0.1:18081/v1/health 2>/dev/null | tr -d '[:space:]' || true)
  case "$body" in
    *'"ok":true'*) desktopd=true ;;
  esac
fi
node_id=$(sed -n 's/.*"node_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /var/lib/maclaw/botnode.json 2>/dev/null | head -n 1)
hub_url=$(sed -n 's/.*"hub_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' /var/lib/maclaw/botnode.json 2>/dev/null | head -n 1)
enroll=$(sed -n 's/^BOTNODE_ENROLL_CODE=//p' /etc/maclaw/botnode.env 2>/dev/null | head -n 1)
srv_token=$(sed -n 's/^MACLAW_DESKTOP_API_TOKEN=//p' /etc/maclaw/maclawsrv.env 2>/dev/null | head -n 1)
if [ "$(systemctl is-active maclaw-botnode 2>/dev/null || true)" = "active" ]; then botnode=true; else botnode=false; fi
if [ "$(systemctl is-active maclaw-srv 2>/dev/null || true)" = "active" ]; then srv=true; else srv=false; fi
if [ -n "$enroll" ]; then enroll_left=true; else enroll_left=false; fi
if [ -n "$srv_token" ]; then has_token=true; else has_token=false; fi
printf '{"desktopd":%s,"node_id":"%s","hub_url":"%s","botnode":%s,"enroll_left":%s,"srv":%s,"srv_token":%s}\n' "$desktopd" "$node_id" "$hub_url" "$botnode" "$enroll_left" "$srv" "$has_token"
`

func botNodeProbeCommand() string {
	return botNodeProbeScript
}

func (a *App) updateBotNode(id string, fn func(*BotNodeRecord)) {
	botNodeMu.Lock()
	defer botNodeMu.Unlock()
	records, err := a.loadBotNodes()
	if err != nil {
		return
	}
	for i := range records {
		if records[i].ID != id {
			continue
		}
		fn(&records[i])
		records[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = a.saveBotNodes(records)
		a.emitEvent(botNodeEvent, "")
		return
	}
}

func (a *App) botNodeMessage(id, message string) {
	message = OpsScrub(strings.TrimSpace(message))
	if message == "" {
		return
	}
	a.updateBotNode(id, func(r *BotNodeRecord) { r.Message = message })
}

func botNodePrompt(rec BotNodeRecord, password string, privilege desktopNodePrivilege, code string, enrollOnly bool) string {
	var b strings.Builder
	if enrollOnly {
		b.WriteString("用 ssh 登录下面这台 Linux 服务器。desktopd 应该已经装好。用新的注册码让 botnode 向当前 Hub 注册。不要用旧码重试。\n\n")
	} else {
		b.WriteString("用 ssh 登录下面这台 Linux 服务器，按安装文档安装私有 Bot 节点，并让 botnode 用注册码向当前 Hub 注册。\n\n")
	}
	b.WriteString("这不是安装 Hub，不要向 Hub Center 注册，也不要登记成个人内网桌面。\n\n")
	fmt.Fprintf(&b, "服务器：\n- 地址：%s\n- 端口：%d\n- 用户：%s\n- 密码：%s\n- 提权：%s\n\n",
		rec.Host, rec.Port, rec.User, password, desktopNodePrivilegeLine(privilege, "安装"))
	fmt.Fprintf(&b, "安装文档：%s/docs/botnode\n", rec.HubURL)
	fmt.Fprintf(&b, "HUB_URL=%s\n", rec.HubURL)
	if rec.WithSrv {
		b.WriteString("WITH_SRV=1\n")
	} else {
		b.WriteString("WITH_SRV=0\n")
	}
	fmt.Fprintf(&b, "ENROLL_CODE=%s\n", code)
	b.WriteString("\n注册方式：botnode 用 ENROLL_CODE 向 HUB_URL 这台 Hub 换长期密钥。成功的标志是 /var/lib/maclaw/botnode.json 里有 node_id，并且 hub_url 就是 HUB_URL。不要自己调用发码接口。\n")
	b.WriteString("desktopd 的健康检查还没返回 \"ok\":true 之前，不要写 BOTNODE_ENROLL_CODE，也不要启动 maclaw-botnode。码过期、输错或已经用过就停下来，最后一行写失败原因，不要用旧码反复重试。\n")
	b.WriteString("状态文件里已经有 node_id 时不要删文件。注册成功后删掉环境里的 BOTNODE_ENROLL_CODE，再重启 botnode。\n")
	if rec.WithSrv {
		b.WriteString("这次 WITH_SRV=1：先做第 7 节，等 node_id 出现后再做第 9 节。hub_token 写入 MACLAW_DESKTOP_API_TOKEN 之前不要启动 maclaw-srv。不要把 BOTNODE_SRV_ADDR 写成 off。\n")
	} else {
		b.WriteString("这次 WITH_SRV=0：跳过第 7 节和第 9 节，BOTNODE_SRV_ADDR=off。\n")
	}
	b.WriteString("不要设置 DESKTOPD_ADVERTISE_HOST 或 DESKTOPD_PUBLIC_URL。不要用 /data/soft。不要打印注册码、node_secret 或 hub_token。\n")
	b.WriteString("完成后最后一行输出 `BOTNODE_INSTALL: OK`；失败则输出 `BOTNODE_INSTALL: FAILED <原因>`。\n")
	return b.String()
}
