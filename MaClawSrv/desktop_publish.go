package main

import (
	"fmt"
	"net/url"
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
		StepBudget:     30 * time.Second,
	}
	if spec.Title == "" && spec.Body == "" {
		return "", fmt.Errorf("publish needs title and/or body")
	}
	if r, ok := desktopPublishRecordOf(scope.RunID); ok && r.posted() {
		return desktopPublishRecordText(r), nil
	}
	if len(spec.Files) > 0 {
		session.Policy.AllowUpload = true
	}
	rep, err := browser.RunPublish(session.PublishDriver(), spec)
	rec := desktopRecordPublish(scope, spec, rep)
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

// desktopPostURLPatterns is site data: the public post URL for an id the
// publish verify list reports (hosts match by suffix).
var desktopPostURLPatterns = map[string]string{
	"xiaohongshu.com": "https://www.xiaohongshu.com/explore/%s",
}

func desktopPostURLFor(verifyURL, id string) string {
	u, err := url.Parse(verifyURL)
	if err != nil || id == "" {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	for suf, pat := range desktopPostURLPatterns {
		if h == suf || strings.HasSuffix(h, "."+suf) {
			return fmt.Sprintf(pat, id)
		}
	}
	return ""
}
