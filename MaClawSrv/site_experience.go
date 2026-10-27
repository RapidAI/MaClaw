package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// Site experience store (layer C of the publish design). How a site's
// publishing works is global per site: it does not depend on the account,
// so it lives once on the hub (MaClawSrv data dir, site-experience/), not per
// tenant. Each site has one current record plus an append-only history of
// every version, so a bad version can be rolled back. Facts about one
// account ("转正答题后才能发布" notices, login walls) are kept per tenant+user
// with a 7-day TTL and are never merged into the global record.
//
// Only a publish that ended "published" with evidence (response, redirect,
// list_api, list) verifies a method. A failure of a step the record pinned
// counts against it (fail_streak, suspect, then retired after two in a row);
// failures caused by the account (blocking notice, login, captcha) do not.
// The embedded site profiles are the v0 seeds; a retired learned record
// falls back to its seed. Code never names a site: the record is data.

const (
	siteExpSchema       = 1
	siteExpRetireAfter  = 2
	siteExpBlockTTL     = 7 * 24 * time.Hour
	siteExpMaxHistory   = 200
	siteExpMaxBlocks    = 20
	siteExpStatusActive = "active"
	siteExpStatusSuspct = "suspect"
	siteExpStatusRetird = "retired"
)

// siteExpRoot overrides the store directory in tests.
var siteExpRoot = ""

var siteExpMu sync.Mutex

type siteExperience struct {
	Schema       int    `json:"schema"`
	Site         string `json:"site"`
	Version      int    `json:"version"`
	Source       string `json:"source"` // seed | learned | rollback
	VerifiedAt   int64  `json:"verified_at,omitempty"`
	VerifiedRuns int    `json:"verified_runs,omitempty"`
	FailStreak   int    `json:"fail_streak,omitempty"`
	Status       string `json:"status"`
	// Profile: the publish surface (selectors, editor and list URLs, image
	// route, evidence endpoints) in the site-profile format, so Apply fills
	// a publish spec from it.
	Profile browser.SiteProfile `json:"profile"`
	// Method: how the verified publish went (upload route, input methods,
	// panel, evidence type and endpoint path), recorded by the pipeline.
	Method  map[string]string `json:"method,omitempty"`
	Updated int64             `json:"updated"`
	Note    string            `json:"note,omitempty"`
}

type siteAccountBlock struct {
	Site   string `json:"site"`
	Kind   string `json:"kind"` // notice | login | captcha
	Text   string `json:"text"`
	At     int64  `json:"at"`
	Expire int64  `json:"expire"`
}

func siteExpDir() string {
	if siteExpRoot != "" {
		return siteExpRoot
	}
	return filepath.Join(corelib.MaclawDataDir(), "site-experience")
}

var siteKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]{0,99}$`)

// siteExpKey: the matching profile's id, else the host's last two labels.
func siteExpKey(raw string) string {
	if p := browser.ProfileForURL(raw); p != nil {
		return p.ID
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	parts := strings.Split(strings.ToLower(u.Hostname()), ".")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	k := strings.Join(parts, ".")
	if !siteKeyRe.MatchString(k) {
		return ""
	}
	return k
}

func siteExpPath(site string) string { return filepath.Join(siteExpDir(), site+".json") }
func siteExpHistoryPath(site string) string {
	return filepath.Join(siteExpDir(), site+".history.jsonl")
}

// siteExpSeed: the embedded profile as version 0.
func siteExpSeed(site string) *siteExperience {
	for _, p := range browser.SiteProfiles() {
		if p.ID == site {
			prof := *p
			prof.Playbook = nil // notes for the model stay in the profile
			return &siteExperience{Schema: siteExpSchema, Site: site, Version: 0, Source: "seed", Status: siteExpStatusActive, Profile: prof}
		}
	}
	return nil
}

func siteExpLoadLocked(site string) *siteExperience {
	if !siteKeyRe.MatchString(site) {
		return nil
	}
	raw, err := os.ReadFile(siteExpPath(site))
	if err == nil {
		var e siteExperience
		if json.Unmarshal(raw, &e) == nil && e.Site == site {
			return &e
		}
	}
	return siteExpSeed(site)
}

// siteExpLoad returns the current record (a seed when nothing was learned).
func siteExpLoad(site string) *siteExperience {
	siteExpMu.Lock()
	defer siteExpMu.Unlock()
	return siteExpLoadLocked(site)
}

func writeFileAtomic(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// siteExpSaveLocked writes the record; a new version is appended to the
// history first.
func siteExpSaveLocked(e *siteExperience, newVersion bool) error {
	e.Schema = siteExpSchema
	e.Updated = time.Now().Unix()
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if newVersion {
		if err := os.MkdirAll(siteExpDir(), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(siteExpHistoryPath(e.Site), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, werr := f.Write(append(append([]byte(nil), raw...), '\n'))
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		if cerr != nil {
			return cerr
		}
	}
	return writeFileAtomic(siteExpPath(e.Site), raw)
}

// siteExpHistory lists the stored versions, oldest first.
func siteExpHistory(site string) []siteExperience {
	siteExpMu.Lock()
	defer siteExpMu.Unlock()
	return siteExpHistoryLocked(site)
}

func siteExpHistoryLocked(site string) []siteExperience {
	f, err := os.Open(siteExpHistoryPath(site))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []siteExperience
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e siteExperience
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Site == site {
			out = append(out, e)
		}
	}
	if len(out) > siteExpMaxHistory {
		out = out[len(out)-siteExpMaxHistory:]
	}
	return out
}

// siteExpRollback makes an earlier version current again, as a new version
// (versions only grow; the history keeps every step). Version 0 is the seed.
func siteExpRollback(site string, version int) (*siteExperience, error) {
	siteExpMu.Lock()
	defer siteExpMu.Unlock()
	cur := siteExpLoadLocked(site)
	if cur == nil {
		return nil, fmt.Errorf("no experience for %q", site)
	}
	var target *siteExperience
	if version == 0 {
		target = siteExpSeed(site)
	} else {
		for _, h := range siteExpHistoryLocked(site) {
			if h.Version == version {
				h := h
				target = &h
			}
		}
	}
	if target == nil {
		return nil, fmt.Errorf("%s has no version %d", site, version)
	}
	next := *target
	next.Version = cur.Version + 1
	next.Source = "rollback"
	next.Status = siteExpStatusActive
	next.FailStreak = 0
	next.Note = fmt.Sprintf("rolled back to v%d from v%d", version, cur.Version)
	if err := siteExpSaveLocked(&next, true); err != nil {
		return nil, err
	}
	return &next, nil
}

// siteExpApply fills the spec's empty fields from the site's experience. A
// retired record is not used; a suspect one is used once more (its next
// failure retires it).
func siteExpApply(e *siteExperience, spec *browser.PublishSpec) bool {
	if e == nil || e.Status == siteExpStatusRetird {
		return false
	}
	prof := e.Profile
	prof.Apply(spec)
	if e.Profile.LazyUpload && e.VerifiedRuns > 0 {
		spec.LazyUpload = true
	}
	return true
}

// siteExpVerifiedEvidence: evidence types that verify a method.
func siteExpVerifiedEvidence(ev string) bool {
	switch ev {
	case "response", "redirect", "list_api", "list":
		return true
	}
	return false
}

// siteExpAccountCause: a failure the account caused (not the method).
func siteExpAccountCause(rep *browser.PublishReport, err error) (kind, text string) {
	if rep != nil && rep.Notice != "" {
		return "notice", rep.Notice
	}
	msg := ""
	if err != nil {
		msg = strings.ToLower(err.Error())
	}
	switch {
	case strings.Contains(msg, "login_wall") || strings.Contains(msg, "sign-in") || strings.Contains(msg, "login wall") || strings.Contains(msg, "not logged in"):
		return "login", "the site asked to sign in"
	case strings.Contains(msg, "captcha"):
		return "captcha", "the site showed a captcha"
	}
	return "", ""
}

// siteExpLearned: the profile fields a verified run proved, from its spec.
func siteExpLearned(base browser.SiteProfile, spec browser.PublishSpec) browser.SiteProfile {
	p := base
	set := func(dst *string, v string) {
		if strings.TrimSpace(v) != "" {
			*dst = v
		}
	}
	set(&p.EditorURL, spec.EditorURL)
	set(&p.VerifyURL, spec.VerifyURL)
	set(&p.VerifyAPI, spec.VerifyAPI)
	set(&p.TitleSelector, spec.TitleSelector)
	set(&p.BodySelector, spec.BodySelector)
	set(&p.UploadSelector, spec.UploadSelector)
	set(&p.SuccessURL, spec.SuccessURL)
	if len(spec.SubmitTexts) > 0 {
		p.SubmitTexts = append([]string(nil), spec.SubmitTexts...)
	}
	if len(spec.ConfirmTexts) > 0 {
		p.ConfirmTexts = append([]string(nil), spec.ConfirmTexts...)
	}
	if spec.LazyUpload {
		p.LazyUpload = true
	}
	p.Playbook = nil
	return p
}

func sameMethod(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sameProfile(a, b browser.SiteProfile) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}

// siteExpRecord updates the site's record from one publish run. used says
// whether the record was applied to the spec.
func siteExpRecord(scope agentruntime.Scope, site string, used bool, spec browser.PublishSpec, rep *browser.PublishReport, runErr error) {
	if site == "" || !siteKeyRe.MatchString(site) {
		return
	}
	if kind, text := siteExpAccountCause(rep, runErr); kind != "" && (rep == nil || rep.State != browser.PublishLive) {
		siteExpAddBlock(scope, site, kind, text)
		return
	}
	siteExpMu.Lock()
	defer siteExpMu.Unlock()
	cur := siteExpLoadLocked(site)
	if rep != nil && rep.State == browser.PublishLive && siteExpVerifiedEvidence(rep.Evidence) {
		var base browser.SiteProfile
		if cur != nil {
			base = cur.Profile
		}
		learned := siteExpLearned(base, spec)
		learned.ID = site
		if len(learned.Hosts) == 0 {
			learned.Hosts = []string{site}
		}
		method := map[string]string{}
		for k, v := range rep.Method {
			method[k] = v
		}
		method["evidence"] = rep.Evidence
		now := time.Now().Unix()
		if cur != nil && cur.Status != siteExpStatusRetird && sameProfile(cur.Profile, learned) && sameMethod(cur.Method, method) {
			cur.VerifiedRuns++
			cur.VerifiedAt = now
			cur.FailStreak = 0
			cur.Status = siteExpStatusActive
			_ = siteExpSaveLocked(cur, false)
			return
		}
		next := &siteExperience{Site: site, Version: 1, Source: "learned", VerifiedAt: now, VerifiedRuns: 1, Status: siteExpStatusActive, Profile: learned, Method: method}
		if cur != nil {
			next.Version = cur.Version + 1
			next.Note = fmt.Sprintf("replaces v%d (%s)", cur.Version, cur.Source)
		}
		_ = siteExpSaveLocked(next, true)
		return
	}
	if !used || cur == nil || rep == nil {
		return
	}
	// A failed run on an applied record. Only steps the record pins count:
	// a run that never reached the editor (no_editor) says nothing.
	if rep.State == browser.PublishNoEditor && rep.Notice == "" {
		return
	}
	cur.FailStreak++
	cur.Status = siteExpStatusSuspct
	if cur.FailStreak >= siteExpRetireAfter {
		if cur.Source == "seed" {
			// Seeds are never deleted; a failing seed stays suspect.
			_ = siteExpSaveLocked(cur, false)
			return
		}
		cur.Status = siteExpStatusRetird
		_ = siteExpSaveLocked(cur, true)
		if seed := siteExpSeed(site); seed != nil {
			seed.Version = cur.Version + 1
			seed.Note = fmt.Sprintf("v%d retired after %d failures; back to the seed", cur.Version, cur.FailStreak)
			_ = siteExpSaveLocked(seed, true)
		}
		return
	}
	_ = siteExpSaveLocked(cur, false)
}

// Account blocks: per tenant+user, never global.

func siteExpBlocksPath(scope agentruntime.Scope) string {
	sum := sha256.Sum256([]byte(scope.TenantID + "\x00" + scope.UserID))
	return filepath.Join(siteExpDir(), "account-blocks", hex.EncodeToString(sum[:12])+".json")
}

func siteExpLoadBlocksLocked(scope agentruntime.Scope, now time.Time) []siteAccountBlock {
	var list []siteAccountBlock
	if raw, err := os.ReadFile(siteExpBlocksPath(scope)); err == nil {
		_ = json.Unmarshal(raw, &list)
	}
	out := list[:0]
	for _, b := range list {
		if b.Expire > now.Unix() {
			out = append(out, b)
		}
	}
	return out
}

func siteExpAddBlock(scope agentruntime.Scope, site, kind, text string) {
	siteExpMu.Lock()
	defer siteExpMu.Unlock()
	now := time.Now()
	list := siteExpLoadBlocksLocked(scope, now)
	t := []rune(strings.TrimSpace(text))
	if len(t) > 200 {
		t = t[:200]
	}
	kept := list[:0]
	for _, b := range list {
		if !(b.Site == site && b.Kind == kind) {
			kept = append(kept, b)
		}
	}
	kept = append(kept, siteAccountBlock{Site: site, Kind: kind, Text: string(t), At: now.Unix(), Expire: now.Add(siteExpBlockTTL).Unix()})
	sort.Slice(kept, func(i, j int) bool { return kept[i].At > kept[j].At })
	if len(kept) > siteExpMaxBlocks {
		kept = kept[:siteExpMaxBlocks]
	}
	raw, _ := json.Marshal(kept)
	_ = writeFileAtomic(siteExpBlocksPath(scope), raw)
}

// siteExpBlocks: this account's live blocks on a site.
func siteExpBlocks(scope agentruntime.Scope, site string) []siteAccountBlock {
	siteExpMu.Lock()
	defer siteExpMu.Unlock()
	var out []siteAccountBlock
	for _, b := range siteExpLoadBlocksLocked(scope, time.Now()) {
		if b.Site == site {
			out = append(out, b)
		}
	}
	return out
}

// siteExpClearBlocks drops an account's blocks on a site (a publish there
// went through, so the block is gone).
func siteExpClearBlocks(scope agentruntime.Scope, site string) {
	siteExpMu.Lock()
	defer siteExpMu.Unlock()
	list := siteExpLoadBlocksLocked(scope, time.Now())
	kept := list[:0]
	for _, b := range list {
		if b.Site != site {
			kept = append(kept, b)
		}
	}
	if len(kept) == len(list) {
		return
	}
	raw, _ := json.Marshal(kept)
	_ = writeFileAtomic(siteExpBlocksPath(scope), raw)
}

// siteExpBlockText tells the model about this account's live blocks.
func siteExpBlockText(blocks []siteAccountBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		parts = append(parts, fmt.Sprintf("%s: %s (seen %s)", b.Kind, b.Text, time.Unix(b.At, 0).Format("01-02 15:04")))
	}
	return "this account was blocked on this site recently: " + strings.Join(parts, "; ") + ". If the same block shows again, tell the person what the site asks for instead of retrying."
}

// siteExpMain: MaClawSrv site-experience show|history|rollback <site> [version]
func siteExpMain(args []string, out io.Writer) int {
	usage := func() int {
		fmt.Fprintln(out, "usage: MaClawSrv site-experience show <site> | history <site> | rollback <site> <version>")
		return 2
	}
	if len(args) < 2 || !siteKeyRe.MatchString(args[1]) {
		return usage()
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	switch args[0] {
	case "show":
		e := siteExpLoad(args[1])
		if e == nil {
			fmt.Fprintln(out, "no experience for", args[1])
			return 1
		}
		_ = enc.Encode(e)
	case "history":
		for _, h := range siteExpHistory(args[1]) {
			fmt.Fprintf(out, "v%d %s %s verified_runs=%d fail_streak=%d updated=%s %s\n", h.Version, h.Source, h.Status, h.VerifiedRuns, h.FailStreak, time.Unix(h.Updated, 0).Format(time.RFC3339), h.Note)
		}
	case "rollback":
		if len(args) != 3 {
			return usage()
		}
		v, err := strconv.Atoi(args[2])
		if err != nil || v < 0 {
			return usage()
		}
		e, err := siteExpRollback(args[1], v)
		if err != nil {
			fmt.Fprintln(out, err)
			return 1
		}
		fmt.Fprintf(out, "%s is now v%d (%s)\n", e.Site, e.Version, e.Note)
	default:
		return usage()
	}
	return 0
}
