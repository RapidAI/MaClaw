package main

import (
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
	ts := desktopTaskSpecFor(scope.RunID)
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
	prof := ts.Profile
	if prof == nil {
		prof = browser.ProfileForURL(spec.EditorURL)
	}
	if prof == nil {
		prof = browser.ProfileForURL(session.CurrentURL())
	}
	prof.Apply(&spec)
	if spec.Title == "" && spec.Body == "" {
		return "", fmt.Errorf("publish needs title and/or body")
	}
	if r, ok := desktopPublishRecordOf(scope.RunID); ok && r.posted() {
		return desktopPublishRecordText(r), nil
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
	rep, err := browser.RunPublish(session.PublishDriver(), spec)
	rec := desktopRecordPublish(scope, spec, rep, err)
	out := ""
	if rep != nil {
		out = rep.String()
	}
	if rec.posted() {
		return out + "\n" + desktopPublishRecordText(rec), nil
	}
	if err == nil {
		err = fmt.Errorf("publish ended without a result")
	}
	return "", fmt.Errorf("%s\n%s\n%s", err.Error(), out, desktopPublishRecordText(rec))
}

// desktopPostURLFor builds a listed post's public URL from its id through
// the profile of the site the list is on (site data, not code).
func desktopPostURLFor(spec browser.PublishSpec, id string) string {
	p := browser.ProfileForURL(spec.VerifyURL)
	if p == nil {
		p = browser.ProfileForURL(spec.EditorURL)
	}
	return p.PostURLFor(id)
}
