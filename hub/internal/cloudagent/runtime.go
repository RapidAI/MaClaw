package cloudagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

func newDispatcher(svc *Service) *Dispatcher {
	return &Dispatcher{
		svc: svc,
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Dispatcher sends cloud ops to maclawsrv on a loopback admin URL.
type Dispatcher struct {
	svc    *Service
	client *http.Client
}

// srvCallResultLimit caps one maclawsrv response the Hub buffers for relay.
const srvCallResultLimit int64 = 1 << 20

// srvCloudFileResultLimit covers the file-carrying ops (cloud_read_file,
// cloud_pack_dir): srv caps the raw bytes at 4MB so base64 plus the result
// envelope stay under the desktop's 8MB remote-control reader; the 1MB
// default would silently truncate a valid download into unparseable JSON.
const srvCloudFileResultLimit int64 = 8 << 20

// DispatchCloud implements remotecontrol.CloudDispatcher.
func (d *Dispatcher) DispatchCloud(ctx context.Context, grant remotecontrol.Grant, op string, body json.RawMessage) (json.RawMessage, error) {
	if d == nil || d.svc == nil {
		return nil, offlineStatus()
	}
	_, _, enabled, err := d.svc.EffectiveLimits(ctx, grant.TenantID, grant.TargetUserID)
	if err != nil || !enabled {
		return nil, &remotecontrol.StatusError{Code: "FORBIDDEN", Message: DisabledMessage}
	}
	instanceID := strings.TrimSpace(grant.TargetAgentID)
	if id := bodyInstanceID(body); id != "" && id != instanceID {
		return nil, &remotecontrol.StatusError{Code: "INVALID", Message: "云端实例不匹配"}
	}
	inst, err := d.svc.getOwned(ctx, grant.TenantID, grant.TargetUserID, instanceID)
	if err != nil {
		return nil, &remotecontrol.StatusError{Code: "NOT_FOUND", Message: "没有找到这个云端实例"}
	}
	if inst.Status == StatusStopped && op == remotecontrol.OpCloudSendMessage {
		return nil, &remotecontrol.StatusError{Code: "FORBIDDEN", Message: "实例已停止"}
	}
	// A provision that never landed — a failed attempt or a stale
	// "provisioning" row from a crashed one — retries here: the runtime PUT
	// is idempotent. Without it the row stayed dead until delete+recreate.
	reprovisioned := false
	if inst.Status == StatusProvisionFailed || inst.Status == StatusProvisioning {
		if err := d.provision(ctx, &inst); err != nil {
			log.Printf("[cloud-agent] re-provision %s: %s", inst.ID, logDetail(err))
			return nil, offlineStatus()
		}
		_ = d.svc.setStatus(ctx, inst.ID, StatusIdle)
		inst.Status = StatusIdle
		reprovisioned = true
	}
	if op == remotecontrol.OpCloudSendMessage {
		// The Hub row stays running until the heartbeat window closes, which is
		// after maclawsrv has already finished. Forward the send. The runtime
		// returns the original request for the same client message and refuses
		// a different message while that turn is still running.
		busy, err := d.svc.instanceBusy(ctx, instanceID)
		if err != nil {
			return nil, &remotecontrol.StatusError{Code: "FORBIDDEN", Message: "云端管理暂时不可用"}
		}
		if !busy {
			err := d.svc.ensureTurnRoom(ctx, grant.TenantID, grant.TargetUserID, instanceID)
			if err != nil && d.settleStaleTurns(ctx, &inst, grant.TargetUserID) {
				// Shadow state can lag the runtime: a lost finish beat pins a
				// stale running row for the whole miss window. If the srv book
				// says nothing is running, retire the rows and re-check now
				// instead of making the caller wait for the sweep.
				err = d.svc.ensureTurnRoom(ctx, grant.TenantID, grant.TargetUserID, instanceID)
			}
			if err != nil {
				return nil, &remotecontrol.StatusError{Code: "FORBIDDEN", Message: err.Error()}
			}
		}
	}
	payload := map[string]any{
		"op":            op,
		"body":          json.RawMessage(bodyOrEmpty(body)),
		"owner_user_id": grant.TargetUserID,
		"grant_id":      grant.ID,
	}
	raw, err := d.control(ctx, &inst, payload)
	if err != nil {
		var client *Failure
		if errors.As(err, &client) && client.HTTP == http.StatusNotFound && !reprovisioned {
			// The runtime no longer has the instance (wiped data, a node
			// replaced under it): re-provision once and retry.
			if perr := d.provision(ctx, &inst); perr != nil {
				log.Printf("[cloud-agent] re-provision %s after 404: %s", inst.ID, logDetail(perr))
				return nil, offlineStatus()
			}
			_ = d.svc.setStatus(ctx, inst.ID, StatusIdle)
			inst.Status = StatusIdle
			raw, err = d.control(ctx, &inst, payload)
		}
	}
	if err != nil {
		var client *Failure
		if errors.As(err, &client) && client.HTTP >= 400 && client.HTTP < 500 && client.Code != "OFFLINE" {
			return nil, &remotecontrol.StatusError{Code: client.Code, Message: client.Message}
		}
		log.Printf("[cloud-agent] instance %s op %s: %s", inst.ID, op, logDetail(err))
		return nil, offlineStatus()
	}
	if op == remotecontrol.OpCloudSendMessage {
		taskID, requestID, accepted := acceptedTurn(body, raw)
		if accepted {
			_ = d.svc.NoteAcceptedTurn(ctx, grant.TenantID, grant.TargetUserID, instanceID, taskID, requestID)
		}
	}
	if op == remotecontrol.OpCloudStopTask {
		if ids := cancelledTurnIDs(raw); len(ids) > 0 {
			_ = d.svc.CancelTurns(ctx, instanceID, ids)
		}
	}
	return raw, nil
}

// settleStaleTurns asks the runtime whether a turn is actually running and,
// when it is not, finishes this instance's stale running rows so the slot
// frees on demand instead of at the next sweep tick. Returns false whenever
// the runtime is unreachable or still busy, leaving the original failure.
func (d *Dispatcher) settleStaleTurns(ctx context.Context, inst *Instance, owner string) bool {
	raw, err := d.control(ctx, inst, map[string]any{
		"op":            "cloud_instance_state",
		"body":          json.RawMessage(`{}`),
		"owner_user_id": owner,
	})
	if err != nil {
		return false
	}
	var state struct {
		Running bool `json:"running"`
	}
	if err := json.Unmarshal(raw, &state); err != nil || state.Running {
		return false
	}
	if err := d.svc.settleRunning(ctx, inst.ID, formatTS(d.svc.now())); err != nil {
		return false
	}
	return true
}

// control resolves the conn the instance's recorded srv binding points at
// and posts the op. A conn failure marks the row provision_failed so the
// next op retries provisioning.
func (d *Dispatcher) control(ctx context.Context, inst *Instance, payload map[string]any) (json.RawMessage, error) {
	conn, err := d.svc.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID)
	if err != nil {
		_ = d.svc.MarkProvisionFailed(ctx, inst.ID)
		log.Printf("[cloud-agent] instance %s conn: %s", inst.ID, logDetail(err))
		return nil, err
	}
	resultLimit := srvCallResultLimit
	if op, _ := payload["op"].(string); op == remotecontrol.OpCloudReadFile || op == remotecontrol.OpCloudPackDir {
		resultLimit = srvCloudFileResultLimit
	}
	return d.callLimit(ctx, http.MethodPost, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/control", payload, resultLimit)
}

func offlineStatus() error {
	return &remotecontrol.StatusError{Code: "OFFLINE", Message: "云端运行时不可用"}
}

func bodyOrEmpty(body json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(body)) == 0 {
		return json.RawMessage(`{}`)
	}
	return body
}

func bodyInstanceID(body json.RawMessage) string {
	var head struct {
		InstanceID string `json:"instance_id"`
	}
	_ = json.Unmarshal(body, &head)
	return strings.TrimSpace(head.InstanceID)
}

func cancelledTurnIDs(raw json.RawMessage) []string {
	var ack struct {
		Cancelled []string `json:"cancelled"`
	}
	_ = json.Unmarshal(raw, &ack)
	out := make([]string, 0, len(ack.Cancelled))
	seen := map[string]struct{}{}
	for _, id := range ack.Cancelled {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func acceptedTurn(body, raw json.RawMessage) (taskID, requestID string, accepted bool) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal(body, &req)
	var ack struct {
		Accepted  bool   `json:"accepted"`
		RequestID string `json:"request_id"`
		TaskID    string `json:"task_id"`
	}
	_ = json.Unmarshal(raw, &ack)
	taskID = strings.TrimSpace(ack.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(req.TaskID)
	}
	return taskID, strings.TrimSpace(ack.RequestID), ack.Accepted && strings.TrimSpace(ack.RequestID) != "" && taskID != ""
}

func (d *Dispatcher) provision(ctx context.Context, inst *Instance) error {
	conn, err := d.svc.srvConn(ctx, inst.TenantID, inst.OwnerUserID)
	if err != nil {
		return err
	}
	if conn.LinkID != inst.SrvNodeID {
		if err := d.svc.setSrvNode(ctx, inst.ID, conn.LinkID); err != nil {
			return err
		}
		inst.SrvNodeID = conn.LinkID
	}
	loopback := hubLoopback()
	origin := hubPublicOrigin()
	if conn.NodeID != "" || !loopbackBaseURL(conn.BaseURL) {
		// A private or off-host srv cannot reach hub's loopback; it calls
		// the public origin directly. Hub accepts its link secret from any
		// address. Without a configured public origin the instance could
		// never call back, so fail instead of provisioning a dead agent.
		if origin == "" {
			return fail(503, "OFFLINE", "私有 maclawsrv 需要 Hub 先配置 MACLAW_HUB_URL 公网地址")
		}
		loopback = origin
	}
	if origin == "" {
		// With MACLAW_HUB_URL unset, the only origin a co-located srv can
		// same-origin match against its own env is the loopback we send.
		origin = loopback
	}
	_, err = d.call(ctx, http.MethodPut, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID), map[string]any{
		"id":                inst.ID,
		"tenant_id":         inst.TenantID,
		"owner_user_id":     inst.OwnerUserID,
		"name":              inst.Name,
		"kind":              inst.Kind,
		"llm_mode":          inst.LLMMode,
		"llm_model":         inst.LLMModel,
		"llm_thinking_mode": inst.LLMThinkingMode,
		"hub_origin":        origin,
		"hub_loopback":      loopback,
	})
	return err
}

func (d *Dispatcher) call(ctx context.Context, method string, conn srvConn, path string, payload any) (json.RawMessage, error) {
	return d.callLimit(ctx, method, conn, path, payload, srvCallResultLimit)
}

func (d *Dispatcher) callLimit(ctx context.Context, method string, conn srvConn, path string, payload any, resultLimit int64) (json.RawMessage, error) {
	var buf io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, conn.BaseURL+path, buf)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-MaClaw-Admin-Secret", conn.AdminSecret)
	client := d.client
	if conn.NodeID != "" {
		if d.svc.NodeHTTP == nil {
			return nil, failOfflineDetail("no edge tunnel resolver for node " + conn.NodeID)
		}
		client = d.svc.NodeHTTP(conn.NodeID)
		if client == nil {
			return nil, failOfflineDetail("no edge tunnel client for node " + conn.NodeID)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, failOfflineDetail("runtime call failed: " + err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, resultLimit))
	if err != nil {
		return nil, failOfflineDetail("runtime response read failed: " + err.Error())
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, failOfflineDetail("runtime rejected the admin secret (401)")
	}
	if resp.StatusCode >= 500 {
		return nil, failOfflineDetail("runtime returned HTTP " + strconv.Itoa(resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(runtimeErrorMessage(body))
		if msg == "" {
			msg = "云端运行时拒绝了这个操作"
		}
		return nil, fail(resp.StatusCode, "INVALID", msg)
	}
	return body, nil
}

func runtimeErrorMessage(body []byte) string {
	var payload struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if strings.TrimSpace(payload.Error) != "" {
		return payload.Error
	}
	return payload.Message
}

func dialReachable(ctx context.Context, base string) bool {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(parsed.Hostname(), port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func loopbackBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.Trim(strings.ToLower(parsed.Hostname()), ".")
	return host == "127.0.0.1" || host == "::1"
}

func hubPublicOrigin() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("MACLAW_HUB_URL")), "/")
}

func hubLoopback() string {
	if raw := strings.TrimRight(strings.TrimSpace(os.Getenv("MACLAW_HUB_LOOPBACK")), "/"); loopbackBaseURL(raw) {
		return raw
	}
	if raw := hubPublicOrigin(); loopbackBaseURL(raw) {
		return raw
	}
	return "http://127.0.0.1:9399"
}

// LoopbackAddr reports whether a request came from the loopback interface.
// An empty address is not loopback.
func LoopbackAddr(remote string) bool {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return false
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "127.0.0.1" || host == "::1"
}

func (s *Service) gitPublic(ctx context.Context, inst Instance) GitPublic {
	unknown := GitPublic{Status: "unknown"}
	conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID)
	if err != nil {
		return unknown
	}
	raw, err := s.disp.call(ctx, http.MethodGet, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/git", nil)
	if err != nil {
		return unknown
	}
	var view GitPublic
	if err := json.Unmarshal(raw, &view); err != nil {
		return unknown
	}
	view.Status = ""
	return view
}

// ConnectGit forwards a coding-instance git connection. The token is not stored here.
func (s *Service) ConnectGit(ctx context.Context, tenantID, userID, id string, in GitConnect) (GitPublic, error) {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return GitPublic{}, err
	}
	if inst.Kind != KindCoding {
		return GitPublic{}, failSessionGit()
	}
	if err := validateGit(in); err != nil {
		return GitPublic{}, err
	}
	conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID)
	if err != nil {
		return GitPublic{}, failOffline()
	}
	raw, err := s.disp.call(ctx, http.MethodPut, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/git", in)
	if err != nil {
		return GitPublic{}, failOffline()
	}
	var view GitPublic
	if err := json.Unmarshal(raw, &view); err != nil {
		return GitPublic{}, failOffline()
	}
	view.Status = ""
	return view, nil
}

// ReadGit forwards the git view (no token) so clients can see which
// credential source the instance uses and refresh account-bound tokens.
func (s *Service) ReadGit(ctx context.Context, tenantID, userID, id string) (GitPublic, error) {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return GitPublic{}, err
	}
	if inst.Kind != KindCoding {
		return GitPublic{}, failSessionGit()
	}
	conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID)
	if err != nil {
		return GitPublic{}, failOffline()
	}
	raw, err := s.disp.call(ctx, http.MethodGet, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/git", nil)
	if err != nil {
		return GitPublic{}, failOffline()
	}
	var view GitPublic
	if err := json.Unmarshal(raw, &view); err != nil {
		return GitPublic{}, failOffline()
	}
	view.Status = ""
	return view, nil
}

// DisconnectGit removes maclawsrv's git.json. The checkout stays.
func (s *Service) DisconnectGit(ctx context.Context, tenantID, userID, id string) error {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return err
	}
	if inst.Kind != KindCoding {
		return failSessionGit()
	}
	conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID)
	if err != nil {
		return failOffline()
	}
	_, err = s.disp.call(ctx, http.MethodDelete, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/git", nil)
	if err != nil {
		return failOffline()
	}
	return nil
}

func validateGit(in GitConnect) error {
	if strings.TrimSpace(in.AuthorName) == "" || strings.TrimSpace(in.AuthorEmail) == "" {
		return failInvalid("请填写提交作者的姓名和邮箱")
	}
	if strings.Contains(in.AuthorEmail, " ") || !strings.Contains(in.AuthorEmail, "@") {
		return failInvalid("提交作者邮箱无效")
	}
	switch strings.TrimSpace(in.Service) {
	case "github", "gitlab":
	default:
		return failInvalid("代码库只支持 GitHub 或 GitLab")
	}
	if strings.TrimSpace(in.Token) == "" {
		return failInvalid("请填写访问令牌")
	}
	repo := strings.TrimSpace(in.Repo)
	if repo == "" || strings.ContainsAny(repo, " \t\r\n") || gitRejected(repo) {
		return failInvalid("代码库名称无效")
	}
	origin := strings.TrimSpace(in.Origin)
	if gitRejected(origin) {
		return failInvalid("代码库地址只接受 HTTPS")
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return failInvalid("代码库地址只接受 HTTPS")
	}
	if strings.TrimSpace(in.Service) == "github" && !strings.EqualFold(parsed.Hostname(), "github.com") {
		return failInvalid("GitHub 地址无效")
	}
	if strings.TrimSpace(in.Service) == "gitlab" && parsed.Path != "" && parsed.Path != "/" {
		return failInvalid("GitLab 只保存站点地址")
	}
	return nil
}

func gitRejected(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "ssh://") || strings.HasPrefix(lower, "git@") || strings.Contains(lower, "git@")
}

// SetCustomLLM stores the mode on Hub and the secret only on maclawsrv.
func (s *Service) SetCustomLLM(ctx context.Context, tenantID, userID, id string, in CustomLLM, hubHosts []string) (Instance, error) {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return Instance{}, err
	}
	rawURL := strings.TrimSpace(in.URL)
	if rawURL == "" || strings.TrimSpace(in.Key) == "" {
		return Instance{}, failInvalid("请填写模型地址和密钥")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Instance{}, failInvalid("模型地址无效")
	}
	if IsHubLLMURL(rawURL, hubHosts) {
		return Instance{}, failInvalid("自定义模型不能指向本 Hub 的官方接口")
	}
	conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID)
	if err != nil {
		return Instance{}, failOffline()
	}
	if _, err := s.disp.call(ctx, http.MethodPut, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/llm", in); err != nil {
		return Instance{}, failOffline()
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		label = parsed.Hostname()
	}
	_, err = s.db.ExecContext(ctx, `UPDATE cloud_agents SET llm_mode = ?, llm_label = ?, llm_model = ?, updated_at = ? WHERE id = ? AND deleted_at = ''`,
		LLMCustom, label, strings.TrimSpace(in.Model), formatTS(s.now()), inst.ID)
	if err != nil {
		// srv already saved llm.json while Hub still records the previous
		// mode. Roll the srv back when the instance was official, otherwise
		// the Hub would show 官方 while turns use the custom key. A previous
		// custom mode keeps the new secret and only the display row lags.
		if inst.LLMMode != LLMCustom {
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			if _, rbErr := s.disp.call(rollbackCtx, http.MethodDelete, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/llm", nil); rbErr != nil {
				log.Printf("[cloud-agent] llm rollback %s: %s", inst.ID, logDetail(rbErr))
			}
			cancel()
		}
		return Instance{}, err
	}
	return s.Get(ctx, inst.TenantID, inst.OwnerUserID, inst.ID)
}

// IsHubLLMURL reports a URL whose host is this Hub and whose path is the official LLM API.
func IsHubLLMURL(raw string, hosts []string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if path != "/api/llm/v1" && !strings.HasPrefix(path, "/api/llm/v1/") {
		return false
	}
	host := strings.ToLower(strings.Trim(parsed.Hostname(), "."))
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}
	for _, extra := range hosts {
		if host == hostOf(extra) {
			return true
		}
	}
	return false
}

func hostOf(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil {
			value = parsed.Host
		}
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.ToLower(strings.Trim(value, "[]"))
}
