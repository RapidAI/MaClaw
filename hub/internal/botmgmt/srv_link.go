package botmgmt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// srvLink is one extra MaClawSrv the tenant admin registered by address.
// Unlike NodeLinks — one per edge node, dialed through the tunnel — a
// registered srv is reached directly at its own base_url. The public
// connection on the record stays the system default.
type srvLink struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	BaseURL     string `json:"base_url"`
	AdminSecret string `json:"admin_secret,omitempty"`
}

// SrvLinkInput is the admin save body. An empty ID creates a link. The
// admin secret is write-only: a nil secret keeps the stored one.
type SrvLinkInput struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	BaseURL     string  `json:"base_url"`
	AdminSecret *string `json:"admin_secret"`
}

// SrvLinkView is the admin payload. The secret never leaves the store.
type SrvLinkView struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	AdminSecretSet bool   `json:"admin_secret_set"`
}

func newSrvLinkID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "srv_" + fmt.Sprint(len(buf))
	}
	return "srv_" + hex.EncodeToString(buf[:])
}

func findSrvLink(links []srvLink, id string) *srvLink {
	id = strings.TrimSpace(id)
	for i := range links {
		if links[i].ID == id {
			return &links[i]
		}
	}
	return nil
}

func srvLinkViewOf(link srvLink) SrvLinkView {
	return SrvLinkView{
		ID:             link.ID,
		Name:           strings.TrimSpace(link.Name),
		BaseURL:        link.BaseURL,
		AdminSecretSet: strings.TrimSpace(link.AdminSecret) != "",
	}
}

// SrvLinks lists the registered extra maclawsrv connections for the admin
// page and the cloud-agent assignment pickers.
func (s *Service) SrvLinks(ctx context.Context, tenantID string) []SrvLinkView {
	out := []SrvLinkView{}
	if s == nil {
		return out
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return out
	}
	for _, link := range rec.SrvLinks {
		out = append(out, srvLinkViewOf(link))
	}
	return out
}

// SaveSrvLink registers or updates one extra maclawsrv. The id must be
// unique across node links too: a cloud-agent scope pin names one srv by
// that id and either map may hold it.
func (s *Service) SaveSrvLink(ctx context.Context, tenantID string, in SrvLinkInput) (SrvLinkView, error) {
	if s == nil {
		return SrvLinkView{}, fmt.Errorf("%w: settings store is unavailable", ErrInvalidInput)
	}
	name := strings.TrimSpace(in.Name)
	baseURL := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if len([]rune(name)) > 80 {
		return SrvLinkView{}, fmt.Errorf("%w: name is too long", ErrInvalidInput)
	}
	if baseURL == "" {
		return SrvLinkView{}, fmt.Errorf("%w: base_url is required", ErrInvalidInput)
	}
	if err := validateBaseURL(baseURL); err != nil {
		return SrvLinkView{}, err
	}
	id := strings.TrimSpace(in.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return SrvLinkView{}, err
	}
	if id == "" {
		for id == "" || findSrvLink(rec.SrvLinks, id) != nil || findNodeLink(rec.NodeLinks, id) != nil {
			id = newSrvLinkID()
		}
	}
	if findNodeLink(rec.NodeLinks, id) != nil {
		return SrvLinkView{}, fmt.Errorf("%w: this id belongs to a node srv", ErrInvalidInput)
	}
	link := findSrvLink(rec.SrvLinks, id)
	if link == nil {
		rec.SrvLinks = append(rec.SrvLinks, srvLink{ID: id})
		link = &rec.SrvLinks[len(rec.SrvLinks)-1]
	}
	link.Name = name
	link.BaseURL = baseURL
	if in.AdminSecret != nil {
		link.AdminSecret = strings.TrimSpace(*in.AdminSecret)
	}
	if err := s.save(ctx, tenantID, rec); err != nil {
		return SrvLinkView{}, err
	}
	return srvLinkViewOf(*link), nil
}

// DeleteSrvLink removes one registered srv. Scope pins that name it are
// left in place: the cloud agent side fails closed on a missing link so a
// deleted srv cannot silently fall back to the public connection.
func (s *Service) DeleteSrvLink(ctx context.Context, tenantID, id string) error {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return fmt.Errorf("%w: srv link is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	kept := rec.SrvLinks[:0]
	for _, link := range rec.SrvLinks {
		if link.ID != id {
			kept = append(kept, link)
		}
	}
	if len(kept) == len(rec.SrvLinks) {
		return nil
	}
	rec.SrvLinks = kept
	return s.save(ctx, tenantID, rec)
}
