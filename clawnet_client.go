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
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description,omitempty"`
	Status      string  `json:"status"` // open, assigned, submitted, approved, rejected, cancelled
	Reward      float64 `json:"reward"`
	Creator     string  `json:"creator,omitempty"`
	Assignee    string  `json:"assignee,omitempty"`
	CreatedAt   string  `json:"created_at,omitempty"`
}

type ClawNetCredits struct {
	Balance  float64 `json:"balance"`
	Tier     string  `json:"tier"`
	TierRank int     `json:"tier_rank,omitempty"`
	Energy   float64 `json:"energy,omitempty"`
}

type ClawNetKnowledgeEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Author    string `json:"author,omitempty"`
	Domain    string `json:"domain,omitempty"`
	Upvotes   int    `json:"upvotes,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
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
	PeerID string   `json:"peer_id,omitempty"`
	Name   string   `json:"name,omitempty"`
	Skills []string `json:"skills,omitempty"`
	Bio    string   `json:"bio,omitempty"`
}

// NewClawNetClient creates a client pointing at the default daemon port.
func NewClawNetClient() *ClawNetClient {
	return &ClawNetClient{
		baseURL: "http://127.0.0.1:3998",
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ---------- Daemon lifecycle ----------

// findBinary locates the clawnet executable.
// Search order: project vendor dir → PATH → user home.
func (c *ClawNetClient) findBinary() string {
	// 1. Vendored binary alongside the app
	candidates := []string{
		filepath.Join(".", "vendor", "clawnet", "clawnet.exe"),
		filepath.Join(".", "vendor", "clawnet", "clawnet"),
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".openclaw", "clawnet", "clawnet.exe"),
			filepath.Join(home, ".openclaw", "clawnet", "clawnet"),
		)
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// 2. PATH lookup
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
	c.mu.Lock()

	// Already reachable?
	if c.ping() {
		c.running = true
		c.mu.Unlock()
		return nil
	}

	bin := c.binPath
	if bin == "" {
		bin = c.findBinary()
	}
	if bin == "" {
		// Release lock during potentially long download
		c.mu.Unlock()
		downloaded, err := DownloadClawNet(emitProgress)
		if err != nil {
			return fmt.Errorf("clawnet binary not found and auto-download failed: %w", err)
		}
		c.mu.Lock()
		bin = downloaded
	}
	c.binPath = bin

	cmd := exec.Command(bin, "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to start clawnet daemon: %w", err)
	}
	c.daemon = cmd
	c.mu.Unlock()

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

// StopDaemon gracefully stops the daemon.
func (c *ClawNetClient) StopDaemon() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.daemon != nil && c.daemon.Process != nil {
		_ = c.daemon.Process.Kill()
		_ = c.daemon.Wait()
		c.daemon = nil
	}
	c.running = false
}

// IsRunning returns true if the daemon is reachable.
func (c *ClawNetClient) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
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
	var task ClawNetTask
	return &task, c.post("/api/tasks", map[string]interface{}{
		"title":  title,
		"reward": reward,
	}, &task)
}

func (c *ClawNetClient) GetTask(id string) (*ClawNetTask, error) {
	var task ClawNetTask
	return &task, c.get("/api/tasks/"+id, &task)
}

func (c *ClawNetClient) BidOnTask(id string, amount float64, message string) error {
	return c.post("/api/tasks/"+id+"/bid", map[string]interface{}{
		"amount":  amount,
		"message": message,
	}, nil)
}

func (c *ClawNetClient) AssignTask(id, peerID string) error {
	return c.post("/api/tasks/"+id+"/assign", map[string]interface{}{
		"peer_id": peerID,
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
	var entry ClawNetKnowledgeEntry
	return &entry, c.post("/api/knowledge", map[string]interface{}{
		"title": title,
		"body":  body,
	}, &entry)
}

func (c *ClawNetClient) ReactKnowledge(id, reaction string) error {
	return c.post("/api/knowledge/"+id+"/react", map[string]interface{}{
		"reaction": reaction,
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
		"stake":  stake,
	}, nil)
}

func (c *ClawNetClient) ResolvePrediction(predID, result string) error {
	return c.post("/api/predictions/"+predID+"/resolve", map[string]interface{}{
		"result": result,
	}, nil)
}

// ---------- Swarm Think ----------

func (c *ClawNetClient) ListSwarmSessions() ([]ClawNetSwarmSession, error) {
	var sessions []ClawNetSwarmSession
	return sessions, c.get("/api/swarm/sessions", &sessions)
}

func (c *ClawNetClient) CreateSwarmSession(topic, question string) (*ClawNetSwarmSession, error) {
	var session ClawNetSwarmSession
	return &session, c.post("/api/swarm/sessions", map[string]interface{}{
		"topic":    topic,
		"question": question,
	}, &session)
}

func (c *ClawNetClient) JoinSwarm(sessionID string) error {
	return c.post("/api/swarm/sessions/"+sessionID+"/join", nil, nil)
}

func (c *ClawNetClient) ContributeToSwarm(sessionID, message, stance string) error {
	return c.post("/api/swarm/sessions/"+sessionID+"/contribute", map[string]interface{}{
		"message": message,
		"stance":  stance,
	}, nil)
}

func (c *ClawNetClient) SynthesizeSwarm(sessionID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	return result, c.post("/api/swarm/sessions/"+sessionID+"/synthesize", nil, &result)
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
	path := fmt.Sprintf("/api/dm/thread/%s?limit=%d", url.PathEscape(peerID), limit)
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

func (c *ClawNetClient) MatchResume(taskID string) ([]ClawNetResume, error) {
	var matches []ClawNetResume
	return matches, c.get("/api/resume/match?task_id="+url.QueryEscape(taskID), &matches)
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("update failed: %w — %s", err, string(out))
	}
	return nil
}
