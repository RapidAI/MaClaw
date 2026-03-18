package invitation

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// --- in-memory mocks ---

type memInvitationCodeRepo struct {
	codes []*store.InvitationCode
}

func (m *memInvitationCodeRepo) Create(_ context.Context, item *store.InvitationCode) error {
	for _, c := range m.codes {
		if c.Code == item.Code {
			return errors.New("UNIQUE constraint failed")
		}
	}
	m.codes = append(m.codes, item)
	return nil
}

func (m *memInvitationCodeRepo) GetByCode(_ context.Context, code string) (*store.InvitationCode, error) {
	for _, c := range m.codes {
		if c.Code == code {
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

func (m *memInvitationCodeRepo) MarkUsed(_ context.Context, id string, email string, usedAt time.Time) error {
	for _, c := range m.codes {
		if c.ID == id {
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

	codes, err := svc.GenerateCodes(ctx, 5, 0)
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

func TestGenerateCodes_InvalidCount(t *testing.T) {
	svc := NewService(&memInvitationCodeRepo{}, newMemSettingsRepo())
	ctx := context.Background()

	for _, count := range []int{0, -1, 51, 100} {
		_, err := svc.GenerateCodes(ctx, count, 0)
		if !errors.Is(err, ErrInvalidCount) {
			t.Errorf("count=%d: expected ErrInvalidCount, got %v", count, err)
		}
	}
}

func TestValidateAndConsume_Success(t *testing.T) {
	repo := &memInvitationCodeRepo{}
	svc := NewService(repo, newMemSettingsRepo())
	ctx := context.Background()

	codes, err := svc.GenerateCodes(ctx, 1, 0)
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

	codes, _ := svc.GenerateCodes(ctx, 1, 0)
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

func TestListCodes(t *testing.T) {
	repo := &memInvitationCodeRepo{}
	svc := NewService(repo, newMemSettingsRepo())
	ctx := context.Background()

	codes, _ := svc.GenerateCodes(ctx, 3, 0)
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
