package guiapp

import (
	"context"
	"errors"

	"github.com/RapidAI/CodeClaw/corelib/mailbox"
)

// mailboxToolBrowser replaces the network dialer in browse tests.
var mailboxToolBrowser mailbox.FolderBrowser

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

// MailboxBrowseRead returns one message as plain text for the management dialog.
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
		"truncated":   truncated,
		"attachments": names,
	}, nil
}
