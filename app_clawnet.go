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

// ClawNetGetCredits returns Shell balance and tier info.
func (a *App) ClawNetGetCredits() map[string]interface{} {
	c := a.initClawNet()
	credits, err := c.GetCredits()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{
		"ok":      true,
		"balance": credits.Balance,
		"tier":    credits.Tier,
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

// ClawNetGetResume returns the agent's profile.
func (a *App) ClawNetGetResume() map[string]interface{} {
	c := a.initClawNet()
	r, err := c.GetResume()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
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

// ---------- Profile ----------

func (a *App) ClawNetGetProfile() map[string]interface{} {
	c := a.initClawNet()
	p, err := c.GetProfile()
	if err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true, "profile": p}
}

func (a *App) ClawNetUpdateProfile(name, bio string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.UpdateProfile(name, bio); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
}

func (a *App) ClawNetSetMotto(motto string) map[string]interface{} {
	c := a.initClawNet()
	if err := c.SetMotto(motto); err != nil {
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	return map[string]interface{}{"ok": true}
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

	if err := copyFile(keyPath, dest); err != nil {
		a.log("ClawNet: export identity key failed: %v", err)
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log("ClawNet: identity key exported to %s", dest)
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
		a.log("ClawNet: import identity — cannot read source file: %v", err)
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("cannot read file: %v", err)}
	}
	// Sanity check: Ed25519 key files are small (typically < 1KB)
	if info.Size() > 10*1024 {
		a.log("ClawNet: import identity — file too large (%d bytes)", info.Size())
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
		a.log("ClawNet: import identity — mkdir failed: %v", err)
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}

	// Backup existing key if present
	if _, err := os.Stat(keyPath); err == nil {
		if renameErr := os.Rename(keyPath, keyPath+".bak"); renameErr != nil {
			a.log("ClawNet: import identity — backup rename failed: %v", renameErr)
		} else {
			a.log("ClawNet: existing identity key backed up to %s.bak", keyPath)
		}
	}

	if err := copyFile(src, keyPath); err != nil {
		a.log("ClawNet: import identity — copy failed: %v", err)
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log("ClawNet: identity key imported from %s", src)

	// Restart daemon with new identity
	restarted := false
	c := a.initClawNet()
	if err := c.EnsureDaemon(); err != nil {
		a.log("ClawNet: daemon restart after import failed: %v", err)
	} else {
		restarted = true
		a.log("ClawNet: daemon restarted with new identity")
	}

	return map[string]interface{}{"ok": true, "path": keyPath, "restarted": restarted}
}

// copyFile copies src to dst atomically via temp file.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
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
		a.log("ClawNet: online backup request failed for %s: %v", email, err)
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
		a.log("ClawNet: online backup failed for %s: %s", email, errMsg)
		return map[string]interface{}{"ok": false, "error": errMsg}
	}
	a.log("ClawNet: identity key backed up to Hub for %s", email)
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
		a.log("ClawNet: online restore request failed for %s: %v", email, err)
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
		a.log("ClawNet: online restore failed for %s: %s", email, errMsg)
		return map[string]interface{}{"ok": false, "error": errMsg}
	}

	keyDataB64, _ := result["key_data"].(string)
	if keyDataB64 == "" {
		a.log("ClawNet: online restore — empty key data in response for %s", email)
		return map[string]interface{}{"ok": false, "error": "empty key data in response"}
	}
	keyData, err := base64.StdEncoding.DecodeString(keyDataB64)
	if err != nil {
		a.log("ClawNet: online restore — invalid key encoding for %s: %v", email, err)
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
		a.log("ClawNet: online restore — mkdir failed: %v", err)
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	// Backup existing key
	if _, err := os.Stat(keyPath); err == nil {
		if renameErr := os.Rename(keyPath, keyPath+".bak"); renameErr != nil {
			a.log("ClawNet: online restore — backup rename failed: %v", renameErr)
		} else {
			a.log("ClawNet: existing identity key backed up to %s.bak", keyPath)
		}
	}
	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		a.log("ClawNet: online restore — write key failed: %v", err)
		return map[string]interface{}{"ok": false, "error": err.Error()}
	}
	a.log("ClawNet: identity key restored from Hub for %s", email)

	// Restart daemon with new identity
	restarted := false
	c := a.initClawNet()
	if err := c.EnsureDaemon(); err != nil {
		a.log("ClawNet: daemon restart after online restore failed: %v", err)
	} else {
		restarted = true
		a.log("ClawNet: daemon restarted with restored identity")
	}

	return map[string]interface{}{"ok": true, "path": keyPath, "restarted": restarted}
}
