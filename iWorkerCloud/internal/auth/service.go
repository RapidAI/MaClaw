package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotSetup       = errors.New("admin not setup")
	ErrAlreadySetup   = errors.New("admin already setup")
	ErrInvalidLogin   = errors.New("invalid credentials")
	ErrInvalidCaptcha = errors.New("invalid captcha answer")
)

// MathCaptcha holds a pending math verification.
type MathCaptcha struct {
	Question string `json:"question"`
	ID       string `json:"id"`
}

type Service struct {
	admins  store.AdminRepository
	captchas sync.Map // id -> answer string
}

func NewService(admins store.AdminRepository) *Service {
	return &Service{admins: admins}
}

// IsSetup checks if an admin account exists.
func (s *Service) IsSetup(ctx context.Context) (bool, error) {
	n, err := s.admins.Count(ctx)
	return n > 0, err
}

// Setup creates the initial admin account.
func (s *Service) Setup(ctx context.Context, username, password string) error {
	ok, _ := s.IsSetup(ctx)
	if ok {
		return ErrAlreadySetup
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.admins.Create(ctx, &store.Admin{
		ID:           fmt.Sprintf("adm_%d", time.Now().UnixNano()),
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	})
}

// GenerateCaptcha creates a math captcha (e.g. "12 + 7 = ?").
func (s *Service) GenerateCaptcha() MathCaptcha {
	a := randInt(10, 50)
	b := randInt(1, 30)
	answer := fmt.Sprintf("%d", a+b)
	id := randomHex(8)
	s.captchas.Store(id, answer)
	// auto-expire after 5 minutes
	go func() {
		time.Sleep(5 * time.Minute)
		s.captchas.Delete(id)
	}()
	return MathCaptcha{
		Question: fmt.Sprintf("%d + %d = ?", a, b),
		ID:       id,
	}
}

// VerifyCaptcha checks the captcha answer.
func (s *Service) VerifyCaptcha(id, answer string) bool {
	v, ok := s.captchas.LoadAndDelete(id)
	if !ok {
		return false
	}
	return v.(string) == answer
}

// Login verifies credentials and returns a session token.
func (s *Service) Login(ctx context.Context, username, password, captchaID, captchaAnswer string) (string, error) {
	if !s.VerifyCaptcha(captchaID, captchaAnswer) {
		return "", ErrInvalidCaptcha
	}
	admin, err := s.admins.GetByUsername(ctx, username)
	if err != nil {
		return "", ErrInvalidLogin
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		return "", ErrInvalidLogin
	}
	token := generateSessionToken(admin.ID)
	return token, nil
}

// ChangePassword updates the admin password.
func (s *Service) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	admin, err := s.admins.GetByUsername(ctx, username)
	if err != nil {
		return ErrInvalidLogin
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(oldPassword)); err != nil {
		return ErrInvalidLogin
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.admins.UpdatePassword(ctx, admin.ID, string(hash))
}

func generateSessionToken(adminID string) string {
	b := make([]byte, 32)
	rand.Read(b)
	mac := hmac.New(sha256.New, b)
	mac.Write([]byte(adminID))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func randInt(min, max int) int {
	b := make([]byte, 1)
	rand.Read(b)
	return min + int(b[0])%(max-min+1)
}
