package mailbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ExternalEffect is one send or delete that must be claimed before it runs.
// Bind identifies the owner, account, host, target, and content. It must not
// contain a password, a token, or the raw message body.
type ExternalEffect struct {
	Key  string
	Tool string
	Bind string
	Run  func() (string, error)
}

// ExternalEffectRunner claims the effect, runs it at most once, and returns
// the recorded result when the same claim is already settled.
type ExternalEffectRunner interface {
	RunExternalEffect(effect ExternalEffect) (text string, replayed bool, err error)
}

// uncertainEffect marks an error whose external outcome may already have
// happened. The journal keeps the claim and does not run the effect again.
type uncertainEffect struct{ err error }

func (e uncertainEffect) Error() string {
	if e.err == nil {
		return MsgUncertain
	}
	return e.err.Error()
}

func (e uncertainEffect) Unwrap() error { return e.err }

func (uncertainEffect) UncertainExternalEffect() {}

func markUncertain(err error) error {
	if err == nil {
		return uncertainEffect{err: ErrUncertain}
	}
	return uncertainEffect{err: err}
}

// DraftEffectBind names the frozen draft without copying its secret or body.
// A different owner, account, host, recipient, folder, or content hash does
// not match an earlier claim.
func DraftEffectBind(d Draft) string {
	var b strings.Builder
	fmt.Fprintf(&b, "owner=%s\naccount=%s\n", strings.TrimSpace(d.Principal), strings.TrimSpace(d.Account.ID))
	if d.Kind == DraftKindDelete {
		writeEndpoint(&b, d.Account.Inbound)
		fmt.Fprintf(&b, "kind=delete\n%s", deleteEffectIdentity(d.Delete))
		return b.String()
	}
	writeEndpoint(&b, d.Account.Outbound)
	fmt.Fprintf(&b, "kind=send\nfrom=%s\nto=%s\ncc=%s\nbcc=%s\nsubject=%s\n",
		strings.TrimSpace(d.Message.From),
		strings.Join(d.Message.To, ","),
		strings.Join(d.Message.Cc, ","),
		strings.Join(d.Message.Bcc, ","),
		oneLine(d.Message.Subject))
	sum := sha256.Sum256([]byte(d.Message.Text))
	fmt.Fprintf(&b, "body=%s\n", hex.EncodeToString(sum[:]))
	for _, file := range d.Message.Files {
		fileSum := sha256.Sum256(file.Bytes)
		fmt.Fprintf(&b, "file=%s %s\n", file.Name, hex.EncodeToString(fileSum[:]))
	}
	return b.String()
}

func writeEndpoint(b *strings.Builder, ep *Endpoint) {
	if ep == nil {
		fmt.Fprintf(b, "host=\nport=0\nuser=\n")
		return
	}
	fmt.Fprintf(b, "host=%s\nport=%d\nuser=%s\n",
		strings.ToLower(strings.TrimSpace(ep.Host)), ep.Port, strings.TrimSpace(ep.Username))
}

func deleteEffectIdentity(plan DeletePlan) string {
	uids := append([]uint32(nil), plan.UIDs...)
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	folders := make([]string, 0, len(plan.Folders)+len(plan.ExtraTrash))
	for _, folder := range plan.Folders {
		ids := append([]uint32(nil), folder.UIDs...)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		folders = append(folders, fmt.Sprintf("%s#%t#%d#%s", folder.Name, folder.Permanent, folder.UIDValidity, joinUIDs(ids)))
	}
	extra := append([]string(nil), plan.ExtraTrash...)
	sort.Strings(extra)
	sort.Strings(folders)
	return fmt.Sprintf("scope=%s\nbox=%s\ntrash=%s\nall=%t\npurge=%t\nuids=%s\nextra=%s\nfolders=%s\n",
		plan.Scope, plan.Box, plan.TrashBox, plan.All, plan.PurgeTrash, joinUIDs(uids), strings.Join(extra, ","), strings.Join(folders, ","))
}

func joinUIDs(ids []uint32) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return strings.Join(parts, ",")
}

func (h *Host) approveClaimedSend(ctx context.Context, draft Draft, secret string, runner ExternalEffectRunner) (string, string) {
	started := time.Now()
	text, _, err := runner.RunExternalEffect(ExternalEffect{
		Key:  h.StepKey,
		Tool: "email",
		Bind: DraftEffectBind(draft),
		Run: func() (string, error) {
			if err := h.Sender.Send(ctx, draft.Account, secret, draft.Message); err != nil {
				class, _ := classifyTransport(err, false)
				if class == "uncertain" {
					return "", markUncertain(err)
				}
				return "", err
			}
			return "邮件已发送", nil
		},
	})
	if err == nil {
		h.countSend("approved")
		h.audit(draft, "approved")
		h.logResult(draft.Account.ID, SideOutbound, "approve", "approved", started)
		if strings.TrimSpace(text) == "" {
			text = "邮件已发送"
		}
		return text, "approved"
	}
	if kind, ok := heldEffect(err); ok {
		h.noteUncertain(draft)
		h.countSend("uncertain")
		h.audit(draft, "uncertain")
		h.logResult(draft.Account.ID, SideOutbound, "approve", "uncertain", started)
		return heldEffectMessage(draft, kind, err), "uncertain"
	}
	class, msg := classifyTransport(err, false)
	if class == "uncertain" {
		h.noteUncertain(draft)
		h.countSend("uncertain")
		h.audit(draft, "uncertain")
		h.logResult(draft.Account.ID, SideOutbound, "approve", "uncertain", started)
		return msg, "uncertain"
	}
	if class == "rejected" {
		h.countSend("rejected")
		h.audit(draft, "rejected")
		h.logResult(draft.Account.ID, SideOutbound, "approve", "rejected", started)
		return msg, "rejected"
	}
	h.countSend(class)
	h.audit(draft, class)
	h.logResult(draft.Account.ID, SideOutbound, "approve", class, started)
	return msg, class
}

func (h *Host) approveClaimedDelete(ctx context.Context, draft Draft, secret string, deleter MailDeleter, runner ExternalEffectRunner) (string, string, int) {
	var count int
	text, _, err := runner.RunExternalEffect(ExternalEffect{
		Key:  h.StepKey,
		Tool: "mailbox_delete",
		Bind: DraftEffectBind(draft),
		Run: func() (string, error) {
			result, err := deleter.ApplyDelete(ctx, draft.Account, secret, draft.Delete)
			if err != nil {
				class := classOf(err)
				if class == "uncertain" {
					count = deletedAuditCount(draft.Delete, result)
					return "", markUncertain(err)
				}
				return "", err
			}
			count = deletedAuditCount(draft.Delete, result)
			return deleteDoneText(result), nil
		},
	})
	if err == nil {
		h.countSend("approved")
		h.auditDelete(draft, "approved", count)
		return text, "approved", count
	}
	if kind, ok := heldEffect(err); ok {
		h.noteUncertain(draft)
		h.countSend("uncertain")
		h.auditDelete(draft, "uncertain", count)
		return heldEffectMessage(draft, kind, err), "uncertain", count
	}
	class := classOf(err)
	if class == "uncertain" {
		h.noteUncertain(draft)
		h.countSend("uncertain")
		h.auditDelete(draft, "uncertain", count)
		return transportText(err), "uncertain", count
	}
	h.countSend(class)
	h.auditDelete(draft, class, 0)
	return transportText(err), class, 0
}

func heldEffect(err error) (string, bool) {
	var held interface{ ExternalEffectHeld() string }
	if errors.As(err, &held) && held != nil {
		return held.ExternalEffectHeld(), true
	}
	var marked interface{ UncertainExternalEffect() }
	if errors.As(err, &marked) && marked != nil {
		return "uncertain", true
	}
	return "", false
}

func heldEffectMessage(draft Draft, kind string, err error) string {
	if kind == "stale" && err != nil && strings.TrimSpace(err.Error()) != "" {
		return err.Error()
	}
	if draft.Kind == DraftKindDelete {
		return MsgDeleteUncertain
	}
	return MsgUncertain
}
