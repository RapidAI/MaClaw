package invitation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	codeCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLength  = 10
	maxRetries  = 3
	maxCount    = 50

	settingsKeyInvitationCodeRequired = "invitation_code_required"
)

var (
	ErrInvalidCount         = errors.New("count must be between 1 and 50")
	ErrCodeConflict         = errors.New("failed to generate unique invitation code after retries")
	ErrInvalidInvitationCode = errors.New("invalid or used invitation code")
)

type Service struct {
	repo     store.InvitationCodeRepository
	settings store.SystemSettingsRepository
}

func NewService(repo store.InvitationCodeRepository, settings store.SystemSettingsRepository) *Service {
	return &Service{repo: repo, settings: settings}
}

// GenerateCodes generates count invitation codes (1-50) and stores them.
func (s *Service) GenerateCodes(ctx context.Context, count int) ([]*store.InvitationCode, error) {
	if count < 1 || count > maxCount {
		return nil, ErrInvalidCount
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
			existing, _ := s.repo.GetByCode(ctx, code)
			if existing != nil {
				continue // conflict, retry
			}

			now := time.Now()
			item := &store.InvitationCode{
				ID:        fmt.Sprintf("ic_%s", randomShortID()),
				Code:      code,
				Status:    "unused",
				CreatedAt: now,
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
	return codes, nil
}

// ValidateAndConsume validates that the code exists and is unused, then marks it used.
func (s *Service) ValidateAndConsume(ctx context.Context, code string, email string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrInvalidInvitationCode
	}

	item, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return ErrInvalidInvitationCode
	}
	if item == nil || item.Status != "unused" {
		return ErrInvalidInvitationCode
	}

	return s.repo.MarkUsed(ctx, item.ID, email, time.Now())
}

// IsRequired reads the invitation_code_required setting from SystemSettings.
func (s *Service) IsRequired(ctx context.Context) (bool, error) {
	if s.settings == nil {
		return false, nil
	}
	raw, err := s.settings.Get(ctx, settingsKeyInvitationCodeRequired)
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
	if s.settings == nil {
		return nil
	}
	data, err := json.Marshal(map[string]bool{"value": required})
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	return s.settings.Set(ctx, settingsKeyInvitationCodeRequired, string(data))
}

// ListCodes delegates to the repository's List method.
func (s *Service) ListCodes(ctx context.Context, status string, search string) ([]*store.InvitationCode, error) {
	return s.repo.List(ctx, status, search)
}

// ListCodesPaged returns a page of invitation codes and the total count.
func (s *Service) ListCodesPaged(ctx context.Context, status string, search string, page, pageSize int) ([]*store.InvitationCode, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.ListPaged(ctx, status, search, offset, pageSize)
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
