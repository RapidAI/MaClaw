package tinytex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// TLNetResolveTimeout bounds one redirect lookup. tlmgr follows
	// mirror.ctan.org again on every invocation, so update --self and the
	// later install can otherwise unpack against two different revisions.
	TLNetResolveTimeout = 12 * time.Second
)

var errTLNetDowngrade = errors.New("refusing https to http tlnet redirect")

// ResolveTLNet follows redirects and returns one concrete https tlnet root.
// The original repository is kept when the lookup fails, the final URL is
// not a tlnet repository, or https would be downgraded to http.
func ResolveTLNet(ctx context.Context, repo string) string {
	return resolveTLNet(ctx, repo, nil)
}

func resolveTLNet(ctx context.Context, repo string, client *http.Client) string {
	repo = strings.TrimRight(strings.TrimSpace(repo), "/")
	if repo == "" {
		return repo
	}
	if client == nil {
		if !strings.HasPrefix(repo, "https://") {
			return repo
		}
		client = tlnetClient()
	}
	if final, ok := followTLNet(ctx, client, repo); ok {
		return final
	}
	return repo
}

func tlnetClient() *http.Client {
	transport := http.DefaultTransport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	}
	return &http.Client{
		Timeout:       TLNetResolveTimeout,
		Transport:     transport,
		CheckRedirect: tlnetCheckRedirect,
	}
}

func tlnetCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 8 {
		return errors.New("too many tlnet redirects")
	}
	if req.URL == nil || (req.URL.Scheme != "https" && req.URL.Scheme != "http") {
		return errors.New("refusing tlnet redirect scheme")
	}
	if len(via) > 0 && via[0].URL != nil && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errTLNetDowngrade
	}
	return nil
}

// followTLNet prefers a HEAD that already landed on a different mirror.
// A redirector that answers HEAD without moving still needs the GET tlmgr
// itself would issue. The body is not the repository; only the final URL is.
func followTLNet(ctx context.Context, client *http.Client, repo string) (string, bool) {
	head, headOK := requestTLNet(ctx, client, http.MethodHead, repo)
	if headOK && head != repo {
		return head, true
	}
	if get, ok := requestTLNet(ctx, client, http.MethodGet, repo); ok {
		return get, true
	}
	if headOK {
		return head, true
	}
	return "", false
}

func requestTLNet(ctx context.Context, client *http.Client, method, repo string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, method, repo, nil)
	if err != nil {
		return "", false
	}
	if method == http.MethodGet {
		req.Header.Set("Range", "bytes=0-0")
	}
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		if resp != nil {
			resp.Body.Close()
		}
		return "", false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
	if resp.Request == nil || resp.Request.URL == nil {
		return "", false
	}
	return concreteTLNet(repo, resp.Request.URL.String())
}

// concreteTLNet accepts a repository root and rejects anything tlmgr should
// not be pinned to. The path has to end at texlive/tlnet so a redirect into
// a package subdirectory is not stored as the repository.
func concreteTLNet(original, final string) (string, bool) {
	ou, err := url.Parse(strings.TrimSpace(original))
	if err != nil || ou.Host == "" {
		return "", false
	}
	fu, err := url.Parse(strings.TrimSpace(final))
	if err != nil || fu.Host == "" || fu.User != nil || fu.RawQuery != "" {
		return "", false
	}
	if ou.Scheme == "https" && fu.Scheme != "https" {
		return "", false
	}
	if fu.Scheme != "https" && fu.Scheme != "http" {
		return "", false
	}
	// Path is decoded. Assigning EscapedPath back into Path would encode a
	// "%" a second time when the URL is serialized for tlmgr.
	path := strings.TrimRight(fu.Path, "/")
	if !strings.HasSuffix(path, "/texlive/tlnet") {
		return "", false
	}
	fu.Path = path
	fu.RawPath = ""
	fu.RawQuery = ""
	fu.Fragment = ""
	return fu.String(), true
}
