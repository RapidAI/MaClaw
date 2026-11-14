package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func desktopStrList(args map[string]any, key string) []string {
	var out []string
	switch v := args[key].(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		out = v
	case string:
		if strings.TrimSpace(v) != "" {
			out = []string{strings.TrimSpace(v)}
		}
	}
	return out
}

// desktopPublish runs the generic publish pipeline in one call and records
// its outcome in the run's publish record. A run whose record shows the
// submit already went out gets that record back instead of a second post.
func desktopPublish(scope agentruntime.Scope, session *browser.BrowserAgentSession, args map[string]any) (string, error) {
	spec := browser.PublishSpec{
		Files:          desktopUploadFiles(args),
		UploadSelector: desktopArg(args, "upload_selector"),
		UploadTrigger:  desktopArg(args, "upload_trigger"),
		Title:          desktopArg(args, "title"),
		TitleSelector:  desktopArg(args, "title_selector"),
		Body:           desktopArg(args, "body"),
		BodySelector:   desktopArg(args, "body_selector"),
		SubmitTexts:    desktopStrList(args, "submit_texts"),
		ConfirmTexts:   desktopStrList(args, "confirm_texts"),
		VerifyURL:      desktopArg(args, "verify_url"),
		EditURL:        desktopArg(args, "edit_url"),
		VerifyAPI:      desktopArg(args, "verify_api"),
		Tags:           desktopStrList(args, "tags"),
		Category:       desktopArg(args, "category"),
		StepBudget:     30 * time.Second,
		// The pipeline owns the time: it ends with its own report before
		// the hard step limit (ZN-21 was cut off at 300s mid-read-back).
		Deadline: time.Now().Add(desktopStepTimeout("publish") - 30*time.Second),
	}
	if err := desktopPublishApplyStrategy(args, &spec); err != nil {
		return "", err
	}
	ts := desktopTaskSpecFor(scope.RunID)
	// A request naming several sites: the call is for the requested site its
	// editor_url or open page is on, else the next one not done yet.
	if base := desktopTaskSpecBase(scope.RunID); len(base.Sites) > 1 {
		ts = desktopPublishTarget(scope.RunID, base, ts, desktopArg(args, "editor_url"), session.CurrentURL())
		desktopPublishUseSite(scope.RunID, ts.Site)
	}
	spec.EditorURL = desktopArg(args, "editor_url")
	if spec.EditorURL == "" {
		spec.EditorURL = ts.EditorURL
	}
	if spec.VerifyURL == "" {
		spec.VerifyURL = ts.VerifyURL
	}
	if spec.EditURL == "" {
		spec.EditURL = ts.EditURL
	}
	// A new post opens a fresh editor; only this run's own drafts (an
	// earlier call that failed) or a draft the person asked to continue
	// (reuse_draft=true) are written on.
	if rec, ok := desktopPublishRecordOf(scope.RunID); ok {
		spec.OwnDrafts = append([]string(nil), rec.Drafts...)
	}
	spec.ReuseDraft = desktopArgBool(args, "reuse_draft")
	expURL := spec.EditorURL
	if expURL == "" {
		expURL = session.CurrentURL()
	}
	site := siteExpKey(expURL)
	if site == "" && ts.Profile != nil {
		site = ts.Profile.ID
	}
	exp := siteExpLoad(site)
	// The site's verified experience first; the profile fills what it lacks.
	expUsed := siteExpApply(exp, &spec)
	prof := ts.Profile
	if prof == nil {
		prof = browser.ProfileForURL(spec.EditorURL)
	}
	if prof == nil {
		prof = browser.ProfileForURL(session.CurrentURL())
	}
	prof.Apply(&spec)
	// Learned experience has dropped CSDN's post_url, user_cookie and the
	// success_url capture, and a model verify_url of https://blog.csdn.net/
	// replaced the account's article list. The embedded profile wins those
	// back so the public link can be built.
	browser.RestorePublishIdentity(prof, &spec)
	if desktopArgBool(args, "explore") {
		probe, perr := browser.ExplorePublish(session.PublishDriver(), spec)
		if exp != nil {
			probe["experience"] = fmt.Sprintf("v%d %s %s", exp.Version, exp.Source, exp.Status)
		}
		b, _ := json.Marshal(probe)
		return "publish explore (read-only, nothing written): " + string(b), perr
	}
	if spec.Title == "" && spec.Body == "" {
		return "", fmt.Errorf("publish needs title and/or body")
	}
	// A placeholder is not content: "…" or 正文 posted as the article went
	// out as an empty post (run_6fa834e0, CSDN). Refuse before anything is
	// filled.
	if desktopPlaceholderText(spec.Title) || desktopPlaceholderText(spec.Body) {
		return "", fmt.Errorf("publish refused: title or body is a placeholder (%q / %q); pass the full finished title and body text (nothing was filled or posted)", desktopRuneHead(spec.Title, 20), desktopRuneHead(spec.Body, 20))
	}
	if r, ok := desktopPublishRecordOf(scope.RunID); ok && r.posted() {
		text := desktopPublishStepText(scope, r)
		// Already on the site with pictures missing: do not post again,
		// and do not report the repeat as a success.
		if r.State == browser.PublishLiveIncomplete {
			return "", fmt.Errorf("%s", text)
		}
		return text, nil
	}
	if out, done, err := desktopPublishLedgerGuard(scope, session, spec, args); done {
		return out, err
	}
	if strings.TrimSpace(spec.Body) == "" && len(spec.Files) == 0 && spec.TrialPictures <= 0 {
		return "", fmt.Errorf("publish not started: no body and no picture files were passed, so there is nothing to post; prepare the full body and pictures first (nothing was filled or posted)")
	}
	if spec.TrialPictures <= 0 {
		if why := browser.PlaceholderTitleProblem(spec.Title); why != "" {
			return "", fmt.Errorf("publish not started: %s (nothing was filled or posted)", why)
		}
	}
	// The pictures a run planned stay planned: a retry that passes fewer
	// files would publish without them (BILI-cv53625427 dropped all three
	// on its third call and passed read-back).
	if r, ok := desktopPublishRecordOf(scope.RunID); ok && r.PlannedImages > len(spec.Files) {
		return "", fmt.Errorf("publish refused: an earlier publish call in this run planned %d picture(s) and this one passes %d; pass every planned picture file again (nothing was filled or posted)", r.PlannedImages, len(spec.Files))
	}
	spec.MinImages = len(spec.Files)
	if len(spec.Files) > 0 {
		session.Policy.AllowUpload = true
	}
	return desktopPublishExecute(scope, spec, site, expUsed, func(spec browser.PublishSpec) (*browser.PublishReport, error) {
		return browser.RunPublish(session.PublishDriver(), spec)
	})
}

// desktopPublishExecute runs the pipeline as one attempt of its recovery
// goal: the goal is acquired first (refused: nothing is filled), held until
// the pipeline's own end and released with its measured result.
func desktopPublishExecute(scope agentruntime.Scope, spec browser.PublishSpec, site string, expUsed bool, run func(browser.PublishSpec) (*browser.PublishReport, error)) (string, error) {
	recAtt, err := desktopRecoveryStartPublish(scope, &spec)
	if err != nil {
		return "", err
	}
	defer desktopRecoveryAbortPublish(scope, recAtt) // only if never finished (panic)
	rep, err := run(spec)
	return desktopPublishFinish(scope, spec, site, expUsed, rep, err)
}

// desktopPublishFinish records a finished pipeline run: the recovery facts,
// the run's publish record and the site experience. A trial (trial_pictures)
// is a test, not an outcome: it records neither, and never counts as posted.
func desktopPublishFinish(scope agentruntime.Scope, spec browser.PublishSpec, site string, expUsed bool, rep *browser.PublishReport, err error) (string, error) {
	desktopRecoveryFinishPublish(scope, rep, err)
	if spec.TrialPictures > 0 {
		out := ""
		if rep != nil {
			out = rep.String()
		}
		if err != nil {
			return "", fmt.Errorf("%s\n%s\ntrial: nothing was submitted", err.Error(), out)
		}
		return out + "\ntrial: nothing was submitted; the editor holds the trial text and pictures until the real publish refills it", nil
	}
	rec := desktopRecordPublish(scope, spec, rep, err)
	siteExpRecord(scope, site, expUsed, spec, rep, err)
	out := ""
	if rep != nil {
		out = rep.String()
		if rep.State == browser.PublishLive {
			siteExpClearBlocks(scope, site)
		}
	}
	if !rec.posted() {
		if bt := siteExpBlockText(siteExpBlocks(scope, site)); bt != "" {
			out += "\n" + bt
		}
	}
	if rec.State == browser.PublishLiveIncomplete {
		// The post is on the site and pictures are missing. That is a
		// failure the bot and the person must see, not a successful step
		// whose only trace is a log line.
		if err == nil {
			err = fmt.Errorf("published but incomplete")
		}
		if rec.Missing != "" && !strings.Contains(err.Error(), rec.Missing) {
			err = fmt.Errorf("%w: %s", err, rec.Missing)
		}
		return "", fmt.Errorf("%s\n%s\n%s", err.Error(), out, desktopPublishStepText(scope, rec))
	}
	if rec.posted() {
		return out + "\n" + desktopPublishStepText(scope, rec), nil
	}
	if err == nil {
		err = fmt.Errorf("publish ended without a result")
	}
	return "", fmt.Errorf("%s\n%s\n%s", err.Error(), out, desktopPublishStepText(scope, rec))
}

// desktopPublishTarget picks the requested site a publish call is for.
func desktopPublishTarget(runID string, base, cur desktopTaskSpec, editorURL, pageURL string) desktopTaskSpec {
	if p := browser.ProfileForURL(editorURL); p != nil && base.site(p.ID) != nil {
		return base.withSite(base.site(p.ID))
	}
	if p := browser.ProfileForURL(pageURL); p != nil {
		for _, q := range desktopTaskSitesPending(runID) {
			if q.ID == p.ID {
				return base.withSite(q)
			}
		}
	}
	return cur
}

// desktopPublishStepText is the record's text, or — when the record's site
// is settled and the request names sites still to do — the next-site step;
// the record is set aside so the next site starts fresh.
func desktopPublishStepText(scope agentruntime.Scope, r desktopPublishRecord) string {
	if !r.settled() {
		return desktopPublishRecordText(r)
	}
	pend := desktopTaskSitesPending(scope.RunID)
	if len(pend) == 0 {
		return desktopPublishRecordText(r)
	}
	desktopPublishParkCurrent(scope.RunID)
	desktopProgressNextSite(scope)
	return desktopPublishNextSiteText(r, pend)
}

// desktopPostURLFor builds a listed post's public URL from its id through
// the profile of the site the list is on (site data, not code). user fills
// a {user} template; "" leaves those templates unresolved.
func desktopPostURLTemplate(spec browser.PublishSpec) string {
	p := browser.ProfileForURL(spec.VerifyURL)
	if p == nil {
		p = browser.ProfileForURL(spec.EditorURL)
	}
	if p != nil && p.PostURL != "" {
		return p.PostURL
	}
	return spec.PostURL
}

func desktopPostURLFor(spec browser.PublishSpec, id, user string) string {
	return browser.PostURLFrom(desktopPostURLTemplate(spec), id, user)
}

func desktopArgBool(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	}
	return false
}

var desktopPlaceholderWords = map[string]bool{
	"正文": true, "内容": true, "标题": true, "文章": true, "文章内容": true, "正文内容": true,
	"待补充": true, "待填写": true, "占位": true, "略": true, "同上": true, "见上": true, "此处填写正文": true,
	"body": true, "title": true, "content": true, "todo": true, "tbd": true, "placeholder": true, "text": true, "xxx": true,
}

// desktopPlaceholderText: a non-empty value that is only dots, ellipses,
// brackets, or a stand-in word. Empty is not a placeholder (title or body
// alone is allowed).
func desktopPlaceholderText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	core := strings.Trim(s, " \t\r\n.。…·-_*#<>[]()（）【】{}\"'“”‘’《》")
	if core == "" {
		return true
	}
	return desktopPlaceholderWords[strings.ToLower(core)]
}

func desktopRuneHead(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}
