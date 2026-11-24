package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
)

// 私有云端服务 nodes: the srv-only installs (MaClawSrv + botnode tunnel, no
// desktopd). The SSH target is remembered so the node can be upgraded and the
// registration deleted; the SSH password is never stored — it lives in memory
// for one upgrade job, exactly like the desktopd records.

const cloudSrvNodesFile = "cloud_srv_nodes.json"

// CloudSrvNodeRecord is one srv-only install this GUI deployed.
type CloudSrvNodeRecord struct {
	NodeID   string `json:"node_id,omitempty"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user"`
	HostKey  string `json:"host_key,omitempty"`
	HubURL   string `json:"hub_url,omitempty"`
	TaskPath string `json:"task_path,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// CloudSrvNodeView is a record merged with what the Hub says about the node.
type CloudSrvNodeView struct {
	CloudSrvNodeRecord
	Registered bool   `json:"registered"`
	Online     bool   `json:"online"`
	// Busy is true while this GUI runs an install or upgrade agent on the
	// machine (one agent per machine, keyed by host:port:user).
	Busy bool `json:"busy,omitempty"`
}

type cloudSrvNodesFileBody struct {
	Nodes []CloudSrvNodeRecord `json:"nodes"`
}

var cloudSrvNodesMu sync.Mutex

// cloudSrvJobs is one upgrade per node. A second run would replace the
// in-memory SSH label the first agent is connected through.
var cloudSrvJobs sync.Map

func (a *App) cloudSrvNodesPath() string {
	dir := ""
	if a != nil {
		dir = a.GetDataDir()
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".maclaw")
	}
	return filepath.Join(dir, cloudSrvNodesFile)
}

func (a *App) loadCloudSrvRecords() ([]CloudSrvNodeRecord, error) {
	raw, err := os.ReadFile(a.cloudSrvNodesPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var file cloudSrvNodesFileBody
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	return file.Nodes, nil
}

func (a *App) saveCloudSrvRecords(records []CloudSrvNodeRecord) error {
	path := a.cloudSrvNodesPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cloudSrvNodesFileBody{Nodes: records}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// saveCloudSrvRecord inserts or updates one srv-only install keyed by
// host/port/user; the node id lands once the Hub enrollment returns it.
func (a *App) saveCloudSrvRecord(rec CloudSrvNodeRecord) error {
	cloudSrvNodesMu.Lock()
	defer cloudSrvNodesMu.Unlock()
	records, err := a.loadCloudSrvRecords()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if rec.CreatedAt == "" {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	for i := range records {
		if records[i].Host == rec.Host && records[i].Port == rec.Port && records[i].User == rec.User {
			rec.CreatedAt = records[i].CreatedAt
			records[i] = rec
			return a.saveCloudSrvRecords(records)
		}
	}
	records = append(records, rec)
	return a.saveCloudSrvRecords(records)
}

func (a *App) findCloudSrvRecord(id string) (CloudSrvNodeRecord, error) {
	cloudSrvNodesMu.Lock()
	defer cloudSrvNodesMu.Unlock()
	records, err := a.loadCloudSrvRecords()
	if err != nil {
		return CloudSrvNodeRecord{}, err
	}
	for _, rec := range records {
		if rec.NodeID == id || rec.Host == id {
			return rec, nil
		}
	}
	return CloudSrvNodeRecord{}, fmt.Errorf("这条私有节点记录不存在")
}

func (a *App) deleteCloudSrvRecord(id string) error {
	cloudSrvNodesMu.Lock()
	defer cloudSrvNodesMu.Unlock()
	records, err := a.loadCloudSrvRecords()
	if err != nil {
		return err
	}
	kept := records[:0]
	found := false
	for _, item := range records {
		if item.NodeID == id || item.Host == id {
			found = true
			continue
		}
		kept = append(kept, item)
	}
	if !found {
		return fmt.Errorf("这条私有节点记录不存在")
	}
	return a.saveCloudSrvRecords(kept)
}

// ListCloudSrvNodes merges the local srv-only install records with the Hub's
// srv picker payload (online comes from the botnode tunnel being up).
func (a *App) ListCloudSrvNodes() ([]CloudSrvNodeView, error) {
	cloudSrvNodesMu.Lock()
	records, err := a.loadCloudSrvRecords()
	cloudSrvNodesMu.Unlock()
	if err != nil {
		return nil, err
	}
	var hub struct {
		NodeID string `json:"node_id"`
		Nodes  []struct {
			NodeID  string `json:"node_id"`
			Name    string `json:"name"`
			Address string `json:"address"`
			Online  bool   `json:"online"`
		} `json:"nodes"`
	}
	hubErr := a.remoteControlJSON(http.MethodGet, "/api/v1/cloud-agents/srv", nil, &hub)
	if hubErr != nil {
		// The records are local; a Hub blip must not wipe the visible list.
		// Rows show 未注册 until the next successful fetch.
		log.Printf("[cloud-srv] hub srv list failed: %v", scrubDesktopNodeDetail(hubErr.Error()))
		hubErr = nil
	}
	byNode := map[string]int{}
	for i, node := range hub.Nodes {
		byNode[node.NodeID] = i
	}
	out := make([]CloudSrvNodeView, 0, len(records))
	for _, rec := range records {
		view := CloudSrvNodeView{CloudSrvNodeRecord: rec}
		if i, ok := byNode[rec.NodeID]; ok {
			view.Registered = true
			view.Online = hub.Nodes[i].Online
		}
		_, busy := cloudSrvJobs.Load(cloudSrvMachineKey(rec.Host, rec.Port, rec.User))
		view.Busy = busy
		out = append(out, view)
	}
	return out, hubErr
}

// cloudSrvMachineKey is the per-machine lock key: one install, upgrade or
// delete at a time no matter how the job was started.
func cloudSrvMachineKey(host string, port int, user string) string {
	return strings.Join([]string{host, strconv.Itoa(port), user}, ":")
}

// UpgradeCloudSrvNode sends the assistant agent to rebuild MaClawSrv and
// botnode from the latest source and restart both services. The enrollment
// (botnode.json) and both env files stay untouched.
func (a *App) UpgradeCloudSrvNode(id, password string) (*CloudSrvNodeRecord, error) {
	rec, err := a.findCloudSrvRecord(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rec.Host) == "" || strings.TrimSpace(rec.User) == "" {
		return nil, fmt.Errorf("这条节点没有保存 SSH 地址，无法升级")
	}
	if password == "" {
		return nil, fmt.Errorf("请输入 %s@%s 的 SSH 密码", rec.User, rec.Host)
	}
	if rec.Port <= 0 {
		rec.Port = 22
	}
	machine := cloudSrvMachineKey(rec.Host, rec.Port, rec.User)
	if !claimCloudSrvJob(machine) {
		return nil, fmt.Errorf("这台节点正在升级")
	}
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		releaseCloudSrvJob(machine)
		return nil, fmt.Errorf("SSH 连接失败: %v", err)
	}
	privilege, privErr := conn.ensurePrivilege()
	conn.Close()
	if privErr != nil {
		releaseCloudSrvJob(machine)
		return nil, privErr
	}
	hubURL := strings.TrimRight(strings.TrimSpace(rec.HubURL), "/")
	if hubURL == "" {
		hubURL, _, _ = a.getHubCredentials()
		hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	}
	go func() {
		defer releaseCloudSrvJob(machine)
		a.runCloudSrvUpgradeJob(rec, password, privilege, hubURL)
	}()
	out := rec
	return &out, nil
}

// runCloudSrvUpgradeJob drives the rebuild. The tunnel enrollment stays:
// restarting the services reconnects it, and a fresh enroll would register
// a second node.
func (a *App) runCloudSrvUpgradeJob(rec CloudSrvNodeRecord, password string, privilege desktopNodePrivilege, hubURL string) {
	nodeKey := strings.TrimSpace(rec.NodeID)
	if nodeKey == "" {
		nodeKey = rec.Host
	}
	label := "maclaw-srvonly-" + nodeKey
	registerEphemeralSSHHost(corelibSSHEntry(label, rec, password))
	cleanupPrivilege := func() {}
	if privilege.tempSudoers != "" {
		cleanupPrivilege = func() {
			conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
			if err != nil {
				log.Printf("[cloud-srv] could not remove temporary sudoers node=%s err=%s", rec.NodeID, scrubDesktopNodeDetail(err.Error()))
				return
			}
			defer conn.Close()
			if _, rmErr := conn.run(context.Background(), "rm -f "+privilege.tempSudoers, desktopNodePrivilege{su: true}, password); rmErr != nil {
				log.Printf("[cloud-srv] could not remove temporary sudoers node=%s err=%s", rec.NodeID, scrubDesktopNodeDetail(rmErr.Error()))
			}
		}
	}
	defer func() {
		unregisterEphemeralSSHHost(label)
		cleanupPrivilege()
	}()

	_, err := RunOpsAgent(a, OpsAgentJob{
		Name:      "升级 MaClawSrv · " + rec.User + "@" + rec.Host,
		Display:   fmt.Sprintf("〔升级〕在 %s@%s 上用最新代码重新编译部署 MaClawSrv", rec.User, rec.Host),
		Prompt:    cloudSrvUpgradePrompt(rec, password, privilege, hubURL),
		Marker:    "MACLAWSRV_UPGRADE",
		Budget:    desktopNodeDeployBudget,
		Every:     desktopNodeProbeEvery,
		Log:       func(step, detail string) { a.desktopNodeDetail(rec.NodeID, step, detail) },
		Fatal:     desktopNodeFatal,
		ProbeStep: desktopNodeProbeStep,
		Probe: func(agentDone bool, _ string) error {
			if !agentDone {
				return fmt.Errorf("MaClawSrv 正在升级")
			}
			return a.finishCloudSrvUpgrade(rec, password, privilege)
		},
	})
	if err != nil {
		msg := scrubDesktopNodeDetail(err.Error())
		a.desktopNodeDetail(rec.NodeID, "failed", msg)
		log.Printf("[cloud-srv] upgrade failed node=%s err=%s", rec.NodeID, msg)
	} else {
		a.desktopNodeDetail(rec.NodeID, "ready", "MaClawSrv 升级完成 host="+rec.Host)
	}
}

// finishCloudSrvUpgrade verifies both services came back and the enrollment
// survived. Errors are worded so desktopNodeFatal keeps probing.
func (a *App) finishCloudSrvUpgrade(rec CloudSrvNodeRecord, password string, privilege desktopNodePrivilege) error {
	conn, err := dialDesktopNodeSSH(rec.Host, rec.Port, rec.User, password, rec.HostKey)
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	state, err := conn.run(ctx, "cat /var/lib/maclaw/botnode.json", privilege, password)
	if err != nil || !strings.Contains(state, "node_id") {
		return fmt.Errorf("隧道注册还未完成恢复")
	}
	a.noteCloudSrvNodeID(rec, state)
	active, err := conn.run(ctx, "systemctl is-active maclaw-srv maclaw-botnode", privilege, password)
	if err != nil || strings.Count(strings.TrimSpace(active), "active") != 2 {
		return fmt.Errorf("maclaw-srv 正在升级")
	}
	return nil
}

// cloudSrvNodeIDRe extracts the enrolled node id from botnode.json so the
// local record can be linked to the Hub node (the deploy-time record starts
// with an empty node id).
var cloudSrvNodeIDRe = regexp.MustCompile(`"node_id"\s*:\s*"([^"]+)"`)

// noteCloudSrvNodeID links an enrolled node id back into the srv-only record.
func (a *App) noteCloudSrvNodeID(rec CloudSrvNodeRecord, botnodeState string) {
	match := cloudSrvNodeIDRe.FindStringSubmatch(botnodeState)
	if match == nil || strings.TrimSpace(match[1]) == "" || rec.NodeID == match[1] {
		return
	}
	nodeID := match[1]
	cloudSrvNodesMu.Lock()
	defer cloudSrvNodesMu.Unlock()
	records, err := a.loadCloudSrvRecords()
	if err != nil {
		log.Printf("[cloud-srv] could not link node id host=%s err=%v", rec.Host, err)
		return
	}
	for i := range records {
		if records[i].Host == rec.Host && records[i].Port == rec.Port && records[i].User == rec.User {
			records[i].NodeID = nodeID
			records[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			if err := a.saveCloudSrvRecords(records); err != nil {
				log.Printf("[cloud-srv] could not save node id host=%s err=%v", rec.Host, err)
			}
			return
		}
	}
}

// DeleteCloudSrvNode unregisters the node on Hub and forgets the local
// record. The software on the server keeps running; uninstalling it is a
// separate manual step. The machine lock is checked BEFORE the Hub call so a
// running upgrade is never orphaned by a half-deleted registration. When the
// deleted node is the user's pinned cloud srv, the pin clears too — cloud
// tasks would otherwise keep dispatching to the dead node.
func (a *App) DeleteCloudSrvNode(id string) error {
	rec, recErr := a.findCloudSrvRecord(id)
	if recErr != nil {
		return recErr
	}
	machine := cloudSrvMachineKey(rec.Host, rec.Port, rec.User)
	if !claimCloudSrvJob(machine) {
		return fmt.Errorf("这台节点正在升级")
	}
	defer releaseCloudSrvJob(machine)
	if strings.TrimSpace(rec.NodeID) != "" {
		serverID, err := a.hubServerIDForNode(rec.NodeID)
		if err != nil {
			// A registration the Hub no longer knows must not trap the local
			// record in a permanent delete failure. Only a real call failure
			// (network, auth) aborts; hubServerIDForNode wraps every lookup
			// miss in that message.
			if !strings.Contains(err.Error(), "没有这台") {
				return err
			}
			log.Printf("[cloud-srv] node already unregistered on Hub, deleting the local record node=%s", rec.NodeID)
		} else {
			var out struct{}
			if err := a.desktopBotCall(context.Background(), http.MethodDelete, "/api/v1/self-desktop-nodes/"+url.PathEscape(serverID), nil, &out, 30*time.Second); err != nil {
				return err
			}
			a.unpinCloudSrvIfPinned(rec.NodeID)
		}
	}
	return a.deleteCloudSrvRecord(rec.NodeID)
}

// unpinCloudSrvIfPinned clears the user's cloud srv choice when it still
// names the deleted node.
func (a *App) unpinCloudSrvIfPinned(nodeID string) {
	var choice struct {
		NodeID string `json:"node_id"`
	}
	if err := a.remoteControlJSON(http.MethodGet, "/api/v1/cloud-agents/srv", nil, &choice); err != nil {
		return
	}
	if strings.TrimSpace(choice.NodeID) != strings.TrimSpace(nodeID) {
		return
	}
	var out struct{}
	if err := a.remoteControlJSON(http.MethodPut, "/api/v1/cloud-agents/srv", map[string]string{"node_id": ""}, &out); err != nil {
		log.Printf("[cloud-srv] could not clear the pinned node after delete: %v", scrubDesktopNodeDetail(err.Error()))
	}
}

func claimCloudSrvJob(nodeID string) bool {
	if strings.TrimSpace(nodeID) == "" {
		return true
	}
	_, loaded := cloudSrvJobs.LoadOrStore(nodeID, struct{}{})
	return !loaded
}

func releaseCloudSrvJob(nodeID string) {
	if strings.TrimSpace(nodeID) == "" {
		return
	}
	cloudSrvJobs.Delete(nodeID)
}

func corelibSSHEntry(label string, rec CloudSrvNodeRecord, password string) corelib.SSHHostEntry {
	return corelib.SSHHostEntry{
		Label: label, Host: rec.Host, Port: rec.Port, User: rec.User,
		AuthMethod: "password", Password: password,
	}
}

func cloudSrvUpgradePrompt(rec CloudSrvNodeRecord, password string, privilege desktopNodePrivilege, hubURL string) string {
	var b strings.Builder
	b.WriteString("用 ssh 登录下面这台 Linux 服务器，把已经在运行的 MaClawSrv 升级到文档里的最新代码。这是更新，不是新安装。\n\n")
	b.WriteString(desktopNodeServerLines(rec0ToDeployRecord(rec), password, privilege, "升级"))
	b.WriteString("要更新的组件：MaClawSrv 和 botnode（到 Hub 的隧道）\n")
	b.WriteString("不要安装 desktopd，不要安装 Docker，不要动桌面相关的东西。\n\n")
	hub := strings.TrimRight(strings.TrimSpace(hubURL), "/")
	fmt.Fprintf(&b, "当前登录的 Hub：%s\n", hub)
	fmt.Fprintf(&b, "安装文档：%s\n", maclawsrvOnlyDocURL(hub))
	b.WriteString("更新步骤按文档「升级」一节做：更新 /opt/maclaw-src 后重新编译 MaClawSrv 和 botnode 两个二进制，重启 maclaw-srv 和 maclaw-botnode。\n")
	b.WriteString("必须保留 /var/lib/maclaw/botnode.json、/etc/maclaw/maclawsrv.env、/etc/maclaw/botnode.env 和已有数据。不要重新注册，不要改 .env，不要删除无关文件。\n")
	b.WriteString("不要在回复里复述密码和密钥。\n\n")
	b.WriteString("完成后最后一行输出 `MACLAWSRV_UPGRADE: OK`；失败则输出 `MACLAWSRV_UPGRADE: FAILED <原因>`。")
	return b.String()
}

// rec0ToDeployRecord adapts a srv-only record to the server-lines helper.
func rec0ToDeployRecord(rec CloudSrvNodeRecord) DesktopNodeRecord {
	return DesktopNodeRecord{
		Name: rec.Name, Host: rec.Host, Port: rec.Port, User: rec.User, HubURL: rec.HubURL,
	}
}
