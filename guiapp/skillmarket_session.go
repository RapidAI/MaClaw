package guiapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

const signedSkillMarketSessionPrefix = "sm2."

// skillMarketMachineLoginTimeout is one HubCenter machine-login attempt.
// HubCenter spends that attempt verifying the viewer token with the Hub.
// The shared Hub HTTP client waits 30s; a shorter deadline cancels a login
// the client would still finish. Each configured center gets its own attempt
// so a center that does not answer cannot spend the next center's budget.
const skillMarketMachineLoginTimeout = 30 * time.Second

// errHubCenterSignInRequired is returned when this desktop has neither a
// usable SkillMarket session nor Hub enrollment it can exchange for one.
var errHubCenterSignInRequired = errors.New("please sign in to HubCenter before submitting a report")

// newSkillMarketDeriveContext bounds session derivation to two machine-login
// attempts: the preferred center, then one configured fallback.
func newSkillMarketDeriveContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*skillMarketMachineLoginTimeout)
}

// skillMarketPrincipal is the Hub enrollment identity. The SkillMarket
// session is derived from it; a cached token is not a second account.
type skillMarketPrincipal struct {
	HubID       string
	UserID      string
	Contact     string
	MachineID   string
	ViewerToken string
}

type skillMarketSession struct {
	Token string
	Base  string
}

type signedSkillMarketClaims struct {
	UserID    string
	Email     string
	ExpiresAt time.Time
}

func skillMarketPrincipalFromConfig(cfg corelib.AppConfig) skillMarketPrincipal {
	contact := strings.TrimSpace(cfg.RemoteEmail)
	if contact == "" {
		if phone := strings.TrimSpace(cfg.RemoteMobile); phone != "" {
			contact = "phone:" + phone
		}
	}
	userID := strings.TrimSpace(cfg.RemoteUserID)
	if userID == "" {
		userID = contact
	}
	return skillMarketPrincipal{
		HubID:       strings.TrimSpace(cfg.RemoteHubID),
		UserID:      userID,
		Contact:     contact,
		MachineID:   strings.TrimSpace(cfg.RemoteMachineID),
		ViewerToken: strings.TrimSpace(cfg.RemoteViewerToken),
	}
}

// withFallback fills only the fields the caller did not already supply.
// An explicit enroll result must win over a config row that has not been
// patched yet.
func (p skillMarketPrincipal) withFallback(cfg corelib.AppConfig) skillMarketPrincipal {
	fallback := skillMarketPrincipalFromConfig(cfg)
	if strings.TrimSpace(p.UserID) == "" && strings.TrimSpace(p.MachineID) == "" && strings.TrimSpace(p.ViewerToken) == "" && strings.TrimSpace(p.Contact) == "" {
		if strings.TrimSpace(p.HubID) != "" {
			fallback.HubID = strings.TrimSpace(p.HubID)
		}
		return fallback
	}
	if strings.TrimSpace(p.HubID) == "" {
		p.HubID = fallback.HubID
	}
	if strings.TrimSpace(p.UserID) == "" {
		p.UserID = fallback.UserID
	}
	if strings.TrimSpace(p.Contact) == "" {
		p.Contact = fallback.Contact
	}
	if strings.TrimSpace(p.MachineID) == "" {
		p.MachineID = fallback.MachineID
	}
	if strings.TrimSpace(p.ViewerToken) == "" {
		p.ViewerToken = fallback.ViewerToken
	}
	return p
}

func (p skillMarketPrincipal) canMint() bool {
	return strings.TrimSpace(p.HubID) != "" &&
		strings.TrimSpace(p.UserID) != "" &&
		strings.TrimSpace(p.MachineID) != "" &&
		strings.TrimSpace(p.ViewerToken) != ""
}

func decodeSignedSkillMarketClaims(token string) (signedSkillMarketClaims, bool) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, signedSkillMarketSessionPrefix) {
		return signedSkillMarketClaims{}, false
	}
	rest := strings.TrimPrefix(token, signedSkillMarketSessionPrefix)
	payloadPart, sigPart, ok := strings.Cut(rest, ".")
	if !ok || payloadPart == "" || sigPart == "" || strings.Contains(sigPart, ".") {
		return signedSkillMarketClaims{}, false
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return signedSkillMarketClaims{}, false
	}
	var payload struct {
		Version int    `json:"v"`
		UserID  string `json:"uid"`
		Email   string `json:"email"`
		Exp     int64  `json:"exp"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return signedSkillMarketClaims{}, false
	}
	if payload.Version != 1 || strings.TrimSpace(payload.UserID) == "" || payload.Exp <= 0 {
		return signedSkillMarketClaims{}, false
	}
	return signedSkillMarketClaims{
		UserID:    strings.TrimSpace(payload.UserID),
		Email:     strings.TrimSpace(payload.Email),
		ExpiresAt: time.Unix(payload.Exp, 0),
	}, true
}

// skillMarketMintedForHubUser records signed sessions this process obtained
// for a Hub principal. Machine-login may adopt an older market row, and the
// token then names that row instead of the Hub user id. A signed token that
// was only loaded from disk is absent here, so a leftover session for
// someone else is still replaced.
var skillMarketMintedForHubUser sync.Map // token -> hub user id

func rememberSkillMarketMint(hubUserID, token string) {
	hubUserID = strings.TrimSpace(hubUserID)
	token = strings.TrimSpace(token)
	if hubUserID == "" || !strings.HasPrefix(token, signedSkillMarketSessionPrefix) {
		return
	}
	skillMarketMintedForHubUser.Store(token, hubUserID)
}

func skillMarketMintMatchesHubUser(hubUserID, token string) bool {
	hubUserID = strings.TrimSpace(hubUserID)
	token = strings.TrimSpace(token)
	if hubUserID == "" || token == "" {
		return false
	}
	stored, ok := skillMarketMintedForHubUser.Load(token)
	if !ok {
		return false
	}
	owner, _ := stored.(string)
	return owner == hubUserID
}

// skillMarketCachedTokenUsable reports whether the cached token is the
// current Hub principal's session. A signed token is usable when it has not
// expired and either names that Hub user or was minted for that Hub user in
// this process. An opaque token cannot be checked, so it is kept only when
// Hub enrollment cannot mint a replacement. force discards every cache: the
// HubCenter that issued it has already rejected it.
func skillMarketCachedTokenUsable(token string, principal skillMarketPrincipal, force bool) bool {
	if force {
		return false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.HasPrefix(token, signedSkillMarketSessionPrefix) {
		claims, ok := decodeSignedSkillMarketClaims(token)
		if !ok || !time.Now().Before(claims.ExpiresAt) {
			return false
		}
		if principal.canMint() && claims.UserID != strings.TrimSpace(principal.UserID) && !skillMarketMintMatchesHubUser(principal.UserID, token) {
			return false
		}
		return true
	}
	return !principal.canMint()
}

// skillMarketTokenKnownForeign is a signed token that is expired, malformed,
// or bound to a different Hub user and was not minted for the current one.
// It must not be reused when minting fails.
func skillMarketTokenKnownForeign(token string, principal skillMarketPrincipal) bool {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, signedSkillMarketSessionPrefix) {
		return false
	}
	claims, ok := decodeSignedSkillMarketClaims(token)
	if !ok || !time.Now().Before(claims.ExpiresAt) {
		return true
	}
	if !principal.canMint() || claims.UserID == strings.TrimSpace(principal.UserID) {
		return false
	}
	return !skillMarketMintMatchesHubUser(principal.UserID, token)
}

func skillMarketReportBase(cfg corelib.AppConfig) string {
	if base := remote.NormalizeHubCenterURL(cfg.RemoteHubCenterURL); base != "" {
		return base
	}
	for _, candidate := range cfg.RemoteHubCenterURLs {
		if base := remote.NormalizeHubCenterURL(candidate); base != "" {
			return base
		}
	}
	return ""
}

// skillMarketSessionBase is where the current token is presented. The center
// that issued it wins when it is still one of the configured centers. A
// discovered center is kept when the config has no explicit list.
func skillMarketSessionBase(cfg corelib.AppConfig) string {
	issued := remote.NormalizeHubCenterURL(cfg.SkillMarketSessionBaseURL)
	if issued == "" {
		return skillMarketReportBase(cfg)
	}
	configured := remote.NormalizeHubCenterURLs(append([]string{cfg.RemoteHubCenterURL}, cfg.RemoteHubCenterURLs...))
	if len(configured) == 0 {
		return issued
	}
	for _, candidate := range configured {
		if candidate == issued {
			return issued
		}
	}
	return skillMarketReportBase(cfg)
}

// storeSkillMarketSession writes the token and the center that issued it
// together. A token with no center clears the previous issuer so a later
// call does not present the new token to a center that did not mint it.
func storeSkillMarketSession(cfg *corelib.AppConfig, token, base string) {
	if cfg == nil {
		return
	}
	cfg.SkillMarketSessionToken = strings.TrimSpace(token)
	if cfg.SkillMarketSessionToken == "" {
		cfg.SkillMarketSessionBaseURL = ""
		return
	}
	cfg.SkillMarketSessionBaseURL = remote.NormalizeHubCenterURL(base)
}

// clearSkillMarketSessionForHubUser drops the derived SkillMarket session when
// the Hub user id changes. The same user keeping the same id keeps the cache.
// A signed token left on disk for another uid is still replaced. A token this
// process minted stays usable when machine-login adopted a market row.
func clearSkillMarketSessionForHubUser(cfg *corelib.AppConfig, nextUserID string) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.RemoteUserID) != strings.TrimSpace(nextUserID) {
		storeSkillMarketSession(cfg, "", "")
	}
}

// bugReportCanReachHubCenter is the pre-archive check. An empty cached token
// is still reachable when Hub enrollment can mint a session.
func bugReportCanReachHubCenter(cfg corelib.AppConfig) bool {
	if skillMarketSessionBase(cfg) == "" {
		return false
	}
	if strings.TrimSpace(cfg.SkillMarketSessionToken) != "" {
		return true
	}
	return skillMarketPrincipalFromConfig(cfg).canMint()
}

// ensureSkillMarketSession returns a SkillMarket session for the current Hub
// principal. The cached token is used only when it still names that principal.
// Otherwise the desktop exchanges Hub enrollment for a new session and posts
// later calls to the HubCenter that issued it.
func (a *App) ensureSkillMarketSession(ctx context.Context, force bool, principal skillMarketPrincipal) (skillMarketSession, error) {
	if a == nil {
		return skillMarketSession{}, errHubCenterSignInRequired
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return skillMarketSession{}, err
	}
	principal = principal.withFallback(cfg)
	cached := strings.TrimSpace(cfg.SkillMarketSessionToken)
	if skillMarketCachedTokenUsable(cached, principal, force) {
		base := skillMarketSessionBase(cfg)
		if base == "" {
			return skillMarketSession{}, errHubCenterSignInRequired
		}
		return skillMarketSession{Token: cached, Base: base}, nil
	}
	if !principal.canMint() {
		return skillMarketSession{}, errHubCenterSignInRequired
	}
	token, base, mintErr := a.mintSkillMarketSession(ctx, cfg, principal)
	if mintErr != nil {
		if !force && cached != "" && !skillMarketTokenKnownForeign(cached, principal) {
			if fallback := skillMarketSessionBase(cfg); fallback != "" {
				return skillMarketSession{Token: cached, Base: fallback}, nil
			}
		}
		return skillMarketSession{}, mintErr
	}
	return skillMarketSession{Token: token, Base: base}, nil
}

func (a *App) skillMarketLoginCandidates(ctx context.Context, cfg corelib.AppConfig) []string {
	configured := remote.NormalizeHubCenterURLs(append([]string{cfg.RemoteHubCenterURL}, cfg.RemoteHubCenterURLs...))
	if len(configured) > 0 {
		return configured
	}
	if a == nil {
		return nil
	}
	base, discovered, err := NewSkillMarketClient(a).selectBaseURL(ctx)
	if err != nil {
		log.Printf("[skillmarket] hubcenter discovery failed: %v", err)
		return nil
	}
	return remote.NormalizeHubCenterURLs(append([]string{base}, discovered...))
}

func (a *App) skillMarketLoginAttemptTimeout() time.Duration {
	if a != nil && a.testSkillMarketLoginAttemptTimeout > 0 {
		return a.testSkillMarketLoginAttemptTimeout
	}
	return skillMarketMachineLoginTimeout
}

func (a *App) mintSkillMarketSession(ctx context.Context, cfg corelib.AppConfig, principal skillMarketPrincipal) (string, string, error) {
	candidates := a.skillMarketLoginCandidates(ctx, cfg)
	if len(candidates) == 0 {
		return "", "", errHubCenterSignInRequired
	}
	authClient := remote.NewSkillMarketAuthClient()
	var lastErr error
	for _, base := range candidates {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return "", "", lastErr
			}
			return "", "", err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, a.skillMarketLoginAttemptTimeout())
		result, err := authClient.MachineLogin(attemptCtx, base, principal.HubID, principal.UserID, principal.Contact, principal.MachineID, principal.ViewerToken)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		token := ""
		if result != nil {
			token = strings.TrimSpace(result.SessionToken)
		}
		if token == "" {
			lastErr = fmt.Errorf("machine-login returned empty session token from %s", base)
			continue
		}
		savedUserID := strings.TrimSpace(principal.UserID)
		if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
			if current := strings.TrimSpace(cfg.RemoteUserID); current != "" && current != savedUserID {
				return
			}
			storeSkillMarketSession(cfg, token, base)
		}); err != nil {
			return "", "", err
		}
		saved, err := a.LoadConfig()
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(saved.SkillMarketSessionToken) != token {
			return "", "", fmt.Errorf("hub user changed before the SkillMarket session could be saved")
		}
		rememberSkillMarketMint(savedUserID, token)
		log.Printf("[skillmarket] session derived from hub enrollment user_id=%s center=%s", savedUserID, base)
		return token, base, nil
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("machine-login failed on all hubcenter candidates")
}
