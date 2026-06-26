package invitation

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// --- in-memory mocks ---

type memInvitationCodeRepo struct {
	codes []*store.InvitationCode
}

func (m *memInvitationCodeRepo) Create(_ context.Context, item *store.InvitationCode) error {
	for _, c := range m.codes {
		if c.TenantID == item.TenantID && c.Code == item.Code {
			return errors.New("UNIQUE constraint failed")
		}
	}
	m.codes = append(m.codes, item)
	return nil
}

func (m *memInvitationCodeRepo) GetByID(_ context.Context, id string) (*store.InvitationCode, error) {
	for _, c := range m.codes {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}

func (m *memInvitationCodeRepo) GetByCode(_ context.Context, code string) (*store.InvitationCode, error) {
	for _, c := range m.codes {
		if c.Code == code {
			return c, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *memInvitationCodeRepo) GetByTenantCode(ctx context.Context, tenantID, code string) (*store.InvitationCode, error) {
	for _, c := range m.codes {
		if c.TenantID == tenantID && c.Code == code {
			return c, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *memInvitationCodeRepo) List(_ context.Context, status string, search string) ([]*store.InvitationCode, error) {
	var result []*store.InvitationCode
	for _, c := range m.codes {
		if status != "" && c.Status != status {
			continue
		}
		if search != "" {
			found := false
			for i := 0; i <= len(c.Code)-len(search); i++ {
				if c.Code[i:i+len(search)] == search {
					found = true
					break
				}
			}
			if !found && !strings.Contains(c.UsedByEmail, search) {
				continue
			}
		}
		result = append(result, c)
	}
	return result, nil
}

func (m *memInvitationCodeRepo) ListPaged(_ context.Context, status string, search string, offset, limit int) ([]*store.InvitationCode, int, error) {
	all, err := m.List(context.Background(), status, search)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (m *memInvitationCodeRepo) ListPagedByTenant(ctx context.Context, tenantID string, status string, search string, offset, limit int) ([]*store.InvitationCode, int, error) {
	var all []*store.InvitationCode
	for _, c := range m.codes {
		if c.TenantID != tenantID {
			continue
		}
		if status != "" && c.Status != status {
			continue
		}
		if search != "" && !strings.Contains(c.Code, search) && !strings.Contains(c.UsedByEmail, search) {
			continue
		}
		all = append(all, c)
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (m *memInvitationCodeRepo) MarkUsed(_ context.Context, id string, email string, usedAt time.Time) error {
	for _, c := range m.codes {
		if c.ID == id {
			if c.Status != "unused" {
				return errors.New("not found")
			}
			c.Status = "used"
			c.UsedByEmail = email
			c.UsedAt = &usedAt
			return nil
		}
	}
	return errors.New("not found")
}

func (m *memInvitationCodeRepo) Unbind(_ context.Context, id string) error {
	for _, c := range m.codes {
		if c.ID == id {
			c.Status = "unused"
			c.UsedByEmail = ""
			c.UsedAt = nil
			c.Exported = false
			return nil
		}
	}
	return errors.New("not found")
}

func (m *memInvitationCodeRepo) DeleteByID(_ context.Context, id string) error {
	for i, c := range m.codes {
		if c.ID == id {
			m.codes = append(m.codes[:i], m.codes[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

func (m *memInvitationCodeRepo) DeleteByEmail(_ context.Context, email string) (int64, error) {
	var kept []*store.InvitationCode
	var count int64
	for _, c := range m.codes {
		if c.UsedByEmail == email && c.Status == "used" {
			count++
		} else {
			kept = append(kept, c)
		}
	}
	m.codes = kept
	return count, nil
}

func (m *memInvitationCodeRepo) DeleteByTenantEmail(ctx context.Context, tenantID, email string) (int64, error) {
	var kept []*store.InvitationCode
	var count int64
	for _, c := range m.codes {
		if c.TenantID == tenantID && c.UsedByEmail == email && c.Status == "used" {
			count++
		} else {
			kept = append(kept, c)
		}
	}
	m.codes = kept
	return count, nil
}

func (m *memInvitationCodeRepo) GetByEmail(_ context.Context, email string) (*store.InvitationCode, error) {
	var latest *store.InvitationCode
	for _, c := range m.codes {
		if c.UsedByEmail == email && c.Status == "used" {
			if latest == nil || (c.UsedAt != nil && (latest.UsedAt == nil || c.UsedAt.After(*latest.UsedAt))) {
				latest = c
			}
		}
	}
	return latest, nil
}

func (m *memInvitationCodeRepo) GetByTenantEmail(ctx context.Context, tenantID, email string) (*store.InvitationCode, error) {
	var latest *store.InvitationCode
	for _, c := range m.codes {
		if c.TenantID == tenantID && c.UsedByEmail == email && c.Status == "used" {
			if latest == nil || (c.UsedAt != nil && (latest.UsedAt == nil || c.UsedAt.After(*latest.UsedAt))) {
				latest = c
			}
		}
	}
	return latest, nil
}

func (m *memInvitationCodeRepo) ListUnused(_ context.Context, exportedFilter string, vipOnly ...bool) ([]*store.InvitationCode, error) {
	filterVIP := len(vipOnly) > 0 && vipOnly[0]
	var result []*store.InvitationCode
	for _, c := range m.codes {
		if c.Status != "unused" {
			continue
		}
		if filterVIP && !c.VIP {
			continue
		}
		switch exportedFilter {
		case "exported":
			if !c.Exported {
				continue
			}
		case "all":
			// no filter
		default:
			// "unexported" or unknown defaults to unexported
			if c.Exported {
				continue
			}
		}
		result = append(result, c)
	}
	return result, nil
}

func (m *memInvitationCodeRepo) ListUnusedByTenant(ctx context.Context, tenantID, exportedFilter string, vipOnly ...bool) ([]*store.InvitationCode, error) {
	all, err := m.ListUnused(ctx, exportedFilter, vipOnly...)
	if err != nil {
		return nil, err
	}
	var out []*store.InvitationCode
	for _, c := range all {
		if c.TenantID == tenantID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memInvitationCodeRepo) MarkExported(_ context.Context, ids []string) error {
	for _, id := range ids {
		for _, c := range m.codes {
			if c.ID == id {
				c.Exported = true
			}
		}
	}
	return nil
}

type memSettingsRepo struct {
	data map[string]string
}

func newMemSettingsRepo() *memSettingsRepo {
	return &memSettingsRepo{data: make(map[string]string)}
}

func (m *memSettingsRepo) Set(_ context.Context, key, valueJSON string) error {
	m.data[key] = valueJSON
	return nil
}

func (m *memSettingsRepo) Get(_ context.Context, key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", nil
	}
	return v, nil
}

// --- tests ---

var codePattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)

func TestGenerateCodes_ValidCount(t *testing.T) {
	svc := NewService(&memInvitationCodeRepo{}, newMemSettingsRepo())
	ctx := context.Background()

	codes, err := svc.GenerateCodes(ctx, 5, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(codes) != 5 {
		t.Fatalf("expected 5 codes, got %d", len(codes))
	}

	seen := make(map[string]bool)
	for _, c := range codes {
		if !codePattern.MatchString(c.Code) {
			t.Errorf("code %q does not match expected pattern", c.Code)
		}
		if c.Status != "unused" {
			t.Errorf("expected status 'unused', got %q", c.Status)
		}
		if seen[c.Code] {
			t.Errorf("duplicate code: %s", c.Code)
		}
		seen[c.Code] = true
	}
}

func TestGenerateCodesWithLLMGrantOptions(t *testing.T) {
	settings := newMemSettingsRepo()
	if err := llmservice.SaveRegistry(context.Background(), settings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-pro", Name: "Coding Pro"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	repo := &memInvitationCodeRepo{}
	svc := NewService(repo, settings)

	codes, err := svc.GenerateCodesForTenantWithOptions(context.Background(), "tenant_a", GenerateCodeOptions{
		Count:                1,
		LLMServiceGroupID:    "coding-pro",
		LLMGrantDurationDays: 14,
		LLMGrantCredits:      2500,
	})
	if err != nil {
		t.Fatalf("GenerateCodesForTenantWithOptions: %v", err)
	}
	if len(codes) != 1 {
		t.Fatalf("len(codes) = %d, want 1", len(codes))
	}
	code := codes[0]
	if code.LLMServiceGroupID != "coding-pro" || code.LLMGrantDurationDays != 14 || code.LLMGrantCredits != 2500 {
		t.Fatalf("unexpected grant options: %#v", code)
	}
}

func TestGenerateCodesWithInvalidLLMGrantOptions(t *testing.T) {
	settings := newMemSettingsRepo()
	if err := llmservice.SaveRegistry(context.Background(), settings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-pro", Name: "Coding Pro"}},
	}); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	svc := NewService(&memInvitationCodeRepo{}, settings)

	_, err := svc.GenerateCodesForTenantWithOptions(context.Background(), "tenant_a", GenerateCodeOptions{
		Count:                1,
		LLMServiceGroupID:    "missing",
		LLMGrantDurationDays: 14,
		LLMGrantCredits:      2500,
	})
	if !errors.Is(err, ErrInvalidLLMGrant) {
		t.Fatalf("err = %v, want ErrInvalidLLMGrant", err)
	}

	_, err = svc.GenerateCodesForTenantWithOptions(context.Background(), "tenant_a", GenerateCodeOptions{
		Count:                1,
		LLMServiceGroupID:    "coding-pro",
		LLMGrantDurationDays: 14,
		LLMGrantCredits:      math.NaN(),
	})
	if !errors.Is(err, ErrInvalidLLMGrant) {
		t.Fatalf("NaN err = %v, want ErrInvalidLLMGrant", err)
	}
}

func TestGenerateCodes_InvalidCount(t *testing.T) {
	svc := NewService(&memInvitationCodeRepo{}, newMemSettingsRepo())
	ctx := context.Background()

	for _, count := range []int{0, -1, 51, 100} {
		_, err := svc.GenerateCodes(ctx, count, 0, false)
		if !errors.Is(err, ErrInvalidCount) {
			t.Errorf("count=%d: expected ErrInvalidCount, got %v", count, err)
		}
	}
}

func TestValidateAndConsume_Success(t *testing.T) {
	repo := &memInvitationCodeRepo{}
	svc := NewService(repo, newMemSettingsRepo())
	ctx := context.Background()

	codes, err := svc.GenerateCodes(ctx, 1, 0, false)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	err = svc.ValidateAndConsume(ctx, codes[0].Code, "user@example.com")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Verify it's now used
	item, _ := repo.GetByCode(ctx, codes[0].Code)
	if item.Status != "used" {
		t.Errorf("expected status 'used', got %q", item.Status)
	}
	if item.UsedByEmail != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got %q", item.UsedByEmail)
	}
	if item.UsedAt == nil {
		t.Error("expected UsedAt to be set")
	}
}

func TestValidateAndConsume_AlreadyUsed(t *testing.T) {
	repo := &memInvitationCodeRepo{}
	svc := NewService(repo, newMemSettingsRepo())
	ctx := context.Background()

	codes, _ := svc.GenerateCodes(ctx, 1, 0, false)
	_ = svc.ValidateAndConsume(ctx, codes[0].Code, "first@example.com")

	err := svc.ValidateAndConsume(ctx, codes[0].Code, "second@example.com")
	if !errors.Is(err, ErrInvalidInvitationCode) {
		t.Errorf("expected ErrInvalidInvitationCode, got %v", err)
	}
}

func TestValidateAndConsume_EmptyCode(t *testing.T) {
	svc := NewService(&memInvitationCodeRepo{}, newMemSettingsRepo())
	err := svc.ValidateAndConsume(context.Background(), "", "user@example.com")
	if !errors.Is(err, ErrInvalidInvitationCode) {
		t.Errorf("expected ErrInvalidInvitationCode, got %v", err)
	}
}

func TestValidateAndConsume_NonexistentCode(t *testing.T) {
	svc := NewService(&memInvitationCodeRepo{}, newMemSettingsRepo())
	err := svc.ValidateAndConsume(context.Background(), "XXXXXXXXXX", "user@example.com")
	if !errors.Is(err, ErrInvalidInvitationCode) {
		t.Errorf("expected ErrInvalidInvitationCode, got %v", err)
	}
}

func TestIsRequired_DefaultFalse(t *testing.T) {
	svc := NewService(&memInvitationCodeRepo{}, newMemSettingsRepo())
	required, err := svc.IsRequired(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if required {
		t.Error("expected default to be false")
	}
}

func TestSetRequired_RoundTrip(t *testing.T) {
	svc := NewService(&memInvitationCodeRepo{}, newMemSettingsRepo())
	ctx := context.Background()

	if err := svc.SetRequired(ctx, true); err != nil {
		t.Fatalf("set true: %v", err)
	}
	val, err := svc.IsRequired(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !val {
		t.Error("expected true after setting true")
	}

	if err := svc.SetRequired(ctx, false); err != nil {
		t.Fatalf("set false: %v", err)
	}
	val, err = svc.IsRequired(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if val {
		t.Error("expected false after setting false")
	}
}

func TestSetRequiredForTenantIsolation(t *testing.T) {
	settings := newMemSettingsRepo()
	svc := NewService(&memInvitationCodeRepo{}, settings)
	ctx := context.Background()

	if err := svc.SetRequiredForTenant(ctx, "tenant_a", true); err != nil {
		t.Fatalf("set tenant_a: %v", err)
	}
	globalRequired, err := svc.IsRequired(ctx)
	if err != nil {
		t.Fatalf("get global: %v", err)
	}
	tenantARequired, err := svc.IsRequiredForTenant(ctx, "tenant_a")
	if err != nil {
		t.Fatalf("get tenant_a: %v", err)
	}
	tenantBRequired, err := svc.IsRequiredForTenant(ctx, "tenant_b")
	if err != nil {
		t.Fatalf("get tenant_b: %v", err)
	}
	if globalRequired || !tenantARequired || tenantBRequired {
		t.Fatalf("required flags global=%t tenant_a=%t tenant_b=%t", globalRequired, tenantARequired, tenantBRequired)
	}
}

func TestDeleteCodeByEmail(t *testing.T) {
	repo := &memInvitationCodeRepo{}
	svc := NewService(repo, newMemSettingsRepo())
	ctx := context.Background()

	codes, _ := svc.GenerateCodes(ctx, 3, 0, false)
	_ = svc.ValidateAndConsume(ctx, codes[0].Code, "alice@example.com")
	_ = svc.ValidateAndConsume(ctx, codes[1].Code, "alice@example.com")
	// codes[2] stays unused

	deleted, err := svc.DeleteCodeByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}

	// The unused code should still exist
	all, _ := svc.ListCodes(ctx, "", "")
	if len(all) != 1 {
		t.Errorf("expected 1 remaining code, got %d", len(all))
	}

	// Deleting again should return 0
	deleted, err = svc.DeleteCodeByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted on second call, got %d", deleted)
	}

	// Empty email should return 0
	deleted, _ = svc.DeleteCodeByEmail(ctx, "")
	if deleted != 0 {
		t.Errorf("expected 0 for empty email, got %d", deleted)
	}
}

func TestGetCodeByTenantEmailCacheIsTenantScoped(t *testing.T) {
	repo := &memInvitationCodeRepo{}
	svc := NewService(repo, newMemSettingsRepo())
	ctx := context.Background()
	now := time.Now()

	repo.codes = append(repo.codes,
		&store.InvitationCode{ID: "a", TenantID: "tenant_a", Code: "AAAAAAAAAA", Status: "used", UsedByEmail: "same@example.com", UsedAt: &now, VIP: true, CreatedAt: now},
		&store.InvitationCode{ID: "b", TenantID: "tenant_b", Code: "BBBBBBBBBB", Status: "used", UsedByEmail: "same@example.com", UsedAt: &now, VIP: false, CreatedAt: now},
	)

	first, err := svc.GetCodeByTenantEmail(ctx, "tenant_a", "same@example.com")
	if err != nil {
		t.Fatalf("tenant a lookup: %v", err)
	}
	second, err := svc.GetCodeByTenantEmail(ctx, "tenant_b", "same@example.com")
	if err != nil {
		t.Fatalf("tenant b lookup: %v", err)
	}
	if first == nil || first.Code != "AAAAAAAAAA" || !first.VIP {
		t.Fatalf("unexpected tenant a code: %#v", first)
	}
	if second == nil || second.Code != "BBBBBBBBBB" || second.VIP {
		t.Fatalf("unexpected tenant b code: %#v", second)
	}
}

func TestCodeRemovalInvalidatesCachedTenantEmailLookup(t *testing.T) {
	ctx := context.Background()
	usedAt := time.Now()
	for _, tt := range []struct {
		name string
		run  func(*Service) error
	}{
		{
			name: "unbind",
			run: func(svc *Service) error {
				return svc.UnbindCode(ctx, "ic_cached")
			},
		},
		{
			name: "delete",
			run: func(svc *Service) error {
				return svc.DeleteCode(ctx, "ic_cached")
			},
		},
		{
			name: "delete_by_email",
			run: func(svc *Service) error {
				_, err := svc.DeleteCodeByTenantEmail(ctx, store.DefaultTenantID, "cached@example.com")
				return err
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &memInvitationCodeRepo{codes: []*store.InvitationCode{{
				ID:                   "ic_cached",
				TenantID:             store.DefaultTenantID,
				Code:                 "CACHED",
				Status:               "used",
				UsedByEmail:          "cached@example.com",
				UsedAt:               &usedAt,
				LLMServiceGroupID:    "invite-pro",
				LLMGrantDurationDays: 7,
				LLMGrantCredits:      10,
				CreatedAt:            usedAt,
			}}}
			svc := NewService(repo, newMemSettingsRepo())
			if got, err := svc.GetCodeByTenantEmail(ctx, store.DefaultTenantID, "cached@example.com"); err != nil || got == nil {
				t.Fatalf("prime cached lookup: code=%+v err=%v", got, err)
			}
			if err := tt.run(svc); err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			got, err := svc.GetCodeByTenantEmail(ctx, store.DefaultTenantID, "cached@example.com")
			if err != nil {
				t.Fatalf("lookup after %s: %v", tt.name, err)
			}
			if got != nil {
				t.Fatalf("expected no cached code after %s, got %+v", tt.name, got)
			}
		})
	}
}

func TestListCodes(t *testing.T) {
	repo := &memInvitationCodeRepo{}
	svc := NewService(repo, newMemSettingsRepo())
	ctx := context.Background()

	codes, _ := svc.GenerateCodes(ctx, 3, 0, false)
	_ = svc.ValidateAndConsume(ctx, codes[0].Code, "user@example.com")

	all, err := svc.ListCodes(ctx, "", "")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3, got %d", len(all))
	}

	unused, err := svc.ListCodes(ctx, "unused", "")
	if err != nil {
		t.Fatalf("list unused: %v", err)
	}
	if len(unused) != 2 {
		t.Errorf("expected 2 unused, got %d", len(unused))
	}

	used, err := svc.ListCodes(ctx, "used", "")
	if err != nil {
		t.Fatalf("list used: %v", err)
	}
	if len(used) != 1 {
		t.Errorf("expected 1 used, got %d", len(used))
	}
}
