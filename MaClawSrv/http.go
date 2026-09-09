package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	transporthttp "github.com/RapidAI/CodeClaw/MaClawSrv/transport/http"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	coreconfig "github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/database"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
)

type configEnvelope struct {
	AppConfig corelib.AppConfig `json:"app_config"`
}

type importStateEnvelope struct {
	Data      agentservice.ExportServiceStateOutput `json:"data"`
	Overwrite bool                                  `json:"overwrite,omitempty"`
	DryRun    bool                                  `json:"dry_run,omitempty"`
}

type readinessCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

type readinessReport struct {
	Status      string           `json:"status"`
	GeneratedAt time.Time        `json:"generated_at"`
	DataRoot    string           `json:"data_root,omitempty"`
	Checks      []readinessCheck `json:"checks"`
}

type adminRiskEvent struct {
	ID           string            `json:"id"`
	Severity     string            `json:"severity"`
	Kind         string            `json:"kind"`
	Summary      string            `json:"summary"`
	Action       string            `json:"action,omitempty"`
	ResourceType string            `json:"resource_type,omitempty"`
	ResourceID   string            `json:"resource_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

type HTTPServer struct {
	svc         *agentservice.Service
	adminSecret string
	mux         *http.ServeMux
	authLimiter *authLimiter
	// databaseAdminLimiter throttles the admin database profile configuration
	// endpoints independently of the login limiter; see admin_database.go.
	databaseAdminLimiter     *authLimiter
	databaseProfileRefresher databaseProfileRefresher
	databaseSecretResolver   database.SecretResolver
	databaseApprovalIssuer   databaseApprovalIssuer
	// databaseAdminIdemMu serializes idempotency check-and-store for profile
	// writes so concurrent retries of the same key cannot both commit.
	databaseAdminIdemMu  sync.Mutex
	launchTokens         *launchTokenStore
	weixinQRTokens       *weixinQRTokenStore
	qqbotQRTokens        *weixinQRTokenStore
	qqbotQR              *qqbot.QRClient
	weixinRuntime        *srvWeixinGatewayManager
	imRuntime            *srvIMGatewayManager
	thirdPartyIM         *srvThirdPartyGatewayManager
	hardwareBindings     *srvDeviceAgentBindingStore
	devicePairings       *srvDevicePairingStore
	devicePairLimit      *authLimiter
	deviceUpdateBindings *srvDeviceUpdateBindingStore
	deviceUpdateCatalog  *srvDeviceUpdateCatalog
	githubReleaseCatalog *srvGitHubReleaseCatalog
	jobs                 *asyncJobManager
	knowledgeMgr         *knowledgeStoreManager
	enterpriseSync       *enterpriseSyncCoordinator
	skillSourceSvc       *cskill.SourceControlService
	aiModels             *srvAIModelManager
	// dynamicCapabilityPublisher is held only by this authenticated admin
	// control-plane host. It is never exposed to request execution or ordinary
	// user-facing Skill/MCP lifecycle APIs.
	dynamicCapabilityPublisher *agentservice.DynamicCapabilityContractPublisher
	// codingRuntimeStore is transport-neutral task/attempt history. HTTP and
	// future service executors use adapters; the server never imports GUI code.
	codingRuntimeStore *codingruntime.SQLiteStore
	// codingRuntimeRecoveryProber is a test seam around the otherwise fixed
	// local, read-only workspace probe. Production leaves it nil and uses the
	// local Git prober below; it must never supply a mutating probe.
	codingRuntimeRecoveryProber func(codingruntime.Task) codingruntime.WorkspaceProber
	closeOnce                   sync.Once
}

func newWeixinQRTokenStore() *weixinQRTokenStore {
	return &weixinQRTokenStore{tokens: map[string]weixinQRTokenRecord{}}
}

// NewHTTPServer keeps the historical nil-on-error API used by tests and
// embedders. New production code should prefer NewHTTPServerWithError so an
// initialization failure cannot turn into a nil-pointer panic in main.
func NewHTTPServer(svc *agentservice.Service, adminSecret string, knowledgeMgr *knowledgeStoreManager, skillSourceSvc ...*cskill.SourceControlService) *HTTPServer {
	server, err := NewHTTPServerWithError(svc, adminSecret, knowledgeMgr, skillSourceSvc...)
	if err != nil {
		return nil
	}
	return server
}

// NewHTTPServerWithError constructs the HTTP transport and reports failures
// from reviewed capability registry/publisher initialization to the caller.
// A partially initialized server must never start accepting requests.
func NewHTTPServerWithError(svc *agentservice.Service, adminSecret string, knowledgeMgr *knowledgeStoreManager, skillSourceSvc ...*cskill.SourceControlService) (*HTTPServer, error) {
	if svc == nil {
		return nil, errors.New("service is required")
	}
	var sourceSvc *cskill.SourceControlService
	if len(skillSourceSvc) > 0 {
		sourceSvc = skillSourceSvc[0]
	}
	if sourceSvc == nil {
		sourceSvc = cskill.NewSourceControlService(newFileKVStore(filepath.Join(svc.DataRoot(), "skill_source_control.json")))
	}
	wireSkillSourceFilter(svc, sourceSvc)
	reviewedRegistry, err := agentservice.NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		return nil, fmt.Errorf("create reviewed capability registry: %w", err)
	}
	publisher, err := agentservice.NewDynamicCapabilityContractPublisher(svc, reviewedRegistry)
	if err != nil {
		return nil, fmt.Errorf("create dynamic capability publisher: %w", err)
	}
	s := &HTTPServer{svc: svc, adminSecret: adminSecret, mux: http.NewServeMux(), authLimiter: newAuthLimiter(20, time.Minute), databaseAdminLimiter: newAuthLimiter(30, time.Minute), launchTokens: newLaunchTokenStore(), weixinQRTokens: newWeixinQRTokenStore(), qqbotQRTokens: newWeixinQRTokenStore(), qqbotQR: qqbot.NewQRClient(), devicePairings: newSrvDevicePairingStore(), devicePairLimit: newAuthLimiter(6, time.Minute), deviceUpdateBindings: newSrvDeviceUpdateBindingStore(svc.DataRoot()), deviceUpdateCatalog: newSrvDeviceUpdateCatalog(svc.DataRoot()), hardwareBindings: newSrvDeviceAgentBindingStore(svc.DataRoot()), jobs: newAsyncJobManager(svc.DataRoot()), knowledgeMgr: knowledgeMgr, skillSourceSvc: sourceSvc, aiModels: newSrvAIModelManager(svc.DataRoot()), dynamicCapabilityPublisher: publisher}
	s.initCodingRuntimeStore()
	if releaseCatalog, err := newSrvGitHubReleaseCatalogFromEnv(s.deviceUpdateCatalog); err != nil {
		// An invalid trust anchor must disable this optional provider rather than
		// silently accepting an unsigned/local substitute. Existing local
		// metadata remains bounded by its own expiry policy.
		srvLog().Warn("release catalog disabled", slog.String("error", err.Error()))
	} else if releaseCatalog != nil {
		s.githubReleaseCatalog = releaseCatalog
		releaseCatalog.start()
	}
	s.aiModels.setDownloadConfigProvider(func() corelib.AppConfig {
		return s.defaultConfigForAIModels(context.Background())
	})
	if s.knowledgeMgr != nil {
		s.knowledgeMgr.ConfigureImageDescriber(newSrvKnowledgeImageDescriber(svc, svc.DataRoot()))
		s.knowledgeMgr.UseSharedAIModels(s.aiModels, func() corelib.AppConfig {
			return s.defaultConfigForAIModels(context.Background())
		})
	}
	svc.AssistantMessageMetadataHook = s.decorateAssistantMessageMetadata
	s.weixinRuntime = newSrvWeixinGatewayManager(svc, s.aiModels)
	s.imRuntime = newSrvIMGatewayManager(svc, s.aiModels)
	s.thirdPartyIM = newSrvThirdPartyGatewayManager(svc, s.aiModels, s.hardwareBindings)
	s.startConfiguredAIModelDownloads(context.Background())
	s.startConfiguredWeixinRuntimes(context.Background())
	s.startConfiguredIMRuntimes(context.Background())
	s.routes()
	s.startSandboxStartupDiagnoseIfEnabled()
	return s, nil
}

func (s *HTTPServer) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.jobs != nil {
			s.jobs.close()
		}
		if s.weixinRuntime != nil {
			s.weixinRuntime.StopAll()
		}
		if s.imRuntime != nil {
			s.imRuntime.StopAll()
		}
		if s.thirdPartyIM != nil {
			s.thirdPartyIM.StopAll()
		}
		if s.githubReleaseCatalog != nil {
			s.githubReleaseCatalog.close()
		}
		if s.aiModels != nil {
			s.aiModels.Close()
		}
		if s.codingRuntimeStore != nil {
			_ = s.codingRuntimeStore.Close()
		}
	})
}

// initCodingRuntimeStore opens the shared corelib ledger only when the Service
// executor can actually consume it. Most control-plane/test executors do not
// support coding workflows; eagerly opening a database for those servers
// wastes a connection and makes transport shutdown own a resource it cannot
// use. Recovery must be initiated by an authenticated host adapter using the
// read-only recovery protocol.
func (s *HTTPServer) initCodingRuntimeStore() {
	if s == nil || s.svc == nil || s.codingRuntimeStore != nil || !s.svc.CodingRuntimeStoreSupported() {
		return
	}
	store, err := codingruntime.NewSQLiteStore(filepath.Join(s.svc.DataRoot(), "coding_runtime.db"))
	if err != nil {
		srvLog().Warn("coding runtime disabled", slog.String("error", err.Error()))
		return
	}
	if expired, err := store.ExpireLeases(time.Now().UTC()); err != nil {
		srvLog().Warn("coding runtime stale lease sweep failed", slog.String("error", err.Error()))
	} else if len(expired) > 0 {
		srvLog().Warn("coding runtime stale attempts interrupted; recovery requires a read-only probe", slog.Int("count", len(expired)))
	}
	if interrupted, err := store.InterruptUnstartedChildren(time.Now().UTC()); err != nil {
		srvLog().Warn("coding runtime unstarted child reconciliation failed", slog.String("error", err.Error()))
	} else if len(interrupted) > 0 {
		srvLog().Warn("coding runtime waiting parent attempts interrupted; child dispatch is not replayed", slog.Int("count", len(interrupted)))
	}
	if !s.svc.SetCodingRuntimeStore(store) {
		_ = store.Close()
		srvLog().Warn("coding runtime adapter no longer supported by executor during initialization")
		return
	}
	s.codingRuntimeStore = store
}

const maxJSONBodyBytes int64 = 1 << 20

func (s *HTTPServer) Handler() http.Handler {
	if s == nil {
		return http.NewServeMux()
	}
	return withRequestID(s.mux)
}

func (s *HTTPServer) routes() {
	s.registerOpsRoutes()
	s.registerAdminRoutes()
	s.registerPlatformRoutes()
	s.registerUserAPIRoutes()
}


func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *HTTPServer) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}
func (s *HTTPServer) handleReady(w http.ResponseWriter, r *http.Request) {
	report := buildReadinessReport(s.svc.DataRoot(), s.jobs.repositoryPath)
	if s.jobs != nil && !s.jobs.persistenceHealthy() {
		report.Status = "not_ready"
		report.Checks = append(report.Checks, readinessCheck{Name: "jobs_store_persistence", Status: "fail", Error: "async job store persistence unavailable"})
	}
	if report.Status != "ready" {
		errMsg := "service not ready"
		for _, check := range report.Checks {
			if check.Status != "pass" && check.Error != "" {
				errMsg = check.Error
				break
			}
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": report.Status, "error": errMsg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": report.Status})
}

func buildReadinessReport(dataRoot, jobsRepositoryPath string) readinessReport {
	report := readinessReport{
		Status:      "ready",
		GeneratedAt: time.Now().UTC(),
		DataRoot:    dataRoot,
		Checks: []readinessCheck{
			checkPathExists("data_root_exists", dataRoot),
			checkPathDirectory("data_root_is_dir", dataRoot),
			checkDirectoryWritable("data_root_writable", dataRoot),
			checkDirectoryWritable("state_dir_writable", filepath.Join(dataRoot, "state")),
		},
	}
	if stringsTrim(jobsRepositoryPath) != "" {
		report.Checks = append(report.Checks, checkDirectoryWritable("jobs_store_parent_writable", filepath.Dir(jobsRepositoryPath)))
	}
	for _, check := range report.Checks {
		if check.Status != "pass" {
			report.Status = "not_ready"
			break
		}
	}
	return report
}

func checkReadyDataRoot(dataRoot string) error {
	report := buildReadinessReport(dataRoot, filepath.Join(dataRoot, "state", "jobs.db"))
	for _, check := range report.Checks {
		if check.Name == "data_root_exists" && check.Status != "pass" {
			return errors.New("data root unavailable")
		}
		if check.Name == "data_root_is_dir" && check.Status != "pass" {
			return errors.New("data root is not a directory")
		}
		if check.Name == "data_root_writable" && check.Status != "pass" {
			return errors.New("data root is not writable")
		}
	}
	return nil
}

func checkPathExists(name, target string) readinessCheck {
	check := readinessCheck{Name: name, Status: "pass", Path: target}
	if _, err := os.Stat(target); err != nil {
		check.Status = "fail"
		check.Error = "data root unavailable"
	}
	return check
}

func checkPathDirectory(name, target string) readinessCheck {
	check := readinessCheck{Name: name, Status: "pass", Path: target}
	info, err := os.Stat(target)
	if err != nil {
		check.Status = "fail"
		check.Error = "data root unavailable"
		return check
	}
	if !info.IsDir() {
		check.Status = "fail"
		check.Error = "data root is not a directory"
	}
	return check
}

func checkDirectoryWritable(name, target string) readinessCheck {
	check := readinessCheck{Name: name, Status: "pass", Path: target}
	if stringsTrim(target) == "" {
		check.Status = "fail"
		check.Error = "directory path is empty"
		return check
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		check.Status = "fail"
		check.Error = "directory is not writable"
		return check
	}
	f, err := os.CreateTemp(target, ".readyz-*")
	if err != nil {
		check.Status = "fail"
		check.Error = "directory is not writable"
		return check
	}
	nameOnDisk := f.Name()
	_ = f.Close()
	_ = os.Remove(nameOnDisk)
	return check
}
func (s *HTTPServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": serviceVersion, "commit": serviceCommit, "built_at": serviceBuiltAt})
}
func writeMetricsUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("# MaClawSrv metrics unavailable\n# TYPE maclaw_metrics_up gauge\nmaclaw_metrics_up 0\n"))
}
func (s *HTTPServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	overview, err := s.svc.GetAdminOverview(r.Context())
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	authFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "auth.token_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	rateLimited, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "auth.token_rate_limited"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminAuthFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.auth_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminOwnerDenied, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.owner_required_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminLoginFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.login_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminLoginRateLimited, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.login_rate_limited"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminPasswordChangeFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.password_change_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	runSucceeded, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "run.succeeded"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	runFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "run.failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	alerts, err := s.svc.GetAdminAlerts(r.Context(), agentservice.AdminAlertsInput{})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	jobCounts := s.jobs.snapshotCounts()
	var b strings.Builder
	b.WriteString("# HELP maclaw_metrics_up Whether metrics collection succeeded\n")
	b.WriteString("# TYPE maclaw_metrics_up gauge\n")
	b.WriteString("maclaw_metrics_up 1\n")
	b.WriteString("# HELP maclaw_tenants_total Number of tenants\n")
	b.WriteString("# TYPE maclaw_tenants_total gauge\n")
	b.WriteString("maclaw_tenants_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Tenants), 10))
	b.WriteString("\n# HELP maclaw_users_total Number of users\n")
	b.WriteString("# TYPE maclaw_users_total gauge\n")
	b.WriteString("maclaw_users_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Users), 10))
	b.WriteString("\n# HELP maclaw_credentials_total Number of credentials\n")
	b.WriteString("# TYPE maclaw_credentials_total gauge\n")
	b.WriteString("maclaw_credentials_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Credentials), 10))
	b.WriteString("\n# HELP maclaw_credentials_by_status Number of credentials by lifecycle status\n")
	b.WriteString("# TYPE maclaw_credentials_by_status gauge\n")
	b.WriteString("maclaw_credentials_by_status{status=\"active\"} ")
	b.WriteString(strconv.FormatInt(int64(overview.ActiveCredentials), 10))
	b.WriteString("\nmaclaw_credentials_by_status{status=\"suspended\"} ")
	b.WriteString(strconv.FormatInt(int64(overview.SuspendedCredentials), 10))
	b.WriteString("\nmaclaw_credentials_by_status{status=\"revoked\"} ")
	b.WriteString(strconv.FormatInt(int64(overview.RevokedCredentials), 10))
	b.WriteString("\n# HELP maclaw_credentials_expired_total Number of credentials whose expires_at has passed\n")
	b.WriteString("# TYPE maclaw_credentials_expired_total gauge\n")
	b.WriteString("maclaw_credentials_expired_total ")
	b.WriteString(strconv.FormatInt(int64(overview.ExpiredCredentials), 10))
	b.WriteString("\n# HELP maclaw_credentials_expiring_total Number of credentials expiring within the default lookahead window\n")
	b.WriteString("# TYPE maclaw_credentials_expiring_total gauge\n")
	b.WriteString("maclaw_credentials_expiring_total ")
	b.WriteString(strconv.FormatInt(int64(overview.ExpiringCredentials), 10))
	b.WriteString("\n# HELP maclaw_instances_total Number of instances\n")
	b.WriteString("# TYPE maclaw_instances_total gauge\n")
	b.WriteString("maclaw_instances_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Instances), 10))
	b.WriteString("\n# HELP maclaw_sessions_total Number of sessions\n")
	b.WriteString("# TYPE maclaw_sessions_total gauge\n")
	b.WriteString("maclaw_sessions_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Sessions), 10))
	b.WriteString("\n# HELP maclaw_messages_total Number of messages\n")
	b.WriteString("# TYPE maclaw_messages_total gauge\n")
	b.WriteString("maclaw_messages_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Messages), 10))
	b.WriteString("\n# HELP maclaw_runs_total Number of runs\n")
	b.WriteString("# TYPE maclaw_runs_total gauge\n")
	b.WriteString("maclaw_runs_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Runs), 10))
	b.WriteString("\n# HELP maclaw_snapshots_total Number of persisted service snapshots\n")
	b.WriteString("# TYPE maclaw_snapshots_total gauge\n")
	b.WriteString("maclaw_snapshots_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Snapshots), 10))
	b.WriteString("\n# HELP maclaw_snapshot_bytes_total Total bytes used by persisted service snapshots\n")
	b.WriteString("# TYPE maclaw_snapshot_bytes_total gauge\n")
	b.WriteString("maclaw_snapshot_bytes_total ")
	b.WriteString(strconv.FormatInt(overview.SnapshotBytes, 10))
	b.WriteString("\n# HELP maclaw_audit_events_total Number of audit events\n")
	b.WriteString("# TYPE maclaw_audit_events_total gauge\n")
	b.WriteString("maclaw_audit_events_total ")
	b.WriteString(strconv.FormatInt(int64(overview.AuditEvents), 10))
	b.WriteString("\n# HELP maclaw_auth_token_failed_total Number of failed token exchanges recorded in audit events\n")
	b.WriteString("# TYPE maclaw_auth_token_failed_total counter\n")
	b.WriteString("maclaw_auth_token_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(authFailed)), 10))
	b.WriteString("\n# HELP maclaw_auth_token_rate_limited_total Number of rate-limited token exchanges recorded in audit events\n")
	b.WriteString("# TYPE maclaw_auth_token_rate_limited_total counter\n")
	b.WriteString("maclaw_auth_token_rate_limited_total ")
	b.WriteString(strconv.FormatInt(int64(len(rateLimited)), 10))
	b.WriteString("\n# HELP maclaw_admin_auth_failed_total Number of failed Admin API authentications recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_auth_failed_total counter\n")
	b.WriteString("maclaw_admin_auth_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminAuthFailed)), 10))
	b.WriteString("\n# HELP maclaw_admin_owner_denied_total Number of Admin API owner-only authorization denials recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_owner_denied_total counter\n")
	b.WriteString("maclaw_admin_owner_denied_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminOwnerDenied)), 10))
	b.WriteString("\n# HELP maclaw_admin_login_failed_total Number of failed Admin Web logins recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_login_failed_total counter\n")
	b.WriteString("maclaw_admin_login_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminLoginFailed)), 10))
	b.WriteString("\n# HELP maclaw_admin_login_rate_limited_total Number of rate-limited Admin Web logins recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_login_rate_limited_total counter\n")
	b.WriteString("maclaw_admin_login_rate_limited_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminLoginRateLimited)), 10))
	b.WriteString("\n# HELP maclaw_admin_password_change_failed_total Number of failed Admin Web password changes recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_password_change_failed_total counter\n")
	b.WriteString("maclaw_admin_password_change_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminPasswordChangeFailed)), 10))
	b.WriteString("\n# HELP maclaw_instances_unready_total Number of instances currently not ready\n")
	b.WriteString("# TYPE maclaw_instances_unready_total gauge\n")
	b.WriteString("maclaw_instances_unready_total ")
	b.WriteString(strconv.FormatInt(int64(len(alerts.UnreadyInstances)), 10))
	b.WriteString("\n# HELP maclaw_runs_waiting_for_user_total Number of runs currently waiting for user input\n")
	b.WriteString("# TYPE maclaw_runs_waiting_for_user_total gauge\n")
	b.WriteString("maclaw_runs_waiting_for_user_total ")
	b.WriteString(strconv.FormatInt(int64(len(alerts.WaitingRuns)), 10))
	b.WriteString("\n# HELP maclaw_runs_failed_total Number of runs currently surfaced as failed alerts\n")
	b.WriteString("# TYPE maclaw_runs_failed_total gauge\n")
	b.WriteString("maclaw_runs_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(alerts.FailedRuns)), 10))
	b.WriteString("\n# HELP maclaw_run_succeeded_events_total Number of succeeded run audit events\n")
	b.WriteString("# TYPE maclaw_run_succeeded_events_total counter\n")
	b.WriteString("maclaw_run_succeeded_events_total ")
	b.WriteString(strconv.FormatInt(int64(len(runSucceeded)), 10))
	b.WriteString("\n# HELP maclaw_run_failed_events_total Number of failed run audit events\n")
	b.WriteString("# TYPE maclaw_run_failed_events_total counter\n")
	b.WriteString("maclaw_run_failed_events_total ")
	b.WriteString(strconv.FormatInt(int64(len(runFailed)), 10))
	b.WriteString("\n# HELP maclaw_async_jobs_total Number of async jobs by lifecycle status\n")
	b.WriteString("# TYPE maclaw_async_jobs_total gauge\n")
	for _, status := range []asyncJobStatus{asyncJobStatusPending, asyncJobStatusRunning, asyncJobStatusSucceeded, asyncJobStatusFailed, asyncJobStatusCanceled, asyncJobStatusUnknown} {
		b.WriteString("maclaw_async_jobs_total{status=\"")
		b.WriteString(string(status))
		b.WriteString("\"} ")
		b.WriteString(strconv.FormatInt(int64(jobCounts[status]), 10))
		b.WriteString("\n")
	}
	b.WriteString("# HELP maclaw_async_jobs_persistence_healthy Whether async job state can be durably persisted\n")
	b.WriteString("# TYPE maclaw_async_jobs_persistence_healthy gauge\n")
	if s.jobs == nil || s.jobs.persistenceHealthy() {
		b.WriteString("maclaw_async_jobs_persistence_healthy 1\n")
	} else {
		b.WriteString("maclaw_async_jobs_persistence_healthy 0\n")
	}
	if s.svc != nil {
		appendRuntimeMetricsPrometheus(&b, s.svc.RuntimeMetrics().Snapshot())
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

func (s *HTTPServer) loadAdminRiskEvents(ctx context.Context, since, until *time.Time) ([]adminRiskEvent, error) {
	audit, err := s.svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Since: since, Until: until})
	if err != nil {
		return nil, err
	}
	items := buildAdminRiskEvents(s.svc.DataRoot(), audit)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func filterRiskEventsBySeverity(items []adminRiskEvent, severity string) []adminRiskEvent {
	filtered := items[:0]
	for _, item := range items {
		if item.Severity == severity {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterRiskEventsByKind(items []adminRiskEvent, kind string) []adminRiskEvent {
	filtered := items[:0]
	for _, item := range items {
		if item.Kind == kind {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func isValidRiskSeverity(severity string) bool {
	switch severity {
	case "high", "medium", "low":
		return true
	default:
		return false
	}
}

func buildAdminRiskEvents(dataRoot string, audit []agentservice.AuditEvent) []adminRiskEvent {
	items := []adminRiskEvent{}
	now := time.Now().UTC()
	mode, _ := effectiveSandboxMode(dataRoot)
	strict, _ := effectiveSandboxStrict(dataRoot)
	if mode == "none" {
		items = append(items, adminRiskEvent{ID: "config:sandbox:none", Severity: "high", Kind: "sandbox_disabled", Summary: "Sandbox mode is none; local execution is not protected.", ResourceType: "sandbox", ResourceID: "runtime", CreatedAt: now})
	} else if !strict {
		items = append(items, adminRiskEvent{ID: "config:sandbox:not_strict", Severity: "medium", Kind: "sandbox_not_strict", Summary: "Sandbox strict mode is disabled; execution may fall back when sandbox is unavailable.", ResourceType: "sandbox", ResourceID: "runtime", CreatedAt: now})
	}
	if adminEnvBool("MACLAW_ALLOW_INSECURE_HTTP", false) {
		items = append(items, adminRiskEvent{ID: "config:http:insecure", Severity: "high", Kind: "insecure_http", Summary: "Non-loopback plaintext HTTP is allowed by service config.", ResourceType: "service_config", ResourceID: "allow_insecure_http", CreatedAt: now})
	}
	for _, event := range audit {
		if risk, ok := riskEventFromAudit(dataRoot, event); ok {
			items = append(items, risk)
		}
	}
	return items
}

func riskEventFromAudit(dataRoot string, event agentservice.AuditEvent) (adminRiskEvent, bool) {
	severity := ""
	kind := ""
	summary := ""
	switch event.Action {
	case "auth.token_rate_limited", "admin.login_rate_limited":
		severity, kind, summary = "high", "auth_rate_limited", "Authentication rate limit was triggered."
	case "auth.token_failed", "admin.auth_failed", "admin.login_failed", "admin.bootstrap_failed", "admin.password_change_failed":
		severity, kind, summary = "medium", "auth_failed", "Authentication or admin credential validation failed."
	case "web.launch_token.rejected":
		severity, kind, summary = "medium", "web_launch_token_rejected", "A user web launch token was rejected during exchange."
	case "admin.owner_required_failed":
		severity, kind, summary = "medium", "admin_authorization_denied", "An Admin Web operator attempted an owner-only operation."
	case "admin.sandbox_diagnose_failed", "admin.sandbox_startup_diagnose_failed", "admin.sandbox_smoke_test_failed":
		severity, kind, summary = "high", "sandbox_failed", "Sandbox verification failed."
	case "admin.sandbox_install_failed":
		severity, kind, summary = "high", "sandbox_install_failed", "Sandbox installation failed."
	case "admin.service_state_exported":
		if event.Metadata["include_secrets"] == "true" {
			severity, kind, summary = "high", "service_state_secrets_exported", "Service state was exported with secrets included."
		} else {
			severity, kind, summary = "medium", "service_state_exported", "Service state was exported."
		}
	case "admin.service_state_imported":
		if event.Metadata["dry_run"] == "true" {
			severity, kind, summary = "medium", "service_state_import_planned", "Service state import dry-run was performed."
		} else {
			severity, kind, summary = "high", "service_state_imported", "Service state was imported into this server."
		}
	case "admin.snapshot_created":
		if event.Metadata["include_secrets"] == "true" {
			severity, kind, summary = "high", "snapshot_secrets_created", "A service snapshot was created with secrets included."
		} else {
			severity, kind, summary = "medium", "snapshot_created", "A service snapshot was created."
		}
	case "admin.snapshot_restored", "snapshot.restored":
		if event.Metadata["dry_run"] == "true" {
			severity, kind, summary = "medium", "snapshot_restore_planned", "Snapshot restore dry-run was performed."
		} else {
			severity, kind, summary = "high", "snapshot_restored", "A service snapshot was restored."
		}
	case "admin.credential_created":
		severity, kind, summary = "high", "credential_created", "An admin credential was created and one-time secrets may have been returned."
	case "admin.credential_key_rotated", "admin.credential_secret_rotated":
		severity, kind, summary = "high", "credential_rotated", "An admin credential key or secret was rotated."
	case "admin.credential_updated":
		severity, kind, summary = "medium", "credential_updated", "An admin credential was updated."
	case "admin.credential_revoked":
		severity, kind, summary = "medium", "credential_revoked", "An admin credential was revoked."
	case "admin.sandbox_config_updated", "admin.sandbox_config_rolled_back", "admin.sandbox_profile_updated", "admin.sandbox_profile_deleted", "admin.sandbox_report_deleted":
		severity, kind, summary = "medium", "sandbox_admin_changed", "Sandbox admin configuration or diagnostics were changed."
	case "admin.sandbox_install_started", "admin.sandbox_install_succeeded":
		severity, kind, summary = "high", "sandbox_install_changed", "Sandbox installation was requested or completed from Admin Web."
	case "admin.service_config_draft_updated", "admin.service_config_draft_cleared", "admin.service_config_export_plan":
		severity, kind, summary = "medium", "service_config_changed", "Service configuration draft or export plan was changed."
	case "admin.knowledge_access_cross_tenant_updated", "admin.knowledge_access_user_updated", "admin.knowledge_access_user_deleted", "admin.knowledge_access_public_library_attached", "admin.knowledge_access_public_library_detached", "admin.knowledge_tenant_cleared", "admin.knowledge_user_cleared", "admin.knowledge_user_clear_failed":
		severity, kind, summary = "medium", "knowledge_policy_changed", "Knowledge access policy or tenant knowledge data was changed."
	case "admin.public_knowledge_library_created", "admin.public_knowledge_library_deleted", "admin.public_knowledge_import_text", "admin.public_knowledge_import_urls", "admin.public_knowledge_import_file":
		severity, kind, summary = "medium", "public_knowledge_changed", "A public knowledge library or its imported sources were changed."
	case "admin.skill_sources_global_updated", "admin.skill_sources_tenant_updated", "admin.skill_sources_tenant_deleted", "admin.skill_sources_tenant_user_updated", "admin.skill_sources_tenant_user_deleted":
		severity, kind, summary = "medium", "skill_source_policy_changed", "Skill source policy was changed."
	case "admin.support_bundle_downloaded", "admin.sandbox_support_bundle_downloaded":
		severity, kind, summary = "medium", "diagnostics_bundle_downloaded", "An admin downloaded a troubleshooting bundle."
	case "admin.logs_rotate":
		severity, kind, summary = "medium", "log_rotated", "An admin log source was rotated."
	case "admin.job_cancel":
		severity, kind, summary = "medium", "job_canceled", "An admin canceled a background job."
	case "admin.runtime_gc":
		severity, kind, summary = "low", "runtime_gc", "An admin triggered runtime garbage collection."
	}
	if severity == "" {
		return adminRiskEvent{}, false
	}
	return adminRiskEvent{ID: event.ID, Severity: severity, Kind: kind, Summary: summary, Action: event.Action, ResourceType: event.ResourceType, ResourceID: redactSupportBundleValue(dataRoot, event.ResourceID), Metadata: redactSupportBundleMetadata(dataRoot, event.Metadata), CreatedAt: event.CreatedAt}, true
}

func countRiskEventsBySeverity(items []adminRiskEvent) map[string]int {
	out := map[string]int{}
	for _, item := range items {
		out[item.Severity]++
	}
	return out
}

func countRiskEventsByKind(items []adminRiskEvent) map[string]int {
	out := map[string]int{}
	for _, item := range items {
		out[item.Kind]++
	}
	return out
}

func sanitizeExportServiceStateForAdminAPI(dataRoot string, in agentservice.ExportServiceStateOutput) agentservice.ExportServiceStateOutput {
	for ui := range in.Users {
		for ii := range in.Users[ui].Instances {
			in.Users[ui].Instances[ii].Instance = sanitizeInstanceForAdminAPI(dataRoot, in.Users[ui].Instances[ii].Instance)
		}
	}
	in.AuditEvents = redactAuditEventsForAdminAPI(dataRoot, in.AuditEvents)
	return in
}

func sanitizeAdminAlertsForAdminAPI(dataRoot string, in agentservice.AdminAlerts) agentservice.AdminAlerts {
	for i := range in.UnreadyInstances {
		in.UnreadyInstances[i] = sanitizeInstanceForAdminAPI(dataRoot, in.UnreadyInstances[i])
	}
	return in
}

func sanitizeTenantSummaryForAdminAPI(in agentservice.TenantSummary) agentservice.TenantSummary {
	for i := range in.UserSummaries {
		in.UserSummaries[i].DataDir = ""
	}
	return in
}

func sanitizeUsageSummaryForAPI(in *agentservice.UsageSummary) agentservice.UsageSummary {
	if in == nil {
		return agentservice.UsageSummary{}
	}
	out := *in
	out.DataDir = ""
	return out
}

func sanitizeInstanceForAPI(dataRoot string, inst *agentservice.Instance) agentservice.Instance {
	if inst == nil {
		return agentservice.Instance{}
	}
	return sanitizeInstanceForAdminAPI(dataRoot, *inst)
}

func sanitizeInstancesForAPI(dataRoot string, items []agentservice.Instance) []agentservice.Instance {
	out := make([]agentservice.Instance, len(items))
	for i := range items {
		out[i] = sanitizeInstanceForAdminAPI(dataRoot, items[i])
	}
	return out
}

func sanitizeRunPtrForAPI(dataRoot string, run *agentservice.Run) agentservice.Run {
	if run == nil {
		return agentservice.Run{}
	}
	return sanitizeRunForAPI(dataRoot, *run)
}

func sanitizeRunForAPI(dataRoot string, run agentservice.Run) agentservice.Run {
	run.Error = redactSupportBundleText(dataRoot, run.Error)
	run.Metadata = redactSupportBundleMetadata(dataRoot, run.Metadata)
	return run
}

func sanitizeRunsForAPI(dataRoot string, items []agentservice.Run) []agentservice.Run {
	out := make([]agentservice.Run, len(items))
	for i := range items {
		out[i] = sanitizeRunForAPI(dataRoot, items[i])
	}
	return out
}

func sanitizeRunStreamSnapshotForAPI(dataRoot string, snapshot *runStreamSnapshot) *runStreamSnapshot {
	if snapshot == nil {
		return nil
	}
	out := *snapshot
	if snapshot.Run != nil {
		run := sanitizeRunPtrForAPI(dataRoot, snapshot.Run)
		out.Run = &run
	}
	return &out
}

func sanitizeAgentCapabilitiesForAPI(dataRoot string, caps *agentservice.AgentCapabilities) agentservice.AgentCapabilities {
	if caps == nil {
		return agentservice.AgentCapabilities{}
	}
	out := *caps
	if len(caps.Metadata) > 0 {
		out.Metadata = make(map[string]string, len(caps.Metadata))
		for key, value := range caps.Metadata {
			if strings.EqualFold(key, "workspace_dir") {
				continue
			}
			out.Metadata[key] = redactSupportBundleText(dataRoot, value)
		}
	}
	return out
}

func sanitizeMCPServerViewForAPI(dataRoot string, in agentservice.MCPServerView) agentservice.MCPServerView {
	in.EndpointURL = redactEndpointForAPI(dataRoot, in.EndpointURL)
	in.Command = redactSupportBundleValue(dataRoot, in.Command)
	for i := range in.Args {
		in.Args[i] = redactSupportBundleText(dataRoot, in.Args[i])
	}
	return in
}

func sanitizeMCPServerViewsForAPI(dataRoot string, items []agentservice.MCPServerView) []agentservice.MCPServerView {
	out := make([]agentservice.MCPServerView, len(items))
	for i := range items {
		out[i] = sanitizeMCPServerViewForAPI(dataRoot, items[i])
	}
	return out
}

func sanitizeMCPServerViewPtrForAPI(dataRoot string, in *agentservice.MCPServerView) agentservice.MCPServerView {
	if in == nil {
		return agentservice.MCPServerView{}
	}
	return sanitizeMCPServerViewForAPI(dataRoot, *in)
}
func sanitizeSkillEntryForAPI(dataRoot string, in corelib.NLSkillEntry) corelib.NLSkillEntry {
	in.SkillDir = ""
	in.Description = redactSupportBundleText(dataRoot, in.Description)
	in.Content = redactSupportBundleText(dataRoot, in.Content)
	in.SourceProject = redactEndpointForAPI(dataRoot, in.SourceProject)
	in.LastError = redactSupportBundleText(dataRoot, in.LastError)
	for i := range in.Triggers {
		in.Triggers[i] = redactSupportBundleText(dataRoot, in.Triggers[i])
	}
	for i := range in.RequiredCredentialFiles {
		in.RequiredCredentialFiles[i] = redactSupportBundleValue(dataRoot, in.RequiredCredentialFiles[i])
	}
	for i := range in.Steps {
		in.Steps[i] = sanitizeSkillStepForAPI(dataRoot, in.Steps[i])
	}
	for i := range in.Operations {
		in.Operations[i].Description = redactSupportBundleText(dataRoot, in.Operations[i].Description)
	}
	for i := range in.Params {
		in.Params[i].Description = redactSupportBundleText(dataRoot, in.Params[i].Description)
		in.Params[i].Default = redactSupportBundleText(dataRoot, in.Params[i].Default)
	}
	for i := range in.SolidificationCandidates {
		in.SolidificationCandidates[i].ScriptPath = redactSupportBundleValue(dataRoot, in.SolidificationCandidates[i].ScriptPath)
	}
	for i := range in.RepairHistory {
		in.RepairHistory[i].Explanation = redactSupportBundleText(dataRoot, in.RepairHistory[i].Explanation)
	}
	for i := range in.References {
		in.References[i].Filename = redactSupportBundleValue(dataRoot, in.References[i].Filename)
		in.References[i].Description = redactSupportBundleText(dataRoot, in.References[i].Description)
	}
	for i := range in.Pipeline {
		in.Pipeline[i].Params = sanitizeStringMapForAPI(dataRoot, in.Pipeline[i].Params)
		in.Pipeline[i].CheckpointMessage = redactSupportBundleText(dataRoot, in.Pipeline[i].CheckpointMessage)
	}
	return in
}

func sanitizeSkillEntryPtrForAPI(dataRoot string, in *corelib.NLSkillEntry) corelib.NLSkillEntry {
	if in == nil {
		return corelib.NLSkillEntry{}
	}
	return sanitizeSkillEntryForAPI(dataRoot, *in)
}

func sanitizeSkillEntriesForAPI(dataRoot string, items []corelib.NLSkillEntry) []corelib.NLSkillEntry {
	out := make([]corelib.NLSkillEntry, len(items))
	for i := range items {
		out[i] = sanitizeSkillEntryForAPI(dataRoot, items[i])
	}
	return out
}

func sanitizeSkillStepForAPI(dataRoot string, in corelib.NLSkillStep) corelib.NLSkillStep {
	in.Action = redactSupportBundleText(dataRoot, in.Action)
	in.Params = sanitizeAnyMapForAPI(dataRoot, in.Params)
	in.Name = redactSupportBundleText(dataRoot, in.Name)
	in.When = redactSupportBundleText(dataRoot, in.When)
	in.Capture = sanitizeStringMapForAPI(dataRoot, in.Capture)
	if in.FallbackStep != nil {
		fallback := sanitizeSkillStepForAPI(dataRoot, *in.FallbackStep)
		in.FallbackStep = &fallback
	}
	return in
}

func sanitizeAnyMapForAPI(dataRoot string, in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = sanitizeAnyValueForAPI(dataRoot, key, value)
	}
	return out
}

func sanitizeStringMapForAPI(dataRoot string, in map[string]string) map[string]string {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		if supportBundleSensitiveKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = redactSupportBundleText(dataRoot, value)
	}
	return out
}

func sanitizeAnyValueForAPI(dataRoot, key string, value interface{}) interface{} {
	switch v := value.(type) {
	case string:
		if supportBundleSensitiveKey(key) {
			return "[redacted]"
		}
		return redactSupportBundleText(dataRoot, v)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i := range v {
			out[i] = sanitizeAnyValueForAPI(dataRoot, key, v[i])
		}
		return out
	case map[string]interface{}:
		return sanitizeAnyMapForAPI(dataRoot, v)
	case map[string]string:
		return sanitizeStringMapForAPI(dataRoot, v)
	default:
		return value
	}
}

func sanitizeSkillSearchResultsForAPI(dataRoot string, items []agentservice.SkillSearchResult) []agentservice.SkillSearchResult {
	out := make([]agentservice.SkillSearchResult, len(items))
	for i := range items {
		item := items[i]
		item.Description = redactSupportBundleText(dataRoot, item.Description)
		item.RepoURL = redactEndpointForAPI(dataRoot, item.RepoURL)
		item.RawURL = redactEndpointForAPI(dataRoot, item.RawURL)
		item.FilePath = redactSupportBundleValue(dataRoot, item.FilePath)
		out[i] = item
	}
	return out
}

func sanitizeSkillValidateResultForAPI(dataRoot string, in *agentservice.SkillValidateResult) agentservice.SkillValidateResult {
	if in == nil {
		return agentservice.SkillValidateResult{}
	}
	out := *in
	out.Report = sanitizePortabilityReportForAPI(dataRoot, out.Report)
	out.SummaryText = redactSupportBundleText(dataRoot, out.SummaryText)
	return out
}

func sanitizeSkillImproveResultForAPI(dataRoot string, in *agentservice.SkillImproveResult) agentservice.SkillImproveResult {
	if in == nil {
		return agentservice.SkillImproveResult{}
	}
	out := *in
	out.ReportBefore = sanitizePortabilityReportForAPI(dataRoot, out.ReportBefore)
	out.ReportAfter = sanitizePortabilityReportForAPI(dataRoot, out.ReportAfter)
	out.SummaryText = redactSupportBundleText(dataRoot, out.SummaryText)
	for i := range out.Changes {
		out.Changes[i].File = redactSupportBundleValue(dataRoot, out.Changes[i].File)
		out.Changes[i].Original = redactSupportBundleText(dataRoot, out.Changes[i].Original)
		out.Changes[i].Replacement = redactSupportBundleText(dataRoot, out.Changes[i].Replacement)
	}
	return out
}

func sanitizePortabilityReportForAPI(dataRoot string, in *cskill.PortabilityReport) *cskill.PortabilityReport {
	if in == nil {
		return nil
	}
	out := *in
	out.SkillDir = ""
	for i := range out.Issues {
		out.Issues[i].Message = redactSupportBundleText(dataRoot, out.Issues[i].Message)
		out.Issues[i].File = redactSupportBundleValue(dataRoot, out.Issues[i].File)
		out.Issues[i].Suggestion = redactSupportBundleText(dataRoot, out.Issues[i].Suggestion)
	}
	return &out
}

func sanitizeSkillUploadResultForAPI(dataRoot string, in *agentservice.SkillUploadResult) agentservice.SkillUploadResult {
	if in == nil {
		return agentservice.SkillUploadResult{}
	}
	out := *in
	out.SubmissionID = redactSupportBundleValue(dataRoot, out.SubmissionID)
	out.Status = redactSupportBundleText(dataRoot, out.Status)
	return out
}

func sanitizeSkillSubmissionStatusForAPI(dataRoot string, in *agentservice.SkillSubmissionStatus) agentservice.SkillSubmissionStatus {
	if in == nil {
		return agentservice.SkillSubmissionStatus{}
	}
	out := *in
	out.Status = redactSupportBundleText(dataRoot, out.Status)
	out.ErrorMsg = redactSupportBundleText(dataRoot, out.ErrorMsg)
	return out
}
func sanitizeConfigValidationForAPI(dataRoot string, in agentservice.ConfigValidationResult) agentservice.ConfigValidationResult {
	for i := range in.Issues {
		in.Issues[i].Key = redactSupportBundleValue(dataRoot, in.Issues[i].Key)
		in.Issues[i].Message = redactSupportBundleText(dataRoot, in.Issues[i].Message)
	}
	return in
}

func sanitizeConfigValidationPtrForAPI(dataRoot string, in *agentservice.ConfigValidationResult) agentservice.ConfigValidationResult {
	if in == nil {
		return agentservice.ConfigValidationResult{}
	}
	return sanitizeConfigValidationForAPI(dataRoot, *in)
}

func sanitizeConfigTestResultForAPI(dataRoot string, in *agentservice.ConfigTestResult) agentservice.ConfigTestResult {
	if in == nil {
		return agentservice.ConfigTestResult{}
	}
	out := *in
	out.Message = redactSupportBundleText(dataRoot, out.Message)
	out.Error = redactSupportBundleText(dataRoot, out.Error)
	out.Endpoint = redactEndpointForAPI(dataRoot, out.Endpoint)
	if out.Validation != nil {
		validation := sanitizeConfigValidationForAPI(dataRoot, *out.Validation)
		out.Validation = &validation
	}
	return out
}

func redactEndpointForAPI(dataRoot, endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return endpoint
	}
	if dataRoot = strings.TrimSpace(dataRoot); dataRoot != "" {
		base := supportBundlePathBase(dataRoot)
		for _, variant := range supportBundlePathRedactionVariants(dataRoot) {
			endpoint = strings.ReplaceAll(endpoint, variant, base)
		}
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return redactSupportBundleValue(dataRoot, endpoint)
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "[redacted]")
		}
	}
	query := u.Query()
	for key := range query {
		if supportBundleSensitiveKey(key) {
			query.Set(key, "[redacted]")
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func sanitizeInstanceForAdminAPI(dataRoot string, inst agentservice.Instance) agentservice.Instance {
	inst.DataDir = ""
	inst.RuntimeDir = ""
	inst.Workspace = ""
	inst.ReadyReason = redactSupportBundleText(dataRoot, inst.ReadyReason)
	inst.Readiness.Reason = redactSupportBundleText(dataRoot, inst.Readiness.Reason)
	inst.ConfigValidation = sanitizeConfigValidationForAPI(dataRoot, inst.ConfigValidation)
	return inst
}

func sanitizeInstanceSummaryForAPI(dataRoot string, in *agentservice.InstanceSummary) agentservice.InstanceSummary {
	if in == nil {
		return agentservice.InstanceSummary{}
	}
	out := *in
	out.ReadyReason = redactSupportBundleText(dataRoot, out.ReadyReason)
	return out
}

func sanitizeInstanceBootstrapForAPI(dataRoot string, in *agentservice.InstanceBootstrap) agentservice.InstanceBootstrap {
	if in == nil {
		return agentservice.InstanceBootstrap{}
	}
	out := *in
	out.DataDir = ""
	out.RuntimeDir = ""
	out.WorkspaceDir = ""
	out.ConversationStorePath = ""
	out.ConfirmationStorePath = ""
	for key, value := range out.Metadata {
		out.Metadata[key] = redactSupportBundleText(dataRoot, value)
	}
	return out
}

func sanitizeTenantRetirePlanForAdminAPI(dataRoot string, in agentservice.TenantRetirePlan) agentservice.TenantRetirePlan {
	in.Export = sanitizeExportServiceStateForAdminAPI(dataRoot, in.Export)
	return in
}

func sanitizeUserRetirePlanForAdminAPI(dataRoot string, in agentservice.UserRetirePlan) agentservice.UserRetirePlan {
	in.Export = sanitizeExportServiceStateForAdminAPI(dataRoot, in.Export)
	return in
}
func sanitizeServiceSnapshotForAdminAPI(snapshot agentservice.ServiceSnapshot) agentservice.ServiceSnapshot {
	snapshot.Path = ""
	return snapshot
}

func sanitizeServiceSnapshotsForAdminAPI(items []agentservice.ServiceSnapshot) []agentservice.ServiceSnapshot {
	out := make([]agentservice.ServiceSnapshot, len(items))
	for i, item := range items {
		out[i] = sanitizeServiceSnapshotForAdminAPI(item)
	}
	return out
}

func sanitizeServiceSnapshotEnvelopeForAdminAPI(dataRoot string, in *agentservice.ServiceSnapshotEnvelope) agentservice.ServiceSnapshotEnvelope {
	if in == nil {
		return agentservice.ServiceSnapshotEnvelope{}
	}
	out := *in
	out.Snapshot = sanitizeServiceSnapshotForAdminAPI(out.Snapshot)
	out.Data = sanitizeExportServiceStateForAdminAPI(dataRoot, out.Data)
	return out
}

func sanitizeRestoreServiceSnapshotOutputForAdminAPI(dataRoot string, in *agentservice.RestoreServiceSnapshotOutput) agentservice.RestoreServiceSnapshotOutput {
	if in == nil {
		return agentservice.RestoreServiceSnapshotOutput{}
	}
	out := *in
	out.Snapshot = sanitizeServiceSnapshotForAdminAPI(out.Snapshot)
	out.Import = sanitizeImportServiceStateOutputForAdminAPI(dataRoot, &out.Import)
	return out
}

func sanitizeImportServiceStateOutputForAdminAPI(dataRoot string, in *agentservice.ImportServiceStateOutput) agentservice.ImportServiceStateOutput {
	if in == nil {
		return agentservice.ImportServiceStateOutput{}
	}
	out := *in
	for i := range out.Plan {
		out.Plan[i].ResourceID = redactSupportBundleValue(dataRoot, out.Plan[i].ResourceID)
		out.Plan[i].Message = redactSupportBundleText(dataRoot, out.Plan[i].Message)
	}
	for i := range out.Conflicts {
		out.Conflicts[i] = redactSupportBundleText(dataRoot, out.Conflicts[i])
	}
	for i := range out.Warnings {
		out.Warnings[i] = redactSupportBundleText(dataRoot, out.Warnings[i])
	}
	return out
}

func sanitizePruneServiceSnapshotsOutputForAdminAPI(in *agentservice.PruneServiceSnapshotsOutput) agentservice.PruneServiceSnapshotsOutput {
	if in == nil {
		return agentservice.PruneServiceSnapshotsOutput{}
	}
	out := *in
	out.KeptSnapshots = sanitizeServiceSnapshotsForAdminAPI(out.KeptSnapshots)
	out.Snapshots = sanitizeServiceSnapshotsForAdminAPI(out.Snapshots)
	return out
}
func tenantAuditMetadata(r *http.Request, tenant *agentservice.Tenant) map[string]string {
	return map[string]string{
		"name":             tenant.Name,
		"status":           string(tenant.Status),
		"delete_protected": strconv.FormatBool(tenant.DeleteProtected),
		"remote_ip":        requestClientIP(r),
	}
}

func userAuditMetadata(r *http.Request, user *agentservice.User) map[string]string {
	return map[string]string{
		"tenant_id":        user.TenantID,
		"name":             user.Name,
		"email":            user.Email,
		"status":           string(user.Status),
		"delete_protected": strconv.FormatBool(user.DeleteProtected),
		"remote_ip":        requestClientIP(r),
	}
}

func credentialAuditMetadata(r *http.Request, cred *agentservice.Credential) map[string]string {
	metadata := map[string]string{
		"tenant_id":           cred.TenantID,
		"user_id":             cred.UserID,
		"name":                cred.Name,
		"status":              string(cred.Status),
		"api_key_prefix":      cred.APIKeyPrefix,
		"token_version":       strconv.Itoa(cred.TokenVersion),
		"has_expires_at":      strconv.FormatBool(cred.ExpiresAt != nil),
		"returned_api_key":    strconv.FormatBool(cred.APIKey != ""),
		"returned_api_secret": strconv.FormatBool(cred.APISecret != ""),
		"remote_ip":           requestClientIP(r),
	}
	if cred.ExpiresAt != nil {
		metadata["expires_at"] = cred.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return metadata
}

func (s *HTTPServer) updateTenantLifecycleStatus(w http.ResponseWriter, r *http.Request, status agentservice.TenantStatus, action string) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	out, err := s.svc.UpdateTenant(r.Context(), r.PathValue("tenantId"), agentservice.UpdateTenantInput{Status: &status})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if status == agentservice.TenantStatusDisabled {
		s.stopWeixinRuntimesForTenant(r.Context(), out.ID)
		s.stopIMRuntimesForTenant(r.Context(), out.ID)
		s.stopThirdPartyIMForTenant(out.ID)
	} else if status == agentservice.TenantStatusActive {
		s.startConfiguredWeixinRuntimesForTenant(r.Context(), out.ID)
		s.startConfiguredIMRuntimesForTenant(r.Context(), out.ID)
	}
	_ = s.recordAdminAudit(r.Context(), action, "tenant", out.ID, map[string]string{"status": string(out.Status), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) adminUserPrincipal(r *http.Request) agentservice.Principal {
	return agentservice.Principal{TenantID: r.PathValue("tenantId"), UserID: r.PathValue("userId")}
}

func (s *HTTPServer) updateUserLifecycleStatus(w http.ResponseWriter, r *http.Request, status agentservice.UserStatus, action string) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	out, err := s.svc.UpdateUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), agentservice.UpdateUserInput{Status: &status})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	p := agentservice.Principal{TenantID: out.TenantID, UserID: out.ID}
	if status == agentservice.UserStatusDisabled {
		s.stopWeixinRuntimeForPrincipal(p)
		s.stopIMRuntimeForPrincipal(p)
		s.stopThirdPartyIMForPrincipal(p)
	} else if status == agentservice.UserStatusActive {
		s.syncWeixinRuntimeFromRawConfig(r.Context(), p)
		s.syncIMRuntimeFromRawConfig(r.Context(), p)
	}
	_ = s.recordAdminAudit(r.Context(), action, "user", out.ID, map[string]string{"tenant_id": out.TenantID, "status": string(out.Status), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, out)
}

func nonDeleteProtectionBlockers(blockers []agentservice.DeleteBlocker) []agentservice.DeleteBlocker {
	out := make([]agentservice.DeleteBlocker, 0, len(blockers))
	for _, blocker := range blockers {
		if blocker.Kind != "delete_protected" {
			out = append(out, blocker)
		}
	}
	return out
}
func (s *HTTPServer) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	var in agentservice.IssueTokenInput
	if !decodeJSON(w, r, &in) {
		return
	}
	clientIP := requestClientIP(r)
	limitKey := clientIP + ":" + strings.TrimSpace(in.APIKey)
	now := time.Now()
	if allowed, retryAfter := s.authLimiter.AllowWithRetry(limitKey, now); !allowed {
		_ = s.svc.RecordTokenRateLimit(r.Context(), in.APIKey, clientIP)
		writeRateLimitError(w, retryAfter)
		return
	}
	out, err := s.svc.IssueToken(r.Context(), in)
	if err != nil {
		if errors.Is(err, agentservice.ErrUnauthorized) || errors.Is(err, agentservice.ErrForbidden) {
			blockFor := s.authLimiter.RegisterFailure(limitKey, now)
			_ = s.svc.RecordTokenAuthFailure(r.Context(), in.APIKey, clientIP, err.Error())
			if blockFor > 0 {
				writeRateLimitError(w, blockFor)
				return
			}
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	s.authLimiter.ResetFailures(limitKey)
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetMe(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetMe(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) thirdPartyRuntimeStatus(ctx context.Context, p agentservice.Principal) srvIMRuntimeStatus {
	now := time.Now().UTC()
	raw, err := s.svc.GetRawUserConfig(ctx, p)
	if err != nil {
		if errors.Is(err, agentservice.ErrUserConfigNotFound) {
			return srvIMRuntimeStatus{Status: srvWeixinStatusDisabled, UpdatedAt: now}
		}
		return srvIMRuntimeStatus{Status: srvWeixinStatusError, LastError: err.Error(), UpdatedAt: now}
	}
	if raw == nil || !raw.AppConfig.ThirdPartyGatewayEnabled {
		return srvIMRuntimeStatus{Status: srvWeixinStatusDisabled, UpdatedAt: now}
	}
	if strings.TrimSpace(raw.AppConfig.ThirdPartyGatewayToken) == "" {
		return srvIMRuntimeStatus{Status: srvWeixinStatusError, LastError: "third-party gateway token is required", UpdatedAt: now}
	}
	if err := s.validateThirdPartyGatewayTokenUnique(ctx, p, raw.AppConfig); err != nil {
		return srvIMRuntimeStatus{Status: srvWeixinStatusError, LastError: err.Error(), UpdatedAt: now}
	}
	return srvIMRuntimeStatus{Status: srvWeixinStatusConnected, UpdatedAt: now}
}

func weixinQRCodeImageProxyURL(qrcodeURL string) string {
	qrcodeURL = strings.TrimSpace(qrcodeURL)
	if qrcodeURL == "" {
		return ""
	}
	return "/api/v1/im/weixin/qr/image?value=" + url.QueryEscape(qrcodeURL)
}

var weixinQRCodeAllowedImageHosts = map[string]bool{
	"liteapp.weixin.qq.com": true,
}

func validateWeixinQRCodeImageURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("qrcode url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("invalid qrcode url")
	}
	if u.Scheme != "https" {
		return nil, errors.New("qrcode image must use https")
	}
	if !weixinQRCodeAllowedImageHosts[strings.ToLower(u.Hostname())] {
		return nil, errors.New("qrcode image host is not allowed")
	}
	return u, nil
}

const userWeixinQRStatusPollTimeout = 5 * time.Second

func weixinQRPollErrorResponse(err error) map[string]any {
	if weixin.IsQRLoginRetryableError(err) {
		return map[string]any{"status": weixin.QRLoginStatusWait.String(), "retryable": true}
	}
	return map[string]any{"error": err.Error(), "status": "error"}
}

func weixinQRPollMessage(status weixin.QRLoginStatus, result *weixin.QRLoginResult) string {
	status = normalizeWeixinQRPollStatus(status, result)
	msg := strings.TrimSpace(resultMessage(result))
	if status == weixin.QRLoginStatusWait && weixin.IsQRLoginWaitMessage(msg) {
		return ""
	}
	return msg
}

func normalizeWeixinQRPollStatus(status weixin.QRLoginStatus, result *weixin.QRLoginResult) weixin.QRLoginStatus {
	normalized := weixin.NormalizeQRLoginStatus(status)
	if normalized == weixin.QRLoginStatusUnknown && weixin.IsQRLoginWaitMessage(resultMessage(result)) {
		return weixin.QRLoginStatusWait
	}
	return normalized
}

func resultMessage(result *weixin.QRLoginResult) string {
	if result == nil {
		return ""
	}
	return result.Message
}

func (s *HTTPServer) weixinQRBaseURL(ctx context.Context, p agentservice.Principal) (string, error) {
	cfg, _, err := s.currentUserConfigForVisibleMerge(ctx, p)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.WeixinBaseURL) != "" {
		return strings.TrimSpace(cfg.WeixinBaseURL), nil
	}
	return weixin.DefaultBaseURL, nil
}

func (s *HTTPServer) saveWeixinQRLoginConfig(ctx context.Context, p agentservice.Principal, result *weixin.QRLoginResult) error {
	if result == nil {
		return errors.New("weixin login result is nil")
	}
	cfg, _, err := s.currentUserConfigForVisibleMerge(ctx, p)
	if err != nil {
		return err
	}
	cfg.WeixinEnabled = true
	cfg.WeixinToken = result.BotToken
	cfg.WeixinAccountID = result.AccountID
	if strings.TrimSpace(result.BaseURL) != "" {
		cfg.WeixinBaseURL = strings.TrimSpace(result.BaseURL)
	}
	if cfg.WeixinLocalMode == nil {
		local := true
		cfg.WeixinLocalMode = &local
	}
	_, err = s.svc.UpdateUserConfig(ctx, p, cfg)
	return err
}

func parseIMAuditQuery(w http.ResponseWriter, r *http.Request) (agentservice.ListIMAuditMessagesInput, bool) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return agentservice.ListIMAuditMessagesInput{}, false
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return agentservice.ListIMAuditMessagesInput{}, false
	}
	role, ok := parseMessageRole(r.URL.Query().Get("role"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return agentservice.ListIMAuditMessagesInput{}, false
	}
	return agentservice.ListIMAuditMessagesInput{
		Platform: strings.TrimSpace(r.URL.Query().Get("platform")),
		Contact:  strings.TrimSpace(r.URL.Query().Get("contact")),
		Query:    strings.TrimSpace(r.URL.Query().Get("q")),
		Role:     role,
		Since:    since,
		Until:    until,
	}, true
}

func csvSafeCell(value string) string {
	if trimmed := strings.TrimLeft(value, " \t\r\n"); trimmed != "" {
		switch trimmed[0] {
		case '=', '+', '-', '@':
			return "'" + value
		}
	}
	return value
}

func (s *HTTPServer) handleGetUsageSummary(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetUsageSummary(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeUsageSummaryForAPI(out))
}

var userHiddenConfigKeys = coreconfig.UserWebHiddenConfigKeys()

func filterUserConfigSchema(defs []agentservice.ParameterDefinition) []agentservice.ParameterDefinition {
	out := make([]agentservice.ParameterDefinition, 0, len(defs))
	for _, def := range defs {
		if !coreconfig.IsUserWebVisibleField(def.Key) {
			continue
		}
		out = append(out, def)
	}
	return out
}

func writeUserConfigResponse(w http.ResponseWriter, status int, cfg *agentservice.UserConfig) {
	if cfg == nil {
		writeJSON(w, status, map[string]any{"app_config": map[string]any{}})
		return
	}
	writeJSON(w, status, map[string]any{
		"tenant_id":  cfg.TenantID,
		"user_id":    cfg.UserID,
		"app_config": userVisibleAppConfigMap(forceSrvAIAutoEnabledConfig(cfg.AppConfig)),
		"updated_at": cfg.UpdatedAt,
	})
}

func userVisibleAppConfigMap(cfg corelib.AppConfig) map[string]any {
	data, err := json.Marshal(stripUserComplexConfig(cfg))
	if err != nil {
		return map[string]any{}
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]any{}
	}
	allowed := make(map[string]struct{})
	for _, def := range filterUserConfigSchema(agentservice.DefaultParameterDefinitions()) {
		allowed[def.Key] = struct{}{}
	}
	for key := range raw {
		if _, ok := allowed[key]; !ok || isUserWebRetiredSettingsKey(key) {
			delete(raw, key)
		}
	}
	return raw
}

func (s *HTTPServer) userVisibleConfigCandidate(ctx context.Context, p agentservice.Principal, cfg *corelib.AppConfig) (*corelib.AppConfig, error) {
	current, found, err := s.currentUserConfigForVisibleMerge(ctx, p)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		if !found {
			out := corelib.AppConfig{}
			return &out, nil
		}
		out := current
		return &out, nil
	}
	out := *cfg
	if found {
		out = preserveUserComplexConfig(current, out)
	} else {
		out = stripUserComplexConfig(out)
	}
	out = stripUserSharedClientConfig(out)
	return &out, nil
}

func (s *HTTPServer) userVisibleConfigUpdate(ctx context.Context, p agentservice.Principal, cfg corelib.AppConfig) (corelib.AppConfig, error) {
	current, found, err := s.currentUserConfigForVisibleMerge(ctx, p)
	if err != nil {
		return corelib.AppConfig{}, err
	}
	if !found {
		return stripUserSharedClientConfig(stripUserComplexConfig(cfg)), nil
	}
	return stripUserSharedClientConfig(preserveUserComplexConfig(current, cfg)), nil
}

func (s *HTTPServer) currentUserConfigForVisibleMerge(ctx context.Context, p agentservice.Principal) (corelib.AppConfig, bool, error) {
	current, err := s.svc.GetRawUserConfig(ctx, p)
	if err != nil {
		if errors.Is(err, agentservice.ErrUserConfigNotFound) {
			return corelib.AppConfig{}, false, nil
		}
		return corelib.AppConfig{}, false, err
	}
	return current.AppConfig, true, nil
}

func preserveUserComplexConfig(current, next corelib.AppConfig) corelib.AppConfig {
	next = preserveUserFlatLLMConfig(current, next)
	next = preserveUserSharedClientConfig(current, next)
	// Complex fields are governed by the same corelib/config metadata used by
	// the schema.  New structured AppConfig fields therefore get preserved
	// automatically instead of requiring another MaClawSrv field assignment.
	next = preserveAppConfigTaggedFields(current, next, coreconfig.IsUserWebComplexField)
	next = preserveUserInvisibleConfig(current, next)
	return next
}

func preserveUserFlatLLMConfig(current, next corelib.AppConfig) corelib.AppConfig {
	if strings.TrimSpace(next.MaclawLLMUrl) == "" {
		next.MaclawLLMUrl = current.MaclawLLMUrl
	}
	if strings.TrimSpace(next.MaclawLLMKey) == "" || agentservice.IsMaskedSecretPlaceholder(next.MaclawLLMKey) {
		next.MaclawLLMKey = current.MaclawLLMKey
	}
	if strings.TrimSpace(next.MaclawLLMModel) == "" {
		next.MaclawLLMModel = current.MaclawLLMModel
	}
	return next
}

func stripUserComplexConfig(cfg corelib.AppConfig) corelib.AppConfig {
	cfg = preserveAppConfigTaggedFields(corelib.AppConfig{}, cfg, coreconfig.IsUserWebComplexField)
	return stripUserInvisibleConfig(cfg)
}

func preserveUserSharedClientConfig(current, next corelib.AppConfig) corelib.AppConfig {
	return preserveAppConfigTaggedFields(current, next, isUserManagedSharedConfigField)
}

func isUserManagedSharedConfigField(key string) bool {
	if !coreconfig.IsSharedClientField(key) || coreconfig.IsUserWebComplexField(key) {
		return false
	}
	switch strings.TrimSpace(key) {
	case "maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_model":
		return false
	default:
		return true
	}
}

func forceSrvAIAutoEnabledConfig(cfg corelib.AppConfig) corelib.AppConfig {
	cfg.VectorSearchEnabled = true
	cfg.ASREnabled = true
	cfg.TTSEnabled = true
	return cfg
}

func stripUserSharedClientConfig(cfg corelib.AppConfig) corelib.AppConfig {
	return preserveAppConfigTaggedFields(corelib.AppConfig{}, cfg, isUserManagedSharedConfigField)
}

func isUserWebRetiredSettingsKey(key string) bool {
	return coreconfig.IsUserWebRetiredSettingsKey(key)
}

func isUserInvisibleConfigKey(key string) bool {
	if _, hidden := userHiddenConfigKeys[key]; hidden {
		return true
	}
	return isUserWebRetiredSettingsKey(key)
}

func preserveUserInvisibleConfig(current, next corelib.AppConfig) corelib.AppConfig {
	return preserveAppConfigTaggedFields(current, next, isUserInvisibleConfigKey)
}

func stripUserInvisibleConfig(cfg corelib.AppConfig) corelib.AppConfig {
	return preserveAppConfigTaggedFields(corelib.AppConfig{}, cfg, isUserInvisibleConfigKey)
}

func preserveAppConfigTaggedFields(current, next corelib.AppConfig, keep func(string) bool) corelib.AppConfig {
	return coreconfig.CopyAppConfigFields(current, next, keep)
}

// prefersAsyncResponse implements RFC 7240 Prefer token matching while
// accepting the common combined form (for example
// "return=representation, respond-async"). Parameters are ignored and token
// matching is case-insensitive.
func prefersAsyncResponse(values []string) bool {
	return transporthttp.PrefersAsyncResponse(values)
}

func wantsAsyncResponse(r *http.Request) (bool, error) {
	return transporthttp.WantsAsyncResponse(r)
}

type runStreamSnapshot struct {
	Run              *agentservice.Run     `json:"run"`
	Session          *agentservice.Session `json:"session,omitempty"`
	AssistantMessage *agentservice.Message `json:"assistant_message,omitempty"`
}

type runStreamEnvelope struct {
	Type     string                 `json:"type"`
	Snapshot *runStreamSnapshot     `json:"snapshot,omitempty"`
	Event    *agentservice.RunEvent `json:"event,omitempty"`
}

func sanitizeRunEventForAPI(dataRoot string, event agentservice.RunEvent) agentservice.RunEvent {
	if len(event.Payload) == 0 {
		return event
	}
	raw, err := json.Marshal(event.Payload)
	if err != nil {
		event.Payload = nil
		return event
	}
	sanitized := sanitizeCommittedEffectPayload(dataRoot, raw)
	if len(sanitized) == 0 {
		event.Payload = nil
		return event
	}
	var payload map[string]any
	if err := json.Unmarshal(sanitized, &payload); err != nil {
		event.Payload = nil
		return event
	}
	event.Payload = payload
	return event
}

func (s *HTTPServer) loadRunStreamSnapshot(ctx context.Context, p agentservice.Principal, instanceID, runID string) (*runStreamSnapshot, error) {
	run, err := s.svc.GetRun(ctx, p, instanceID, runID)
	if err != nil {
		return nil, err
	}
	snap := &runStreamSnapshot{Run: run}
	if run == nil || strings.TrimSpace(run.SessionID) == "" {
		return sanitizeRunStreamSnapshotForAPI(s.svc.DataRoot(), snap), nil
	}
	sess, err := s.svc.GetSession(ctx, p, instanceID, run.SessionID)
	if err != nil && !errors.Is(err, agentservice.ErrSessionNotFound) {
		return nil, err
	}
	if err == nil {
		snap.Session = sess
	}
	if strings.TrimSpace(run.AssistantMessageID) == "" {
		return sanitizeRunStreamSnapshotForAPI(s.svc.DataRoot(), snap), nil
	}
	messages, err := s.svc.ListMessages(ctx, p, instanceID, run.SessionID, agentservice.ListMessagesInput{})
	if err != nil {
		return nil, err
	}
	for i := range messages {
		if messages[i].ID == run.AssistantMessageID {
			msg := messages[i]
			snap.AssistantMessage = &msg
			break
		}
	}
	return sanitizeRunStreamSnapshotForAPI(s.svc.DataRoot(), snap), nil
}

func writeSSEError(w http.ResponseWriter, flusher http.Flusher, err error, dataRoot string) {
	payload, _ := json.Marshal(map[string]string{"error": redactSupportBundleText(dataRoot, err.Error())})
	_, _ = w.Write([]byte("event: error\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}

func (s *HTTPServer) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setAdminSecurityHeaders(w)
		provided := r.Header.Get("X-MaClaw-Admin-Secret")
		if s.adminSecretAuthorized(provided) {
			ctx := contextWithAdminAuditIdentity(r.Context(), adminAuditIdentity{AuthType: "admin_secret"})
			next(w, r.WithContext(ctx))
			return
		}
		session, user, err := getAdminSessionUser(s.svc.DataRoot(), provided, time.Now().UTC())
		if err != nil {
			_ = s.recordAdminAudit(r.Context(), "admin.auth_failed", "admin_auth", strings.TrimSpace(r.URL.Path), map[string]string{"method": r.Method, "remote_ip": requestClientIP(r)})
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid admin secret"})
			return
		}
		ctx := contextWithAdminAuditIdentity(r.Context(), adminAuditIdentity{AuthType: "admin_session", SessionID: session.ID, UserID: user.ID, Username: user.Username, Role: user.Role})
		next(w, r.WithContext(ctx))
	}
}
func (s *HTTPServer) withPrincipal(next func(http.ResponseWriter, *http.Request, agentservice.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(authz, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": agentservice.ErrUnauthorized.Error()})
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
		if token == "" || strings.Contains(token, " ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": agentservice.ErrUnauthorized.Error()})
			return
		}
		p, err := s.svc.Authenticate(token)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		next(w, r, *p)
	}
}
func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	return true
}
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	return true
}
func (s *HTTPServer) requireSecretExportAccess(w http.ResponseWriter, r *http.Request, includeSecrets bool) bool {
	if !includeSecrets {
		return true
	}
	if !s.requireAdminOwner(w, r) {
		return false
	}
	if err := requireAdminConfirmation(r, "secret export operations"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func parseExportServiceStateInput(r *http.Request) (agentservice.ExportServiceStateInput, error) {
	includeMessages, err := parseOptionalBoolQuery(r, "include_messages")
	if err != nil {
		return agentservice.ExportServiceStateInput{}, err
	}
	includeRuns, err := parseOptionalBoolQuery(r, "include_runs")
	if err != nil {
		return agentservice.ExportServiceStateInput{}, err
	}
	includeAudit, err := parseOptionalBoolQuery(r, "include_audit")
	if err != nil {
		return agentservice.ExportServiceStateInput{}, err
	}
	includeSecrets, err := parseOptionalBoolQuery(r, "include_secrets")
	if err != nil {
		return agentservice.ExportServiceStateInput{}, err
	}
	return agentservice.ExportServiceStateInput{
		IncludeMessages: includeMessages == nil || *includeMessages,
		IncludeRuns:     includeRuns == nil || *includeRuns,
		IncludeAudit:    includeAudit == nil || *includeAudit,
		IncludeSecrets:  includeSecrets != nil && *includeSecrets,
	}, nil
}

func decodeImportStateRequest(w http.ResponseWriter, r *http.Request) (*agentservice.ImportServiceStateRequest, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body is required"})
		return nil, false
	}
	var envelope importStateEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && (envelope.Data.Scope != "" || len(envelope.Data.Tenants) > 0 || len(envelope.Data.Users) > 0 || len(envelope.Data.AuditEvents) > 0) {
		return &agentservice.ImportServiceStateRequest{Data: envelope.Data, Overwrite: envelope.Overwrite, DryRun: envelope.DryRun}, true
	}
	var raw agentservice.ExportServiceStateOutput
	if err := json.Unmarshal(body, &raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return nil, false
	}
	return &agentservice.ImportServiceStateRequest{Data: raw}, true
}

func decodeOptionalAppConfig(w http.ResponseWriter, r *http.Request) (*corelib.AppConfig, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, true
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err == nil {
		if appConfigBody, ok := raw["app_config"]; ok {
			var cfg corelib.AppConfig
			if err := json.Unmarshal(appConfigBody, &cfg); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid app_config body: " + err.Error()})
				return nil, false
			}
			return &cfg, true
		}
	}

	var cfg corelib.AppConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return nil, false
	}
	return &cfg, true
}

func decodeOptionalAppConfigWithBase(w http.ResponseWriter, r *http.Request, base corelib.AppConfig) (*corelib.AppConfig, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, true
	}

	payload := body
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err == nil {
		if appConfigBody, ok := raw["app_config"]; ok {
			payload = appConfigBody
		}
	}
	var nextFields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &nextFields); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid app_config body: " + err.Error()})
		return nil, false
	}
	baseBytes, err := json.Marshal(base)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to prepare config base"})
		return nil, false
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(baseBytes, &merged); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to prepare config base"})
		return nil, false
	}
	for key, value := range nextFields {
		merged[key] = value
	}
	mergedBytes, err := json.Marshal(merged)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid app_config body: " + err.Error()})
		return nil, false
	}
	var cfg corelib.AppConfig
	if err := json.Unmarshal(mergedBytes, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid app_config body: " + err.Error()})
		return nil, false
	}
	return &cfg, true
}

type pageQuery struct {
	Limit  int
	Before time.Time
}

type skillPageQuery struct {
	Limit  int
	Before string
}

type pageMeta struct {
	Limit      int
	HasMore    bool
	NextBefore string
}

const (
	defaultPageLimit = 100
	maxPageLimit     = 500
)

func parseRequiredBoolLikeQuery(r *http.Request, key string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New(key + " must be a boolean")
	}
	return v, nil
}

func requireDeleteConfirmation(r *http.Request) error {
	return requireAdminConfirmation(r, "delete operations")
}

func requireAdminConfirmation(r *http.Request, operation string) error {
	raw := strings.TrimSpace(r.URL.Query().Get("confirm"))
	if raw == "" {
		return errors.New("confirm=true is required for " + operation)
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return errors.New("confirm must be a boolean")
	}
	if !v {
		return errors.New("confirm=true is required for " + operation)
	}
	return nil
}
func parseOptionalBoolQuery(r *http.Request, key string) (*bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, errors.New(key + " must be a boolean")
	}
	return &v, nil
}

func parseOptionalTimeQuery(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	v, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, errors.New(key + " must be an RFC3339 timestamp")
	}
	return &v, nil
}

func parseRequiredTimeQuery(r *http.Request, key string) (time.Time, error) {
	value, err := parseOptionalTimeQuery(r, key)
	if err != nil {
		return time.Time{}, err
	}
	if value == nil {
		return time.Time{}, errors.New(key + " is required")
	}
	return *value, nil
}

func validateOptionalTimeRange(since, until *time.Time) error {
	if since != nil && until != nil && since.After(*until) {
		return errors.New("since must be before or equal to until")
	}
	return nil
}
func filterCredentialsByQuery(items []agentservice.Credential, r *http.Request) ([]agentservice.Credential, error) {
	statusRaw := strings.TrimSpace(r.URL.Query().Get("status"))
	var status agentservice.CredentialStatus
	if statusRaw != "" {
		status = agentservice.CredentialStatus(statusRaw)
		switch status {
		case agentservice.CredentialStatusActive, agentservice.CredentialStatusSuspended, agentservice.CredentialStatusRevoked:
		default:
			return nil, errors.New("status must be active, suspended, or revoked")
		}
	}
	expired, err := parseOptionalBoolQuery(r, "expired")
	if err != nil {
		return nil, err
	}
	expiring, err := parseOptionalBoolQuery(r, "expiring")
	if err != nil {
		return nil, err
	}
	if statusRaw == "" && expired == nil && expiring == nil {
		return items, nil
	}
	now := time.Now().UTC()
	expiringCutoff := now.Add(7 * 24 * time.Hour)
	filtered := make([]agentservice.Credential, 0, len(items))
	for _, item := range items {
		if statusRaw != "" && item.Status != status {
			continue
		}
		isExpired := item.ExpiresAt != nil && !item.ExpiresAt.After(now)
		isExpiring := item.ExpiresAt != nil && item.ExpiresAt.After(now) && !item.ExpiresAt.After(expiringCutoff)
		if expired != nil && isExpired != *expired {
			continue
		}
		if expiring != nil && isExpiring != *expiring {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func parseAsyncJobStatus(raw string, terminalOnly bool) (asyncJobStatus, bool) {
	statusRaw := strings.TrimSpace(raw)
	if statusRaw == "" {
		return "", true
	}
	status := asyncJobStatus(statusRaw)
	if terminalOnly {
		switch status {
		case asyncJobStatusSucceeded, asyncJobStatusFailed, asyncJobStatusCanceled, asyncJobStatusUnknown:
			return status, true
		default:
			return "", false
		}
	}
	switch status {
	case asyncJobStatusPending, asyncJobStatusRunning, asyncJobStatusSucceeded, asyncJobStatusFailed, asyncJobStatusCanceled, asyncJobStatusUnknown:
		return status, true
	default:
		return "", false
	}
}

func parseRunStatus(raw string) (agentservice.RunStatus, bool) {
	statusRaw := strings.TrimSpace(raw)
	if statusRaw == "" {
		return "", true
	}
	status := agentservice.RunStatus(statusRaw)
	switch status {
	case agentservice.RunStatusRunning, agentservice.RunStatusSucceeded, agentservice.RunStatusFailed, agentservice.RunStatusCancelled:
		return status, true
	default:
		return "", false
	}
}

func parseMessageRole(raw string) (agentservice.MessageRole, bool) {
	roleRaw := strings.TrimSpace(raw)
	if roleRaw == "" {
		return "", true
	}
	role := agentservice.MessageRole(roleRaw)
	switch role {
	case agentservice.MessageRoleUser, agentservice.MessageRoleAssistant, agentservice.MessageRoleSystem:
		return role, true
	default:
		return "", false
	}
}

func parseRunResponseSource(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	switch value {
	case "ask_user", "":
		return value, true
	default:
		return "", false
	}
}

func parseTenantStatus(raw string) (agentservice.TenantStatus, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	status := agentservice.TenantStatus(value)
	switch status {
	case agentservice.TenantStatusActive, agentservice.TenantStatusDisabled:
		return status, true
	default:
		return "", false
	}
}

func parseUserStatus(raw string) (agentservice.UserStatus, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	status := agentservice.UserStatus(value)
	switch status {
	case agentservice.UserStatusActive, agentservice.UserStatusDisabled:
		return status, true
	default:
		return "", false
	}
}

func parsePageLimit(r *http.Request) (int, error) {
	return transporthttp.ParsePageLimit(r)
}

func parsePageQuery(r *http.Request) (pageQuery, error) {
	parsed, err := transporthttp.ParsePageQuery(r)
	if err != nil {
		return pageQuery{}, err
	}
	return pageQuery{Limit: parsed.Limit, Before: parsed.Before}, nil
}

func parseSkillPageQuery(r *http.Request) (skillPageQuery, error) {
	limit, err := parsePageLimit(r)
	if err != nil {
		return skillPageQuery{}, err
	}
	return skillPageQuery{Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before"))}, nil
}

func listResponse(items any, meta pageMeta) map[string]any {
	out := map[string]any{
		"items":    items,
		"limit":    meta.Limit,
		"has_more": meta.HasMore,
	}
	if meta.NextBefore != "" {
		out["next_before"] = meta.NextBefore
	}
	return out
}

func paginateInstances(items []agentservice.Instance, page pageQuery) ([]agentservice.Instance, pageMeta) {
	filtered := make([]agentservice.Instance, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateSessions(items []agentservice.Session, page pageQuery) ([]agentservice.Session, pageMeta) {
	filtered := make([]agentservice.Session, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateMessages(items []agentservice.Message, page pageQuery) ([]agentservice.Message, pageMeta) {
	filtered := make([]agentservice.Message, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateIMAuditMessages(items []agentservice.IMAuditMessage, page pageQuery) ([]agentservice.IMAuditMessage, pageMeta) {
	filtered := make([]agentservice.IMAuditMessage, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	limit := page.Limit
	if limit > len(filtered) {
		limit = len(filtered)
	}
	window := filtered[:limit]
	meta := pageMeta{Limit: page.Limit, HasMore: len(filtered) > limit}
	if meta.HasMore && len(window) > 0 {
		meta.NextBefore = window[len(window)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	return window, meta
}

func paginateRuns(items []agentservice.Run, page pageQuery) ([]agentservice.Run, pageMeta) {
	filtered := make([]agentservice.Run, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.StartedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].StartedAt
	})
}

func recordsPageMeta(items []agentservice.StructuredRecord, page pageQuery) ([]agentservice.StructuredRecord, pageMeta) {
	meta := pageMeta{Limit: page.Limit, HasMore: len(items) == page.Limit}
	if meta.HasMore && len(items) > 0 {
		meta.NextBefore = items[len(items)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	return items, meta
}

func paginateTenants(items []agentservice.Tenant, page pageQuery) ([]agentservice.Tenant, pageMeta) {
	filtered := make([]agentservice.Tenant, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateUsers(items []agentservice.User, page pageQuery) ([]agentservice.User, pageMeta) {
	filtered := make([]agentservice.User, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateCredentials(items []agentservice.Credential, page pageQuery) ([]agentservice.Credential, pageMeta) {
	filtered := make([]agentservice.Credential, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateServiceSnapshots(items []agentservice.ServiceSnapshot, page pageQuery) ([]agentservice.ServiceSnapshot, pageMeta) {
	filtered := make([]agentservice.ServiceSnapshot, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}
func paginateAsyncJobs(items []asyncJobRecord, page pageQuery) ([]asyncJobRecord, pageMeta) {
	filtered := make([]asyncJobRecord, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateMCPServers(items []agentservice.MCPServerView, page pageQuery) ([]agentservice.MCPServerView, pageMeta) {
	filtered := make([]agentservice.MCPServerView, 0, len(items))
	for _, item := range items {
		createdAt, ok := parseCursorTime(item.CreatedAt)
		if !ok {
			continue
		}
		if page.Before.IsZero() || createdAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		cursor, _ := parseCursorTime(window[0].CreatedAt)
		return cursor
	})
}

func paginateSkills(items []corelib.NLSkillEntry, page skillPageQuery) ([]corelib.NLSkillEntry, pageMeta) {
	filtered := make([]corelib.NLSkillEntry, 0, len(items))
	before := strings.ToLower(strings.TrimSpace(page.Before))
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		if before == "" || name < before {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	meta := pageMeta{Limit: page.Limit, HasMore: start > 0}
	if meta.HasMore && len(window) > 0 {
		meta.NextBefore = window[0].Name
	}
	return window, meta
}

func paginateAuditEvents(items []agentservice.AuditEvent, page pageQuery) ([]agentservice.AuditEvent, pageMeta) {
	filtered := make([]agentservice.AuditEvent, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func parseCursorTime(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func formatOptionalCursorTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func buildPageMeta(total, start, limit int, cursor func() time.Time) pageMeta {
	meta := pageMeta{Limit: limit, HasMore: start > 0}
	if meta.HasMore {
		meta.NextBefore = cursor().Format(time.RFC3339Nano)
	}
	return meta
}

func writeRateLimitError(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := retryAfterSeconds(retryAfter)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":               "too many token attempts",
		"retry_after_seconds": seconds,
	})
}

func writeRedactedError(w http.ResponseWriter, err error, dataRoot string) {
	if rateLimited := (*agentservice.RateLimitError)(nil); errors.As(err, &rateLimited) && rateLimited != nil {
		seconds := retryAfterSeconds(rateLimited.RetryAfter)
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"code":                agentservice.ErrorCode(err),
			"error":               redactSupportBundleText(dataRoot, err.Error()),
			"retry_after_seconds": seconds,
		})
		return
	}
	writeJSON(w, errorStatusCode(err), map[string]string{
		"code":  agentservice.ErrorCode(err),
		"error": redactSupportBundleText(dataRoot, err.Error()),
	})
}

func retryAfterSeconds(retryAfter time.Duration) int {
	if retryAfter <= 0 {
		return 1
	}
	seconds := int(retryAfter / time.Second)
	if retryAfter%time.Second != 0 {
		seconds++
	}
	if seconds <= 0 {
		return 1
	}
	return seconds
}

func errorStatusCode(err error) int {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, agentservice.ErrUnauthorized):
		code = http.StatusUnauthorized
	case errors.Is(err, agentservice.ErrForbidden):
		code = http.StatusForbidden
	case errors.Is(err, agentservice.ErrTenantNotFound), errors.Is(err, agentservice.ErrUserNotFound), errors.Is(err, agentservice.ErrCredentialNotFound), errors.Is(err, agentservice.ErrUserConfigNotFound), errors.Is(err, agentservice.ErrInstanceNotFound), errors.Is(err, agentservice.ErrSessionNotFound), errors.Is(err, agentservice.ErrRunNotFound), errors.Is(err, agentservice.ErrSnapshotNotFound), errors.Is(err, agentservice.ErrRecordNotFound):
		code = http.StatusNotFound
	case errors.Is(err, agentservice.ErrRunNotRunning), errors.Is(err, agentservice.ErrInstanceBusy), errors.Is(err, agentservice.ErrUserBusy), errors.Is(err, agentservice.ErrTenantBusy), errors.Is(err, agentservice.ErrDeleteProtected), errors.Is(err, agentservice.ErrSessionBusy), errors.Is(err, agentservice.ErrSessionArchived), errors.Is(err, agentservice.ErrAlreadyExists):
		code = http.StatusConflict
	case errors.Is(err, agentservice.ErrQuotaExceeded):
		code = http.StatusTooManyRequests
	case errors.Is(err, agentservice.ErrRateLimited):
		code = http.StatusTooManyRequests
	case errors.Is(err, agentservice.ErrJobPersistence):
		code = http.StatusServiceUnavailable
	}
	return code
}

func (s *HTTPServer) requireExistingTenantUser(w http.ResponseWriter, r *http.Request, tenantID, userID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	if tenantID == "" || userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id and user_id are required"})
		return false
	}
	if _, err := s.svc.GetUser(r.Context(), tenantID, userID); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func requestClientIP(r *http.Request) string {
	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if remoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return strings.TrimSpace(host)
	}
	return remoteAddr
}
