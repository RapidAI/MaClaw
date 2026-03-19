package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ClawNetClient wraps the ClawNet daemon REST API (localhost:3998).
// It manages the daemon lifecycle and provides typed access to all endpoints.
// Upstream updates only require replacing the clawnet binary — no code changes.
type ClawNetClient struct {
	mu      sync.Mutex
	baseURL string
	client  *http.Client
	daemon  *exec.Cmd
	binPath string
	running bool

	autoUpdateStop chan struct{} // signals the auto-update goroutine to stop
}

// ClawNet API response types

type ClawNetStatus struct {
	PeerID   string `json:"peer_id"`
	Peers    int    `json:"peers"`
	UnreadDM int    `json:"unread_dm"`
	Version  string `json:"version"`
	Uptime   string `json:"uptime,omitempty"`
}

type ClawNetPeer struct {
	PeerID  string `json:"peer_id"`
	Addr    string `json:"addr,omitempty"`
	Latency string `json:"latency,omitempty"`
	Country string `json:"country,omitempty"`
	City    string `json:"city,omitempty"`
}

type ClawNetTask struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"` // open, assigned, submitted, approved, rejected, cancelled
	Reward      float64        `json:"reward"`
	Creator     string         `json:"creator,omitempty"`
	Assignee    string         `json:"assignee,omitempty"`
	TargetPeer  string         `json:"target_peer,omitempty"`
	Tags        FlexStringList `json:"tags,omitempty"`
	CreatedAt   string         `json:"created_at,omitempty"`
}

// FlexStringList can unmarshal from either a JSON array of strings or a single
// comma-separated string, so the client tolerates both server formats.
type FlexStringList []string

func (f *FlexStringList) UnmarshalJSON(data []byte) error {
	// Try array first.
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*f = arr
		return nil
	}
	// Fall back to a single string (possibly comma-separated).
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*f = nil
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	*f = parts
	return nil
}

type ClawNetCredits struct {
	Balance      float64 `json:"balance"`
	Tier         string  `json:"tier"`
	TierRank     int     `json:"tier_rank,omitempty"`
	Energy       float64 `json:"energy,omitempty"`
	Currency     string  `json:"currency,omitempty"`
	ExchangeRate float64 `json:"exchange_rate,omitempty"`
	LocalValue   string  `json:"local_value,omitempty"`
}

type ClawNetKnowledgeEntry struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Author    string   `json:"author,omitempty"`
	Domain    string   `json:"domain,omitempty"`
	Domains   []string `json:"domains,omitempty"`
	Upvotes   int      `json:"upvotes,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

type ClawNetPrediction struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Status   string   `json:"status,omitempty"`
	Creator  string   `json:"creator,omitempty"`
}

type ClawNetSwarmSession struct {
	ID       string `json:"id"`
	Topic    string `json:"topic"`
	Question string `json:"question,omitempty"`
	Status   string `json:"status,omitempty"`
	Members  int    `json:"members,omitempty"`
}

type ClawNetDM struct {
	PeerID  string `json:"peer_id"`
	Body    string `json:"body"`
	Unread  int    `json:"unread,omitempty"`
	SentAt  string `json:"sent_at,omitempty"`
}

type ClawNetResume struct {
	PeerID  string   `json:"peer_id,omitempty"`
	Name    string   `json:"name,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	Domains []string `json:"domains,omitempty"`
	Bio     string   `json:"bio,omitempty"`
}

// NewClawNetClient creates a client pointing at the default daemon port.
func NewClawNetClient() *ClawNetClient {
	return &ClawNetClient{
		baseURL: "http://127.0.0.1:3998",
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// ---------- Daemon lifecycle ----------

// findBinary locates the clawnet executable.
// Search order: project vendor dir → user home dir → PATH.
func (c *ClawNetClient) findBinary() string {
	binName := clawnetLocalBinaryName()
	// 1. Vendored binary alongside the app
	candidates := []string{
		filepath.Join(".", "vendor", "clawnet", binName),
	}
	// 2. User home install dir
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".openclaw", "clawnet", binName),
		)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// 3. PATH lookup
	if p, err := exec.LookPath("clawnet"); err == nil {
		return p
	}
	return ""
}

// EnsureDaemon starts the clawnet daemon if not already running.
// It first checks whether the daemon is reachable; if so, it skips launching.
func (c *ClawNetClient) EnsureDaemon() error {
	return c.EnsureDaemonWithProgress(nil)
}

// EnsureDaemonWithProgress starts the daemon, auto-downloading the binary if needed.
// The optional emitProgress callback reports download progress to the caller.
func (c *ClawNetClient) EnsureDaemonWithProgress(emitProgress func(stage string, pct int, msg string)) error {
	// Check reachability without holding the lock (ping does a network call).
	// If an existing daemon is already healthy, just reuse it — no new process.
	if c.ping() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

	c.mu.Lock()
	bin := c.binPath
	if bin == "" {
		bin = c.findBinary()
	}
	if bin == "" {
		// Release lock during potentially long download.
		c.mu.Unlock()
		downloaded, err := DownloadClawNet(emitProgress)
		if err != nil {
			return err // DownloadClawNet already provides a user-friendly message
		}
		c.mu.Lock()
		// Re-check: another goroutine may have started the daemon while we downloaded.
		if c.ping() {
			c.running = true
			c.mu.Unlock()
			return nil
		}
		bin = downloaded
	}
	c.binPath = bin

	// --- Cleanup stale/zombie daemon processes before starting a new one ---
	// If we reach here, ping failed — there may be orphaned clawnet processes
	// holding the port (e.g. maclaw was force-killed without clean shutdown).
	// Release the lock during cleanup to avoid blocking other goroutines.
	c.mu.Unlock()
	stopCmd := exec.Command(bin, "stop")
	hideCommandWindow(stopCmd)
	// Run with a timeout so a hung zombie can't block us forever.
	stopDone := make(chan error, 1)
	go func() { stopDone <- stopCmd.Run() }()
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		if stopCmd.Process != nil {
			_ = stopCmd.Process.Kill()
		}
	}
	// Give the OS a moment to release the port.
	time.Sleep(1 * time.Second)

	// One more ping — the stop may have cleaned up a half-alive daemon that
	// is now restarting itself, or another maclaw instance may have started one.
	if c.ping() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}
	c.mu.Lock()

	cmd := exec.Command(bin, "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	hideCommandWindow(cmd)
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to start clawnet daemon: %w", err)
	}
	c.daemon = cmd
	c.mu.Unlock()

	// Reap the child process in the background to avoid zombie processes.
	go func() { _ = cmd.Wait() }()

	// Wait for daemon to become ready (up to 15s) without holding the lock
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c.ping() {
			c.mu.Lock()
			c.running = true
			c.mu.Unlock()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("clawnet daemon started but not responding on %s", c.baseURL)
}

// StopDaemon gracefully stops the daemon via `clawnet stop`.
func (c *ClawNetClient) StopDaemon() {
	c.StopAutoUpdate()
	c.mu.Lock()

	// Prefer using the CLI to stop the daemon gracefully.
	bin := c.binPath
	if bin == "" {
		bin = c.findBinary()
	}
	daemon := c.daemon
	c.daemon = nil
	c.running = false
	c.mu.Unlock()

	// Run stop command outside the lock to avoid blocking other goroutines.
	if bin != "" {
		cmd := exec.Command(bin, "stop")
		hideCommandWindow(cmd)
		done := make(chan error, 1)
		go func() { done <- cmd.Run() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	} else if daemon != nil && daemon.Process != nil {
		// Fallback: kill the launcher process directly.
		_ = daemon.Process.Kill()
	}
}

// IsRunning returns true if the daemon is reachable.
// It retries once after a short pause to tolerate transient failures
// (e.g. after system wake from sleep).
func (c *ClawNetClient) IsRunning() bool {
	if c.ping() {
		return true
	}
	// One quick retry to avoid false negatives on transient hiccups.
	time.Sleep(300 * time.Millisecond)
	return c.ping()
}

func (c *ClawNetClient) ping() bool {
	resp, err := c.client.Get(c.baseURL + "/api/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// DaemonPID returns the PID of the daemon process we launched, or 0 if unknown.
func (c *ClawNetClient) DaemonPID() int {
	c.mu.Lock()
	daemon := c.daemon
	c.mu.Unlock()
	if daemon != nil && daemon.Process != nil {
		return daemon.Process.Pid
	}
	return 0
}

// ---------- HTTP helpers ----------

func (c *ClawNetClient) get(path string, out interface{}) error {
	resp, err := c.client.Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("clawnet GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clawnet GET %s: status %d: %s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *ClawNetClient) post(path string, payload interface{}, out interface{}) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	resp, err := c.client.Post(c.baseURL+path, "application/json", body)
	if err != nil {
		return fmt.Errorf("clawnet POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clawnet POST %s: status %d: %s", path, resp.StatusCode, string(b))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *ClawNetClient) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("clawnet DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clawnet DELETE %s: status %d: %s", path, resp.StatusCode, string(b))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *ClawNetClient) put(path string, payload interface{}, out interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("clawnet PUT %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clawnet PUT %s: status %d: %s", path, resp.StatusCode, string(b))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ---------- Status & Peers ----------

func (c *ClawNetClient) GetStatus() (*ClawNetStatus, error) {
	var s ClawNetStatus
	return &s, c.get("/api/status", &s)
}

func (c *ClawNetClient) GetPeers() ([]ClawNetPeer, error) {
	var peers []ClawNetPeer
	return peers, c.get("/api/peers", &peers)
}

// ---------- Task Bazaar ----------

func (c *ClawNetClient) ListTasks(status string) ([]ClawNetTask, error) {
	path := "/api/tasks"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var tasks []ClawNetTask
	return tasks, c.get(path, &tasks)
}

func (c *ClawNetClient) GetTaskBoard() (map[string]interface{}, error) {
	var board map[string]interface{}
	return board, c.get("/api/tasks/board", &board)
}

func (c *ClawNetClient) CreateTask(title string, reward float64) (*ClawNetTask, error) {
	return c.CreateTaskFull(title, "", reward, nil, "")
}

// CreateTaskFull creates a task with all optional fields: description, tags, target_peer.
func (c *ClawNetClient) CreateTaskFull(title, description string, reward float64, tags []string, targetPeer string) (*ClawNetTask, error) {
	payload := map[string]interface{}{
		"title":  title,
		"reward": reward,
	}
	if description != "" {
		payload["description"] = description
	}
	if len(tags) > 0 {
		payload["tags"] = tags
	}
	if targetPeer != "" {
		payload["target_peer"] = targetPeer
	}
	var task ClawNetTask
	return &task, c.post("/api/tasks", payload, &task)
}

func (c *ClawNetClient) GetTask(id string) (*ClawNetTask, error) {
	var task ClawNetTask
	return &task, c.get("/api/tasks/"+id, &task)
}

func (c *ClawNetClient) BidOnTask(id string, amount float64, message string) error {
	payload := map[string]interface{}{
		"message": message,
	}
	if amount > 0 {
		payload["amount"] = amount
	}
	return c.post("/api/tasks/"+id+"/bid", payload, nil)
}

func (c *ClawNetClient) AssignTask(id, peerID string) error {
	return c.post("/api/tasks/"+id+"/assign", map[string]interface{}{
		"bidder_id": peerID,
	}, nil)
}

func (c *ClawNetClient) ClaimTask(id string) error {
	return c.post("/api/tasks/"+id+"/claim", nil, nil)
}

func (c *ClawNetClient) ApproveTask(id string) error {
	return c.post("/api/tasks/"+id+"/approve", nil, nil)
}

func (c *ClawNetClient) RejectTask(id string) error {
	return c.post("/api/tasks/"+id+"/reject", nil, nil)
}

func (c *ClawNetClient) CancelTask(id string) error {
	return c.post("/api/tasks/"+id+"/cancel", nil, nil)
}

// ---------- Shell Economy ----------

func (c *ClawNetClient) GetCredits() (*ClawNetCredits, error) {
	var credits ClawNetCredits
	return &credits, c.get("/api/credits/balance", &credits)
}

func (c *ClawNetClient) GetCreditsHistory() ([]map[string]interface{}, error) {
	var history []map[string]interface{}
	return history, c.get("/api/credits/history", &history)
}

// ---------- Knowledge Mesh ----------

func (c *ClawNetClient) GetKnowledgeFeed(domain string, limit int) ([]ClawNetKnowledgeEntry, error) {
	params := make(url.Values)
	if domain != "" {
		params.Set("domain", domain)
	}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/api/knowledge/feed"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var entries []ClawNetKnowledgeEntry
	return entries, c.get(path, &entries)
}

func (c *ClawNetClient) SearchKnowledge(query string) ([]ClawNetKnowledgeEntry, error) {
	var entries []ClawNetKnowledgeEntry
	return entries, c.get("/api/knowledge/search?q="+url.QueryEscape(query), &entries)
}

func (c *ClawNetClient) PublishKnowledge(title, body string) (*ClawNetKnowledgeEntry, error) {
	return c.PublishKnowledgeFull(title, body, nil)
}

// PublishKnowledgeFull publishes knowledge with optional domain tags.
func (c *ClawNetClient) PublishKnowledgeFull(title, body string, domains []string) (*ClawNetKnowledgeEntry, error) {
	payload := map[string]interface{}{
		"title": title,
		"body":  body,
	}
	if len(domains) > 0 {
		payload["domains"] = domains
	}
	var entry ClawNetKnowledgeEntry
	return &entry, c.post("/api/knowledge", payload, &entry)
}

func (c *ClawNetClient) ReactKnowledge(id, reaction string) error {
	return c.post("/api/knowledge/"+id+"/react", map[string]interface{}{
		"emoji": reaction,
	}, nil)
}

func (c *ClawNetClient) ReplyKnowledge(id, body string) error {
	return c.post("/api/knowledge/"+id+"/reply", map[string]interface{}{
		"body": body,
	}, nil)
}

// ---------- Prediction Market ----------

func (c *ClawNetClient) ListPredictions() ([]ClawNetPrediction, error) {
	var preds []ClawNetPrediction
	return preds, c.get("/api/predictions", &preds)
}

func (c *ClawNetClient) CreatePrediction(question string, options []string) (*ClawNetPrediction, error) {
	var pred ClawNetPrediction
	return &pred, c.post("/api/predictions", map[string]interface{}{
		"question": question,
		"options":  options,
	}, &pred)
}

func (c *ClawNetClient) PlaceBet(predID, option string, stake float64) error {
	return c.post("/api/predictions/"+predID+"/bet", map[string]interface{}{
		"option": option,
		"amount": stake,
	}, nil)
}

func (c *ClawNetClient) ResolvePrediction(predID, result string) error {
	return c.post("/api/predictions/"+predID+"/resolve", map[string]interface{}{
		"winning_option": result,
	}, nil)
}

// ---------- Swarm Think ----------

func (c *ClawNetClient) ListSwarmSessions() ([]ClawNetSwarmSession, error) {
	var sessions []ClawNetSwarmSession
	return sessions, c.get("/api/swarm", &sessions)
}

func (c *ClawNetClient) CreateSwarmSession(topic, question string) (*ClawNetSwarmSession, error) {
	var session ClawNetSwarmSession
	return &session, c.post("/api/swarm", map[string]interface{}{
		"topic":       topic,
		"description": question,
	}, &session)
}

func (c *ClawNetClient) JoinSwarm(sessionID string) error {
	return c.post("/api/swarm/"+sessionID+"/join", nil, nil)
}

func (c *ClawNetClient) ContributeToSwarm(sessionID, message, stance string) error {
	payload := map[string]interface{}{
		"body": message,
	}
	if stance != "" {
		payload["stance"] = stance
	}
	return c.post("/api/swarm/"+sessionID+"/contribute", payload, nil)
}

func (c *ClawNetClient) SynthesizeSwarm(sessionID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	return result, c.post("/api/swarm/"+sessionID+"/synthesize", nil, &result)
}

// ---------- Direct Messages ----------

func (c *ClawNetClient) SendDM(peerID, body string) error {
	return c.post("/api/dm/send", map[string]interface{}{
		"peer_id": peerID,
		"body":    body,
	}, nil)
}

func (c *ClawNetClient) GetDMInbox() ([]ClawNetDM, error) {
	var inbox []ClawNetDM
	return inbox, c.get("/api/dm/inbox", &inbox)
}

func (c *ClawNetClient) GetDMThread(peerID string, limit int) ([]ClawNetDM, error) {
	path := "/api/dm/thread/" + url.PathEscape(peerID)
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var thread []ClawNetDM
	return thread, c.get(path, &thread)
}

// ---------- Resume / Agent Profile ----------

func (c *ClawNetClient) GetResume() (*ClawNetResume, error) {
	var r ClawNetResume
	return &r, c.get("/api/resume", &r)
}

func (c *ClawNetClient) UpdateResume(resume *ClawNetResume) error {
	return c.put("/api/resume", resume, nil)
}

// MatchResume finds agents matching a task. Delegates to MatchAgentsForTask.
func (c *ClawNetClient) MatchResume(taskID string) ([]ClawNetResume, error) {
	return c.MatchAgentsForTask(taskID)
}

// ---------- Profile ----------

type ClawNetProfile struct {
	PeerID string `json:"peer_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Bio    string `json:"bio,omitempty"`
	Motto  string `json:"motto,omitempty"`
}

func (c *ClawNetClient) GetProfile() (*ClawNetProfile, error) {
	var p ClawNetProfile
	return &p, c.get("/api/profile", &p)
}

func (c *ClawNetClient) UpdateProfile(name, bio string) error {
	return c.put("/api/profile", map[string]interface{}{"name": name, "bio": bio}, nil)
}

func (c *ClawNetClient) SetMotto(motto string) error {
	return c.put("/api/motto", map[string]interface{}{"motto": motto}, nil)
}

// ---------- Topic Rooms ----------

type ClawNetTopic struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Members     int    `json:"members,omitempty"`
}

type ClawNetTopicMessage struct {
	PeerID string `json:"peer_id,omitempty"`
	Body   string `json:"body"`
	SentAt string `json:"sent_at,omitempty"`
}

func (c *ClawNetClient) ListTopics() ([]ClawNetTopic, error) {
	var topics []ClawNetTopic
	return topics, c.get("/api/topics", &topics)
}

func (c *ClawNetClient) CreateTopic(name, description string) error {
	return c.post("/api/topics", map[string]interface{}{
		"name": name, "description": description,
	}, nil)
}

func (c *ClawNetClient) GetTopicMessages(topicName string) ([]ClawNetTopicMessage, error) {
	var msgs []ClawNetTopicMessage
	return msgs, c.get("/api/topics/"+url.PathEscape(topicName)+"/messages", &msgs)
}

func (c *ClawNetClient) PostTopicMessage(topicName, body string) error {
	return c.post("/api/topics/"+url.PathEscape(topicName)+"/messages", map[string]interface{}{
		"body": body,
	}, nil)
}

// ---------- Task Bazaar (extended) ----------

func (c *ClawNetClient) SubmitTaskResult(id, result string) error {
	return c.post("/api/tasks/"+id+"/submit", map[string]interface{}{
		"result": result,
	}, nil)
}

func (c *ClawNetClient) GetTaskBids(id string) ([]map[string]interface{}, error) {
	var bids []map[string]interface{}
	return bids, c.get("/api/tasks/"+id+"/bids", &bids)
}

func (c *ClawNetClient) MatchTasks() ([]ClawNetTask, error) {
	var tasks []ClawNetTask
	return tasks, c.get("/api/match/tasks", &tasks)
}

func (c *ClawNetClient) MatchAgentsForTask(taskID string) ([]ClawNetResume, error) {
	var agents []ClawNetResume
	return agents, c.get("/api/tasks/"+taskID+"/match", &agents)
}

// ---------- Credits (extended) ----------

func (c *ClawNetClient) GetCreditsTransactions() ([]map[string]interface{}, error) {
	var txns []map[string]interface{}
	return txns, c.get("/api/credits/transactions", &txns)
}

func (c *ClawNetClient) GetLeaderboard() ([]map[string]interface{}, error) {
	var lb []map[string]interface{}
	return lb, c.get("/api/leaderboard", &lb)
}

// ---------- Diagnostics ----------

func (c *ClawNetClient) GetDiagnostics() (map[string]interface{}, error) {
	var diag map[string]interface{}
	return diag, c.get("/api/diagnostics", &diag)
}

func (c *ClawNetClient) SelfUpdate() error {
	bin := c.binPath
	if bin == "" {
		bin = c.findBinary()
	}
	if bin == "" {
		return fmt.Errorf("clawnet binary not found")
	}
	cmd := exec.Command(bin, "update")
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("update failed: %w — %s", err, string(out))
	}
	return nil
}

// ---------- Auto-Update ----------

const clawnetAutoUpdateInterval = 24 * time.Hour

// clawnetLastUpdatePath returns the path to the timestamp file.
func clawnetLastUpdatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "clawnet", ".last_update")
}

// readLastUpdateTime reads the last successful update timestamp.
func readLastUpdateTime() time.Time {
	p := clawnetLastUpdatePath()
	if p == "" {
		return time.Time{}
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}
	}
	return t
}

// writeLastUpdateTime persists the current time as the last update timestamp.
func writeLastUpdateTime() {
	p := clawnetLastUpdatePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	_ = os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)), 0644)
}

// needsUpdate returns true if more than 24 hours have passed since the last update.
func needsUpdate() bool {
	last := readLastUpdateTime()
	if last.IsZero() {
		return true
	}
	return time.Since(last) > clawnetAutoUpdateInterval
}

// tryAutoUpdate runs SelfUpdate and records the timestamp on success.
// Errors are logged but never propagated.
func (c *ClawNetClient) tryAutoUpdate(logFn func(string)) {
	if logFn != nil {
		logFn("ClawNet: auto-update check started")
	}
	if err := c.SelfUpdate(); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("ClawNet: auto-update failed (non-fatal): %v", err))
		}
		return
	}
	writeLastUpdateTime()
	if logFn != nil {
		logFn("ClawNet: auto-update completed successfully")
	}
	// SelfUpdate may replace the binary; verify daemon is still alive.
	if !c.ping() && logFn != nil {
		logFn("ClawNet: daemon unreachable after update — it may need a manual restart")
	}
}

// StartAutoUpdate launches a background goroutine that:
//  1. Checks on startup if >24h since last update and runs immediately if so.
//  2. Then ticks every 24h to run SelfUpdate.
//
// Idempotent while running. After StopAutoUpdate/StopDaemon it can be
// started again (e.g. daemon restart).
func (c *ClawNetClient) StartAutoUpdate(logFn func(string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.autoUpdateStop != nil {
		// Check if previous goroutine already exited.
		select {
		case <-c.autoUpdateStop:
			// Closed — allow restart below.
		default:
			return // still running
		}
	}
	c.autoUpdateStop = make(chan struct{})
	go c.autoUpdateLoop(logFn, c.autoUpdateStop)
}

// StopAutoUpdate cancels the background auto-update goroutine.
func (c *ClawNetClient) StopAutoUpdate() {
	c.mu.Lock()
	ch := c.autoUpdateStop
	c.autoUpdateStop = nil
	c.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
}

func (c *ClawNetClient) autoUpdateLoop(logFn func(string), stop <-chan struct{}) {
	// Immediate check on startup.
	if needsUpdate() {
		c.tryAutoUpdate(logFn)
	}

	ticker := time.NewTicker(clawnetAutoUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if needsUpdate() {
				c.tryAutoUpdate(logFn)
			}
		case <-stop:
			return
		}
	}
}

// ---------- Knowledge Replies ----------

// GetKnowledgeReplies returns replies for a knowledge entry.
func (c *ClawNetClient) GetKnowledgeReplies(id string) ([]map[string]interface{}, error) {
	var replies []map[string]interface{}
	return replies, c.get("/api/knowledge/"+id+"/replies", &replies)
}

// ---------- Credits Audit ----------

// GetCreditsAudit returns the credit audit log.
func (c *ClawNetClient) GetCreditsAudit() ([]map[string]interface{}, error) {
	var audit []map[string]interface{}
	return audit, c.get("/api/credits/audit", &audit)
}

// ---------- Prediction Market (extended) ----------

// AppealPrediction files an appeal against a prediction resolution.
func (c *ClawNetClient) AppealPrediction(predID, reason string) error {
	return c.post("/api/predictions/"+predID+"/appeal", map[string]interface{}{
		"reason": reason,
	}, nil)
}

// GetPredictionLeaderboard returns the prediction market leaderboard.
func (c *ClawNetClient) GetPredictionLeaderboard() ([]map[string]interface{}, error) {
	var lb []map[string]interface{}
	return lb, c.get("/api/predictions/leaderboard", &lb)
}

// ---------- Auction House ----------

// SubmitTaskWork submits work for an auction-style task (multi-worker).
func (c *ClawNetClient) SubmitTaskWork(id, result string) error {
	return c.post("/api/tasks/"+id+"/work", map[string]interface{}{
		"result": result,
	}, nil)
}

// GetTaskSubmissions returns all submissions for an auction-style task.
func (c *ClawNetClient) GetTaskSubmissions(id string) ([]map[string]interface{}, error) {
	var subs []map[string]interface{}
	return subs, c.get("/api/tasks/"+id+"/submissions", &subs)
}

// PickTaskWinner selects the winning submission for an auction-style task.
func (c *ClawNetClient) PickTaskWinner(id, winnerPeerID string) error {
	return c.post("/api/tasks/"+id+"/pick", map[string]interface{}{
		"winner": winnerPeerID,
	}, nil)
}

// ---------- Overlay Mesh ----------

// GetOverlayStatus returns the overlay mesh network status.
func (c *ClawNetClient) GetOverlayStatus() (map[string]interface{}, error) {
	var status map[string]interface{}
	return status, c.get("/api/overlay/status", &status)
}

// GetOverlayTree returns the overlay peer tree.
func (c *ClawNetClient) GetOverlayTree() (map[string]interface{}, error) {
	var tree map[string]interface{}
	return tree, c.get("/api/overlay/tree", &tree)
}

// GetOverlayPeersGeo returns overlay peers with geographic info.
func (c *ClawNetClient) GetOverlayPeersGeo() ([]map[string]interface{}, error) {
	var peers []map[string]interface{}
	return peers, c.get("/api/overlay/peers/geo", &peers)
}

// AddOverlayPeer adds a custom overlay peer by URI.
func (c *ClawNetClient) AddOverlayPeer(uri string) error {
	return c.post("/api/overlay/peers/add", map[string]interface{}{
		"uri": uri,
	}, nil)
}

// ---------- Extended Diagnostics ----------

// GetMatrixStatus returns the matrix status diagnostics.
func (c *ClawNetClient) GetMatrixStatus() (map[string]interface{}, error) {
	var status map[string]interface{}
	return status, c.get("/api/matrix/status", &status)
}

// GetTraffic returns network traffic statistics.
func (c *ClawNetClient) GetTraffic() (map[string]interface{}, error) {
	var traffic map[string]interface{}
	return traffic, c.get("/api/traffic", &traffic)
}

// ---------------------------------------------------------------------------
// Hub-relayed task discovery
// ---------------------------------------------------------------------------

// PublishTasksToHub pushes local open tasks to the Hub task bulletin board
// so other peers can discover them.
func (c *ClawNetClient) PublishTasksToHub(hubURL string) error {
	if hubURL == "" {
		return fmt.Errorf("hub URL is empty")
	}
	tasks, err := c.ListTasks("open")
	if err != nil {
		return fmt.Errorf("list local tasks: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}
	status, _ := c.GetStatus()
	peerID := ""
	if status != nil {
		peerID = status.PeerID
	}

	type hubTask struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		Description string   `json:"description,omitempty"`
		Status      string   `json:"status"`
		Reward      float64  `json:"reward"`
		Creator     string   `json:"creator,omitempty"`
		PeerID      string   `json:"peer_id,omitempty"`
		Tags        []string `json:"tags,omitempty"`
		CreatedAt   string   `json:"created_at,omitempty"`
	}

	payload := make([]hubTask, 0, len(tasks))
	for _, t := range tasks {
		// Skip the local tutorial task — it's not interesting to others.
		if t.ID == "tutorial-onboarding" {
			continue
		}
		payload = append(payload, hubTask{
			ID:          t.ID,
			Title:       t.Title,
			Description: t.Description,
			Status:      t.Status,
			Reward:      t.Reward,
			Creator:     t.Creator,
			PeerID:      peerID,
			Tags:        []string(t.Tags),
			CreatedAt:   t.CreatedAt,
		})
	}
	if len(payload) == 0 {
		return nil
	}

	body, _ := json.Marshal(payload)
	endpoint := strings.TrimRight(hubURL, "/") + "/api/clawnet/tasks/publish"
	hubClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := hubClient.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("publish to hub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hub returned %d", resp.StatusCode)
	}
	return nil
}

// BrowseHubTasks fetches tasks from the Hub bulletin board (tasks published
// by other peers). Returns tasks that are NOT from the local peer.
func (c *ClawNetClient) BrowseHubTasks(hubURL string) ([]ClawNetTask, error) {
	if hubURL == "" {
		return nil, fmt.Errorf("hub URL is empty")
	}
	endpoint := strings.TrimRight(hubURL, "/") + "/api/clawnet/tasks/browse?limit=50"
	hubClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := hubClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("browse hub tasks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub returned %d", resp.StatusCode)
	}

	var result struct {
		OK    bool `json:"ok"`
		Tasks []struct {
			ID          string   `json:"id"`
			Title       string   `json:"title"`
			Description string   `json:"description,omitempty"`
			Status      string   `json:"status"`
			Reward      float64  `json:"reward"`
			Creator     string   `json:"creator,omitempty"`
			PeerID      string   `json:"peer_id,omitempty"`
			Tags        []string `json:"tags,omitempty"`
			CreatedAt   string   `json:"created_at,omitempty"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode hub response: %w", err)
	}

	// Get local peer ID to filter out own tasks.
	status, _ := c.GetStatus()
	localPeerID := ""
	if status != nil {
		localPeerID = status.PeerID
	}

	var tasks []ClawNetTask
	for _, t := range result.Tasks {
		if localPeerID != "" && t.PeerID == localPeerID {
			continue // skip own tasks
		}
		tasks = append(tasks, ClawNetTask{
			ID:          t.ID,
			Title:       t.Title,
			Description: t.Description,
			Status:      t.Status,
			Reward:      t.Reward,
			Creator:     t.Creator,
			Tags:        FlexStringList(t.Tags),
			CreatedAt:   t.CreatedAt,
		})
	}
	return tasks, nil
}
