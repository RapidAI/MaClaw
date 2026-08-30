package skillmarket

import (
	"context"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
)

const (
	defaultVoucherCount = 3
	defaultVoucherDays  = 7
)

// ErrEmailBoundToAnotherUser is returned when a Hub principal would be
// created under a contact that already belongs to a different market user,
// and the caller did not present that contact as Hub-verified.
var ErrEmailBoundToAnotherUser = errors.New("account email is already bound to another user")

// UserService 管理 SkillMarket 用户账户。
type UserService struct {
	store  *Store
	mailer *mail.Service
}

// NewUserService 创建 UserService。
func NewUserService(store *Store, mailer *mail.Service) *UserService {
	return &UserService{store: store, mailer: mailer}
}

// EnsureAccount 延迟创建：email 不存在则创建 unverified 账户并赠送体验券。
// 已存在则直接返回。
func (s *UserService) EnsureAccount(ctx context.Context, email string) (*SkillMarketUser, error) {
	email = normalizeEmail(email)
	u, err := s.store.GetUserByEmail(ctx, email)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	now := time.Now()
	user := &SkillMarketUser{
		ID:               generateID(),
		Email:            email,
		Status:           "unverified",
		Credits:          0,
		VoucherCount:     defaultVoucherCount,
		VoucherExpiresAt: now.Add(defaultVoucherDays * 24 * time.Hour),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		// 并发创建时可能 UNIQUE 冲突，重新查询
		if u2, err2 := s.store.GetUserByEmail(ctx, email); err2 == nil {
			return u2, nil
		}
		return nil, err
	}
	return user, nil
}

// EnsureAccountWithID creates a market user for a durable Hub principal.
// Email and phone are login contacts: an unmatched contact must never redirect
// this user ID onto another account's market assets.
func (s *UserService) EnsureAccountWithID(ctx context.Context, userID, email string) (*SkillMarketUser, error) {
	return s.ensureAccountWithID(ctx, userID, email, false)
}

// EnsureAccountWithVerifiedContact is EnsureAccountWithID for a contact the
// Hub (or an already-authenticated session) has proven belongs to this user.
// A pre-existing email/phone account is adopted so machine login can reopen
// the market instead of returning HTTP 500 on leftover email-first rows.
func (s *UserService) EnsureAccountWithVerifiedContact(ctx context.Context, userID, email string) (*SkillMarketUser, error) {
	return s.ensureAccountWithID(ctx, userID, email, true)
}

func (s *UserService) ensureAccountWithID(ctx context.Context, userID, email string, claimVerifiedContact bool) (*SkillMarketUser, error) {
	email = normalizeEmail(email)
	if userID != "" {
		if u, err := s.store.GetUserByID(ctx, userID); err == nil {
			return u, nil
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	if userID == "" {
		return s.EnsureAccount(ctx, email)
	}
	if existing, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return s.adoptOrRejectBoundEmail(existing, userID, claimVerifiedContact)
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now := time.Now()
	stubTime := time.Unix(0, 0).UTC()
	user := &SkillMarketUser{
		ID:               userID,
		Email:            email,
		Status:           "verified",
		VerifyMethod:     "session",
		Credits:          0,
		VoucherCount:     defaultVoucherCount,
		VoucherExpiresAt: now.Add(defaultVoucherDays * 24 * time.Hour),
		CreatedAt:        stubTime,
		UpdatedAt:        stubTime,
		VerifiedAt:       stubTime,
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		if u, err2 := s.store.GetUserByID(ctx, userID); err2 == nil {
			return u, nil
		}
		if u, err2 := s.store.GetUserByEmail(ctx, email); err2 == nil {
			return s.adoptOrRejectBoundEmail(u, userID, claimVerifiedContact)
		}
		return nil, err
	}
	return user, nil
}

func (s *UserService) adoptOrRejectBoundEmail(existing *SkillMarketUser, userID string, claimVerifiedContact bool) (*SkillMarketUser, error) {
	if existing == nil {
		return nil, ErrEmailBoundToAnotherUser
	}
	if existing.ID == userID || claimVerifiedContact {
		return existing, nil
	}
	return nil, ErrEmailBoundToAnotherUser
}

// VerifyAccount 将账户升级为 verified。
func (s *UserService) VerifyAccount(ctx context.Context, email, method string) (*SkillMarketUser, error) {
	email = normalizeEmail(email)
	u, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if u.Status == "verified" {
		return u, nil // 已验证
	}
	if err := s.store.UpdateUserStatus(ctx, u.ID, "verified", method); err != nil {
		return nil, err
	}
	u.Status = "verified"
	u.VerifyMethod = method
	u.VerifiedAt = time.Now()
	return u, nil
}

// GetAccount 获取账户信息。
func (s *UserService) GetAccount(ctx context.Context, email string) (*SkillMarketUser, error) {
	email = normalizeEmail(email)
	return s.store.GetUserByEmail(ctx, email)
}

// GetAccountByID 通过 ID 获取账户信息。
func (s *UserService) GetAccountByID(ctx context.Context, id string) (*SkillMarketUser, error) {
	return s.store.GetUserByID(ctx, id)
}
