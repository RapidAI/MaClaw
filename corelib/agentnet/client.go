package agentnet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Client wraps the AgentNet daemon REST API (localhost:3998).
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

// BinPath returns the resolved path to the anet binary.
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
	DID      string `json:"did,omitempty"`
	Peers    int    `json:"peers"`
	UnreadDM int    `json:"unread_dm"`
	Version  string `json:"version"`
	Uptime   string `json:"uptime,omitempty"`
}

type Peer struct {
	PeerID  string   `json:"peer_id"`
	Addrs   []string `json:"addrs,omitempty"`
	Addr    string   `json:"addr,omitempty"`
	Latency string   `json:"latency,omitempty"`
	Country string   `json:"country,omitempty"`
	City    string   `json:"city,omitempty"`
}

type Task struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	TaskStatus  string         `json:"state"`
	Reward      float64        `json:"reward"`
	Creator     string         `json:"publisher,omitempty"`
	Assignee    string         `json:"claimant,omitempty"`
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
	Balance        float64 `json:"shell_balance"`
	LifetimeEarned float64 `json:"lifetime_earned,omitempty"`
	LifetimeSpent  float64 `json:"lifetime_spent,omitempty"`
	LockedShell    float64 `json:"locked_shell,omitempty"`
	DID            string  `json:"did,omitempty"`
	Tier           string  `json:"tier,omitempty"`
	TierRank       int     `json:"tier_rank,omitempty"`
	Energy         float64 `json:"energy,omitempty"`
	Currency       string  `json:"currency,omitempty"`
	ExchangeRate   float64 `json:"exchange_rate,omitempty"`
	LocalValue     string  `json:"local_value,omitempty"`
}

type KnowledgeEntry struct {
	ID        string   `json:"id"`
	Title     string   `json:"title,omitempty"`
	Intent    string   `json:"intent,omitempty"`
	Body      string   `json:"body,omitempty"`
	Author    string   `json:"author,omitempty"`
	PeerID    string   `json:"peer_id,omitempty"`
	SourceID  string   `json:"source_id,omitempty"`
	Domain    string   `json:"domain,omitempty"`
	Domains   []string `json:"domains,omitempty"`
	Skills    string   `json:"skills,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	NodeType  string   `json:"node_type,omitempty"`
	GraphID   string   `json:"graph_id,omitempty"`
	Upvotes   int      `json:"upvotes,omitempty"`
	NodeCount int      `json:"node_count,omitempty"`
	EdgeCount int      `json:"edge_count,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	Metadata  string   `json:"metadata,omitempty"`
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
	PeerID    string `json:"peer_id,omitempty"`
	From      string `json:"from,omitempty"`
	Body      string `json:"body"`
	Unread    int    `json:"unread,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	SentAt    string `json:"sent_at,omitempty"`
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
	Access      string `json:"access,omitempty"`
	Members     int    `json:"members,omitempty"`
	Messages    int    `json:"messages,omitempty"`
	JoinedAt    string `json:"joined_at,omitempty"`
}

type TopicMessage struct {
	ID        string `json:"id,omitempty"`
	From      string `json:"from,omitempty"`
	Body      string `json:"body"`
	Timestamp string `json:"timestamp,omitempty"`
}

// Service represents a P2P service registered on the network.
type Service struct {
	Name        string   `json:"name"`
	URL         string   `json:"url,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Modes       []string `json:"modes,omitempty"`
	Billing     string   `json:"billing,omitempty"`
	Price       float64  `json:"price,omitempty"`
	FreeTier    int      `json:"free_tier,omitempty"`
}

// ANSEntry represents an Agent Name Service entry.
type ANSEntry struct {
	Name string `json:"name"`
	DID  string `json:"did"`
	Tags string `json:"tags,omitempty"`
}

// Reputation holds peer reputation data.
type Reputation struct {
	DID   string  `json:"did"`
	Score float64 `json:"score"`
	Tier  string  `json:"tier,omitempty"`
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
	return filepath.Join(home, ".anet", "daemon.pid")
}

func writePIDFile(pid int) {
	p := pidFilePath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	_ = os.WriteFile(p, []byte(fmt.Sprintf("%d", pid)), 0644)
}

func removePIDFile() {
	if p := pidFilePath(); p != "" {
		_ = os.Remove(p)
	}
}

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
// a process with the anet binary name, then confirming via HTTP ping.
// Falls back to PID file check for robustness.
func (c *Client) isDaemonAlive() bool {
	if pid := findProcessByName(LocalBinaryName()); pid != 0 {
		if c.ping() {
			return true
		}
		removePIDFile()
		return false
	}
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
		filepath.Join(".", "vendor", "agentnet", binName),
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".anet", binName))
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			candidates = append(candidates, filepath.Join(localAppData, "anet", binName))
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("anet"); err == nil {
		return p
	}
	return ""
}

func (c *Client) EnsureDaemon() error {
	return c.EnsureDaemonWithProgress(nil)
}

func (c *Client) EnsureDaemonWithProgress(emitProgress func(stage string, pct int, msg string)) error {
	logDetail("[agentnet-lifecycle] ▶ EnsureDaemon: checking if daemon is already running...")
	if c.ping() {
		logDetail("[agentnet-lifecycle] daemon already reachable via ping")
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

	if c.isDaemonAlive() {
		logDetail("[agentnet-lifecycle] daemon process alive (PID file), marking as running")
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

	logDetail("[agentnet-lifecycle] daemon not running, acquiring startup lock...")
	lockFile, lockErr := acquireDaemonLock()
	if lockErr != nil {
		logDetail("[agentnet-lifecycle] another process holds the startup lock, waiting up to 20s...")
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if c.ping() {
				logDetail("[agentnet-lifecycle] daemon became reachable while waiting for lock")
				c.mu.Lock()
				c.running = true
				c.mu.Unlock()
				return nil
			}
			time.Sleep(1 * time.Second)
		}
		log.Printf("[agentnet-lifecycle] ✖ timed out waiting for another process to start daemon")
		return fmt.Errorf("another process is starting agentnet daemon but it did not become reachable")
	}
	defer lockFile.Close()
	logDetail("[agentnet-lifecycle] startup lock acquired")

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
		logDetail("[agentnet-lifecycle] binary not found locally, downloading...")
		downloaded, err := Download(emitProgress)
		if err != nil {
			log.Printf("[agentnet-lifecycle] ✖ download failed: %v", err)
			return err
		}
		logDetail("[agentnet-lifecycle] binary downloaded to %s", downloaded)
		c.mu.Lock()
		if c.ping() {
			c.running = true
			c.mu.Unlock()
			return nil
		}
		bin = downloaded
	} else {
		logDetail("[agentnet-lifecycle] using binary: %s", bin)
	}
	c.binPath = bin
	c.mu.Unlock()

	// Guard: if an anet process is already running (by name), don't start another.
	if pid := findProcessByName(LocalBinaryName()); pid != 0 {
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
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
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

	if c.ping() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

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
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
		removePIDFile()
		for i := 0; i < 6; i++ {
			if findProcessByName(LocalBinaryName()) == 0 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	logDetail("[agentnet-lifecycle] starting daemon: %s start", bin)
	c.mu.Lock()
	cmd := exec.Command(bin, "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	hideCommandWindow(cmd)
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		log.Printf("[agentnet-lifecycle] ✖ failed to start daemon: %v", err)
		return fmt.Errorf("failed to start agentnet daemon: %w", err)
	}
	c.daemon = cmd
	if cmd.Process != nil {
		writePIDFile(cmd.Process.Pid)
		logDetail("[agentnet-lifecycle] daemon process started, pid=%d", cmd.Process.Pid)
	}
	c.mu.Unlock()

	go func() { _ = cmd.Wait() }()

	logDetail("[agentnet-lifecycle] waiting for daemon to become reachable (timeout 15s)...")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c.ping() {
			logDetail("[agentnet-lifecycle] ✔ daemon is reachable on %s", c.baseURL)
			c.mu.Lock()
			c.running = true
			c.mu.Unlock()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Printf("[agentnet-lifecycle] ✖ daemon started but not responding after 15s on %s", c.baseURL)
	return fmt.Errorf("agentnet daemon started but not responding on %s", c.baseURL)
}

func (c *Client) StopDaemon() {
	logDetail("[agentnet-lifecycle] ◼ StopDaemon: shutting down daemon...")
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
		logDetail("[agentnet-lifecycle] sending stop command via: %s stop", bin)
		cmd := exec.Command(bin, "stop")
		hideCommandWindow(cmd)
		done := make(chan error, 1)
		go func() { done <- cmd.Run() }()
		select {
		case err := <-done:
			if err != nil {
				log.Printf("[agentnet-lifecycle] stop command returned error: %v", err)
			} else {
				logDetail("[agentnet-lifecycle] stop command completed successfully")
			}
		case <-time.After(5 * time.Second):
			log.Printf("[agentnet-lifecycle] stop command timed out after 5s, killing process")
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	} else if daemon != nil && daemon.Process != nil {
		logDetail("[agentnet-lifecycle] no binary path, killing daemon process directly")
		_ = daemon.Process.Kill()
	}
	removePIDFile()
	logDetail("[agentnet-lifecycle] ◼ StopDaemon: complete")
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
		return fmt.Errorf("agentnet GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agentnet GET %s: status %d: %s", path, resp.StatusCode, string(body))
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
		return fmt.Errorf("agentnet POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agentnet POST %s: status %d: %s", path, resp.StatusCode, string(b))
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
		return fmt.Errorf("agentnet DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agentnet DELETE %s: status %d: %s", path, resp.StatusCode, string(b))
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
		return fmt.Errorf("agentnet PUT %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agentnet PUT %s: status %d: %s", path, resp.StatusCode, string(b))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// getList fetches a JSON endpoint and decodes into out (must be a pointer to a
// slice). The daemon may return a bare JSON array or an object wrapping the
// array under one of the given keys. extraKeys are checked in addition to the
// built-in fallbacks "data", "results", "items".
func (c *Client) getList(path string, out interface{}, extraKeys ...string) error {
	var raw json.RawMessage
	if err := c.get(path, &raw); err != nil {
		return err
	}
	// Fast path: bare array.
	if err := json.Unmarshal(raw, out); err == nil {
		return nil
	}
	// Slow path: wrapped object.
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return fmt.Errorf("%s: unexpected response format", path)
	}
	keys := append(extraKeys, "data", "results", "items")
	for _, key := range keys {
		if v, ok := wrapper[key]; ok {
			if err := json.Unmarshal(v, out); err == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("%s: cannot find array in response", path)
}

// postList is like getList but uses POST with a JSON payload.
func (c *Client) postList(path string, payload interface{}, out interface{}, extraKeys ...string) error {
	var raw json.RawMessage
	if err := c.post(path, payload, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err == nil {
		return nil
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return fmt.Errorf("%s: unexpected response format", path)
	}
	keys := append(extraKeys, "data", "results", "items")
	for _, key := range keys {
		if v, ok := wrapper[key]; ok {
			if err := json.Unmarshal(v, out); err == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("%s: cannot find array in response", path)
}

// ---------- Status & Peers ----------

func (c *Client) GetStatus() (*Status, error) {
	var s Status
	return &s, c.get("/api/status", &s)
}

func (c *Client) GetPeers() ([]Peer, error) {
	var peers []Peer
	return peers, c.getList("/api/peers", &peers, "peers")
}

// ---------- Task Bazaar ----------

func (c *Client) ListTasks(status string) ([]Task, error) {
	path := "/api/tasks/board"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var tasks []Task
	return tasks, c.getList(path, &tasks, "tasks", "value")
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
func (c *Client) ApproveTask(id string) error { return c.post("/api/tasks/"+id+"/accept", nil, nil) }
func (c *Client) RejectTask(id string) error  { return c.post("/api/tasks/"+id+"/reject", nil, nil) }
func (c *Client) CancelTask(id string) error  { return c.post("/api/tasks/"+id+"/cancel", nil, nil) }

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

// ---------- Shell Economy ----------

func (c *Client) GetCredits() (*Credits, error) {
	var credits Credits
	return &credits, c.get("/api/credits/balance", &credits)
}

func (c *Client) GetCreditsHistory() ([]map[string]interface{}, error) {
	var history []map[string]interface{}
	return history, c.get("/api/credits/history", &history)
}

func (c *Client) TransferCredits(toDID string, amount float64, reason string) error {
	payload := map[string]interface{}{"to": toDID, "amount": amount}
	if reason != "" {
		payload["reason"] = reason
	}
	return c.post("/api/credits/transfer", payload, nil)
}

func (c *Client) GetLeaderboard() ([]map[string]interface{}, error) {
	var lb []map[string]interface{}
	return lb, c.get("/api/leaderboard", &lb)
}

func (c *Client) GetCreditsAudit() ([]map[string]interface{}, error) {
	var audit []map[string]interface{}
	return audit, c.get("/api/credits/audit", &audit)
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
	if err := c.getList(path, &entries, "entries", "feed", "graphs"); err != nil {
		return nil, err
	}
	// The feed API returns graph-level metadata without content fields.
	// Enrich each entry by fetching graph detail and extracting node intent.
	c.enrichKnowledgeEntries(entries)
	return entries, nil
}

// enrichKnowledgeEntries fetches graph details for entries that lack a title
// and populates Title/Intent/Skills from the first node.
func (c *Client) enrichKnowledgeEntries(entries []KnowledgeEntry) {
	if len(entries) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // concurrency limit

	for i := range entries {
		if entries[i].Title != "" || entries[i].Intent != "" || entries[i].Body != "" {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			var detail struct {
				Nodes []struct {
					Intent   string `json:"intent"`
					Skills   string `json:"skills"`
					NodeType string `json:"node_type"`
				} `json:"nodes"`
			}
			if err := c.get("/api/knowledge/"+entries[idx].ID, &detail); err != nil || len(detail.Nodes) == 0 {
				return
			}
			n := detail.Nodes[0]
			if n.Intent != "" {
				entries[idx].Title = n.Intent
				entries[idx].Intent = n.Intent
			}
			if n.Skills != "" {
				entries[idx].Skills = n.Skills
			}
			if n.NodeType != "" {
				entries[idx].NodeType = n.NodeType
			}
		}(i)
	}
	wg.Wait()
}

func (c *Client) SearchKnowledge(query string) ([]KnowledgeEntry, error) {
	var entries []KnowledgeEntry
	return entries, c.postList("/api/knowledge/search", map[string]interface{}{"query": query}, &entries, "entries", "graphs")
}

func (c *Client) FindClaw(query string) ([]KnowledgeEntry, error) {
	var entries []KnowledgeEntry
	return entries, c.postList("/api/knowledge/findclaw", map[string]interface{}{"query": query}, &entries, "entries", "graphs")
}

func (c *Client) PublishKnowledge(title, body string) (*KnowledgeEntry, error) {
	return c.PublishKnowledgeFull(title, body, nil, nil)
}

func (c *Client) PublishKnowledgeFull(title, body string, domains []string, tags []string) (*KnowledgeEntry, error) {
	payload := map[string]interface{}{"title": title, "content": body}
	if len(domains) > 0 {
		payload["domains"] = domains
	}
	if len(tags) > 0 {
		payload["tags"] = tags
	}
	var entry KnowledgeEntry
	return &entry, c.post("/api/knowledge/publish", payload, &entry)
}

func (c *Client) ReactKnowledge(id, reaction string) error {
	return c.post("/api/knowledge/"+id+"/react", map[string]interface{}{"emoji": reaction}, nil)
}

func (c *Client) ReplyKnowledge(id, body string) error {
	return c.post("/api/knowledge/"+id+"/reply", map[string]interface{}{"body": body}, nil)
}

func (c *Client) GetKnowledgeReplies(id string) ([]map[string]interface{}, error) {
	var replies []map[string]interface{}
	return replies, c.get("/api/knowledge/"+id+"/replies", &replies)
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

func (c *Client) AppealPrediction(predID, reason string) error {
	return c.post("/api/predictions/"+predID+"/appeal", map[string]interface{}{"reason": reason}, nil)
}

func (c *Client) GetPredictionLeaderboard() ([]map[string]interface{}, error) {
	var lb []map[string]interface{}
	return lb, c.get("/api/predictions/leaderboard", &lb)
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
	return c.post("/api/dm/send-plaintext", map[string]interface{}{"to": peerID, "body": body}, nil)
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
	return topics, c.getList("/api/topics", &topics, "topics")
}

func (c *Client) CreateTopic(name, description string) error {
	return c.post("/api/topics", map[string]interface{}{"name": name, "description": description}, nil)
}

func (c *Client) GetTopicMessages(topicName string) ([]TopicMessage, error) {
	var msgs []TopicMessage
	return msgs, c.getList("/api/topics/"+url.PathEscape(topicName)+"/messages", &msgs, "messages")
}

func (c *Client) PostTopicMessage(topicName, body string) error {
	return c.post("/api/topics/"+url.PathEscape(topicName)+"/send", map[string]interface{}{"body": body}, nil)
}

// ---------- P2P Service Gateway (new in skill.md) ----------

func (c *Client) RegisterService(svc *Service) error {
	return c.post("/api/svc/register", svc, nil)
}

func (c *Client) UnregisterService(name string) error {
	return c.post("/api/svc/unregister", map[string]interface{}{"name": name}, nil)
}

func (c *Client) ListServices() ([]Service, error) {
	var svcs []Service
	return svcs, c.get("/api/svc", &svcs)
}

func (c *Client) CallService(peer, service, method, path string, headers map[string]string, body string) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"peer":    peer,
		"service": service,
		"method":  method,
		"path":    path,
	}
	if len(headers) > 0 {
		payload["headers"] = headers
	}
	if body != "" {
		payload["body"] = body
	}
	var result map[string]interface{}
	return result, c.post("/api/svc/call", payload, &result)
}

func (c *Client) DiscoverServices(peer string) ([]Service, error) {
	var svcs []Service
	payload := map[string]interface{}{
		"peer":    peer,
		"service": "__discover__",
	}
	return svcs, c.post("/api/svc/call", payload, &svcs)
}

// ---------- ANS (Agent Name Service) ----------

func (c *Client) RegisterANS(name string, tags string) (*ANSEntry, error) {
	var entry ANSEntry
	payload := map[string]interface{}{"name": name}
	if tags != "" {
		payload["tags"] = tags
	}
	return &entry, c.post("/api/ans/register?confirm=yes", payload, &entry)
}

func (c *Client) ResolveANS(name string) (*ANSEntry, error) {
	var entry ANSEntry
	return &entry, c.get("/api/ans/resolve?name="+url.QueryEscape(name), &entry)
}

func (c *Client) LookupANS(tags string, limit int) ([]ANSEntry, error) {
	params := url.Values{"tags": {tags}}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var entries []ANSEntry
	return entries, c.get("/api/ans/lookup?"+params.Encode(), &entries)
}

func (c *Client) DiscoverAgents(query string) ([]map[string]interface{}, error) {
	var agents []map[string]interface{}
	return agents, c.get("/api/discover?q="+url.QueryEscape(query), &agents)
}

func (c *Client) CrossDomainSearch(query string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	return results, c.get("/api/search?q="+url.QueryEscape(query), &results)
}

// ---------- Reputation ----------

func (c *Client) GetReputation(did string) (*Reputation, error) {
	var rep Reputation
	return &rep, c.get("/api/reputation/"+url.PathEscape(did), &rep)
}

// ---------- Proof of Intelligence (PoI) ----------

func (c *Client) ListPoIChallenges() ([]map[string]interface{}, error) {
	var challenges []map[string]interface{}
	return challenges, c.get("/api/poi/challenges", &challenges)
}

func (c *Client) RespondToPoI(challengeID string, response map[string]interface{}) error {
	return c.post("/api/poi/challenges/"+challengeID+"/respond", response, nil)
}

func (c *Client) GetPoIScores() ([]map[string]interface{}, error) {
	var scores []map[string]interface{}
	return scores, c.get("/api/poi/scores", &scores)
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

// ---------- Diagnostics ----------

func (c *Client) GetDiagnostics() (map[string]interface{}, error) {
	var diag map[string]interface{}
	return diag, c.get("/api/diagnostics", &diag)
}

func (c *Client) GetMatrixStatus() (map[string]interface{}, error) {
	var status map[string]interface{}
	return status, c.get("/api/matrix/status", &status)
}

func (c *Client) GetTraffic() (map[string]interface{}, error) {
	var traffic map[string]interface{}
	return traffic, c.get("/api/traffic", &traffic)
}

func (c *Client) SelfUpdate() error {
	info, err := SmartUpdate(nil)
	if err != nil {
		return err
	}
	if info != nil && info.NeedsUpdate {
		return fmt.Errorf("update check passed but update was not applied")
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
	return filepath.Join(home, ".anet", ".last_update")
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
		logFn("AgentNet: auto-update check started")
	}

	// Smart update: compare versions before downloading.
	info, err := SmartUpdate(func(stage string, pct int, msg string) {
		if logFn != nil {
			logFn(fmt.Sprintf("AgentNet: [%s] %d%% %s", stage, pct, msg))
		}
	})
	if err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("AgentNet: auto-update failed (non-fatal): %v", err))
		}
		return
	}

	writeLastUpdateTime()
	if info != nil && !info.NeedsUpdate {
		if logFn != nil {
			logFn(fmt.Sprintf("AgentNet: already up to date (%s)", info.LocalVersion))
		}
		return
	}
	if logFn != nil {
		if info != nil {
			logFn(fmt.Sprintf("AgentNet: updated %s → %s", info.LocalVersion, info.RemoteVersion))
		} else {
			logFn("AgentNet: auto-update completed successfully")
		}
	}
	if !c.ping() && logFn != nil {
		logFn("AgentNet: daemon unreachable after update — it may need a manual restart")
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
	endpoint := strings.TrimRight(hubURL, "/") + "/api/agentnet/tasks/publish"
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
	endpoint := strings.TrimRight(hubURL, "/") + "/api/agentnet/tasks/browse?limit=50"
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

// ---------- Agent Card & Init (skill.md §1, §5) ----------

// PublishAgentCard publishes the agent's profile card to the network.
func (c *Client) PublishAgentCard(name, desc string, skills []string) error {
	return c.post("/api/adp/publish", map[string]interface{}{
		"name":   name,
		"desc":   desc,
		"skills": skills,
	}, nil)
}

// InitAgent initializes the agent identity and profile (equivalent to `anet init`).
func (c *Client) InitAgent(name string, skills []string) error {
	return c.post("/api/init", map[string]interface{}{
		"name":   name,
		"skills": skills,
	}, nil)
}

// ---------- Task Bundles (skill.md §Workflow B Step 4-5) ----------

// AttachBundle attaches a .nut bundle to a task.
func (c *Client) AttachBundle(taskID string, bundleData []byte) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/tasks/"+taskID+"/bundle", bytes.NewReader(bundleData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST /api/tasks/%s/bundle: %w", taskID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /api/tasks/%s/bundle: status %d: %s", taskID, resp.StatusCode, string(b))
	}
	return nil
}

// DownloadBundle downloads a .nut bundle from a task.
func (c *Client) DownloadBundle(taskID string) ([]byte, error) {
	resp, err := c.client.Get(c.baseURL + "/api/tasks/" + taskID + "/bundle")
	if err != nil {
		return nil, fmt.Errorf("GET /api/tasks/%s/bundle: %w", taskID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /api/tasks/%s/bundle: status %d: %s", taskID, resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}

// ---------- Split Tasks (skill.md §8) ----------

// CreateSplitTask creates a multi-slot task.
func (c *Client) CreateSplitTask(title string, reward float64, slots int) (*Task, error) {
	var task Task
	return &task, c.post("/api/tasks/split", map[string]interface{}{
		"title":  title,
		"reward": reward,
		"slots":  slots,
	}, &task)
}

// ---------- Disputes (skill.md §8) ----------

// FileDispute files a dispute for a rejected task.
func (c *Client) FileDispute(taskID, reason string) error {
	return c.post("/api/disputes", map[string]interface{}{
		"task_id": taskID,
		"reason":  reason,
	}, nil)
}

// ---------- DAG & Ontology (skill.md §8) ----------

// DAGNode represents a node in a task DAG.
type DAGNode struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Deps    []string `json:"deps,omitempty"`
	Outputs []string `json:"outputs,omitempty"`
}

// ExtractDAG extracts a structured DAG from task steps.
func (c *Client) ExtractDAG(intent string, steps []string, outputs []string) ([]DAGNode, error) {
	var nodes []DAGNode
	return nodes, c.post("/api/dag/extract", map[string]interface{}{
		"intent":  intent,
		"steps":   steps,
		"outputs": outputs,
	}, &nodes)
}

// QueryOntology queries the knowledge graph for a subgraph.
func (c *Client) QueryOntology(query string, depth int) (map[string]interface{}, error) {
	path := fmt.Sprintf("/api/ontology/subgraph?q=%s&depth=%d", url.QueryEscape(query), depth)
	var result map[string]interface{}
	return result, c.get(path, &result)
}
