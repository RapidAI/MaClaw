package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const maxHubDownloadAttempts = 3

var errMaclawHubDownloadNeedsLogin = fmt.Errorf("MaClaw Hub download requires a signed-in hub account")

// HubDownloadChannel is the runtime RapidSearch download proxy (same Hub
// bearer token as search). It is never persisted.
type HubDownloadChannel struct {
	Token   string
	BaseURL string
}

// HubDownloadFromStrategy returns the download channel when MaClaw Hub /
// RapidSearch is enabled. A nil channel means the engine is off and callers
// should download directly. A missing hub token is an error, not a silent
// fallback to a direct GET.
func HubDownloadFromStrategy(strategy corelib.WebSearchStrategy) (*HubDownloadChannel, error) {
	for _, engine := range strategy.Engines {
		if engine.ID != WebSearchEngineMaclawHub {
			continue
		}
		if !engine.Enabled {
			return nil, nil
		}
		token := strings.TrimSpace(engine.APIKey)
		if token == "" {
			return nil, errMaclawHubDownloadNeedsLogin
		}
		return &HubDownloadChannel{Token: token, BaseURL: engine.BaseURL}, nil
	}
	return nil, nil
}

// ApplyHubDownload routes later Fetch/download calls through the Hub proxy
// when RapidSearch is enabled. Public-network-only callers stay on the
// guarded direct transport.
func ApplyHubDownload(opts *FetchOptions, strategy corelib.WebSearchStrategy) error {
	if opts == nil {
		return fmt.Errorf("fetch options required")
	}
	if opts.PublicNetworkOnly {
		opts.HubDownload = nil
		return nil
	}
	channel, err := HubDownloadFromStrategy(strategy)
	if err != nil {
		return err
	}
	if channel != nil {
		opts.HubDownload = channel
		applyHubDownloadTimeout(opts)
	}
	return nil
}

// FetchWithStrategyCtx is Fetch with strategy-aware Hub download routing.
func FetchWithStrategyCtx(parent context.Context, rawURL string, opts *FetchOptions, strategy corelib.WebSearchStrategy) (*FetchResult, error) {
	if opts == nil {
		opts = &FetchOptions{}
	}
	if err := ApplyHubDownload(opts, strategy); err != nil {
		return nil, err
	}
	return FetchCtx(parent, rawURL, opts)
}

func applyProviderHubDownload(opts *FetchOptions, provider corelib.WebSearchProvider) error {
	if opts == nil {
		return fmt.Errorf("fetch options required")
	}
	if opts.PublicNetworkOnly {
		return nil
	}
	token := strings.TrimSpace(provider.Key)
	if token == "" {
		return errMaclawHubDownloadNeedsLogin
	}
	opts.HubDownload = &HubDownloadChannel{Token: token, BaseURL: provider.BaseURL}
	applyHubDownloadTimeout(opts)
	return nil
}

func shouldRouteMaclawHubDownload(opts *FetchOptions) bool {
	return opts != nil && !opts.PublicNetworkOnly && opts.HubDownload != nil
}

func prepareHubDownload(opts *FetchOptions) error {
	if opts == nil || opts.HubDownload == nil {
		return errMaclawHubDownloadNeedsLogin
	}
	if strings.TrimSpace(opts.HubDownload.Token) == "" {
		return errMaclawHubDownloadNeedsLogin
	}
	applyHubDownloadTimeout(opts)
	return nil
}

func applyHubDownloadTimeout(opts *FetchOptions) {
	if opts == nil {
		return
	}
	minSec := int(WebSearchMaclawHubDownloadTimeout / time.Second)
	if opts.TimeoutS < minSec {
		opts.TimeoutS = minSec
	}
}

func maclawHubDownloadURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return defaultMaclawHubDownloadURL, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid download base URL")
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case path == "/searchproxy" || strings.HasSuffix(path, "/searchproxy"):
		u.Path = path + "/download"
	case strings.HasSuffix(path, "/search"):
		u.Path = strings.TrimSuffix(path, "search") + "download"
	case strings.HasSuffix(path, "/download"):
		u.Path = path
	case path == "":
		u.Path = "/download"
	default:
		u.Path = path + "/download"
	}
	return u.String(), nil
}

func maclawHubDownloadHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       0,
		Transport:     httpClient().Transport,
		CheckRedirect: sharedCheckRedirect,
	}
}

func fetchViaMaclawHub(ctx context.Context, rawURL string, opts *FetchOptions) (*FetchResult, error) {
	if err := validateHubDownloadTarget(rawURL); err != nil {
		return nil, err
	}
	logURL := sanitizeURLForLog(rawURL)
	dlogf("[download] hub-proxy start url=%q save_path=%q timeout=%ds", logURL, opts.SavePath, opts.TimeoutS)

	client := maclawHubDownloadHTTPClient()
	var lastErr error
	for attempt := 1; attempt <= maxHubDownloadAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(1<<uint(attempt-2)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			dlogf("[download] hub-proxy retry attempt=%d backoff=%s url=%q", attempt, backoff, logURL)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		out := performHubProxyFetch(ctx, rawURL, logURL, opts, client)
		if out.err == nil {
			dlogf("[download] hub-proxy success url=%q bytes=%d saved=%q attempts=%d", logURL, out.result.BytesRead, out.result.SavedTo, attempt)
			return out.result, nil
		}
		lastErr = out.err
		dlogf("[download] hub-proxy attempt=%d failed: %v retryable=%t", attempt, out.err, out.retryable)
		if !out.retryable {
			return nil, out.err
		}
	}
	return nil, lastErr
}

func validateHubDownloadTarget(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil {
		return fmt.Errorf("invalid URL")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("hub download only supports HTTP(S) URLs")
	}
	if strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("invalid URL")
	}
	return nil
}

func performHubProxyFetch(ctx context.Context, rawURL, logURL string, opts *FetchOptions, client *http.Client) *fetchAttempt {
	downloadURL, err := maclawHubDownloadURL(opts.HubDownload.BaseURL)
	if err != nil {
		return &fetchAttempt{err: err}
	}
	resp, err := doHubProxyRequest(ctx, client, downloadURL, rawURL, opts.HubDownload.Token, opts, http.MethodGet)
	if err != nil {
		return &fetchAttempt{err: fmt.Errorf("MaClaw Hub download request failed: %w", err), retryable: true}
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		resp, err = doHubProxyRequest(ctx, client, downloadURL, rawURL, opts.HubDownload.Token, opts, http.MethodPost)
		if err != nil {
			return &fetchAttempt{err: fmt.Errorf("MaClaw Hub download request failed: %w", err), retryable: true}
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return &fetchAttempt{err: hubProxyStatusError(resp.StatusCode, peek)}
	}
	if resp.StatusCode >= 400 {
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		var retryAfter time.Duration
		if retryable {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		}
		return &fetchAttempt{
			err:        hubProxyStatusError(resp.StatusCode, peek),
			retryable:  retryable,
			retryAfter: retryAfter,
		}
	}
	if resp.StatusCode >= 300 {
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return &fetchAttempt{err: hubProxyStatusError(resp.StatusCode, peek)}
	}

	var out *fetchAttempt
	if strings.TrimSpace(opts.SavePath) != "" {
		out = completeFileDownload(rawURL, logURL, opts, resp)
	} else {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, opts.MaxBytes))
		if readErr != nil {
			return &fetchAttempt{err: fmt.Errorf("read body failed: %w", readErr), retryable: true}
		}
		out = completeTextFetch(rawURL, opts, resp, body, false)
	}
	if out != nil && out.result != nil {
		// Keep the caller's target URL; the proxy request URL is an
		// implementation detail and may include the target as a query value.
		out.result.URL = rawURL
	}
	return out
}

func doHubProxyRequest(ctx context.Context, client *http.Client, downloadURL, targetURL, token string, opts *FetchOptions, method string) (*http.Response, error) {
	var req *http.Request
	var err error
	switch method {
	case http.MethodPost:
		payload, marshalErr := json.Marshal(map[string]string{"url": targetURL})
		if marshalErr != nil {
			return nil, marshalErr
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, downloadURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	default:
		u, parseErr := url.Parse(downloadURL)
		if parseErr != nil {
			return nil, parseErr
		}
		query := u.Query()
		query.Set("url", targetURL)
		u.RawQuery = query.Encode()
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", pickUserAgent())
	if rangeVal := headerFromOpts(opts, "Range"); rangeVal != "" {
		req.Header.Set("Range", rangeVal)
	}
	return client.Do(req)
}

func headerFromOpts(opts *FetchOptions, name string) string {
	if opts == nil {
		return ""
	}
	for key, value := range opts.Headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

func hubProxyStatusError(status int, body []byte) error {
	if status == http.StatusUnauthorized {
		return fmt.Errorf("MaClaw Hub download returned HTTP 401 (sign in to MaClaw Hub)")
	}
	detail := hubProxyErrorDetail(body)
	if detail != "" {
		return fmt.Errorf("MaClaw Hub download returned HTTP %d: %s", status, detail)
	}
	return fmt.Errorf("MaClaw Hub download returned HTTP %d", status)
}

func hubProxyErrorDetail(body []byte) string {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	detail := strings.TrimSpace(payload.Error)
	if detail == "" {
		detail = strings.TrimSpace(payload.Message)
	}
	if detail == "" {
		detail = strings.TrimSpace(payload.Detail)
	}
	if detail == "" || hubProxyDetailLooksSecret(detail) {
		return ""
	}
	if len([]rune(detail)) > 200 {
		detail = string([]rune(detail)[:200]) + "…"
	}
	return detail
}

func hubProxyDetailLooksSecret(detail string) bool {
	lower := strings.ToLower(detail)
	for _, marker := range []string{
		"bearer ", "authorization", "proxy.token", "api key", "apikey", "access token", "token", "credential",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
