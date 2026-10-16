package mailbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Browser logins use the public desktop mail clients that already accept a
// loopback redirect on 127.0.0.1 with any port and the IMAP/SMTP scopes.
// The provider's consent page names that registration (Thunderbird), not
// MaClaw. Replace the client id to put MaClaw's name on the consent page.
const (
	gmailOAuthClientID     = "406964657835-aq8lmia8j95dhl1a2bvharmfk3t1hgqj.apps.googleusercontent.com"
	gmailOAuthClientSecret = "kSmqreRr0qwBWJgbf5Y-PjSU"
	hotmailOAuthClientID   = "9e5f94bc-e8a4-4e73-b8be-63364c29d753"
)

type oauthProvider struct {
	ID            string
	Label         string
	ClientID      string
	ClientSecret  string
	AuthEndpoint  string
	TokenEndpoint string
	ProfileURL    string
	ProfileAlts   []string
	Scopes        []string
	UsePKCE       bool
	ExtraAuth     map[string]string
	Inbound       Endpoint
	Outbound      Endpoint
}

func oauthProviders() map[string]oauthProvider {
	return map[string]oauthProvider{
		ProviderGmail: {
			ID:            ProviderGmail,
			Label:         "Gmail",
			ClientID:      gmailOAuthClientID,
			ClientSecret:  gmailOAuthClientSecret,
			AuthEndpoint:  "https://accounts.google.com/o/oauth2/auth",
			TokenEndpoint: "https://www.googleapis.com/oauth2/v3/token",
			ProfileURL:    "https://gmail.googleapis.com/gmail/v1/users/me/profile",
			Scopes:        []string{"https://mail.google.com/"},
			UsePKCE:       true,
			ExtraAuth: map[string]string{
				"access_type": "offline",
				"prompt":      "consent",
			},
			Inbound:  Endpoint{Host: "imap.gmail.com", Port: 993, Security: SecurityTLS},
			Outbound: Endpoint{Host: "smtp.gmail.com", Port: 465, Security: SecurityTLS},
		},
		ProviderHotmail: {
			ID:            ProviderHotmail,
			Label:         "Hotmail",
			ClientID:      hotmailOAuthClientID,
			AuthEndpoint:  "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenEndpoint: "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			ProfileURL:    "https://outlook.office.com/api/v2.0/me",
			ProfileAlts:   []string{"https://outlook.office365.com/api/v2.0/me"},
			Scopes: []string{
				"https://outlook.office.com/IMAP.AccessAsUser.All",
				"https://outlook.office.com/SMTP.Send",
				"offline_access",
				"openid",
				"profile",
				"email",
			},
			ExtraAuth: map[string]string{
				"response_mode": "query",
				"prompt":        "select_account",
			},
			Inbound:  Endpoint{Host: "outlook.office365.com", Port: 993, Security: SecurityTLS},
			Outbound: Endpoint{Host: "smtp.office365.com", Port: 587, Security: SecuritySTARTTLS},
		},
	}
}

// oauthProviderTable is replaced in tests that point token calls at a local server.
var oauthProviderTable = oauthProviders()

func KnownOAuthProvider(id string) bool {
	_, ok := oauthProviderTable[strings.TrimSpace(id)]
	return ok
}

// OAuthGrant is a finished browser login. Secret is the keyring blob, never a config field.
type OAuthGrant struct {
	Provider string
	Email    string
	Secret   string
}

type oauthToken struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    int
}

type oauthSecret struct {
	Kind         string    `json:"kind"`
	Provider     string    `json:"provider"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

var oauthHTTP = &http.Client{
	Timeout: 20 * time.Second,
	// The token request carries the authorization code and client secret.
	// A redirect must not receive that body.
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// LoginOAuth opens the provider login page and waits for the loopback redirect.
// open receives the authorization URL. The returned secret belongs in the keyring.
func LoginOAuth(ctx context.Context, providerID string, open func(string) error) (OAuthGrant, error) {
	return loginOAuth(ctx, providerID, "", open)
}

// LoginOAuthWithHint is LoginOAuth with the account the user asked to refresh.
// Google and Microsoft pre-select that address. An empty hint is a new login.
func LoginOAuthWithHint(ctx context.Context, providerID, hint string, open func(string) error) (OAuthGrant, error) {
	hint = strings.TrimSpace(hint)
	if hint != "" {
		normalized, err := normalizeAddress(hint)
		if err != nil {
			return OAuthGrant{}, err
		}
		hint = normalized
	}
	return loginOAuth(ctx, providerID, hint, open)
}

func loginOAuth(ctx context.Context, providerID, hint string, open func(string) error) (OAuthGrant, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	provider, ok := oauthProviderTable[strings.TrimSpace(providerID)]
	if !ok || provider.ClientID == "" {
		return OAuthGrant{}, classErr("invalid", "不支持这个邮箱登录")
	}
	if open == nil {
		return OAuthGrant{}, classErr("auth", "无法打开浏览器")
	}
	verifier, challenge, err := oauthPKCE()
	if err != nil {
		return OAuthGrant{}, classErr("auth", "无法开始登录")
	}
	state, err := oauthState()
	if err != nil {
		return OAuthGrant{}, classErr("auth", "无法开始登录")
	}
	loop, err := startOAuthLoopback(state)
	if err != nil {
		return OAuthGrant{}, classErr("auth", "无法开始登录")
	}
	defer loop.close()
	authURL := provider.authURL(loop.redirect, state, challenge, hint)
	if err := open(authURL); err != nil {
		return OAuthGrant{}, classErr("auth", "无法打开浏览器")
	}
	code, gotState, err := loop.wait(ctx)
	if err != nil {
		return OAuthGrant{}, err
	}
	if subtle.ConstantTimeCompare([]byte(gotState), []byte(state)) != 1 {
		return OAuthGrant{}, classErr("auth", "登录回调无效")
	}
	tok, err := exchangeOAuthCode(ctx, provider, code, verifier, loop.redirect)
	if err != nil {
		return OAuthGrant{}, err
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return OAuthGrant{}, classErr("auth", "登录没有拿到长期授权，请重新登录")
	}
	email, err := oauthIdentity(ctx, provider, tok)
	if err != nil {
		return OAuthGrant{}, err
	}
	email, err = normalizeAddress(email)
	if err != nil {
		return OAuthGrant{}, classErr("auth", "登录成功，但没有拿到邮箱地址")
	}
	secret, err := encodeOAuthSecret(oauthSecret{
		Kind:         AuthOAuth,
		Provider:     provider.ID,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Expiry:       oauthExpiry(time.Now(), tok.ExpiresIn),
	})
	if err != nil {
		return OAuthGrant{}, classErr("auth", "登录失败，授权没有完成")
	}
	return OAuthGrant{Provider: provider.ID, Email: email, Secret: secret}, nil
}

func (p oauthProvider) authURL(redirectURI, state, challenge, hint string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", p.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", strings.Join(p.Scopes, " "))
	params.Set("state", state)
	if p.UsePKCE {
		params.Set("code_challenge", challenge)
		params.Set("code_challenge_method", "S256")
	}
	for key, value := range p.ExtraAuth {
		if strings.TrimSpace(key) != "" && value != "" {
			params.Set(key, value)
		}
	}
	if hint != "" {
		params.Set("login_hint", hint)
	}
	return p.AuthEndpoint + "?" + params.Encode()
}

func exchangeOAuthCode(ctx context.Context, p oauthProvider, code, verifier, redirectURI string) (oauthToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", p.ClientID)
	form.Set("redirect_uri", redirectURI)
	if p.ClientSecret != "" {
		form.Set("client_secret", p.ClientSecret)
	}
	if p.UsePKCE {
		form.Set("code_verifier", verifier)
	}
	return postOAuthToken(ctx, p.TokenEndpoint, form)
}

func refreshOAuthToken(ctx context.Context, p oauthProvider, refreshToken string) (oauthToken, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", p.ClientID)
	if p.ClientSecret != "" {
		form.Set("client_secret", p.ClientSecret)
	}
	if scope := refreshScopeList(p.Scopes); scope != "" {
		form.Set("scope", scope)
	}
	return postOAuthToken(ctx, p.TokenEndpoint, form)
}

// refreshScopeList keeps the mailbox scopes. openid, profile and email are
// only needed while signing in; sending them again can make Microsoft replace
// the IMAP and SMTP token.
func refreshScopeList(scopes []string) string {
	kept := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		switch strings.TrimSpace(scope) {
		case "", "openid", "profile", "email":
			continue
		default:
			kept = append(kept, scope)
		}
	}
	return strings.Join(kept, " ")
}

func postOAuthToken(ctx context.Context, endpoint string, form url.Values) (oauthToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return oauthToken{}, classErr("auth", "登录失败，授权没有完成")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oauthHTTP.Do(req)
	if err != nil {
		return oauthToken{}, classErr("connect", MsgConnect)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return oauthToken{}, classErr("connect", MsgConnect)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || resp.StatusCode != http.StatusOK || payload.AccessToken == "" {
		if payload.Error == "access_denied" || payload.Error == "consent_required" {
			return oauthToken{}, classErr("auth", "登录已取消")
		}
		if payload.Error == "invalid_grant" {
			return oauthToken{}, classErr("auth", MsgOAuthReauth)
		}
		return oauthToken{}, classErr("auth", "登录失败，授权没有完成")
	}
	return oauthToken{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		IDToken:      payload.IDToken,
		ExpiresIn:    payload.ExpiresIn,
	}, nil
}

func oauthIdentity(ctx context.Context, p oauthProvider, tok oauthToken) (string, error) {
	if email := emailFromJWT(tok.IDToken); email != "" {
		return email, nil
	}
	if email := emailFromJWT(tok.AccessToken); email != "" {
		return email, nil
	}
	urls := p.profileURLs()
	if len(urls) == 0 {
		return "", classErr("auth", "登录成功，但没有拿到邮箱地址")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	sawNetwork := false
	sawHTTP := false
	for _, rawURL := range urls {
		email, err := oauthProfileEmail(ctx, rawURL, tok.AccessToken)
		if email != "" {
			return email, nil
		}
		if errors.Is(err, errProfileRejected) {
			sawHTTP = true
			break
		}
		if err != nil {
			sawNetwork = true
			continue
		}
		sawHTTP = true
	}
	if sawNetwork && !sawHTTP {
		return "", classErr("connect", MsgConnect)
	}
	return "", classErr("auth", "登录成功，但没有拿到邮箱地址")
}

func (p oauthProvider) profileURLs() []string {
	urls := make([]string, 0, 1+len(p.ProfileAlts))
	if raw := strings.TrimSpace(p.ProfileURL); raw != "" {
		urls = append(urls, raw)
	}
	for _, raw := range p.ProfileAlts {
		if raw = strings.TrimSpace(raw); raw != "" {
			urls = append(urls, raw)
		}
	}
	return urls
}

var errProfileRejected = errors.New("profile rejected")

func oauthProfileEmail(ctx context.Context, rawURL, accessToken string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := oauthHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", errProfileRejected
	default:
		return "", nil
	}
	return emailFromProfile(body), nil
}

func emailFromProfile(body []byte) string {
	var profile map[string]any
	if err := json.Unmarshal(body, &profile); err != nil {
		return ""
	}
	for _, key := range []string{"emailAddress", "email", "mail", "userPrincipalName", "preferred_username", "upn"} {
		value := profileField(profile, key)
		if email := mailboxClaim(value); email != "" {
			return email
		}
		obj, _ := value.(map[string]any)
		if email := mailboxClaim(profileField(obj, "address")); email != "" {
			return email
		}
	}
	return ""
}

func profileField(profile map[string]any, name string) any {
	for key, value := range profile {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return nil
}

func emailFromJWT(raw string) string {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := decodeJWTPayload(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	for _, key := range []string{"preferred_username", "upn", "email", "mail", "verified_primary_email", "unique_name"} {
		if email := mailboxClaim(claims[key]); email != "" {
			return email
		}
	}
	return ""
}

func decodeJWTPayload(seg string) ([]byte, error) {
	var last error
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		payload, err := enc.DecodeString(seg)
		if err == nil {
			return payload, nil
		}
		last = err
	}
	return nil, last
}

func mailboxClaim(raw any) string {
	value, _ := raw.(string)
	value = strings.TrimSpace(value)
	// Personal Microsoft accounts put the mailbox after a hash, as in
	// live.com#name@hotmail.com. The part before the hash is not an address.
	if hash := strings.LastIndex(value, "#"); hash >= 0 {
		value = strings.TrimSpace(value[hash+1:])
	}
	if start := strings.LastIndex(value, "<"); start >= 0 {
		if end := strings.LastIndex(value, ">"); end > start {
			value = strings.TrimSpace(value[start+1 : end])
		}
	}
	if strings.Count(value, "@") != 1 || strings.ContainsAny(value, " \t\r\n#<>\"'\\") {
		return ""
	}
	local, domain, _ := strings.Cut(value, "@")
	if local == "" || !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return ""
	}
	return value
}

func oauthExpiry(now time.Time, expiresIn int) time.Time {
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}

func encodeOAuthSecret(sec oauthSecret) (string, error) {
	raw, err := json.Marshal(sec)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func parseOAuthSecret(raw string) (oauthSecret, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return oauthSecret{}, false
	}
	var sec oauthSecret
	if err := json.Unmarshal([]byte(raw), &sec); err != nil {
		return oauthSecret{}, false
	}
	if sec.Kind != AuthOAuth || strings.TrimSpace(sec.AccessToken) == "" || !KnownOAuthProvider(sec.Provider) {
		return oauthSecret{}, false
	}
	sec.AccessToken = strings.TrimSpace(sec.AccessToken)
	sec.RefreshToken = strings.TrimSpace(sec.RefreshToken)
	return sec, true
}

// OAuthAccessToken reports the bearer token inside a browser-login secret.
// A password is returned as not-oauth so IMAP LOGIN and SMTP PLAIN stay unchanged.
func OAuthAccessToken(secret string) (string, bool) {
	sec, ok := parseOAuthSecret(secret)
	if !ok {
		return "", false
	}
	return sec.AccessToken, true
}

var (
	oauthRefreshMu    sync.Mutex
	oauthRefreshCache = map[string]string{}
)

// MaterializeSecret refreshes an expired browser-login secret. current is the
// secret dialers should use. updated is non-empty when the keyring copy must
// be replaced, which happens when the provider rotates the refresh token.
func MaterializeSecret(ctx context.Context, raw string, now time.Time) (current, updated string, err error) {
	sec, ok := parseOAuthSecret(raw)
	if !ok {
		return raw, "", nil
	}
	if !oauthSecretExpired(sec, now) {
		return raw, "", nil
	}
	if sec.RefreshToken == "" {
		return "", "", classErr("auth", MsgOAuthReauth)
	}
	provider, ok := oauthProviderTable[sec.Provider]
	if !ok {
		return "", "", classErr("auth", MsgOAuthReauth)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	blob, err := refreshOAuthSingle(ctx, provider, sec, now)
	if err != nil {
		return "", "", err
	}
	if blob == strings.TrimSpace(raw) {
		return blob, "", nil
	}
	return blob, blob, nil
}

type oauthRefreshFlight struct {
	done chan struct{}
	blob string
	err  error
}

var oauthRefreshFlights = map[string]*oauthRefreshFlight{}

// refreshOAuthSingle lets one refresh run per refresh token. Other mailboxes
// keep moving, and a second caller waits for that result instead of rotating
// the token twice. A cached result is used only while it is still fresh.
func refreshOAuthSingle(ctx context.Context, provider oauthProvider, sec oauthSecret, now time.Time) (string, error) {
	oauthRefreshMu.Lock()
	if blob, ok := freshOAuthRefreshCache(sec.RefreshToken, now); ok {
		oauthRefreshMu.Unlock()
		return blob, nil
	}
	if flight, ok := oauthRefreshFlights[sec.RefreshToken]; ok {
		oauthRefreshMu.Unlock()
		return waitOAuthRefresh(ctx, flight)
	}
	flight := &oauthRefreshFlight{done: make(chan struct{})}
	oauthRefreshFlights[sec.RefreshToken] = flight
	oauthRefreshMu.Unlock()

	// The caller's cancel must not abort the HTTP refresh. Microsoft rotates
	// the refresh token as soon as it accepts the request; dropping the
	// response would leave every waiter holding a dead token.
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	refreshCtx, refreshCancel := context.WithTimeout(context.WithoutCancel(parent), 20*time.Second)
	defer refreshCancel()

	finished := false
	defer func() {
		if finished {
			return
		}
		rec := recover()
		finishOAuthRefresh(sec.RefreshToken, flight, "", classErr("auth", MsgOAuthReauth), now)
		if rec != nil {
			panic(rec)
		}
	}()
	blob, err := refreshOAuthSecret(refreshCtx, provider, sec, now)
	finishOAuthRefresh(sec.RefreshToken, flight, blob, err, now)
	finished = true
	return blob, err
}

func waitOAuthRefresh(ctx context.Context, flight *oauthRefreshFlight) (string, error) {
	select {
	case <-flight.done:
	case <-ctx.Done():
		select {
		case <-flight.done:
		default:
			return "", classErr("connect", MsgConnect)
		}
	}
	if flight.err != nil {
		return "", flight.err
	}
	return flight.blob, nil
}

func finishOAuthRefresh(refreshToken string, flight *oauthRefreshFlight, blob string, err error, now time.Time) {
	oauthRefreshMu.Lock()
	defer oauthRefreshMu.Unlock()
	flight.blob, flight.err = blob, err
	select {
	case <-flight.done:
	default:
		close(flight.done)
	}
	delete(oauthRefreshFlights, refreshToken)
	if err == nil {
		rememberOAuthRefresh(refreshToken, blob, now)
	}
}

func freshOAuthRefreshCache(refreshToken string, now time.Time) (string, bool) {
	blob, ok := oauthRefreshCache[refreshToken]
	if !ok {
		return "", false
	}
	cached, ok := parseOAuthSecret(blob)
	if !ok || oauthSecretExpired(cached, now) {
		delete(oauthRefreshCache, refreshToken)
		return "", false
	}
	return blob, true
}

func rememberOAuthRefresh(oldRefresh, blob string, now time.Time) {
	cached, ok := parseOAuthSecret(blob)
	if !ok {
		return
	}
	oauthRefreshCache[oldRefresh] = blob
	if cached.RefreshToken != "" {
		oauthRefreshCache[cached.RefreshToken] = blob
	}
	if len(oauthRefreshCache) < 8 {
		return
	}
	for key, item := range oauthRefreshCache {
		sec, ok := parseOAuthSecret(item)
		if !ok || oauthSecretExpired(sec, now) {
			delete(oauthRefreshCache, key)
		}
	}
}

func refreshOAuthSecret(ctx context.Context, provider oauthProvider, sec oauthSecret, now time.Time) (string, error) {
	tok, err := refreshOAuthToken(ctx, provider, sec.RefreshToken)
	if err != nil {
		return "", err
	}
	refresh := strings.TrimSpace(tok.RefreshToken)
	if refresh == "" {
		refresh = sec.RefreshToken
	}
	blob, err := encodeOAuthSecret(oauthSecret{
		Kind:         AuthOAuth,
		Provider:     sec.Provider,
		AccessToken:  tok.AccessToken,
		RefreshToken: refresh,
		Expiry:       oauthExpiry(now, tok.ExpiresIn),
	})
	if err != nil {
		return "", classErr("auth", MsgOAuthReauth)
	}
	return blob, nil
}

func oauthSecretExpired(sec oauthSecret, now time.Time) bool {
	if sec.Expiry.IsZero() {
		return true
	}
	return !now.Add(5 * time.Minute).Before(sec.Expiry)
}

// SaveOAuthAccount creates or updates the mailbox for a browser login.
// The same address updates the existing mailbox and keeps its name.
func SaveOAuthAccount(existing []Account, email, provider string, now time.Time) ([]Account, Account, error) {
	spec, ok := oauthProviderTable[strings.TrimSpace(provider)]
	if !ok {
		return nil, Account{}, classErr("invalid", "不支持这个邮箱登录")
	}
	email, err := normalizeAddress(email)
	if err != nil {
		return nil, Account{}, err
	}
	var prev *Account
	for i := range existing {
		if strings.EqualFold(existing[i].Address, email) {
			prev = &existing[i]
			break
		}
	}
	name := spec.Label
	id := ""
	if prev != nil {
		id = prev.ID
		name = prev.Name
	} else {
		name = oauthAccountName(existing, spec.Label, email)
	}
	inbound, outbound := spec.endpoints(email)
	enabled := true
	auth := AuthOAuth
	prov := spec.ID
	return UpsertAccount(existing, SaveRequest{
		ID: id, Name: name, Address: email, Enabled: &enabled,
		Inbound: &inbound, Outbound: &outbound, Auth: &auth, Provider: &prov,
	}, now)
}

func (p oauthProvider) endpoints(email string) (Endpoint, Endpoint) {
	in := p.Inbound
	out := p.Outbound
	in.Username = email
	in.Box = "INBOX"
	out.Username = email
	out.FromAddress = email
	return in, out
}

func oauthAccountName(existing []Account, label, email string) string {
	local, _, _ := strings.Cut(email, "@")
	candidates := []string{label, label + " (" + local + ")", label + " (" + email + ")"}
	for i := 2; i < 20; i++ {
		candidates = append(candidates, fmt.Sprintf("%s %d", label, i))
	}
	for _, name := range candidates {
		if !oauthNameTaken(existing, name) {
			return name
		}
	}
	return label + " " + email
}

func oauthNameTaken(existing []Account, name string) bool {
	for _, acc := range existing {
		if acc.Name == name {
			return true
		}
	}
	return false
}

func oauthPKCE() (verifier, challenge string, err error) {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
	buf := make([]byte, 64)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	for i := range buf {
		buf[i] = chars[int(buf[i])%len(chars)]
	}
	verifier = string(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func oauthState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

type oauthLoopback struct {
	redirect string
	want     string
	ch       chan oauthCallback
	errCh    chan error
	srv      *http.Server
}

type oauthCallback struct {
	code  string
	state string
}

func startOAuthLoopback(state string) (*oauthLoopback, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	loop := &oauthLoopback{
		redirect: fmt.Sprintf("http://127.0.0.1:%d", port),
		want:     state,
		ch:       make(chan oauthCallback, 1),
		errCh:    make(chan error, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", loop.serve)
	loop.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go loop.srv.Serve(ln) //nolint:errcheck
	if err := waitOAuthLoopback(loop.redirect); err != nil {
		loop.close()
		return nil, err
	}
	return loop, nil
}

func waitOAuthLoopback(rawURL string) error {
	deadline := time.Now().Add(2 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		resp, err := oauthHTTP.Get(rawURL + "/")
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			return nil
		}
		last = err
		time.Sleep(10 * time.Millisecond)
	}
	if last == nil {
		last = errors.New("loopback did not start")
	}
	return last
}

func (l *oauthLoopback) close() {
	if l == nil || l.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = l.srv.Shutdown(ctx)
}

func (l *oauthLoopback) wait(ctx context.Context) (string, string, error) {
	select {
	case cb := <-l.ch:
		return cb.code, cb.state, nil
	case err := <-l.errCh:
		return "", "", err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", "", classErr("auth", "登录超时，请重试")
		}
		return "", "", classErr("auth", "登录已取消")
	}
}

func (l *oauthLoopback) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	if errCode := strings.TrimSpace(q.Get("error")); errCode != "" {
		desc := strings.TrimSpace(q.Get("error_description"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, oauthErrorHTML, html.EscapeString(errCode), html.EscapeString(desc))
		msg := "登录已取消"
		if errCode != "access_denied" && errCode != "consent_required" {
			msg = "登录失败，授权没有完成"
		}
		select {
		case l.errCh <- classErr("auth", msg):
		default:
		}
		return
	}
	code := strings.TrimSpace(q.Get("code"))
	if code == "" || len(code) > 8192 || strings.ContainsAny(code, " \r\n") {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	gotState := strings.TrimSpace(q.Get("state"))
	if subtle.ConstantTimeCompare([]byte(gotState), []byte(l.want)) != 1 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, oauthErrorHTML, "state", "登录回调无效")
		select {
		case l.errCh <- classErr("auth", "登录回调无效"):
		default:
		}
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(oauthSuccessHTML))
	select {
	case l.ch <- oauthCallback{code: code, state: strings.TrimSpace(q.Get("state"))}:
	default:
	}
}

const oauthSuccessHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>MaClaw</title></head>
<body style="font-family:sans-serif;text-align:center;padding:60px">
  <h2>已收到授权</h2>
  <p>请回到 MaClaw。登录结果会显示在邮箱设置里。此页面将自动关闭。</p>
  <script>setTimeout(()=>window.close(),3000)</script>
</body></html>`

const oauthErrorHTML = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>MaClaw</title></head>
<body style="font-family:sans-serif;text-align:center;padding:60px">
  <h2>授权失败</h2>
  <p>错误: %s</p>
  <p>%s</p>
  <p>请关闭此页面并重试。</p>
</body></html>`
