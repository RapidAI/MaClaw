package main

import (
	"fmt"
	"net/url"
	"strings"
	"time"

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

// desktopPublish runs the generic publish pipeline in one call. Each step
// has its own check and budget; a failed step is an error naming it.
func desktopPublish(session *browser.BrowserAgentSession, args map[string]any) (string, error) {
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
	if len(spec.Files) > 0 {
		session.Policy.AllowUpload = true
	}
	rep, err := browser.RunPublish(session.PublishDriver(), spec)
	out := ""
	if rep != nil {
		out = rep.String()
	}
	if err != nil {
		return "", fmt.Errorf("%s\n%s", err.Error(), out)
	}
	if rep.PostID != "" && spec.VerifyURL != "" {
		if link := desktopPostURLFor(spec.VerifyURL, rep.PostID); link != "" {
			return out + "\nPUBLISHED and verified. Reply to the user once with this link: " + link, nil
		}
	}
	if rep.PostID != "" && spec.VerifyURL != "" && strings.TrimRight(rep.ResultURL, "/") == strings.TrimRight(spec.VerifyURL, "/") {
		return out + "\nPUBLISHED and verified: the post is listed on " + rep.ResultURL + " but the list has no direct link. Its id is " + rep.PostID + "; build the post link from the site's post URL pattern (site notes) with that id and reply to the user once with it. Do not publish again.", nil
	}
	if rep.PostID == "" && spec.VerifyURL != "" && strings.TrimRight(rep.ResultURL, "/") == strings.TrimRight(spec.VerifyURL, "/") {
		return out + "\nPUBLISHED and verified: the post is listed on " + rep.ResultURL + ", but that list shows no link or id for it. Reply to the user once now with the title and that list page; do not hunt for the post link (no probes, fetches or guessed ids) and do not publish again.", nil
	}
	return out + "\nPUBLISHED and verified. Reply to the user once with this link: " + rep.ResultURL, nil
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
