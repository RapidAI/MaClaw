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
// files this run declared as outputs. A path mentioned only in the reply
// text is not a file to fetch.
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

const (
	desktopReplyOutputMaxFiles = 8
	desktopReplyOutputMaxBytes = 200_000
)

// noteDesktopOutput records a file this run wrote or edited and declared as
// an output. The reply delivers that record. The reply text does not.
func noteDesktopOutput(ctx context.Context, filePath string) {
	resolved, err := desktop.DeliverableContainerPath(filePath)
	if err != nil {
		return
	}
	slot := desktopShotSlotFrom(ctx)
	if slot == nil {
		return
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	for _, have := range slot.outputs {
		if have == resolved {
			return
		}
	}
	slot.outputs = append(slot.outputs, resolved)
}

// autoDeliverReplyFiles attaches files this run declared as outputs and that
// are not already on the reply. Count and total size are capped. A path that
// is only named in the reply is not fetched.
func autoDeliverReplyFiles(ctx context.Context, scope agentruntime.Scope, msg *agentservice.Message) int {
	if msg == nil {
		return 0
	}
	slot := desktopShotSlotFrom(ctx)
	if slot == nil {
		return 0
	}
	slot.mu.Lock()
	paths := append([]string(nil), slot.outputs...)
	slot.mu.Unlock()
	n := 0
	total := 0
	for _, filePath := range paths {
		if n >= desktopReplyOutputMaxFiles || total >= desktopReplyOutputMaxBytes {
			break
		}
		if replyAlreadyHasFile(msg, path.Base(filePath)) {
			continue
		}
		encoded, err := desktopContainerFile(ctx, scope, "file_bytes", map[string]any{"path": filePath})
		if err != nil {
			continue
		}
		raw, err := desktop.DecodeFileBytes(encoded)
		if err != nil || len(raw) > desktopReplyOutputMaxBytes-total || base64.StdEncoding.EncodedLen(len(raw)) > desktopHubReplyFileMax {
			continue
		}
		if err := noteDesktopFile(ctx, path.Base(filePath), desktopDeliverMIME(path.Base(filePath)), raw); err != nil {
			continue
		}
		total += len(raw)
		n += attachDesktopFile(msg, slot)
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
