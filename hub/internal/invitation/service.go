package invitation

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	codeCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLength  = 10
	maxRetries  = 3
	maxCount    = 50

	settingsKeyInvitationCodeRequired = "invitation_code_required"
	vipLookupCacheTTL                 = 10 * time.Second
)

var (
	ErrInvalidCount          = errors.New("count must be between 1 and 50")
	ErrCodeConflict          = errors.New("failed to generate unique invitation code after retries")
	ErrInvalidInvitationCode = errors.New("invalid or used invitation code")
	ErrCodeNotFound          = errors.New("invitation code not found")
	ErrInvalidLLMGrant       = errors.New("invalid invitation LLM grant")
)

// CenterSyncer is an optional callback invoked when invitation codes are
// generated or consumed, to sync them to HubCenter for routing.
type CenterSyncer interface {
	// InvitationSyncFingerprint identifies the configured HubCenter nodes.
	// An empty fingerprint means the hub is not registered or has no center URL.
	InvitationSyncFingerprint(ctx context.Context) (string, error)
	// RetainInvitationCodes drops sync state for codes that are no longer unused.
	RetainInvitationCodes(codes []string)
	// SyncInvitationCodesToCenter reports accepted=true only after every
	// configured HubCenter node has stored the routes. fingerprint is the node
	// set that accepted the write. A nil error with accepted=false means the
	// hub is not registered, or only some nodes stored the batch, so the caller
	// must try again later.
	SyncInvitationCodesToCenter(ctx context.Context, codes []string, tenantID string) (accepted bool, fingerprint string, err error)
	DeleteInvitationCodesFromCenter(ctx context.Context, codes []string) (accepted bool, err error)
	ForgetInvitationCodes(codes []string)
	MarkInvitationCodeUsedOnCenter(ctx context.Context, code string, email string) (accepted bool, err error)
}

type Service struct {
	repo     store.InvitationCodeRepository
	settings store.SystemSettingsRepository
	syncer   CenterSyncer

	vipLookupMu    sync.RWMutex
	vipLookupCache map[string]vipLookupCacheEntry

	invitationSyncMu          sync.Mutex
	invitationPushMu          sync.Mutex
	syncedInvitationKey       map[string]invitationSyncAck
	pendingInvitationUse      map[string]string
	pendingInvitationDelete   map[string]struct{}
	suppressedInvitationCodes map[string]struct{}
}

type invitationSyncAck struct {
	tenantID    string
	fingerprint string
}

type vipLookupCacheEntry struct {
	code      *store.InvitationCode
	expiresAt time.Time
}

func NewService(repo store.InvitationCodeRepository, settings store.SystemSettingsRepository) *Service {
	return &Service{repo: repo, settings: settings, vipLookupCache: make(map[string]vipLookupCacheEntry)}
}

func (s *Service) SetCenterSyncer(syncer CenterSyncer) {
	s.syncer = syncer
}

type GenerateCodeOptions struct {
	Count                int
	ValidityDays         int
	VIP                  bool
	LLMServiceGroupID    string
	LLMGrantDurationDays int
	LLMGrantCredits      float64
}

// GenerateCodes generates count invitation codes (1-50) and stores them.
// validityDays specifies the validity period in days; values < 0 are treated as 0 (permanent).
func (s *Service) GenerateCodes(ctx context.Context, count int, validityDays int, vip bool) ([]*store.InvitationCode, error) {
	return s.GenerateCodesForTenant(ctx, store.DefaultTenantID, count, validityDays, vip)
}

func (s *Service) GenerateCodesForTenant(ctx context.Context, tenantID string, count int, validityDays int, vip bool) ([]*store.InvitationCode, error) {
	return s.GenerateCodesForTenantWithOptions(ctx, tenantID, GenerateCodeOptions{Count: count, ValidityDays: validityDays, VIP: vip})
}

func (s *Service) GenerateCodesForTenantWithOptions(ctx context.Context, tenantID string, opts GenerateCodeOptions) ([]*store.InvitationCode, error) {
	count := opts.Count
	validityDays := opts.ValidityDays
	if count < 1 || count > maxCount {
		return nil, ErrInvalidCount
	}
	if validityDays < 0 {
		validityDays = 0
	}
	llmServiceGroupID := strings.TrimSpace(opts.LLMServiceGroupID)
	llmGrantDurationDays := opts.LLMGrantDurationDays
	if llmGrantDurationDays < 0 {
		llmGrantDurationDays = 0
	}
	llmGrantCredits := opts.LLMGrantCredits
	if math.IsNaN(llmGrantCredits) || math.IsInf(llmGrantCredits, 0) {
		return nil, ErrInvalidLLMGrant
	}
	if llmGrantCredits < 0 {
		llmGrantCredits = 0
	}
	hasLLMGrantInput := llmServiceGroupID != "" || llmGrantDurationDays > 0 || llmGrantCredits > 0
	if hasLLMGrantInput {
		if llmServiceGroupID == "" || llmGrantDurationDays <= 0 || llmGrantCredits <= 0 {
			return nil, ErrInvalidLLMGrant
		}
		if s == nil || s.settings == nil {
			return nil, ErrInvalidLLMGrant
		}
		reg, err := llmservice.LoadRegistry(ctx, s.settings)
		if err != nil {
			return nil, err
		}
		if reg.FindModelServiceGroup(llmServiceGroupID) == nil {
			return nil, ErrInvalidLLMGrant
		}
	}

	codes := make([]*store.InvitationCode, 0, count)
	for i := 0; i < count; i++ {
		var created *store.InvitationCode
		var err error
		for attempt := 0; attempt < maxRetries; attempt++ {
			code, genErr := generateCode()
			if genErr != nil {
				return nil, fmt.Errorf("generating code: %w", genErr)
			}

			// Check for conflict
			existing, _ := s.repo.GetByTenantCode(ctx, tenantID, code)
			if existing != nil {
				continue // conflict, retry
			}

			now := time.Now()
			item := &store.InvitationCode{
				ID:                   fmt.Sprintf("ic_%s", randomShortID()),
				TenantID:             tenantID,
				Code:                 code,
				Status:               "unused",
				ValidityDays:         validityDays,
				VIP:                  opts.VIP,
				LLMServiceGroupID:    llmServiceGroupID,
				LLMGrantDurationDays: llmGrantDurationDays,
				LLMGrantCredits:      llmGrantCredits,
				CreatedAt:            now,
			}
			if createErr := s.repo.Create(ctx, item); createErr != nil {
				// Could be a race condition conflict
				if strings.Contains(createErr.Error(), "UNIQUE") {
					continue
				}
				return nil, fmt.Errorf("creating invitation code: %w", createErr)
			}
			created = item
			err = nil
			break
		}
		if created == nil {
			if err != nil {
				return nil, err
			}
			return nil, ErrCodeConflict
		}
		codes = append(codes, created)
	}

	// Sync newly generated codes to HubCenter for routing. Failures are logged
	// and retried by ReconcileInvitationCodes on the center heartbeat.
	if s.syncer != nil && len(codes) > 0 {
		codeStrings := make([]string, len(codes))
		for i, c := range codes {
			codeStrings[i] = c.Code
		}
		syncTenantID := strings.TrimSpace(tenantID)
		if syncTenantID == "" {
			syncTenantID = store.DefaultTenantID
		}
		go func(codes []string, tenantID string) {
			if err := s.pushInvitationCodes(context.Background(), tenantID, codes); err != nil {
				log.Printf("[invitation] sync codes to hubcenter failed tenant=%s count=%d err=%v", tenantID, len(codes), err)
			}
		}(codeStrings, syncTenantID)
	}

	return codes, nil
}

const invitationSyncBatchSize = 200

// ReconcileInvitationCodes pushes unused invitation codes that HubCenter has
// not accepted yet, including codes owned by non-default tenants. Generation
// sync is best-effort; this runs from the center heartbeat so a failed push
// is retried and codes created before a HubCenter node had the route still
// become routable. Codes already accepted are left alone until their tenant
// changes or they leave the unused set.
func (s *Service) ReconcileInvitationCodes(ctx context.Context) error {
	if s == nil || s.syncer == nil || s.repo == nil {
		return nil
	}
	var lastErr error
	fingerprint, err := s.syncer.InvitationSyncFingerprint(ctx)
	if err != nil {
		return err
	}
	if fingerprint != "" {
		codes, err := s.repo.ListUnused(ctx, "all")
		if err != nil {
			return err
		}
		s.syncer.RetainInvitationCodes(unusedInvitationCodeKeys(codes))
		grouped, order := s.pendingInvitationSync(codes, fingerprint)
		for _, tenantID := range order {
			items := grouped[tenantID]
			for start := 0; start < len(items); start += invitationSyncBatchSize {
				end := start + invitationSyncBatchSize
				if end > len(items) {
					end = len(items)
				}
				batch := items[start:end]
				if err := s.pushInvitationCodes(ctx, tenantID, batch); err != nil {
					lastErr = err
				}
			}
		}
	}
	if err := s.retryPendingInvitationDeletes(ctx); err != nil {
		lastErr = err
	}
	if err := s.retryPendingInvitationUses(ctx); err != nil {
		lastErr = err
	}
	return lastErr
}

func unusedInvitationCodeKeys(codes []*store.InvitationCode) []string {
	keys := make([]string, 0, len(codes))
	for _, item := range codes {
		if item == nil {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(item.Code))
		if code == "" {
			continue
		}
		keys = append(keys, code)
	}
	return keys
}

func (s *Service) pendingInvitationSync(codes []*store.InvitationCode, fingerprint string) (map[string][]string, []string) {
	s.invitationSyncMu.Lock()
	defer s.invitationSyncMu.Unlock()
	if s.syncedInvitationKey == nil {
		s.syncedInvitationKey = map[string]invitationSyncAck{}
	}
	current := make(map[string]struct{}, len(codes))
	grouped := map[string][]string{}
	order := make([]string, 0)
	for _, item := range codes {
		if item == nil {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(item.Code))
		if code == "" {
			continue
		}
		tenantID := strings.TrimSpace(item.TenantID)
		if tenantID == "" {
			tenantID = store.DefaultTenantID
		}
		current[code] = struct{}{}
		if _, suppressed := s.suppressedInvitationCodes[code]; suppressed {
			continue
		}
		ack := s.syncedInvitationKey[code]
		if ack.tenantID == tenantID && ack.fingerprint == fingerprint {
			continue
		}
		if _, ok := grouped[tenantID]; !ok {
			order = append(order, tenantID)
		}
		grouped[tenantID] = append(grouped[tenantID], code)
	}
	for code := range s.syncedInvitationKey {
		if _, ok := current[code]; !ok {
			delete(s.syncedInvitationKey, code)
		}
	}
	return grouped, order
}

func (s *Service) rememberInvitationSync(tenantID, fingerprint string, codes []string) {
	if s == nil || fingerprint == "" || len(codes) == 0 {
		return
	}
	s.invitationSyncMu.Lock()
	defer s.invitationSyncMu.Unlock()
	if s.syncedInvitationKey == nil {
		s.syncedInvitationKey = map[string]invitationSyncAck{}
	}
	for _, code := range codes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		if _, suppressed := s.suppressedInvitationCodes[code]; suppressed {
			continue
		}
		s.syncedInvitationKey[code] = invitationSyncAck{tenantID: tenantID, fingerprint: fingerprint}
	}
}

func (s *Service) pushInvitationCodes(ctx context.Context, tenantID string, codes []string) error {
	if s == nil || s.syncer == nil || len(codes) == 0 {
		return nil
	}
	s.invitationPushMu.Lock()
	defer s.invitationPushMu.Unlock()
	live := s.unsuppressedInvitationCodes(codes)
	var err error
	if len(live) > 0 {
		accepted, fingerprint, syncErr := s.syncer.SyncInvitationCodesToCenter(ctx, live, tenantID)
		if syncErr != nil {
			log.Printf("[invitation] sync codes to hubcenter failed tenant=%s count=%d err=%v", tenantID, len(live), syncErr)
			err = syncErr
		}
		if accepted && fingerprint != "" {
			s.rememberInvitationSync(tenantID, fingerprint, live)
		}
	}
	if delErr := s.flushInvitationCodeDeletions(ctx); delErr != nil && err == nil {
		err = delErr
	}
	return err
}

func (s *Service) unsuppressedInvitationCodes(codes []string) []string {
	s.invitationSyncMu.Lock()
	defer s.invitationSyncMu.Unlock()
	live := make([]string, 0, len(codes))
	for _, code := range codes {
		normalized := strings.ToUpper(strings.TrimSpace(code))
		if normalized == "" {
			continue
		}
		if _, suppressed := s.suppressedInvitationCodes[normalized]; suppressed {
			continue
		}
		live = append(live, normalized)
	}
	return live
}

func (s *Service) queueInvitationCodeDeletion(codes []string) {
	if s == nil || s.syncer == nil || len(codes) == 0 {
		return
	}
	normalized := make([]string, 0, len(codes))
	s.invitationSyncMu.Lock()
	if s.suppressedInvitationCodes == nil {
		s.suppressedInvitationCodes = map[string]struct{}{}
	}
	if s.pendingInvitationDelete == nil {
		s.pendingInvitationDelete = map[string]struct{}{}
	}
	for _, code := range codes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		s.suppressedInvitationCodes[code] = struct{}{}
		s.pendingInvitationDelete[code] = struct{}{}
		delete(s.syncedInvitationKey, code)
		delete(s.pendingInvitationUse, code)
		normalized = append(normalized, code)
	}
	s.invitationSyncMu.Unlock()
	if len(normalized) == 0 {
		return
	}
	s.syncer.ForgetInvitationCodes(normalized)
	go func() {
		s.invitationPushMu.Lock()
		defer s.invitationPushMu.Unlock()
		if err := s.flushInvitationCodeDeletions(context.Background()); err != nil {
			log.Printf("[invitation] delete codes on hubcenter failed count=%d err=%v", len(normalized), err)
		}
	}()
}

func (s *Service) retryPendingInvitationDeletes(ctx context.Context) error {
	if s == nil || s.syncer == nil {
		return nil
	}
	s.invitationPushMu.Lock()
	defer s.invitationPushMu.Unlock()
	return s.flushInvitationCodeDeletions(ctx)
}

func (s *Service) flushInvitationCodeDeletions(ctx context.Context) error {
	s.invitationSyncMu.Lock()
	if len(s.pendingInvitationDelete) == 0 {
		s.invitationSyncMu.Unlock()
		return nil
	}
	codes := make([]string, 0, len(s.pendingInvitationDelete))
	for code := range s.pendingInvitationDelete {
		codes = append(codes, code)
	}
	s.invitationSyncMu.Unlock()
	var lastErr error
	for start := 0; start < len(codes); start += invitationSyncBatchSize {
		end := start + invitationSyncBatchSize
		if end > len(codes) {
			end = len(codes)
		}
		batch := codes[start:end]
		accepted, err := s.syncer.DeleteInvitationCodesFromCenter(ctx, batch)
		if err != nil {
			log.Printf("[invitation] delete codes on hubcenter failed count=%d err=%v", len(batch), err)
			lastErr = err
		}
		if !accepted {
			continue
		}
		s.invitationSyncMu.Lock()
		for _, code := range batch {
			delete(s.pendingInvitationDelete, code)
		}
		s.invitationSyncMu.Unlock()
		s.syncer.ForgetInvitationCodes(batch)
	}
	return lastErr
}

func (s *Service) rememberPendingInvitationUse(code, email string) {
	code = strings.ToUpper(strings.TrimSpace(code))
	email = strings.TrimSpace(strings.ToLower(email))
	if s == nil || code == "" || email == "" {
		return
	}
	s.invitationSyncMu.Lock()
	defer s.invitationSyncMu.Unlock()
	if s.pendingInvitationUse == nil {
		s.pendingInvitationUse = map[string]string{}
	}
	s.pendingInvitationUse[code] = email
}

func (s *Service) clearPendingInvitationUse(code, email string) {
	code = strings.ToUpper(strings.TrimSpace(code))
	email = strings.TrimSpace(strings.ToLower(email))
	if s == nil || code == "" {
		return
	}
	s.invitationSyncMu.Lock()
	defer s.invitationSyncMu.Unlock()
	if s.pendingInvitationUse[code] == email {
		delete(s.pendingInvitationUse, code)
	}
}

func (s *Service) retryPendingInvitationUses(ctx context.Context) error {
	if s == nil || s.syncer == nil {
		return nil
	}
	s.invitationSyncMu.Lock()
	pending := make(map[string]string, len(s.pendingInvitationUse))
	for code, email := range s.pendingInvitationUse {
		pending[code] = email
	}
	s.invitationSyncMu.Unlock()
	s.invitationPushMu.Lock()
	defer s.invitationPushMu.Unlock()
	var lastErr error
	for code, email := range pending {
		if s.invitationCodeSuppressed(code) {
			s.clearPendingInvitationUse(code, email)
			continue
		}
		accepted, err := s.syncer.MarkInvitationCodeUsedOnCenter(ctx, code, email)
		if err != nil {
			log.Printf("[invitation] retry mark used on hubcenter failed code=%s err=%v", code, err)
			lastErr = err
		}
		if accepted {
			s.clearPendingInvitationUse(code, email)
		}
	}
	if delErr := s.flushInvitationCodeDeletions(ctx); delErr != nil && lastErr == nil {
		lastErr = delErr
	}
	return lastErr
}

func (s *Service) invitationCodeSuppressed(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	s.invitationSyncMu.Lock()
	defer s.invitationSyncMu.Unlock()
	_, ok := s.suppressedInvitationCodes[code]
	return ok
}

func (s *Service) pushInvitationUse(code, email string) {
	if s == nil || s.syncer == nil {
		return
	}
	s.invitationPushMu.Lock()
	defer s.invitationPushMu.Unlock()
	if !s.invitationCodeSuppressed(code) {
		accepted, err := s.syncer.MarkInvitationCodeUsedOnCenter(context.Background(), code, email)
		if err != nil {
			log.Printf("[invitation] mark used on hubcenter failed code=%s err=%v", code, err)
		}
		if accepted {
			s.clearPendingInvitationUse(code, email)
		} else {
			s.rememberPendingInvitationUse(code, email)
		}
	}
	if err := s.flushInvitationCodeDeletions(context.Background()); err != nil {
		log.Printf("[invitation] delete codes on hubcenter failed err=%v", err)
	}
}

// ValidateAndConsume validates that the code exists and is unused, then marks it used.
func (s *Service) ValidateAndConsume(ctx context.Context, code string, email string) error {
	return s.ValidateAndConsumeForTenant(ctx, store.DefaultTenantID, code, email)
}

func (s *Service) ValidateAndConsumeForTenant(ctx context.Context, tenantID string, code string, email string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrInvalidInvitationCode
	}

	item, err := s.repo.GetByTenantCode(ctx, tenantID, code)
	if err != nil {
		return ErrInvalidInvitationCode
	}
	if item == nil {
		return ErrInvalidInvitationCode
	}

	// If the code was already used by the SAME email, allow re-enrollment
	// (e.g. user switching devices or re-installing). This is idempotent —
	// the code stays consumed, no side effects.
	if item.Status != "unused" {
		if strings.EqualFold(strings.TrimSpace(item.UsedByEmail), strings.TrimSpace(email)) {
			return nil
		}
		return ErrInvalidInvitationCode
	}

	if err := s.repo.MarkUsed(ctx, item.ID, email, time.Now()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidInvitationCode
		}
		return err
	}
	s.invalidateVIPLookup(tenantID, email)

	// Notify HubCenter that this code has been consumed — mark the bound email
	// on the routing record (preserves it for admin lookup and re-enrollment).
	if s.syncer != nil {
		go s.pushInvitationUse(code, email)
	}

	return nil
}

// UnbindCode clears the binding of an invitation code, resetting it to unused.
func (s *Service) UnbindCode(ctx context.Context, id string) error {
	if id == "" {
		return ErrCodeNotFound
	}
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	invalidateTenantID, invalidateEmail := "", ""
	if item != nil && item.UsedByEmail != "" {
		invalidateTenantID = item.TenantID
		invalidateEmail = item.UsedByEmail
	}
	if err := s.repo.Unbind(ctx, id); err != nil {
		return err
	}
	if invalidateEmail != "" {
		s.invalidateVIPLookup(invalidateTenantID, invalidateEmail)
	}
	return nil
}

// GetCodeByID returns the invitation code with the given ID.
func (s *Service) GetCodeByID(ctx context.Context, id string) (*store.InvitationCode, error) {
	if id == "" {
		return nil, ErrCodeNotFound
	}
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrCodeNotFound
	}
	return item, nil
}

// DeleteCode permanently deletes a single invitation code by ID.
func (s *Service) DeleteCode(ctx context.Context, id string) error {
	if id == "" {
		return ErrCodeNotFound
	}
	item, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteByID(ctx, id); err != nil {
		return err
	}
	if item != nil && item.UsedByEmail != "" {
		s.invalidateVIPLookup(item.TenantID, item.UsedByEmail)
	}
	if item != nil {
		s.queueInvitationCodeDeletion([]string{item.Code})
	}
	return nil
}

// DeleteCodeByEmail permanently deletes all invitation codes bound to the given email.
// Returns the number of deleted codes.
func (s *Service) DeleteCodeByEmail(ctx context.Context, email string) (int64, error) {
	return s.DeleteCodeByTenantEmail(ctx, store.DefaultTenantID, email)
}

func (s *Service) DeleteCodeByTenantEmail(ctx context.Context, tenantID, email string) (int64, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return 0, nil
	}
	codes := s.invitationCodesBoundToEmail(ctx, tenantID, email)
	count, err := s.repo.DeleteByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return count, err
	}
	if count > 0 {
		s.invalidateVIPLookup(tenantID, email)
		s.queueInvitationCodeDeletion(codes)
	}
	return count, nil
}

func (s *Service) invitationCodesBoundToEmail(ctx context.Context, tenantID, email string) []string {
	if s == nil || s.repo == nil {
		return nil
	}
	var codes []string
	for offset := 0; ; offset += 100 {
		items, total, err := s.repo.ListPagedByTenant(ctx, tenantID, "used", email, offset, 100)
		if err != nil {
			log.Printf("[invitation] list codes before delete failed tenant=%s email=%s err=%v", tenantID, email, err)
			return codes
		}
		for _, item := range items {
			if item == nil || !strings.EqualFold(strings.TrimSpace(item.UsedByEmail), email) {
				continue
			}
			codes = append(codes, item.Code)
		}
		if len(items) == 0 || offset+len(items) >= total {
			break
		}
	}
	return codes
}

// GetCodeByEmail returns the invitation code bound to the given email.
func (s *Service) GetCodeByEmail(ctx context.Context, email string) (*store.InvitationCode, error) {
	return s.GetCodeByTenantEmail(ctx, store.DefaultTenantID, email)
}

func (s *Service) GetCodeByTenantEmail(ctx context.Context, tenantID, email string) (*store.InvitationCode, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return nil, nil
	}

	now := time.Now()
	s.vipLookupMu.RLock()
	cacheKey := vipLookupCacheKey(tenantID, email)
	if entry, ok := s.vipLookupCache[cacheKey]; ok && now.Before(entry.expiresAt) {
		cached := cloneInvitationCode(entry.code)
		s.vipLookupMu.RUnlock()
		return cached, nil
	}
	s.vipLookupMu.RUnlock()

	item, err := s.repo.GetByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	s.cacheVIPLookup(tenantID, email, item)
	return cloneInvitationCode(item), nil
}

func (s *Service) cacheVIPLookup(tenantID, email string, item *store.InvitationCode) {
	s.vipLookupMu.Lock()
	defer s.vipLookupMu.Unlock()
	s.vipLookupCache[vipLookupCacheKey(tenantID, email)] = vipLookupCacheEntry{
		code:      cloneInvitationCode(item),
		expiresAt: time.Now().Add(vipLookupCacheTTL),
	}
}

func (s *Service) invalidateVIPLookup(tenantID, email string) {
	s.vipLookupMu.Lock()
	defer s.vipLookupMu.Unlock()
	delete(s.vipLookupCache, vipLookupCacheKey(tenantID, email))
}

func vipLookupCacheKey(tenantID, email string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	return tenantID + "\x00" + strings.TrimSpace(strings.ToLower(email))
}

func settingsKeyForTenant(tenantID, key string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return key
	}
	return "tenant:" + tenantID + ":" + key
}

func cloneInvitationCode(item *store.InvitationCode) *store.InvitationCode {
	if item == nil {
		return nil
	}
	cloned := *item
	if item.UsedAt != nil {
		usedAt := *item.UsedAt
		cloned.UsedAt = &usedAt
	}
	return &cloned
}

// CheckExpiry checks whether the invitation code associated with the given email has expired.
// Returns (expired, expiresAt, error).
// If no code is found or validity_days == 0, returns (false, nil, nil).
func (s *Service) CheckExpiry(ctx context.Context, email string) (bool, *time.Time, error) {
	return s.CheckExpiryForTenant(ctx, store.DefaultTenantID, email)
}

func (s *Service) CheckExpiryForTenant(ctx context.Context, tenantID, email string) (bool, *time.Time, error) {
	item, err := s.repo.GetByTenantEmail(ctx, tenantID, email)
	if err != nil {
		return false, nil, fmt.Errorf("checking expiry: %w", err)
	}
	if item == nil || item.ValidityDays == 0 {
		return false, nil, nil
	}

	if item.UsedAt == nil {
		return false, nil, nil
	}

	expiresAt := item.UsedAt.Add(time.Duration(item.ValidityDays) * 24 * time.Hour)
	expired := time.Now().After(expiresAt)
	return expired, &expiresAt, nil
}

// IsRequired reads the invitation_code_required setting from SystemSettings.
func (s *Service) IsRequired(ctx context.Context) (bool, error) {
	return s.IsRequiredForTenant(ctx, store.DefaultTenantID)
}

func (s *Service) IsRequiredForTenant(ctx context.Context, tenantID string) (bool, error) {
	if s == nil || s.settings == nil {
		return false, nil
	}
	raw, err := s.settings.Get(ctx, settingsKeyForTenant(tenantID, settingsKeyInvitationCodeRequired))
	if err != nil {
		return false, nil
	}
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	var payload struct {
		Value bool `json:"value"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false, nil
	}
	return payload.Value, nil
}

// SetRequired updates the invitation_code_required setting in SystemSettings.
func (s *Service) SetRequired(ctx context.Context, required bool) error {
	return s.SetRequiredForTenant(ctx, store.DefaultTenantID, required)
}

func (s *Service) SetRequiredForTenant(ctx context.Context, tenantID string, required bool) error {
	if s == nil || s.settings == nil {
		return nil
	}
	data, err := json.Marshal(map[string]bool{"value": required})
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	return s.settings.Set(ctx, settingsKeyForTenant(tenantID, settingsKeyInvitationCodeRequired), string(data))
}

// ListCodes delegates to the repository's List method.
func (s *Service) ListCodes(ctx context.Context, status string, search string) ([]*store.InvitationCode, error) {
	return s.repo.List(ctx, status, search)
}

// ExportUnusedCodes returns unused invitation codes filtered by exported status,
// and marks the returned codes as exported.
// exportedFilter: "unexported" (default), "exported", or "all".
func (s *Service) ExportUnusedCodes(ctx context.Context, exportedFilter string, vipOnly bool) ([]*store.InvitationCode, error) {
	return s.ExportUnusedCodesForTenant(ctx, store.DefaultTenantID, exportedFilter, vipOnly)
}

func (s *Service) ExportUnusedCodesForTenant(ctx context.Context, tenantID string, exportedFilter string, vipOnly bool) ([]*store.InvitationCode, error) {
	switch exportedFilter {
	case "exported", "all":
		// valid
	default:
		exportedFilter = "unexported"
	}
	codes, err := s.repo.ListUnusedByTenant(ctx, tenantID, exportedFilter, vipOnly)
	if err != nil {
		return nil, fmt.Errorf("listing unused codes: %w", err)
	}
	// Mark unexported codes as exported
	var toMark []string
	for _, c := range codes {
		if !c.Exported {
			toMark = append(toMark, c.ID)
		}
	}
	if len(toMark) > 0 {
		if err := s.repo.MarkExported(ctx, toMark); err != nil {
			return nil, fmt.Errorf("marking exported: %w", err)
		}
	}
	return codes, nil
}

// ListCodesPaged returns a page of invitation codes and the total count.
func (s *Service) ListCodesPaged(ctx context.Context, status string, search string, page, pageSize int) ([]*store.InvitationCode, int, error) {
	return s.ListCodesPagedForTenant(ctx, store.DefaultTenantID, status, search, page, pageSize)
}

func (s *Service) ListCodesPagedForTenant(ctx context.Context, tenantID string, status string, search string, page, pageSize int) ([]*store.InvitationCode, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.ListPagedByTenant(ctx, tenantID, status, search, offset, pageSize)
}

// generateCode generates a random 10-character code from A-Z0-9 using crypto/rand.
func generateCode() (string, error) {
	charsetLen := big.NewInt(int64(len(codeCharset)))
	buf := make([]byte, codeLength)
	for i := 0; i < codeLength; i++ {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", err
		}
		buf[i] = codeCharset[idx.Int64()]
	}
	return string(buf), nil
}

// randomShortID generates a random 16-character hex string for use as an ID suffix.
func randomShortID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", buf)
}
