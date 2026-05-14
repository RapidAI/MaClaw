package ve

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

const (
	AuthRequestTimeout = 60 * time.Second
)

// AuthorizationRequest represents a pending per-request authorization.
type AuthorizationRequest struct {
	ID                 string    `json:"id"`
	RequesterName      string    `json:"requester_name"`
	RequesterMachineID string    `json:"requester_machine_id"`
	TargetVEID         string    `json:"target_ve_id"`
	TargetVEName       string    `json:"target_ve_name"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}

// AuthorizationResponse is the owner's decision on an auth request.
type AuthorizationResponse struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"` // "allow" or "deny"
}

// AuthResult is returned to the requester after authorization completes.
type AuthResult struct {
	Allowed bool
	Reason  string // "approved", "denied", "timeout"
}

// pendingAuth tracks a single pending authorization with its result channel.
type pendingAuth struct {
	Request  AuthorizationRequest
	ResultCh chan AuthResult
}

// AuthHandler manages per-request authorization flows.
type AuthHandler struct {
	mu       sync.Mutex
	pending  map[string]*pendingAuth // key: request ID
	onPush   func(ownerMachineID string, req AuthorizationRequest) // push to VE owner
	onResult func(requesterMachineID string, result AuthResult)    // notify requester
}

// NewAuthHandler creates a new authorization handler.
func NewAuthHandler() *AuthHandler {
	return &AuthHandler{
		pending: make(map[string]*pendingAuth),
	}
}

// SetOnPush sets the callback for pushing auth requests to VE owners.
func (h *AuthHandler) SetOnPush(fn func(ownerMachineID string, req AuthorizationRequest)) {
	h.mu.Lock()
	h.onPush = fn
	h.mu.Unlock()
}

// SetOnResult sets the callback for notifying requesters of auth results.
func (h *AuthHandler) SetOnResult(fn func(requesterMachineID string, result AuthResult)) {
	h.mu.Lock()
	h.onResult = fn
	h.mu.Unlock()
}

// InitiateAuth starts a per-request authorization flow.
// It sends the auth request to the VE owner and waits for a response or timeout.
// This is a blocking call — use in a goroutine if needed.
func (h *AuthHandler) InitiateAuth(ctx context.Context, requesterName, requesterMachineID string, ve *VirtualEmployee) AuthResult {
	now := time.Now().UTC()
	req := AuthorizationRequest{
		ID:                 idgen.New("auth"),
		RequesterName:      requesterName,
		RequesterMachineID: requesterMachineID,
		TargetVEID:         ve.ID,
		TargetVEName:       ve.Name,
		CreatedAt:          now,
		ExpiresAt:          now.Add(AuthRequestTimeout),
	}

	resultCh := make(chan AuthResult, 1)
	pa := &pendingAuth{Request: req, ResultCh: resultCh}

	h.mu.Lock()
	h.pending[req.ID] = pa
	pushFn := h.onPush
	h.mu.Unlock()

	// Push to VE owner
	if pushFn != nil {
		pushFn(ve.OwnerMachineID, req)
	}

	// Start timeout goroutine
	timeoutCtx, cancel := context.WithTimeout(ctx, AuthRequestTimeout)
	defer cancel()

	select {
	case result := <-resultCh:
		return result
	case <-timeoutCtx.Done():
		// Timeout or context cancelled
		h.mu.Lock()
		delete(h.pending, req.ID)
		h.mu.Unlock()

		if ctx.Err() != nil {
			return AuthResult{Allowed: false, Reason: "cancelled"}
		}
		return AuthResult{Allowed: false, Reason: "timeout"}
	}
}

// HandleResponse processes the VE owner's authorization decision.
func (h *AuthHandler) HandleResponse(resp AuthorizationResponse) error {
	if resp.Decision != "allow" && resp.Decision != "deny" {
		return fmt.Errorf("invalid decision: %q (expected allow or deny)", resp.Decision)
	}

	h.mu.Lock()
	pa, ok := h.pending[resp.RequestID]
	if ok {
		delete(h.pending, resp.RequestID)
	}
	h.mu.Unlock()

	if !ok {
		return fmt.Errorf("authorization request %q not found or already expired", resp.RequestID)
	}

	var result AuthResult
	if resp.Decision == "allow" {
		result = AuthResult{Allowed: true, Reason: "approved"}
	} else {
		result = AuthResult{Allowed: false, Reason: "denied"}
	}

	// Send result to waiting goroutine (non-blocking)
	select {
	case pa.ResultCh <- result:
	default:
		log.Printf("[ve-auth] WARNING: result channel full for request %s", resp.RequestID)
	}

	return nil
}

// PendingCount returns the number of pending authorization requests.
func (h *AuthHandler) PendingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending)
}

// PendingForOwner returns all pending requests targeting VEs owned by the given machine.
func (h *AuthHandler) PendingForOwner(ownerMachineID string, registry *Registry) []AuthorizationRequest {
	h.mu.Lock()
	defer h.mu.Unlock()

	var result []AuthorizationRequest
	for _, pa := range h.pending {
		ve, ok := registry.GetByID(pa.Request.TargetVEID)
		if ok && ve.OwnerMachineID == ownerMachineID {
			result = append(result, pa.Request)
		}
	}
	return result
}

// CleanupExpired removes expired pending requests. Called periodically.
func (h *AuthHandler) CleanupExpired() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	cleaned := 0
	for id, pa := range h.pending {
		if now.After(pa.Request.ExpiresAt) {
			// Send timeout result
			select {
			case pa.ResultCh <- AuthResult{Allowed: false, Reason: "timeout"}:
			default:
			}
			delete(h.pending, id)
			cleaned++
		}
	}
	return cleaned
}

// StartCleanupLoop starts a background goroutine that periodically cleans up expired requests.
func (h *AuthHandler) StartCleanupLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n := h.CleanupExpired(); n > 0 {
					log.Printf("[ve-auth] cleaned %d expired authorization requests", n)
				}
			}
		}
	}()
}

// ErrAuthTimeout is returned when authorization times out.
var ErrAuthTimeout = errors.New("authorization request timed out")
