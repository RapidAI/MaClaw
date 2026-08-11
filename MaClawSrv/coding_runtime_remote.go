package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// remoteCodingRuntimeStartRequest deliberately accepts an SSH host label, not
// a host, user, port or host-key pin. Those authority-bearing fields are
// resolved from the authenticated user's current configuration immediately
// before the Runtime attempt begins.
type remoteCodingRuntimeStartRequest struct {
	Content      string `json:"content"`
	WorkflowID   string `json:"workflow_id"`
	PhaseID      string `json:"phase_id"`
	SSHHostLabel string `json:"ssh_host_label"`
	WorkDir      string `json:"work_dir"`
}

func (s *HTTPServer) handleStartRemoteCodingRuntime(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in remoteCodingRuntimeStartRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	metadata, err := s.remoteCodingRuntimeMetadata(r, p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	run, msg, err := s.svc.PostMessage(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), agentservice.PostMessageInput{
		Content:  in.Content,
		Metadata: metadata,
	})
	if err != nil {
		if run != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
			return
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg})
}

func (s *HTTPServer) remoteCodingRuntimeMetadata(r *http.Request, p agentservice.Principal, in remoteCodingRuntimeStartRequest) (map[string]string, error) {
	if s == nil || s.svc == nil {
		return nil, fmt.Errorf("remote coding runtime service is unavailable")
	}
	if strings.TrimSpace(in.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}
	workflowID := strings.TrimSpace(in.WorkflowID)
	phaseID := strings.TrimSpace(in.PhaseID)
	if workflowID == "" || phaseID != "implementation" {
		// MaClawSrv does not own a mutable Workflow V2 document. Until it does,
		// only the explicit implementation phase may use this write-capable
		// adapter; planning/review messages stay on their normal paths.
		return nil, fmt.Errorf("remote coding runtime requires a workflow_id and implementation phase")
	}
	cfg, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil || cfg == nil {
		return nil, fmt.Errorf("remote coding runtime configuration is unavailable")
	}
	target, err := configuredRemoteCodingTarget(cfg.AppConfig.SSHHosts, in.SSHHostLabel, in.WorkDir)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"coding_runtime_mode":                        "remote_workflow",
		"coding_runtime_workflow_id":                 workflowID,
		"coding_runtime_phase_id":                    phaseID,
		"coding_runtime_remote_host":                 target.Host,
		"coding_runtime_remote_user":                 target.User,
		"coding_runtime_remote_port":                 fmt.Sprintf("%d", target.Port),
		"coding_runtime_remote_workdir":              target.WorkDir,
		"coding_runtime_remote_host_key_fingerprint": target.HostKeyFingerprint,
		"mutation_scope":                             string(v2.MutationScopeProject),
	}, nil
}

func configuredRemoteCodingTarget(hosts []corelib.SSHHostEntry, label, workDir string) (codingruntime.RemoteTarget, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return codingruntime.RemoteTarget{}, fmt.Errorf("ssh_host_label is required")
	}
	var matches []corelib.SSHHostEntry
	for _, host := range hosts {
		if strings.EqualFold(strings.TrimSpace(host.Label), label) {
			matches = append(matches, host)
		}
	}
	if len(matches) != 1 {
		return codingruntime.RemoteTarget{}, fmt.Errorf("remote coding runtime requires exactly one configured SSH host for the supplied label")
	}
	return codingruntime.NormalizeRemoteTarget(codingruntime.RemoteTarget{
		Host:               matches[0].Host,
		User:               matches[0].User,
		Port:               matches[0].Port,
		WorkDir:            workDir,
		HostKeyFingerprint: matches[0].HostKeyFingerprint,
	})
}

// isReservedCodingRuntimeMetadata prevents the generic chat endpoint from
// becoming a confused-deputy path for Runtime authority. Runtime metadata is
// produced only by a host workflow adapter such as the explicit endpoint
// above, never accepted directly from an HTTP caller.
func isReservedCodingRuntimeMetadata(metadata map[string]string) bool {
	for key := range metadata {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "coding_runtime_") {
			return true
		}
	}
	return false
}
