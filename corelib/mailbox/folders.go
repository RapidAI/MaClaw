package mailbox

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/emersion/go-imap"
	imapclient "github.com/emersion/go-imap/client"
)

// Folder is one selectable IMAP mailbox. Special is a stable role when the
// server advertises it: inbox, drafts, sent, junk, trash, archive, or all.
type Folder struct {
	Name    string `json:"name"`
	Special string `json:"special,omitempty"`
}

// BrowsePage is one signed-in look at a mailbox: the folder list and one page
// of messages. Both come from the same IMAP session. Unseen maps a folder name
// to its unread count; folders the server refuses to STATUS stay absent.
type BrowsePage struct {
	Folders  []Folder
	Box      string
	Messages []MessageSummary
	Unseen   map[string]uint64
}

// FolderBrowser is the read side of the mailbox management dialog.
type FolderBrowser interface {
	Folders(ctx context.Context, account Account, secret string) ([]Folder, error)
	Browse(ctx context.Context, account Account, secret, box string, limit int) (BrowsePage, error)
	List(ctx context.Context, account Account, secret, box string, limit int) ([]MessageSummary, error)
	Read(ctx context.Context, account Account, secret, box string, uid uint32, offset int) (Body, error)
}

// Folders lists the selectable folders on the account. Gmail and Hotmail use
// the same IMAP session as a password mailbox, including a refreshed token.
func (d *NetDialer) Folders(ctx context.Context, account Account, secret string) ([]Folder, error) {
	if isPOP3(account) {
		return []Folder{{Name: "INBOX", Special: "inbox"}}, nil
	}
	c, err := d.imapClient(ctx, account, secret)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	infos, err := listMailboxes(c)
	if err != nil {
		return nil, mapIMAP(err, "list")
	}
	return classifyFolders(infos), nil
}

// Browse lists folders and one page of messages on a single login.
// An empty folder is an empty page, not a failure. box picks the folder;
// an empty box opens the inbox, or the first folder when there is no inbox.
func (d *NetDialer) Browse(ctx context.Context, account Account, secret, box string, limit int) (BrowsePage, error) {
	if isPOP3(account) {
		msgs, err := d.pop3Summaries(ctx, account, secret, box, "", limit)
		if err != nil {
			return BrowsePage{}, err
		}
		return BrowsePage{Folders: []Folder{{Name: "INBOX", Special: "inbox"}}, Box: "INBOX", Messages: msgs}, nil
	}
	c, err := d.imapClient(ctx, account, secret)
	if err != nil {
		return BrowsePage{}, err
	}
	defer c.Logout()
	infos, err := listMailboxes(c)
	if err != nil {
		return BrowsePage{}, mapIMAP(err, "list")
	}
	folders := classifyFolders(infos)
	box = preferredFolder(folders, box)
	page := BrowsePage{Folders: folders, Box: box, Unseen: folderUnseen(c, folders)}
	if box == "" {
		return page, nil
	}
	if limit <= 0 || limit > ListMaxLimit {
		limit = 30
	}
	if _, err := c.Select(box, true); err != nil {
		return page, mapIMAP(err, "select")
	}
	items, _ := summaryFetchItems(false)
	msgs, err := fetchRecent(c, limit, items)
	if err != nil {
		var class *ClassError
		if errors.As(err, &class) && class != nil && class.Class == "empty" {
			return page, nil
		}
		return page, err
	}
	page.Messages = make([]MessageSummary, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		page.Messages = append(page.Messages, summaryFromMessage(msg, ""))
	}
	sort.Slice(page.Messages, func(i, j int) bool { return page.Messages[i].UID > page.Messages[j].UID })
	return page, nil
}

func preferredFolder(folders []Folder, want string) string {
	want = strings.TrimSpace(want)
	if want != "" {
		for _, folder := range folders {
			if folder.Name == want {
				return want
			}
		}
	}
	for _, folder := range folders {
		if folder.Special == "inbox" {
			return folder.Name
		}
	}
	if len(folders) > 0 {
		return folders[0].Name
	}
	return ""
}

// folderUnseen asks STATUS UNSEEN per folder. One command each, and a folder
// that refuses the STATUS (some servers only allow it on selectable mailboxes)
// simply gets no badge.
func folderUnseen(c *imapclient.Client, folders []Folder) map[string]uint64 {
	out := make(map[string]uint64, len(folders))
	for _, folder := range folders {
		status, err := c.Status(folder.Name, []imap.StatusItem{imap.StatusUnseen})
		if err != nil || status == nil || status.Unseen == 0 {
			continue
		}
		out[folder.Name] = uint64(status.Unseen)
	}
	return out
}

func listMailboxes(c *imapclient.Client) ([]*imap.MailboxInfo, error) {
	ch := make(chan *imap.MailboxInfo, 16)
	errCh := make(chan error, 1)
	go func() { errCh <- c.List("", "*", ch) }()
	var out []*imap.MailboxInfo
	for info := range ch {
		out = append(out, info)
	}
	return out, <-errCh
}

func classifyFolders(infos []*imap.MailboxInfo) []Folder {
	out := make([]Folder, 0, len(infos))
	seen := map[string]bool{}
	for _, info := range infos {
		if info == nil {
			continue
		}
		name := strings.TrimSpace(info.Name)
		if name == "" || seen[name] || mailboxAttr(info, imap.NoSelectAttr) {
			continue
		}
		seen[name] = true
		out = append(out, Folder{Name: name, Special: folderSpecial(info)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := folderRank(out[i].Special), folderRank(out[j].Special)
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func folderSpecial(info *imap.MailboxInfo) string {
	if info == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(info.Name), "INBOX") {
		return "inbox"
	}
	switch {
	case mailboxAttr(info, imap.DraftsAttr):
		return "drafts"
	case mailboxAttr(info, imap.SentAttr):
		return "sent"
	case mailboxAttr(info, imap.JunkAttr):
		return "junk"
	case mailboxAttr(info, imap.TrashAttr):
		return "trash"
	case mailboxAttr(info, imap.ArchiveAttr):
		return "archive"
	case mailboxAttr(info, imap.AllAttr):
		return "all"
	default:
		return ""
	}
}

func folderRank(special string) int {
	switch special {
	case "inbox":
		return 0
	case "drafts":
		return 1
	case "sent":
		return 2
	case "junk":
		return 3
	case "trash":
		return 4
	case "archive":
		return 5
	case "all":
		return 6
	default:
		return 7
	}
}

func mailboxAttr(info *imap.MailboxInfo, attr string) bool {
	if info == nil {
		return false
	}
	for _, item := range info.Attributes {
		if strings.EqualFold(item, attr) {
			return true
		}
	}
	return false
}
