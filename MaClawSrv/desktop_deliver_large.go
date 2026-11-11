package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// A file over the chat reply's inline budget (desktop.FileBytesMax, about
// 512 KiB: the reply body Hub reads is sized for one screenshot plus that)
// used to come back as "file is too large", and the model spent rounds
// shrinking it by hand (papers 2026-11-12 06:21, Bot 4: a 600 KB weather
// PDF, gs and pypdf missing, re-rendered again and again). Hub's say route
// takes files up to desktopDeliverLargeMax each, so such a file is fetched
// in pieces that each fit the current file route and sent as its own chat
// message. The model never shrinks a file to get it delivered.
const (
	desktopDeliverLargeMax   = 8 << 20
	desktopDeliverPieceBytes = 400_000
)

var desktopDeliverLargeMarkRe = regexp.MustCompile(`MCLDL (/tmp/maclaw-deliver\.[A-Za-z0-9]+) ([0-9]+)`)

// desktopDeliverLargeSay sends one say body; tests swap it.
var desktopDeliverLargeSay = func(ctx context.Context, body map[string]any) error {
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if desktopRemoteAssign != nil {
		_, err := desktopRemoteAssign(cctx, "/api/v1/desktop-services/say", body)
		return err
	}
	if client := desktopHubClientFromEnv(); client != nil {
		out := map[string]any{}
		return client.post(cctx, "/api/v1/desktop-services/say", body, &out)
	}
	return fmt.Errorf("Hub is not configured on this server")
}

// desktopDeliverLargeBash and desktopDeliverLargeBytes reach the bot's
// computer; tests swap them.
var (
	desktopDeliverLargeBash  = desktopContainerBash
	desktopDeliverLargeBytes = func(ctx context.Context, scope agentruntime.Scope, filePath string) (string, error) {
		return desktopContainerFile(ctx, scope, "file_bytes", map[string]any{"path": filePath})
	}
)

func desktopShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// desktopDeliverTooLargeError names the exact limits and the remedy.
func desktopDeliverTooLargeError(size int) error {
	return fmt.Errorf("desktop document is too large to send: %d bytes; this server's Hub has no large-file store, so a file is sent up to %d bytes (%d MB). Do not shrink or re-render it to get it delivered: tell the person the file's path on the computer and its size in your final reply", size, desktopDeliverLargeMax, desktopDeliverLargeMax>>20)
}

// desktopSayUploadBytes is botFileUploadBytes; tests swap it.
var desktopSayUploadBytes = botFileUploadBytes

// desktopDeliverViaStore is deliverDesktopViaStore; tests swap it.
var desktopDeliverViaStore = deliverDesktopViaStore

// deliverDesktopLarge fetches a file over the inline budget in pieces and
// sends it to the person as its own message.
func deliverDesktopLarge(ctx context.Context, scope agentruntime.Scope, resolved, name string) (string, error) {
	return deliverDesktopLargeAt(ctx, scope, resolved, name, botFileInlineMax())
}

// deliverDesktopLargeAt: a file over inlineMax goes through Hub's bot file
// store (any size up to the store's cap) and the message carries a
// reference; a smaller one is read in pieces and sent inline. When Hub has
// no store yet, files up to desktopDeliverLargeMax still go inline.
func deliverDesktopLargeAt(ctx context.Context, scope agentruntime.Scope, resolved, name string, inlineMax int) (string, error) {
	q := desktopShellQuote(resolved)
	out, err := desktopDeliverLargeBash(ctx, scope, fmt.Sprintf(
		`s=$(stat -c %%s -- %s) || exit 3; if [ "$s" -gt %d ]; then echo "MCLDL_STORE $s"; exit 0; fi; if [ "$s" -gt %d ]; then echo "MCLDL_BIG $s"; exit 0; fi; d=$(mktemp -d /tmp/maclaw-deliver.XXXXXXXX) && split -b %d -d -a 3 -- %s "$d/p." && echo "MCLDL $d $s"`,
		q, inlineMax, desktopDeliverLargeMax, desktopDeliverPieceBytes, q))
	if err != nil {
		return "", fmt.Errorf("desktop file could not be read: %v", err)
	}
	if i := strings.Index(out, "MCLDL_STORE "); i >= 0 {
		size, _ := strconv.Atoi(strings.Fields(out[i+len("MCLDL_STORE "):])[0])
		text, serr := desktopDeliverViaStore(ctx, scope, resolved, name)
		if serr != nil && strings.Contains(serr.Error(), "no bot file store yet") && size <= desktopDeliverLargeMax && inlineMax < desktopDeliverLargeMax {
			return deliverDesktopLargeAt(ctx, scope, resolved, name, desktopDeliverLargeMax)
		}
		return text, serr
	}
	if i := strings.Index(out, "MCLDL_BIG "); i >= 0 {
		size, _ := strconv.Atoi(strings.Fields(out[i+len("MCLDL_BIG "):])[0])
		return "", desktopDeliverTooLargeError(size)
	}
	m := desktopDeliverLargeMarkRe.FindStringSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("desktop file could not be read")
	}
	dir := m[1]
	size, _ := strconv.Atoi(m[2])
	defer func() {
		_, _ = desktopDeliverLargeBash(context.WithoutCancel(ctx), scope, "rm -rf -- "+desktopShellQuote(dir))
	}()
	raw := make([]byte, 0, size)
	for i := 0; len(raw) < size; i++ {
		if i > desktopDeliverLargeMax/desktopDeliverPieceBytes+1 {
			return "", fmt.Errorf("desktop file could not be read")
		}
		encoded, err := desktopDeliverLargeBytes(ctx, scope, fmt.Sprintf("%s/p.%03d", dir, i))
		if err != nil {
			return "", fmt.Errorf("desktop file could not be read: %v", err)
		}
		piece, err := desktop.DecodeFileBytes(encoded)
		if err != nil {
			return "", fmt.Errorf("desktop file could not be read: %v", err)
		}
		raw = append(raw, piece...)
	}
	if len(raw) != size {
		return "", fmt.Errorf("desktop file changed while it was read; deliver it again")
	}
	return desktopSayFile(ctx, scope, name, raw)
}

// desktopHubReplyFileMax is the largest base64 file a run reply carries
// through Hub today (hub botmgmt replyFiles maxFile, 200 000 characters).
// A larger file on the reply was dropped there without a word while the
// bot said it was attached (papers 2026-11-12 06:28, Bot 4: a 365 KB PDF).
// Such a file goes as its own message, which Hub stores up to 8 MB.
const desktopHubReplyFileMax = 200_000

// desktopSayFile sends one file to the person as its own chat message.
func desktopSayFile(ctx context.Context, scope agentruntime.Scope, name string, raw []byte) (string, error) {
	if len(raw) > botFileInlineMax() {
		// Over the inline size: the bytes go to Hub's file store and the
		// message carries a reference the person's app downloads.
		ref, err := desktopSayUploadBytes(ctx, scope, name, desktopDeliverMIME(name), raw)
		if err == nil {
			return desktopSayFileRef(ctx, scope, name, ref, int64(len(raw)))
		}
		if !strings.Contains(err.Error(), "no bot file store yet") || len(raw) > desktopDeliverLargeMax {
			return "", fmt.Errorf("desktop document (%d bytes) could not be sent: %v. Do not shrink it: tell the person its path on the computer in your final reply", len(raw), err)
		}
	}
	tenant := strings.TrimSpace(scope.TenantID)
	owner := desktopOwnerUserID(scope.UserID)
	if inst := strings.TrimSpace(scope.InstanceID); inst == "" || owner == "" {
		return "", fmt.Errorf("desktop document could not be attached: this run has no bot instance to send it from")
	}
	text := fmt.Sprintf("文件：%s（%s）", name, desktopHumanSize(len(raw)))
	body := map[string]any{
		"tenant_id":   tenant,
		"user_id":     owner,
		"instance_id": scope.InstanceID,
		"room":        "",
		"text":        text,
		"key":         desktopMessageKey(scope.InstanceID+"|"+scope.RunID, "deliver-large", name, strconv.Itoa(len(raw))),
		"files": []map[string]string{{
			"name": name, "mime": desktopDeliverMIME(name), "data": base64.StdEncoding.EncodeToString(raw),
		}},
	}
	if err := desktopDeliverLargeSay(ctx, body); err != nil && !strings.Contains(err.Error(), "ALREADY_SAID") && !strings.Contains(err.Error(), "409") {
		return "", fmt.Errorf("desktop document (%d bytes) could not be sent as its own message: %v. Do not shrink it: tell the person its path on the computer in your final reply", len(raw), err)
	}
	return fmt.Sprintf("Delivered %s (%d bytes) as its own chat message, because it is larger than one reply can carry. The person has it now; say in your final reply that the file was sent as a separate message. Do not shrink, re-render or deliver it again.", name, len(raw)), nil
}

func desktopHumanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
