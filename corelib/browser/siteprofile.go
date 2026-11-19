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
	// VerifyAPI is a JSON endpoint listing the account's posts, read with
	// the page's session; the post's title found in it is publish evidence.
	VerifyAPI string `json:"verify_api,omitempty"`
	// LazyUpload: verified that the site keeps picked pictures local and
	// sends them with the post (see PublishSpec.LazyUpload).
	LazyUpload bool `json:"lazy_upload,omitempty"`
	// Tags and Category fill a publish settings panel's required fields
	// when the request names none.
	Tags     []string `json:"tags,omitempty"`
	Category string   `json:"category,omitempty"`
	// EditURL opens a published post for editing; {url} is the post URL.
	EditURL string `json:"edit_url"`
	// PostURL builds a post's public URL from an id the list reports ({id});
	// {user} is the signed-in account name read from UserCookie.
	PostURL string `json:"post_url"`
	// UserCookie: the cookie holding the account name PostURL's {user}
	// needs. It is often set on another host of the same site (HttpOnly),
	// so it is read from the browser's cookie jar, not only document.cookie
	// on the editor page.
	UserCookie string `json:"user_cookie,omitempty"`
	// EditorRedirects: regexps of other editors on the site (a second,
	// different editor this profile's selectors do not fit); an editor_url
	// matching one is replaced by EditorURL.
	EditorRedirects []string `json:"editor_redirects,omitempty"`
	// PendingTexts: text on the result page that says the post is out but
	// still under review (not public yet), e.g. 审核中.
	PendingTexts []string `json:"pending_texts,omitempty"`

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
	for _, r := range p.EditorRedirects {
		if _, err := regexp.Compile(r); err != nil {
			return nil, fmt.Errorf("profile %s editor_redirects: %v", p.ID, err)
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

// ProfilesForRequest lists every site the request names (a URL or a
// profile's request word), in the order the request first mentions them, so
// one task that posts to several sites can go through them one by one.
func ProfilesForRequest(request string) []*SiteProfile {
	type hit struct {
		p   *SiteProfile
		pos int
	}
	var hits []hit
	add := func(p *SiteProfile, pos int) {
		if p == nil || pos < 0 {
			return
		}
		for i := range hits {
			if hits[i].p.ID == p.ID {
				if pos < hits[i].pos {
					hits[i].pos = pos
				}
				return
			}
		}
		hits = append(hits, hit{p, pos})
	}
	for _, loc := range requestURLRe.FindAllStringIndex(request, -1) {
		add(ProfileForURL(request[loc[0]:loc[1]]), loc[0])
	}
	low := strings.ToLower(request)
	for _, p := range SiteProfiles() {
		for _, w := range p.RequestWords {
			if w == "" {
				continue
			}
			if i := strings.Index(low, strings.ToLower(w)); i >= 0 {
				add(p, i)
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	out := make([]*SiteProfile, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.p)
	}
	return out
}

// IsPostResultURL: the URL is a site's page for a post already out (its
// profile's success_url), i.e. the result of some earlier publish.
func IsPostResultURL(raw string) bool {
	p := ProfileForURL(raw)
	if p == nil || p.SuccessURL == "" {
		return false
	}
	re, err := regexp.Compile(p.SuccessURL)
	return err == nil && re.MatchString(strings.TrimSpace(raw))
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
	if p == nil {
		return ""
	}
	return PostURLFrom(p.PostURL, id, "")
}

// PublishURLIsBareOrigin reports a URL with no path. https://blog.csdn.net/
// is the public homepage, not the account's post list.
func PublishURLIsBareOrigin(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	// A tracking query (https://blog.csdn.net/?spm=1) is still the homepage.
	return strings.Trim(u.Path, "/") == ""
}

// SuccessPatternCaptures is how many groups a success_url regexp captures
// (0 when it does not compile). The first group is the post id; a learned
// pattern that dropped it can no longer build the public link.
func SuccessPatternCaptures(pattern string) int {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return 0
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0
	}
	return re.NumSubexp()
}

// RestorePublishIdentity puts profile fields back when a learned record
// replaced them with a value that can no longer build the public link.
// A homepage is not a post list. A success_url that dropped its capture
// group can no longer yield the post id. Empty post_url / user_cookie are
// filled from the profile (a learned record used to forget both).
func RestorePublishIdentity(p *SiteProfile, s *PublishSpec) {
	if p == nil || s == nil {
		return
	}
	if PublishURLIsBareOrigin(s.VerifyURL) && strings.TrimSpace(p.VerifyURL) != "" && !PublishURLIsBareOrigin(p.VerifyURL) {
		s.VerifyURL = p.VerifyURL
	}
	if SuccessPatternCaptures(s.SuccessURL) < SuccessPatternCaptures(p.SuccessURL) {
		s.SuccessURL = p.SuccessURL
	}
	if strings.TrimSpace(s.PostURL) == "" {
		s.PostURL = p.PostURL
	}
	if strings.TrimSpace(s.UserCookie) == "" {
		s.UserCookie = p.UserCookie
	}
}

// PostURLFrom fills a post URL template; "" when a placeholder it needs
// ({id}, or {user} when the template has one) is unknown.
func PostURLFrom(tmpl, id, user string) string {
	if tmpl == "" || id == "" {
		return ""
	}
	if strings.Contains(tmpl, "{user}") {
		if user == "" {
			return ""
		}
		tmpl = strings.ReplaceAll(tmpl, "{user}", url.PathEscape(user))
	}
	return strings.ReplaceAll(tmpl, "{id}", url.PathEscape(id))
}

// RedirectsEditor: raw is another editor of this site that the profile
// replaces with its own EditorURL.
func (p *SiteProfile) RedirectsEditor(raw string) bool {
	if p == nil || p.EditorURL == "" || raw == "" {
		return false
	}
	for _, r := range p.EditorRedirects {
		if re, err := regexp.Compile(r); err == nil && re.MatchString(strings.TrimSpace(raw)) {
			return true
		}
	}
	return false
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
	if p.RedirectsEditor(s.EditorURL) {
		// A second editor of the site the profile's selectors do not fit
		// (CSDN's rich mp.csdn.net editor dropped the pictures, run_6840f5a1).
		s.EditorURL = p.EditorURL
	}
	str(&s.EditorURL, p.EditorURL)
	str(&s.VerifyURL, p.VerifyURL)
	str(&s.VerifyAPI, p.VerifyAPI)
	if p.LazyUpload {
		s.LazyUpload = true
	}
	str(&s.Category, p.Category)
	list(&s.Tags, p.Tags)
	str(&s.EditURL, p.EditURL)
	str(&s.TitleSelector, p.TitleSelector)
	str(&s.BodySelector, p.BodySelector)
	str(&s.UploadSelector, p.UploadSelector)
	str(&s.BodyKind, p.BodyKind)
	str(&s.ImageMode, p.ImageMode)
	str(&s.PlainBold, p.PlainBold)
	str(&s.SuccessURL, p.SuccessURL)
	str(&s.PostURL, p.PostURL)
	str(&s.UserCookie, p.UserCookie)
	list(&s.PendingTexts, p.PendingTexts)
	list(&s.ImageTabTexts, p.ImageTabTexts)
	list(&s.SubmitTexts, p.SubmitTexts)
	list(&s.ConfirmTexts, p.ConfirmTexts)
	list(&s.UpdateTexts, p.UpdateTexts)
	list(&s.SuccessTexts, p.SuccessTexts)
}
