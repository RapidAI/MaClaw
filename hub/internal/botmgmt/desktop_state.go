package botmgmt

import (
	"context"
	"log"
	"strings"
	"time"
)

// desktopPin is the persisted "who owns this desktop right now" state for one
// user. Without it, a Hub restart forgets that a person is mid-login and the
// next StopDesktopIfIdle kills the browser they are typing into.
type desktopPin struct {
	HeldByBot  string `json:"held_by_bot,omitempty"`
	Awaiting   bool   `json:"awaiting,omitempty"`
	KeyboardBy string `json:"keyboard_by,omitempty"`
}

// desktopViewPin keeps one gated noVNC picture servable across a restart. The
// handoff token, origin and expiry ride along so ProxyDesktopHandoff can
// resolve the restored path; bearer is the Docker-side gate token the proxy
// must present.
type desktopViewPin struct {
	Raw    string `json:"raw"`
	Gated  string `json:"gated"`
	Token  string `json:"token"`
	Origin string `json:"origin"`
	Until  string `json:"until"`
	Bearer string `json:"bearer,omitempty"`
}

// desktopStateRecord is the tenant-scoped snapshot stored inside the botmgmt
// record. Usage counters (desktopOpening/desktopOpenInstance) are deliberately
// not persisted: the goroutines that decrement them die with the process, so a
// restored count could never reach zero and would pin the desktop forever.
// Their blind spot after a restart is bounded — a mistimed stop only restarts
// the browser, and desktopd flushes the website login into the profile volume
// on every stop.
type desktopStateRecord struct {
	Pins  map[string]*desktopPin     `json:"pins,omitempty"`
	Views map[string]*desktopViewPin `json:"views,omitempty"`
	// Viewed is when an admin last opened that desktop from the console,
	// as RFC3339. Without it a restart right after a check would forget the
	// hold and the next stop would blank the picture the admin is watching.
	Viewed map[string]string `json:"viewed,omitempty"`
	// Userviewed is the same hold for a user watching or taking over a
	// desktop from their own Bot page.
	Userviewed map[string]string `json:"userviewed,omitempty"`
}

// ensureDesktopHydrated restores the persisted desktop state into memory once
// per tenant per process. Callers must hold s.mu. load() does not take s.mu,
// so the read is safe here. A failed read leaves the tenant unhydrated so the
// next call retries — persistDesktopState refuses to write until then.
func (s *Service) ensureDesktopHydrated(tenantID string) {
	if s == nil || s.System == nil {
		return
	}
	if s.desktopHydrated == nil {
		s.desktopHydrated = map[string]bool{}
	}
	if s.desktopHydrated[tenantID] {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return
	}
	s.desktopHydrated[tenantID] = true
	if rec.Desktop == nil {
		return
	}
	now := s.now()
	prefix := desktopTenantPrefix(tenantID)
	for key, pin := range rec.Desktop.Pins {
		if pin == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		if pin.HeldByBot != "" {
			if s.desktopHeld == nil {
				s.desktopHeld = map[string]string{}
			}
			s.desktopHeld[key] = pin.HeldByBot
		}
		if pin.Awaiting {
			if s.desktopAwaiting == nil {
				s.desktopAwaiting = map[string]bool{}
			}
			s.desktopAwaiting[key] = true
		}
		if pin.KeyboardBy != "" {
			if s.desktopKeyboardTaken == nil {
				s.desktopKeyboardTaken = map[string]string{}
			}
			s.desktopKeyboardTaken[key] = pin.KeyboardBy
		}
	}
	for key, view := range rec.Desktop.Views {
		if view == nil || !strings.HasPrefix(key, prefix) {
			continue
		}
		until, err := time.Parse(time.RFC3339, view.Until)
		if err != nil || !now.Before(until) {
			continue
		}
		if s.desktopView == nil {
			s.desktopView = map[string]desktopWatch{}
		}
		s.desktopView[key] = desktopWatch{raw: view.Raw, gated: view.Gated}
		if s.handoff == nil {
			s.handoff = map[string]handoffView{}
		}
		s.handoff[view.Token] = handoffView{origin: view.Origin, until: until, bearer: view.Bearer}
	}
	for key, stamp := range rec.Desktop.Viewed {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		last, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			continue
		}
		if s.desktopAdminView == nil {
			s.desktopAdminView = map[string]time.Time{}
		}
		s.desktopAdminView[key] = last
	}
	for key, stamp := range rec.Desktop.Userviewed {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		last, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			continue
		}
		if s.desktopUserView == nil {
			s.desktopUserView = map[string]time.Time{}
		}
		s.desktopUserView[key] = last
	}
}

// persistDesktopState writes the in-memory desktop pins and views of this
// tenant into the botmgmt record. Callers must hold s.mu. A failed write is
// logged and left to the next mutation; the in-memory state stays authoritative
// until the process exits.
func (s *Service) persistDesktopState(tenantID string) {
	if s == nil || s.System == nil {
		return
	}
	// Never overwrite the persisted pins with a snapshot taken before that
	// state was read: a failed hydration would wipe every user's login pin
	// in this tenant.
	if s.desktopHydrated == nil || !s.desktopHydrated[tenantID] {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	prefix := desktopTenantPrefix(tenantID)
	now := s.now()
	st := &desktopStateRecord{Pins: map[string]*desktopPin{}, Views: map[string]*desktopViewPin{}}
	for key, holder := range s.desktopHeld {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		pin := &desktopPin{HeldByBot: holder, Awaiting: s.desktopAwaiting[key]}
		if s.desktopKeyboardTaken[key] != "" {
			pin.KeyboardBy = s.desktopKeyboardTaken[key]
		}
		st.Pins[key] = pin
	}
	// Awaiting/keyboard state can exist without a holder (releaseDesktopKeyboard
	// clears the keyboard pin while the holder stays). Catch those keys too.
	for key := range s.desktopAwaiting {
		if _, ok := st.Pins[key]; !ok && strings.HasPrefix(key, prefix) {
			pin := &desktopPin{Awaiting: s.desktopAwaiting[key]}
			if s.desktopKeyboardTaken[key] != "" {
				pin.KeyboardBy = s.desktopKeyboardTaken[key]
			}
			st.Pins[key] = pin
		}
	}
	for key, watch := range s.desktopView {
		if !strings.HasPrefix(key, prefix) || watch.gated == "" {
			continue
		}
		view, ok := s.handoffTokenForGated(watch.gated)
		if !ok || !now.Before(view.until) {
			continue
		}
		st.Views[key] = &desktopViewPin{
			Raw:    watch.raw,
			Gated:  watch.gated,
			Token:  tokenOfGatedPath(watch.gated),
			Origin: view.origin,
			Until:  view.until.UTC().Format(time.RFC3339),
			Bearer: view.bearer,
		}
	}
	// An admin hold that already expired needs no restoring after a restart.
	for key, last := range s.desktopAdminView {
		if !strings.HasPrefix(key, prefix) || now.Sub(last) >= AdminDesktopViewHold {
			continue
		}
		if st.Viewed == nil {
			st.Viewed = map[string]string{}
		}
		st.Viewed[key] = last.UTC().Format(time.RFC3339)
	}
	// A user view hold expires in minutes, so a restart must not resurrect a
	// stale one and keep the desktop up long after the page closed.
	for key, last := range s.desktopUserView {
		if !strings.HasPrefix(key, prefix) || now.Sub(last) >= UserDesktopViewHold {
			continue
		}
		if st.Userviewed == nil {
			st.Userviewed = map[string]string{}
		}
		st.Userviewed[key] = last.UTC().Format(time.RFC3339)
	}
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		log.Printf("[botmgmt] desktop state persist skipped: %v", err)
		return
	}
	rec.Desktop = st
	if err := s.save(ctx, tenantID, rec); err != nil {
		log.Printf("[botmgmt] desktop state persist failed: %v", err)
	}
}

// handoffTokenForGated finds the handoff entry a gated URL was built from by
// matching on the stored token rather than scanning every entry.
func (s *Service) handoffTokenForGated(gated string) (handoffView, bool) {
	token := tokenOfGatedPath(gated)
	if token == "" {
		return handoffView{}, false
	}
	view, ok := s.handoff[token]
	return view, ok
}

// tokenOfGatedPath extracts the 64-hex token from a Hub handoff path of the
// form /api/v1/desktop-handoff/<token>/...
func tokenOfGatedPath(gated string) string {
	const prefix = "/api/v1/desktop-handoff/"
	parsed := gated
	if idx := strings.IndexByte(parsed, '?'); idx >= 0 {
		parsed = parsed[:idx]
	}
	if !strings.HasPrefix(parsed, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(parsed, prefix)
	token, _, ok := strings.Cut(rest, "/")
	if !ok || !validHandoffToken(token) {
		return ""
	}
	return token
}

func desktopTenantPrefix(tenantID string) string {
	return tenantID + "\x00"
}
