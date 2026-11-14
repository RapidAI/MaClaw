package main

import (
	"context"
	"encoding/base64"
	"path"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// publishDesktopReplyIn attaches this turn's screenshot and documents, then
// any file the reply names under this run's work folder or the desktop that
// was not already delivered and still fits the reply limit.
func publishDesktopReplyIn(ctx context.Context, scope agentruntime.Scope, msg *agentservice.Message, slot *desktopShotSlot) (images, files int) {
	images = attachDesktopShot(msg, slot)
	files = attachDesktopFile(msg, slot)
	if slot != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		ctx = context.WithValue(ctx, desktopShotSlotKey{}, slot)
		files += autoDeliverReplyFiles(ctx, scope, msg)
	}
	if msg != nil {
		msg.Content = correctDesktopPathClaim(msg.Content, desktopSavedPath(slot))
		msg.Content = replaceLeakedMonologue(msg.Content)
		msg.Content = correctDesktopAttachClaim(msg.Content, len(msg.Attachments))
	}
	return images, files
}

// autoDeliverReplyFiles reads files the reply names under the run work
// directory or ~/Desktop and attaches the ones that fit. A path is kept
// only when ContainerPath resolves it under one of those roots.
func autoDeliverReplyFiles(ctx context.Context, scope agentruntime.Scope, msg *agentservice.Message) int {
	if msg == nil {
		return 0
	}
	n := 0
	for _, filePath := range desktopReplyFileCandidates(msg.Content, scope) {
		if replyAlreadyHasFile(msg, path.Base(filePath)) {
			continue
		}
		encoded, err := desktopContainerFile(ctx, scope, "file_bytes", map[string]any{"path": filePath})
		if err != nil {
			continue
		}
		raw, err := desktop.DecodeFileBytes(encoded)
		if err != nil || base64.StdEncoding.EncodedLen(len(raw)) > desktopHubReplyFileMax {
			continue
		}
		if err := noteDesktopFile(ctx, path.Base(filePath), desktopDeliverMIME(path.Base(filePath)), raw); err != nil {
			continue
		}
		n += attachDesktopFile(msg, desktopShotSlotFrom(ctx))
	}
	return n
}

func replyAlreadyHasFile(msg *agentservice.Message, name string) bool {
	name = strings.TrimSpace(name)
	for _, item := range msg.Attachments {
		if item.FileName == name {
			return true
		}
	}
	return false
}

func desktopReplyFileCandidates(text string, scope agentruntime.Scope) []string {
	work := desktopRunWorkDir(scope)
	roots := []string{
		path.Join(desktop.ContainerHome, "Desktop"),
		path.Join(desktop.ContainerHome, "桌面"),
	}
	if work != "" {
		roots = append([]string{work}, roots...)
	}
	var out []string
	seen := map[string]bool{}
	for _, token := range desktopPathTokens(text) {
		resolved, err := desktop.ContainerPath(token)
		if err != nil || seen[resolved] {
			continue
		}
		if !pathUnderAny(resolved, roots) {
			continue
		}
		seen[resolved] = true
		out = append(out, resolved)
	}
	return out
}

func pathUnderAny(filePath string, roots []string) bool {
	for _, root := range roots {
		if root != "" && (filePath == root || strings.HasPrefix(filePath, root+"/")) {
			return true
		}
	}
	return false
}

// desktopPathTokens lists path-shaped tokens. Whether one is a desktop file
// is decided by ContainerPath and the allowed roots, not by the sentence around it.
func desktopPathTokens(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '/' && text[i] != '~' {
			continue
		}
		if i > 0 {
			prev := text[i-1]
			switch prev {
			case ' ', '\n', '\t', '(', '`', '"', '\'':
			default:
				continue
			}
		}
		if text[i] == '~' && (i+1 >= len(text) || text[i+1] != '/') {
			continue
		}
		j := i + 1
		for j < len(text) {
			c := text[j]
			if c <= ' ' || c == '`' || c == '\'' || c == '"' || c == '<' || c == '>' {
				break
			}
			j++
		}
		token := strings.TrimRight(text[i:j], ".,;:!?)]}")
		if token != "" {
			out = append(out, token)
		}
		if j > i {
			i = j - 1
		}
	}
	return out
}
