package browser

// Site profiles: everything the publish pipeline knows about one site is
// data, loaded from siteprofiles/*.json (embedded) and from the directory in
// MACLAW_SITE_PROFILES_DIR (a file there with the same id replaces the
// embedded one). Code never names a site; a site without a profile runs on
// the pipeline's generic discovery defaults.

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// SiteProfile is the declared shape of one site's publishing surface. Every
// field is optional; an empty field falls back to generic discovery.
type SiteProfile struct {
	ID string `json:"id"`
	// Hosts the profile applies to (suffix match: "example.com" also
	// matches "www.example.com").
	Hosts []string `json:"hosts"`
	// RequestWords name the site in a person's request (case-insensitive
	// literal words, e.g. the site's name in several languages).
	RequestWords []string `json:"request_words"`

	EditorURL string `json:"editor_url"`
	// VerifyURL lists the account's posts (where a new post must appear).
	VerifyURL string `json:"verify_url"`
	// EditURL opens a published post for editing; {url} is the post URL.
	EditURL string `json:"edit_url"`
	// PostURL builds a post's public URL from an id the list reports ({id}).
	PostURL string `json:"post_url"`

	TitleSelector  string `json:"title_selector"`
	BodySelector   string `json:"body_selector"`
	UploadSelector string `json:"upload_selector"`
	// BodyKind: "rich" (formatted HTML), "markdown" or "plain"; empty
	// means detect from the editor element.
	BodyKind string `json:"body_kind"`
	// ImageMode: "paste" (pictures pasted into the body), "gallery"
	// (pictures go to a file input, the body is plain text) or empty
	// (detect).
	ImageMode string `json:"image_mode"`
	// ImageTabTexts: labels of a tab that switches the editor to image posts.
	ImageTabTexts []string `json:"image_tab_texts"`
	// PlainBold renders a bold or heading run for a plain-text body; $1 is
	// the text (default: the text unchanged with line breaks kept).
	PlainBold string `json:"plain_bold"`

	SubmitTexts  []string `json:"submit_texts"`
	ConfirmTexts []string `json:"confirm_texts"`
	UpdateTexts  []string `json:"update_texts"`
	// SuccessTexts: text that appears on the page after a successful
	// submit (only text that was not on the page before the click counts).
	SuccessTexts []string `json:"success_texts"`
	// SuccessURL: a regexp the page URL matches once the post is out.
	SuccessURL string `json:"success_url"`

	// Playbook: notes for the model when it works on this site by hand
	// (shown as the site's seed experience).
	Playbook []string `json:"playbook"`
	// RiskySubmitSelectors: selector fragments whose click submits a post
	// on this site (a click on one needs the publish approval).
	RiskySubmitSelectors []string `json:"risky_submit_selectors"`
}

//go:embed siteprofiles/*.json
var embeddedProfiles embed.FS

var (
	profilesOnce sync.Once
	profilesList []*SiteProfile
	profilesErr  error
)

// SiteProfiles returns every loaded profile (embedded first, then the
// directory's, which override by id), sorted by id.
func SiteProfiles() []*SiteProfile {
	profilesOnce.Do(func() { profilesList, profilesErr = loadProfiles(os.Getenv("MACLAW_SITE_PROFILES_DIR")) })
	return profilesList
}

// SiteProfilesError is the first error met while loading profiles (bad files
// are skipped, never fatal).
func SiteProfilesError() error { SiteProfiles(); return profilesErr }

func loadProfiles(dir string) ([]*SiteProfile, error) {
	byID := map[string]*SiteProfile{}
	var firstErr error
	note := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	ents, _ := embeddedProfiles.ReadDir("siteprofiles")
	for _, e := range ents {
		b, err := embeddedProfiles.ReadFile("siteprofiles/" + e.Name())
		if err != nil {
			note(err)
			continue
		}
		if p, err := ParseSiteProfile(b); err != nil {
			note(fmt.Errorf("%s: %v", e.Name(), err))
		} else {
			byID[p.ID] = p
		}
	}
	if dir != "" {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				note(err)
				continue
			}
			if p, err := ParseSiteProfile(b); err != nil {
				note(fmt.Errorf("%s: %v", f, err))
			} else {
				byID[p.ID] = p
			}
		}
	}
	out := make([]*SiteProfile, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, firstErr
}

// ParseSiteProfile reads and checks one profile.
func ParseSiteProfile(b []byte) (*SiteProfile, error) {
	var p SiteProfile
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, fmt.Errorf("profile has no id")
	}
	if len(p.Hosts) == 0 {
		return nil, fmt.Errorf("profile %s has no hosts", p.ID)
	}
	if p.SuccessURL != "" {
		if _, err := regexp.Compile(p.SuccessURL); err != nil {
			return nil, fmt.Errorf("profile %s success_url: %v", p.ID, err)
		}
	}
	switch p.BodyKind {
	case "", "rich", "markdown", "plain":
	default:
		return nil, fmt.Errorf("profile %s body_kind %q", p.ID, p.BodyKind)
	}
	switch p.ImageMode {
	case "", "paste", "gallery":
	default:
		return nil, fmt.Errorf("profile %s image_mode %q", p.ID, p.ImageMode)
	}
	return &p, nil
}

// ProfileForURL finds the profile whose hosts cover the URL's host.
func ProfileForURL(raw string) *SiteProfile {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	h := strings.ToLower(u.Hostname())
	for _, p := range SiteProfiles() {
		for _, s := range p.Hosts {
			s = strings.ToLower(s)
			if h == s || strings.HasSuffix(h, "."+s) {
				return p
			}
		}
	}
	return nil
}

// ProfileForRequest finds the profile a request names: a URL in it first,
// then the profiles' request words.
func ProfileForRequest(request string) *SiteProfile {
	for _, m := range requestURLRe.FindAllString(request, -1) {
		if p := ProfileForURL(m); p != nil {
			return p
		}
	}
	low := strings.ToLower(request)
	for _, p := range SiteProfiles() {
		for _, w := range p.RequestWords {
			if w != "" && strings.Contains(low, strings.ToLower(w)) {
				return p
			}
		}
	}
	return nil
}

var requestURLRe = regexp.MustCompile(`https?://[^\s，。)'"]+`)

// SiteRequestWords lists every profile's request words (for intent checks).
func SiteRequestWords() []string {
	var out []string
	for _, p := range SiteProfiles() {
		out = append(out, p.RequestWords...)
	}
	return out
}

// PostURLFor builds a post's public URL from a list-reported id.
func (p *SiteProfile) PostURLFor(id string) string {
	if p == nil || p.PostURL == "" || id == "" {
		return ""
	}
	return strings.ReplaceAll(p.PostURL, "{id}", url.PathEscape(id))
}

// Apply fills the spec's empty fields from the profile; what the caller set
// explicitly wins.
func (p *SiteProfile) Apply(s *PublishSpec) {
	if p == nil {
		return
	}
	str := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	list := func(dst *[]string, v []string) {
		if len(*dst) == 0 && len(v) > 0 {
			*dst = append([]string(nil), v...)
		}
	}
	str(&s.EditorURL, p.EditorURL)
	str(&s.VerifyURL, p.VerifyURL)
	str(&s.EditURL, p.EditURL)
	str(&s.TitleSelector, p.TitleSelector)
	str(&s.BodySelector, p.BodySelector)
	str(&s.UploadSelector, p.UploadSelector)
	str(&s.BodyKind, p.BodyKind)
	str(&s.ImageMode, p.ImageMode)
	str(&s.PlainBold, p.PlainBold)
	str(&s.SuccessURL, p.SuccessURL)
	list(&s.ImageTabTexts, p.ImageTabTexts)
	list(&s.SubmitTexts, p.SubmitTexts)
	list(&s.ConfirmTexts, p.ConfirmTexts)
	list(&s.UpdateTexts, p.UpdateTexts)
	list(&s.SuccessTexts, p.SuccessTexts)
}
