package desktoppool

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

var (
	// ErrComputerResetDisabled is a reset the tenant administrator turned off.
	ErrComputerResetDisabled = errors.New("desktop reset is disabled by the tenant administrator")
	// ErrComputerBusy is desktopd's 409: a bot of this user is working.
	ErrComputerBusy = errors.New("a bot is working on this desktop")
	// ErrComputerMissing is desktopd's 404: the user has no desktop yet.
	ErrComputerMissing = errors.New("the desktop has not been created yet")
)

// ComputerPolicy is the tenant's control over the user-facing computer actions.
type ComputerPolicy struct {
	ResetDisabled bool `json:"reset_disabled"`
}

// ComputerResult is one restart or reset as desktopd reported it.
type ComputerResult struct {
	Action    string `json:"action"`
	Container string `json:"container"`
	Image     string `json:"image,omitempty"`
	Status    string `json:"status"`
	Backup    string `json:"backup,omitempty"`
}

// computerOpTimeout covers the login write-back (stop grace), an image pull
// for reset, and the supervisor coming back.
const computerOpTimeout = 10 * time.Minute

func (p *Pool) TenantComputerPolicy(ctx context.Context, tenantID string) (ComputerPolicy, error) {
	p.mu.Lock()
	rec, err := p.load(ctx, tenantID)
	p.mu.Unlock()
	if err != nil {
		return ComputerPolicy{}, err
	}
	return ComputerPolicy{ResetDisabled: rec.ComputerResetDisabled}, nil
}

func (p *Pool) SetTenantComputerPolicy(ctx context.Context, tenantID string, in ComputerPolicy) (ComputerPolicy, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, err := p.load(ctx, tenantID)
	if err != nil {
		return ComputerPolicy{}, err
	}
	rec.ComputerResetDisabled = in.ResetDisabled
	if err := p.save(ctx, tenantID, rec); err != nil {
		return ComputerPolicy{}, err
	}
	return ComputerPolicy{ResetDisabled: rec.ComputerResetDisabled}, nil
}

// RestartDesktop restarts the user's desktop container (docker restart
// semantics, logins written back first). force skips the busy-bot refusal.
func (p *Pool) RestartDesktop(ctx context.Context, tenantID, userID string, force bool) (ComputerResult, error) {
	return p.computerOp(ctx, tenantID, userID, "/v1/desktops/restart", force)
}

// ResetDesktop recreates the user's desktop from the assigned image, keeping
// every private volume. Refused when the tenant disabled it.
func (p *Pool) ResetDesktop(ctx context.Context, tenantID, userID string, force bool) (ComputerResult, error) {
	policy, err := p.TenantComputerPolicy(ctx, tenantID)
	if err != nil {
		return ComputerResult{}, err
	}
	if policy.ResetDisabled {
		return ComputerResult{}, ErrComputerResetDisabled
	}
	return p.computerOp(ctx, tenantID, userID, "/v1/desktops/reset", force)
}

func (p *Pool) computerOp(ctx context.Context, tenantID, userID, path string, force bool) (ComputerResult, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ComputerResult{}, ErrInvalidInput
	}
	server, err := p.resolve(ctx, tenantID, userID)
	if err != nil {
		return ComputerResult{}, err
	}
	body := map[string]any{
		"tenant_id": store.NormalizeTenantID(tenantID),
		"user_id":   userID,
		"image":     server.Image,
		"memory":    server.Memory,
		"cpus":      server.CPUs,
		"shm_size":  server.ShmSize,
		"force":     force,
	}
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: computerOpTimeout}
	}
	var out ComputerResult
	if err := p.callClient(ctx, client, server, http.MethodPost, path, body, &out); err != nil {
		var statusErr *upstream.StatusError
		if errors.As(err, &statusErr) {
			switch statusErr.Status {
			case http.StatusConflict:
				return ComputerResult{}, ErrComputerBusy
			case http.StatusNotFound:
				// An older desktopd without the endpoint answers a plain
				// "404 page not found"; that is a service problem.
				if strings.Contains(statusErr.Detail, "does not exist") {
					return ComputerResult{}, ErrComputerMissing
				}
			}
		}
		return ComputerResult{}, err
	}
	return out, nil
}
