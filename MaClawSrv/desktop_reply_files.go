package main

import (
	"context"
	"encoding/base64"
	"errors"
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
		delivered, deliverErr := autoDeliverReplyFiles(ctx, scope, msg)
		files += delivered
		rememberDesktopDeliveryFailure(msg, deliverErr)
	}
	if msg != nil {
		msg.Content = correctDesktopPathClaim(msg.Content, desktopSavedPath(slot))
		msg.Content = replaceLeakedMonologue(msg.Content)
		msg.Content = correctDesktopAttachClaim(msg.Content, len(msg.Attachments))
	}
	return images, files
}

const (
	desktopOutputUnreadable = "DESKTOP_OUTPUT_UNREADABLE"
	desktopOutputTooLarge   = "DESKTOP_OUTPUT_TOO_LARGE"
	desktopOutputOverCount  = "DESKTOP_OUTPUT_OVER_COUNT"
	desktopOutputFailed     = "DESKTOP_OUTPUT_FAILED"
)

// desktopReplyDeliveryError is a declared output that did not reach the person.
// Success is not reported for that file.
type desktopReplyDeliveryError struct {
	Code string
	Path string
	Err  error
}

func (e *desktopReplyDeliveryError) Error() string {
	if e == nil {
		return ""
	}
	msg := ""
	if e.Err != nil {
		msg = e.Err.Error()
	}
	return e.Code + " " + e.Path + ": " + msg
}

func (e *desktopReplyDeliveryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func desktopReplyFailure(code, filePath string, err error) error {
	return &desktopReplyDeliveryError{Code: code, Path: filePath, Err: err}
}

func rememberDesktopDeliveryFailure(msg *agentservice.Message, err error) {
	if msg == nil || err == nil {
		return
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]string{}
	}
	msg.Metadata["desktop_delivery_failures"] = err.Error()
	note := "这些输出没有送达：" + err.Error()
	if !strings.Contains(msg.Content, note) {
		msg.Content = strings.TrimRight(msg.Content, "\n") + "\n" + note
	}
}

// desktopDeliverOutputBeyondReply sends one declared output that does not fit
// the reply's inline budget. Bytes already read are sent as-is. A read the
// desktop refused as too large is fetched through the large-file path.
var desktopDeliverOutputBeyondReply = func(ctx context.Context, scope agentruntime.Scope, resolved, name string, raw []byte) error {
	if len(raw) > 0 {
		_, err := deliverFileOnItsOwn(ctx, scope, name, raw, "")
		return err
	}
	_, err := deliverDesktopLarge(ctx, scope, resolved, name)
	return err
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
// are not already on the reply. At most desktopReplyOutputMaxFiles are
// delivered, and the reply itself holds at most desktopReplyOutputMaxBytes.
// A file past the inline budget goes out as its own message. A file that
// cannot be read, is past the large-file cap, or is past the count is a
// typed failure on the returned error. A path that is only named in the
// reply is not fetched.
func autoDeliverReplyFiles(ctx context.Context, scope agentruntime.Scope, msg *agentservice.Message) (int, error) {
	if msg == nil {
		return 0, nil
	}
	slot := desktopShotSlotFrom(ctx)
	if slot == nil {
		return 0, nil
	}
	slot.mu.Lock()
	paths := append([]string(nil), slot.outputs...)
	slot.mu.Unlock()
	n := 0
	total := 0
	var failures []error
	for _, filePath := range paths {
		name := path.Base(filePath)
		if replyAlreadyHasFile(msg, name) {
			continue
		}
		if n >= desktopReplyOutputMaxFiles {
			failures = append(failures, desktopReplyFailure(desktopOutputOverCount, filePath, errors.New("declared output was not delivered")))
			continue
		}
		encoded, err := desktopContainerFile(ctx, scope, "file_bytes", map[string]any{"path": filePath})
		if err != nil {
			if desktopBytesTooLarge(err) {
				if err = desktopDeliverOutputBeyondReply(ctx, scope, filePath, name, nil); err != nil {
					failures = append(failures, desktopReplyFailure(desktopReplyFailureCode(err), filePath, err))
				} else {
					n++
				}
				continue
			}
			failures = append(failures, desktopReplyFailure(desktopOutputUnreadable, filePath, err))
			continue
		}
		raw, err := desktop.DecodeFileBytes(encoded)
		if err != nil {
			if desktopBytesTooLarge(err) {
				if err = desktopDeliverOutputBeyondReply(ctx, scope, filePath, name, nil); err != nil {
					failures = append(failures, desktopReplyFailure(desktopReplyFailureCode(err), filePath, err))
				} else {
					n++
				}
				continue
			}
			failures = append(failures, desktopReplyFailure(desktopOutputUnreadable, filePath, err))
			continue
		}
		if len(raw) > desktopDeliverLargeMax {
			failures = append(failures, desktopReplyFailure(desktopOutputTooLarge, filePath, desktopDeliverTooLargeError(len(raw))))
			continue
		}
		if len(raw) > desktopReplyOutputMaxBytes-total || base64.StdEncoding.EncodedLen(len(raw)) > desktopHubReplyFileMax {
			if err = desktopDeliverOutputBeyondReply(ctx, scope, filePath, name, raw); err != nil {
				failures = append(failures, desktopReplyFailure(desktopReplyFailureCode(err), filePath, err))
			} else {
				n++
			}
			continue
		}
		if err := noteDesktopFile(ctx, name, desktopDeliverMIME(name), raw); err != nil {
			if desktopBytesTooLarge(err) {
				if err = desktopDeliverOutputBeyondReply(ctx, scope, filePath, name, raw); err != nil {
					failures = append(failures, desktopReplyFailure(desktopReplyFailureCode(err), filePath, err))
				} else {
					n++
				}
				continue
			}
			failures = append(failures, desktopReplyFailure(desktopOutputFailed, filePath, err))
			continue
		}
		total += len(raw)
		n += attachDesktopFile(msg, slot)
	}
	return n, errors.Join(failures...)
}

func desktopBytesTooLarge(err error) bool {
	return err != nil && strings.Contains(err.Error(), "too large")
}

func desktopReplyFailureCode(err error) string {
	if desktopBytesTooLarge(err) {
		return desktopOutputTooLarge
	}
	return desktopOutputFailed
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
