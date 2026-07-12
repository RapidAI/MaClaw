package im

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"time"
)

// ---------------------------------------------------------------------------
// Audit types and constants
// ---------------------------------------------------------------------------

// AuditAction represents the action to take after content audit.
type AuditAction int

const (
	AuditPass         AuditAction = iota // 0: allow content through
	AuditBlock                           // 1: block content
	AuditDelay                           // 2: delay delivery, poll later
	AuditSanitize                        // 3: replace with sanitized content
	AuditManualReview                    // 4: hold for manual review
	AuditError                           // 5: audit program error
)

// AuditRequest is the JSON structure written to the audit program's stdin.
type AuditRequest struct {
	Type     string   `json:"type"`    // "text", "image", "file"
	Content  string   `json:"content"` // text content or base64 data
	UserID   string   `json:"user_id"`
	Platform string   `json:"platform"`
	Keywords []string `json:"keywords,omitempty"`
}

// AuditResponse is the JSON structure read from the audit program's stdout.
type AuditResponse struct {
	Code             int    `json:"code"`
	Message          string `json:"message,omitempty"`
	SanitizedContent string `json:"sanitized_content,omitempty"`
}

// AuditResult is the result returned by ContentAuditor to the caller.
type AuditResult struct {
	Action   AuditAction
	Response *GenericResponse // replacement response (for block/sanitize)
	Message  string           // message from audit program
}

// AuditLogEntry represents a single audit log record.
type AuditLogEntry struct {
	ID          int64
	TenantID    string
	Timestamp   time.Time
	UserID      string
	Platform    string
	ContentType string // "text", "image", "file"
	Summary     string // first 200 chars of text or filename
	ReturnCode  int
	Duration    time.Duration
	Message     string // message from audit program
	ContentHash string // SHA-256, only for blocked content (code 2,3,4)
}

// ContentAuditDynamicConfig holds dynamic config from SystemSettings.
type ContentAuditDynamicConfig struct {
	ProgramPath    string   `json:"program_path"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	TimeoutPolicy  string   `json:"timeout_policy"`
	Keywords       []string `json:"keywords"`
}

// AuditLogStore is the interface for persisting audit log entries.
type AuditLogStore interface {
	WriteLog(ctx context.Context, entry *AuditLogEntry) error
}

// ---------------------------------------------------------------------------
// ContentAuditor
// ---------------------------------------------------------------------------

const (
	auditMaxConcurrent   = 10
	auditSummaryMaxLen   = 200
	delayPollInterval    = 5 * time.Second
	delayPollMaxAttempts = 10
)

// ContentAuditor calls an external audit program to check outbound IM content.
type ContentAuditor struct {
	programPath    string
	timeoutSec     int
	timeoutPolicy  string // "block" or "pass"
	semaphore      chan struct{}
	logStore       AuditLogStore
	configProvider func(context.Context) *ContentAuditDynamicConfig
}

// NewContentAuditor creates a new ContentAuditor.
func NewContentAuditor(programPath string, timeoutSec int, timeoutPolicy string, logStore AuditLogStore, configProvider func(context.Context) *ContentAuditDynamicConfig) *ContentAuditor {
	return &ContentAuditor{
		programPath:    programPath,
		timeoutSec:     timeoutSec,
		timeoutPolicy:  timeoutPolicy,
		semaphore:      make(chan struct{}, auditMaxConcurrent),
		logStore:       logStore,
		configProvider: configProvider,
	}
}

func (ca *ContentAuditor) dynamicConfig(ctx context.Context) *ContentAuditDynamicConfig {
	if ca == nil || ca.configProvider == nil {
		return nil
	}
	return ca.configProvider(ctx)
}

// auditContent calls the external audit program via stdin/stdout JSON protocol.
func (ca *ContentAuditor) auditContent(ctx context.Context, req *AuditRequest) (*AuditResponse, error) {
	// Acquire semaphore slot.
	select {
	case ca.semaphore <- struct{}{}:
		defer func() { <-ca.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	timeout := time.Duration(ca.timeoutSec) * time.Second
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(timeoutCtx, ca.programPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("audit stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("audit stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("audit start: %w", err)
	}

	// Write request JSON to stdin.
	reqData, _ := json.Marshal(req)
	_, _ = stdin.Write(reqData)
	stdin.Close()

	// Read response JSON from stdout.
	outData, err := io.ReadAll(stdout)
	if err != nil {
		_ = cmd.Wait()
		return nil, fmt.Errorf("audit read stdout: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		if timeoutCtx.Err() != nil {
			return nil, fmt.Errorf("audit timeout: %w", timeoutCtx.Err())
		}
		// Process exited with non-zero but we may still have valid JSON output.
	}

	var resp AuditResponse
	if err := json.Unmarshal(outData, &resp); err != nil {
		return nil, fmt.Errorf("audit invalid JSON: %w", err)
	}
	return &resp, nil
}

// Audit performs content audit on a GenericResponse.
// Returns an AuditResult indicating the action to take.
func (ca *ContentAuditor) Audit(ctx context.Context, userID, platform string, resp *GenericResponse) *AuditResult {
	if ca.programPath == "" {
		return &AuditResult{Action: AuditPass, Response: resp}
	}

	// Determine content type and content string.
	contentType, content, summary := classifyResponse(resp)

	// Build audit request.
	req := &AuditRequest{
		Type:     contentType,
		Content:  content,
		UserID:   userID,
		Platform: platform,
	}

	// Inject keywords from dynamic config.
	if dynCfg := ca.dynamicConfig(ctx); dynCfg != nil && len(dynCfg.Keywords) > 0 {
		req.Keywords = dynCfg.Keywords
	}

	start := time.Now()
	auditResp, err := ca.auditContent(ctx, req)
	duration := time.Since(start)

	var returnCode int
	var message string

	if err != nil {
		returnCode = -1
		message = err.Error()
	} else {
		returnCode = auditResp.Code
		message = auditResp.Message
	}

	// Map return code to action.
	action := mapReturnCode(returnCode)

	// Build result.
	result := &AuditResult{
		Action:  action,
		Message: message,
	}

	switch action {
	case AuditPass:
		result.Response = resp
	case AuditBlock:
		blockMsg := blockMessage(returnCode)
		result.Response = &GenericResponse{
			StatusCode: 200,
			StatusIcon: "warning",
			Body:       blockMsg,
		}
	case AuditManualReview:
		result.Response = &GenericResponse{
			StatusCode: 200,
			StatusIcon: "busy",
			Body:       "内容需要人工审核，请等待管理员审批",
		}
	case AuditSanitize:
		sanitized := *resp
		if auditResp != nil && auditResp.SanitizedContent != "" {
			sanitized.Body = auditResp.SanitizedContent
		}
		result.Response = &sanitized
	case AuditDelay:
		result.Response = &GenericResponse{
			StatusCode: 200,
			StatusIcon: "busy",
			Body:       "内容正在审核中，请稍候",
		}
	case AuditError:
		if ca.timeoutPolicy == "pass" {
			result.Action = AuditPass
			result.Response = resp
		} else {
			result.Action = AuditBlock
			result.Response = &GenericResponse{
				StatusCode: 200,
				StatusIcon: "warning",
				Body:       "内容审核服务异常，消息已被拦截",
			}
		}
	}

	// Write audit log (failure does not affect decision).
	ca.writeAuditLog(ctx, userID, platform, contentType, summary, content, returnCode, duration, message)

	return result
}

// DeliveryCallback is called to deliver a response asynchronously (for delay polling).
type DeliveryCallback func(ctx context.Context, resp *GenericResponse)

// StartDelayPolling starts background polling for delayed audit.
// It sends the placeholder message immediately via deliverFn, then polls
// the audit program until a final decision is reached.
func (ca *ContentAuditor) StartDelayPolling(ctx context.Context, userID, platform string, originalResp *GenericResponse, deliverFn DeliveryCallback) {
	dynCfg := ca.dynamicConfig(ctx)
	go func() {
		// Use a detached context so polling survives after sendResponse returns.
		// Total budget: pollInterval * maxAttempts + margin.
		pollCtx, pollCancel := context.WithTimeout(context.Background(),
			delayPollInterval*time.Duration(delayPollMaxAttempts)+30*time.Second)
		defer pollCancel()

		contentType, content, _ := classifyResponse(originalResp)
		req := &AuditRequest{
			Type:     contentType,
			Content:  content,
			UserID:   userID,
			Platform: platform,
		}
		if dynCfg != nil && len(dynCfg.Keywords) > 0 {
			req.Keywords = dynCfg.Keywords
		}

		for i := 0; i < delayPollMaxAttempts; i++ {
			select {
			case <-time.After(delayPollInterval):
			case <-pollCtx.Done():
				return
			}

			auditResp, err := ca.auditContent(pollCtx, req)
			if err != nil {
				// Treat as error, apply timeout policy.
				if ca.timeoutPolicy == "pass" {
					deliverFn(pollCtx, originalResp)
				} else {
					deliverFn(pollCtx, &GenericResponse{
						StatusCode: 200,
						StatusIcon: "warning",
						Body:       "内容审核服务异常，消息已被拦截",
					})
				}
				return
			}

			switch auditResp.Code {
			case 0:
				deliverFn(pollCtx, originalResp)
				return
			case 2:
				deliverFn(pollCtx, &GenericResponse{
					StatusCode: 200,
					StatusIcon: "warning",
					Body:       "内容不符合数据安全规则，已被拦截",
				})
				return
			case 3:
				deliverFn(pollCtx, &GenericResponse{
					StatusCode: 200,
					StatusIcon: "warning",
					Body:       "内容包含非法信息，已被拦截",
				})
				return
			case 1:
				// Still pending, continue polling.
				continue
			default:
				// Unexpected code during polling, treat as error.
				if ca.timeoutPolicy == "pass" {
					deliverFn(pollCtx, originalResp)
				} else {
					deliverFn(pollCtx, &GenericResponse{
						StatusCode: 200,
						StatusIcon: "warning",
						Body:       "内容审核服务异常，消息已被拦截",
					})
				}
				return
			}
		}

		// Exceeded max poll attempts.
		deliverFn(pollCtx, &GenericResponse{
			StatusCode: 200,
			StatusIcon: "warning",
			Body:       "审核超时，内容未通过",
		})
	}()
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// mapReturnCode maps an audit program return code to an AuditAction.
func mapReturnCode(code int) AuditAction {
	switch code {
	case 0:
		return AuditPass
	case 1:
		return AuditDelay
	case 2, 3:
		return AuditBlock
	case 4:
		return AuditManualReview
	case 5:
		return AuditSanitize
	default: // -1 and any undefined code
		return AuditError
	}
}

// blockMessage returns the user-facing block message for a given return code.
func blockMessage(code int) string {
	switch code {
	case 2:
		return "内容不符合数据安全规则，已被拦截"
	case 3:
		return "内容包含非法信息，已被拦截"
	default:
		return "内容已被拦截"
	}
}

// classifyResponse determines the content type, content string, and summary
// from a GenericResponse.
func classifyResponse(resp *GenericResponse) (contentType, content, summary string) {
	if resp.FileData != "" {
		contentType = "file"
		content = resp.FileData
		summary = resp.FileName
		if summary == "" {
			summary = "(file)"
		}
		return
	}
	if resp.ImageKey != "" {
		contentType = "image"
		content = resp.ImageKey
		summary = resp.ImageCaption
		if summary == "" {
			summary = "(image)"
		}
		return
	}
	contentType = "text"
	content = resp.Body
	summary = resp.Body
	if runes := []rune(summary); len(runes) > auditSummaryMaxLen {
		summary = string(runes[:auditSummaryMaxLen])
	}
	return
}

// writeAuditLog writes an audit log entry. Errors are logged but do not
// affect the audit decision.
func (ca *ContentAuditor) writeAuditLog(ctx context.Context, userID, platform, contentType, summary, content string, returnCode int, duration time.Duration, message string) {
	if ca.logStore == nil {
		return
	}

	entry := &AuditLogEntry{
		TenantID:    TenantIDFromContext(ctx),
		Timestamp:   time.Now(),
		UserID:      userID,
		Platform:    platform,
		ContentType: contentType,
		Summary:     summary,
		ReturnCode:  returnCode,
		Duration:    duration,
		Message:     message,
	}

	// Record content hash for blocked content (code 2, 3, 4).
	if returnCode == 2 || returnCode == 3 || returnCode == 4 {
		h := sha256.Sum256([]byte(content))
		entry.ContentHash = fmt.Sprintf("%x", h)
	}

	if err := ca.logStore.WriteLog(ctx, entry); err != nil {
		log.Printf("[ContentAuditor] failed to write audit log: %v", err)
	}
}
