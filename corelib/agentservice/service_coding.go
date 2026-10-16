package agentservice

import (
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// SetCodingRuntimeStore attaches a host-owned durable coding Ledger to an
// executor that explicitly supports it. Service keeps ownership of principal,
// tenant, user, instance and session resolution; transport code never invokes
// a coding executor directly. Returns false for executors that do not expose
// the optional coding-runtime capability.
func (s *Service) SetCodingRuntimeStore(store codingruntime.Store) bool {
	if s == nil {
		return false
	}
	configurer, ok := s.executor.(interface {
		SetCodingRuntimeStore(codingruntime.Store)
	})
	if !ok {
		return false
	}
	configurer.SetCodingRuntimeStore(store)
	return true
}

// CodingRuntimeStoreSupported reports whether the configured executor can host
// the durable coding runtime ledger. Test doubles such as EchoExecutor cannot.
func (s *Service) CodingRuntimeStoreSupported() bool {
	if s == nil {
		return false
	}
	_, ok := s.executor.(interface {
		SetCodingRuntimeStore(codingruntime.Store)
	})
	return ok
}

// CodingRuntimeRemoteRecoveryProber asks the configured executor for a
// session-bound, read-only remote probe. It deliberately exposes no SSH
// connection or command controls to HTTP hosts; recovery can only use an
// existing verified session for the authenticated principal.
func (s *Service) CodingRuntimeRemoteRecoveryProber(p Principal, cfg corelib.AppConfig, task codingruntime.Task, policy codingruntime.PolicySnapshot) (codingruntime.WorkspaceProber, error) {
	if s == nil {
		return nil, fmt.Errorf("coding runtime service is unavailable")
	}
	provider, ok := s.executor.(interface {
		CodingRuntimeRemoteRecoveryProber(Principal, corelib.AppConfig, codingruntime.Task, codingruntime.PolicySnapshot) (codingruntime.WorkspaceProber, error)
	})
	if !ok {
		return nil, fmt.Errorf("coding runtime executor does not support remote recovery")
	}
	return provider.CodingRuntimeRemoteRecoveryProber(p, cfg, task, policy)
}
