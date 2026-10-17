package mailbox

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap"
)

func TestClassifyFoldersKeepsSelectableRoles(t *testing.T) {
	got := classifyFolders([]*imap.MailboxInfo{
		{Name: "[Gmail]", Attributes: []string{imap.NoSelectAttr}},
		{Name: "[Gmail]/Spam", Attributes: []string{imap.JunkAttr}},
		{Name: "INBOX"},
		{Name: "Deleted Items", Attributes: []string{imap.TrashAttr}},
		{Name: "Sent", Attributes: []string{imap.SentAttr}},
		{Name: "Notes"},
	})
	names := make([]string, 0, len(got))
	for _, folder := range got {
		names = append(names, folder.Name+":"+folder.Special)
	}
	want := "INBOX:inbox, Sent:sent, [Gmail]/Spam:junk, Deleted Items:trash, Notes:"
	if strings.Join(names, ", ") != want {
		t.Fatalf("folders = %s", strings.Join(names, ", "))
	}
}

func TestPreferredFolderFallsBackToInbox(t *testing.T) {
	folders := []Folder{{Name: "INBOX", Special: "inbox"}, {Name: "Junk", Special: "junk"}}
	if got := preferredFolder(folders, "missing"); got != "INBOX" {
		t.Fatalf("fallback=%s", got)
	}
	if got := preferredFolder(folders, "Junk"); got != "Junk" {
		t.Fatalf("named=%s", got)
	}
}

func TestClientErrorNamesALoginFailure(t *testing.T) {
	if got := ClientError(ErrAuth); got == nil || got.Error() != MsgAuth {
		t.Fatalf("auth=%v", got)
	}
	if got := ClientError(&ClassError{Class: "empty", Message: MsgEmpty}); got == nil || got.Error() != MsgEmpty {
		t.Fatalf("class=%v", got)
	}
}

func TestViewBodyOmitsTheModelBanner(t *testing.T) {
	text, truncated := ViewBody(Body{HTML: "<p>hello</p><script>steal()</script>"})
	if truncated || strings.Contains(text, MsgUntrusted) || strings.Contains(text, "steal") || !strings.Contains(text, "hello") {
		t.Fatalf("text=%q truncated=%v", text, truncated)
	}
}
