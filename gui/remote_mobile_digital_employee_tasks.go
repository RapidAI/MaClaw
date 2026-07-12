package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Adaptive poll intervals for mobile digital-employee tasks.
// Idle desktops should not hammer Hub every few seconds; after work is found
// (or Hub pushes a task), poll faster so multi-step mobile flows feel snappy.
const (
	mobileDigitalEmployeeTaskPollIdle     = 12 * time.Second
	mobileDigitalEmployeeTaskPollActive   = 3 * time.Second
	mobileDigitalEmployeeTaskPollAfterHit = 45 * time.Second // stay "active" this long after a task/push
	mobileDigitalEmployeeHTTPTimeout      = 25 * time.Second
	mobileDigitalEmployeeMaxConcurrent    = 2 // claimed tasks run async; bound agent fan-out
	// Progress push cadence while the agent streams tokens to the phone.
	mobileDigitalEmployeeProgressMinInterval = 1200 * time.Millisecond
	mobileDigitalEmployeeProgressMinChars    = 64
	mobileDigitalEmployeeProgressMaxRunes    = 480
)

type mobileDigitalEmployeeTask struct {
	TaskID     string            `json:"task_id"`
	EmployeeID string            `json:"employee_id"`
	Prompt     string            `json:"prompt"`
	TaskType   string            `json:"task_type"`
	Context    map[string]string `json:"context"`
	Status     string            `json:"status"`
	Result     string            `json:"result"`
	ClaimedBy  string            `json:"claimed_by"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
}

type mobileDigitalEmployeeTaskClaimResponse struct {
	Status string                     `json:"status"`
	Task   *mobileDigitalEmployeeTask `json:"task"`
}

// Shared HTTP client for machine-auth task polling (timeout + connection reuse).
var mobileDigitalEmployeeHTTPClient = &http.Client{
	Timeout: mobileDigitalEmployeeHTTPTimeout,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// mobileTaskPollState tracks adaptive poll timing for one hub client.
type mobileTaskPollState struct {
	mu           sync.Mutex
	lastHitAt    time.Time
	bulkClaimOK  atomic.Bool // once bulk claim succeeds, skip expensive per-id fallbacks
	bulkClaimBad atomic.Bool // once bulk claim returns definitive 404, use per-id path
	// workers bounds concurrent processMobileDigitalEmployeeTask goroutines.
	// Poll claims only after tryReserveWorker succeeds (no claim-without-capacity).
	workers chan struct{}
	// kick wakes the poll loop immediately (Hub push or reconnect warm-up).
	kick chan struct{}
}

func (c *RemoteHubClient) ensureMobileTaskPollState() *mobileTaskPollState {
	if c == nil {
		return newMobileTaskPollState()
	}
	c.veHandlerMu.Lock()
	defer c.veHandlerMu.Unlock()
	if c.mobileTaskPollState == nil {
		c.mobileTaskPollState = newMobileTaskPollState()
	}
	return c.mobileTaskPollState
}

func newMobileTaskPollState() *mobileTaskPollState {
	return &mobileTaskPollState{
		workers: make(chan struct{}, mobileDigitalEmployeeMaxConcurrent),
		kick:    make(chan struct{}, 1),
	}
}

// requestImmediatePoll wakes the mobile task loop without waiting for the timer.
func (s *mobileTaskPollState) requestImmediatePoll() {
	if s == nil || s.kick == nil {
		return
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *mobileTaskPollState) noteHit() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.lastHitAt = time.Now()
	s.mu.Unlock()
}

func (s *mobileTaskPollState) interval() time.Duration {
	if s == nil {
		return mobileDigitalEmployeeTaskPollIdle
	}
	s.mu.Lock()
	last := s.lastHitAt
	s.mu.Unlock()
	if !last.IsZero() && time.Since(last) < mobileDigitalEmployeeTaskPollAfterHit {
		return mobileDigitalEmployeeTaskPollActive
	}
	return mobileDigitalEmployeeTaskPollIdle
}

// tryReserveWorker non-blocking-acquires a worker slot. Returns false when all
// slots are busy so the poll loop can leave tasks on Hub instead of claiming
// work it cannot start promptly.
func (s *mobileTaskPollState) tryReserveWorker() bool {
	if s == nil || s.workers == nil {
		return true
	}
	select {
	case s.workers <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *mobileTaskPollState) releaseWorker() {
	if s == nil || s.workers == nil {
		return
	}
	select {
	case <-s.workers:
	default:
	}
}

// runMobileDigitalEmployeeTaskAsync processes a claimed task without blocking
// the poll loop. reserved must already own a worker slot from tryReserveWorker.
func (c *RemoteHubClient) runMobileDigitalEmployeeTaskAsync(task mobileDigitalEmployeeTask, reserved bool) {
	poll := c.ensureMobileTaskPollState()
	poll.noteHit()
	go func() {
		if reserved && poll.workers != nil {
			defer poll.releaseWorker()
		} else if poll.workers != nil {
			// Fallback path: block for a slot (should not happen in normal poll).
			poll.workers <- struct{}{}
			defer poll.releaseWorker()
		}
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[hub-client] mobile digital employee task panic task=%s: %v", task.TaskID, r)
				_, _ = c.updateMobileDigitalEmployeeTask(task.TaskID, "failed", fmt.Sprintf("panic: %v", r))
			}
		}()
		c.processMobileDigitalEmployeeTask(task)
	}()
}

func (c *RemoteHubClient) mobileDigitalEmployeeTaskLoop() {
	if c == nil || c.app == nil {
		return
	}
	if !c.mobileTaskActive.CompareAndSwap(false, true) {
		return
	}
	defer c.mobileTaskActive.Store(false)

	poll := c.ensureMobileTaskPollState()
	// Warm start: poll aggressively right after connect so the first mobile
	// task after GUI online is claimed quickly even without a Hub push.
	poll.noteHit()

	c.publishMobileServerProfilesOnce()
	c.pollMobileDigitalEmployeeTasksOnce()
	c.pollMobileDocumentUploadTasksOnce()
	c.pollMobileBackendSSHSessionsOnce()
	c.pollMobileBackendSSHTasksOnce()
	c.pollMobileBackendSSHFileOperationsOnce()

	timer := time.NewTimer(poll.interval())
	defer timer.Stop()
	runPollTick := func() {
		if !c.IsConnected() {
			return
		}
		c.publishMobileServerProfilesOnce()
		c.pollMobileDocumentUploadTasksOnce()
		c.pollMobileBackendSSHSessionsOnce()
		c.pollMobileBackendSSHTasksOnce()
		c.pollMobileBackendSSHFileOperationsOnce()
		c.pollMobileDigitalEmployeeTasksOnce()
	}
	for {
		select {
		case <-timer.C:
			if !c.IsConnected() {
				return
			}
			runPollTick()
			timer.Reset(poll.interval())
		case <-poll.kick:
			if !c.IsConnected() {
				return
			}
			// Drain timer so we don't double-fire immediately after a kick.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			runPollTick()
			timer.Reset(poll.interval())
		}
	}
}

// handleMobileDigitalEmployeeTaskPush reacts to Hub WS push when a mobile user
// queues a digital-employee task. Switches to active poll cadence and claims ASAP.
func (c *RemoteHubClient) handleMobileDigitalEmployeeTaskPush(msg inboundHubEnvelope) {
	if c == nil {
		return
	}
	poll := c.ensureMobileTaskPollState()
	poll.noteHit()
	poll.requestImmediatePoll()
	// Also claim once immediately in case the poll loop is mid-tick or not started.
	go c.pollMobileDigitalEmployeeTasksOnce()
	log.Printf("[hub-client] mobile digital employee task push received type=%s", msg.Type)
}

func (c *RemoteHubClient) pollMobileDigitalEmployeeTasksOnce() {
	poll := c.ensureMobileTaskPollState()
	// Reserve capacity before claim so we never own a Hub task we cannot start.
	if !poll.tryReserveWorker() {
		return
	}
	reserved := true
	releaseIfUnused := func() {
		if reserved {
			poll.releaseWorker()
			reserved = false
		}
	}
	defer releaseIfUnused()

	// Prefer bulk claim when available: Hub matches machine-hosted VEs + personal aliases.
	if !poll.bulkClaimBad.Load() {
		claim, err := c.claimAnyMobileDigitalEmployeeTask()
		if err != nil {
			// Mark bulk claim as unavailable only on definitive 404 so we stop
			// probing a missing endpoint forever on older hubs.
			if isHTTPNotFoundError(err) {
				poll.bulkClaimBad.Store(true)
			} else {
				log.Printf("[hub-client] mobile digital employee bulk claim failed: %v", err)
			}
		} else {
			poll.bulkClaimOK.Store(true)
			if claim != nil && claim.Task != nil && strings.TrimSpace(claim.Task.TaskID) != "" {
				reserved = false // ownership transferred to task goroutine
				c.runMobileDigitalEmployeeTaskAsync(*claim.Task, true)
				return
			}
			// Bulk endpoint works and returned empty — skip per-id fallback this tick.
			return
		}
	}

	// Fallback: per-id claim for older Hubs without bulk endpoint.
	// Skip when bulk claim is known-good to avoid N extra HTTP round-trips every poll.
	if poll.bulkClaimOK.Load() {
		return
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return
	}
	for _, employeeID := range c.mobileDigitalEmployeeClaimCandidateIDs(cfg) {
		claim, err := c.claimMobileDigitalEmployeeTask(employeeID)
		if err != nil {
			log.Printf("[hub-client] mobile digital employee claim failed employee=%s: %v", employeeID, err)
			continue
		}
		if claim == nil || claim.Task == nil || strings.TrimSpace(claim.Task.TaskID) == "" {
			continue
		}
		reserved = false
		c.runMobileDigitalEmployeeTaskAsync(*claim.Task, true)
		return
	}
}

func isHTTPNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 404") || strings.Contains(msg, "status 404")
}

func mobileDigitalEmployeeCandidateIDs(machineID, clientID string, extra ...string) []string {
	out := make([]string, 0, 4+len(extra))
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if !strings.HasPrefix(strings.ToLower(value), "ve_") {
			ve := "ve_" + value
			if _, ok := seen[strings.ToLower(ve)]; !ok {
				seen[strings.ToLower(ve)] = struct{}{}
				out = append(out, ve)
			}
		}
	}
	add(machineID)
	add(clientID)
	for _, id := range extra {
		add(id)
	}
	return out
}

// mobileDigitalEmployeeClaimCandidateIDs includes machine aliases plus VEs hosted
// on this desktop (from local discoverable/status cache when available).
func (c *RemoteHubClient) mobileDigitalEmployeeClaimCandidateIDs(cfg corelib.AppConfig) []string {
	extra := make([]string, 0, 8)
	if c != nil && c.app != nil {
		// Best-effort: own local VE registration id.
		if st, err := c.app.GetVEStatus(); err == nil && st != nil && st.Employee != nil {
			extra = append(extra, st.Employee.ID, st.Employee.MachineID)
		}
		// Discoverable list filtered to this machine when hub is reachable.
		if employees, err := c.app.ListVirtualEmployees(); err == nil {
			local := strings.TrimSpace(cfg.RemoteMachineID)
			for _, emp := range employees {
				if local != "" &&
					!veGroupParticipantIdentityMatches(emp.MachineID, local) &&
					!veGroupParticipantIdentityMatches(emp.ID, local) &&
					!veGroupParticipantIdentityMatches(emp.ID, virtualEmployeeIDForMachine(local)) {
					// Not hosted here — skip unless identity is empty machine (platform-only).
					if strings.TrimSpace(emp.MachineID) != "" {
						continue
					}
				}
				extra = append(extra, emp.ID, emp.MachineID)
			}
		}
	}
	return mobileDigitalEmployeeCandidateIDs(cfg.RemoteMachineID, cfg.RemoteClientID, extra...)
}

func (c *RemoteHubClient) claimAnyMobileDigitalEmployeeTask() (*mobileDigitalEmployeeTaskClaimResponse, error) {
	var out mobileDigitalEmployeeTaskClaimResponse
	path := "/api/mobile/digital-employees/tasks/claim"
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) claimMobileDigitalEmployeeTask(employeeID string) (*mobileDigitalEmployeeTaskClaimResponse, error) {
	var out mobileDigitalEmployeeTaskClaimResponse
	path := "/api/mobile/digital-employees/" + url.PathEscape(strings.TrimSpace(employeeID)) + "/tasks/claim"
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) updateMobileDigitalEmployeeTask(taskID, status, result string) (*mobileDigitalEmployeeTask, error) {
	payload := map[string]string{
		"status": strings.TrimSpace(status),
		"result": strings.TrimSpace(result),
	}
	var out mobileDigitalEmployeeTask
	path := "/api/mobile/digital-employees/tasks/" + url.PathEscape(strings.TrimSpace(taskID))
	if err := c.doMobileDigitalEmployeeTaskRequest(context.Background(), http.MethodPatch, path, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *RemoteHubClient) doMobileDigitalEmployeeTaskRequest(ctx context.Context, method, path string, payload any, out any) error {
	if c == nil || c.app == nil {
		return fmt.Errorf("remote hub client is not initialized")
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return err
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	token := strings.TrimSpace(cfg.RemoteMachineToken)
	if base == "" || machineID == "" || token == "" {
		return fmt.Errorf("remote hub machine identity is incomplete")
	}

	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Bound request lifetime via context. Client.Timeout is a second backstop;
	// prefer the caller's ctx deadline when present and shorter.
	reqCtx := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		reqCtx, cancel = context.WithTimeout(ctx, mobileDigitalEmployeeHTTPTimeout)
	}
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := mobileDigitalEmployeeHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub returned HTTP %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *RemoteHubClient) processMobileDigitalEmployeeTask(task mobileDigitalEmployeeTask) {
	taskID := strings.TrimSpace(task.TaskID)
	prompt := strings.TrimSpace(task.Prompt)
	if taskID == "" || prompt == "" {
		return
	}
	_, _ = c.updateMobileDigitalEmployeeTask(taskID, "in_progress", "远程数字员工正在处理手机任务。")

	handler := c.digitalEmployeeMessageHandler()
	if handler == nil {
		_, _ = c.updateMobileDigitalEmployeeTask(taskID, "failed", "远程数字员工不可用。")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sessionID := "mobile-digital-employee:" + taskID

	// Stream partial agent text to Hub as throttled in_progress patches so the
	// phone realtime channel shows live progress without per-token HTTP spam.
	// A generation counter drops superseded progress writes once we finalize.
	var (
		progressMu    sync.Mutex
		progressBuf   strings.Builder
		lastProgress  time.Time
		progressSeq   atomic.Uint64
		firstProgress atomic.Bool
	)
	flushProgress := func() {
		progressMu.Lock()
		content := strings.TrimSpace(progressBuf.String())
		now := time.Now()
		if content == "" {
			progressMu.Unlock()
			return
		}
		// First visible chunk: push ASAP so the phone leaves a static "processing" state.
		// Later chunks: require min interval or min size to limit Hub PATCH rate.
		if firstProgress.Load() {
			elapsed := now.Sub(lastProgress)
			if elapsed < mobileDigitalEmployeeProgressMinInterval &&
				utf8.RuneCountInString(content) < mobileDigitalEmployeeProgressMinChars {
				progressMu.Unlock()
				return
			}
		}
		lastProgress = now
		progressMu.Unlock()
		firstProgress.Store(true)

		n := progressSeq.Add(1)
		clipped := clipRunesForMobileProgress(content, mobileDigitalEmployeeProgressMaxRunes)
		go func(seq uint64, text string) {
			if progressSeq.Load() != seq {
				return
			}
			_, _ = c.updateMobileDigitalEmployeeTask(taskID, "in_progress", text)
		}(n, clipped)
	}

	onToken := func(delta string) {
		if delta == "" {
			return
		}
		progressMu.Lock()
		progressBuf.WriteString(delta)
		progressMu.Unlock()
		flushProgress()
	}

	result, err := handler.runAgentForVE(ctx, sessionID, buildMobileDigitalEmployeeExecutionPrompt(task), taskID, onToken)
	// Invalidate in-flight progress PATCHes so they cannot race past terminal status.
	progressSeq.Add(1)
	if err != nil {
		_, _ = c.updateMobileDigitalEmployeeTask(taskID, "failed", err.Error())
		return
	}
	result = strings.TrimSpace(result)
	if result == "" {
		result = "任务已完成，但没有生成可展示结果。"
	}
	_, _ = c.updateMobileDigitalEmployeeTask(taskID, "done", result)
}

// clipRunesForMobileProgress keeps the tail of streaming text so the phone UI
// shows the newest generated content within a fixed rune budget.
func clipRunesForMobileProgress(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || text == "" {
		return text
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return "…" + string(runes[len(runes)-maxRunes:])
}

func buildMobileDigitalEmployeeExecutionPrompt(task mobileDigitalEmployeeTask) string {
	prompt := strings.TrimSpace(task.Prompt)
	taskType := strings.TrimSpace(task.TaskType)
	if taskType == "" {
		taskType = "general"
	}
	var b strings.Builder
	b.WriteString("[MaClaw Mobile emergency task]\n")
	b.WriteString("Task type: ")
	b.WriteString(taskType)
	b.WriteString("\n")
	if len(task.Context) > 0 {
		b.WriteString("Context:\n")
		keys := make([]string, 0, len(task.Context))
		contextValues := make(map[string]string, len(task.Context))
		for rawKey, rawValue := range task.Context {
			key := strings.TrimSpace(rawKey)
			value := strings.TrimSpace(rawValue)
			if key == "" || value == "" {
				continue
			}
			contextValues[key] = value
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := contextValues[key]
			b.WriteString("- ")
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(value)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nUser request:\n")
	b.WriteString(prompt)
	b.WriteString("\n\nMobile response requirements:\n")
	b.WriteString("- Start with a concise conclusion suitable for phone reading.\n")
	b.WriteString("- Include evidence, impact, and next steps.\n")
	b.WriteString("- For high-risk server or desktop operations, provide command drafts and ask for manual confirmation instead of executing automatically.\n")
	return strings.TrimSpace(b.String())
}
