package main

// app_clawnet.go — Wails bindings for ClawNet integration.
// Exposes ClawNet P2P network features to the frontend via the App struct.

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

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// initClawNet lazily creates the ClawNet client on first use.
func (a *App) initClawNet() *ClawNetClient {
	if a.clawNetClient == nil {
		a.clawNetClient = NewClawNetClient()
	}
	return a.clawNetClient
}

// ---------- Wails-exposed methods ----------

// ClawNetEnsureDaemon starts the ClawNet daemon if not running.
func (a *App) ClawNetEnsureDaemon() map[string]interface{} {
	c := a.initClawNet()
	if err := c.EnsureDaemon(); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Start background auto-update (24h cycle, immediate if stale).
	c.StartAutoUpdate(func(msg string) { a.log(msg) })
	return map[string]interface{}{"ok": true}
}

// ClawNetStopDaemon stops the ClawNet daemon.
func (a *App) ClawNetStopDaemon() {
	if a.clawNetClient != nil {
		a.clawNetClient.StopDaemon()
	}
}

// ClawNetIsRunning checks if the daemon is reachable.
func (a *App) ClawNetIsRunning() bool {
	return a.clawNetClient != nil && a.clawNetClient.IsRunning()
}

// ClawNetGetStatus returns node status.
func (a *App) ClawNetGetStatus() map[string]interface{} {
	c := a.initClawNet()
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

// ClawNetGetPeers returns connected peers.
func (a *App) ClawNetGetPeers() map[string]interface{} {
	c := a.initClawNet()
	peers, err := c.GetPeers()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "peers": peers}
}

// ClawNetListTasks lists tasks with optional status filter.
func (a *App) ClawNetListTasks(status string) map[string]interface{} {
	c := a.initClawNet()
	tasks, err := c.ListTasks(status)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "tasks": tasks}
}

// ClawNetCreateTask posts a new task to the network.
func (a *App) ClawNetCreateTask(title string, reward float64) map[string]interface{} {
	c := a.initClawNet()
	task, err := c.CreateTask(title, reward)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "task": task}
}

// ClawNetCreateTaskFull creates a task with description, tags, and optional target peer.
func (a *App) ClawNetCreateTaskFull(title, description string, reward float64, tags []string, targetPeer string) map[string]interface{} {
	c := a.initClawNet()
	task, err := c.CreateTaskFull(title, description, reward, tags, targetPeer)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "task": task}
}

// ClawNetGetCredits returns Shell balance and tier info.
func (a *App) ClawNetGetCredits() map[string]interface{} {
	c := a.initClawNet()
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

// ClawNetSearchKnowledge searches the knowledge mesh.
func (a *App) ClawNetSearchKnowledge(query string) map[string]interface{} {
	c := a.initClawNet()
	entries, err := c.SearchKnowledge(query)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entries": entries}
}

// ClawNetPublishKnowledge publishes a knowledge entry.
func (a *App) ClawNetPublishKnowledge(title, body string) map[string]interface{} {
	c := a.initClawNet()
	entry, err := c.PublishKnowledge(title, body)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entry": entry}
}

// ClawNetPublishKnowledgeFull publishes a knowledge entry with domain tags.
func (a *App) ClawNetPublishKnowledgeFull(title, body string, domains []string) map[string]interface{} {
	c := a.initClawNet()
	entry, err := c.PublishKnowledgeFull(title, body, domains)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entry": entry}
}

// ClawNetSendDM sends an encrypted direct message.
func (a *App) ClawNetSendDM(peerID, body string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.SendDM(peerID, body); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetGetDMInbox returns the DM inbox.
func (a *App) ClawNetGetDMInbox() map[string]interface{} {
	c := a.initClawNet()
	inbox, err := c.GetDMInbox()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "inbox": inbox}
}

// ClawNetListSwarmSessions lists active Swarm Think sessions.
func (a *App) ClawNetListSwarmSessions() map[string]interface{} {
	c := a.initClawNet()
	sessions, err := c.ListSwarmSessions()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "sessions": sessions}
}

// ClawNetCreateSwarmSession starts a new Swarm Think session.
func (a *App) ClawNetCreateSwarmSession(topic, question string) map[string]interface{} {
	c := a.initClawNet()
	session, err := c.CreateSwarmSession(topic, question)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "session": session}
}

// ClawNetGetResume returns the agent's profile (with local cache fallback).
func (a *App) ClawNetGetResume() map[string]interface{} {
	c := a.initClawNet()
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

// ClawNetListPredictions lists active prediction markets.
func (a *App) ClawNetListPredictions() map[string]interface{} {
	c := a.initClawNet()
	preds, err := c.ListPredictions()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "predictions": preds}
}

// ClawNetInstallBinary downloads the clawnet binary from GitHub Releases.
// Emits "clawnet-install-progress" events to the frontend during download.
func (a *App) ClawNetInstallBinary() map[string]interface{} {
	emitter := func(stage string, pct int, msg string) {
		a.emitEvent("clawnet-install-progress", map[string]interface{}{
			"stage":   stage,
			"percent": pct,
			"message": msg,
		})
	}
	path, err := DownloadClawNet(emitter)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "path": path}
}

// ClawNetEnsureDaemonWithDownload starts the daemon, auto-downloading if needed.
// Emits "clawnet-install-progress" events during download.
func (a *App) ClawNetEnsureDaemonWithDownload() map[string]interface{} {
	c := a.initClawNet()
	emitter := func(stage string, pct int, msg string) {
		a.emitEvent("clawnet-install-progress", map[string]interface{}{
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

// ClawNetGetBinaryPath returns the resolved clawnet binary path (for diagnostics).
func (a *App) ClawNetGetBinaryPath() string {
	c := a.initClawNet()
	if c.binPath != "" {
		return c.binPath
	}
	p := c.findBinary()
	if p == "" {
		return "not found (searched vendor/clawnet/, ~/.openclaw/clawnet/, PATH)"
	}
	return p
}

// ---------- Profile (with local cache fallback) ----------

// profileCachePath returns ~/.openclaw/clawnet/profile_cache.json
func profileCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "clawnet", "profile_cache.json")
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

func (a *App) ClawNetGetProfile() map[string]interface{} {
	c := a.initClawNet()
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

func (a *App) ClawNetUpdateProfile(name, bio string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.UpdateProfile(name, bio); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Write to local cache.
	writeProfileCacheFields(&name, &bio, nil, nil, nil)
	return map[string]interface{}{"ok": true}
}

func (a *App) ClawNetSetMotto(motto string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.SetMotto(motto); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	writeProfileCacheFields(nil, nil, &motto, nil, nil)
	return map[string]interface{}{"ok": true}
}

// ---------- Daemon Info ----------

// ClawNetGetDaemonInfo returns daemon process info for diagnostics display.
// Returns PID (if we launched it), binary path, and version.
// The caller (frontend) already knows the alive/running state from ClawNetIsRunning.
func (a *App) ClawNetGetDaemonInfo() map[string]interface{} {
	c := a.initClawNet()
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

func (a *App) ClawNetListTopics() map[string]interface{} {
	c := a.initClawNet()
	topics, err := c.ListTopics()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "topics": topics}
}

func (a *App) ClawNetCreateTopic(name, description string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.CreateTopic(name, description); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) ClawNetGetTopicMessages(topicName string) map[string]interface{} {
	c := a.initClawNet()
	msgs, err := c.GetTopicMessages(topicName)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "messages": msgs}
}

func (a *App) ClawNetPostTopicMessage(topicName, body string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.PostTopicMessage(topicName, body); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ---------- Task Bazaar (extended) ----------

func (a *App) ClawNetBidOnTask(id string, amount float64, message string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.BidOnTask(id, amount, message); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) ClawNetSubmitTaskResult(id, result string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.SubmitTaskResult(id, result); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) ClawNetApproveTask(id string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.ApproveTask(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) ClawNetRejectTask(id string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.RejectTask(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) ClawNetCancelTask(id string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.CancelTask(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) ClawNetGetTaskBids(id string) map[string]interface{} {
	c := a.initClawNet()
	bids, err := c.GetTaskBids(id)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "bids": bids}
}

func (a *App) ClawNetMatchTasks() map[string]interface{} {
	c := a.initClawNet()
	tasks, err := c.MatchTasks()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "tasks": tasks}
}

func (a *App) ClawNetGetTaskBoard() map[string]interface{} {
	c := a.initClawNet()
	board, err := c.GetTaskBoard()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "board": board}
}

// ---------- Credits (extended) ----------

func (a *App) ClawNetGetTransactions() map[string]interface{} {
	c := a.initClawNet()
	txns, err := c.GetCreditsTransactions()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "transactions": txns}
}

func (a *App) ClawNetGetLeaderboard() map[string]interface{} {
	c := a.initClawNet()
	lb, err := c.GetLeaderboard()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "leaderboard": lb}
}

// ---------- Diagnostics ----------

func (a *App) ClawNetGetDiagnostics() map[string]interface{} {
	c := a.initClawNet()
	diag, err := c.GetDiagnostics()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "diagnostics": diag}
}

func (a *App) ClawNetSelfUpdate() map[string]interface{} {
	c := a.initClawNet()
	if err := c.SelfUpdate(); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ---------- Knowledge Feed ----------

func (a *App) ClawNetGetKnowledgeFeed(domain string, limit int) map[string]interface{} {
	c := a.initClawNet()
	entries, err := c.GetKnowledgeFeed(domain, limit)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "entries": entries}
}

// ---------- DM Thread ----------

func (a *App) ClawNetGetDMThread(peerID string, limit int) map[string]interface{} {
	c := a.initClawNet()
	thread, err := c.GetDMThread(peerID, limit)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "thread": thread}
}

// ---------- Identity Key Backup / Restore ----------

// clawnetIdentityKeyPath returns the path to ~/.openclaw/clawnet/identity.key
func clawnetIdentityKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw", "clawnet", "identity.key"), nil
}

// ClawNetHasIdentity checks whether an identity.key file exists.
func (a *App) ClawNetHasIdentity() map[string]interface{} {
	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		return map[string]interface{}{"ok": true, "exists": false, "path": keyPath}
	}
	return map[string]interface{}{"ok": true, "exists": true, "path": keyPath, "size": info.Size()}
}

// ClawNetExportIdentity copies identity.key to a user-chosen location via save dialog.
func (a *App) ClawNetExportIdentity() map[string]interface{} {
	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	if _, err := os.Stat(keyPath); err != nil {
		return map[string]interface{}{"ok": false, "error": "identity.key not found — daemon may not have been initialized"}
	}

	dest, err := wailsrt.SaveFileDialog(a.ctx, wailsrt.SaveDialogOptions{
		Title:           "Export ClawNet Identity Key",
		DefaultFilename: "clawnet-identity.key",
		Filters: []wailsrt.FileFilter{
			{DisplayName: "Key Files", Pattern: "*.key"},
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
	if err != nil || dest == "" {
		return map[string]interface{}{"ok": false, "error": "cancelled"}
	}

	if err := clawnetCopyFile(keyPath, dest); err != nil {
		a.log(fmt.Sprintf("ClawNet: export identity key failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log(fmt.Sprintf("ClawNet: identity key exported to %s", dest))
	return map[string]interface{}{"ok": true, "path": dest}
}

// ClawNetImportIdentity restores identity.key from a user-chosen file via open dialog.
// Stops daemon before importing, restarts after successful import.
func (a *App) ClawNetImportIdentity() map[string]interface{} {
	src, err := wailsrt.OpenFileDialog(a.ctx, wailsrt.OpenDialogOptions{
		Title: "Import ClawNet Identity Key",
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
		a.log(fmt.Sprintf("ClawNet: import identity — cannot read source file: %v", err))
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("cannot read file: %v", err)}
	}
	// Sanity check: Ed25519 key files are small (typically < 1KB)
	if info.Size() > 10*1024 {
		a.log(fmt.Sprintf("ClawNet: import identity — file too large (%d bytes)", info.Size()))
		return map[string]interface{}{"ok": false, "error": "file too large — does not look like an identity key"}
	}

	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	// Stop daemon before replacing key
	if a.clawNetClient != nil && a.clawNetClient.IsRunning() {
		a.log("ClawNet: stopping daemon before key import")
		a.clawNetClient.StopDaemon()
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		a.log(fmt.Sprintf("ClawNet: import identity — mkdir failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	// Backup existing key if present
	if _, err := os.Stat(keyPath); err == nil {
		if renameErr := os.Rename(keyPath, keyPath+".bak"); renameErr != nil {
			a.log(fmt.Sprintf("ClawNet: import identity — backup rename failed: %v", renameErr))
		} else {
			a.log(fmt.Sprintf("ClawNet: existing identity key backed up to %s.bak", keyPath))
		}
	}

	if err := clawnetCopyFile(src, keyPath); err != nil {
		a.log(fmt.Sprintf("ClawNet: import identity — copy failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log(fmt.Sprintf("ClawNet: identity key imported from %s", src))

	// Restart daemon with new identity
	restarted := false
	c := a.initClawNet()
	if err := c.EnsureDaemon(); err != nil {
		a.log(fmt.Sprintf("ClawNet: daemon restart after import failed: %v", err))
	} else {
		restarted = true
		a.log("ClawNet: daemon restarted with new identity")
	}

	return map[string]interface{}{"ok": true, "path": keyPath, "restarted": restarted}
}

// clawnetCopyFile copies src to dst atomically via temp file.
// Preserves 0600 permissions for security-sensitive files like identity keys.
func clawnetCopyFile(src, dst string) error {
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

// ClawNetOnlineBackupKey encrypts the identity key with the user's password
// and uploads it to the Hub, bound to the user's email.
func (a *App) ClawNetOnlineBackupKey(password string) map[string]interface{} {
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

	keyPath, err := clawnetIdentityKeyPath()
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
	resp, err := client.Post(hubURL+"/api/clawnet/key/backup", "application/json", bytes.NewReader(payload))
	if err != nil {
		a.log(fmt.Sprintf("ClawNet: online backup request failed for %s: %v", email, err))
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
		a.log(fmt.Sprintf("ClawNet: online backup failed for %s: %s", email, errMsg))
		return map[string]interface{}{"ok": false, "error": errMsg}
	}
	a.log(fmt.Sprintf("ClawNet: identity key backed up to Hub for %s", email))
	return map[string]interface{}{"ok": true}
}

// ClawNetOnlineRestoreKey downloads and decrypts the identity key from the Hub.
// Stops daemon before replacing key, restarts after successful restore.
func (a *App) ClawNetOnlineRestoreKey(password string) map[string]interface{} {
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
	resp, err := client.Post(hubURL+"/api/clawnet/key/restore", "application/json", bytes.NewReader(payload))
	if err != nil {
		a.log(fmt.Sprintf("ClawNet: online restore request failed for %s: %v", email, err))
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
		a.log(fmt.Sprintf("ClawNet: online restore failed for %s: %s", email, errMsg))
		return map[string]interface{}{"ok": false, "error": errMsg}
	}

	keyDataB64, _ := result["key_data"].(string)
	if keyDataB64 == "" {
		a.log(fmt.Sprintf("ClawNet: online restore — empty key data in response for %s", email))
		return map[string]interface{}{"ok": false, "error": "empty key data in response"}
	}
	keyData, err := base64.StdEncoding.DecodeString(keyDataB64)
	if err != nil {
		a.log(fmt.Sprintf("ClawNet: online restore — invalid key encoding for %s: %v", email, err))
		return map[string]interface{}{"ok": false, "error": "invalid key data encoding"}
	}

	// Stop daemon before replacing key
	if a.clawNetClient != nil && a.clawNetClient.IsRunning() {
		a.log("ClawNet: stopping daemon before online key restore")
		a.clawNetClient.StopDaemon()
	}

	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		a.log(fmt.Sprintf("ClawNet: online restore — mkdir failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Backup existing key
	if _, err := os.Stat(keyPath); err == nil {
		if renameErr := os.Rename(keyPath, keyPath+".bak"); renameErr != nil {
			a.log(fmt.Sprintf("ClawNet: online restore — backup rename failed: %v", renameErr))
		} else {
			a.log(fmt.Sprintf("ClawNet: existing identity key backed up to %s.bak", keyPath))
		}
	}
	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		a.log(fmt.Sprintf("ClawNet: online restore — write key failed: %v", err))
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log(fmt.Sprintf("ClawNet: identity key restored from Hub for %s", email))

	// Restart daemon with new identity
	restarted := false
	c := a.initClawNet()
	if err := c.EnsureDaemon(); err != nil {
		a.log(fmt.Sprintf("ClawNet: daemon restart after online restore failed: %v", err))
	} else {
		restarted = true
		a.log("ClawNet: daemon restarted with restored identity")
	}

	return map[string]interface{}{"ok": true, "path": keyPath, "restarted": restarted}
}

// ---------- Missing Wails Bindings ----------

// ClawNetUpdateResume updates the agent's resume/skills profile.
func (a *App) ClawNetUpdateResume(skills []string, domains []string, bio string) map[string]interface{} {
	c := a.initClawNet()
	resume := &ClawNetResume{Skills: skills, Domains: domains, Bio: bio}
	if err := c.UpdateResume(resume); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Write to local cache.
	writeProfileCacheFields(nil, &bio, nil, skills, domains)
	return map[string]interface{}{"ok": true}
}

// ClawNetAssignTask assigns a task to a specific bidder.
func (a *App) ClawNetAssignTask(id, peerID string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.AssignTask(id, peerID); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetClaimTask claims an open task.
func (a *App) ClawNetClaimTask(id string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.ClaimTask(id); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetCreatePrediction creates a new prediction market question.
func (a *App) ClawNetCreatePrediction(question string, options []string) map[string]interface{} {
	c := a.initClawNet()
	pred, err := c.CreatePrediction(question, options)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "prediction": pred}
}

// ClawNetPlaceBet places a bet on a prediction.
func (a *App) ClawNetPlaceBet(predID, option string, stake float64) map[string]interface{} {
	c := a.initClawNet()
	if err := c.PlaceBet(predID, option, stake); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetResolvePrediction resolves a prediction with the winning option.
func (a *App) ClawNetResolvePrediction(predID, result string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.ResolvePrediction(predID, result); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetAppealPrediction files an appeal against a prediction resolution.
func (a *App) ClawNetAppealPrediction(predID, reason string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.AppealPrediction(predID, reason); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetGetPredictionLeaderboard returns the prediction market leaderboard.
func (a *App) ClawNetGetPredictionLeaderboard() map[string]interface{} {
	c := a.initClawNet()
	lb, err := c.GetPredictionLeaderboard()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "leaderboard": lb}
}

// ClawNetJoinSwarm joins an existing swarm session.
func (a *App) ClawNetJoinSwarm(sessionID string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.JoinSwarm(sessionID); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetContributeToSwarm adds a contribution to a swarm session.
func (a *App) ClawNetContributeToSwarm(sessionID, message, stance string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.ContributeToSwarm(sessionID, message, stance); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetSynthesizeSwarm triggers synthesis for a swarm session.
func (a *App) ClawNetSynthesizeSwarm(sessionID string) map[string]interface{} {
	c := a.initClawNet()
	result, err := c.SynthesizeSwarm(sessionID)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "result": result}
}

// ClawNetReactKnowledge reacts to a knowledge entry.
func (a *App) ClawNetReactKnowledge(id, reaction string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.ReactKnowledge(id, reaction); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetReplyKnowledge replies to a knowledge entry.
func (a *App) ClawNetReplyKnowledge(id, body string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.ReplyKnowledge(id, body); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetGetKnowledgeReplies returns replies for a knowledge entry.
func (a *App) ClawNetGetKnowledgeReplies(id string) map[string]interface{} {
	c := a.initClawNet()
	replies, err := c.GetKnowledgeReplies(id)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "replies": replies}
}

// ClawNetGetCreditsAudit returns the credit audit log.
func (a *App) ClawNetGetCreditsAudit() map[string]interface{} {
	c := a.initClawNet()
	audit, err := c.GetCreditsAudit()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "audit": audit}
}

// ClawNetMatchAgentsForTask finds agents matching a task's requirements.
func (a *App) ClawNetMatchAgentsForTask(taskID string) map[string]interface{} {
	c := a.initClawNet()
	agents, err := c.MatchAgentsForTask(taskID)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "agents": agents}
}

// ---------- Auction House Bindings ----------

// ClawNetSubmitTaskWork submits work for an auction-style task.
func (a *App) ClawNetSubmitTaskWork(id, result string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.SubmitTaskWork(id, result); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ClawNetGetTaskSubmissions returns all submissions for an auction-style task.
func (a *App) ClawNetGetTaskSubmissions(id string) map[string]interface{} {
	c := a.initClawNet()
	subs, err := c.GetTaskSubmissions(id)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "submissions": subs}
}

// ClawNetPickTaskWinner selects the winning submission for an auction-style task.
func (a *App) ClawNetPickTaskWinner(id, winnerPeerID string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.PickTaskWinner(id, winnerPeerID); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

// ---------- Overlay Mesh Bindings ----------

// ClawNetGetOverlayStatus returns the overlay mesh network status.
func (a *App) ClawNetGetOverlayStatus() map[string]interface{} {
	c := a.initClawNet()
	status, err := c.GetOverlayStatus()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "overlay": status}
}

// ClawNetGetOverlayPeersGeo returns overlay peers with geographic info.
func (a *App) ClawNetGetOverlayPeersGeo() map[string]interface{} {
	c := a.initClawNet()
	peers, err := c.GetOverlayPeersGeo()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "peers": peers}
}

// ---------- Hub-relayed task discovery ----------

// ClawNetBrowseNetworkTasks fetches tasks from the Hub bulletin board
// (tasks published by other ClawNet peers) and merges them with local tasks.
func (a *App) ClawNetBrowseNetworkTasks() map[string]interface{} {
	c := a.initClawNet()
	cfg, err := a.LoadConfig()
	if err != nil || cfg.RemoteHubURL == "" {
		return map[string]interface{}{"ok": false, "error": "Hub URL not configured"}
	}
	tasks, err := c.BrowseHubTasks(cfg.RemoteHubURL)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	if tasks == nil {
		tasks = []ClawNetTask{}
	}
	return map[string]interface{}{"ok": true, "tasks": tasks}
}

// ClawNetPublishTasksToHub pushes local open tasks to the Hub bulletin board
// so other peers can discover them.
func (a *App) ClawNetPublishTasksToHub() map[string]interface{} {
	c := a.initClawNet()
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
// Auto Task Picker — maClaw automatically picks up ClawNet tasks for credits
// ---------------------------------------------------------------------------

// ClawNetAutoPickerGetStatus returns the current auto-task-picker status.
func (a *App) ClawNetAutoPickerGetStatus() map[string]interface{} {
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

// ClawNetAutoPickerConfigure enables/disables and configures the auto-task-picker.
func (a *App) ClawNetAutoPickerConfigure(enabled bool, pollMinutes int, minReward float64, tags []string) map[string]interface{} {
	a.ensureAutoTaskPicker()
	if a.autoTaskPicker == nil {
		return map[string]interface{}{"ok": false, "error": "auto-task-picker not initialized"}
	}
	a.autoTaskPicker.Configure(enabled, pollMinutes, minReward, tags)

	if enabled {
		a.autoTaskPicker.Start()
		if ShowNotification != nil {
			ShowNotification("🦐 虾网自动接单", "已开启自动接单模式，maClaw 将自动寻找并完成任务赚取 🐚", 1)
		}
	} else {
		a.autoTaskPicker.Stop()
	}

	// Persist to config so the setting survives restarts.
	if cfg, err := a.LoadConfig(); err == nil {
		cfg.ClawNetAutoPickerEnabled = enabled
		if pollMinutes > 0 {
			cfg.ClawNetAutoPickerPollMin = pollMinutes
		}
		cfg.ClawNetAutoPickerMinReward = minReward
		_ = a.SaveConfig(cfg)
	}

	return map[string]interface{}{"ok": true}
}

// ClawNetAutoPickerTriggerNow forces an immediate task poll (for testing/manual trigger).
func (a *App) ClawNetAutoPickerTriggerNow() map[string]interface{} {
	a.ensureAutoTaskPicker()
	if a.autoTaskPicker == nil {
		return map[string]interface{}{"ok": false, "error": "auto-task-picker not initialized"}
	}
	go a.autoTaskPicker.pollAndPickTask()
	return map[string]interface{}{"ok": true}
}

// ClawNetManualPickTask manually picks a specific task: claim → execute → submit.
// Returns detailed status/error for the frontend to display.
func (a *App) ClawNetManualPickTask(taskID string) map[string]interface{} {
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
		c := a.initClawNet()
		cfg, err := a.LoadConfig()
		if err != nil {
			return
		}

		picker := NewClawNetAutoTaskPicker(c, cfg.RemoteHubURL)

		// Wire the executor: send the task to the agent via the IM handler,
		// similar to how scheduled tasks work.
		picker.SetExecutor(func(taskTitle, taskDescription string) (string, error) {
			a.ensureRemoteInfra()
			hubClient := a.hubClient()
			if hubClient == nil {
				return "", fmt.Errorf("hub client not available")
			}

			// Prepend a hint so the agent knows this is an autonomous ClawNet task.
			actionText := fmt.Sprintf("[虾网自动接单任务 — 请一次性完成，不要等待用户输入]\n任务: %s\n\n%s", taskTitle, taskDescription)

			resp := hubClient.imHandler.HandleIMMessageWithProgress(IMUserMessage{
				UserID:        "clawnet_auto_task",
				Platform:      "clawnet",
				Text:          actionText,
				MinIterations: 30,
				IsBackground:  true,
			}, func(text string) {
				// Progress callback — send to IM so user can see live updates.
				progressMsg := fmt.Sprintf("🦐 虾网任务「%s」进度:\n%s", taskTitle, text)
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
				proactiveMsg := fmt.Sprintf("🦐 虾网任务「%s」已完成:\n\n%s", taskTitle, resultText)
				_ = hubClient.SendIMProactiveMessage(proactiveMsg)
			}

			return resultText, nil
		})

		// Wire onChange to emit Wails event for frontend reactivity.
		picker.SetOnChange(func() {
			if a.ctx != nil {
				wailsrt.EventsEmit(a.ctx, "clawnet:auto-picker-changed")
			}
		})

		a.autoTaskPicker = picker

		// Restore saved auto-picker state from config so it survives restarts.
		if cfg.ClawNetAutoPickerEnabled {
			pollMin := cfg.ClawNetAutoPickerPollMin
			if pollMin <= 0 {
				pollMin = 5
			}
			picker.Configure(true, pollMin, cfg.ClawNetAutoPickerMinReward, nil)
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
