package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// A body that only names a file is not the article: run_6fa834e0 wrote its
// review to /tmp/wf/body.md with bash and passed a 23-char reference as
// body=, and CSDN refused the near-empty post. publish reads the file
// instead (body_file=, or a body that is only $(cat PATH), @PATH,
// file://PATH or a bare text-file path).
var desktopBodyFileRefRe = regexp.MustCompile(`^(?:\$\(\s*cat\s+['"]?([^'"\s)]+)['"]?\s*\)|` + "`" + `\s*cat\s+['"]?([^'"\s` + "`" + `]+)['"]?\s*` + "`" + `|@([~/][^\s]+)|file://(/[^\s]+)|([~/][^\s]*\.(?:md|markdown|txt|html?)))$`)

// desktopBodyFileRef returns the file a publish body stands for ("" when
// the body is text).
func desktopBodyFileRef(body string) string {
	m := desktopBodyFileRefRe.FindStringSubmatch(strings.TrimSpace(body))
	for _, g := range m[min(1, len(m)):] {
		if g != "" {
			return g
		}
	}
	return ""
}

// desktopResolvePublishBody replaces a file reference with the file's text
// (read from the desktop container) before anything is filled.
func desktopResolvePublishBody(ctx context.Context, scope agentruntime.Scope, args map[string]any) error {
	path := strings.TrimSpace(desktopArg(args, "body_file"))
	if path == "" {
		path = desktopBodyFileRef(desktopArg(args, "body"))
	}
	if path == "" {
		return nil
	}
	content, err := desktopContainerFile(ctx, scope, "file_read", map[string]any{"path": path})
	if err != nil {
		return fmt.Errorf("publish refused: could not read the body file %s: %v; pass body= with the full text or a readable body_file= (nothing was filled or posted)", path, err)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("publish refused: the body file %s is empty (nothing was filled or posted)", path)
	}
	args["body"] = content
	delete(args, "body_file")
	return nil
}
