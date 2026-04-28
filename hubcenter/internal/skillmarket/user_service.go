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

// VerifyAccount 将账户升级为 verified。
// 如果 email 已有 unverified 账户，直接接管（方案 A）。
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
