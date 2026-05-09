package license

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

func TestIssueManualRejectsNegativeDuration(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	repo := &memoryLicenseRepo{items: map[string]*store.License{}}
	svc := NewService(repo, key)

	_, err = svc.IssueManual(context.Background(), "ctr_1", []string{"compute"}, -1)
	if !errors.Is(err, ErrInvalidDuration) {
		t.Fatalf("IssueManual() error = %v, want ErrInvalidDuration", err)
	}
	if len(repo.items) != 0 {
		t.Fatalf("unexpected licenses created: %+v", repo.items)
	}
}

type memoryLicenseRepo struct {
	items map[string]*store.License
}

func (m *memoryLicenseRepo) Create(_ context.Context, l *store.License) error {
	copy := *l
	m.items[l.ID] = &copy
	return nil
}

func (m *memoryLicenseRepo) GetByID(_ context.Context, id string) (*store.License, error) {
	if item, ok := m.items[id]; ok {
		copy := *item
		return &copy, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *memoryLicenseRepo) GetByCenterID(_ context.Context, centerID string) ([]*store.License, error) {
	out := []*store.License{}
	for _, item := range m.items {
		if item.CenterID == centerID {
			copy := *item
			out = append(out, &copy)
		}
	}
	return out, nil
}

func (m *memoryLicenseRepo) GetActiveByCenterID(_ context.Context, centerID string) (*store.License, error) {
	now := time.Now()
	for _, item := range m.items {
		if item.CenterID == centerID && item.RevokedAt == nil && (item.IsLongTerm || item.ExpiresAt.After(now)) {
			copy := *item
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *memoryLicenseRepo) Revoke(_ context.Context, id string) error {
	if item, ok := m.items[id]; ok {
		now := time.Now()
		item.RevokedAt = &now
		return nil
	}
	return fmt.Errorf("not found")
}

func (m *memoryLicenseRepo) List(context.Context) ([]*store.License, error) {
	out := make([]*store.License, 0, len(m.items))
	for _, item := range m.items {
		copy := *item
		out = append(out, &copy)
	}
	return out, nil
}
