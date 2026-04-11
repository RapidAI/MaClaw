package centers

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

var (
	ErrNotFound    = errors.New("center not found")
	ErrDisabled    = errors.New("center disabled")
	ErrUnauthorized = errors.New("unauthorized")
)

type RegisterRequest struct {
	CompanyName string `json:"company_name"`
	AdminEmail  string `json:"admin_email"`
	AdminPhone  string `json:"admin_phone"`
	Address     string `json:"address"`
	LegalPerson string `json:"legal_person"`
}

type RegisterResult struct {
	CenterID string `json:"center_id"`
	Secret   string `json:"secret"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

type Service struct {
	centers    store.CenterRepository
	licenseSvc *license.Service
	privKey    *rsa.PrivateKey
	httpClient *http.Client
}

func NewService(centers store.CenterRepository, licenseSvc *license.Service) *Service {
	return &Service{centers: centers, licenseSvc: licenseSvc, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

// SetPrivateKey sets the RSA private key for signing provision requests.
func (s *Service) SetPrivateKey(key *rsa.PrivateKey) {
	s.privKey = key
}

// Register creates a new center in pending status.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*RegisterResult, error) {
	if strings.TrimSpace(req.CompanyName) == "" || strings.TrimSpace(req.AdminEmail) == "" {
		return nil, fmt.Errorf("company_name and admin_email are required")
	}

	now := time.Now()
	rawSecret, err := randomToken()
	if err != nil {
		return nil, err
	}

	id := fmt.Sprintf("ctr_%d", now.UnixNano())
	c := &store.Center{
		ID:          id,
		CompanyName: strings.TrimSpace(req.CompanyName),
		AdminEmail:  strings.TrimSpace(req.AdminEmail),
		AdminPhone:  strings.TrimSpace(req.AdminPhone),
		Address:     strings.TrimSpace(req.Address),
		LegalPerson: strings.TrimSpace(req.LegalPerson),
		Status:      "pending",
		SecretHash:  hashSecret(rawSecret),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.centers.Create(ctx, c); err != nil {
		return nil, err
	}

	return &RegisterResult{
		CenterID: id,
		Secret:   rawSecret,
		Status:   "pending",
		Message:  "注册成功，等待管理员审核。",
	}, nil
}

// ConfirmWithTrial approves a center and issues a 30-day trial license (email confirm path).
func (s *Service) ConfirmWithTrial(ctx context.Context, centerID string) error {
	if err := s.centers.UpdateStatus(ctx, centerID, "active"); err != nil {
		return err
	}
	_, err := s.licenseSvc.IssueTrial(ctx, centerID)
	return err
}

// ConfirmManual approves a center and issues a manual license with custom duration.
func (s *Service) ConfirmManual(ctx context.Context, centerID string, modules []string, days int) error {
	if err := s.centers.UpdateStatus(ctx, centerID, "active"); err != nil {
		return err
	}
	_, err := s.licenseSvc.IssueManual(ctx, centerID, modules, days)
	return err
}

// Disable disables a center.
func (s *Service) Disable(ctx context.Context, centerID string) error {
	return s.centers.UpdateStatus(ctx, centerID, "disabled")
}

// Enable re-enables a center.
func (s *Service) Enable(ctx context.Context, centerID string) error {
	return s.centers.UpdateStatus(ctx, centerID, "active")
}

// Heartbeat updates the last heartbeat time.
func (s *Service) Heartbeat(ctx context.Context, centerID, rawSecret string) error {
	c, err := s.centers.GetByID(ctx, centerID)
	if err != nil {
		return ErrNotFound
	}
	if c.Status == "disabled" {
		return ErrDisabled
	}
	if c.SecretHash != hashSecret(rawSecret) {
		return ErrUnauthorized
	}
	return s.centers.UpdateHeartbeat(ctx, centerID)
}

// AuthenticateCenter validates the center secret and returns the center record.
// Returns ErrNotFound if the center does not exist, ErrUnauthorized if the secret
// is wrong, and ErrDisabled if the center is disabled (caller should handle 403).
func (s *Service) AuthenticateCenter(ctx context.Context, centerID, rawSecret string) (*store.Center, error) {
	c, err := s.centers.GetByID(ctx, centerID)
	if err != nil {
		return nil, ErrNotFound
	}
	if c.SecretHash != hashSecret(rawSecret) {
		return nil, ErrUnauthorized
	}
	return c, nil
}

// List returns all centers.
func (s *Service) List(ctx context.Context) ([]*store.Center, error) {
	return s.centers.List(ctx)
}

// Get returns a center by ID.
func (s *Service) Get(ctx context.Context, id string) (*store.Center, error) {
	return s.centers.GetByID(ctx, id)
}

// Delete removes a center.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.centers.Delete(ctx, id)
}

// ProvisionRequest is sent to an iWorkerCenter to create a tenant.
type ProvisionRequest struct {
	CompanyName   string `json:"company_name"`
	LegalPerson   string `json:"legal_person"`
	Email         string `json:"email"`
	Address       string `json:"address"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
	Timestamp     int64  `json:"timestamp"`
	Nonce         string `json:"nonce"`
	Signature     string `json:"signature"`
}

// ProvisionResult is returned by iWorkerCenter.
type ProvisionResult struct {
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// ProvisionRemote sends a signed provision request to an iWorkerCenter.
func (s *Service) ProvisionRemote(ctx context.Context, centerBaseURL string, companyName, legalPerson, email, address, adminUser, adminPass string) (*ProvisionResult, error) {
	if s.privKey == nil {
		return nil, fmt.Errorf("private key not set")
	}

	timestamp := time.Now().Unix()
	nonce, err := randomToken()
	if err != nil {
		return nil, err
	}

	// Build body without signature
	bodyMap := map[string]any{
		"company_name":   companyName,
		"legal_person":   legalPerson,
		"email":          email,
		"address":        address,
		"admin_username": adminUser,
		"admin_password": adminPass,
		"timestamp":      timestamp,
		"nonce":          nonce,
	}
	bodyJSON, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, err
	}

	// Sign: sha256(timestamp:nonce:sha256hex(body))
	bodyHash := sha256.Sum256(bodyJSON)
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	payload := fmt.Sprintf("%d:%s:%s", timestamp, nonce, bodyHashHex)
	payloadHash := sha256.Sum256([]byte(payload))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.privKey, crypto.SHA256, payloadHash[:])
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// Add signature to body
	bodyMap["signature"] = base64.StdEncoding.EncodeToString(sig)
	finalBody, _ := json.Marshal(bodyMap)

	url := strings.TrimRight(centerBaseURL, "/") + "/api/tenants/provision"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(finalBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provision request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provision failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result ProvisionResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode provision response: %w", err)
	}
	return &result, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashSecret(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
