package clawnet

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

// Client wraps the ClawNet daemon REST API (localhost:3998).
// It manages the daemon lifecycle and provides typed access to all endpoints.
type Client struct {
	mu      sync.Mutex
	baseURL string
	client  *http.Client
	daemon  *exec.Cmd
	binPath string
	running bool

	autoUpdateStop chan struct{}
}

// BinPath returns the resolved path to the clawnet binary.
func (c *Client) BinPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.binPath != "" {
		return c.binPath
	}
	return c.findBinary()
}

// --- API response types ---

type Status struct {
	PeerID   string `json:"peer_id"`
	Peers    int    `json:"peers"`
	UnreadDM int    `json:"unread_dm"`
	Version  string `json:"version"`
	Uptime   string `json:"uptime,omitempty"`
}

type Peer struct {
	PeerID  string `json:"peer_id"`
	Addr    string `json:"addr,omitempty"`
	Latency string `json:"latency,omitempty"`
	Country string `json:"country,omitempty"`
	City    string `json:"city,omitempty"`
}

type Task struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	TaskStatus  string         `json:"status"`
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
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*f = arr
		return nil
	}
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

type Credits struct {
	Balance      float64 `json:"balance"`
	Tier         string  `json:"tier"`
	TierRank     int     `json:"tier_rank,omitempty"`
	Energy       float64 `json:"energy,omitempty"`
	Currency     string  `json:"currency,omitempty"`
	ExchangeRate float64 `json:"exchange_rate,omitempty"`
	LocalValue   string  `json:"local_value,omitempty"`
}

type KnowledgeEntry struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Author    string   `json:"author,omitempty"`
	Domain    string   `json:"domain,omitempty"`
	Domains   []string `json:"domains,omitempty"`
	Upvotes   int      `json:"upvotes,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

type Prediction struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Status   string   `json:"status,omitempty"`
	Creator  string   `json:"creator,omitempty"`
}

type SwarmSession struct {
	ID       string `json:"id"`
	Topic    string `json:"topic"`
	Question string `json:"question,omitempty"`
	Status   string `json:"status,omitempty"`
	Members  int    `json:"members,omitempty"`
}

type DM struct {
	PeerID string `json:"peer_id"`
	Body   string `json:"body"`
	Unread int    `json:"unread,omitempty"`
	SentAt string `json:"sent_at,omitempty"`
}

type Resume struct {
	PeerID  string   `json:"peer_id,omitempty"`
	Name    string   `json:"name,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	Domains []string `json:"domains,omitempty"`
	Bio     string   `json:"bio,omitempty"`
}

type Profile struct {
	PeerID string `json:"peer_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Bio    string `json:"bio,omitempty"`
	Motto  string `json:"motto,omitempty"`
}

type Topic struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Members     int    `json:"members,omitempty"`
}

type TopicMessage struct {
	PeerID string `json:"peer_id,omitempty"`
	Body   string `json:"body"`
	SentAt string `json:"sent_at,omitempty"`
}

// NewClient creates a client pointing at the default daemon port.
func NewClient() *Client {
	return &Client{
		baseURL: "http://127.0.0.1:3998",
		client:  &http.Client{Timeout: 3 * time.Second},
	}
}

// ---------- PID lock file (duplicate-process guard) ----------

func pidFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "clawnet", "daemon.pid")
}

// writePIDFile writes the given PID to the lock file.
func writePIDFile(pid int) {
	p := pidFilePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	_ = os.WriteFile(p, []byte(fmt.Sprintf("%d", pid)), 0644)
}

// removePIDFile removes the PID lock file.
func removePIDFile() {
	if p := pidFilePath(); p != "" {
		_ = os.Remove(p)
	}
}

// readPIDFile returns the PID stored in the lock file, or 0 if absent/invalid.
func readPIDFile() int {
	p := pidFilePath()
	if p == "" {
		return 0
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil {
		return 0
	}
	return pid
}

// isDaemonAlive checks if a previous daemon is still running by looking for
// a process with the clawnet binary name, then confirming via HTTP ping.
// Falls back to PID file check for robustness.
func (c *Client) isDaemonAlive() bool {
	// Primary: detect by process name — immune to stale PID files.
	if pid := findProcessByName(LocalBinaryName()); pid != 0 {
		if c.ping() {
			return true
		}
		// Process exists but not responding — not considered alive.
		// Clean up stale PID file if present.
		removePIDFile()
		return false
	}
	// Fallback: PID file (covers edge cases where process name differs).
	pid := readPIDFile()
	if pid == 0 {
		return false
	}
	if !isProcessAlive(pid) {
		removePIDFile()
		return false
	}
	return c.ping()
}

// ---------- Daemon lifecycle ----------

func (c *Client) findBinary() string {
	binName := LocalBinaryName()
	candidates := []string{
		filepath.Join(".", "vendor", "clawnet", binName),
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".openclaw", "clawnet", binName))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("clawnet"); err == nil {
		return p
	}
	return ""
}

func (c *Client) EnsureDaemon() error {
	return c.EnsureDaemonWithProgress(nil)
}

func (c *Client) EnsureDaemonWithProgress(emitProgress func(stage string, pct int, msg string)) error {
	// Fast path: daemon already reachable.
	if c.ping() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

	// Check if another instance already has the daemon running.
	if c.isDaemonAlive() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

	// --- Cross-process file lock ---
	// Only one process at a time may attempt to start the daemon.
	// If another process holds the lock, wait for it to finish and then
	// just check if the daemon is now reachable.
	lockFile, lockErr := acquireDaemonLock()
	if lockErr != nil {
		// Another process is starting the daemon — wait for it.
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if c.ping() {
				c.mu.Lock()
				c.running = true
				c.mu.Unlock()
				return nil
			}
			time.Sleep(1 * time.Second)
		}
		return fmt.Errorf("another process is starting clawnet daemon but it did not become reachable")
	}
	defer lockFile.Close() // releases the file lock

	// Re-check after acquiring lock — the previous holder may have started it.
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
		c.mu.Unlock()
		downloaded, err := Download(emitProgress)
		if err != nil {
			return err
		}
		c.mu.Lock()
		if c.ping() {
			c.running = true
			c.mu.Unlock()
			return nil
		}
		bin = downloaded
	}
	c.binPath = bin
	c.mu.Unlock()

	// Guard: if a clawnet process is already running (by name), don't start another.
	if pid := findProcessByName(LocalBinaryName()); pid != 0 {
		// Another daemon process exists — try to reach it.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if c.ping() {
				c.mu.Lock()
				c.running = true
				c.mu.Unlock()
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		// Process exists but not responding — kill it before starting fresh.
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
		// Wait until the process is truly gone (up to 3s).
		for i := 0; i < 6; i++ {
			if findProcessByName(LocalBinaryName()) == 0 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	stopCmd := exec.Command(bin, "stop")
	hideCommandWindow(stopCmd)
	stopDone := make(chan error, 1)
	go func() { stopDone <- stopCmd.Run() }()
	select {
	case <-stopDone:
	case <-time.After(5 * time.Second):
		if stopCmd.Process != nil {
			_ = stopCmd.Process.Kill()
		}
	}
	removePIDFile()
	time.Sleep(1 * time.Second)

	// Final check after cleanup — another caller may have started it.
	if c.ping() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

	// Final guard: if a clawnet process already exists (e.g. another maclaw
	// instance just started one between our stop and now), wait for it instead
	// of spawning a duplicate.
	if pid := findProcessByName(LocalBinaryName()); pid != 0 {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if c.ping() {
				c.mu.Lock()
				c.running = true
				c.mu.Unlock()
				return nil
			}
			time.Sleep(500 * time.Millisecond)
		}
		// Process exists but never became reachable — kill it before starting fresh.
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
		removePIDFile()
		// Wait until the process is truly gone (up to 3s).
		for i := 0; i < 6; i++ {
			if findProcessByName(LocalBinaryName()) == 0 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
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
	// Write PID lock file immediately after start.
	if cmd.Process != nil {
		writePIDFile(cmd.Process.Pid)
	}
	c.mu.Unlock()

	go func() { _ = cmd.Wait() }()

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

func (c *Client) StopDaemon() {
	c.StopAutoUpdate()
	c.mu.Lock()
	bin := c.binPath
	if bin == "" {
		bin = c.findBinary()
	}
	daemon := c.daemon
	c.daemon = nil
	c.running = false
	c.mu.Unlock()

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
		_ = daemon.Process.Kill()
	}
	removePIDFile()
}

func (c *Client) IsRunning() bool {
	if c.ping() {
		return true
	}
	time.Sleep(300 * time.Millisecond)
	return c.ping()
}

func (c *Client) ping() bool {
	resp, err := c.client.Get(c.baseURL + "/api/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

func (c *Client) DaemonPID() int {
	c.mu.Lock()
	daemon := c.daemon
	c.mu.Unlock()
	if daemon != nil && daemon.Process != nil {
		return daemon.Process.Pid
	}
	return 0
}

// ---------- HTTP helpers ----------

func (c *Client) get(path string, out interface{}) error {
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

func (c *Client) post(path string, payload interface{}, out interface{}) error {
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

func (c *Client) delete(path string) error {
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

func (c *Client) put(path string, payload interface{}, out interface{}) error {
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

func (c *Client) GetStatus() (*Status, error) {
	var s Status
	return &s, c.get("/api/status", &s)
}

func (c *Client) GetPeers() ([]Peer, error) {
	var peers []Peer
	return peers, c.get("/api/peers", &peers)
}

// ---------- Task Bazaar ----------

func (c *Client) ListTasks(status string) ([]Task, error) {
	path := "/api/tasks"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var tasks []Task
	return tasks, c.get(path, &tasks)
}

func (c *Client) GetTaskBoard() (map[string]interface{}, error) {
	var board map[string]interface{}
	return board, c.get("/api/tasks/board", &board)
}

func (c *Client) CreateTask(title string, reward float64) (*Task, error) {
	return c.CreateTaskFull(title, "", reward, nil, "")
}

func (c *Client) CreateTaskFull(title, description string, reward float64, tags []string, targetPeer string) (*Task, error) {
	payload := map[string]interface{}{"title": title, "reward": reward}
	if description != "" {
		payload["description"] = description
	}
	if len(tags) > 0 {
		payload["tags"] = tags
	}
	if targetPeer != "" {
		payload["target_peer"] = targetPeer
	}
	var task Task
	return &task, c.post("/api/tasks", payload, &task)
}

func (c *Client) GetTask(id string) (*Task, error) {
	var task Task
	return &task, c.get("/api/tasks/"+id, &task)
}

func (c *Client) BidOnTask(id string, amount float64, message string) error {
	payload := map[string]interface{}{"message": message}
	if amount > 0 {
		payload["amount"] = amount
	}
	return c.post("/api/tasks/"+id+"/bid", payload, nil)
}

func (c *Client) AssignTask(id, peerID string) error {
	return c.post("/api/tasks/"+id+"/assign", map[string]interface{}{"bidder_id": peerID}, nil)
}

func (c *Client) ClaimTask(id string) error  { return c.post("/api/tasks/"+id+"/claim", nil, nil) }
func (c *Client) ApproveTask(id string) error { return c.post("/api/tasks/"+id+"/approve", nil, nil) }
func (c *Client) RejectTask(id string) error  { return c.post("/api/tasks/"+id+"/reject", nil, nil) }
func (c *Client) CancelTask(id string) error  { return c.post("/api/tasks/"+id+"/cancel", nil, nil) }

// ---------- Shell Economy ----------

func (c *Client) GetCredits() (*Credits, error) {
	var credits Credits
	return &credits, c.get("/api/credits/balance", &credits)
}

func (c *Client) GetCreditsHistory() ([]map[string]interface{}, error) {
	var history []map[string]interface{}
	return history, c.get("/api/credits/history", &history)
}

// ---------- Knowledge Mesh ----------

func (c *Client) GetKnowledgeFeed(domain string, limit int) ([]KnowledgeEntry, error) {
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
	var entries []KnowledgeEntry
	return entries, c.get(path, &entries)
}

func (c *Client) SearchKnowledge(query string) ([]KnowledgeEntry, error) {
	var entries []KnowledgeEntry
	return entries, c.get("/api/knowledge/search?q="+url.QueryEscape(query), &entries)
}

func (c *Client) PublishKnowledge(title, body string) (*KnowledgeEntry, error) {
	return c.PublishKnowledgeFull(title, body, nil)
}

func (c *Client) PublishKnowledgeFull(title, body string, domains []string) (*KnowledgeEntry, error) {
	payload := map[string]interface{}{"title": title, "body": body}
	if len(domains) > 0 {
		payload["domains"] = domains
	}
	var entry KnowledgeEntry
	return &entry, c.post("/api/knowledge", payload, &entry)
}

func (c *Client) ReactKnowledge(id, reaction string) error {
	return c.post("/api/knowledge/"+id+"/react", map[string]interface{}{"emoji": reaction}, nil)
}

func (c *Client) ReplyKnowledge(id, body string) error {
	return c.post("/api/knowledge/"+id+"/reply", map[string]interface{}{"body": body}, nil)
}

// ---------- Prediction Market ----------

func (c *Client) ListPredictions() ([]Prediction, error) {
	var preds []Prediction
	return preds, c.get("/api/predictions", &preds)
}

func (c *Client) CreatePrediction(question string, options []string) (*Prediction, error) {
	var pred Prediction
	return &pred, c.post("/api/predictions", map[string]interface{}{"question": question, "options": options}, &pred)
}

func (c *Client) PlaceBet(predID, option string, stake float64) error {
	return c.post("/api/predictions/"+predID+"/bet", map[string]interface{}{"option": option, "amount": stake}, nil)
}

func (c *Client) ResolvePrediction(predID, result string) error {
	return c.post("/api/predictions/"+predID+"/resolve", map[string]interface{}{"winning_option": result}, nil)
}

// ---------- Swarm Think ----------

func (c *Client) ListSwarmSessions() ([]SwarmSession, error) {
	var sessions []SwarmSession
	return sessions, c.get("/api/swarm", &sessions)
}

func (c *Client) CreateSwarmSession(topic, question string) (*SwarmSession, error) {
	var session SwarmSession
	return &session, c.post("/api/swarm", map[string]interface{}{"topic": topic, "description": question}, &session)
}

func (c *Client) JoinSwarm(sessionID string) error {
	return c.post("/api/swarm/"+sessionID+"/join", nil, nil)
}

func (c *Client) ContributeToSwarm(sessionID, message, stance string) error {
	payload := map[string]interface{}{"body": message}
	if stance != "" {
		payload["stance"] = stance
	}
	return c.post("/api/swarm/"+sessionID+"/contribute", payload, nil)
}

func (c *Client) SynthesizeSwarm(sessionID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	return result, c.post("/api/swarm/"+sessionID+"/synthesize", nil, &result)
}

// ---------- Direct Messages ----------

func (c *Client) SendDM(peerID, body string) error {
	return c.post("/api/dm/send", map[string]interface{}{"peer_id": peerID, "body": body}, nil)
}

func (c *Client) GetDMInbox() ([]DM, error) {
	var inbox []DM
	return inbox, c.get("/api/dm/inbox", &inbox)
}

func (c *Client) GetDMThread(peerID string, limit int) ([]DM, error) {
	path := "/api/dm/thread/" + url.PathEscape(peerID)
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var thread []DM
	return thread, c.get(path, &thread)
}

// ---------- Resume / Agent Profile ----------

func (c *Client) GetResume() (*Resume, error) {
	var r Resume
	return &r, c.get("/api/resume", &r)
}

func (c *Client) UpdateResume(resume *Resume) error {
	return c.put("/api/resume", resume, nil)
}

func (c *Client) MatchResume(taskID string) ([]Resume, error) {
	return c.MatchAgentsForTask(taskID)
}

// ---------- Profile ----------

func (c *Client) GetProfile() (*Profile, error) {
	var p Profile
	return &p, c.get("/api/profile", &p)
}

func (c *Client) UpdateProfile(name, bio string) error {
	return c.put("/api/profile", map[string]interface{}{"name": name, "bio": bio}, nil)
}

func (c *Client) SetMotto(motto string) error {
	return c.put("/api/motto", map[string]interface{}{"motto": motto}, nil)
}

// ---------- Topic Rooms ----------

func (c *Client) ListTopics() ([]Topic, error) {
	var topics []Topic
	return topics, c.get("/api/topics", &topics)
}

func (c *Client) CreateTopic(name, description string) error {
	return c.post("/api/topics", map[string]interface{}{"name": name, "description": description}, nil)
}

func (c *Client) GetTopicMessages(topicName string) ([]TopicMessage, error) {
	var msgs []TopicMessage
	return msgs, c.get("/api/topics/"+url.PathEscape(topicName)+"/messages", &msgs)
}

func (c *Client) PostTopicMessage(topicName, body string) error {
	return c.post("/api/topics/"+url.PathEscape(topicName)+"/messages", map[string]interface{}{"body": body}, nil)
}

// ---------- Task Bazaar (extended) ----------

func (c *Client) SubmitTaskResult(id, result string) error {
	return c.post("/api/tasks/"+id+"/submit", map[string]interface{}{"result": result}, nil)
}

func (c *Client) GetTaskBids(id string) ([]map[string]interface{}, error) {
	var bids []map[string]interface{}
	return bids, c.get("/api/tasks/"+id+"/bids", &bids)
}

func (c *Client) MatchTasks() ([]Task, error) {
	var tasks []Task
	return tasks, c.get("/api/match/tasks", &tasks)
}

func (c *Client) MatchAgentsForTask(taskID string) ([]Resume, error) {
	var agents []Resume
	return agents, c.get("/api/tasks/"+taskID+"/match", &agents)
}

// ---------- Credits (extended) ----------

func (c *Client) GetCreditsTransactions() ([]map[string]interface{}, error) {
	var txns []map[string]interface{}
	return txns, c.get("/api/credits/transactions", &txns)
}

func (c *Client) GetLeaderboard() ([]map[string]interface{}, error) {
	var lb []map[string]interface{}
	return lb, c.get("/api/leaderboard", &lb)
}

// ---------- Diagnostics ----------

func (c *Client) GetDiagnostics() (map[string]interface{}, error) {
	var diag map[string]interface{}
	return diag, c.get("/api/diagnostics", &diag)
}

func (c *Client) SelfUpdate() error {
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

const autoUpdateInterval = 24 * time.Hour

func lastUpdatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "clawnet", ".last_update")
}

func readLastUpdateTime() time.Time {
	p := lastUpdatePath()
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

func writeLastUpdateTime() {
	p := lastUpdatePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	_ = os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)), 0644)
}

func needsUpdate() bool {
	last := readLastUpdateTime()
	if last.IsZero() {
		return true
	}
	return time.Since(last) > autoUpdateInterval
}

func (c *Client) tryAutoUpdate(logFn func(string)) {
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
	if !c.ping() && logFn != nil {
		logFn("ClawNet: daemon unreachable after update — it may need a manual restart")
	}
}

func (c *Client) StartAutoUpdate(logFn func(string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.autoUpdateStop != nil {
		select {
		case <-c.autoUpdateStop:
		default:
			return
		}
	}
	c.autoUpdateStop = make(chan struct{})
	go c.autoUpdateLoop(logFn, c.autoUpdateStop)
}

func (c *Client) StopAutoUpdate() {
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

func (c *Client) autoUpdateLoop(logFn func(string), stop <-chan struct{}) {
	if needsUpdate() {
		c.tryAutoUpdate(logFn)
	}
	ticker := time.NewTicker(autoUpdateInterval)
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

func (c *Client) GetKnowledgeReplies(id string) ([]map[string]interface{}, error) {
	var replies []map[string]interface{}
	return replies, c.get("/api/knowledge/"+id+"/replies", &replies)
}

// ---------- Credits Audit ----------

func (c *Client) GetCreditsAudit() ([]map[string]interface{}, error) {
	var audit []map[string]interface{}
	return audit, c.get("/api/credits/audit", &audit)
}

// ---------- Prediction Market (extended) ----------

func (c *Client) AppealPrediction(predID, reason string) error {
	return c.post("/api/predictions/"+predID+"/appeal", map[string]interface{}{"reason": reason}, nil)
}

func (c *Client) GetPredictionLeaderboard() ([]map[string]interface{}, error) {
	var lb []map[string]interface{}
	return lb, c.get("/api/predictions/leaderboard", &lb)
}

// ---------- Auction House ----------

func (c *Client) SubmitTaskWork(id, result string) error {
	return c.post("/api/tasks/"+id+"/work", map[string]interface{}{"result": result}, nil)
}

func (c *Client) GetTaskSubmissions(id string) ([]map[string]interface{}, error) {
	var subs []map[string]interface{}
	return subs, c.get("/api/tasks/"+id+"/submissions", &subs)
}

func (c *Client) PickTaskWinner(id, winnerPeerID string) error {
	return c.post("/api/tasks/"+id+"/pick", map[string]interface{}{"winner": winnerPeerID}, nil)
}

// ---------- Overlay Mesh ----------

func (c *Client) GetOverlayStatus() (map[string]interface{}, error) {
	var status map[string]interface{}
	return status, c.get("/api/overlay/status", &status)
}

func (c *Client) GetOverlayTree() (map[string]interface{}, error) {
	var tree map[string]interface{}
	return tree, c.get("/api/overlay/tree", &tree)
}

func (c *Client) GetOverlayPeersGeo() ([]map[string]interface{}, error) {
	var peers []map[string]interface{}
	return peers, c.get("/api/overlay/peers/geo", &peers)
}

func (c *Client) AddOverlayPeer(uri string) error {
	return c.post("/api/overlay/peers/add", map[string]interface{}{"uri": uri}, nil)
}

// ---------- Extended Diagnostics ----------

func (c *Client) GetMatrixStatus() (map[string]interface{}, error) {
	var status map[string]interface{}
	return status, c.get("/api/matrix/status", &status)
}

func (c *Client) GetTraffic() (map[string]interface{}, error) {
	var traffic map[string]interface{}
	return traffic, c.get("/api/traffic", &traffic)
}

// ---------- Hub-relayed task discovery ----------

func (c *Client) PublishTasksToHub(hubURL string) error {
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
		if t.ID == "tutorial-onboarding" {
			continue
		}
		payload = append(payload, hubTask{
			ID:          t.ID,
			Title:       t.Title,
			Description: t.Description,
			Status:      t.TaskStatus,
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

func (c *Client) BrowseHubTasks(hubURL string) ([]Task, error) {
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

	status, _ := c.GetStatus()
	localPeerID := ""
	if status != nil {
		localPeerID = status.PeerID
	}

	var tasks []Task
	for _, t := range result.Tasks {
		if localPeerID != "" && t.PeerID == localPeerID {
			continue
		}
		tasks = append(tasks, Task{
			ID:          t.ID,
			Title:       t.Title,
			Description: t.Description,
			TaskStatus:  t.Status,
			Reward:      t.Reward,
			Creator:     t.Creator,
			Tags:        FlexStringList(t.Tags),
			CreatedAt:   t.CreatedAt,
		})
	}
	return tasks, nil
}
