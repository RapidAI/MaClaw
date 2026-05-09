package license

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

var (
	ErrInvalidDuration     = errors.New("license duration days must be zero or positive")
	ErrSignerNotConfigured = errors.New("license signer private key is not configured")
)

// CertificatePayload is the signed content of a license certificate.
type CertificatePayload struct {
	CenterID   string   `json:"center_id"`
	Modules    []string `json:"modules"`
	Type       string   `json:"type"`
	IssuedAt   string   `json:"issued_at"`
	ExpiresAt  string   `json:"expires_at"`
	IsLongTerm bool     `json:"is_long_term"`
}

// SignedCertificate is the full certificate with payload + signature.
type SignedCertificate struct {
	Payload   string `json:"payload"`   // base64 of JSON payload
	Signature string `json:"signature"` // base64 of RSA-SHA256 signature
}

type Service struct {
	licenses store.LicenseRepository
	privKey  *rsa.PrivateKey
}

func NewService(licenses store.LicenseRepository, privKey *rsa.PrivateKey) *Service {
	return &Service{licenses: licenses, privKey: privKey}
}

// IssueTrial creates a 30-day trial license for a center.
func (s *Service) IssueTrial(ctx context.Context, centerID string) (*store.License, error) {
	return s.issue(ctx, centerID, []string{"compute"}, "trial", 30)
}

// IssueManual creates a license with custom duration. days=0 means long-term.
func (s *Service) IssueManual(ctx context.Context, centerID string, modules []string, days int) (*store.License, error) {
	return s.issue(ctx, centerID, modules, "manual", days)
}

func (s *Service) issue(ctx context.Context, centerID string, modules []string, licType string, days int) (*store.License, error) {
	if days < 0 {
		return nil, ErrInvalidDuration
	}
	if s == nil || s.privKey == nil {
		return nil, ErrSignerNotConfigured
	}
	now := time.Now()
	isLongTerm := days == 0
	var expiresAt time.Time
	if isLongTerm {
		expiresAt = now.Add(100 * 365 * 24 * time.Hour) // far future
	} else {
		expiresAt = now.Add(time.Duration(days) * 24 * time.Hour)
	}

	payload := CertificatePayload{
		CenterID:   centerID,
		Modules:    modules,
		Type:       licType,
		IssuedAt:   now.Format(time.RFC3339),
		ExpiresAt:  expiresAt.Format(time.RFC3339),
		IsLongTerm: isLongTerm,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	sig, err := SignData(s.privKey, payloadJSON)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	cert := SignedCertificate{
		Payload:   base64.StdEncoding.EncodeToString(payloadJSON),
		Signature: base64.StdEncoding.EncodeToString(sig),
	}
	certJSON, _ := json.Marshal(cert)

	modulesJSON, _ := json.Marshal(modules)
	id := fmt.Sprintf("lic_%d", now.UnixNano())

	lic := &store.License{
		ID:          id,
		CenterID:    centerID,
		Modules:     string(modulesJSON),
		Type:        licType,
		ExpiresAt:   expiresAt,
		IsLongTerm:  isLongTerm,
		Certificate: string(certJSON),
		CreatedAt:   now,
	}

	if err := s.licenses.Create(ctx, lic); err != nil {
		return nil, err
	}
	return lic, nil
}

// Revoke revokes a license by ID.
func (s *Service) Revoke(ctx context.Context, id string) error {
	return s.licenses.Revoke(ctx, id)
}

// GetActive returns the active license for a center.
func (s *Service) GetActive(ctx context.Context, centerID string) (*store.License, error) {
	return s.licenses.GetActiveByCenterID(ctx, centerID)
}

// ListByCenter returns all licenses for a center.
func (s *Service) ListByCenter(ctx context.Context, centerID string) ([]*store.License, error) {
	return s.licenses.GetByCenterID(ctx, centerID)
}

// ListAll returns all licenses.
func (s *Service) ListAll(ctx context.Context) ([]*store.License, error) {
	return s.licenses.List(ctx)
}
