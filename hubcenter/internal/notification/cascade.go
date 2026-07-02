package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// CascadeRequest is the payload sent to a Hub's /api/v1/notifications/cascade endpoint.
type CascadeRequest struct {
	Notification *Notification `json:"notification"`
}

// CascadeRevokeRequest is the payload sent to revoke a notification via cascade.
type CascadeRevokeRequest struct {
	NotificationID string `json:"notification_id"`
	Action         string `json:"action"` // "revoke"
}

// CascadeService handles HTTP POST cascade pushes to target Hub instances.
type CascadeService struct {
	httpClient *http.Client
	store      Store
}

// NewCascadeService creates a CascadeService with a 30-second timeout HTTP client.
func NewCascadeService(store Store) *CascadeService {
	return &CascadeService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		store: store,
	}
}

// DispatchToHubs concurrently pushes a notification to all target Hub instances.
// Each Hub's push result is recorded via store.RecordCascadeResult.
// Concurrency is bounded to 10 goroutines to avoid resource exhaustion.
func (c *CascadeService) DispatchToHubs(ctx context.Context, notif *Notification, targetHubs []HubEndpoint) error {
	if len(targetHubs) == 0 {
		return nil
	}

	const maxConcurrency = 10
	sem := make(chan struct{}, maxConcurrency)

	var wg sync.WaitGroup
	for _, hub := range targetHubs {
		wg.Add(1)
		sem <- struct{}{} // acquire semaphore
		go func(h HubEndpoint) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore
			err := c.pushToHub(ctx, h, notif)
			now := time.Now().UTC()
			result := &CascadeResult{
				NotificationID: notif.ID,
				HubID:          h.ID,
				Status:         classifyCascadeStatus(err),
				PushedAt:       &now,
			}
			if err != nil {
				result.ErrorMessage = err.Error()
				log.Printf("[cascade] push to hub %s (%s) failed: status=%s err=%v", h.ID, h.URL, result.Status, err)
			}
			if c.store != nil {
				_ = c.store.RecordCascadeResult(ctx, result)
			}
		}(hub)
	}
	wg.Wait()
	return nil
}

// DispatchRevoke concurrently sends a revoke message to all target Hub instances.
// Concurrency is bounded to 10 goroutines to avoid resource exhaustion.
func (c *CascadeService) DispatchRevoke(ctx context.Context, notifID string, targetHubs []HubEndpoint) error {
	if len(targetHubs) == 0 {
		return nil
	}

	const maxConcurrency = 10
	sem := make(chan struct{}, maxConcurrency)

	var wg sync.WaitGroup
	for _, hub := range targetHubs {
		wg.Add(1)
		sem <- struct{}{} // acquire semaphore
		go func(h HubEndpoint) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore
			err := c.pushRevokeToHub(ctx, h, notifID)
			now := time.Now().UTC()
			result := &CascadeResult{
				NotificationID: notifID,
				HubID:          h.ID,
				Status:         classifyCascadeStatus(err),
				PushedAt:       &now,
			}
			if err != nil {
				result.ErrorMessage = err.Error()
				log.Printf("[cascade] revoke to hub %s (%s) failed: status=%s err=%v", h.ID, h.URL, result.Status, err)
			}
			if c.store != nil {
				_ = c.store.RecordCascadeResult(ctx, result)
			}
		}(hub)
	}
	wg.Wait()
	return nil
}

// pushToHub sends a single notification to one Hub via HTTP POST.
func (c *CascadeService) pushToHub(ctx context.Context, hub HubEndpoint, notif *Notification) error {
	payload := CascadeRequest{Notification: notif}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal cascade request: %w", err)
	}

	endpoint := trimTrailingSlash(hub.URL) + "/api/v1/notifications/cascade"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+hub.GlobalAdminToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &CascadeError{StatusCode: 0, Category: "timeout", Cause: err}
		}
		return &CascadeError{StatusCode: 0, Category: "error", Cause: err}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return classifyHTTPError(resp.StatusCode)
}

// pushRevokeToHub sends a revoke request to one Hub via HTTP POST.
func (c *CascadeService) pushRevokeToHub(ctx context.Context, hub HubEndpoint, notifID string) error {
	payload := CascadeRevokeRequest{
		NotificationID: notifID,
		Action:         "revoke",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal revoke request: %w", err)
	}

	endpoint := trimTrailingSlash(hub.URL) + "/api/v1/notifications/cascade"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+hub.GlobalAdminToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return &CascadeError{StatusCode: 0, Category: "timeout", Cause: err}
		}
		return &CascadeError{StatusCode: 0, Category: "error", Cause: err}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return classifyHTTPError(resp.StatusCode)
}

// CascadeError represents a classified cascade push failure.
type CascadeError struct {
	StatusCode int
	Category   string // "auth_failed", "server_error", "timeout", "error"
	Cause      error
}

func (e *CascadeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("cascade %s (HTTP %d): %v", e.Category, e.StatusCode, e.Cause)
	}
	return fmt.Sprintf("cascade %s (HTTP %d)", e.Category, e.StatusCode)
}

func (e *CascadeError) Unwrap() error {
	return e.Cause
}

// classifyHTTPError maps an HTTP status code to a CascadeError.
func classifyHTTPError(statusCode int) *CascadeError {
	switch {
	case statusCode == 401 || statusCode == 403:
		return &CascadeError{StatusCode: statusCode, Category: "auth_failed"}
	case statusCode >= 500:
		return &CascadeError{StatusCode: statusCode, Category: "server_error"}
	default:
		return &CascadeError{StatusCode: statusCode, Category: "error"}
	}
}

// classifyCascadeStatus determines the CascadeStatus from a push error.
func classifyCascadeStatus(err error) CascadeStatus {
	if err == nil {
		return CascadeStatusSuccess
	}
	if ce, ok := err.(*CascadeError); ok {
		switch ce.Category {
		case "auth_failed":
			return CascadeStatusAuthFailed
		case "timeout":
			return CascadeStatusTimeout
		case "server_error":
			return CascadeStatusFailed
		}
	}
	return CascadeStatusFailed
}

// trimTrailingSlash removes trailing slashes from a URL.
func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
