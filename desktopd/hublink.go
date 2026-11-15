package desktopd

import (
	"bytes"
	"context"
	"encoding/json"
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

// errHubResourcesGone means this Hub does not have the desktop size route yet.
var errHubResourcesGone = errors.New("hub has no desktop size route")

// errHubDirectBaseGone means this Hub does not have the intranet address route yet.
var errHubDirectBaseGone = errors.New("hub has no intranet address route")

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
	Enrolled   bool   `json:"enrolled"`
	HubURL     string `json:"hub_url,omitempty"`
	NodeID     string `json:"node_id,omitempty"`
	Online     bool   `json:"online"`
	Error      string `json:"error,omitempty"`
	Since      string `json:"since,omitempty"`
	SrvAddr    string `json:"srv_addr,omitempty"`
	DirectBase string `json:"direct_base,omitempty"`
}

// normalizeDirectBase checks the intranet address a GUI probes. Empty means
// pictures stay on the Hub address. A virtual node host is refused.
func normalizeDirectBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", errors.New("内网访问地址要写成 http://地址:端口")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".node.invalid") || strings.Contains(parsed.Path, "..") {
		return "", errors.New("内网访问地址要写成 http://地址:端口")
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
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
	HubKey(label string) (string, error)
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
	direct, err := normalizeDirectBase(in.DirectBase)
	if err != nil {
		return HubLinkStatus{}, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	in.DirectBase = direct
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
	ram, cpus := HostCapacity()
	creds, err := edgelink.Enroll(ctx, client, hub, edgelink.EnrollRequest{
		Code:         code,
		DesktopdKey:  desktopdKey,
		SrvAdmin:     in.SrvAdmin,
		Version:      "desktopd",
		HasDesktopd:  true,
		HasSrv:       in.HasSrv,
		DirectBase:   in.DirectBase,
		HostRAMBytes: ram,
		HostCPUs:     cpus,
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
	creds.DirectBase = in.DirectBase
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
	go h.SyncDesktopSize(context.Background())
	return h.Status(), nil
}

// ReportCapacity tells Hub the memory and CPU of this machine so an installed
// desktop can be sized from it. A missing measurement or a hub that does not
// have the route yet leaves the current size in place.
func (h *HubLink) ReportCapacity(ctx context.Context) {
	if h == nil {
		return
	}
	h.mu.Lock()
	creds := h.creds
	client := h.Client
	h.mu.Unlock()
	if creds.NodeID == "" || creds.NodeSecret == "" || creds.HubURL == "" {
		return
	}
	ram, cpus := HostCapacity()
	if ram == 0 {
		return
	}
	body, err := json.Marshal(map[string]any{"ram_bytes": ram, "cpus": cpus})
	if err != nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(creds.HubURL, "/")+"/api/v1/edge/capacity", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.NodeSecret)
	req.Header.Set("X-Node-Id", creds.NodeID)
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[desktopd] host capacity was not reported node=%s err=%v", creds.NodeID, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[desktopd] hub has no host-capacity route yet node=%s", creds.NodeID)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[desktopd] host capacity was not stored node=%s status=%d", creds.NodeID, resp.StatusCode)
		return
	}
	log.Printf("[desktopd] host capacity reported node=%s ram_bytes=%d cpus=%d", creds.NodeID, ram, cpus)
}

// SyncDesktopSize tells Hub which size this machine should use. A size saved
// in the admin panel is sent as a fixed choice, so a later measurement does
// not replace it. With no saved choice, Hub measures the machine.
func (h *HubLink) SyncDesktopSize(ctx context.Context) {
	if h == nil {
		return
	}
	if choice := panelResourceChoice(); choice != nil {
		err := h.PushResources(ctx, choice.Memory, choice.CPUs, choice.ShmSize, false)
		if err == nil {
			return
		}
		if errors.Is(err, errHubResourcesGone) {
			log.Printf("[desktopd] hub has no desktop size route yet")
			return
		}
		log.Printf("[desktopd] saved desktop size was not stored on hub err=%v", err)
		return
	}
	h.ReportCapacity(ctx)
}

// PushResources stores a size typed in the admin panel on Hub. restore puts
// the measured size back in front of later measurements.
func (h *HubLink) PushResources(ctx context.Context, memory, cpus, shm string, restore bool) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	creds := h.creds
	client := h.Client
	h.mu.Unlock()
	if creds.NodeID == "" || creds.NodeSecret == "" || creds.HubURL == "" {
		return nil
	}
	payload := map[string]any{"memory": memory, "cpus": cpus, "shm_size": shm}
	if restore {
		ram, logical := HostCapacity()
		payload = map[string]any{"restore_auto": true, "ram_bytes": ram, "host_cpus": logical}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(creds.HubURL, "/")+"/api/v1/edge/resources", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.NodeSecret)
	req.Header.Set("X-Node-Id", creds.NodeID)
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusNotFound {
		return errHubResourcesGone
	}
	if resp.StatusCode == http.StatusNoContent || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	var reply struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &reply) == nil {
		if msg := strings.TrimSpace(reply.Message); msg != "" && len(msg) <= 200 {
			return errors.New(msg)
		}
	}
	return fmt.Errorf("hub did not store the desktop size (%d)", resp.StatusCode)
}

// SaveDirectBase stores the intranet address on this machine and tells Hub.
// A Hub that does not have the route yet leaves the local copy in place.
// hubSaved is false when Hub did not record it. The node secret is not logged.
func (h *HubLink) SaveDirectBase(ctx context.Context, raw string) (bool, error) {
	base, err := normalizeDirectBase(raw)
	if err != nil {
		return false, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	}
	if h == nil {
		return false, fmt.Errorf("%w: hub link is disabled", ErrInvalid)
	}
	h.mu.Lock()
	if h.creds.NodeID == "" || h.creds.NodeSecret == "" || h.creds.HubURL == "" {
		h.mu.Unlock()
		return false, fmt.Errorf("%w: 还没有注册", ErrInvalid)
	}
	creds := h.creds
	creds.DirectBase = base
	path := h.StatePath
	client := h.Client
	h.mu.Unlock()
	if err := edgelink.SaveCredentials(path, creds); err != nil {
		return false, err
	}
	h.mu.Lock()
	h.creds = creds
	h.mu.Unlock()
	if err := h.pushDirectBase(ctx, creds, client); err != nil {
		if errors.Is(err, errHubDirectBaseGone) {
			log.Printf("[desktopd] hub has no intranet address route yet node=%s", creds.NodeID)
		} else {
			log.Printf("[desktopd] hub did not store the intranet address node=%s", creds.NodeID)
		}
		return false, nil
	}
	return true, nil
}

func (h *HubLink) syncDirectBase(ctx context.Context) {
	if h == nil {
		return
	}
	h.mu.Lock()
	creds := h.creds
	client := h.Client
	h.mu.Unlock()
	if strings.TrimSpace(creds.DirectBase) == "" || creds.NodeID == "" || creds.NodeSecret == "" {
		return
	}
	if err := h.pushDirectBase(ctx, creds, client); err != nil && !errors.Is(err, errHubDirectBaseGone) {
		log.Printf("[desktopd] hub did not store the intranet address node=%s", creds.NodeID)
	}
}

func (h *HubLink) pushDirectBase(ctx context.Context, creds edgelink.Credentials, client *http.Client) error {
	body, err := json.Marshal(map[string]string{"direct_base": creds.DirectBase})
	if err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, strings.TrimRight(creds.HubURL, "/")+"/api/v1/edge/direct-base", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.NodeSecret)
	req.Header.Set("X-Node-Id", creds.NodeID)
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusNotFound {
		return errHubDirectBaseGone
	}
	if resp.StatusCode == http.StatusNoContent || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf("hub did not store the intranet address (%d)", resp.StatusCode)
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

// signingKey is the key Hub uses to sign direct-view and local-data tickets.
// After enrollment that is the extra hub-<nodeID> key, and the primary
// DESKTOPD_TOKEN no longer verifies those tickets. A desktop that never
// stored an extra key still uses the primary token.
func (h *HubLink) signingKey() string {
	if h == nil {
		return ""
	}
	if h.Keys != nil {
		if label, err := hubKeyLabel(h.NodeID()); err == nil {
			if key, err := h.Keys.HubKey(label); err == nil && strings.TrimSpace(key) != "" {
				return strings.TrimSpace(key)
			}
		}
	}
	return strings.TrimSpace(h.Key)
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
		Enrolled:   h.creds.NodeID != "",
		HubURL:     h.creds.HubURL,
		NodeID:     h.creds.NodeID,
		Online:     h.online,
		Error:      h.lastErr,
		SrvAddr:    h.creds.SrvAddr,
		DirectBase: h.creds.DirectBase,
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

// hubLinkRoutes is the only pair of addresses this tunnel will dial.
// desktopd is this process. srv is the same-machine MaClawSrv when one was
// enrolled. Anything else is absent, so the tunnel closes that stream.
func hubLinkRoutes(self, srvAddr string) map[string]string {
	routes := map[string]string{edgelink.TargetDesktopd: strings.TrimSpace(self)}
	if addr := hubLinkDialAddr(srvAddr); addr != "" {
		routes[edgelink.TargetSrv] = addr
	}
	return routes
}

// hubLinkDialAddr keeps a host:port and also accepts an http URL typed into
// the panel, since Dial needs the host and not the path.
func hubLinkDialAddr(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return parsed.Host
	}
	return raw
}

func (h *HubLink) runLocked() {
	base := h.base
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	h.cancel = cancel
	creds := h.creds
	routes := hubLinkRoutes(h.Self, creds.SrvAddr)
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
				go h.syncDirectBase(context.Background())
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
