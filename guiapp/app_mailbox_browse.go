package guiapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

// mailboxToolBrowser replaces the network dialer in browse tests.
var mailboxToolBrowser mailbox.FolderBrowser

// browseHTMLCap bounds one message's HTML part. The sanitizer on the desktop
// side parses it; a multi-megabyte marketing mail must not slow that parse.
const browseHTMLCap = 512 * 1024

func (a *App) browsePeer() mailbox.FolderBrowser {
	if mailboxToolBrowser != nil {
		return mailboxToolBrowser
	}
	_, reader, _ := a.mailboxIO()
	if peer, ok := reader.(mailbox.FolderBrowser); ok {
		return peer
	}
	return nil
}

func (a *App) mailboxByID(id string) (mailbox.Account, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return mailbox.Account{}, err
	}
	for _, account := range cfg.MailboxAccounts {
		if account.ID == id {
			return account, nil
		}
	}
	return mailbox.Account{}, errors.New("没有这个邮箱")
}

func (a *App) browseInbound(ctx context.Context, id string) (mailbox.Account, string, error) {
	account, err := a.mailboxByID(id)
	if err != nil {
		return mailbox.Account{}, "", err
	}
	if account.Inbound == nil {
		return mailbox.Account{}, "", errors.New(mailbox.MsgNoInbound(account.Name))
	}
	secret, err := a.mailboxSecret(ctx, mailbox.SecretRef(account.ID, mailbox.SideInbound))
	if err != nil {
		return mailbox.Account{}, "", mailboxCallErr(err)
	}
	return account, secret, nil
}

func browseLimit(limit int) int {
	if limit <= 0 || limit > mailbox.ListMaxLimit {
		return 30
	}
	return limit
}

func messageRows(items []mailbox.MessageSummary) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]interface{}{
			"uid":            item.UID,
			"from":           item.From,
			"to":             item.To,
			"subject":        item.Subject,
			"date":           item.Date,
			"seen":           item.Seen,
			"has_attachment": item.HasAttachment,
		})
	}
	return out
}

// MailboxBrowseStart lists folders and the first page in one login.
func (a *App) MailboxBrowseStart(accountID string) (map[string]interface{}, error) {
	account, secret, err := a.browseInbound(context.Background(), accountID)
	if err != nil {
		return nil, err
	}
	peer := a.browsePeer()
	if peer == nil {
		return nil, errors.New(mailbox.MsgConnect)
	}
	page, err := peer.Browse(context.Background(), account, secret, "", 30)
	if err != nil && len(page.Folders) == 0 {
		return nil, mailbox.ClientError(err)
	}
	return map[string]interface{}{
		"folders":  page.Folders,
		"box":      page.Box,
		"messages": messageRows(page.Messages),
		"unseen":   page.Unseen,
		"error":    browseNote(err),
	}, nil
}

func browseNote(err error) string {
	if err == nil {
		return ""
	}
	return mailbox.ClientError(err).Error()
}

// MailboxBrowseFolders lists selectable folders for one saved mailbox.
func (a *App) MailboxBrowseFolders(accountID string) ([]mailbox.Folder, error) {
	account, secret, err := a.browseInbound(context.Background(), accountID)
	if err != nil {
		return nil, err
	}
	peer := a.browsePeer()
	if peer == nil {
		return nil, errors.New(mailbox.MsgConnect)
	}
	folders, err := peer.Folders(context.Background(), account, secret)
	if err != nil {
		return nil, mailbox.ClientError(err)
	}
	return folders, nil
}

// MailboxBrowseMessages lists recent messages in one folder. An empty folder
// is an empty list, not an error.
func (a *App) MailboxBrowseMessages(accountID, box string, limit int) ([]map[string]interface{}, error) {
	account, secret, err := a.browseInbound(context.Background(), accountID)
	if err != nil {
		return nil, err
	}
	peer := a.browsePeer()
	if peer == nil {
		return nil, errors.New(mailbox.MsgConnect)
	}
	items, err := peer.List(context.Background(), account, secret, box, browseLimit(limit))
	if err != nil {
		var class *mailbox.ClassError
		if errors.As(err, &class) && class != nil && class.Class == "empty" {
			return []map[string]interface{}{}, nil
		}
		return nil, mailbox.ClientError(err)
	}
	return messageRows(items), nil
}

// MailboxBrowseRead returns one message for the management dialog: the plain
// text view plus the raw HTML part when the mail has one. The desktop side
// sanitizes the HTML before it ever renders; this function only caps its size.
func (a *App) MailboxBrowseRead(accountID, box string, uid int) (map[string]interface{}, error) {
	if uid <= 0 {
		return nil, errors.New("需要 uid")
	}
	account, secret, err := a.browseInbound(context.Background(), accountID)
	if err != nil {
		return nil, err
	}
	peer := a.browsePeer()
	if peer == nil {
		return nil, errors.New(mailbox.MsgConnect)
	}
	body, err := peer.Read(context.Background(), account, secret, box, uint32(uid), 0)
	if err != nil {
		return nil, mailbox.ClientError(err)
	}
	text, truncated := mailbox.ViewBody(body)
	names := make([]string, 0, len(body.Attachments))
	for _, att := range body.Attachments {
		if att.Name != "" {
			names = append(names, att.Name)
		}
	}
	return map[string]interface{}{
		"uid":         body.UID,
		"from":        body.From,
		"to":          body.To,
		"subject":     body.Subject,
		"date":        body.Date,
		"seen":        body.Seen,
		"body":        text,
		"html":        clipHTML(body.HTML),
		"truncated":   truncated,
		"attachments": names,
	}, nil
}

func clipHTML(raw string) string {
	if len(raw) <= browseHTMLCap {
		return raw
	}
	return raw[:browseHTMLCap]
}

// MailboxBrowseMarkRead marks the given messages read or unread in one folder.
// The organize path is reversible, so no confirmation is needed here; the
// dialog marks a message read when it is opened and lets the user undo it.
func (a *App) MailboxBrowseMarkRead(accountID, box string, uids []uint32, read bool) (map[string]interface{}, error) {
	if len(uids) == 0 {
		return nil, errors.New("需要 uid")
	}
	if len(uids) > mailbox.OrganizeMaxUIDs {
		return nil, fmt.Errorf("一次最多处理 %d 封邮件", mailbox.OrganizeMaxUIDs)
	}
	account, secret, err := a.browseInbound(context.Background(), accountID)
	if err != nil {
		return nil, err
	}
	organizer, ok := a.browsePeer().(mailbox.MailOrganizer)
	if !ok {
		return nil, errors.New(mailbox.MsgConnect)
	}
	action := "mark_read"
	if !read {
		action = "mark_unread"
	}
	updated, err := organizer.Organize(context.Background(), account, secret, mailbox.OrganizeRequest{
		Action: action,
		Box:    box,
		UIDs:   uids,
	})
	if err != nil {
		return nil, mailbox.ClientError(err)
	}
	return map[string]interface{}{"updated": updated}, nil
}

// MailboxBrowseDelete moves the given messages to the trash folder, or expunges
// them when the folder already is the trash. The plan is frozen by a preview
// first, so a folder that changed in between cannot delete different mail.
func (a *App) MailboxBrowseDelete(accountID, box string, uids []uint32) (map[string]interface{}, error) {
	if len(uids) == 0 {
		return nil, errors.New("需要 uid")
	}
	if len(uids) > mailbox.DeleteUIDCap {
		return nil, fmt.Errorf("一次最多删除 %d 封邮件", mailbox.DeleteUIDCap)
	}
	account, secret, err := a.browseInbound(context.Background(), accountID)
	if err != nil {
		return nil, err
	}
	deleter, ok := a.browsePeer().(mailbox.MailDeleter)
	if !ok {
		return nil, errors.New(mailbox.MsgConnect)
	}
	preview, err := deleter.PreviewDelete(context.Background(), account, secret, mailbox.DeleteRequest{
		Scope: mailbox.DeleteScopeFolder,
		Box:   box,
		UIDs:  uids,
	})
	if err != nil {
		return nil, mailbox.ClientError(err)
	}
	if preview.Total <= 0 {
		return nil, errors.New(mailbox.MsgNotFound)
	}
	result, err := deleter.ApplyDelete(context.Background(), account, secret, preview.Plan)
	if err != nil {
		return nil, mailbox.ClientError(err)
	}
	return map[string]interface{}{"moved": result.Moved, "purged": result.Purged}, nil
}
