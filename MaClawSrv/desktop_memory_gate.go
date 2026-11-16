package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

const (
	desktopdURLEnv         = "MACLAW_DESKTOPD_URL"
	desktopdURLLegacyEnv   = "DESKTOPD_URL"
	desktopdTokenEnv       = "MACLAW_DESKTOPD_TOKEN"
	desktopdTokenLegacyEnv = "DESKTOPD_TOKEN"
	desktopdMemoryPath     = "/v1/desktops/memory"

	memoryReadPolicyEnv = "MACLAW_DESKTOP_MEM_READ_POLICY"

	memoryPolicyFailOpen   = "fail_open"
	memoryPolicyFailClosed = "fail_closed"

	memoryGateOK       = "ok"
	memoryGateDegraded = "degraded"

	memorySourceDesktopd = "desktopd"
	memorySourceHub      = "hub"
	memorySourceTest     = "test"

	memoryReasonUnsupported  = "unsupported"
	memoryReasonUnauthorized = "unauthorized"
	memoryReasonUnreachable  = "unreachable"
	memoryReasonInvalid      = "invalid"
	memoryReasonRejected     = "rejected"
	memoryReasonNoClient     = "no_client"
	memoryReasonPolicy       = "policy_unrecognized"
)

// errMemoryNoClient means neither desktopd nor Hub is configured, so the
// gauge has nothing to read.
var errMemoryNoClient = errors.New("memory_no_client")

// errMemoryGateDegraded is the fail-closed result of an unread gauge.
var errMemoryGateDegraded = errors.New("memory_gate_degraded")

const memoryGateClosedLine = "这台电脑的内存读不到，这次先不做。"

// memoryGateError is a fail-closed unread gauge. Reason is a typed constant.
// The sentence is what the person sees. Callers use errors.Is and Kind.
type memoryGateError struct {
	Reason string
	Source string
}

func (e *memoryGateError) Error() string {
	if e == nil {
		return ""
	}
	return memoryGateClosedLine
}

func (e *memoryGateError) Unwrap() error { return errMemoryGateDegraded }

func (e *memoryGateError) Kind() string {
	if e == nil {
		return ""
	}
	return e.Reason
}

type memoryGateEvent struct {
	Gate   string
	Policy string
	Source string
	Reason string
}

// memoryGateNotify is the degradation log. Tests replace it and count calls.
var memoryGateNotify = func(ev memoryGateEvent) {
	log.Printf("[desktop-memory] gate=%s policy=%s source=%s reason=%s", ev.Gate, ev.Policy, ev.Source, ev.Reason)
}

// memoryGateLogGap is how often the same unread reason is logged again.
var memoryGateLogGap = time.Minute

type memoryGateState struct {
	degraded   bool
	reason     string
	source     string
	logged     time.Time
	fallbackAt time.Time
	policyNote time.Time
}

var (
	memoryGateMu sync.Mutex
	memoryGate   memoryGateState
)

// desktopdClientFromEnv is the direct desktopd reader. It is present when a
// desktopd URL and token are configured. Hub is not this client.
func desktopdClientFromEnv() *desktopHubClient {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv(desktopdURLEnv)), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(os.Getenv(desktopdURLLegacyEnv)), "/")
	}
	token := strings.TrimSpace(os.Getenv(desktopdTokenEnv))
	if token == "" {
		token = strings.TrimSpace(os.Getenv(desktopdTokenLegacyEnv))
	}
	if base == "" || token == "" {
		return nil
	}
	return &desktopHubClient{baseURL: base, token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

func memoryReadPolicy() string {
	raw := strings.TrimSpace(os.Getenv(memoryReadPolicyEnv))
	switch raw {
	case "", memoryPolicyFailOpen:
		return memoryPolicyFailOpen
	case memoryPolicyFailClosed:
		return memoryPolicyFailClosed
	default:
		noteMemoryPolicyUnrecognized()
		return memoryPolicyFailOpen
	}
}

func noteMemoryPolicyUnrecognized() {
	memoryGateMu.Lock()
	due := memoryGate.policyNote.IsZero() || time.Since(memoryGate.policyNote) >= memoryGateLogGap
	if due {
		memoryGate.policyNote = time.Now()
	}
	memoryGateMu.Unlock()
	if !due {
		return
	}
	memoryGateNotify(memoryGateEvent{
		Gate:   memoryGateDegraded,
		Policy: memoryPolicyFailOpen,
		Reason: memoryReasonPolicy,
	})
}

func noteMemoryReadFailure(source, reason string) {
	memoryGateMu.Lock()
	now := time.Now()
	same := memoryGate.degraded && memoryGate.reason == reason && memoryGate.source == source
	due := !same || memoryGate.logged.IsZero() || now.Sub(memoryGate.logged) >= memoryGateLogGap
	memoryGate.degraded = true
	memoryGate.reason = reason
	memoryGate.source = source
	if due {
		memoryGate.logged = now
	}
	memoryGateMu.Unlock()
	if !due {
		return
	}
	memoryGateNotify(memoryGateEvent{
		Gate:   memoryGateDegraded,
		Policy: memoryReadPolicy(),
		Source: source,
		Reason: reason,
	})
}

// noteMemoryFallback records that desktopd missed and Hub answered. The
// gate stays ok because a read succeeded. The miss is still logged once
// per interval.
func noteMemoryFallback(reason string) {
	memoryGateMu.Lock()
	now := time.Now()
	due := memoryGate.fallbackAt.IsZero() || now.Sub(memoryGate.fallbackAt) >= memoryGateLogGap
	if due {
		memoryGate.fallbackAt = now
	}
	memoryGateMu.Unlock()
	if !due {
		return
	}
	memoryGateNotify(memoryGateEvent{
		Gate:   memoryGateOK,
		Policy: memoryReadPolicy(),
		Source: memorySourceDesktopd,
		Reason: reason,
	})
}

func noteMemoryReadOK() {
	memoryGateMu.Lock()
	memoryGate.degraded = false
	memoryGate.reason = ""
	memoryGate.source = ""
	memoryGateMu.Unlock()
}

func memoryReadReasonOf(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errMemoryNoClient) {
		return memoryReasonNoClient
	}
	var down *desktopUnreachableError
	if errors.As(err, &down) {
		return memoryReasonUnreachable
	}
	var bad desktopInvalidBodyError
	if errors.As(err, &bad) {
		return memoryReasonInvalid
	}
	var api *desktopAPIError
	if errors.As(err, &api) {
		switch api.Status {
		case http.StatusNotFound, http.StatusMethodNotAllowed:
			return memoryReasonUnsupported
		case http.StatusUnauthorized, http.StatusForbidden:
			return memoryReasonUnauthorized
		default:
			return memoryReasonRejected
		}
	}
	return memoryReasonRejected
}

func finishMemoryRead(source string, used, limit int64, err error) (bool, string, error) {
	if err != nil {
		reason := memoryReadReasonOf(err)
		noteMemoryReadFailure(source, reason)
		if memoryReadPolicy() == memoryPolicyFailClosed {
			return false, "", &memoryGateError{Reason: reason, Source: source}
		}
		return false, "", nil
	}
	noteMemoryReadOK()
	if !desktop.MemoryPressured(used, limit) {
		return false, "", nil
	}
	return true, desktopMemoryWaitLine, nil
}

// memoryGateFields is what /version and /health expose. memory_gate is
// ok or degraded. The reason is present only while the gauge is degraded.
func memoryGateFields() map[string]string {
	memoryGateMu.Lock()
	degraded := memoryGate.degraded
	reason := memoryGate.reason
	memoryGateMu.Unlock()
	mode := memoryGateOK
	if degraded {
		mode = memoryGateDegraded
	}
	out := map[string]string{
		"memory_gate":        mode,
		"memory_gate_policy": memoryReadPolicy(),
	}
	if degraded && reason != "" {
		out["memory_gate_reason"] = reason
	}
	return out
}

func resetMemoryGateForTest() {
	memoryGateMu.Lock()
	memoryGate = memoryGateState{}
	memoryGateMu.Unlock()
}
