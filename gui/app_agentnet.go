package main

// app_agentnet.go — Wails bindings for AgentNet integration.
// Exposes AgentNet P2P network features to the frontend via the App struct.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentnet"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// initAgentNet lazily creates the AgentNet client on first use.
func (a *App) initAgentNet() *AgentNetClient {
	if a.agentNetClient == nil {
		a.agentNetClient = NewAgentNetClient()
	}
	return a.agentNetClient
}

func (a *App) agentNetStartAllowed() error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if !cfg.AgentNetEnabled {
		return fmt.Errorf("agentnet is disabled in settings")
	}
	return nil
}

func (a *App) agentNetEnabled() bool {
	return a.agentNetStartAllowed() == nil
}

// ---------- Wails-exposed methods ----------

// AgentNetEnsureDaemon starts the AgentNet daemon if not running.
func (a *App) AgentNetEnsureDaemon() map[string]interface{} {
	if err := a.agentNetStartAllowed(); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	c := a.initAgentNet()
	if err := c.EnsureDaemon(); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Start background auto-update (24h cycle, immediate if stale).
	c.StartAutoUpdate(func(msg string) { a.log(msg) })
	return map[string]interface{}{"ok": true}
}

// AgentNetStopDaemon stops the AgentNet daemon.
func (a *App) AgentNetStopDaemon() {
	if a.agentNetClient != nil {
		a.agentNetClient.StopDaemon()
	}
}

// AgentNetIsRunning checks if the daemon is reachable.
// Lazily initialises the client so the App-level poller can detect a
// daemon that was started externally (e.g. by the OS or a previous run)
// without requiring the user to visit the settings panel first.
func (a *App) AgentNetIsRunning() bool {
	c := a.initAgentNet()
	return c.IsRunning()
}

// AgentNetGetStatus returns node status.
func (a *App) AgentNetGetStatus() map[string]interface{} {
	c := a.initAgentNet()
	s, err := c.GetStatus()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{
		"ok":        true,
		"peer_id":   s.PeerID,
		"peers":     s.Peers,
		"unread_dm": s.UnreadDM,
		"version":   s.Version,
		"uptime":    s.Uptime,
	}
}

// AgentNetGetPeers returns connected peers.
func (a *App) AgentNetGetPeers() map[string]interface{} {
	c := a.initAgentNet()
	peers, err := c.GetPeers()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "peers": peers}
}

// AgentNetListTasks lists tasks with optional status filter.
func (a *App) AgentNetListTasks(status string) map[string]interface{} {
	c := a.initAgentNet()
	tasks, err := c.ListTasks(status)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "tasks": tasks}
}

// AgentNetCreateTask posts a new task to the network.
func (a *App) AgentNetCreateTask(title string, reward float64) map[string]interface{} {
	c := a.initAgentNet()
	task, err := c.CreateTask(title, reward)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "task": task}
}

// AgentNetCreateTaskFull creates a task with description, tags, and optional target peer.
func (a *App) AgentNetCreateTaskFull(title, description string, reward float64, tags []string, targetPeer string) map[string]interface{} {
	c := a.initAgentNet()
	task, err := c.CreateTaskFull(title, description, reward, tags, targetPeer)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "task": task}
}

// AgentNetGetCredits returns Shell balance and tier info.
func (a *App) AgentNetGetCredits() map[string]interface{} {
	c := a.initAgentNet()
	credits, err := c.GetCredits()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{
		"ok":            true,
		"balance":       credits.Balance,
		"tier":          credits.Tier,
		"currency":      credits.Currency,
		"exchange_rate": credits.ExchangeRate,
		"local_value":   credits.LocalValue,
	}
}

// AgentNetSearchKnowledge searches the knowledge mesh.
func (a *App) AgentNetSearchKnowledge(query string) map[string]interface{} {
	c := a.initAgentNet()
	entries, err := c.SearchKnowledge(query)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entries": entries}
}

// AgentNetPublishKnowledge publishes a knowledge entry.
func (a *App) AgentNetPublishKnowledge(title, body string) map[string]interface{} {
	c := a.initAgentNet()
	entry, err := c.PublishKnowledge(title, body)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entry": entry}
}

// AgentNetPublishKnowledgeFull publishes a knowledge entry with domain tags.
func (a *App) AgentNetPublishKnowledgeFull(title, body string, domains []string) map[string]interface{} {
	c := a.initAgentNet()
	entry, err := c.PublishKnowledgeFull(title, body, domains)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entry": entry}
}

// AgentNetSendDM sends an encrypted direct message.
func (a *App) AgentNetSendDM(peerID, body string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.SendDM(peerID, body); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetGetDMInbox returns the DM inbox.
func (a *App) AgentNetGetDMInbox() map[string]interface{} {
	c := a.initAgentNet()
	inbox, err := c.GetDMInbox()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "inbox": inbox}
}

// AgentNetListSwarmSessions lists active Swarm Think sessions.
func (a *App) AgentNetListSwarmSessions() map[string]interface{} {
	c := a.initAgentNet()
	sessions, err := c.ListSwarmSessions()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "sessions": sessions}
}

// AgentNetCreateSwarmSession starts a new Swarm Think session.
func (a *App) AgentNetCreateSwarmSession(topic, question string) map[string]interface{} {
	c := a.initAgentNet()
	session, err := c.CreateSwarmSession(topic, question)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "session": session}
}

// AgentNetGetResume returns the agent's profile (with local cache fallback).
func (a *App) AgentNetGetResume() map[string]interface{} {
	c := a.initAgentNet()
	r, err := c.GetResume()
	if err != nil {
		// Fallback to local cache on API failure.
		cache := readProfileCache()
		if cache != nil {
			return map[string]interface{}{
				"ok": true,
				"resume": map[string]interface{}{
					"skills": cache.Skills, "domains": cache.Domains, "bio": cache.Bio,
				},
				"cached": true,
			}
		}
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Fill empty fields from cache.
	cache := readProfileCache()
	if cache != nil {
		if r.Bio == "" && cache.Bio != "" {
			r.Bio = cache.Bio
		}
		if len(r.Skills) == 0 && len(cache.Skills) > 0 {
			r.Skills = cache.Skills
		}
		if len(r.Domains) == 0 && len(cache.Domains) > 0 {
			r.Domains = cache.Domains
		}
	}
	return map[string]interface{}{"ok": true, "resume": r}
}

// AgentNetListPredictions lists active prediction markets.
func (a *App) AgentNetListPredictions() map[string]interface{} {
	c := a.initAgentNet()
	preds, err := c.ListPredictions()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "predictions": preds}
}

// AgentNetInstallBinary downloads the anet binary via official installer.
// Emits "agentnet-install-progress" events to the frontend during download.
func (a *App) AgentNetInstallBinary() map[string]interface{} {
	emitter := func(stage string, pct int, msg string) {
		a.emitEvent("agentnet-install-progress", map[string]interface{}{
			"stage":   stage,
			"percent": pct,
			"message": msg,
		})
	}
	path, err := DownloadAnet(emitter)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "path": path}
}

// AgentNetEnsureDaemonWithDownload starts the daemon, auto-downloading if needed.
// Emits "agentnet-install-progress" events during download.
func (a *App) AgentNetEnsureDaemonWithDownload() map[string]interface{} {
	if err := a.agentNetStartAllowed(); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	c := a.initAgentNet()
	emitter := func(stage string, pct int, msg string) {
		a.emitEvent("agentnet-install-progress", map[string]interface{}{
			"stage":   stage,
			"percent": pct,
			"message": msg,
		})
	}
	if err := c.EnsureDaemonWithProgress(emitter); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Start background auto-update (24h cycle, immediate if stale).
	c.StartAutoUpdate(func(msg string) { a.log(msg) })
	return map[string]interface{}{"ok": true}
}

// AgentNetManualUpdate checks for a new version of the agentnet binary,
// downloads it if available, and restarts the daemon.
// Returns {ok, updated, message, error}.
func (a *App) AgentNetManualUpdate() map[string]interface{} {
	c := a.initAgentNet()
	a.log("AgentNet: manual update triggered")

	emitter := func(stage string, pct int, msg string) {
		a.emitEvent("agentnet-install-progress", map[string]interface{}{
			"stage":   stage,
			"percent": pct,
			"message": msg,
		})
	}

	// If binary is not installed yet, download it first.
	if c.findBinary() == "" {
		a.log("AgentNet: binary not found, downloading first...")
		downloaded, dlErr := DownloadAnet(emitter)
		if dlErr != nil {
			a.log(fmt.Sprintf("AgentNet: download failed: %v", dlErr))
			return map[string]interface{}{"ok": false, "error": dlErr.Error()}
		}
		c.mu.Lock()
		c.binPath = downloaded
		c.mu.Unlock()
		a.log(fmt.Sprintf("AgentNet: binary installed at %s", downloaded))
	}

	// Run `agentnet update` via SelfUpdate.
	err := c.SelfUpdate()
	if err != nil {
		a.log(fmt.Sprintf("AgentNet: manual update failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	a.log("AgentNet: update applied")
	if !a.agentNetEnabled() {
		a.log("AgentNet: daemon restart skipped after update because agentnet_enabled=false")
		return map[string]interface{}{"ok": true, "updated": true, "restarted": false}
	}

	a.log("AgentNet: update applied, restarting daemon...")

	// Stop the current daemon.
	c.StopDaemon()

	// Restart with progress events (reuse emitter from above).
	if restartErr := c.EnsureDaemonWithProgress(emitter); restartErr != nil {
		a.log(fmt.Sprintf("AgentNet: restart after update failed: %v", restartErr))
		return map[string]interface{}{"ok": false, "updated": true, "error": restartErr.Error()}
	}

	c.StartAutoUpdate(func(msg string) { a.log(msg) })
	a.log("AgentNet: manual update completed, daemon restarted")
	return map[string]interface{}{"ok": true, "updated": true, "restarted": true}
}

// AgentNetGetBinaryPath returns the resolved agentnet binary path (for diagnostics).
func (a *App) AgentNetGetBinaryPath() string {
	c := a.initAgentNet()
	if c.binPath != "" {
		return c.binPath
	}
	p := c.findBinary()
	if p == "" {
		return "not found (searched ~/.anet/, %LOCALAPPDATA%\\anet\\, PATH)"
	}
	return p
}

// ---------- Profile (with local cache fallback) ----------

// profileCachePath returns ~/.anet/profile_cache.json
func profileCachePath() string {
	dir, err := anetInstallDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "profile_cache.json")
}

// profileCache is the structure persisted to local JSON for offline fallback.
type profileCache struct {
	Name    string   `json:"name,omitempty"`
	Bio     string   `json:"bio,omitempty"`
	Motto   string   `json:"motto,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	Domains []string `json:"domains,omitempty"`
}

func readProfileCache() *profileCache {
	p := profileCachePath()
	if p == "" {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var cache profileCache
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	return &cache
}

// writeProfileCacheFields updates specific fields in the local cache.
// Only the provided fields are overwritten; others are preserved.
func writeProfileCacheFields(name *string, bio *string, motto *string, skills []string, domains []string) {
	p := profileCachePath()
	if p == "" {
		return
	}
	existing := readProfileCache()
	if existing == nil {
		existing = &profileCache{}
	}
	if name != nil {
		existing.Name = *name
	}
	if bio != nil {
		existing.Bio = *bio
	}
	if motto != nil {
		existing.Motto = *motto
	}
	if skills != nil {
		existing.Skills = skills
	}
	if domains != nil {
		existing.Domains = domains
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	_ = os.WriteFile(p, data, 0644)
}

func (a *App) AgentNetGetProfile() map[string]interface{} {
	c := a.initAgentNet()
	p, err := c.GetProfile()
	if err != nil {
		// Fallback to local cache on API failure.
		cache := readProfileCache()
		if cache != nil {
			return map[string]interface{}{
				"ok": true,
				"profile": map[string]interface{}{
					"name": cache.Name, "bio": cache.Bio, "motto": cache.Motto,
				},
				"cached": true,
			}
		}
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Fill empty fields from cache.
	cache := readProfileCache()
	if cache != nil {
		if p.Name == "" && cache.Name != "" {
			p.Name = cache.Name
		}
		if p.Bio == "" && cache.Bio != "" {
			p.Bio = cache.Bio
		}
		if p.Motto == "" && cache.Motto != "" {
			p.Motto = cache.Motto
		}
	}
	return map[string]interface{}{"ok": true, "profile": p}
}

func (a *App) AgentNetUpdateProfile(name, bio string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.UpdateProfile(name, bio); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Write to local cache.
	writeProfileCacheFields(&name, &bio, nil, nil, nil)
	return map[string]interface{}{"ok": true}
}

func (a *App) AgentNetSetMotto(motto string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.SetMotto(motto); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	writeProfileCacheFields(nil, nil, &motto, nil, nil)
	return map[string]interface{}{"ok": true}
}

// ---------- Daemon Info ----------

// AgentNetGetDaemonInfo returns daemon process info for diagnostics display.
// Returns PID (if we launched it), binary path, and version.
// The caller (frontend) already knows the alive/running state from AgentNetIsRunning.
func (a *App) AgentNetGetDaemonInfo() map[string]interface{} {
	c := a.initAgentNet()
	binPath := c.binPath
	if binPath == "" {
		binPath = c.findBinary()
	}

	pid := c.DaemonPID()
	version := ""
	if s, err := c.GetStatus(); err == nil {
		version = s.Version
	}

	return map[string]interface{}{
		"ok":       true,
		"pid":      pid,
		"bin_path": binPath,
		"version":  version,
	}
}

// ---------- Topic Rooms ----------

func (a *App) AgentNetListTopics() map[string]interface{} {
	c := a.initAgentNet()
	topics, err := c.ListTopics()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "topics": topics}
}

func (a *App) AgentNetCreateTopic(name, description string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.CreateTopic(name, description); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) AgentNetGetTopicMessages(topicName string) map[string]interface{} {
	c := a.initAgentNet()
	msgs, err := c.GetTopicMessages(topicName)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "messages": msgs}
}

func (a *App) AgentNetPostTopicMessage(topicName, body string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.PostTopicMessage(topicName, body); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ---------- Task Bazaar (extended) ----------

func (a *App) AgentNetBidOnTask(id string, amount float64, message string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.BidOnTask(id, amount, message); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) AgentNetSubmitTaskResult(id, result string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.SubmitTaskResult(id, result); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) AgentNetApproveTask(id string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.ApproveTask(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) AgentNetRejectTask(id string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.RejectTask(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) AgentNetCancelTask(id string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.CancelTask(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) AgentNetGetTaskBids(id string) map[string]interface{} {
	c := a.initAgentNet()
	bids, err := c.GetTaskBids(id)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "bids": bids}
}

func (a *App) AgentNetMatchTasks() map[string]interface{} {
	c := a.initAgentNet()
	tasks, err := c.MatchTasks()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "tasks": tasks}
}

func (a *App) AgentNetGetTaskBoard() map[string]interface{} {
	c := a.initAgentNet()
	board, err := c.GetTaskBoard()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "board": board}
}

// ---------- Credits (extended) ----------

func (a *App) AgentNetGetTransactions() map[string]interface{} {
	c := a.initAgentNet()
	txns, err := c.GetCreditsTransactions()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "transactions": txns}
}

func (a *App) AgentNetGetLeaderboard() map[string]interface{} {
	c := a.initAgentNet()
	lb, err := c.GetLeaderboard()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "leaderboard": lb}
}

// ---------- Diagnostics ----------

func (a *App) AgentNetGetDiagnostics() map[string]interface{} {
	c := a.initAgentNet()
	diag, err := c.GetDiagnostics()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "diagnostics": diag}
}

func (a *App) AgentNetSelfUpdate() map[string]interface{} {
	c := a.initAgentNet()
	if err := c.SelfUpdate(); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ---------- Knowledge Feed ----------

func (a *App) AgentNetGetKnowledgeFeed(domain string, limit int) map[string]interface{} {
	c := a.initAgentNet()
	entries, err := c.GetKnowledgeFeed(domain, limit)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entries": entries}
}

// ---------- DM Thread ----------

func (a *App) AgentNetGetDMThread(peerID string, limit int) map[string]interface{} {
	c := a.initAgentNet()
	thread, err := c.GetDMThread(peerID, limit)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "thread": thread}
}

// ---------- Identity Key Backup / Restore ----------

// agentnetIdentityKeyPath returns the path to ~/.anet/anet/identity.key
func agentnetIdentityKeyPath() (string, error) {
	dir, err := anetInstallDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "anet", "identity.key"), nil
}

// AgentNetHasIdentity checks whether an identity.key file exists.
func (a *App) AgentNetHasIdentity() map[string]interface{} {
	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		return map[string]interface{}{"ok": true, "exists": false, "path": keyPath}
	}
	return map[string]interface{}{"ok": true, "exists": true, "path": keyPath, "size": info.Size()}
}

// AgentNetExportIdentity copies identity.key to a user-chosen location via save dialog.
func (a *App) AgentNetExportIdentity() map[string]interface{} {
	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	if _, err := os.Stat(keyPath); err != nil {
		return map[string]interface{}{"ok": false, "error": "identity.key not found — daemon may not have been initialized"}
	}

	dest, err := wailsrt.SaveFileDialog(a.ctx, wailsrt.SaveDialogOptions{
		Title:           "Export AgentNet Identity Key",
		DefaultFilename: "agentnet-identity.key",
		Filters: []wailsrt.FileFilter{
			{DisplayName: "Key Files", Pattern: "*.key"},
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
	if err != nil || dest == "" {
		return map[string]interface{}{"ok": false, "error": "cancelled"}
	}

	if err := agentnetCopyFile(keyPath, dest); err != nil {
		a.log(fmt.Sprintf("AgentNet: export identity key failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log(fmt.Sprintf("AgentNet: identity key exported to %s", dest))
	return map[string]interface{}{"ok": true, "path": dest}
}

// AgentNetImportIdentity restores identity.key from a user-chosen file via open dialog.
// Stops daemon before importing, and only restarts if AgentNet is enabled.
func (a *App) AgentNetImportIdentity() map[string]interface{} {
	src, err := wailsrt.OpenFileDialog(a.ctx, wailsrt.OpenDialogOptions{
		Title: "Import AgentNet Identity Key",
		Filters: []wailsrt.FileFilter{
			{DisplayName: "Key Files", Pattern: "*.key"},
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
	if err != nil || src == "" {
		return map[string]interface{}{"ok": false, "error": "cancelled"}
	}

	info, err := os.Stat(src)
	if err != nil {
		a.log(fmt.Sprintf("AgentNet: import identity — cannot read source file: %v", err))
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("cannot read file: %v", err)}
	}
	// Sanity check: Ed25519 key files are small (typically < 1KB)
	if info.Size() > 10*1024 {
		a.log(fmt.Sprintf("AgentNet: import identity — file too large (%d bytes)", info.Size()))
		return map[string]interface{}{"ok": false, "error": "file too large — does not look like an identity key"}
	}

	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	// Stop daemon before replacing key
	if a.agentNetClient != nil && a.agentNetClient.IsRunning() {
		a.log("AgentNet: stopping daemon before key import")
		a.agentNetClient.StopDaemon()
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		a.log(fmt.Sprintf("AgentNet: import identity — mkdir failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	// Backup existing key if present
	if _, err := os.Stat(keyPath); err == nil {
		if renameErr := os.Rename(keyPath, keyPath+".bak"); renameErr != nil {
			a.log(fmt.Sprintf("AgentNet: import identity — backup rename failed: %v", renameErr))
		} else {
			a.log(fmt.Sprintf("AgentNet: existing identity key backed up to %s.bak", keyPath))
		}
	}

	if err := agentnetCopyFile(src, keyPath); err != nil {
		a.log(fmt.Sprintf("AgentNet: import identity — copy failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log(fmt.Sprintf("AgentNet: identity key imported from %s", src))

	// Restart daemon with new identity only when the main switch allows it.
	restarted := false
	if !a.agentNetEnabled() {
		a.log("AgentNet: daemon restart skipped after identity import because agentnet_enabled=false")
		return map[string]interface{}{"ok": true, "path": keyPath, "restarted": restarted}
	}
	c := a.initAgentNet()
	if err := c.EnsureDaemon(); err != nil {
		a.log(fmt.Sprintf("AgentNet: daemon restart after import failed: %v", err))
	} else {
		restarted = true
		a.log("AgentNet: daemon restarted with new identity")
	}

	return map[string]interface{}{"ok": true, "path": keyPath, "restarted": restarted}
}

// agentnetCopyFile copies src to dst atomically via temp file.
// Preserves 0600 permissions for security-sensitive files like identity keys.
func agentnetCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	os.Remove(dst)
	return os.Rename(tmp, dst)
}

// ---------- Online Key Backup / Restore via Hub ----------

// AgentNetOnlineBackupKey encrypts the identity key with the user's password
// and uploads it to the Hub, bound to the user's email.
func (a *App) AgentNetOnlineBackupKey(password string) map[string]interface{} {
	config, err := a.LoadConfig()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": "failed to load config"}
	}
	email := config.RemoteEmail
	hubURL := config.RemoteHubURL
	if email == "" {
		return map[string]interface{}{"ok": false, "error": "no email configured — please activate remote first"}
	}
	if hubURL == "" {
		return map[string]interface{}{"ok": false, "error": "no hub URL configured"}
	}

	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": "identity.key not found — daemon may not have been initialized"}
	}

	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
		"key_data": base64.StdEncoding.EncodeToString(keyData),
	})

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(hubURL+"/api/agentnet/key/backup", "application/json", bytes.NewReader(payload))
	if err != nil {
		a.log(fmt.Sprintf("AgentNet: online backup request failed for %s: %v", email, err))
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("hub request failed: %v", err)}
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		a.log(fmt.Sprintf("AgentNet: online backup failed for %s: %s", email, errMsg))
		return map[string]interface{}{"ok": false, "error": errMsg}
	}
	a.log(fmt.Sprintf("AgentNet: identity key backed up to Hub for %s", email))
	return map[string]interface{}{"ok": true}
}

// AgentNetOnlineRestoreKey downloads and decrypts the identity key from the Hub.
// Stops daemon before replacing key, and only restarts if AgentNet is enabled.
func (a *App) AgentNetOnlineRestoreKey(password string) map[string]interface{} {
	config, err := a.LoadConfig()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": "failed to load config"}
	}
	email := config.RemoteEmail
	hubURL := config.RemoteHubURL
	if email == "" {
		return map[string]interface{}{"ok": false, "error": "no email configured — please activate remote first"}
	}
	if hubURL == "" {
		return map[string]interface{}{"ok": false, "error": "no hub URL configured"}
	}

	payload, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(hubURL+"/api/agentnet/key/restore", "application/json", bytes.NewReader(payload))
	if err != nil {
		a.log(fmt.Sprintf("AgentNet: online restore request failed for %s: %v", email, err))
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("hub request failed: %v", err)}
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != http.StatusOK {
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		a.log(fmt.Sprintf("AgentNet: online restore failed for %s: %s", email, errMsg))
		return map[string]interface{}{"ok": false, "error": errMsg}
	}

	keyDataB64, _ := result["key_data"].(string)
	if keyDataB64 == "" {
		a.log(fmt.Sprintf("AgentNet: online restore — empty key data in response for %s", email))
		return map[string]interface{}{"ok": false, "error": "empty key data in response"}
	}
	keyData, err := base64.StdEncoding.DecodeString(keyDataB64)
	if err != nil {
		a.log(fmt.Sprintf("AgentNet: online restore — invalid key encoding for %s: %v", email, err))
		return map[string]interface{}{"ok": false, "error": "invalid key data encoding"}
	}

	// Stop daemon before replacing key
	if a.agentNetClient != nil && a.agentNetClient.IsRunning() {
		a.log("AgentNet: stopping daemon before online key restore")
		a.agentNetClient.StopDaemon()
	}

	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		a.log(fmt.Sprintf("AgentNet: online restore — mkdir failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Backup existing key
	if _, err := os.Stat(keyPath); err == nil {
		if renameErr := os.Rename(keyPath, keyPath+".bak"); renameErr != nil {
			a.log(fmt.Sprintf("AgentNet: online restore — backup rename failed: %v", renameErr))
		} else {
			a.log(fmt.Sprintf("AgentNet: existing identity key backed up to %s.bak", keyPath))
		}
	}
	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		a.log(fmt.Sprintf("AgentNet: online restore — write key failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log(fmt.Sprintf("AgentNet: identity key restored from Hub for %s", email))

	// Restart daemon with restored identity only when the main switch allows it.
	restarted := false
	if !a.agentNetEnabled() {
		a.log("AgentNet: daemon restart skipped after online restore because agentnet_enabled=false")
		return map[string]interface{}{"ok": true, "path": keyPath, "restarted": restarted}
	}
	c := a.initAgentNet()
	if err := c.EnsureDaemon(); err != nil {
		a.log(fmt.Sprintf("AgentNet: daemon restart after online restore failed: %v", err))
	} else {
		restarted = true
		a.log("AgentNet: daemon restarted with restored identity")
	}

	return map[string]interface{}{"ok": true, "path": keyPath, "restarted": restarted}
}

// ---------- Missing Wails Bindings ----------

// AgentNetUpdateResume updates the agent's resume/skills profile.
func (a *App) AgentNetUpdateResume(skills []string, domains []string, bio string) map[string]interface{} {
	c := a.initAgentNet()
	resume := &AgentNetResume{Skills: skills, Domains: domains, Bio: bio}
	if err := c.UpdateResume(resume); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Write to local cache.
	writeProfileCacheFields(nil, &bio, nil, skills, domains)
	return map[string]interface{}{"ok": true}
}

// AgentNetAssignTask assigns a task to a specific bidder.
func (a *App) AgentNetAssignTask(id, peerID string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.AssignTask(id, peerID); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetClaimTask claims an open task.
func (a *App) AgentNetClaimTask(id string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.ClaimTask(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetCreatePrediction creates a new prediction market question.
func (a *App) AgentNetCreatePrediction(question string, options []string) map[string]interface{} {
	c := a.initAgentNet()
	pred, err := c.CreatePrediction(question, options)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "prediction": pred}
}

// AgentNetPlaceBet places a bet on a prediction.
func (a *App) AgentNetPlaceBet(predID, option string, stake float64) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.PlaceBet(predID, option, stake); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetResolvePrediction resolves a prediction with the winning option.
func (a *App) AgentNetResolvePrediction(predID, result string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.ResolvePrediction(predID, result); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetAppealPrediction files an appeal against a prediction resolution.
func (a *App) AgentNetAppealPrediction(predID, reason string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.AppealPrediction(predID, reason); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetGetPredictionLeaderboard returns the prediction market leaderboard.
func (a *App) AgentNetGetPredictionLeaderboard() map[string]interface{} {
	c := a.initAgentNet()
	lb, err := c.GetPredictionLeaderboard()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "leaderboard": lb}
}

// AgentNetJoinSwarm joins an existing swarm session.
func (a *App) AgentNetJoinSwarm(sessionID string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.JoinSwarm(sessionID); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetContributeToSwarm adds a contribution to a swarm session.
func (a *App) AgentNetContributeToSwarm(sessionID, message, stance string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.ContributeToSwarm(sessionID, message, stance); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetSynthesizeSwarm triggers synthesis for a swarm session.
func (a *App) AgentNetSynthesizeSwarm(sessionID string) map[string]interface{} {
	c := a.initAgentNet()
	result, err := c.SynthesizeSwarm(sessionID)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "result": result}
}

// AgentNetReactKnowledge reacts to a knowledge entry.
func (a *App) AgentNetReactKnowledge(id, reaction string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.ReactKnowledge(id, reaction); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetReplyKnowledge replies to a knowledge entry.
func (a *App) AgentNetReplyKnowledge(id, body string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.ReplyKnowledge(id, body); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetGetKnowledgeReplies returns replies for a knowledge entry.
func (a *App) AgentNetGetKnowledgeReplies(id string) map[string]interface{} {
	c := a.initAgentNet()
	replies, err := c.GetKnowledgeReplies(id)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "replies": replies}
}

// AgentNetGetCreditsAudit returns the credit audit log.
func (a *App) AgentNetGetCreditsAudit() map[string]interface{} {
	c := a.initAgentNet()
	audit, err := c.GetCreditsAudit()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "audit": audit}
}

// AgentNetMatchAgentsForTask finds agents matching a task's requirements.
func (a *App) AgentNetMatchAgentsForTask(taskID string) map[string]interface{} {
	c := a.initAgentNet()
	agents, err := c.MatchAgentsForTask(taskID)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "agents": agents}
}

// ---------- Auction House Bindings ----------

// AgentNetSubmitTaskWork submits work for an auction-style task.
func (a *App) AgentNetSubmitTaskWork(id, result string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.SubmitTaskWork(id, result); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetGetTaskSubmissions returns all submissions for an auction-style task.
func (a *App) AgentNetGetTaskSubmissions(id string) map[string]interface{} {
	c := a.initAgentNet()
	subs, err := c.GetTaskSubmissions(id)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "submissions": subs}
}

// AgentNetPickTaskWinner selects the winning submission for an auction-style task.
func (a *App) AgentNetPickTaskWinner(id, winnerPeerID string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.PickTaskWinner(id, winnerPeerID); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ---------- Overlay Mesh Bindings ----------

// AgentNetGetOverlayStatus returns the overlay mesh network status.
func (a *App) AgentNetGetOverlayStatus() map[string]interface{} {
	c := a.initAgentNet()
	status, err := c.GetOverlayStatus()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "overlay": status}
}

// AgentNetGetOverlayPeersGeo returns overlay peers with geographic info.
func (a *App) AgentNetGetOverlayPeersGeo() map[string]interface{} {
	c := a.initAgentNet()
	peers, err := c.GetOverlayPeersGeo()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "peers": peers}
}

// ---------- Hub-relayed task discovery ----------

// AgentNetBrowseNetworkTasks fetches tasks from the Hub bulletin board
// (tasks published by other AgentNet peers) and merges them with local tasks.
func (a *App) AgentNetBrowseNetworkTasks() map[string]interface{} {
	c := a.initAgentNet()
	cfg, err := a.LoadConfig()
	if err != nil || cfg.RemoteHubURL == "" {
		return map[string]interface{}{"ok": false, "error": "Hub URL not configured"}
	}
	tasks, err := c.BrowseHubTasks(cfg.RemoteHubURL)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	if tasks == nil {
		tasks = []AgentNetTask{}
	}
	return map[string]interface{}{"ok": true, "tasks": tasks}
}

// AgentNetPublishTasksToHub pushes local open tasks to the Hub bulletin board
// so other peers can discover them.
func (a *App) AgentNetPublishTasksToHub() map[string]interface{} {
	c := a.initAgentNet()
	cfg, err := a.LoadConfig()
	if err != nil || cfg.RemoteHubURL == "" {
		return map[string]interface{}{"ok": false, "error": "Hub URL not configured"}
	}
	if err := c.PublishTasksToHub(cfg.RemoteHubURL); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ---------------------------------------------------------------------------
// Auto Task Picker — maClaw automatically picks up AgentNet tasks for credits
// ---------------------------------------------------------------------------

// AgentNetAutoPickerGetStatus returns the current auto-task-picker status.
func (a *App) AgentNetAutoPickerGetStatus() map[string]interface{} {
	a.ensureAutoTaskPicker()
	if a.autoTaskPicker == nil {
		return map[string]interface{}{
			"ok":      true,
			"enabled": false,
			"running": false,
		}
	}
	status := a.autoTaskPicker.GetStatus()
	status["ok"] = true
	return status
}

// AgentNetAutoPickerConfigure enables/disables and configures the auto-task-picker.
func (a *App) AgentNetAutoPickerConfigure(enabled bool, pollMinutes int, minReward float64, tags []string) map[string]interface{} {
	a.ensureAutoTaskPicker()
	if a.autoTaskPicker == nil {
		return map[string]interface{}{"ok": false, "error": "auto-task-picker not initialized"}
	}
	if enabled && !a.agentNetEnabled() {
		a.autoTaskPicker.Configure(false, pollMinutes, minReward, tags)
		a.autoTaskPicker.Stop()
		if cfg, err := a.LoadConfig(); err == nil {
			cfg.AgentNetAutoPickerEnabled = false
			if pollMinutes > 0 {
				cfg.AgentNetAutoPickerPollMin = pollMinutes
			}
			cfg.AgentNetAutoPickerMinReward = minReward
			_ = a.SaveConfig(cfg)
		}
		return map[string]interface{}{"ok": false, "error": "agentnet is disabled in settings"}
	}
	a.autoTaskPicker.Configure(enabled, pollMinutes, minReward, tags)

	if enabled {
		a.autoTaskPicker.Start()
		if ShowNotification != nil {
			ShowNotification("🦐 智网自动接单", "已开启自动接单模式，maClaw 将自动寻找并完成任务赚取 🐚", 1)
		}
	} else {
		a.autoTaskPicker.Stop()
	}

	// Persist to config so the setting survives restarts.
	if cfg, err := a.LoadConfig(); err == nil {
		cfg.AgentNetAutoPickerEnabled = enabled
		if pollMinutes > 0 {
			cfg.AgentNetAutoPickerPollMin = pollMinutes
		}
		cfg.AgentNetAutoPickerMinReward = minReward
		_ = a.SaveConfig(cfg)
	}

	return map[string]interface{}{"ok": true}
}

// AgentNetAutoPickerTriggerNow forces an immediate task poll (for testing/manual trigger).
func (a *App) AgentNetAutoPickerTriggerNow() map[string]interface{} {
	a.ensureAutoTaskPicker()
	if a.autoTaskPicker == nil {
		return map[string]interface{}{"ok": false, "error": "auto-task-picker not initialized"}
	}
	go a.autoTaskPicker.pollAndPickTask()
	return map[string]interface{}{"ok": true}
}

// AgentNetManualPickTask manually picks a specific task: claim → execute → submit.
// Returns detailed status/error for the frontend to display.
func (a *App) AgentNetManualPickTask(taskID string) map[string]interface{} {
	if taskID == "" {
		return map[string]interface{}{"ok": false, "error": "task ID is required"}
	}
	a.ensureAutoTaskPicker()
	if a.autoTaskPicker == nil {
		return map[string]interface{}{"ok": false, "error": "auto-task-picker not initialized"}
	}
	return a.autoTaskPicker.PickAndExecuteTask(taskID)
}

// ensureAutoTaskPicker lazily creates and wires the auto-task-picker.
// Thread-safe via sync.Once — safe to call from multiple goroutines.
func (a *App) ensureAutoTaskPicker() {
	a.autoPickerOnce.Do(func() {
		c := a.initAgentNet()
		cfg, err := a.LoadConfig()
		if err != nil {
			return
		}

		picker := NewAgentNetAutoTaskPicker(c, cfg.RemoteHubURL)

		// Wire the executor: send the task to the agent via the IM handler,
		// similar to how scheduled tasks work.
		picker.SetExecutor(func(taskTitle, taskDescription string) (string, error) {
			a.ensureInteractionInfra()
			hubClient := a.hubClient()
			if hubClient == nil {
				return "", fmt.Errorf("hub client not available")
			}

			// Prepend a hint so the agent knows this is an autonomous AgentNet task.
			actionText := fmt.Sprintf("[智网自动接单任务 — 请一次性完成，不要等待用户输入]\n任务: %s\n\n%s", taskTitle, taskDescription)

			handler := hubClient.ensureIMHandler()
			resp := handler.HandleIMMessageWithProgress(IMUserMessage{
				UserID:        "agentnet_auto_task",
				Platform:      "agentnet",
				Text:          actionText,
				MinIterations: 30,
				IsBackground:  true,
			}, func(text string) {
				// Progress callback — send to IM so user can see live updates.
				progressMsg := fmt.Sprintf("🦐 智网任务「%s」进度:\n%s", taskTitle, text)
				_ = hubClient.SendIMProactiveMessage(progressMsg)
			})

			if resp == nil {
				return "", fmt.Errorf("nil response from agent")
			}

			// Check for agent-level errors (same pattern as scheduled tasks).
			if resp.Error != "" {
				return resp.Text, fmt.Errorf("%s", resp.Error)
			}

			// Notify user of completion.
			resultText := resp.Text
			if resultText != "" {
				proactiveMsg := fmt.Sprintf("🦐 智网任务「%s」已完成:\n\n%s", taskTitle, resultText)
				_ = hubClient.SendIMProactiveMessage(proactiveMsg)
			}

			return resultText, nil
		})

		// Wire onChange to emit Wails event for frontend reactivity.
		picker.SetOnChange(func() {
			if a.ctx != nil {
				wailsrt.EventsEmit(a.ctx, "agentnet:auto-picker-changed")
			}
		})

		a.autoTaskPicker = picker

		// Restore saved auto-picker state from config so it survives restarts.
		if cfg.AgentNetAutoPickerEnabled && cfg.AgentNetEnabled {
			pollMin := cfg.AgentNetAutoPickerPollMin
			if pollMin <= 0 {
				pollMin = 5
			}
			picker.Configure(true, pollMin, cfg.AgentNetAutoPickerMinReward, nil)
			picker.Start()
		}
	})
}

// hubClient returns the current RemoteHubClient if available.
func (a *App) hubClient() *RemoteHubClient {
	if a.remoteSessions == nil {
		return nil
	}
	return a.remoteSessions.GetHubClient()
}

// ensureHubClient returns the existing RemoteHubClient or creates the fully
// wired default instance used by the AI assistant and remote launch flows.
func (a *App) ensureHubClient() *RemoteHubClient {
	if hubClient := a.hubClient(); hubClient != nil {
		return hubClient
	}
	return a.createAndWireHubClient()
}

// ---------------------------------------------------------------------------
// Nutshell Integration
// ---------------------------------------------------------------------------

func (a *App) nutshellMgr() *agentnet.NutshellManager {
	c := a.initAgentNet()
	return agentnet.NewNutshellManager(c.BinPath())
}

// AgentNetNutshellStatus checks if the nutshell CLI is installed.
func (a *App) AgentNetNutshellStatus() map[string]interface{} {
	st := a.nutshellMgr().IsInstalled()
	return map[string]interface{}{"ok": true, "installed": st.Installed, "version": st.Version, "error": st.Error}
}

// AgentNetNutshellInstall installs the nutshell CLI via agentnet.
func (a *App) AgentNetNutshellInstall() map[string]interface{} {
	emitter := func(stage string, pct int, msg string) {
		a.emitEvent("nutshell-install-progress", map[string]interface{}{
			"stage":   stage,
			"percent": pct,
			"message": msg,
		})
	}
	path, err := a.nutshellMgr().InstallWithProgress(emitter)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error(), "manualPath": agentnet.NutshellBinaryPath()}
	}
	return map[string]interface{}{"ok": true, "path": path}
}

// AgentNetNutshellInit initializes a new nutshell bundle in the given directory.
func (a *App) AgentNetNutshellInit(dir string) map[string]interface{} {
	out, err := a.nutshellMgr().Init(dir)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error(), "output": out}
	}
	return map[string]interface{}{"ok": true, "output": out}
}

// AgentNetNutshellCheck validates a nutshell bundle directory.
func (a *App) AgentNetNutshellCheck(dir string) map[string]interface{} {
	out, err := a.nutshellMgr().Check(dir)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error(), "output": out}
	}
	return map[string]interface{}{"ok": true, "output": out}
}

// AgentNetNutshellPublish publishes a nutshell bundle with a reward.
func (a *App) AgentNetNutshellPublish(dir string, reward float64) map[string]interface{} {
	out, err := a.nutshellMgr().Publish(dir, reward)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error(), "output": out}
	}
	return map[string]interface{}{"ok": true, "output": out}
}

// AgentNetNutshellClaim claims a task and creates a local workspace.
func (a *App) AgentNetNutshellClaim(taskID, outputDir string) map[string]interface{} {
	out, err := a.nutshellMgr().Claim(taskID, outputDir)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error(), "output": out}
	}
	return map[string]interface{}{"ok": true, "output": out}
}

// AgentNetNutshellDeliver submits completed work from a workspace directory.
func (a *App) AgentNetNutshellDeliver(dir string) map[string]interface{} {
	out, err := a.nutshellMgr().Deliver(dir)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error(), "output": out}
	}
	return map[string]interface{}{"ok": true, "output": out}
}

// AgentNetNutshellPack creates a .nut bundle file. peerID is optional for encryption.
func (a *App) AgentNetNutshellPack(dir, outputFile, peerID string) map[string]interface{} {
	out, err := a.nutshellMgr().Pack(dir, outputFile, peerID)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error(), "output": out}
	}
	return map[string]interface{}{"ok": true, "output": out}
}

// AgentNetNutshellUnpack extracts a .nut bundle file.
func (a *App) AgentNetNutshellUnpack(nutFile, outputDir string) map[string]interface{} {
	out, err := a.nutshellMgr().Unpack(nutFile, outputDir)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error(), "output": out}
	}
	return map[string]interface{}{"ok": true, "output": out}
}

// ========== P2P Service Gateway (skill.md §Workflow F) ==========

// AgentNetListServices returns locally registered P2P services.
func (a *App) AgentNetListServices() map[string]interface{} {
	c := a.initAgentNet()
	svcs, err := c.ListServices()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "services": svcs}
}

// AgentNetRegisterService registers a local HTTP service on the P2P network.
func (a *App) AgentNetRegisterService(name, localURL, description string, tags []string, modes []string, billing string, price float64, freeTier int) map[string]interface{} {
	c := a.initAgentNet()
	reg := &AgentNetServiceRegistration{
		Name: name, URL: localURL, Description: description,
		Tags: tags, Modes: modes, Billing: billing,
		Price: price, FreeTier: freeTier,
	}
	if err := c.RegisterService(reg); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetUnregisterService removes a registered service.
func (a *App) AgentNetUnregisterService(name string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.UnregisterService(name); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetCallService calls a remote peer's service (request-response).
func (a *App) AgentNetCallService(peer, service, method, path string, headers map[string]string, body string) map[string]interface{} {
	c := a.initAgentNet()
	result, err := c.CallService(peer, service, method, path, headers, body)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "result": result}
}

// AgentNetDiscoverServices discovers services on a remote peer.
func (a *App) AgentNetDiscoverServices(peer string) map[string]interface{} {
	c := a.initAgentNet()
	svcs, err := c.DiscoverServices(peer)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "services": svcs}
}

// ========== ANS (Agent Name Service) ==========

// AgentNetANSRegister registers an ANS name with skill tags.
func (a *App) AgentNetANSRegister(name, tags string) map[string]interface{} {
	c := a.initAgentNet()
	entry, err := c.ANSRegister(name, tags)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entry": entry}
}

// AgentNetANSResolve resolves an ANS name to a DID.
func (a *App) AgentNetANSResolve(name string) map[string]interface{} {
	c := a.initAgentNet()
	entry, err := c.ANSResolve(name)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "name": entry.Name, "did": entry.DID, "tags": entry.Tags}
}

// AgentNetANSLookup finds agents by skill tags.
func (a *App) AgentNetANSLookup(tags string, limit int) map[string]interface{} {
	c := a.initAgentNet()
	entries, err := c.ANSLookup(tags, limit)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entries": entries}
}

// ========== Agent Discovery ==========

// AgentNetDiscoverAgents performs full-text agent search.
func (a *App) AgentNetDiscoverAgents(query string) map[string]interface{} {
	c := a.initAgentNet()
	agents, err := c.DiscoverAgents(query)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "agents": agents}
}

// AgentNetCrossDomainSearch performs cross-domain search.
func (a *App) AgentNetCrossDomainSearch(query string) map[string]interface{} {
	c := a.initAgentNet()
	results, err := c.CrossDomainSearch(query)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "results": results}
}

// AgentNetFindClaw performs semantic knowledge search (FindClaw).
func (a *App) AgentNetFindClaw(query string) map[string]interface{} {
	c := a.initAgentNet()
	entries, err := c.FindAgent(query)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entries": entries}
}

// ========== Reputation ==========

// AgentNetGetReputation returns the reputation score for a DID.
func (a *App) AgentNetGetReputation(did string) map[string]interface{} {
	c := a.initAgentNet()
	rep, err := c.GetReputation(did)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "did": rep.DID, "score": rep.Score, "tier": rep.Tier}
}

// ========== Proof of Intelligence (PoI) ==========

// AgentNetListPoIChallenges returns available PoI challenges.
func (a *App) AgentNetListPoIChallenges() map[string]interface{} {
	c := a.initAgentNet()
	challenges, err := c.ListPoIChallenges()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "challenges": challenges}
}

// AgentNetRespondToPoI submits a response to a PoI challenge.
func (a *App) AgentNetRespondToPoI(challengeID, response string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.RespondToPoI(challengeID, map[string]interface{}{"response": response}); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetGetPoIScores returns PoI intelligence ranking scores.
func (a *App) AgentNetGetPoIScores() map[string]interface{} {
	c := a.initAgentNet()
	scores, err := c.GetPoIScores()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "scores": scores}
}

// ========== Agent Card & Init ==========

// AgentNetPublishAgentCard publishes the agent's profile card to the network.
func (a *App) AgentNetPublishAgentCard(name, desc string, skills []string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.PublishAgentCard(name, desc, skills); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetInitAgent initializes the agent identity and profile.
func (a *App) AgentNetInitAgent(name string, skills []string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.InitAgent(name, skills); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ========== Credits Transfer ==========

// AgentNetTransferCredits transfers Shell credits to another agent.
func (a *App) AgentNetTransferCredits(toDID string, amount float64, reason string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.TransferCredits(toDID, amount, reason); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ========== Task Bundles ==========

// AgentNetAttachBundle attaches a .nut bundle to a task (base64-encoded data).
func (a *App) AgentNetAttachBundle(taskID, base64Data string) map[string]interface{} {
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": "invalid base64: " + err.Error()}
	}
	c := a.initAgentNet()
	if err := c.AttachBundle(taskID, data); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// AgentNetDownloadBundle downloads a .nut bundle from a task (returns base64).
func (a *App) AgentNetDownloadBundle(taskID string) map[string]interface{} {
	c := a.initAgentNet()
	data, err := c.DownloadBundle(taskID)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "data": base64.StdEncoding.EncodeToString(data), "size": len(data)}
}

// ========== Split Tasks ==========

// AgentNetCreateSplitTask creates a multi-slot task.
func (a *App) AgentNetCreateSplitTask(title string, reward float64, slots int) map[string]interface{} {
	c := a.initAgentNet()
	task, err := c.CreateSplitTask(title, reward, slots)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "task": task}
}

// ========== Disputes ==========

// AgentNetFileDispute files a dispute for a rejected task.
func (a *App) AgentNetFileDispute(taskID, reason string) map[string]interface{} {
	c := a.initAgentNet()
	if err := c.FileDispute(taskID, reason); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ========== DAG & Ontology ==========

// AgentNetExtractDAG extracts a structured DAG from task steps.
func (a *App) AgentNetExtractDAG(intent string, steps []string, outputs []string) map[string]interface{} {
	c := a.initAgentNet()
	nodes, err := c.ExtractDAG(intent, steps, outputs)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "nodes": nodes}
}

// AgentNetQueryOntology queries the knowledge graph for a subgraph.
func (a *App) AgentNetQueryOntology(query string, depth int) map[string]interface{} {
	c := a.initAgentNet()
	result, err := c.QueryOntology(query, depth)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "result": result}
}
