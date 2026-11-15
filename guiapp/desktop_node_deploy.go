package guiapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/ssh"
)

// Self-registered desktopds ("桌面" settings tab). Hub keeps which desktopd
// serves this user; this file keeps how each one was reached (SSH target,
// deploy dir) so it can be checked, re-registered, backed up and restored.
// The SSH password is never stored: it lives in memory for one deploy job.

const (
	desktopNodeDeployDir    = "/data/soft/maclaw_desktopd"
	desktopNodeDesktopdPort = 18081
	desktopNodeSrvDir       = "/data/soft/maclaw_srv"
	desktopNodeSrvAddr      = "127.0.0.1:18080"
	desktopNodesFile        = "desktop_nodes.json"
	desktopNodesEvent       = "desktop-nodes-changed"

	desktopNodeStatusDeploying   = "deploying"
	desktopNodeStatusUpgrading   = "upgrading"
	desktopNodeStatusRegistering = "registering"
	desktopNodeStatusReady       = "ready"
	desktopNodeStatusFailed      = "failed"
	desktopNodeStatusImported    = "imported"
)

// DesktopNodeRecord is one desktopd this GUI deployed, registered or imported.
type DesktopNodeRecord struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Mode         string `json:"mode"` // ssh or direct
	Host         string `json:"host,omitempty"`
	Port         int    `json:"port,omitempty"`
	User         string `json:"user,omitempty"`
	HostKey      string `json:"host_key,omitempty"`
	DeployDir    string `json:"deploy_dir,omitempty"`
	DesktopdPort int    `json:"desktopd_port,omitempty"`
	DesktopdURL  string `json:"desktopd_url,omitempty"`
	Token        string `json:"token,omitempty"`
	HubURL       string `json:"hub_url,omitempty"`
	NodeID       string `json:"node_id,omitempty"`
	ServerID     string `json:"server_id,omitempty"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	TaskPath     string `json:"task_path,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	// WithSrv installs MaClawSrv on the same machine. SrvTokenSet means the
	// Hub token was written into that srv; the token itself is not stored here.
	WithSrv     bool `json:"with_srv,omitempty"`
	SrvTokenSet bool `json:"srv_token_set,omitempty"`
	// LastKind, LastResult and LastAt are the latest finished install or
	// update. They stay after the live status line is cleared, until the
	// person clears them. They never contain a token or a password.
	LastKind   string `json:"last_kind,omitempty"`
	LastResult string `json:"last_result,omitempty"`
	LastAt     string `json:"last_at,omitempty"`
	// JobStartedAt is when the current install or update began. It is empty
	// once that job finishes. It is not a secret.
	JobStartedAt string `json:"job_started_at,omitempty"`
}

// DesktopNodeEntry is a record merged with what Hub says about it.
type DesktopNodeEntry struct {
	DesktopNodeRecord
	Registered bool `json:"registered"`
	Online     bool `json:"online"`
	Active     bool `json:"active"`
}

type DesktopNodesView struct {
	Enabled  bool               `json:"enabled"`
	HubError string             `json:"hub_error,omitempty"`
	Nodes    []DesktopNodeEntry `json:"nodes"`
}

type DesktopNodeDeployRequest struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	WithSrv  bool   `json:"with_srv"`
}

type DesktopNodeDirectRequest struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

type hubSelfDesktopNode struct {
	ServerID    string `json:"server_id"`
	NodeID      string `json:"node_id"`
	Name        string `json:"name"`
	Online      bool   `json:"online"`
	Active      bool   `json:"active"`
	AccessToken string `json:"access_token"`
}

type hubSelfDesktopView struct {
	Enabled bool                 `json:"enabled"`
	Nodes   []hubSelfDesktopNode `json:"nodes"`
}

var desktopNodesMu sync.Mutex

// ---------------------------------------------------------------------------
// Ephemeral SSH labels: the deploy agent connects by label, so the password
// reaches the SSH tool without ever entering the model's context.
// ---------------------------------------------------------------------------

var ephemeralSSHHosts sync.Map // lower(label) -> corelib.SSHHostEntry

func registerEphemeralSSHHost(entry corelib.SSHHostEntry) {
	ephemeralSSHHosts.Store(strings.ToLower(strings.TrimSpace(entry.Label)), entry)
}

func unregisterEphemeralSSHHost(label string) {
	ephemeralSSHHosts.Delete(strings.ToLower(strings.TrimSpace(label)))
}

func ephemeralSSHHost(label string) (corelib.SSHHostEntry, bool) {
	value, ok := ephemeralSSHHosts.Load(strings.ToLower(strings.TrimSpace(label)))
	if !ok {
		return corelib.SSHHostEntry{}, false
	}
	entry, ok := value.(corelib.SSHHostEntry)
	return entry, ok
}

// ---------------------------------------------------------------------------
// Local store
// ---------------------------------------------------------------------------

func (a *App) desktopNodesPath() string {
	dir := ""
	if a != nil {
		dir = a.GetDataDir()
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".maclaw")
	}
	return filepath.Join(dir, desktopNodesFile)
}

func (a *App) loadDesktopNodeRecords() ([]DesktopNodeRecord, error) {
	raw, err := os.ReadFile(a.desktopNodesPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var file struct {
		Nodes []DesktopNodeRecord `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	return file.Nodes, nil
}

func (a *App) saveDesktopNodeRecords(records []DesktopNodeRecord) error {
	path := a.desktopNodesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(struct {
		Nodes []DesktopNodeRecord `json:"nodes"`
	}{Nodes: records}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// updateDesktopNode applies fn to an existing record and saves. A missing id
// is an error: recreating it would bring back a desktop the user just deleted
// and drop every field fn did not set.
func (a *App) updateDesktopNode(id string, fn func(*DesktopNodeRecord)) (DesktopNodeRecord, error) {
	desktopNodesMu.Lock()
	records, err := a.loadDesktopNodeRecords()
	if err != nil {
		desktopNodesMu.Unlock()
		return DesktopNodeRecord{}, err
	}
	index := -1
	for i := range records {
		if records[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		desktopNodesMu.Unlock()
		return DesktopNodeRecord{}, fmt.Errorf("desktopd 记录不存在")
	}
	prevStatus := records[index].Status
	fn(&records[index])
	noteDesktopNodeJobStart(&records[index], prevStatus)
	records[index].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	out := records[index]
	err = a.saveDesktopNodeRecords(records)
	desktopNodesMu.Unlock()
	if err != nil {
		return DesktopNodeRecord{}, err
	}
	a.emitEvent(desktopNodesEvent, out.ID)
	return out, nil
}

func (a *App) insertDesktopNode(rec DesktopNodeRecord) (DesktopNodeRecord, error) {
	if strings.TrimSpace(rec.ID) == "" {
		rec.ID = newDesktopNodeID()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if rec.CreatedAt == "" {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	noteDesktopNodeJobStart(&rec, "")
	desktopNodesMu.Lock()
	records, err := a.loadDesktopNodeRecords()
	if err != nil {
		desktopNodesMu.Unlock()
		return DesktopNodeRecord{}, err
	}
	records = append(records, rec)
	err = a.saveDesktopNodeRecords(records)
	desktopNodesMu.Unlock()
	if err != nil {
		return DesktopNodeRecord{}, err
	}
	a.emitEvent(desktopNodesEvent, rec.ID)
	return rec, nil
}

// noteDesktopNodeJobStart records when an install or update begins and
// clears that mark when the job leaves the busy states.
func noteDesktopNodeJobStart(rec *DesktopNodeRecord, prev string) {
	if rec == nil {
		return
	}
	if !desktopNodeBusyStatus(rec.Status) {
		rec.JobStartedAt = ""
		return
	}
	if rec.JobStartedAt == "" || !desktopNodeBusyStatus(prev) {
		rec.JobStartedAt = time.Now().UTC().Format(time.RFC3339)
	}
}

// LocalDesktopJob is one install or update still running on this GUI.
// It carries no token, password, or hub secret.
type LocalDesktopJob struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
}

// ListLocalDesktopJobs is the task monitor's view of desktop installs and
// updates. It reads the local records only and does not call Hub. A busy
// record with no job in this process is the leftover of a crash, so it is
// closed here and drops off the list.
func (a *App) ListLocalDesktopJobs() ([]LocalDesktopJob, error) {
	desktopNodesMu.Lock()
	records, err := a.loadDesktopNodeRecords()
	if err != nil {
		desktopNodesMu.Unlock()
		return nil, err
	}
	records, repaired := settleDesktopNodeJobs(records)
	if repaired {
		err = a.saveDesktopNodeRecords(records)
	}
	desktopNodesMu.Unlock()
	if err != nil {
		return nil, err
	}
	if repaired {
		a.emitEvent(desktopNodesEvent, "")
	}
	out := make([]LocalDesktopJob, 0)
	for _, rec := range records {
		if !desktopNodeBusyStatus(rec.Status) {
			continue
		}
		started := rec.JobStartedAt
		if started == "" {
			started = rec.UpdatedAt
		}
		out = append(out, LocalDesktopJob{
			ID:        rec.ID,
			Title:     localDesktopJobTitle(rec),
			Status:    rec.Status,
			StartedAt: started,
		})
	}
	return out, nil
}

func localDesktopJobTitle(rec DesktopNodeRecord) string {
	action := "部署"
	if rec.Status == desktopNodeStatusUpgrading {
		action = "更新"
	}
	what := "desktopd"
	if rec.WithSrv {
		what = "desktopd 和 MaClawSrv"
	}
	target := strings.TrimSpace(rec.User)
	if rec.Host != "" {
		if target != "" {
			target += "@"
		}
		target += rec.Host
	}
	if target == "" {
		target = strings.TrimSpace(rec.Name)
	}
	if target == "" {
		return action + " " + what
	}
	return action + " " + what + " · " + target
}

func (a *App) setDesktopNodeStatus(id, status, message string) {
	if _, err := a.updateDesktopNode(id, func(rec *DesktopNodeRecord) {
		kind := "deploy"
		if rec.Status == desktopNodeStatusUpgrading || strings.Contains(message, "升级") {
			kind = "upgrade"
		}
		rec.Status = status
		rec.Message = message
		if status == desktopNodeStatusFailed || status == desktopNodeStatusReady {
			recordDesktopNodeResult(rec, kind, message)
		}
	}); err != nil {
		log.Printf("[desktop-node] save status failed id=%s err=%v", id, err)
	}
}

// desktopNodeOutcomeKind keeps an upgrade labeled as an update when the
// finishing step is the srv token write. A fresh install stays a deploy.
func desktopNodeOutcomeKind(status string) string {
	if status == desktopNodeStatusUpgrading {
		return "upgrade"
	}
	return "deploy"
}

// recordDesktopNodeResult keeps the outcome of one install or update.
// An empty message becomes a short finished sentence. The text is capped
// and must not be a token.
func recordDesktopNodeResult(rec *DesktopNodeRecord, kind, message string) {
	if rec == nil {
		return
	}
	if kind != "upgrade" {
		kind = "deploy"
	}
	rec.LastKind = kind
	rec.LastAt = time.Now().UTC().Format(time.RFC3339)
	text := strings.TrimSpace(message)
	if text == "" {
		switch {
		case kind == "upgrade":
			text = "更新完成。访问令牌和 Hub 连接保持不变。"
		case rec.WithSrv && rec.SrvTokenSet:
			text = "部署完成。MaClawSrv 和桌面都已安装，令牌已写入。"
		case rec.WithSrv:
			text = "部署完成。MaClawSrv 已安装，令牌还没有写上。"
		default:
			text = "部署完成。"
		}
	}
	if len([]rune(text)) > 500 {
		text = string([]rune(text)[:500])
	}
	rec.LastResult = text
}

// ClearDesktopNodeResult removes the stored install or update report.
// The desktop itself, and a job that is still running, stay.
func (a *App) ClearDesktopNodeResult(id string) error {
	_, err := a.updateDesktopNode(id, func(rec *DesktopNodeRecord) {
		rec.LastKind = ""
		rec.LastResult = ""
		rec.LastAt = ""
		if !desktopNodeBusyStatus(rec.Status) {
			rec.Message = ""
		}
	})
	return err
}

func newDesktopNodeID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return "dn_" + hex.EncodeToString(buf[:])
}

// ---------------------------------------------------------------------------
// Bindings
// ---------------------------------------------------------------------------

// ListDesktopNodes merges this GUI's records with the desktops Hub has for
// this user. A Hub-only entry (registered from another computer) is listed
// with what Hub knows, including its token.
func (a *App) ListDesktopNodes() (*DesktopNodesView, error) {
	desktopNodesMu.Lock()
	records, err := a.loadDesktopNodeRecords()
	if err != nil {
		desktopNodesMu.Unlock()
		return nil, err
	}
	records, repaired := settleDesktopNodeJobs(records)
	if repaired {
		err = a.saveDesktopNodeRecords(records)
	}
	desktopNodesMu.Unlock()
	if repaired && err == nil {
		a.emitEvent(desktopNodesEvent, "")
	}
	if err != nil {
		return nil, err
	}
	view := &DesktopNodesView{Nodes: []DesktopNodeEntry{}}
	var hub hubSelfDesktopView
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots/desktop-node", nil, &hub, 20*time.Second); err != nil {
		view.HubError = err.Error()
	} else {
		view.Enabled = hub.Enabled
	}
	byNode := map[string]hubSelfDesktopNode{}
	for _, node := range hub.Nodes {
		byNode[node.NodeID] = node
	}
	seen := map[string]bool{}
	for _, rec := range records {
		entry := DesktopNodeEntry{DesktopNodeRecord: rec}
		if node, ok := byNode[rec.NodeID]; ok && rec.NodeID != "" {
			seen[rec.NodeID] = true
			entry.Registered = true
			entry.Online = node.Online
			entry.Active = node.Active
			entry.ServerID = node.ServerID
			if entry.Token == "" {
				entry.Token = node.AccessToken
			}
			if entry.Name == "" {
				entry.Name = node.Name
			}
		}
		view.Nodes = append(view.Nodes, entry)
	}
	for _, node := range hub.Nodes {
		if seen[node.NodeID] {
			continue
		}
		view.Nodes = append(view.Nodes, DesktopNodeEntry{
			DesktopNodeRecord: DesktopNodeRecord{
				ID: "hub:" + node.ServerID, Name: node.Name, NodeID: node.NodeID, ServerID: node.ServerID,
				Token: node.AccessToken, Status: desktopNodeStatusReady,
			},
			Registered: true, Online: node.Online, Active: node.Active,
		})
	}
	sortDesktopNodeEntries(view.Nodes)
	return view, nil
}

// DeployDesktopNode starts a one-click deploy: the assistant agent installs
// desktopd on the SSH target, then this GUI reads its token over SSH and
// registers it on Hub for this user only.
func (a *App) DeployDesktopNode(req DesktopNodeDeployRequest) (*DesktopNodeRecord, error) {
	req.Host = strings.TrimSpace(req.Host)
	req.User = strings.TrimSpace(req.User)
	if req.Port <= 0 {
		req.Port = 22
	}
	if req.Host == "" || req.User == "" || req.Password == "" {
		return nil, fmt.Errorf("请填写 IP、用户名和密码")
	}
	if req.Port > 65535 || strings.ContainsAny(req.Host, " /@\\") || strings.ContainsAny(req.User, " :/@\\'\"") {
		return nil, fmt.Errorf("SSH 地址或用户名无效")
	}
	conn, err := dialDesktopNodeSSH(req.Host, req.Port, req.User, req.Password, "")
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %v", err)
	}
	privilege, privErr := conn.ensurePrivilege()
	conn.Close()
	if privErr != nil {
		return nil, privErr
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = req.User + "@" + req.Host
	}
	hubURL, _, _ := a.getHubCredentials()
	id := newDesktopNodeID()
	if !claimDesktopNodeJob(id) {
		return nil, fmt.Errorf("这台桌面正在部署或更新")
	}
	rec, err := a.insertDesktopNode(DesktopNodeRecord{
		ID: id, Name: name, Mode: "ssh",
		Host: req.Host, Port: req.Port, User: req.User, HostKey: conn.hostKey,
		DeployDir: desktopNodeDeployDir, DesktopdPort: desktopNodeDesktopdPort,
		HubURL:  strings.TrimRight(strings.TrimSpace(hubURL), "/"),
		WithSrv: req.WithSrv,
		Status:  desktopNodeStatusDeploying, Message: desktopNodeDeployMessage(req.WithSrv),
	})
	if err != nil {
		releaseDesktopNodeJob(id)
		return nil, err
	}
	a.spawnDesktopNodeJob(rec, req.Password, privilege, desktopNodeJob{})
	return &rec, nil
}

// RetryDesktopNode checks an SSH desktopd again and registers it when it is
// healthy. When it is not, the agent deploy runs again.
func (a *App) RetryDesktopNode(id, password string) (*DesktopNodeRecord, error) {
	rec, err := a.findDesktopNode(id)
	if err != nil {
		return nil, err
	}
	switch rec.Mode {
	case "direct":
		if !claimDesktopNodeJob(rec.ID) {
			return nil, fmt.Errorf("这台桌面正在部署或更新")
		}
		err := a.registerDirectDesktopNode(rec)
		releaseDesktopNodeJob(rec.ID)
		if err != nil {
			return nil, err
		}
	case "ssh":
		if password == "" {
			return nil, fmt.Errorf("请输入 %s@%s 的 SSH 密码", rec.User, rec.Host)
		}
		conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
		if err != nil {
			return nil, fmt.Errorf("SSH 连接失败: %v", err)
		}
		privilege, privErr := conn.ensurePrivilege()
		conn.Close()
		if privErr != nil {
			return nil, privErr
		}
		if !claimDesktopNodeJob(rec.ID) {
			return nil, fmt.Errorf("这台桌面正在部署或更新")
		}
		a.setDesktopNodeStatus(rec.ID, desktopNodeStatusRegistering, "正在检查 desktopd")
		go func() {
			defer releaseDesktopNodeJob(rec.ID)
			if err := a.finishDesktopNodeDeploy(rec, password, privilege, false); err != nil {
				if desktopNodeFatal(err) {
					a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, err.Error())
					return
				}
				a.setDesktopNodeStatus(rec.ID, desktopNodeStatusDeploying, "desktopd 不可用，agent 重新部署: "+err.Error())
				a.runDesktopNodeJob(rec, password, privilege, desktopNodeJob{})
			}
		}()
	default:
		return nil, fmt.Errorf("这条记录没有可连接的 desktopd 地址")
	}
	out, err := a.findDesktopNode(id)
	return &out, err
}

// UpgradeDesktopNode sends the assistant agent to an already configured
// intranet desktopd. The agent pulls the latest source, rebuilds and runs the
// same installer. The existing token and Hub link stay; this GUI only checks
// that the service came back.
func (a *App) UpgradeDesktopNode(id, password string) (*DesktopNodeRecord, error) {
	rec, err := a.findDesktopNode(id)
	if err != nil {
		return nil, err
	}
	if rec.Mode != "ssh" || strings.TrimSpace(rec.Host) == "" || strings.TrimSpace(rec.User) == "" {
		return nil, fmt.Errorf("这台桌面没有保存 SSH 地址，无法派 agent 去升级")
	}
	if rec.Status == desktopNodeStatusDeploying || rec.Status == desktopNodeStatusUpgrading {
		return nil, fmt.Errorf("这台桌面正在部署或升级")
	}
	if password == "" {
		return nil, fmt.Errorf("请输入 %s@%s 的 SSH 密码", rec.User, rec.Host)
	}
	if rec.Port <= 0 {
		rec.Port = 22
	}
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %v", err)
	}
	privilege, privErr := conn.ensurePrivilege()
	conn.Close()
	if privErr != nil {
		return nil, privErr
	}
	if !claimDesktopNodeJob(rec.ID) {
		return nil, fmt.Errorf("这台桌面正在部署或更新")
	}
	updated, err := a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.Status = desktopNodeStatusUpgrading
		r.Message = "agent 正在用最新代码升级 desktopd"
	})
	if err != nil {
		releaseDesktopNodeJob(rec.ID)
		return nil, err
	}
	go func() {
		defer releaseDesktopNodeJob(updated.ID)
		a.runDesktopNodeJob(updated, password, privilege, desktopNodeJob{kind: "upgrade", stamp: newDesktopNodeStamp()})
	}()
	return &updated, nil
}

// RedeployDesktopNode runs a fresh install on a desktop that has not
// finished registering. withSrv is the choice made on the desktop page.
// An already registered desktop is updated instead, so its token stays.
func (a *App) RedeployDesktopNode(id, password string, withSrv bool) (*DesktopNodeRecord, error) {
	rec, err := a.findDesktopNode(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rec.NodeID) != "" {
		return a.UpgradeDesktopNode(id, password)
	}
	if rec.Mode != "ssh" || strings.TrimSpace(rec.Host) == "" || strings.TrimSpace(rec.User) == "" {
		return nil, fmt.Errorf("这台桌面没有保存 SSH 地址，无法重新部署")
	}
	if rec.Status == desktopNodeStatusDeploying || rec.Status == desktopNodeStatusUpgrading {
		return nil, fmt.Errorf("这台桌面正在部署或升级")
	}
	if password == "" {
		return nil, fmt.Errorf("请输入 %s@%s 的 SSH 密码", rec.User, rec.Host)
	}
	if rec.Port <= 0 {
		rec.Port = 22
	}
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %v", err)
	}
	privilege, privErr := conn.ensurePrivilege()
	conn.Close()
	if privErr != nil {
		return nil, privErr
	}
	if !claimDesktopNodeJob(rec.ID) {
		return nil, fmt.Errorf("这台桌面正在部署或更新")
	}
	updated, err := a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.WithSrv = withSrv
		r.Status = desktopNodeStatusDeploying
		r.Message = desktopNodeDeployMessage(withSrv)
	})
	if err != nil {
		releaseDesktopNodeJob(rec.ID)
		return nil, err
	}
	a.spawnDesktopNodeJob(updated, password, privilege, desktopNodeJob{})
	return &updated, nil
}

// ConfigureDesktopNodeToken writes this machine's Hub token into the
// same-machine MaClawSrv. The token is read over SSH and is not returned.
func (a *App) ConfigureDesktopNodeToken(id, password string) (*DesktopNodeRecord, error) {
	rec, err := a.findDesktopNode(id)
	if err != nil {
		return nil, err
	}
	if !rec.WithSrv {
		return nil, fmt.Errorf("这台安装没有 MaClawSrv")
	}
	if rec.Mode != "ssh" || strings.TrimSpace(rec.Host) == "" || strings.TrimSpace(rec.User) == "" {
		return nil, fmt.Errorf("这台桌面没有保存 SSH 地址，无法配置令牌")
	}
	if strings.TrimSpace(rec.NodeID) == "" {
		return nil, fmt.Errorf("桌面还没有注册完成")
	}
	if password == "" {
		return nil, fmt.Errorf("请输入 %s@%s 的 SSH 密码", rec.User, rec.Host)
	}
	if rec.Port <= 0 {
		rec.Port = 22
	}
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败: %v", err)
	}
	defer conn.Close()
	privilege, privErr := conn.ensurePrivilege()
	if privErr != nil {
		return nil, privErr
	}
	if err := a.writeDesktopNodeSrvToken(conn, rec, password, privilege); err != nil {
		return nil, err
	}
	out, err := a.findDesktopNode(id)
	return &out, err
}

// spawnDesktopNodeJob runs the agent job. The caller already claimed rec.ID;
// the goroutine releases it.
func (a *App) spawnDesktopNodeJob(rec DesktopNodeRecord, password string, privilege desktopNodePrivilege, job desktopNodeJob) {
	go func() {
		defer releaseDesktopNodeJob(rec.ID)
		a.runDesktopNodeJob(rec, password, privilege, job)
	}()
}

// RegisterDesktopNodeDirect registers a desktopd this computer reaches at
// url with its token (the "填写内网 desktopd 地址" path).
func (a *App) RegisterDesktopNodeDirect(req DesktopNodeDirectRequest) (*DesktopNodeRecord, error) {
	base, err := normalizeDesktopdURL(req.URL)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return nil, fmt.Errorf("请填写 desktopd 访问令牌（DESKTOPD_TOKEN）")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = base
	}
	hubURL, _, _ := a.getHubCredentials()
	id := newDesktopNodeID()
	if !claimDesktopNodeJob(id) {
		return nil, fmt.Errorf("这台桌面正在部署或更新")
	}
	rec, err := a.insertDesktopNode(DesktopNodeRecord{
		ID: id, Name: name, Mode: "direct", DesktopdURL: base, Token: token,
		HubURL: strings.TrimRight(strings.TrimSpace(hubURL), "/"),
		Status: desktopNodeStatusRegistering,
	})
	if err != nil {
		releaseDesktopNodeJob(id)
		return nil, err
	}
	err = a.registerDirectDesktopNode(rec)
	releaseDesktopNodeJob(id)
	if err != nil {
		return nil, err
	}
	out, err := a.findDesktopNode(rec.ID)
	return &out, err
}

// ActivateDesktopNode makes this desktopd the one this user's desktop uses.
func (a *App) ActivateDesktopNode(id string) error {
	serverID, err := a.desktopNodeServerID(id)
	if err != nil {
		return err
	}
	var out hubSelfDesktopView
	if err := a.desktopBotCall(context.Background(), http.MethodPut, "/api/v1/bots/desktop-node/"+url.PathEscape(serverID)+"/active", map[string]string{}, &out, 30*time.Second); err != nil {
		return err
	}
	a.emitEvent(desktopNodesEvent, id)
	return nil
}

// DeleteDesktopNode unregisters the desktopd on Hub and forgets the record.
// The desktopd itself keeps running on its machine.
func (a *App) DeleteDesktopNode(id string) error {
	rec, recErr := a.findDesktopNode(id)
	serverID := ""
	if recErr == nil {
		serverID = rec.ServerID
		if serverID == "" && rec.NodeID != "" {
			serverID, _ = a.hubServerIDForNode(rec.NodeID)
		}
	} else if strings.HasPrefix(id, "hub:") {
		serverID = strings.TrimPrefix(id, "hub:")
	} else {
		return recErr
	}
	if serverID != "" {
		var out hubSelfDesktopView
		if err := a.desktopBotCall(context.Background(), http.MethodDelete, "/api/v1/bots/desktop-node/"+url.PathEscape(serverID), nil, &out, 30*time.Second); err != nil {
			return err
		}
	}
	if recErr == nil {
		if rec.Mode == "direct" && rec.DesktopdURL != "" && rec.Token != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = desktopdHubLinkCall(ctx, http.DefaultClient, rec.DesktopdURL, rec.Token, http.MethodDelete, nil, nil)
			cancel()
		}
		desktopNodesMu.Lock()
		records, err := a.loadDesktopNodeRecords()
		if err == nil {
			kept := records[:0]
			for _, item := range records {
				if item.ID != rec.ID {
					kept = append(kept, item)
				}
			}
			err = a.saveDesktopNodeRecords(kept)
		}
		desktopNodesMu.Unlock()
		if err != nil {
			return err
		}
	}
	a.emitEvent(desktopNodesEvent, id)
	return nil
}

type desktopNodesBackup struct {
	Kind       string              `json:"kind"`
	Version    int                 `json:"version"`
	ExportedAt string              `json:"exported_at"`
	Nodes      []DesktopNodeRecord `json:"nodes"`
}

// ExportDesktopNodes writes every desktopd record, tokens included, to a file
// the user picks. The SSH password is never part of it.
func (a *App) ExportDesktopNodes() (string, error) {
	view, err := a.ListDesktopNodes()
	if err != nil {
		return "", err
	}
	backup := desktopNodesBackup{Kind: "maclaw-desktop-nodes", Version: 1, ExportedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, entry := range view.Nodes {
		backup.Nodes = append(backup.Nodes, entry.DesktopNodeRecord)
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "备份 desktopd 注册信息",
		DefaultFilename: "maclaw-desktopd-" + time.Now().Format("20060102") + ".json",
		Filters:         []runtime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil || strings.TrimSpace(dest) == "" {
		return "", err
	}
	raw, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dest, append(raw, '\n'), 0o600); err != nil {
		return "", err
	}
	return dest, nil
}

// ImportDesktopNodes merges a backup into this GUI's records. A desktop
// already known (same node, or same SSH target / URL) is updated in place.
func (a *App) ImportDesktopNodes() (int, error) {
	src, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "导入 desktopd 注册信息",
		Filters: []runtime.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}},
	})
	if err != nil || strings.TrimSpace(src) == "" {
		return 0, err
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	return a.importDesktopNodes(raw)
}

func (a *App) importDesktopNodes(raw []byte) (int, error) {
	var backup desktopNodesBackup
	if err := json.Unmarshal(raw, &backup); err != nil || backup.Kind != "maclaw-desktop-nodes" {
		return 0, fmt.Errorf("不是 desktopd 备份文件")
	}
	desktopNodesMu.Lock()
	records, err := a.loadDesktopNodeRecords()
	if err != nil {
		desktopNodesMu.Unlock()
		return 0, err
	}
	count := 0
	now := time.Now().UTC().Format(time.RFC3339)
	for _, in := range backup.Nodes {
		if in.Mode != "ssh" && in.Mode != "direct" && in.NodeID == "" {
			continue
		}
		in.Status = desktopNodeStatusImported
		in.Message = ""
		in.TaskPath = ""
		in.UpdatedAt = now
		matched := false
		for i := range records {
			if sameDesktopNode(records[i], in) {
				in.ID = records[i].ID
				records[i] = in
				matched = true
				break
			}
		}
		if !matched {
			if in.ID == "" || strings.HasPrefix(in.ID, "hub:") {
				in.ID = newDesktopNodeID()
			}
			records = append(records, in)
		}
		count++
	}
	if err := a.saveDesktopNodeRecords(records); err != nil {
		desktopNodesMu.Unlock()
		return 0, err
	}
	desktopNodesMu.Unlock()
	a.emitEvent(desktopNodesEvent, "")
	return count, nil
}

func sameDesktopNode(a, b DesktopNodeRecord) bool {
	if a.NodeID != "" && a.NodeID == b.NodeID {
		return true
	}
	if a.Mode == "ssh" && b.Mode == "ssh" && a.Host == b.Host && a.Port == b.Port && a.User == b.User {
		return true
	}
	return a.Mode == "direct" && b.Mode == "direct" && a.DesktopdURL != "" && a.DesktopdURL == b.DesktopdURL
}

func (a *App) findDesktopNode(id string) (DesktopNodeRecord, error) {
	desktopNodesMu.Lock()
	defer desktopNodesMu.Unlock()
	records, err := a.loadDesktopNodeRecords()
	if err != nil {
		return DesktopNodeRecord{}, err
	}
	for _, rec := range records {
		if rec.ID == id {
			return rec, nil
		}
	}
	return DesktopNodeRecord{}, fmt.Errorf("desktopd 记录不存在")
}

func (a *App) desktopNodeServerID(id string) (string, error) {
	if strings.HasPrefix(id, "hub:") {
		return strings.TrimPrefix(id, "hub:"), nil
	}
	rec, err := a.findDesktopNode(id)
	if err != nil {
		return "", err
	}
	if rec.NodeID == "" {
		return "", fmt.Errorf("这台 desktopd 还没有注册到 Hub")
	}
	return a.hubServerIDForNode(rec.NodeID)
}

func (a *App) hubServerIDForNode(nodeID string) (string, error) {
	var hub hubSelfDesktopView
	if err := a.desktopBotCall(context.Background(), http.MethodGet, "/api/v1/bots/desktop-node", nil, &hub, 20*time.Second); err != nil {
		return "", err
	}
	for _, node := range hub.Nodes {
		if node.NodeID == nodeID {
			return node.ServerID, nil
		}
	}
	return "", fmt.Errorf("Hub 上没有这台 desktopd 的注册，请重新注册")
}

// ---------------------------------------------------------------------------
// Deploy job
// ---------------------------------------------------------------------------

// desktopNodeDeployBudget bounds one deploy: the image build alone can take
// half an hour on a slow mirror.
var desktopNodeDeployBudget = 90 * time.Minute

// desktopNodeProbeEvery is how often the job checks the target while the
// agent works. A healthy desktopd is registered without waiting for the turn.
var desktopNodeProbeEvery = 45 * time.Second

// desktopNodeJob is one agent run against an SSH desktopd. An upgrade waits
// for a stamp file written after the new build is healthy: the previous
// desktopd already answers /v1/health, so that check alone cannot mean the
// upgrade finished.
type desktopNodeJob struct {
	kind  string
	stamp string
}

func (j desktopNodeJob) upgrade() bool { return j.kind == "upgrade" }

// One job per desktop. A second run would replace the in-memory SSH label
// and then delete it while the first agent is still connected.
var desktopNodeJobs sync.Map

func claimDesktopNodeJob(id string) bool {
	_, loaded := desktopNodeJobs.LoadOrStore(id, struct{}{})
	return !loaded
}

func releaseDesktopNodeJob(id string) { desktopNodeJobs.Delete(id) }

func desktopNodeJobLive(id string) bool {
	_, ok := desktopNodeJobs.Load(id)
	return ok
}

func desktopNodeBusyStatus(status string) bool {
	return status == desktopNodeStatusDeploying || status == desktopNodeStatusUpgrading || status == desktopNodeStatusRegistering
}

// settleDesktopNodeJobs marks a busy record failed when no job in this process
// owns it. That is what a crash or a killed app leaves behind.
func settleDesktopNodeJobs(records []DesktopNodeRecord) ([]DesktopNodeRecord, bool) {
	changed := false
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range records {
		if desktopNodeBusyStatus(records[i].Status) && !desktopNodeJobLive(records[i].ID) {
			kind := "deploy"
			if records[i].Status == desktopNodeStatusUpgrading {
				kind = "upgrade"
			}
			records[i].Status = desktopNodeStatusFailed
			records[i].Message = "上次部署或更新已中断，请重新开始"
			records[i].JobStartedAt = ""
			recordDesktopNodeResult(&records[i], kind, records[i].Message)
			records[i].UpdatedAt = now
			changed = true
		}
	}
	return records, changed
}

// desktopNodeFatal is a failure another probe will not fix.
func desktopNodeFatal(err error) bool {
	if err == nil {
		return false
	}
	var reg desktopNodeRegisterError
	if errors.As(err, &reg) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "还没有") && strings.Contains(msg, ".env") {
		return false
	}
	// The install is still running, or this probe is only a progress check.
	if strings.Contains(msg, "正在安装") || strings.Contains(msg, "还未完成") {
		return false
	}
	return !strings.Contains(msg, "不可达") && !strings.Contains(msg, "正在重新连接") && !strings.Contains(msg, "正在重启")
}

var desktopNodeDetailMu sync.Mutex

var desktopNodeDetailSecret = regexp.MustCompile(`(?i)(password|DESKTOPD_TOKEN|MACLAW_ADMIN_SECRET|MACLAW_TOKEN_SECRET|MACLAW_DESKTOP_API_TOKEN|hub_token|node_secret|srv_admin_secret|desktopd_key|authorization)\s*[:=]\s*(?:"[^"]*"|'[^']*'|Bearer\s+\S+|\S+)`)
var desktopNodeDetailBearer = regexp.MustCompile(`(?i)Bearer\s+\S+`)
var desktopNodeDetailUserinfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@:]+:[^\s/@]+@`)
var desktopNodeDetailBlob = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)

func scrubDesktopNodeDetail(text string) string {
	text = strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
	text = desktopNodeDetailBearer.ReplaceAllString(text, "Bearer <redacted>")
	text = desktopNodeDetailSecret.ReplaceAllString(text, "$1=<redacted>")
	text = desktopNodeDetailUserinfo.ReplaceAllString(text, "$1<redacted>@")
	text = desktopNodeDetailBlob.ReplaceAllString(text, "<redacted>")
	if len([]rune(text)) > 2000 {
		text = string([]rune(text)[:2000]) + "..."
	}
	return strings.TrimSpace(text)
}

// desktopNodeDetail appends one deploy step when detailed logging is on.
// It goes to logs/desktop-node-detail.log and never keeps a secret.
func (a *App) desktopNodeDetail(id, step, detail string) {
	if !corelib.IsLogDetailEnabled() {
		return
	}
	dir := a.desktopNodeDetailDir()
	if dir == "" {
		return
	}
	path := filepath.Join(dir, "desktop-node-detail.log")
	line := fmt.Sprintf("%s id=%s step=%s %s\n", time.Now().Format("2006-01-02 15:04:05"), scrubDesktopNodeDetail(id), scrubDesktopNodeDetail(step), scrubDesktopNodeDetail(detail))
	desktopNodeDetailMu.Lock()
	defer desktopNodeDetailMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	flag := os.O_APPEND | os.O_CREATE | os.O_WRONLY
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > 5*1024*1024 {
		flag = os.O_TRUNC | os.O_CREATE | os.O_WRONLY
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if flag&os.O_TRUNC != 0 {
		_, _ = fmt.Fprintln(f, "[log truncated: exceeded 5MB]")
	}
	_, _ = f.WriteString(line)
}

// desktopNodeDetailDir is the same logs directory as maclaw.log.
// A test app keeps the file under its temporary home.
func (a *App) desktopNodeDetailDir() string {
	if a != nil && strings.TrimSpace(a.testHomeDir) != "" {
		return filepath.Join(a.getMaclawBaseDir(), "logs")
	}
	if dir := strings.TrimSpace(corelib.MaclawLogsDir()); dir != "" {
		return dir
	}
	if a != nil {
		if base := strings.TrimSpace(a.getMaclawBaseDir()); base != "" {
			return filepath.Join(base, "logs")
		}
	}
	if base := strings.TrimSpace(corelib.MaclawBaseDir()); base != "" {
		return filepath.Join(base, "logs")
	}
	return ""
}

func (a *App) runDesktopNodeJob(rec DesktopNodeRecord, password string, privilege desktopNodePrivilege, job desktopNodeJob) {
	label := "maclaw-desktopd-" + strings.TrimPrefix(rec.ID, "dn_")
	registerEphemeralSSHHost(corelib.SSHHostEntry{
		Label: label, Host: rec.Host, Port: rec.Port, User: rec.User,
		AuthMethod: "password", Password: password,
	})
	cleanupPrivilege := func() {}
	if privilege.tempSudoers != "" {
		cleanupPrivilege = func() {
			conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
			if err != nil {
				log.Printf("[desktop-node] could not remove temporary sudoers id=%s err=%s", rec.ID, scrubDesktopNodeDetail(err.Error()))
				a.desktopNodeDetail(rec.ID, "sudoers", err.Error())
				return
			}
			defer conn.Close()
			if _, rmErr := conn.run(context.Background(), "rm -f "+privilege.tempSudoers, desktopNodePrivilege{su: true}, password); rmErr != nil {
				log.Printf("[desktop-node] could not remove temporary sudoers id=%s err=%s", rec.ID, scrubDesktopNodeDetail(rmErr.Error()))
				a.desktopNodeDetail(rec.ID, "sudoers", rmErr.Error())
			}
		}
	}
	defer func() {
		unregisterEphemeralSSHHost(label)
		cleanupPrivilege()
	}()

	if job.upgrade() {
		a.clearDesktopNodeStamp(rec, password, privilege)
	}
	if strings.TrimSpace(rec.HubURL) == "" {
		hub, _, hubErr := a.getHubCredentials()
		if strings.TrimSpace(hub) != "" {
			rec.HubURL = strings.TrimRight(strings.TrimSpace(hub), "/")
		} else if hubErr != nil {
			a.desktopNodeDetail(rec.ID, "hub", hubErr.Error())
		}
	}
	taskName := "部署 desktopd · " + rec.User + "@" + rec.Host
	prompt := desktopNodeDeployPrompt(rec, label, privilege)
	display := fmt.Sprintf("〔一键部署〕在 %s@%s 上部署 desktopd", rec.User, rec.Host)
	if rec.WithSrv {
		taskName = "部署 desktopd 和 MaClawSrv · " + rec.User + "@" + rec.Host
		display = fmt.Sprintf("〔一键部署〕在 %s@%s 上部署 desktopd 和 MaClawSrv", rec.User, rec.Host)
	}
	if job.upgrade() {
		taskName = "升级 desktopd · " + rec.User + "@" + rec.Host
		prompt = desktopNodeUpgradePrompt(rec, label, privilege, job.stamp)
		display = fmt.Sprintf("〔升级 desktopd〕在 %s@%s 上用最新代码重新编译部署", rec.User, rec.Host)
	}
	task := a.CreateTask(taskName, "")
	if strings.TrimSpace(task.ProjectPath) == "" {
		a.desktopNodeDetail(rec.ID, "task", "无法创建部署任务")
		a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, "无法创建部署任务")
		return
	}
	_, _ = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) { r.TaskPath = task.ProjectPath })
	a.desktopNodeDetail(rec.ID, "start", fmt.Sprintf("host=%s port=%d user=%s with_srv=%v upgrade=%v hub=%s doc=%s label=%s task=%s", rec.Host, rec.Port, rec.User, rec.WithSrv, job.upgrade(), rec.HubURL, desktopNodeDocURL(rec.HubURL), label, task.ProjectPath))
	done := make(chan *IMAgentResponse, 1)
	_, err := a.SendAIAssistantMessage(AIAssistantSendRequest{
		Text:               prompt,
		ProjectPath:        task.ProjectPath,
		BackendDisplayText: display,
		OnDone: func(resp *IMAgentResponse) {
			select {
			case done <- resp:
			default:
			}
		},
	})
	if err != nil {
		a.desktopNodeDetail(rec.ID, "agent", err.Error())
		a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, "agent 启动失败: "+err.Error())
		return
	}

	deadline := time.Now().Add(desktopNodeDeployBudget)
	var agentDoneAt time.Time
	agentSummary := ""
	stampSeen := !job.upgrade()
	lastProbe := ""
	ticker := time.NewTicker(desktopNodeProbeEvery)
	defer ticker.Stop()
	for {
		select {
		case resp := <-done:
			agentDoneAt = time.Now()
			if resp != nil {
				agentSummary = desktopNodeAgentVerdict(resp)
			}
			if agentSummary == "" {
				a.desktopNodeDetail(rec.ID, "agent-done", "没有 DESKTOPD_DEPLOY 结论")
			} else {
				a.desktopNodeDetail(rec.ID, "agent-done", agentSummary)
			}
		case <-ticker.C:
		}
		var err error
		if job.upgrade() && !stampSeen {
			seen, probeErr := a.noteDesktopNodeStamp(rec, password, privilege, job.stamp)
			if seen {
				stampSeen = true
				a.setDesktopNodeStatus(rec.ID, desktopNodeStatusUpgrading, "正在验证升级后的 desktopd")
				err = probeErr
			} else if probeErr != nil {
				err = probeErr
			} else {
				err = fmt.Errorf("升级还未完成")
			}
		} else if agentDoneAt.IsZero() {
			// Do not rewrite .env or enroll while the install script may still
			// be writing that file. A half-old desktopd answering health is
			// not the process we just built.
			err = fmt.Errorf("desktopd 正在安装")
		} else {
			err = a.finishDesktopNodeDeploy(rec, password, privilege, job.upgrade())
		}
		if err == nil {
			ready := "desktopd 已就绪"
			if latest, findErr := a.findDesktopNode(rec.ID); findErr == nil && strings.TrimSpace(latest.NodeID) != "" {
				ready += " node=" + latest.NodeID
			}
			a.desktopNodeDetail(rec.ID, "ready", ready)
			return
		}
		failed := strings.HasPrefix(strings.ToUpper(agentSummary), "FAILED")
		grace := 10 * time.Minute
		if failed {
			grace = 30 * time.Second
		}
		givingUp := desktopNodeFatal(err) || (!agentDoneAt.IsZero() && time.Since(agentDoneAt) > grace) || time.Now().After(deadline)
		if givingUp {
			msg := err.Error()
			if !desktopNodeFatal(err) {
				msg = "desktopd 没有就绪: " + err.Error()
				if agentSummary != "" {
					msg += "；agent: " + agentSummary
				}
			}
			msg = scrubDesktopNodeDetail(msg)
			a.desktopNodeDetail(rec.ID, "failed", msg)
			a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, msg)
			return
		}
		if msg := err.Error(); msg != lastProbe {
			lastProbe = msg
			step := "probe"
			if strings.Contains(msg, "正在安装") || strings.Contains(msg, "还未完成") || strings.Contains(msg, "正在重启") || strings.Contains(msg, "正在重新连接") {
				step = "wait"
			}
			a.desktopNodeDetail(rec.ID, step, msg)
		}
		if !agentDoneAt.IsZero() {
			status := desktopNodeStatusDeploying
			message := "agent 已结束，等待 desktopd 就绪"
			if job.upgrade() {
				status = desktopNodeStatusUpgrading
				message = "agent 已结束，等待升级完成"
			}
			a.setDesktopNodeStatus(rec.ID, status, message)
		}
	}
}

// desktopNodeAgentVerdict is the agent's last DESKTOPD_DEPLOY line, if any.
func desktopNodeAgentVerdict(resp *IMAgentResponse) string {
	text := resp.Text
	if resp.Error != "" {
		text += "\n" + resp.Error
	}
	verdict := ""
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "DESKTOPD_DEPLOY:"); i >= 0 {
			verdict = strings.TrimSpace(line[i+len("DESKTOPD_DEPLOY:"):])
		}
	}
	if verdict == "" && resp.Error != "" {
		verdict = resp.Error
	}
	if len([]rune(verdict)) > 300 {
		verdict = string([]rune(verdict)[:300])
	}
	return verdict
}

// desktopNodeRegisterError is a failure after desktopd was found healthy:
// probing again will not fix it.
type desktopNodeRegisterError struct{ msg string }

func (e desktopNodeRegisterError) Error() string { return e.msg }

// finishDesktopNodeDeploy checks the target over SSH, reads the desktopd
// token and registers it on Hub. A not-yet-healthy desktopd is a plain error.
// preserveLink keeps an upgrade on the node this desktopd already enrolled:
// restarting the service reconnects that link, and a new enrollment would
// replace the desktop this user is on.
// noteDesktopNodeStamp reads the upgrade stamp and, when it matches, checks
// the new process on the same SSH connection.
func (a *App) noteDesktopNodeStamp(rec DesktopNodeRecord, password string, privilege desktopNodePrivilege, stamp string) (bool, error) {
	if stamp == "" {
		return false, nil
	}
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	dir, _ := desktopNodeDeployPaths(rec)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	out, err := conn.run(ctx, "cat "+shellQuote(dir+"/.upgrade-stamp"), privilege, password)
	cancel()
	if err != nil {
		return false, fmt.Errorf("升级还未完成: %s", scrubDesktopNodeDetail(err.Error()))
	}
	if strings.TrimSpace(out) != stamp {
		return false, fmt.Errorf("升级还未完成: 标记还不是这次升级")
	}
	return true, a.finishDesktopNodeConn(conn, rec, password, privilege, true)
}

func (a *App) finishDesktopNodeDeploy(rec DesktopNodeRecord, password string, privilege desktopNodePrivilege, preserveLink bool) error {
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return err
	}
	defer conn.Close()
	return a.finishDesktopNodeConn(conn, rec, password, privilege, preserveLink)
}

func (a *App) finishDesktopNodeConn(conn *desktopNodeSSH, rec DesktopNodeRecord, password string, privilege desktopNodePrivilege, preserveLink bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	deployDir := rec.DeployDir
	if deployDir == "" {
		deployDir = desktopNodeDeployDir
	}
	envFile, envText, err := readDesktopNodeEnv(conn, ctx, deployDir, privilege, password)
	if err != nil {
		return err
	}
	env := parseDotEnv(envText)
	if !desktopNodeIntranetReady(env) {
		// The install script or an older copy of the runbook may have stored a
		// public name. Fix that file here so the command never contains the token.
		if _, err := conn.run(ctx, intranetDesktopEnvFileCommand(envFile), privilege, password); err != nil {
			return fmt.Errorf("desktopd 正在重启为内网桌面: %v", err)
		}
		return fmt.Errorf("desktopd 正在重启为内网桌面")
	}
	token := env["DESKTOPD_TOKEN"]
	if token == "" {
		return fmt.Errorf(".env 里没有 DESKTOPD_TOKEN")
	}
	port := rec.DesktopdPort
	if _, p, err := net.SplitHostPort(env["DESKTOPD_ADDR"]); err == nil {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			port = n
		}
	}
	if port <= 0 {
		port = desktopNodeDesktopdPort
	}
	client := conn.httpClient()
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	if err := desktopdHealth(ctx, client, base, token); err != nil {
		return err
	}
	if latest, findErr := a.findDesktopNode(rec.ID); findErr == nil {
		rec.WithSrv = latest.WithSrv
		rec.SrvTokenSet = latest.SrvTokenSet
		if latest.NodeID != "" {
			rec.NodeID = latest.NodeID
		}
	}
	srvAdmin := ""
	if rec.WithSrv {
		admin, err := a.ensureRemoteSrvAdmin(conn, rec, password, privilege)
		if err != nil {
			return err
		}
		srvAdmin = admin
	}
	if preserveLink && strings.TrimSpace(rec.NodeID) != "" {
		var link struct {
			Enrolled bool   `json:"enrolled"`
			NodeID   string `json:"node_id"`
			Online   bool   `json:"online"`
			Error    string `json:"error"`
		}
		if err := desktopdHubLinkCall(ctx, client, base, token, http.MethodGet, nil, &link); err != nil {
			return err
		}
		if link.Enrolled && link.NodeID != "" {
			if !link.Online {
				return fmt.Errorf("desktopd 已升级，正在重新连接 Hub")
			}
			if rec.WithSrv && !rec.SrvTokenSet {
				rec.NodeID = link.NodeID
				rec.Token = token
				rec.DesktopdPort = port
				return a.writeDesktopNodeSrvToken(conn, rec, password, privilege)
			}
			_, err := a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
				r.Token = token
				r.DesktopdPort = port
				r.NodeID = link.NodeID
				r.Status = desktopNodeStatusReady
				r.Message = ""
				kind := "deploy"
				if preserveLink {
					kind = "upgrade"
				}
				recordDesktopNodeResult(r, kind, "")
			})
			return err
		}
	}
	if strings.TrimSpace(rec.NodeID) != "" {
		if rec.WithSrv && !rec.SrvTokenSet {
			rec.Token = token
			rec.DesktopdPort = port
			return a.writeDesktopNodeSrvToken(conn, rec, password, privilege)
		}
		_, err := a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
			r.Token = token
			r.DesktopdPort = port
			r.Status = desktopNodeStatusReady
			r.Message = ""
			kind := "deploy"
			if preserveLink {
				kind = "upgrade"
			}
			recordDesktopNodeResult(r, kind, "")
		})
		return err
	}
	_, _ = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.Token = token
		r.DesktopdPort = port
		r.Status = desktopNodeStatusRegistering
		r.Message = "desktopd 已就绪，正在注册到 Hub"
	})
	nodeID, err := a.enrollDesktopd(ctx, client, base, token, rec, srvAdmin)
	if err != nil {
		return err
	}
	if rec.WithSrv {
		rec.NodeID = nodeID
		rec.Token = token
		rec.DesktopdPort = port
		_, _ = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
			r.NodeID = nodeID
			r.Status = desktopNodeStatusRegistering
			r.Message = "正在写入 srv 令牌"
		})
		return a.writeDesktopNodeSrvToken(conn, rec, password, privilege)
	}
	_, _ = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.NodeID = nodeID
		r.Status = desktopNodeStatusReady
		r.Message = ""
		recordDesktopNodeResult(r, "deploy", "")
	})
	log.Printf("[desktop-node] registered id=%s node=%s host=%s", rec.ID, nodeID, rec.Host)
	return nil
}

func (a *App) registerDirectDesktopNode(rec DesktopNodeRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 60 * time.Second}
	if err := desktopdHealth(ctx, client, rec.DesktopdURL, rec.Token); err != nil {
		a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, err.Error())
		return err
	}
	nodeID, err := a.enrollDesktopd(ctx, client, rec.DesktopdURL, rec.Token, rec, "")
	if err != nil {
		a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, err.Error())
		return err
	}
	_, err = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.NodeID = nodeID
		r.Status = desktopNodeStatusReady
		r.Message = ""
		recordDesktopNodeResult(r, "deploy", "")
	})
	return err
}

// enrollDesktopd asks Hub for a personal code and hands it to desktopd, which
// enrolls itself and opens its tunnel. The old node of this record, if any,
// is unregistered once the new one is up.
func (a *App) enrollDesktopd(ctx context.Context, client *http.Client, base, token string, rec DesktopNodeRecord, srvAdmin string) (string, error) {
	hubURL, _, err := a.getHubCredentials()
	if err != nil || strings.TrimSpace(hubURL) == "" {
		return "", fmt.Errorf("还没有连接 Hub")
	}
	var code struct {
		Code   string `json:"code"`
		NodeID string `json:"node_id"`
	}
	a.desktopNodeDetail(rec.ID, "enroll", "hub="+strings.TrimRight(strings.TrimSpace(hubURL), "/")+" host="+rec.Host)
	if err := a.desktopBotCall(ctx, http.MethodPost, "/api/v1/bots/desktop-node/code", map[string]string{"name": rec.Name}, &code, 20*time.Second); err != nil {
		a.desktopNodeDetail(rec.ID, "enroll-code", err.Error())
		return "", fmt.Errorf("Hub 拒绝了桌面注册: %v", err)
	}
	var status struct {
		Enrolled bool   `json:"enrolled"`
		NodeID   string `json:"node_id"`
		Online   bool   `json:"online"`
		Error    string `json:"error"`
	}
	body := map[string]any{
		"hub_url":     strings.TrimRight(strings.TrimSpace(hubURL), "/"),
		"code":        code.Code,
		"direct_base": strings.TrimSpace(base),
	}
	if strings.TrimSpace(srvAdmin) != "" {
		body["has_srv"] = true
		body["srv_addr"] = desktopNodeSrvAddr
		body["srv_admin_secret"] = srvAdmin
	}
	if err := desktopdHubLinkCall(ctx, client, base, token, http.MethodPost, body, &status); err != nil {
		a.desktopNodeDetail(rec.ID, "enroll-link", err.Error())
		return "", err
	}
	if !status.Enrolled || status.NodeID == "" {
		msg := "desktopd 没有完成注册"
		if status.Error != "" {
			msg += ": " + status.Error
		}
		a.desktopNodeDetail(rec.ID, "enroll-link", msg)
		return "", errors.New(msg)
	}
	// The link comes up a moment after enrollment.
	for i := 0; i < 20 && !status.Online; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
		_ = desktopdHubLinkCall(ctx, client, base, token, http.MethodGet, nil, &status)
	}
	if !status.Online {
		msg := "desktopd 已注册，但还没有连上 Hub"
		if status.Error != "" {
			msg += ": " + status.Error
		}
		a.desktopNodeDetail(rec.ID, "enroll-online", "node="+status.NodeID+" "+msg)
		return status.NodeID, desktopNodeRegisterError{msg: msg}
	}
	if old := strings.TrimSpace(rec.NodeID); old != "" && old != status.NodeID {
		if serverID, err := a.hubServerIDForNode(old); err == nil {
			_ = a.desktopBotCall(ctx, http.MethodDelete, "/api/v1/bots/desktop-node/"+url.PathEscape(serverID), nil, nil, 20*time.Second)
		}
	}
	return status.NodeID, nil
}

func desktopdHealth(ctx context.Context, client *http.Client, base, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/v1/health", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("desktopd 不可达")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return desktopNodeRegisterError{msg: "desktopd 拒绝了访问令牌"}
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("desktopd 健康检查失败: HTTP %d", resp.StatusCode)
	}
	return nil
}

func desktopdHubLinkCall(ctx context.Context, client *http.Client, base, token, method string, body any, dest any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+"/v1/hub/link", reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("desktopd 不可达")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return desktopNodeRegisterError{msg: "这台 desktopd 版本太旧，不支持连接 Hub（需要 /v1/hub/link），请用最新源码重新部署"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &failure)
		if failure.Message == "" {
			failure.Message = resp.Status
		}
		return desktopNodeRegisterError{msg: "desktopd: " + failure.Message}
	}
	if dest != nil {
		return json.Unmarshal(raw, dest)
	}
	return nil
}

func normalizeDesktopdURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", fmt.Errorf("desktopd 地址应为 http://IP:18081 这样的形式")
	}
	if u.Port() == "" && u.Scheme == "http" {
		u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(desktopNodeDesktopdPort))
	}
	u.RawQuery, u.Fragment = "", ""
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// desktopNodeIntranetReady reports an env file that already describes an
// intranet desktop: no public name, and DESKTOPD_INTRANET set.
func desktopNodeIntranetReady(env map[string]string) bool {
	if strings.TrimSpace(env["DESKTOPD_ADVERTISE_HOST"]) != "" || strings.TrimSpace(env["DESKTOPD_PUBLIC_URL"]) != "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(env["DESKTOPD_INTRANET"])) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// intranetDesktopEnvCommand removes a public name and sets the intranet
// flag, then restarts desktopd. It does not read or write the token.
func intranetDesktopEnvCommand(dir string) string {
	return intranetDesktopEnvFileCommand(dir + "/.env")
}

func intranetDesktopEnvFileCommand(path string) string {
	q := shellQuote(path)
	// Parentheses keep a failed edit from restarting, and a missing flag from
	// being treated as success before the line is appended.
	return "(sed -i -e '/^DESKTOPD_ADVERTISE_HOST=/d' -e '/^DESKTOPD_PUBLIC_URL=/d' -e 's/^DESKTOPD_INTRANET=.*/DESKTOPD_INTRANET=1/' " + q +
		" && grep -q '^DESKTOPD_INTRANET=1$' " + q +
		" || printf '\\nDESKTOPD_INTRANET=1\\n' >> " + q +
		") && chmod 600 " + q + " && systemctl restart maclaw-desktopd"
}

// readDesktopNodeEnv reads the install the agent just finished. The runbook
// writes /etc/maclaw/desktopd.env. Older GUI installs still use <dir>/.env.
func readDesktopNodeEnv(conn *desktopNodeSSH, ctx context.Context, deployDir string, privilege desktopNodePrivilege, password string) (path, text string, err error) {
	var notes []string
	for _, candidate := range []string{deployDir + "/.env", "/etc/maclaw/desktopd.env"} {
		body, catErr := conn.run(ctx, "cat "+shellQuote(candidate), privilege, password)
		if catErr == nil && strings.TrimSpace(body) != "" {
			return candidate, body, nil
		}
		if catErr != nil {
			notes = append(notes, candidate+": "+scrubDesktopNodeDetail(catErr.Error()))
		} else {
			notes = append(notes, candidate+": 文件是空的")
		}
	}
	if len(notes) > 0 {
		return "", "", fmt.Errorf("还没有 %s/.env: %s", deployDir, strings.Join(notes, "; "))
	}
	return "", "", fmt.Errorf("还没有 %s/.env", deployDir)
}

func parseDotEnv(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		out[strings.TrimSpace(key)] = value
	}
	return out
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// desktopNodeDeployPrompt is the task for the existing assistant agent. It
// names an SSH label, never the password, and tells the agent not to print
// the desktopd token: this GUI reads it over its own SSH connection.
func desktopNodePrivilegeLine(privilege desktopNodePrivilege, ending string) string {
	switch {
	case privilege.root:
		return "登录用户就是 root，直接执行即可。"
	case privilege.tempSudoers != "":
		return "登录用户已临时获得免密 sudo（" + ending + "结束后会自动撤销），需要 root 的命令用 sudo 执行。"
	default:
		return "登录用户可以 sudo：先调用 ssh 的 sudo_prepare（session_id 为 connect 返回的会话），之后需要 root 的命令用 sudo 执行。"
	}
}

func desktopNodeDocURL(hub string) string {
	hub = strings.TrimRight(strings.TrimSpace(hub), "/")
	if hub == "" {
		return ""
	}
	return hub + "/docs/self-desktopd"
}

func desktopNodeDeployPrompt(rec DesktopNodeRecord, label string, privilege desktopNodePrivilege) string {
	var b strings.Builder
	b.WriteString("请在下面这台内网 Linux 服务器上安装。安装步骤和配置以安装文档为准。\n\n")
	if rec.WithSrv {
		b.WriteString("这次的安装选择：一并安装 desktopd 和 MaClawSrv。按文档第 2 节和第 3 节做。不要创建 .env，不要生成密钥，不要启动 MaClawSrv。\n\n")
	} else {
		b.WriteString("这次的安装选择：只安装 desktopd。按文档第 2 节做到健康检查为止。不要安装 MaClawSrv，也不要安装 botnode。\n\n")
	}
	fmt.Fprintf(&b, "服务器：\n- 地址：%s\n- 端口：%d\n- 用户：%s\n- 提权：%s\n", rec.Host, rec.Port, rec.User, desktopNodePrivilegeLine(privilege, "部署"))
	fmt.Fprintf(&b, "- 连接：调用 ssh 工具 action=connect，label=%q（不要传 host/user/password，密码已由客户端托管）。\n\n", label)
	hub := strings.TrimRight(strings.TrimSpace(rec.HubURL), "/")
	fmt.Fprintf(&b, "当前登录的 Hub：%s\n", hub)
	fmt.Fprintf(&b, "安装文档：%s\n", desktopNodeDocURL(hub))
	b.WriteString("文档里的 HUB_URL 用上面这个 Hub 地址，不要用文档里的示例域名。\n")
	b.WriteString("不要输出或复述令牌和密钥。\n\n")
	b.WriteString("完成后最后一行输出 `DESKTOPD_DEPLOY: OK`；失败则输出 `DESKTOPD_DEPLOY: FAILED <原因>`。")
	return b.String()
}

func desktopNodeDeployMessage(withSrv bool) string {
	if withSrv {
		return "agent 正在部署 desktopd 和 MaClawSrv"
	}
	return "agent 正在部署 desktopd"
}

func (a *App) ensureRemoteSrvAdmin(conn *desktopNodeSSH, rec DesktopNodeRecord, password string, privilege desktopNodePrivilege) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := conn.run(ctx, "test -x "+shellQuote(desktopNodeSrvDir+"/bin/maclawsrv"), privilege, password); err != nil {
		return "", fmt.Errorf("maclawsrv 正在安装: %s", scrubDesktopNodeDetail(err.Error()))
	}
	raw, err := conn.run(ctx, "cat "+shellQuote(desktopNodeSrvDir+"/.env"), privilege, password)
	if err == nil {
		if admin := strings.TrimSpace(parseDotEnv(raw)["MACLAW_ADMIN_SECRET"]); admin != "" {
			return admin, nil
		}
	}
	admin, err := desktopNodeRandomHex(32)
	if err != nil {
		return "", err
	}
	tokenSecret, err := desktopNodeRandomHex(32)
	if err != nil {
		return "", err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(rec.HubURL), "/")
	if hubURL == "" {
		hubURL, _, _ = a.getHubCredentials()
		hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	}
	if _, err := conn.run(ctx, srvEnvWriteCommand(desktopNodeSrvDir, admin, tokenSecret, hubURL), privilege, password); err != nil {
		return "", fmt.Errorf("正在写入 srv 配置: %s", scrubDesktopNodeDetail(err.Error()))
	}
	return admin, nil
}

func (a *App) writeDesktopNodeSrvToken(conn *desktopNodeSSH, rec DesktopNodeRecord, password string, privilege desktopNodePrivilege) error {
	dir := rec.DeployDir
	if dir == "" {
		dir = desktopNodeDeployDir
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := conn.run(ctx, "cat "+shellQuote(dir+"/state/hub_link.json"), privilege, password)
	if err != nil {
		return fmt.Errorf("正在写入 srv 令牌: %s", scrubDesktopNodeDetail(err.Error()))
	}
	var creds struct {
		HubToken string `json:"hub_token"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &creds) != nil || strings.TrimSpace(creds.HubToken) == "" {
		return desktopNodeRegisterError{msg: "Hub 没有签发 srv 令牌"}
	}
	if _, err := conn.run(ctx, srvHubTokenWriteCommand(desktopNodeSrvDir, strings.TrimSpace(creds.HubToken)), privilege, password); err != nil {
		return fmt.Errorf("正在写入 srv 令牌: %s", scrubDesktopNodeDetail(err.Error()))
	}
	_, err = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		if rec.Token != "" {
			r.Token = rec.Token
		}
		if rec.DesktopdPort > 0 {
			r.DesktopdPort = rec.DesktopdPort
		}
		if rec.NodeID != "" {
			r.NodeID = rec.NodeID
		}
		kind := desktopNodeOutcomeKind(r.Status)
		r.SrvTokenSet = true
		r.Status = desktopNodeStatusReady
		r.Message = ""
		recordDesktopNodeResult(r, kind, "")
	})
	if err != nil {
		return err
	}
	log.Printf("[desktop-node] srv token configured id=%s host=%s", rec.ID, rec.Host)
	return nil
}

func desktopNodeRandomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func srvEnvWriteCommand(dir, admin, tokenSecret, hubURL string) string {
	payload := base64.StdEncoding.EncodeToString([]byte(admin + "\n" + tokenSecret + "\n" + hubURL))
	return fmt.Sprintf(`python3 -c 'import base64,pathlib,os
p=pathlib.Path(%q)/".env"
admin,token_secret,hub=base64.b64decode(%q).decode().split("\n",2)
p.parent.mkdir(parents=True, exist_ok=True)
(p.parent/"data").mkdir(exist_ok=True)
(p.parent/"logs").mkdir(exist_ok=True)
keys={"MACLAW_ADMIN_SECRET":admin,"MACLAW_TOKEN_SECRET":token_secret,"MACLAW_HTTP_ADDR":"127.0.0.1:18080","MACLAW_DATA_ROOT":str(p.parent/"data"),"MACLAW_HUB_URL":hub,"MACLAW_ALLOW_INSECURE_HTTP":"true"}
lines=p.read_text().splitlines() if p.exists() else []
seen=set(); out=[]
for line in lines:
    key=line.split("=",1)[0]
    if key in keys:
        out.append(key+"="+keys[key]); seen.add(key)
    else:
        out.append(line)
for key,value in keys.items():
    if key not in seen:
        out.append(key+"="+value)
p.write_text("\n".join(out)+"\n")
os.chmod(p, 0o600)
'`, dir, payload)
}

func srvHubTokenWriteCommand(dir, token string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(token))
	return fmt.Sprintf(`python3 -c 'import base64,pathlib,os
p=pathlib.Path(%q)/".env"
t=base64.b64decode(%q).decode()
lines=p.read_text().splitlines() if p.exists() else []
out=[]; seen=False
for line in lines:
    if line.startswith("MACLAW_DESKTOP_API_TOKEN="):
        out.append("MACLAW_DESKTOP_API_TOKEN="+t); seen=True
    else:
        out.append(line)
if not seen:
    out.append("MACLAW_DESKTOP_API_TOKEN="+t)
text="\n".join(out)+"\n"
p.write_text(text)
os.chmod(p, 0o600)
unit=pathlib.Path("/etc/systemd/system/maclawsrv.service")
unit.write_text("""[Unit]
Description=MaClawSrv
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
EnvironmentFile=%s/.env
ExecStart=%s/bin/maclawsrv
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
""")
' && { systemctl daemon-reload; systemctl enable maclawsrv.service; systemctl restart maclawsrv; }`, dir, encoded, dir, dir, dir)
}

func newDesktopNodeStamp() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

func desktopNodeDeployPaths(rec DesktopNodeRecord) (dir string, port int) {
	dir = rec.DeployDir
	if dir == "" {
		dir = desktopNodeDeployDir
	}
	port = rec.DesktopdPort
	if port <= 0 {
		port = desktopNodeDesktopdPort
	}
	return dir, port
}

// desktopNodeUpgradePrompt tells the existing assistant to rebuild desktopd
// from the latest source on a machine that already runs it. The stamp is not
// a secret: the GUI treats the file as the sign that this upgrade, not the
// previous process, is the one answering health checks.
func desktopNodeUpgradePrompt(rec DesktopNodeRecord, label string, privilege desktopNodePrivilege, stamp string) string {
	dir, _ := desktopNodeDeployPaths(rec)
	var b strings.Builder
	fmt.Fprintf(&b, "请把下面这台内网 Linux 服务器上已经在运行的 MaClaw desktopd 升级到文档里的最新代码。这是更新，不是新安装。\n\n")
	fmt.Fprintf(&b, "服务器：\n- 地址：%s\n- 端口：%d\n- 用户：%s\n- 提权：%s\n", rec.Host, rec.Port, rec.User, desktopNodePrivilegeLine(privilege, "升级"))
	fmt.Fprintf(&b, "- 连接：调用 ssh 工具 action=connect，label=%q（不要传 host/user/password，密码已由客户端托管）。\n\n", label)
	hub := strings.TrimRight(strings.TrimSpace(rec.HubURL), "/")
	fmt.Fprintf(&b, "当前登录的 Hub：%s\n", hub)
	fmt.Fprintf(&b, "安装文档：%s\n", desktopNodeDocURL(hub))
	b.WriteString("文档里的 HUB_URL 用上面这个 Hub 地址，不要用文档里的示例域名。更新步骤按文档第 4 节。\n\n")
	if rec.WithSrv {
		b.WriteString("这次一并更新 desktopd 和 MaClawSrv。两边都换成最新二进制。不要改 .env，不要重新生成密钥。\n")
	} else {
		b.WriteString("这次只更新 desktopd。不要安装 MaClawSrv，不要安装 botnode。不要改 .env。\n")
	}
	b.WriteString("必须保留现有 DESKTOPD_TOKEN、环境文件、state 里的 Hub 连接，以及已有桌面容器和用户数据。不要重新注册，不要删除无关容器。\n")
	fmt.Fprintf(&b, "开始前删除旧标记：rm -f %s/.upgrade-stamp\n", dir)
	fmt.Fprintf(&b, "desktopd 健康之后才写入完成标记，内容必须正好是这一行：printf '%%s\\n' %s > %s/.upgrade-stamp && chmod 600 %s/.upgrade-stamp\n", stamp, dir, dir)
	b.WriteString("不要输出或复述 DESKTOPD_TOKEN 和其他密钥。\n\n")
	b.WriteString("完成后最后一行输出 `DESKTOPD_DEPLOY: OK`；失败则输出 `DESKTOPD_DEPLOY: FAILED <原因>`。")
	return b.String()
}

func (a *App) clearDesktopNodeStamp(rec DesktopNodeRecord, password string, privilege desktopNodePrivilege) {
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return
	}
	defer conn.Close()
	dir, _ := desktopNodeDeployPaths(rec)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = conn.run(ctx, "rm -f "+shellQuote(dir+"/.upgrade-stamp"), privilege, password)
}

// ---------------------------------------------------------------------------
// SSH helper (GUI side only: probing, reading the token, enrolling)
// ---------------------------------------------------------------------------

type desktopNodeSSH struct {
	client   *ssh.Client
	user     string
	password string
	hostKey  string
}

// desktopNodePrivilege is how root commands run on the target.
type desktopNodePrivilege struct {
	root bool
	sudo bool
	su   bool
	// tempSudoers is a sudoers drop-in this job wrote through su so the agent
	// can use sudo; it is removed when the job ends.
	tempSudoers string
}

// dialDesktopNodeSSH logs in with a password. pin is the SHA256 host key seen
// on first deploy; a different key is refused.
func dialDesktopNodeSSH(host string, port int, user, password, pin string) (*desktopNodeSSH, error) {
	seen := ""
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			seen = ssh.FingerprintSHA256(key)
			if pin != "" && seen != pin {
				return fmt.Errorf("主机密钥变了（期望 %s，实际 %s）", pin, seen)
			}
			return nil
		},
		Timeout: 15 * time.Second,
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), cfg)
	if err != nil {
		return nil, err
	}
	return &desktopNodeSSH{client: client, user: user, password: password, hostKey: seen}, nil
}

func (c *desktopNodeSSH) Close() {
	if c != nil && c.client != nil {
		_ = c.client.Close()
	}
}

// httpClient reaches the target's loopback through this SSH connection.
func (c *desktopNodeSSH) httpClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
				return c.client.Dial(network, addr)
			},
			DisableKeepAlives: true,
		},
	}
}

// ensurePrivilege finds how root commands run. A user that cannot sudo but
// can su gets a temporary NOPASSWD sudoers entry so the agent can work.
func (c *desktopNodeSSH) ensurePrivilege() (desktopNodePrivilege, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := c.exec(ctx, "id -u", ""); err == nil && strings.TrimSpace(out) == "0" {
		return desktopNodePrivilege{root: true}, nil
	}
	password := c.password
	if out, err := c.exec(ctx, "sudo -S -p '' id -u", password+"\n"); err == nil && strings.TrimSpace(lastLine(out)) == "0" {
		return desktopNodePrivilege{sudo: true}, nil
	}
	if out, err := c.suExec(ctx, "id -u", password); err == nil && strings.TrimSpace(lastLine(out)) == "0" {
		path := "/etc/sudoers.d/zz-maclaw-desktopd-deploy"
		line := c.user + " ALL=(ALL) NOPASSWD:ALL"
		// The sleeper removes the drop-in even if this app exits first.
		// visudo failure deletes it instead of leaving sudo broken.
		expire := int((desktopNodeDeployBudget + time.Minute).Seconds())
		script := "printf '%s\\n' " + shellQuote(line) + " > " + path +
			" && chmod 440 " + path +
			" && if command -v visudo >/dev/null 2>&1 && ! visudo -cf " + path + "; then rm -f " + path + "; exit 1; fi" +
			" && command -v sudo >/dev/null" +
			" && nohup sh -c " + shellQuote("sleep "+strconv.Itoa(expire)+"; rm -f "+path) + " >/dev/null 2>&1 &"
		if _, err := c.suExec(ctx, script, password); err == nil {
			return desktopNodePrivilege{su: true, tempSudoers: path}, nil
		}
		return desktopNodePrivilege{}, fmt.Errorf("%s 可以 su，但主机没有可用的 sudo，agent 无法执行 root 命令", c.user)
	}
	return desktopNodePrivilege{}, fmt.Errorf("%s 既不是 root，也不能用这个密码 sudo 或 su 到 root", c.user)
}

// run executes cmd with root rights per privilege.
func (c *desktopNodeSSH) run(ctx context.Context, cmd string, privilege desktopNodePrivilege, password string) (string, error) {
	switch {
	case privilege.root:
		return c.exec(ctx, cmd, "")
	case privilege.sudo:
		out, err := c.exec(ctx, "sudo -S -p '' sh -c "+shellQuote(cmd), password+"\n")
		return out, err
	case privilege.tempSudoers != "":
		if out, err := c.exec(ctx, "sudo -n sh -c "+shellQuote(cmd), ""); err == nil {
			return out, nil
		}
		return c.suExec(ctx, cmd, password)
	case privilege.su:
		return c.suExec(ctx, cmd, password)
	}
	return c.exec(ctx, cmd, "")
}

func (c *desktopNodeSSH) exec(ctx context.Context, cmd, stdin string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			return stdout.String(), fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), nil
	}
}

// suExec runs cmd through `su root -c` on a PTY, answering its password
// prompt. Output before the prompt answer is dropped.
func (c *desktopNodeSSH) suExec(ctx context.Context, cmd, password string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	if err := session.RequestPty("dumb", 40, 200, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
		return "", err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	var mu sync.Mutex
	session.Stdout = writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return out.Write(p)
	})
	marker := "__MACLAW_SU_" + strconv.FormatInt(time.Now().UnixNano(), 36) + "__"
	if err := session.Start("su root -c " + shellQuote("echo "+marker+"; "+cmd)); err != nil {
		return "", err
	}
	answered := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		mu.Lock()
		text := out.String()
		mu.Unlock()
		if strings.Contains(text, marker) {
			break
		}
		lower := strings.ToLower(text)
		if !answered && (strings.Contains(lower, "password") || strings.Contains(lower, "密码") || strings.Contains(lower, "passwort")) {
			_, _ = io.WriteString(stdin, password+"\n")
			answered = true
		}
	}
	_ = stdin.Close()
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-done:
		mu.Lock()
		text := out.String()
		mu.Unlock()
		i := strings.Index(text, marker)
		if i < 0 {
			return "", fmt.Errorf("su 失败")
		}
		body := strings.TrimLeft(text[i+len(marker):], "\r\n")
		body = strings.ReplaceAll(body, "\r\n", "\n")
		if err != nil {
			return body, err
		}
		return body, nil
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// sortDesktopNodeEntries keeps the desktop in use first, then the oldest.
func sortDesktopNodeEntries(nodes []DesktopNodeEntry) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Active != nodes[j].Active {
			return nodes[i].Active
		}
		return nodes[i].CreatedAt < nodes[j].CreatedAt
	})
}
