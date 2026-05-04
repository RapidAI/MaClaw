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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentnet"
)

const agentNetMinPaidTaskReward = 100

var agentNetANSNamePattern = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

// AgentNetClient wraps the anet daemon REST API (localhost:3998).
// It manages the daemon lifecycle and provides typed access to all endpoints.
// The anet daemon is a standalone process that persists across maclaw restarts.
type AgentNetClient struct {
	mu      sync.Mutex
	baseURL string
	client  *http.Client
	binPath string
	running bool

	tokenMu     sync.RWMutex // guards apiToken / tokenLoaded
	apiToken    string       // Bearer token read from ~/.anet/api_token
	tokenLoaded bool         // true after first successful or failed load attempt

	autoUpdateStop chan struct{} // signals the auto-update goroutine to stop
}

// BinPath returns the resolved path to the anet binary.
func (c *AgentNetClient) BinPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.binPath != "" {
		return c.binPath
	}
	return c.findBinary()
}

// AgentNet API response types

type AgentNetStatus struct {
	PeerID   string `json:"peer_id"`
	Peers    int    `json:"peers"`
	UnreadDM int    `json:"unread_dm"`
	Version  string `json:"version"`
	Uptime   string `json:"uptime,omitempty"`
}

type AgentNetTaskCreateOptions struct {
	RequireDeposit   bool
	DepositConfirmed bool
}

type AgentNetPeer struct {
	PeerID  string   `json:"peer_id"`
	Addrs   []string `json:"addrs,omitempty"`
	Addr    string   `json:"addr,omitempty"`
	Latency string   `json:"latency,omitempty"`
	Country string   `json:"country,omitempty"`
	City    string   `json:"city,omitempty"`
}

type AgentNetTask struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"state"` // created, accepted, rejected, cancelled
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

type AgentNetCredits struct {
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

type AgentNetKnowledgeEntry struct {
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
	NodeType  string   `json:"node_type,omitempty"`
	GraphID   string   `json:"graph_id,omitempty"`
	Upvotes   int      `json:"upvotes,omitempty"`
	NodeCount int      `json:"node_count,omitempty"`
	EdgeCount int      `json:"edge_count,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
	Metadata  string   `json:"metadata,omitempty"`
}

// DisplayTitle returns the best available title for display.
func (e AgentNetKnowledgeEntry) DisplayTitle() string {
	if e.Title != "" {
		return e.Title
	}
	if e.Intent != "" {
		return e.Intent
	}
	if e.ID != "" {
		return e.ID[:min(len(e.ID), 16)]
	}
	return "Untitled"
}

type AgentNetPrediction struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Status   string   `json:"status,omitempty"`
	Creator  string   `json:"creator,omitempty"`
}

type AgentNetSwarmSession struct {
	ID       string `json:"id"`
	Topic    string `json:"topic"`
	Question string `json:"question,omitempty"`
	Status   string `json:"status,omitempty"`
	Members  int    `json:"members,omitempty"`
}

type AgentNetDM struct {
	PeerID    string `json:"peer_id,omitempty"`
	From      string `json:"from,omitempty"`
	Body      string `json:"body"`
	Unread    int    `json:"unread,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	SentAt    string `json:"sent_at,omitempty"`
}

type AgentNetResume struct {
	PeerID  string   `json:"peer_id,omitempty"`
	Name    string   `json:"name,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	Domains []string `json:"domains,omitempty"`
	Bio     string   `json:"bio,omitempty"`
}

// NewAgentNetClient creates a client pointing at the default daemon port.
func NewAgentNetClient() *AgentNetClient {
	return &AgentNetClient{
		baseURL: "http://127.0.0.1:3998",
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

func normalizeAgentNetTaskReward(reward float64) (float64, error) {
	if reward < 0 {
		return 0, fmt.Errorf("task reward cannot be negative")
	}
	if reward > 0 && reward < agentNetMinPaidTaskReward {
		return 0, nil
	}
	return reward, nil
}

func normalizeAgentNetANSName(name string) (string, error) {
	name = strings.TrimSpace(strings.ToLower(strings.TrimPrefix(name, "agent://")))
	if !agentNetANSNamePattern.MatchString(name) {
		return "", fmt.Errorf("ANS name must be lowercase alphanumeric and 1-32 characters")
	}
	return name, nil
}

func normalizeAgentNetServiceName(name string) (string, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if !agentNetANSNamePattern.MatchString(name) {
		return "", fmt.Errorf("service name must be lowercase alphanumeric and 1-32 characters")
	}
	return name, nil
}

func normalizeAgentNetServiceCall(peer, service, method, requestPath string) (string, string, string, string, error) {
	peer = strings.TrimSpace(peer)
	if peer == "" {
		return "", "", "", "", fmt.Errorf("peer is required")
	}
	service, err := normalizeAgentNetServiceName(service)
	if err != nil {
		return "", "", "", "", err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodPost
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
	default:
		return "", "", "", "", fmt.Errorf("unsupported service method: %s", method)
	}
	requestPath = strings.TrimSpace(requestPath)
	if requestPath == "" {
		requestPath = "/"
	}
	if strings.ContainsAny(requestPath, "\r\n") {
		return "", "", "", "", fmt.Errorf("service path cannot contain newlines")
	}
	if strings.HasPrefix(requestPath, "//") {
		return "", "", "", "", fmt.Errorf("service path must not start with //")
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	return peer, service, method, requestPath, nil
}

func normalizeAgentNetOntologyQuery(query string, depth int) (string, int, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", 0, fmt.Errorf("ontology query is required")
	}
	if depth <= 0 {
		depth = 2
	}
	if depth > 10 {
		depth = 10
	}
	return query, depth, nil
}
func validateAgentNetServiceRegistration(reg *AgentNetServiceRegistration) error {
	if reg == nil {
		return fmt.Errorf("service is nil")
	}
	name, err := normalizeAgentNetServiceName(reg.Name)
	if err != nil {
		return err
	}
	reg.Name = name
	reg.URL = strings.TrimSpace(reg.URL)
	parsed, err := url.Parse(reg.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("service url must be an absolute localhost HTTP URL")
	}
	if parsed.Scheme != "http" {
		return fmt.Errorf("service url must use http")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("service url must point to localhost or loopback")
	}
	reg.Billing = strings.TrimSpace(strings.ToLower(reg.Billing))
	if reg.Billing == "" {
		reg.Billing = "free"
	}
	switch reg.Billing {
	case "free", "per_call", "per_kb":
	default:
		return fmt.Errorf("billing must be free, per_call, or per_kb")
	}
	if reg.Price < 0 {
		return fmt.Errorf("service price cannot be negative")
	}
	if reg.Billing != "free" && reg.Price <= 0 {
		return fmt.Errorf("paid services require a positive price")
	}
	if reg.FreeTier < 0 {
		return fmt.Errorf("free tier cannot be negative")
	}
	if len(reg.Modes) == 0 {
		reg.Modes = []string{"rr"}
	}
	for i, mode := range reg.Modes {
		mode = strings.TrimSpace(strings.ToLower(mode))
		reg.Modes[i] = mode
		switch mode {
		case "rr", "server-stream", "bidi":
		default:
			return fmt.Errorf("unsupported service mode: %s", mode)
		}
	}
	return nil
}

// ---------- Daemon lifecycle ----------

// findBinary locates the anet executable.
// Search order: install dir -> PATH.
func (c *AgentNetClient) findBinary() string {
	binName := anetLocalBinaryName()
	// 1. Standard install dir
	if dir, err := anetInstallDir(); err == nil {
		p := filepath.Join(dir, binName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// 2. PATH lookup
	if p, err := exec.LookPath("anet"); err == nil {
		return p
	}
	return ""
}

// EnsureDaemon starts the anet daemon if not already running.
// It first checks whether the daemon is reachable; if so, it skips launching.
func (c *AgentNetClient) EnsureDaemon() error {
	return c.EnsureDaemonWithProgress(nil)
}

// EnsureDaemonWithProgress starts the daemon, auto-downloading the binary if needed.
// The anet daemon is a standalone process - it persists across maclaw restarts.
// We only start it if no instance is already running (ping or process check).
func (c *AgentNetClient) EnsureDaemonWithProgress(emitProgress func(stage string, pct int, msg string)) error {
	// Fast path: daemon already reachable.
	if c.ping() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

	// Cross-process file lock to prevent multiple maclaw instances from
	// racing to start the daemon simultaneously.
	lockFile, lockErr := acquireGUIDaemonLock()
	if lockErr != nil {
		// Another process is starting the daemon - wait for it.
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
		return fmt.Errorf("another process is starting anet daemon but it did not become reachable")
	}
	defer lockFile.Close()

	// Re-check after acquiring lock.
	if c.ping() {
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		return nil
	}

	// Locate or install the binary.
	c.mu.Lock()
	bin := c.binPath
	if bin == "" {
		bin = c.findBinary()
	}
	if bin == "" {
		c.mu.Unlock()
		downloaded, err := DownloadAnet(emitProgress)
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

	// Check if an anet process is already running (e.g. started externally).
	// If so, wait for it to become reachable instead of spawning a duplicate.
	if pid := agentnetFindProcessByName(anetLocalBinaryName()); pid != 0 {
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
		// Process exists but not responding - kill it before starting fresh.
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
		for i := 0; i < 6; i++ {
			if agentnetFindProcessByName(anetLocalBinaryName()) == 0 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Start the daemon. anet manages its own singleton via PID file internally.
	cmd := exec.Command(bin, "start")
	cmd.Stdout = nil
	cmd.Stderr = nil
	hideCommandWindow(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start anet daemon: %w", err)
	}
	// Reap the child process in the background to avoid zombie processes.
	go func() { _ = cmd.Wait() }()

	// Wait for daemon to become ready (up to 15s).
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c.ping() {
			c.mu.Lock()
			c.running = true
			c.mu.Unlock()
			c.clearTokenCache() // re-read token from disk after daemon restart
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("anet daemon started but not responding on %s", c.baseURL)
}

// StopDaemon gracefully stops the daemon via `anet stop`.
// Note: the anet daemon is designed to persist independently. This is only
// called when the user explicitly requests a stop from the GUI.
func (c *AgentNetClient) StopDaemon() {
	c.StopAutoUpdate()
	c.mu.Lock()
	bin := c.binPath
	if bin == "" {
		bin = c.findBinary()
	}
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
	}
}

// IsRunning returns true if the daemon is reachable.
// It retries once after a short pause to tolerate transient failures
// (e.g. after system wake from sleep).
func (c *AgentNetClient) IsRunning() bool {
	if c.ping() {
		return true
	}
	// One quick retry to avoid false negatives on transient hiccups.
	time.Sleep(300 * time.Millisecond)
	return c.ping()
}

func (c *AgentNetClient) ping() bool {
	resp, err := c.client.Get(c.baseURL + "/api/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	// 401 means the daemon is alive (just needs auth), so treat it as reachable.
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized
}

// DaemonPID returns the PID of a running anet process, or 0 if not found.
func (c *AgentNetClient) DaemonPID() int {
	return agentnetFindProcessByName(anetLocalBinaryName())
}

// ---------- HTTP helpers ----------

// loadAPIToken reads the API token from ~/.anet/api_token (or the install dir).
// The result is cached; disk I/O happens at most once until the cache is
// explicitly cleared (e.g. after a daemon restart).
func (c *AgentNetClient) loadAPIToken() string {
	// Fast path: already loaded.
	c.tokenMu.RLock()
	if c.tokenLoaded {
		tok := c.apiToken
		c.tokenMu.RUnlock()
		return tok
	}
	c.tokenMu.RUnlock()

	// Slow path: read from disk.
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.tokenLoaded {
		return c.apiToken // another goroutine loaded it while we waited
	}
	c.tokenLoaded = true

	if token := strings.TrimSpace(os.Getenv("AGENTNETWORK_API_TOKEN")); token != "" {
		c.apiToken = token
		return c.apiToken
	}

	// Try ~/.anet/api_token first (matches the daemon's error message).
	if home, err := os.UserHomeDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(home, ".anet", "api_token")); err == nil {
			c.apiToken = strings.TrimSpace(string(data))
			return c.apiToken
		}
	}
	// Fallback: anetInstallDir (e.g. %LOCALAPPDATA%\anet on Windows).
	if dir, err := anetInstallDir(); err == nil {
		if data, err := os.ReadFile(filepath.Join(dir, "api_token")); err == nil {
			c.apiToken = strings.TrimSpace(string(data))
			return c.apiToken
		}
	}
	return ""
}

// clearTokenCache resets the cached token so the next request re-reads from disk.
func (c *AgentNetClient) clearTokenCache() {
	c.tokenMu.Lock()
	c.apiToken = ""
	c.tokenLoaded = false
	c.tokenMu.Unlock()
}

// setAuth adds the Authorization header if a token is available.
func (c *AgentNetClient) setAuth(req *http.Request) {
	if tok := c.loadAPIToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
}

// handleAuthError checks if the response is 401 and clears the token cache
// so the next request will re-read the token from disk. This handles the case
// where the daemon was restarted externally and generated a new token.
func (c *AgentNetClient) handleAuthError(resp *http.Response) {
	if resp != nil && resp.StatusCode == http.StatusUnauthorized {
		c.clearTokenCache()
	}
}

func (c *AgentNetClient) get(path string, out interface{}) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("agentnet GET %s: %w", path, err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("agentnet GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	c.handleAuthError(resp)
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

func (c *AgentNetClient) post(path string, payload interface{}, out interface{}) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("agentnet POST %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("agentnet POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	c.handleAuthError(resp)
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

func (c *AgentNetClient) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("agentnet DELETE %s: %w", path, err)
	}
	defer resp.Body.Close()
	c.handleAuthError(resp)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agentnet DELETE %s: status %d: %s", path, resp.StatusCode, string(b))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *AgentNetClient) put(path string, payload interface{}, out interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("agentnet PUT %s: %w", path, err)
	}
	defer resp.Body.Close()
	c.handleAuthError(resp)
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
// slice). Handles both bare arrays and objects wrapping the array.
func (c *AgentNetClient) getList(path string, out interface{}, extraKeys ...string) error {
	var raw json.RawMessage
	if err := c.get(path, &raw); err != nil {
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

func (c *AgentNetClient) postList(path string, payload interface{}, out interface{}, extraKeys ...string) error {
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

func (c *AgentNetClient) GetStatus() (*AgentNetStatus, error) {
	var s AgentNetStatus
	return &s, c.get("/api/status", &s)
}

func (c *AgentNetClient) GetPeers() ([]AgentNetPeer, error) {
	var peers []AgentNetPeer
	return peers, c.getList("/api/peers", &peers, "peers")
}

// ---------- Task Bazaar ----------

func (c *AgentNetClient) ListTasks(status string) ([]AgentNetTask, error) {
	path := "/api/tasks/board"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var tasks []AgentNetTask
	return tasks, c.getList(path, &tasks, "tasks", "value")
}

func (c *AgentNetClient) GetTaskBoard() (map[string]interface{}, error) {
	var board map[string]interface{}
	return board, c.get("/api/tasks/board", &board)
}

func (c *AgentNetClient) CreateTask(title string, reward float64) (*AgentNetTask, error) {
	return c.CreateTaskFull(title, "", reward, nil, "")
}

// CreateTaskFull creates a task with all optional fields: description, tags, target_peer.
func (c *AgentNetClient) CreateTaskFull(title, description string, reward float64, tags []string, targetPeer string) (*AgentNetTask, error) {
	return c.CreateTaskWithOptions(title, description, reward, tags, targetPeer, AgentNetTaskCreateOptions{})
}

func (c *AgentNetClient) CreateTaskWithOptions(title, description string, reward float64, tags []string, targetPeer string, opts AgentNetTaskCreateOptions) (*AgentNetTask, error) {
	normalizedReward, err := normalizeAgentNetTaskReward(reward)
	if err != nil {
		return nil, err
	}
	if opts.RequireDeposit && !opts.DepositConfirmed {
		return nil, fmt.Errorf("require_deposit needs explicit user confirmation")
	}
	payload := map[string]interface{}{
		"title":  title,
		"reward": normalizedReward,
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
	if opts.RequireDeposit {
		payload["require_deposit"] = true
	}
	var task AgentNetTask
	return &task, c.post("/api/tasks", payload, &task)
}

func (c *AgentNetClient) GetTask(id string) (*AgentNetTask, error) {
	var task AgentNetTask
	return &task, c.get("/api/tasks/"+url.PathEscape(id), &task)
}

func (c *AgentNetClient) BidOnTask(id string, amount float64, message string) error {
	if amount < 0 {
		return fmt.Errorf("bid amount cannot be negative")
	}
	payload := map[string]interface{}{
		"message": strings.TrimSpace(message),
	}
	if amount > 0 {
		payload["amount"] = amount
	}
	return c.post("/api/tasks/"+url.PathEscape(id)+"/bid", payload, nil)
}

func (c *AgentNetClient) AssignTask(id, peerID string) error {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return fmt.Errorf("assignee peer ID is required")
	}
	return c.post("/api/tasks/"+url.PathEscape(id)+"/assign", map[string]interface{}{
		"bidder_id": peerID,
	}, nil)
}

func (c *AgentNetClient) ClaimTask(id string) error {
	return c.post("/api/tasks/"+url.PathEscape(id)+"/claim", nil, nil)
}

func (c *AgentNetClient) ApproveTask(id string) error {
	return c.post("/api/tasks/"+url.PathEscape(id)+"/accept", nil, nil)
}

func (c *AgentNetClient) RejectTask(id string) error {
	return c.post("/api/tasks/"+url.PathEscape(id)+"/reject", nil, nil)
}

func (c *AgentNetClient) CancelTask(id string) error {
	return c.post("/api/tasks/"+url.PathEscape(id)+"/cancel", nil, nil)
}

// ---------- Shell Economy ----------

func (c *AgentNetClient) GetCredits() (*AgentNetCredits, error) {
	var credits AgentNetCredits
	return &credits, c.get("/api/credits/balance", &credits)
}

func (c *AgentNetClient) GetCreditsHistory() ([]map[string]interface{}, error) {
	var history []map[string]interface{}
	return history, c.get("/api/credits/history", &history)
}

// ---------- Knowledge Mesh ----------

func (c *AgentNetClient) GetKnowledgeFeed(domain string, limit int) ([]AgentNetKnowledgeEntry, error) {
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
	var entries []AgentNetKnowledgeEntry
	if err := c.getList(path, &entries, "entries", "feed", "graphs"); err != nil {
		return nil, err
	}
	// The feed API returns graph-level metadata (id, node_count, etc.) without
	// content fields (title/body/intent). Enrich each entry by fetching the
	// graph detail and extracting the first node's intent as the title.
	c.enrichKnowledgeEntries(entries)
	return entries, nil
}

// enrichKnowledgeEntries fetches graph details for entries that lack a title
// and populates Title/Intent/Skills from the first node.
func (c *AgentNetClient) enrichKnowledgeEntries(entries []AgentNetKnowledgeEntry) {
	if len(entries) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5) // concurrency limit

	for i := range entries {
		if entries[i].Title != "" || entries[i].Intent != "" || entries[i].Body != "" {
			continue // already has content
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

func (c *AgentNetClient) SearchKnowledge(query string) ([]AgentNetKnowledgeEntry, error) {
	var entries []AgentNetKnowledgeEntry
	return entries, c.postList("/api/knowledge/search", map[string]interface{}{"query": query}, &entries, "entries", "graphs")
}

func (c *AgentNetClient) PublishKnowledge(title, body string) (*AgentNetKnowledgeEntry, error) {
	return c.PublishKnowledgeFull(title, body, nil)
}

// PublishKnowledgeFull publishes knowledge with optional domain tags.
func (c *AgentNetClient) PublishKnowledgeFull(title, body string, domains []string) (*AgentNetKnowledgeEntry, error) {
	payload := map[string]interface{}{
		"title": title,
		"body":  body,
	}
	if len(domains) > 0 {
		payload["domains"] = domains
	}
	var entry AgentNetKnowledgeEntry
	return &entry, c.post("/api/knowledge", payload, &entry)
}

func (c *AgentNetClient) ReactKnowledge(id, reaction string) error {
	return c.post("/api/knowledge/"+id+"/react", map[string]interface{}{
		"emoji": reaction,
	}, nil)
}

func (c *AgentNetClient) ReplyKnowledge(id, body string) error {
	return c.post("/api/knowledge/"+id+"/reply", map[string]interface{}{
		"body": body,
	}, nil)
}

// ---------- Prediction Market ----------

func (c *AgentNetClient) ListPredictions() ([]AgentNetPrediction, error) {
	var preds []AgentNetPrediction
	return preds, c.get("/api/predictions", &preds)
}

func (c *AgentNetClient) CreatePrediction(question string, options []string) (*AgentNetPrediction, error) {
	var pred AgentNetPrediction
	return &pred, c.post("/api/predictions", map[string]interface{}{
		"question": question,
		"options":  options,
	}, &pred)
}

func (c *AgentNetClient) PlaceBet(predID, option string, stake float64) error {
	return c.post("/api/predictions/"+predID+"/bet", map[string]interface{}{
		"option": option,
		"amount": stake,
	}, nil)
}

func (c *AgentNetClient) ResolvePrediction(predID, result string) error {
	return c.post("/api/predictions/"+predID+"/resolve", map[string]interface{}{
		"winning_option": result,
	}, nil)
}

// ---------- Swarm Think ----------

func (c *AgentNetClient) ListSwarmSessions() ([]AgentNetSwarmSession, error) {
	var sessions []AgentNetSwarmSession
	return sessions, c.get("/api/swarm", &sessions)
}

func (c *AgentNetClient) CreateSwarmSession(topic, question string) (*AgentNetSwarmSession, error) {
	var session AgentNetSwarmSession
	return &session, c.post("/api/swarm", map[string]interface{}{
		"topic":       topic,
		"description": question,
	}, &session)
}

func (c *AgentNetClient) JoinSwarm(sessionID string) error {
	return c.post("/api/swarm/"+sessionID+"/join", nil, nil)
}

func (c *AgentNetClient) ContributeToSwarm(sessionID, message, stance string) error {
	payload := map[string]interface{}{
		"body": message,
	}
	if stance != "" {
		payload["stance"] = stance
	}
	return c.post("/api/swarm/"+sessionID+"/contribute", payload, nil)
}

func (c *AgentNetClient) SynthesizeSwarm(sessionID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	return result, c.post("/api/swarm/"+sessionID+"/synthesize", nil, &result)
}

// ---------- Direct Messages ----------

func (c *AgentNetClient) SendDM(peerID, body string) error {
	peerID = strings.TrimSpace(peerID)
	if strings.HasPrefix(peerID, "agent://") {
		entry, err := c.ANSResolve(peerID)
		if err != nil {
			return err
		}
		peerID = entry.DID
	} else if !strings.HasPrefix(peerID, "did:") && !strings.HasPrefix(peerID, "12D3") {
		entry, err := c.ANSResolve(peerID)
		if err != nil {
			return err
		}
		peerID = entry.DID
	}
	if peerID == "" {
		return fmt.Errorf("DM target is empty")
	}
	return c.post("/api/dm/send", map[string]interface{}{
		"peer_id": peerID,
		"body":    body,
	}, nil)
}

func (c *AgentNetClient) GetDMInbox() ([]AgentNetDM, error) {
	var inbox []AgentNetDM
	return inbox, c.get("/api/dm/inbox", &inbox)
}

func (c *AgentNetClient) GetDMThread(peerID string, limit int) ([]AgentNetDM, error) {
	path := "/api/dm/thread/" + url.PathEscape(peerID)
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var thread []AgentNetDM
	return thread, c.get(path, &thread)
}

// ---------- Resume / Agent Profile ----------

func (c *AgentNetClient) GetResume() (*AgentNetResume, error) {
	var r AgentNetResume
	return &r, c.get("/api/resume", &r)
}

func (c *AgentNetClient) UpdateResume(resume *AgentNetResume) error {
	return c.put("/api/resume", resume, nil)
}

// MatchResume finds agents matching a task. Delegates to MatchAgentsForTask.
func (c *AgentNetClient) MatchResume(taskID string) ([]AgentNetResume, error) {
	return c.MatchAgentsForTask(taskID)
}

// ---------- Profile ----------

type AgentNetProfile struct {
	PeerID string `json:"peer_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Bio    string `json:"bio,omitempty"`
	Motto  string `json:"motto,omitempty"`
}

func (c *AgentNetClient) GetProfile() (*AgentNetProfile, error) {
	var p AgentNetProfile
	return &p, c.get("/api/profile", &p)
}

func (c *AgentNetClient) UpdateProfile(name, bio string) error {
	return c.put("/api/profile", map[string]interface{}{"name": name, "bio": bio}, nil)
}

func (c *AgentNetClient) SetMotto(motto string) error {
	return c.put("/api/motto", map[string]interface{}{"motto": motto}, nil)
}

// ---------- Topic Rooms ----------

type AgentNetTopic struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Access      string `json:"access,omitempty"`
	Members     int    `json:"members,omitempty"`
	Messages    int    `json:"messages,omitempty"`
	JoinedAt    string `json:"joined_at,omitempty"`
}

type AgentNetTopicMessage struct {
	ID        string `json:"id,omitempty"`
	From      string `json:"from,omitempty"`
	Body      string `json:"body"`
	Timestamp string `json:"timestamp,omitempty"`
}

func (c *AgentNetClient) ListTopics() ([]AgentNetTopic, error) {
	var topics []AgentNetTopic
	return topics, c.getList("/api/topics", &topics, "topics")
}

func (c *AgentNetClient) CreateTopic(name, description string) error {
	return c.post("/api/topics", map[string]interface{}{
		"name": name, "description": description,
	}, nil)
}

func (c *AgentNetClient) GetTopicMessages(topicName string) ([]AgentNetTopicMessage, error) {
	var msgs []AgentNetTopicMessage
	return msgs, c.getList("/api/topics/"+url.PathEscape(topicName)+"/messages", &msgs, "messages")
}

func (c *AgentNetClient) PostTopicMessage(topicName, body string) error {
	return c.post("/api/topics/"+url.PathEscape(topicName)+"/send", map[string]interface{}{
		"body": body,
	}, nil)
}

// ---------- Task Bazaar (extended) ----------

func (c *AgentNetClient) SubmitTaskResult(id, result string) error {
	return c.post("/api/tasks/"+url.PathEscape(id)+"/submit", map[string]interface{}{
		"evidence": result,
		"result":   result,
	}, nil)
}

func (c *AgentNetClient) GetTaskBids(id string) ([]map[string]interface{}, error) {
	var bids []map[string]interface{}
	return bids, c.get("/api/tasks/"+url.PathEscape(id)+"/bids", &bids)
}

func (c *AgentNetClient) MatchTasks() ([]AgentNetTask, error) {
	var tasks []AgentNetTask
	return tasks, c.get("/api/match/tasks", &tasks)
}

func (c *AgentNetClient) MatchAgentsForTask(taskID string) ([]AgentNetResume, error) {
	var agents []AgentNetResume
	return agents, c.get("/api/tasks/"+url.PathEscape(taskID)+"/match", &agents)
}

// ---------- Credits (extended) ----------

func (c *AgentNetClient) GetLeaderboard() ([]map[string]interface{}, error) {
	var lb []map[string]interface{}
	return lb, c.get("/api/leaderboard", &lb)
}

// ---------- Diagnostics ----------

func (c *AgentNetClient) GetDiagnostics() (map[string]interface{}, error) {
	var diag map[string]interface{}
	return diag, c.get("/api/diagnostics", &diag)
}

func (c *AgentNetClient) SelfUpdate() error {
	bin := c.binPath
	if bin == "" {
		bin = c.findBinary()
	}
	if bin == "" {
		return fmt.Errorf("anet binary not found")
	}
	cmd := exec.Command(bin, "update")
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("update failed: %w - %s", err, string(out))
	}
	return nil
}

// ---------- Auto-Update ----------

const agentnetAutoUpdateInterval = 24 * time.Hour

// agentnetLastUpdatePath returns the path to the timestamp file.
func agentnetLastUpdatePath() string {
	dir, err := anetInstallDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, ".last_update")
}

// readLastUpdateTime reads the last successful update timestamp.
func readLastUpdateTime() time.Time {
	p := agentnetLastUpdatePath()
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
	p := agentnetLastUpdatePath()
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
	return time.Since(last) > agentnetAutoUpdateInterval
}

// tryAutoUpdate runs SelfUpdate and records the timestamp on success.
// Errors are logged but never propagated.
func (c *AgentNetClient) tryAutoUpdate(logFn func(string)) {
	if logFn != nil {
		logFn("AgentNet: auto-update check started")
	}
	if err := c.SelfUpdate(); err != nil {
		if logFn != nil {
			logFn(fmt.Sprintf("AgentNet: auto-update failed (non-fatal): %v", err))
		}
		return
	}
	writeLastUpdateTime()
	if logFn != nil {
		logFn("AgentNet: auto-update completed successfully")
	}
	// SelfUpdate may replace the binary; verify daemon is still alive.
	if !c.ping() && logFn != nil {
		logFn("AgentNet: daemon unreachable after update - it may need a manual restart")
	}
}

// StartAutoUpdate launches a background goroutine that:
//  1. Checks on startup if >24h since last update and runs immediately if so.
//  2. Then ticks every 24h to run SelfUpdate.
//
// Idempotent while running. After StopAutoUpdate/StopDaemon it can be
// started again (e.g. daemon restart).
func (c *AgentNetClient) StartAutoUpdate(logFn func(string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.autoUpdateStop != nil {
		// Check if previous goroutine already exited.
		select {
		case <-c.autoUpdateStop:
			// Closed - allow restart below.
		default:
			return // still running
		}
	}
	c.autoUpdateStop = make(chan struct{})
	go c.autoUpdateLoop(logFn, c.autoUpdateStop)
}

// StopAutoUpdate cancels the background auto-update goroutine.
func (c *AgentNetClient) StopAutoUpdate() {
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

func (c *AgentNetClient) autoUpdateLoop(logFn func(string), stop <-chan struct{}) {
	// Immediate check on startup.
	if needsUpdate() {
		c.tryAutoUpdate(logFn)
	}

	ticker := time.NewTicker(agentnetAutoUpdateInterval)
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
func (c *AgentNetClient) GetKnowledgeReplies(id string) ([]map[string]interface{}, error) {
	var replies []map[string]interface{}
	return replies, c.get("/api/knowledge/"+id+"/replies", &replies)
}

// ---------- Credits Audit ----------

// GetCreditsAudit returns the credit audit log.
func (c *AgentNetClient) GetCreditsAudit() ([]map[string]interface{}, error) {
	var audit []map[string]interface{}
	return audit, c.get("/api/credits/audit", &audit)
}

// ---------- Prediction Market (extended) ----------

// AppealPrediction files an appeal against a prediction resolution.
func (c *AgentNetClient) AppealPrediction(predID, reason string) error {
	return c.post("/api/predictions/"+predID+"/appeal", map[string]interface{}{
		"reason": reason,
	}, nil)
}

// GetPredictionLeaderboard returns the prediction market leaderboard.
func (c *AgentNetClient) GetPredictionLeaderboard() ([]map[string]interface{}, error) {
	var lb []map[string]interface{}
	return lb, c.get("/api/predictions/leaderboard", &lb)
}

// ---------- Auction House ----------

// SubmitTaskWork submits work for an auction-style task (multi-worker).
func (c *AgentNetClient) SubmitTaskWork(id, result string) error {
	return c.post("/api/tasks/"+url.PathEscape(id)+"/work", map[string]interface{}{
		"result": result,
	}, nil)
}

// GetTaskSubmissions returns all submissions for an auction-style task.
func (c *AgentNetClient) GetTaskSubmissions(id string) ([]map[string]interface{}, error) {
	var subs []map[string]interface{}
	return subs, c.get("/api/tasks/"+url.PathEscape(id)+"/submissions", &subs)
}

// PickTaskWinner selects the winning submission for an auction-style task.
func (c *AgentNetClient) PickTaskWinner(id, winnerPeerID string) error {
	return c.post("/api/tasks/"+url.PathEscape(id)+"/pick", map[string]interface{}{
		"winner": winnerPeerID,
	}, nil)
}

// ---------- Overlay Mesh ----------

// GetOverlayStatus returns the overlay mesh network status.
func (c *AgentNetClient) GetOverlayStatus() (map[string]interface{}, error) {
	var status map[string]interface{}
	return status, c.get("/api/overlay/status", &status)
}

// GetOverlayTree returns the overlay peer tree.
func (c *AgentNetClient) GetOverlayTree() (map[string]interface{}, error) {
	var tree map[string]interface{}
	return tree, c.get("/api/overlay/tree", &tree)
}

// GetOverlayPeersGeo returns overlay peers with geographic info.
func (c *AgentNetClient) GetOverlayPeersGeo() ([]map[string]interface{}, error) {
	var peers []map[string]interface{}
	return peers, c.get("/api/overlay/peers/geo", &peers)
}

// AddOverlayPeer adds a custom overlay peer by URI.
func (c *AgentNetClient) AddOverlayPeer(uri string) error {
	return c.post("/api/overlay/peers/add", map[string]interface{}{
		"uri": uri,
	}, nil)
}

// ---------- Extended Diagnostics ----------

// GetMatrixStatus returns the matrix status diagnostics.
func (c *AgentNetClient) GetMatrixStatus() (map[string]interface{}, error) {
	var status map[string]interface{}
	return status, c.get("/api/matrix/status", &status)
}

// GetTraffic returns network traffic statistics.
func (c *AgentNetClient) GetTraffic() (map[string]interface{}, error) {
	var traffic map[string]interface{}
	return traffic, c.get("/api/traffic", &traffic)
}

// ---------------------------------------------------------------------------
// Hub-relayed task discovery
// ---------------------------------------------------------------------------

// PublishTasksToHub pushes local open tasks to the Hub task bulletin board
// so other peers can discover them.
func (c *AgentNetClient) PublishTasksToHub(hubURL string) error {
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
		// Skip the local tutorial task - it's not interesting to others.
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
	endpoint := strings.TrimRight(hubURL, "/") + "/api/AgentNet/tasks/publish"
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
func (c *AgentNetClient) BrowseHubTasks(hubURL string) ([]AgentNetTask, error) {
	if hubURL == "" {
		return nil, fmt.Errorf("hub URL is empty")
	}
	endpoint := strings.TrimRight(hubURL, "/") + "/api/AgentNet/tasks/browse?limit=50"
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

	var tasks []AgentNetTask
	for _, t := range result.Tasks {
		if localPeerID != "" && t.PeerID == localPeerID {
			continue // skip own tasks
		}
		tasks = append(tasks, AgentNetTask{
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

// ---------- P2P Service Gateway (skill.md Workflow F) ----------

// AgentNetServiceRegistration describes a local service to expose on the P2P network.
type AgentNetServiceRegistration struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Modes       []string `json:"modes,omitempty"`
	Billing     string   `json:"billing,omitempty"`
	Price       float64  `json:"price,omitempty"`
	FreeTier    int      `json:"free_tier,omitempty"`
}

// AgentNetServiceInfo describes a registered service.
type AgentNetServiceInfo struct {
	Name        string   `json:"name"`
	URL         string   `json:"url,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Modes       []string `json:"modes,omitempty"`
	Billing     string   `json:"billing,omitempty"`
	Price       float64  `json:"price,omitempty"`
	FreeTier    int      `json:"free_tier,omitempty"`
}

func (c *AgentNetClient) ListServices() ([]AgentNetServiceInfo, error) {
	var svcs []AgentNetServiceInfo
	return svcs, c.get("/api/svc", &svcs)
}

func (c *AgentNetClient) RegisterService(reg *AgentNetServiceRegistration) error {
	if err := validateAgentNetServiceRegistration(reg); err != nil {
		return err
	}
	return c.post("/api/svc/register", reg, nil)
}

func (c *AgentNetClient) UnregisterService(name string) error {
	name, err := normalizeAgentNetServiceName(name)
	if err != nil {
		return err
	}
	return c.post("/api/svc/unregister", map[string]interface{}{"name": name}, nil)
}

func (c *AgentNetClient) CallService(peer, service, method, path string, headers map[string]string, body string) (map[string]interface{}, error) {
	peer, service, method, path, err := normalizeAgentNetServiceCall(peer, service, method, path)
	if err != nil {
		return nil, err
	}
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

func (c *AgentNetClient) DiscoverServices(peer string) ([]AgentNetServiceInfo, error) {
	peer = strings.TrimSpace(peer)
	if peer == "" {
		return nil, fmt.Errorf("peer is required")
	}
	var svcs []AgentNetServiceInfo
	payload := map[string]interface{}{
		"peer":    peer,
		"service": "__discover__",
	}
	return svcs, c.post("/api/svc/call", payload, &svcs)
}

// ---------- ANS (Agent Name Service) ----------

type AgentNetANSEntry struct {
	Name string `json:"name"`
	DID  string `json:"did"`
	Tags string `json:"tags,omitempty"`
}

func (c *AgentNetClient) ANSRegister(name string, tags string) (*AgentNetANSEntry, error) {
	name, err := normalizeAgentNetANSName(name)
	if err != nil {
		return nil, err
	}
	var entry AgentNetANSEntry
	payload := map[string]interface{}{"name": name}
	if tags != "" {
		payload["tags"] = tags
	}
	return &entry, c.post("/api/ans/register?confirm=yes", payload, &entry)
}

func (c *AgentNetClient) ANSResolve(name string) (*AgentNetANSEntry, error) {
	name, err := normalizeAgentNetANSName(name)
	if err != nil {
		return nil, err
	}
	var entry AgentNetANSEntry
	return &entry, c.get("/api/ans/resolve?name="+url.QueryEscape(name), &entry)
}

func (c *AgentNetClient) ANSLookup(tags string, limit int) ([]AgentNetANSEntry, error) {
	params := url.Values{"tags": {tags}}
	if limit > 0 {
		params.Set("limit", fmt.Sprintf("%d", limit))
	}
	var entries []AgentNetANSEntry
	return entries, c.get("/api/ans/lookup?"+params.Encode(), &entries)
}

// ---------- Agent Discovery ----------

func (c *AgentNetClient) DiscoverAgents(query string) ([]map[string]interface{}, error) {
	var agents []map[string]interface{}
	return agents, c.get("/api/discover?q="+url.QueryEscape(query), &agents)
}

func (c *AgentNetClient) CrossDomainSearch(query string) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	return results, c.get("/api/search?q="+url.QueryEscape(query), &results)
}

func (c *AgentNetClient) FindAgent(query string) ([]AgentNetKnowledgeEntry, error) {
	var entries []AgentNetKnowledgeEntry
	return entries, c.postList("/api/knowledge/findclaw", map[string]interface{}{"query": query}, &entries, "entries", "graphs")
}

// ---------- Reputation ----------

type AgentNetReputation struct {
	DID   string  `json:"did"`
	Score float64 `json:"score"`
	Tier  string  `json:"tier,omitempty"`
}

func (c *AgentNetClient) GetReputation(did string) (*AgentNetReputation, error) {
	var rep AgentNetReputation
	return &rep, c.get("/api/reputation/"+url.PathEscape(did), &rep)
}

// ---------- Proof of Intelligence (PoI) ----------

func (c *AgentNetClient) ListPoIChallenges() ([]map[string]interface{}, error) {
	var challenges []map[string]interface{}
	return challenges, c.get("/api/poi/challenges", &challenges)
}

func (c *AgentNetClient) RespondToPoI(challengeID string, response map[string]interface{}) error {
	return c.post("/api/poi/challenges/"+url.PathEscape(challengeID)+"/respond", response, nil)
}

func (c *AgentNetClient) GetPoIScores() ([]map[string]interface{}, error) {
	var scores []map[string]interface{}
	return scores, c.get("/api/poi/scores", &scores)
}

// ---------- Agent Card & Init ----------

func (c *AgentNetClient) PublishAgentCard(name, desc string, skills []string) error {
	return c.post("/api/adp/publish", map[string]interface{}{
		"name": name, "desc": desc, "skills": skills,
	}, nil)
}

func (c *AgentNetClient) InitAgent(name string, skills []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("agent name is required")
	}
	cleanSkills := make([]string, 0, len(skills))
	seenSkills := make(map[string]bool)
	for _, skill := range skills {
		skill = strings.TrimSpace(strings.ToLower(skill))
		if skill == "" || seenSkills[skill] {
			continue
		}
		seenSkills[skill] = true
		cleanSkills = append(cleanSkills, skill)
	}
	if len(cleanSkills) == 0 {
		return fmt.Errorf("at least one skill is required")
	}
	return c.post("/api/init", map[string]interface{}{
		"name": name, "skills": cleanSkills,
	}, nil)
}

// ---------- Credits Transfer ----------

func (c *AgentNetClient) TransferCredits(toDID string, amount float64, reason string) error {
	toDID = strings.TrimSpace(toDID)
	if toDID == "" {
		return fmt.Errorf("credit recipient is required")
	}
	if amount <= 0 {
		return fmt.Errorf("credit transfer amount must be positive")
	}
	payload := map[string]interface{}{"to": toDID, "amount": amount}
	if reason != "" {
		payload["reason"] = reason
	}
	return c.post("/api/credits/transfer", payload, nil)
}

// ---------- Task Bundles ----------

func (c *AgentNetClient) AttachBundle(taskID string, bundleData []byte) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/tasks/"+url.PathEscape(taskID)+"/bundle", bytes.NewReader(bundleData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST /api/tasks/%s/bundle: %w", taskID, err)
	}
	defer resp.Body.Close()
	c.handleAuthError(resp)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST /api/tasks/%s/bundle: status %d: %s", taskID, resp.StatusCode, string(b))
	}
	return nil
}

func (c *AgentNetClient) DownloadBundle(taskID string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/tasks/"+url.PathEscape(taskID)+"/bundle", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /api/tasks/%s/bundle: %w", taskID, err)
	}
	defer resp.Body.Close()
	c.handleAuthError(resp)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /api/tasks/%s/bundle: status %d: %s", taskID, resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}

func (c *AgentNetClient) SubmitTaskDeliverable(task *AgentNetTask, result string) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	workDir, err := os.MkdirTemp("", "agentnet-deliverable-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	manifest := agentnet.BundleManifest{
		Intention:   firstNonEmpty(task.Title, "Complete AgentNetwork task"),
		Context:     firstNonEmpty(task.Description, "Task was claimed from AgentNetwork."),
		Constraints: "Follow the publisher task description and avoid exposing local secrets or private user data.",
		Harness:     "Publisher reviews the attached result and evidence summary.",
		Acceptance:  "The deliverable satisfies the requested task description.",
		Evidence:    "See result.md.",
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, "manifest.json"), manifestData, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, "result.md"), []byte(result), 0600); err != nil {
		return err
	}

	bundlePath := filepath.Join(workDir, "deliverable.nut")
	manager := agentnet.NewBundleManager(c.BinPath())
	if _, err := manager.Pack(workDir, bundlePath, ""); err != nil {
		return fmt.Errorf("pack deliverable: %w", err)
	}
	bundleData, err := os.ReadFile(bundlePath)
	if err != nil {
		return err
	}
	if err := c.AttachBundle(task.ID, bundleData); err != nil {
		return err
	}
	return c.SubmitTaskResult(task.ID, "Task completed. See attached .nut bundle for manifest.json and result.md.")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ---------- Split Tasks ----------

func (c *AgentNetClient) CreateSplitTask(title string, reward float64, slots int) (*AgentNetTask, error) {
	normalizedReward, err := normalizeAgentNetTaskReward(reward)
	if err != nil {
		return nil, err
	}
	var task AgentNetTask
	return &task, c.post("/api/tasks/split", map[string]interface{}{
		"title": title, "reward": normalizedReward, "slots": slots,
	}, &task)
}

// ---------- Disputes ----------

func (c *AgentNetClient) FileDispute(taskID, reason string) error {
	return c.post("/api/disputes", map[string]interface{}{
		"task_id": taskID, "reason": reason,
	}, nil)
}

// ---------- DAG & Ontology ----------

func (c *AgentNetClient) ExtractDAG(intent string, steps []string, outputs []string) ([]map[string]interface{}, error) {
	var nodes []map[string]interface{}
	return nodes, c.post("/api/dag/extract", map[string]interface{}{
		"intent": intent, "steps": steps, "outputs": outputs,
	}, &nodes)
}

func (c *AgentNetClient) QueryOntology(query string, depth int) (map[string]interface{}, error) {
	query, depth, err := normalizeAgentNetOntologyQuery(query, depth)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/api/ontology/subgraph?q=%s&depth=%d", url.QueryEscape(query), depth)
	var result map[string]interface{}
	return result, c.get(path, &result)
}
