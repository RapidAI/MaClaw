package repomaint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/google/uuid"
)

// OnlineSender is the desktop socket. Implementations must not enqueue when
// the machine is offline. *device.Service.SendToOnlineMachine satisfies this.
type OnlineSender interface {
	IsMachineOnline(machineID string) bool
	SendToOnlineMachine(machineID string, msg any) error
}

// MachineAuthenticator is the desktop bearer check used by the machine routes.
type MachineAuthenticator interface {
	AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error)
}

// Service accepts signed GitHub webhooks and hands normalized rows to an
// online desktop. Offline rows stay pending in SQLite.
type Service struct {
	db           *sql.DB
	store        *store
	sender       OnlineSender
	identity     MachineAuthenticator
	lim          *limiter
	pendingLimit int
	maxBody      int
}

// Open prepares the schema and a service. sender may be nil; rows then stay
// pending until a later process can deliver them.
func Open(db *sql.DB, sender OnlineSender) (*Service, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	if senderNil(sender) {
		sender = nil
	}
	return &Service{
		db:           db,
		store:        newStore(db),
		sender:       sender,
		lim:          newLimiter(),
		pendingLimit: defaultPendingLimit,
		maxBody:      defaultMaxWebhookBody,
	}, nil
}

// SetPendingLimit overrides the per-machine pending cap. Tests use this.
func (s *Service) SetPendingLimit(n int) {
	if s == nil || n <= 0 {
		return
	}
	s.pendingLimit = n
}

// SetRateLimits overrides the per-hook and process-wide windows. Tests use this.
func (s *Service) SetRateLimits(perHook, global int) {
	if s == nil || s.lim == nil {
		return
	}
	s.lim.set(perHook, global)
}

// SetMaxBody overrides the webhook body cap. Tests use this.
func (s *Service) SetMaxBody(n int) {
	if s == nil || n <= 0 {
		return
	}
	s.maxBody = n
}

// PushPending sends every pending row for a machine that is online now.
// MarkOnline calls this beside the in-memory queue drain. A failed send
// leaves the row pending and does not touch that queue.
func (s *Service) PushPending(machineID string) {
	if s == nil || s.sender == nil {
		return
	}
	machineID = strings.TrimSpace(machineID)
	if machineID == "" || !s.sender.IsMachineOnline(machineID) {
		return
	}
	rows, err := s.store.listPending(context.Background(), machineID, s.pendingLimit)
	if err != nil {
		log.Printf("[github-repo-maint] list pending machine_id=%s err=%v", machineID, err)
		return
	}
	for _, row := range rows {
		if !s.sender.IsMachineOnline(machineID) {
			return
		}
		if err := s.sender.SendToOnlineMachine(machineID, frame(row.Event)); err != nil {
			log.Printf("[github-repo-maint] online send failed rule_id=%s delivery_id=%s event_kind=%s object_id=%s err=%v",
				row.Event.RuleID, row.Event.DeliveryID, row.Event.EventKind, row.Event.ObjectID, err)
			return
		}
	}
}

// SweepOnce deletes pending rows older than 7 days. It does not redeliver them.
func (s *Service) SweepOnce(now time.Time) (int, error) {
	if s == nil {
		return 0, errNoDB
	}
	cutoff := now.UTC().Add(-7 * 24 * time.Hour).Format(timeLayout)
	return s.store.sweepPending(context.Background(), cutoff)
}

// StartSweeper deletes expired pending rows once an hour. The router starts it.
func (s *Service) StartSweeper() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for now := range ticker.C {
		n, err := s.SweepOnce(now)
		if err != nil {
			log.Printf("[github-repo-maint] sweeper error: %v", err)
			continue
		}
		if n > 0 {
			log.Printf("[github-repo-maint] sweeper deleted %d", n)
		}
	}
}

func (s *Service) serveWebhook(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.PathValue("hookToken"))
	body, tooBig, err := readBody(r, s.maxBody)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if tooBig {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	status := s.accept(r.Context(), token, r.Header, body)
	w.WriteHeader(status)
}

func (s *Service) accept(ctx context.Context, token string, header http.Header, body []byte) int {
	if token == "" {
		return http.StatusNotFound
	}
	delivery := strings.TrimSpace(header.Get("X-GitHub-Delivery"))
	if delivery == "" {
		return http.StatusBadRequest
	}
	rule, err := s.store.ruleByToken(ctx, token)
	if errors.Is(err, errNotFound) {
		return http.StatusNotFound
	}
	if err != nil {
		log.Printf("[github-repo-maint] rule lookup err=%v", err)
		return http.StatusInternalServerError
	}
	if !s.lim.allow(token) {
		log.Printf("[github-repo-maint] rate limited hook=%s", tokenPrefix(token))
		return http.StatusTooManyRequests
	}
	if !verifyHubSignature(rule.HookSecret, header.Get("X-Hub-Signature-256"), body) {
		log.Printf("[github-repo-maint] signature rejected hook=%s", tokenPrefix(token))
		return http.StatusUnauthorized
	}
	hookID, hookOK := parseHookID(header.Get("X-GitHub-Hook-ID"))
	if rule.GitHubHookID != 0 {
		if !hookOK || hookID != rule.GitHubHookID {
			log.Printf("[github-repo-maint] hook id rejected rule_id=%s", rule.RuleID)
			return http.StatusUnauthorized
		}
	} else if hookOK {
		if err := s.store.noteHookID(ctx, rule.RuleID, hookID); err != nil {
			log.Printf("[github-repo-maint] note hook id rule_id=%s err=%v", rule.RuleID, err)
		}
	}
	if strings.EqualFold(strings.TrimSpace(header.Get("X-GitHub-Event")), "ping") {
		return http.StatusOK
	}
	if !rule.Enabled {
		log.Printf("[github-repo-maint] discarded disabled rule_id=%s github_delivery=%s", rule.RuleID, delivery)
		return http.StatusOK
	}
	ev, keep := normalizeWebhook(rule, header.Get("X-GitHub-Event"), body)
	if !keep {
		log.Printf("[github-repo-maint] filtered rule_id=%s github_delivery=%s event=%s", rule.RuleID, delivery, header.Get("X-GitHub-Event"))
		return http.StatusOK
	}
	ev.DeliveryID = uuid.NewString()
	ev.GitHubDelivery = delivery
	ev.RuleID = rule.RuleID
	err = s.store.insertPending(ctx, rule.MachineID, ev, s.pendingLimit)
	if errors.Is(err, errDuplicate) {
		log.Printf("[github-repo-maint] duplicate github_delivery=%s", delivery)
		return http.StatusOK
	}
	if errors.Is(err, errPendingLimit) {
		log.Printf("[github-repo-maint] pending limit machine_id=%s github_delivery=%s", rule.MachineID, delivery)
		return http.StatusServiceUnavailable
	}
	if err != nil {
		log.Printf("[github-repo-maint] insert rule_id=%s github_delivery=%s err=%v", rule.RuleID, delivery, err)
		return http.StatusInternalServerError
	}
	log.Printf("[github-repo-maint] stored rule_id=%s delivery_id=%s github_delivery=%s event_kind=%s object_id=%s",
		ev.RuleID, ev.DeliveryID, ev.GitHubDelivery, ev.EventKind, ev.ObjectID)
	s.pushOne(rule.MachineID, ev)
	return http.StatusOK
}

func (s *Service) pushOne(machineID string, ev RepoMaintEvent) {
	if s.sender == nil || !s.sender.IsMachineOnline(machineID) {
		return
	}
	if err := s.sender.SendToOnlineMachine(machineID, frame(ev)); err != nil {
		log.Printf("[github-repo-maint] online send failed rule_id=%s delivery_id=%s event_kind=%s object_id=%s err=%v",
			ev.RuleID, ev.DeliveryID, ev.EventKind, ev.ObjectID, err)
	}
}

func frame(ev RepoMaintEvent) map[string]any {
	return map[string]any{"type": FrameType, "payload": ev}
}

func readBody(r *http.Request, max int) ([]byte, bool, error) {
	if max <= 0 {
		max = defaultMaxWebhookBody
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(max)+1))
	if err != nil {
		return nil, false, err
	}
	if len(body) > max {
		return nil, true, nil
	}
	return body, false, nil
}

func parseHookID(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func tokenPrefix(token string) string {
	if len(token) > 6 {
		return token[:6]
	}
	return token
}

func senderNil(sender OnlineSender) bool {
	if sender == nil {
		return true
	}
	v := reflect.ValueOf(sender)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map, reflect.Func:
		return v.IsNil()
	default:
		return false
	}
}

// rulePublic is the machine-facing rule view. hook_secret is never copied onto it.
type rulePublic struct {
	RuleID       string   `json:"rule_id"`
	MachineID    string   `json:"machine_id"`
	Owner        string   `json:"owner"`
	Repo         string   `json:"repo"`
	Enabled      bool     `json:"enabled"`
	PushBranch   string   `json:"push_branch"`
	EventKinds   []string `json:"event_kinds"`
	GitHubHookID int64    `json:"github_hook_id"`
	SecretSet    bool     `json:"secret_set"`
}

func publishRule(row ruleRow) rulePublic {
	kinds := row.EventKinds
	if kinds == nil {
		kinds = []string{}
	}
	return rulePublic{
		RuleID:       row.RuleID,
		MachineID:    row.MachineID,
		Owner:        row.Owner,
		Repo:         row.Repo,
		Enabled:      row.Enabled,
		PushBranch:   row.PushBranch,
		EventKinds:   kinds,
		GitHubHookID: row.GitHubHookID,
		SecretSet:    row.HookSecret != "",
	}
}

type ruleWrite struct {
	MachineID    string   `json:"machine_id"`
	Owner        string   `json:"owner"`
	Repo         string   `json:"repo"`
	HookToken    string   `json:"hook_token"`
	HookSecret   string   `json:"hook_secret"`
	GitHubHookID int64    `json:"github_hook_id"`
	Enabled      bool     `json:"enabled"`
	PushBranch   string   `json:"push_branch"`
	EventKinds   []string `json:"event_kinds"`
}

func (s *Service) putRule(ctx context.Context, principal *auth.MachinePrincipal, ruleID string, body []byte) (int, rulePublic) {
	var in ruleWrite
	if err := json.Unmarshal(body, &in); err != nil {
		return http.StatusBadRequest, rulePublic{}
	}
	ruleID = strings.TrimSpace(ruleID)
	in.MachineID = strings.TrimSpace(in.MachineID)
	in.Owner = strings.TrimSpace(in.Owner)
	in.Repo = strings.TrimSpace(in.Repo)
	in.HookToken = strings.TrimSpace(in.HookToken)
	in.HookSecret = strings.TrimSpace(in.HookSecret)
	if ruleID == "" || in.Owner == "" || in.Repo == "" || in.HookToken == "" || in.HookSecret == "" {
		return http.StatusBadRequest, rulePublic{}
	}
	if in.MachineID == "" || in.MachineID != principal.MachineID {
		return http.StatusForbidden, rulePublic{}
	}
	if in.EventKinds == nil {
		in.EventKinds = []string{}
	}
	row := ruleRow{
		RuleID:       ruleID,
		TenantID:     principal.TenantID,
		MachineID:    principal.MachineID,
		Owner:        in.Owner,
		Repo:         in.Repo,
		HookToken:    in.HookToken,
		HookSecret:   in.HookSecret,
		GitHubHookID: in.GitHubHookID,
		Enabled:      in.Enabled,
		PushBranch:   strings.TrimSpace(in.PushBranch),
		EventKinds:   in.EventKinds,
	}
	if err := s.store.upsertRule(ctx, row); err != nil {
		if errors.Is(err, errConflict) {
			return http.StatusConflict, rulePublic{}
		}
		log.Printf("[github-repo-maint] upsert rule_id=%s err=%v", ruleID, err)
		return http.StatusInternalServerError, rulePublic{}
	}
	log.Printf("[github-repo-maint] rule upsert rule_id=%s enabled=%t", ruleID, row.Enabled)
	return http.StatusOK, publishRule(row)
}

func (s *Service) deleteRule(ctx context.Context, principal *auth.MachinePrincipal, ruleID string) int {
	err := s.store.deleteRule(ctx, strings.TrimSpace(ruleID), principal.MachineID)
	if errors.Is(err, errNotFound) {
		return http.StatusNotFound
	}
	if err != nil {
		log.Printf("[github-repo-maint] delete rule_id=%s err=%v", ruleID, err)
		return http.StatusInternalServerError
	}
	log.Printf("[github-repo-maint] rule deleted rule_id=%s", ruleID)
	return http.StatusOK
}

type deliveryView struct {
	ID             string         `json:"id"`
	RuleID         string         `json:"rule_id"`
	GitHubDelivery string         `json:"github_delivery"`
	EventKind      string         `json:"event_kind"`
	ObjectID       string         `json:"object_id"`
	Status         string         `json:"status"`
	Payload        RepoMaintEvent `json:"payload"`
	CreatedAt      string         `json:"created_at"`
}

func (s *Service) listDeliveries(ctx context.Context, principal *auth.MachinePrincipal, limit int) ([]deliveryView, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.store.listPending(ctx, principal.MachineID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]deliveryView, 0, len(rows))
	for _, row := range rows {
		out = append(out, deliveryView{
			ID:             row.Event.DeliveryID,
			RuleID:         row.Event.RuleID,
			GitHubDelivery: row.Event.GitHubDelivery,
			EventKind:      row.Event.EventKind,
			ObjectID:       row.Event.ObjectID,
			Status:         statusPending,
			Payload:        row.Event,
			CreatedAt:      row.Created,
		})
	}
	return out, nil
}

func (s *Service) ackDelivery(ctx context.Context, principal *auth.MachinePrincipal, id string) int {
	err := s.store.ack(ctx, strings.TrimSpace(id), principal.MachineID)
	if errors.Is(err, errNotFound) {
		return http.StatusNotFound
	}
	if err != nil {
		log.Printf("[github-repo-maint] ack delivery_id=%s err=%v", id, err)
		return http.StatusInternalServerError
	}
	log.Printf("[github-repo-maint] ack delivery_id=%s", id)
	return http.StatusOK
}
