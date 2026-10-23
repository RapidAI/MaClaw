package main

import (
	"fmt"
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
	if rep.PostID != "" && spec.VerifyURL != "" && strings.TrimRight(rep.ResultURL, "/") == strings.TrimRight(spec.VerifyURL, "/") {
		return out + "\nPUBLISHED and verified: the post is listed on " + rep.ResultURL + " but the list has no direct link. Its id is " + rep.PostID + "; build the post link from the site's post URL pattern (site notes) with that id and reply to the user once with it. Do not publish again.", nil
	}
	return out + "\nPUBLISHED and verified. Reply to the user once with this link: " + rep.ResultURL, nil
}
