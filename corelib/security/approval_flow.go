package security

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ApprovalMode defines how user approval is collected.
type ApprovalMode string

const (
	// ApprovalSync is the default mode: blocks until user responds (CLI/GUI).
	ApprovalSync ApprovalMode = "sync"
	// ApprovalAsync is for IM scenarios: sends a message and waits for reply.
	ApprovalAsync ApprovalMode = "async"
)

// ApprovalRequest represents a pending approval request sent to the user.
type ApprovalRequest struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"session_id"`
	ToolName   string         `json:"tool_name"`
	Args       map[string]interface{} `json:"args"`
	Risk       RiskAssessment `json:"risk"`
	Categories []string       `json:"categories"`
	CreatedAt  time.Time      `json:"created_at"`
	ExpiresAt  time.Time      `json:"expires_at"`
}

// ApprovalResponse represents the user's response to an approval request.
type ApprovalResponse struct {
	RequestID    string `json:"request_id"`
	Approved     bool   `json:"approved"`
	ApproveScope string `json:"approve_scope"` // "once", "tool", "category", "session"
}

// ApprovalSender sends approval requests to users via IM or other channels.
type ApprovalSender interface {
	// SendApprovalRequest sends an approval request and returns a request ID.
	SendApprovalRequest(req ApprovalRequest) (string, error)
}

// ApprovalManager manages pending approval requests for async (IM) scenarios.
type ApprovalManager struct {
	mu       sync.Mutex
	pending  map[string]*pendingApproval
	sender   ApprovalSender
	timeout  time.Duration
}

type pendingApproval struct {
	request  ApprovalRequest
	resultCh chan ApprovalResponse
}

// NewApprovalManager creates an ApprovalManager.
// If timeout is 0, defaults to 2 minutes.
func NewApprovalManager(sender ApprovalSender, timeout time.Duration) *ApprovalManager {
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	return &ApprovalManager{
		pending: make(map[string]*pendingApproval),
		sender:  sender,
		timeout: timeout,
	}
}

// RequestApproval sends an approval request via the configured sender and
// blocks until the user responds or the timeout expires.
func (am *ApprovalManager) RequestApproval(req ApprovalRequest) (ApprovalResponse, error) {
	if am.sender == nil {
		return ApprovalResponse{}, fmt.Errorf("no approval sender configured")
	}

	req.CreatedAt = time.Now()
	req.ExpiresAt = time.Now().Add(am.timeout)

	reqID, err := am.sender.SendApprovalRequest(req)
	if err != nil {
		return ApprovalResponse{}, fmt.Errorf("failed to send approval request: %w", err)
	}
	req.ID = reqID

	pa := &pendingApproval{
		request:  req,
		resultCh: make(chan ApprovalResponse, 1),
	}

	am.mu.Lock()
	am.pending[reqID] = pa
	am.mu.Unlock()

	defer func() {
		am.mu.Lock()
		delete(am.pending, reqID)
		am.mu.Unlock()
	}()

	select {
	case resp := <-pa.resultCh:
		return resp, nil
	case <-time.After(am.timeout):
		return ApprovalResponse{
			RequestID: reqID,
			Approved:  false,
		}, fmt.Errorf("approval request timed out after %v", am.timeout)
	}
}

// HandleResponse processes a user's response to a pending approval request.
// Called when the IM gateway receives a reply from the user.
func (am *ApprovalManager) HandleResponse(resp ApprovalResponse) bool {
	am.mu.Lock()
	pa, ok := am.pending[resp.RequestID]
	am.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case pa.resultCh <- resp:
		return true
	default:
		return false
	}
}

// PendingCount returns the number of pending approval requests.
func (am *ApprovalManager) PendingCount() int {
	am.mu.Lock()
	defer am.mu.Unlock()
	return len(am.pending)
}

// CancelAll cancels all pending approval requests.
func (am *ApprovalManager) CancelAll() {
	am.mu.Lock()
	defer am.mu.Unlock()
	for id, pa := range am.pending {
		select {
		case pa.resultCh <- ApprovalResponse{RequestID: id, Approved: false}:
		default:
		}
	}
	am.pending = make(map[string]*pendingApproval)
}

// FormatApprovalMessage generates a human-readable approval request message
// suitable for IM channels (Feishu, QQ, etc.).
func FormatApprovalMessage(req ApprovalRequest) string {
	msg := fmt.Sprintf("安全审批请求\n\n"+
		"工具: %s\n"+
		"风险等级: %s\n"+
		"原因: %s\n",
		req.ToolName, req.Risk.Level, req.Risk.Reason)

	if cmd, ok := req.Args["command"]; ok {
		msg += fmt.Sprintf("命令: %v\n", cmd)
	}

	msg += fmt.Sprintf("\n请回复:\n"+
		"  批准 - 允许本次执行\n"+
		"  批准同类 - 允许本会话内同类操作\n"+
		"  拒绝 - 阻止执行\n"+
		"\n超时时间: %v", req.ExpiresAt.Sub(req.CreatedAt))

	return msg
}

// approvalBareNegativeWords are short latin replies that must match as a whole
// word. Matching them as plain substrings would reject benign replies such as
// "deploy node" or "not sure", which merely contain "no".
var approvalBareNegativeWords = []string{"no", "nope", "nah"}

// ParseApprovalReply parses a user's IM reply into an ApprovalResponse.
//
// Matching order matters: scope keywords are checked first because they also
// contain the plain approval keyword ("批准同类" contains "批准"); rejection is
// checked before approval because "不允许" contains "允许".
//
// The parse is fail-closed: anything that is not recognised as approval
// (including empty and whitespace-only replies) is treated as a rejection.
func ParseApprovalReply(requestID, reply string) ApprovalResponse {
	resp := ApprovalResponse{RequestID: requestID, ApproveScope: "once"}

	normalized := strings.ToLower(strings.TrimSpace(reply))
	if normalized == "" {
		return resp
	}

	switch {
	case containsAny(normalized, "批准同类", "approve category", "approve similar"):
		resp.Approved = true
		resp.ApproveScope = "category"
	case containsAny(normalized, "批准全部", "approve all", "全部允许"):
		resp.Approved = true
		resp.ApproveScope = "session"
	case containsAny(normalized, "拒绝", "reject", "deny", "不允许"),
		containsWord(normalized, approvalBareNegativeWords...):
		resp.Approved = false
	case containsAny(normalized, "批准", "approve", "允许", "yes"),
		containsWord(normalized, "y", "ok", "okay"):
		resp.Approved = true
	}

	return resp
}

func containsAny(s string, substrs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range substrs {
		if sub == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// containsWord reports whether any of words appears in s as a whole word.
// s is expected to be lower-cased ASCII; non-ASCII bytes (e.g. CJK) count as
// word boundaries, so "批准no" still matches "no".
func containsWord(s string, words ...string) bool {
	for _, word := range words {
		if word == "" {
			continue
		}
		re, err := regexp.Compile(`(^|[^a-z0-9_])` + regexp.QuoteMeta(word) + `([^a-z0-9_]|$)`)
		if err != nil {
			continue
		}
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
