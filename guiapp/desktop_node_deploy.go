package guiapp

import (
	"bytes"
	"context"
	"crypto/rand"
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
	fn(&records[index])
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

func (a *App) setDesktopNodeStatus(id, status, message string) {
	if _, err := a.updateDesktopNode(id, func(rec *DesktopNodeRecord) {
		rec.Status = status
		rec.Message = message
	}); err != nil {
		log.Printf("[desktop-node] save status failed id=%s err=%v", id, err)
	}
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
		HubURL: strings.TrimRight(strings.TrimSpace(hubURL), "/"),
		Status: desktopNodeStatusDeploying, Message: "agent 正在部署 desktopd",
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
			records[i].Status = desktopNodeStatusFailed
			records[i].Message = "上次部署或更新已中断，请重新更新"
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
				log.Printf("[desktop-node] could not remove temporary sudoers id=%s err=%v", rec.ID, err)
				return
			}
			defer conn.Close()
			_, _ = conn.run(context.Background(), "rm -f "+privilege.tempSudoers, desktopNodePrivilege{su: true}, password)
		}
	}
	defer func() {
		unregisterEphemeralSSHHost(label)
		cleanupPrivilege()
	}()

	if job.upgrade() {
		a.clearDesktopNodeStamp(rec, password, privilege)
	}
	taskName := "部署 desktopd · " + rec.User + "@" + rec.Host
	prompt := desktopNodeDeployPrompt(rec, label, privilege)
	display := fmt.Sprintf("〔一键部署〕在 %s@%s 上部署 desktopd", rec.User, rec.Host)
	if job.upgrade() {
		taskName = "升级 desktopd · " + rec.User + "@" + rec.Host
		prompt = desktopNodeUpgradePrompt(rec, label, privilege, job.stamp)
		display = fmt.Sprintf("〔升级 desktopd〕在 %s@%s 上用最新代码重新编译部署", rec.User, rec.Host)
	}
	task := a.CreateTask(taskName, "")
	if strings.TrimSpace(task.ProjectPath) == "" {
		a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, "无法创建部署任务")
		return
	}
	_, _ = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) { r.TaskPath = task.ProjectPath })
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
		a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, "agent 启动失败: "+err.Error())
		return
	}

	deadline := time.Now().Add(desktopNodeDeployBudget)
	var agentDoneAt time.Time
	agentSummary := ""
	stampSeen := !job.upgrade()
	ticker := time.NewTicker(desktopNodeProbeEvery)
	defer ticker.Stop()
	for {
		select {
		case resp := <-done:
			agentDoneAt = time.Now()
			if resp != nil {
				agentSummary = desktopNodeAgentVerdict(resp)
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
			return
		}
		if desktopNodeFatal(err) {
			a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, err.Error())
			return
		}
		// The agent may leave a long step running in the background, so a
		// finished turn gets a short grace before the deploy is called failed.
		// An explicit FAILED line does not get that grace.
		failed := strings.HasPrefix(strings.ToUpper(agentSummary), "FAILED")
		grace := 10 * time.Minute
		if failed {
			grace = 30 * time.Second
		}
		if !agentDoneAt.IsZero() && time.Since(agentDoneAt) > grace || time.Now().After(deadline) {
			msg := "desktopd 没有就绪: " + err.Error()
			if agentSummary != "" {
				msg += "；agent: " + agentSummary
			}
			a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, msg)
			return
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
	if err != nil || strings.TrimSpace(out) != stamp {
		return false, nil
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
	envText, err := conn.run(ctx, "cat "+shellQuote(deployDir+"/.env"), privilege, password)
	if err != nil {
		return fmt.Errorf("还没有 %s/.env", deployDir)
	}
	env := parseDotEnv(envText)
	if !desktopNodeIntranetReady(env) {
		// The install script may be an older copy that stored a public name.
		// Fix the file here so the command never contains the token.
		if _, err := conn.run(ctx, intranetDesktopEnvCommand(deployDir), privilege, password); err != nil {
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
			_, err := a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
				r.Token = token
				r.DesktopdPort = port
				r.NodeID = link.NodeID
				r.Status = desktopNodeStatusReady
				r.Message = ""
			})
			return err
		}
	}
	_, _ = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.Token = token
		r.DesktopdPort = port
		r.Status = desktopNodeStatusRegistering
		r.Message = "desktopd 已就绪，正在注册到 Hub"
	})
	nodeID, err := a.enrollDesktopd(ctx, client, base, token, rec)
	if err != nil {
		return err
	}
	_, _ = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.NodeID = nodeID
		r.Status = desktopNodeStatusReady
		r.Message = ""
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
	nodeID, err := a.enrollDesktopd(ctx, client, rec.DesktopdURL, rec.Token, rec)
	if err != nil {
		a.setDesktopNodeStatus(rec.ID, desktopNodeStatusFailed, err.Error())
		return err
	}
	_, err = a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.NodeID = nodeID
		r.Status = desktopNodeStatusReady
		r.Message = ""
	})
	return err
}

// enrollDesktopd asks Hub for a personal code and hands it to desktopd, which
// enrolls itself and opens its tunnel. The old node of this record, if any,
// is unregistered once the new one is up.
func (a *App) enrollDesktopd(ctx context.Context, client *http.Client, base, token string, rec DesktopNodeRecord) (string, error) {
	hubURL, _, err := a.getHubCredentials()
	if err != nil || strings.TrimSpace(hubURL) == "" {
		return "", fmt.Errorf("还没有连接 Hub")
	}
	var code struct {
		Code   string `json:"code"`
		NodeID string `json:"node_id"`
	}
	if err := a.desktopBotCall(ctx, http.MethodPost, "/api/v1/bots/desktop-node/code", map[string]string{"name": rec.Name}, &code, 20*time.Second); err != nil {
		return "", fmt.Errorf("Hub 拒绝了桌面注册: %v", err)
	}
	var status struct {
		Enrolled bool   `json:"enrolled"`
		NodeID   string `json:"node_id"`
		Online   bool   `json:"online"`
		Error    string `json:"error"`
	}
	body := map[string]string{"hub_url": strings.TrimRight(strings.TrimSpace(hubURL), "/"), "code": code.Code, "direct_base": strings.TrimSpace(base)}
	if err := desktopdHubLinkCall(ctx, client, base, token, http.MethodPost, body, &status); err != nil {
		return "", err
	}
	if !status.Enrolled || status.NodeID == "" {
		return "", fmt.Errorf("desktopd 没有完成注册")
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
	q := shellQuote(dir + "/.env")
	// Parentheses keep a failed edit from restarting, and a missing flag from
	// being treated as success before the line is appended.
	return "(sed -i -e '/^DESKTOPD_ADVERTISE_HOST=/d' -e '/^DESKTOPD_PUBLIC_URL=/d' -e 's/^DESKTOPD_INTRANET=.*/DESKTOPD_INTRANET=1/' " + q +
		" && grep -q '^DESKTOPD_INTRANET=1$' " + q +
		" || printf '\\nDESKTOPD_INTRANET=1\\n' >> " + q +
		") && chmod 600 " + q + " && systemctl restart maclaw-desktopd"
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
func desktopNodeDeployPrompt(rec DesktopNodeRecord, label string, privilege desktopNodePrivilege) string {
	var root string
	switch {
	case privilege.root:
		root = "登录用户就是 root，直接执行即可。"
	case privilege.tempSudoers != "":
		root = "登录用户已临时获得免密 sudo（部署结束后会自动撤销），需要 root 的命令用 sudo 执行。"
	default:
		root = "登录用户可以 sudo：先调用 ssh 的 sudo_prepare（session_id 为 connect 返回的会话），之后需要 root 的命令用 sudo 执行。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "请在远程 Linux 主机 %s@%s:%d 上部署 MaClaw 的 desktopd（Docker 桌面服务），这是用户在「桌面」设置里点的一键部署。\n\n", rec.User, rec.Host, rec.Port)
	fmt.Fprintf(&b, "连接方式：调用 ssh 工具 action=connect，label=%q（不要传 host/user/password，密码已由客户端托管）。%s\n\n", label, root)
	b.WriteString("目标：\n")
	fmt.Fprintf(&b, "- desktopd 安装在 %s，systemd 服务名 maclaw-desktopd，监听 127.0.0.1:%d（不要对外暴露端口）。\n", desktopNodeDeployDir, desktopNodeDesktopdPort)
	b.WriteString("- 主机上要有可用的 Docker，并且能得到 maclaw-gui:2 镜像。\n\n")
	b.WriteString("建议步骤（遇到环境差异请自行判断处理，比如换国内镜像源、包管理器不是 apt 等）：\n")
	b.WriteString("1. 确认系统（uname -m、/etc/os-release、systemd）。只支持 x86_64/aarch64 Linux。\n")
	b.WriteString("2. 没有 Docker 就安装（如 curl -fsSL https://get.docker.com | sh，国内可加 --mirror Aliyun），并 systemctl enable --now docker。\n")
	b.WriteString("3. 安装 git、curl、tar。安装 Go 1.26 或更新版本到 /usr/local/go（https://go.dev/dl/ 或 https://golang.google.cn/dl/），已有足够新的就跳过。\n")
	b.WriteString("4. 获取源码：git clone --depth 1 https://github.com/RapidAI/MaClaw.git /opt/maclaw-src（已存在则 git -C /opt/maclaw-src pull --ff-only）。\n")
	b.WriteString("5. 构建并打包（GOPROXY=https://goproxy.cn,direct）：\n")
	b.WriteString("   mkdir -p /tmp/maclaw_desktopd_deploy/stage/bin /tmp/maclaw_desktopd_deploy/stage/image\n")
	b.WriteString("   cd /opt/maclaw-src && CGO_ENABLED=0 /usr/local/go/bin/go build -ldflags \"-s -w\" -o /tmp/maclaw_desktopd_deploy/stage/bin/desktopd ./desktopd/cmd/desktopd\n")
	b.WriteString("   cp desktopd/image/desktop_supervisor.py desktopd/image/Dockerfile desktopd/image/Dockerfile.v2 desktopd/image/close_range_shim.c desktopd/image/fcitx5-profile /tmp/maclaw_desktopd_deploy/stage/image/\n")
	b.WriteString("   tar -czf /tmp/maclaw_desktopd_deploy/desktopd-deploy.tar.gz -C /tmp/maclaw_desktopd_deploy/stage . && cp desktopd/remote_deploy.sh /tmp/maclaw_desktopd_deploy/\n")
	fmt.Fprintf(&b, "6. 以 root 运行安装脚本（耗时可能很长，用 exec_background 执行再 wait_task 等待）。这是用户内网里的桌面，第一次不要设置 DESKTOPD_ADVERTISE_HOST，不要把它配成公网地址：\n   REMOTE_TMP_DIR=/tmp/maclaw_desktopd_deploy DESKTOPD_DEPLOY_DIR=%s DESKTOPD_BIND_ADDR=127.0.0.1:%d DESKTOPD_PORT=%d DESKTOPD_INTRANET=1 DESKTOPD_IMAGE_FROM=auto sh /tmp/maclaw_desktopd_deploy/remote_deploy.sh\n", desktopNodeDeployDir, desktopNodeDesktopdPort, desktopNodeDesktopdPort)
	b.WriteString(desktopNodeOldScriptFallback(desktopNodeDeployDir, desktopNodeDesktopdPort, "auto"))
	fmt.Fprintf(&b, "7. 验证：systemctl is-active maclaw-desktopd，并执行 sh -c '. %s/.env; curl -fsS -H \"Authorization: Bearer $DESKTOPD_TOKEN\" http://127.0.0.1:%d/v1/health'（不要回显令牌）。\n\n", desktopNodeDeployDir, desktopNodeDesktopdPort)
	b.WriteString("约束：\n")
	b.WriteString("- 不要输出、复述或保存 .env 内容和 DESKTOPD_TOKEN；不要自己去 Hub 注册，客户端会在验证通过后自动注册。\n")
	b.WriteString("- 不要修改 SSH 配置、防火墙对外规则，也不要删除主机上与本次部署无关的容器或数据。\n")
	b.WriteString("- 已经部署过的主机按升级处理：脚本会保留现有 .env 和令牌。\n\n")
	b.WriteString("完成后最后一行输出 `DESKTOPD_DEPLOY: OK`；失败则输出 `DESKTOPD_DEPLOY: FAILED <原因>`。")
	return b.String()
}

func newDesktopNodeStamp() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// desktopNodeOldScriptFallback is the second attempt when the cloned
// install script still requires a public name. The loopback value is only
// an environment variable for that old script. The cleanup edits two keys
// and does not rewrite the token line.
func desktopNodeOldScriptFallback(dir string, port int, imageFrom string) string {
	return fmt.Sprintf("   若脚本因缺少 DESKTOPD_ADVERTISE_HOST 退出，说明脚本还是旧版。不要填这台机器的 IP 或域名，也不要改 .env。把下面这一整行再运行一次，127.0.0.1 只为通过旧脚本，客户端随后会删掉它：\n"+
		"   REMOTE_TMP_DIR=/tmp/maclaw_desktopd_deploy DESKTOPD_DEPLOY_DIR=%s DESKTOPD_BIND_ADDR=127.0.0.1:%d DESKTOPD_PORT=%d DESKTOPD_INTRANET=1 DESKTOPD_ADVERTISE_HOST=127.0.0.1 DESKTOPD_IMAGE_FROM=%s sh /tmp/maclaw_desktopd_deploy/remote_deploy.sh\n",
		dir, port, port, imageFrom)
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
	var root string
	switch {
	case privilege.root:
		root = "登录用户就是 root，直接执行即可。"
	case privilege.tempSudoers != "":
		root = "登录用户已临时获得免密 sudo（升级结束后会自动撤销），需要 root 的命令用 sudo 执行。"
	default:
		root = "登录用户可以 sudo：先调用 ssh 的 sudo_prepare（session_id 为 connect 返回的会话），之后需要 root 的命令用 sudo 执行。"
	}
	dir, port := desktopNodeDeployPaths(rec)
	var b strings.Builder
	fmt.Fprintf(&b, "请把远程 Linux 主机 %s@%s:%d 上已经在运行的 MaClaw desktopd 升级到最新代码。这是用户在「桌面」设置里对这台内网桌面点的「更新」。\n\n", rec.User, rec.Host, rec.Port)
	fmt.Fprintf(&b, "连接方式：调用 ssh 工具 action=connect，label=%q（不要传 host/user/password，密码已由客户端托管）。%s\n\n", label, root)
	b.WriteString("这是升级，不是新安装。必须保留：\n")
	fmt.Fprintf(&b, "- %s/.env 里的 DESKTOPD_TOKEN 和其他配置\n", dir)
	fmt.Fprintf(&b, "- %s/state（里面有这台 desktopd 自己保存的 Hub 连接）\n", dir)
	b.WriteString("- 已有的桌面容器和用户数据。安装脚本会把新的 supervisor 拷进正在运行的桌面，不要 docker rm 它们。\n\n")
	b.WriteString("步骤：\n")
	fmt.Fprintf(&b, "1. 先删除旧的完成标记：rm -f %s/.upgrade-stamp\n", dir)
	b.WriteString("2. 确认系统仍是 x86_64 或 aarch64 Linux，Docker 和 systemd 可用。缺 git、curl、tar 或 Go 1.26+ 就补上（Go 装到 /usr/local/go，下载可用 https://go.dev/dl/ 或 https://golang.google.cn/dl/）。\n")
	b.WriteString("3. 取最新源码。/opt/maclaw-src 已存在时：git -C /opt/maclaw-src fetch --depth 1 origin，再 git -C /opt/maclaw-src reset --hard FETCH_HEAD。不存在则 git clone --depth 1 https://github.com/RapidAI/MaClaw.git /opt/maclaw-src。\n")
	b.WriteString("4. 用这份最新源码重新编译并打包（GOPROXY=https://goproxy.cn,direct）：\n")
	b.WriteString("   rm -rf /tmp/maclaw_desktopd_deploy && mkdir -p /tmp/maclaw_desktopd_deploy/stage/bin /tmp/maclaw_desktopd_deploy/stage/image\n")
	b.WriteString("   cd /opt/maclaw-src && CGO_ENABLED=0 /usr/local/go/bin/go build -ldflags \"-s -w\" -o /tmp/maclaw_desktopd_deploy/stage/bin/desktopd ./desktopd/cmd/desktopd\n")
	b.WriteString("   cp desktopd/image/desktop_supervisor.py desktopd/image/Dockerfile desktopd/image/Dockerfile.v2 desktopd/image/close_range_shim.c desktopd/image/fcitx5-profile /tmp/maclaw_desktopd_deploy/stage/image/\n")
	b.WriteString("   tar -czf /tmp/maclaw_desktopd_deploy/desktopd-deploy.tar.gz -C /tmp/maclaw_desktopd_deploy/stage . && cp desktopd/remote_deploy.sh /tmp/maclaw_desktopd_deploy/\n")
	fmt.Fprintf(&b, "5. 以 root 运行安装脚本（耗时可能很长，用 exec_background 执行再 wait_task 等待）。DESKTOPD_IMAGE_FROM=build，让桌面镜像也按最新 Dockerfile 重建。这是用户内网里的桌面，保留 DESKTOPD_INTRANET=1，第一次不要设置 DESKTOPD_ADVERTISE_HOST：\n   REMOTE_TMP_DIR=/tmp/maclaw_desktopd_deploy DESKTOPD_DEPLOY_DIR=%s DESKTOPD_BIND_ADDR=127.0.0.1:%d DESKTOPD_PORT=%d DESKTOPD_INTRANET=1 DESKTOPD_IMAGE_FROM=build sh /tmp/maclaw_desktopd_deploy/remote_deploy.sh\n", dir, port, port)
	b.WriteString(desktopNodeOldScriptFallback(dir, port, "build"))
	fmt.Fprintf(&b, "6. 确认 systemctl is-active maclaw-desktopd，并执行 sh -c '. %s/.env; curl -fsS -H \"Authorization: Bearer $DESKTOPD_TOKEN\" http://127.0.0.1:%d/v1/health'（不要回显令牌）。\n", dir, port)
	fmt.Fprintf(&b, "7. 健康检查通过后才写入完成标记（内容必须正好是这一行）：printf '%%s\\n' %s > %s/.upgrade-stamp && chmod 600 %s/.upgrade-stamp\n\n", stamp, dir, dir)
	b.WriteString("约束：\n")
	b.WriteString("- 不要输出、复述或保存 .env 内容和 DESKTOPD_TOKEN；不要自己去 Hub 注册或注销，客户端会确认原连接恢复。\n")
	b.WriteString("- 不要修改 SSH 配置、防火墙对外规则，也不要删除与本次升级无关的容器、镜像或数据。\n")
	b.WriteString("- 不要在健康检查通过之前写 .upgrade-stamp。\n\n")
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
