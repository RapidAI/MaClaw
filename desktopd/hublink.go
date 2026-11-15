package desktopd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

// HubLink keeps this desktopd connected to Hub as a desktop-only Bot node.
// Hub reaches the desktop API and the CDP/noVNC tunnel through it, so an
// intranet desktopd needs no public address. Credentials live in StatePath
// (0600); the node secret is never logged.
type HubLink struct {
	StatePath string
	// Self is the address Hub-opened streams are forwarded to: this
	// process's own listener.
	Self string
	// Key is the primary DESKTOPD_TOKEN. It stays on this machine. When Keys
	// is set, enrollment reports a separate hub-<nodeID> key instead.
	Key    string
	Keys   HubKeyStore
	Client *http.Client

	mu      sync.Mutex
	base    context.Context
	creds   edgelink.Credentials
	cancel  context.CancelFunc
	online  bool
	lastErr string
	since   time.Time
}

// HubLinkStatus is what the GUI shows. It has no secret.
type HubLinkStatus struct {
	Enrolled bool   `json:"enrolled"`
	HubURL   string `json:"hub_url,omitempty"`
	NodeID   string `json:"node_id,omitempty"`
	Online   bool   `json:"online"`
	Error    string `json:"error,omitempty"`
	Since    string `json:"since,omitempty"`
	SrvAddr  string `json:"srv_addr,omitempty"`
}

// SelfAddr turns a listen address into one this host can dial.
func SelfAddr(listen string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil || port == "" {
		return "127.0.0.1:18081"
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// Start resumes a saved link. A missing state file is not an error.
func (h *HubLink) Start(ctx context.Context) error {
	if h == nil {
		return nil
	}
	creds, err := edgelink.LoadCredentials(h.StatePath)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.base = ctx
	if creds.NodeID == "" || creds.NodeSecret == "" || creds.HubURL == "" {
		return nil
	}
	h.creds = creds
	h.runLocked()
	return nil
}

// HubKeyStore keeps the extra API key Hub uses to call this desktopd.
// The primary DESKTOPD_TOKEN is not written here.
type HubKeyStore interface {
	PutHubKey(label, token string) error
	DropHubKeys() error
}

// HubLinkEnroll is one registration. SrvAdmin is sent once and is not stored.
type HubLinkEnroll struct {
	HubURL     string
	Code       string
	HasSrv     bool
	SrvAddr    string
	SrvAdmin   string
	DirectBase string
}

// EnrollFromEnv registers on first start when DESKTOPD_HUB_URL and
// DESKTOPD_HUB_ENROLL_CODE are set. An enrolled node is left alone. The code
// is not logged; Hub invalidates it when enrollment succeeds.
func (h *HubLink) EnrollFromEnv(ctx context.Context) {
	if h == nil || strings.TrimSpace(h.NodeID()) != "" {
		return
	}
	hub := strings.TrimSpace(os.Getenv("DESKTOPD_HUB_URL"))
	code := strings.TrimSpace(os.Getenv("DESKTOPD_HUB_ENROLL_CODE"))
	if hub == "" || code == "" {
		return
	}
	if _, err := h.Enroll(ctx, hub, code); err != nil {
		log.Printf("[desktopd] hub enroll from env failed hub=%s err=%s", hub, redactSecret(err.Error(), code))
		return
	}
	log.Printf("[desktopd] hub enroll from env ok hub=%s node=%s", hub, h.NodeID())
}

func redactSecret(msg, secret string) string {
	if secret != "" {
		msg = strings.ReplaceAll(msg, secret, "<redacted>")
	}
	return scrubDesktopdLog(msg)
}

// Enroll trades a one-time code from Hub for node credentials and starts the
// link. A previous enrollment is replaced only after the new one succeeds.
func (h *HubLink) Enroll(ctx context.Context, hubURL, code string) (HubLinkStatus, error) {
	return h.EnrollWith(ctx, HubLinkEnroll{HubURL: hubURL, Code: code})
}

// EnrollWith is Enroll plus an optional same-machine srv.
func (h *HubLink) EnrollWith(ctx context.Context, in HubLinkEnroll) (HubLinkStatus, error) {
	if h == nil {
		return HubLinkStatus{}, fmt.Errorf("%w: hub link is disabled", ErrInvalid)
	}
	hub, err := edgelink.NormalizeHubURL(in.HubURL)
	if err != nil {
		return HubLinkStatus{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	code := strings.TrimSpace(in.Code)
	if code == "" || len(code) > 128 {
		return HubLinkStatus{}, fmt.Errorf("%w: enrollment code is required", ErrInvalid)
	}
	if in.HasSrv && strings.TrimSpace(in.SrvAddr) == "" {
		return HubLinkStatus{}, fmt.Errorf("%w: srv address is required", ErrInvalid)
	}
	if strings.TrimSpace(h.Key) == "" && h.Keys == nil {
		return HubLinkStatus{}, fmt.Errorf("%w: DESKTOPD_TOKEN is not set", ErrInvalid)
	}
	desktopdKey := strings.TrimSpace(h.Key)
	if h.Keys != nil {
		issued, err := randomHex(24)
		if err != nil {
			return HubLinkStatus{}, err
		}
		desktopdKey = issued
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	creds, err := edgelink.Enroll(ctx, client, hub, edgelink.EnrollRequest{
		Code:        code,
		DesktopdKey: desktopdKey,
		SrvAdmin:    in.SrvAdmin,
		Version:     "desktopd",
		HasDesktopd: true,
		HasSrv:      in.HasSrv,
		DirectBase:  in.DirectBase,
	})
	if err != nil {
		log.Printf("[desktopd] hub enroll failed hub=%s err=%s", hub, redactSecret(err.Error(), code))
		return HubLinkStatus{}, err
	}
	if h.Keys != nil {
		label, err := hubKeyLabel(creds.NodeID)
		if err != nil {
			log.Printf("[desktopd] hub key label rejected node=%s err=%v", creds.NodeID, err)
			return HubLinkStatus{}, err
		}
		if err := h.Keys.PutHubKey(label, desktopdKey); err != nil {
			log.Printf("[desktopd] hub key was not saved node=%s err=%v", creds.NodeID, err)
			return HubLinkStatus{}, err
		}
		log.Printf("[desktopd] hub key saved node=%s label=%s", creds.NodeID, label)
	}
	creds.SrvAddr = strings.TrimSpace(in.SrvAddr)
	if err := edgelink.SaveCredentials(h.StatePath, creds); err != nil {
		log.Printf("[desktopd] hub credentials were not saved node=%s err=%v", creds.NodeID, err)
		if h.Keys != nil {
			_ = h.Keys.DropHubKeys()
		}
		return HubLinkStatus{}, err
	}
	h.mu.Lock()
	h.stopLocked()
	h.creds = creds
	h.lastErr = ""
	h.runLocked()
	h.mu.Unlock()
	log.Printf("[desktopd] hub link enrolled node=%s hub=%s", creds.NodeID, creds.HubURL)
	return h.Status(), nil
}

// Disconnect stops the tunnel and keeps the saved credentials.
func (h *HubLink) Disconnect() HubLinkStatus {
	if h == nil {
		return HubLinkStatus{}
	}
	h.mu.Lock()
	h.stopLocked()
	h.online = false
	h.mu.Unlock()
	return h.Status()
}

// Reconnect opens the tunnel again from the saved credentials.
func (h *HubLink) Reconnect() (HubLinkStatus, error) {
	if h == nil {
		return HubLinkStatus{}, fmt.Errorf("%w: hub link is disabled", ErrInvalid)
	}
	h.mu.Lock()
	if h.creds.NodeID == "" || h.creds.NodeSecret == "" || h.creds.HubURL == "" {
		h.mu.Unlock()
		return HubLinkStatus{}, fmt.Errorf("%w: hub link is not enrolled", ErrInvalid)
	}
	h.stopLocked()
	h.runLocked()
	h.mu.Unlock()
	return h.Status(), nil
}

// Leave stops the link and forgets the credentials.
func (h *HubLink) Leave() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	h.stopLocked()
	h.creds = edgelink.Credentials{}
	h.lastErr = ""
	h.mu.Unlock()
	if err := os.Remove(h.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if h.Keys != nil {
		if err := h.Keys.DropHubKeys(); err != nil {
			log.Printf("[desktopd] hub key was not removed err=%v", err)
			return err
		}
	}
	return nil
}

func hubKeyLabel(nodeID string) (string, error) {
	id := strings.TrimSpace(nodeID)
	if id == "" || len(id) > 80 {
		return "", fmt.Errorf("%w: node id", ErrInvalid)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return "", fmt.Errorf("%w: node id", ErrInvalid)
		}
	}
	return "hub-" + id, nil
}

// BoundTenant is the tenant this desktop accepted at enrollment.
// Empty means it is not registered and still serves every tenant.
func (h *HubLink) BoundTenant() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.TrimSpace(h.creds.TenantID)
}

// NodeID is the enrolled node, or "" before enrollment.
func (h *HubLink) NodeID() string {
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.creds.NodeID
}

// ConfirmViewer asks Hub whether this direct-view ticket is still allowed.
// The node secret is sent as the bearer and is not logged.
func (h *HubLink) ConfirmViewer(ctx context.Context, id string) error {
	if h == nil {
		return fmt.Errorf("%w: hub link is disabled", ErrInvalid)
	}
	h.mu.Lock()
	hub := strings.TrimRight(h.creds.HubURL, "/")
	nodeID := h.creds.NodeID
	secret := h.creds.NodeSecret
	client := h.Client
	h.mu.Unlock()
	if hub == "" || nodeID == "" || secret == "" || strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: hub link is not enrolled", ErrInvalid)
	}
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hub+"/api/v1/edge/viewer/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Node-Id", nodeID)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: viewer was withdrawn", ErrInvalid)
	}
	return nil
}

func (h *HubLink) Status() HubLinkStatus {
	if h == nil {
		return HubLinkStatus{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := HubLinkStatus{
		Enrolled: h.creds.NodeID != "",
		HubURL:   h.creds.HubURL,
		NodeID:   h.creds.NodeID,
		Online:   h.online,
		Error:    h.lastErr,
		SrvAddr:  h.creds.SrvAddr,
	}
	if !h.since.IsZero() {
		out.Since = h.since.UTC().Format(time.RFC3339)
	}
	return out
}

func registerHubLinkRoutes(mux *http.ServeMux, link *HubLink, authOK func(*http.Request) bool) {
	mux.HandleFunc("GET /v1/hub/link", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeJSON(w, http.StatusOK, link.Status())
	})
	mux.HandleFunc("POST /v1/hub/link", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			HubURL     string `json:"hub_url"`
			Code       string `json:"code"`
			DirectBase string `json:"direct_base"`
			HasSrv     bool   `json:"has_srv"`
			SrvAddr    string `json:"srv_addr"`
			SrvAdmin   string `json:"srv_admin_secret"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		status, err := link.EnrollWith(r.Context(), HubLinkEnroll{
			HubURL: in.HubURL, Code: in.Code, DirectBase: in.DirectBase,
			HasSrv: in.HasSrv, SrvAddr: in.SrvAddr, SrvAdmin: in.SrvAdmin,
		})
		if err != nil {
			if errors.Is(err, ErrInvalid) {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
	mux.HandleFunc("DELETE /v1/hub/link", func(w http.ResponseWriter, r *http.Request) {
		if !authOK(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err := link.Leave(); err != nil {
			writeErr(w, http.StatusInternalServerError, "hub link state could not be removed")
			return
		}
		writeJSON(w, http.StatusOK, link.Status())
	})
}

func (h *HubLink) runLocked() {
	base := h.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	h.cancel = cancel
	creds := h.creds
	routes := map[string]string{edgelink.TargetDesktopd: h.Self}
	go func() {
		_ = edgelink.RunLink(ctx, creds, routes, func(up bool, err error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
			if up != h.online {
				h.since = time.Now()
			}
			h.online = up
			if up {
				h.lastErr = ""
				log.Printf("[desktopd] hub link up node=%s", creds.NodeID)
				return
			}
			if err != nil {
				h.lastErr = scrubDesktopdLog(err.Error())
				log.Printf("[desktopd] hub link down node=%s err=%s", creds.NodeID, h.lastErr)
				return
			}
			log.Printf("[desktopd] hub link down node=%s", creds.NodeID)
		})
	}()
}

func (h *HubLink) stopLocked() {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	h.online = false
	h.since = time.Time{}
}

var desktopdLogSecret = regexp.MustCompile(`(?i)(password|DESKTOPD_TOKEN|MACLAW_ADMIN_SECRET|MACLAW_TOKEN_SECRET|MACLAW_DESKTOP_API_TOKEN|hub_token|node_secret|srv_admin_secret|desktopd_key|authorization)\s*[:=]\s*(?:"[^"]*"|'[^']*'|Bearer\s+\S+|\S+)`)
var desktopdLogBearer = regexp.MustCompile(`(?i)Bearer\s+\S+`)
var desktopdLogBlob = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)

func scrubDesktopdLog(text string) string {
	text = strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
	text = desktopdLogBearer.ReplaceAllString(text, "Bearer <redacted>")
	text = desktopdLogSecret.ReplaceAllString(text, "$1=<redacted>")
	text = desktopdLogBlob.ReplaceAllString(text, "<redacted>")
	return strings.TrimSpace(text)
}
