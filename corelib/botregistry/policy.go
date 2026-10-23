package botregistry

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Action is one thing the main bot can do to another bot.
type Action string

const (
	ActionRead     Action = "read"     // list / status / events
	ActionNotify   Action = "notify"   // a message the bot sees on its next turn
	ActionAssign   Action = "assign"   // hand it a task
	ActionStop     Action = "stop"     // cancel its current run
	ActionPause    Action = "pause"    // hold new work
	ActionResume   Action = "resume"   // release held work
	ActionTransfer Action = "transfer" // move a file between bots
	// ActionMessage is a bot-originated report (done, blocked, question,
	// file…) to the main bot or a peer. Any bot may send one; the Bus
	// rate-limits it and stops ping-pong. It never controls the target.
	ActionMessage Action = "message"
)

// Decision is the outcome of a policy check.
type Decision struct {
	Allowed bool
	// NeedsConfirm means the person must say yes first; the tool returns a
	// question instead of acting until it is called again with confirm=true.
	NeedsConfirm bool
	Reason       string
}

var destructiveWord = regexp.MustCompile(`(?i)(^|[^a-z])(install|uninstall|delete|remove|wipe|format|rm -rf|drop table|pay|purchase|checkout|transfer money)([^a-z]|$)`)

// Destructive reports whether an order reads like something irreversible:
// installing, deleting, paying, publishing on someone's behalf.
func Destructive(text string) bool {
	for _, w := range []string{"安装", "卸载", "删除", "刪除", "付款", "支付", "清空", "格式化", "转账", "代发", "代發"} {
		if strings.Contains(text, w) {
			return true
		}
	}
	return destructiveWord.MatchString(text)
}

// Check decides whether caller may do action to target.
//   - Anyone may read.
//   - A bot may always act on itself.
//   - Only the primary bot may notify, assign, stop, pause, resume or
//     transfer to another bot.
//   - Assign/notify text that looks destructive, and stopping a bot that is
//     waiting on the person, need the person's confirmation.
func Check(caller, target Bot, action Action, text string) Decision {
	if action == ActionRead {
		return Decision{Allowed: true}
	}
	if action == ActionMessage {
		if caller.ID == "" || target.ID == "" || caller.ID == target.ID {
			return Decision{Reason: "a message needs another bot as target"}
		}
		return Decision{Allowed: true}
	}
	if caller.ID == "" {
		return Decision{Reason: "unknown caller"}
	}
	if caller.ID != target.ID && !caller.Primary {
		return Decision{Reason: "only the primary bot can control other bots"}
	}
	switch action {
	case ActionAssign, ActionNotify:
		if Destructive(text) {
			return Decision{Allowed: true, NeedsConfirm: true, Reason: "the order looks irreversible"}
		}
		if action == ActionAssign && target.State == StateOffline {
			return Decision{Reason: "the bot is offline"}
		}
	case ActionStop:
		if target.State == StateWaitingUser {
			return Decision{Allowed: true, NeedsConfirm: true, Reason: "the bot is waiting for the person"}
		}
	case ActionTransfer:
		if strings.TrimSpace(text) != "" && Destructive(text) {
			return Decision{Allowed: true, NeedsConfirm: true, Reason: "the note looks irreversible"}
		}
	}
	return Decision{Allowed: true}
}

// Secret-looking local paths never leave the machine through a transfer.
var secretNames = []string{".env", "id_rsa", "id_ed25519", "id_ecdsa", ".pem", ".key", ".p12", ".pfx", "credentials", ".netrc", ".npmrc", ".pypirc", "secrets", "token"}
var secretDirs = []string{".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker"}

// TransferMax caps one artifact moved between bots.
const TransferMax int64 = 50 << 20

// CheckOutboundPath refuses files that should not be sent to a remote bot.
func CheckOutboundPath(path string, size int64) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || clean == "." {
		return fmt.Errorf("path is required")
	}
	if size > TransferMax {
		return fmt.Errorf("file is larger than %d MB", TransferMax>>20)
	}
	lower := strings.ToLower(filepath.ToSlash(clean))
	for _, dir := range secretDirs {
		if strings.Contains("/"+lower+"/", "/"+dir+"/") {
			return fmt.Errorf("refusing to send a file from %s", dir)
		}
	}
	base := strings.ToLower(filepath.Base(clean))
	for _, name := range secretNames {
		if base == name || strings.HasPrefix(base, name+".") || strings.HasSuffix(base, name) {
			return fmt.Errorf("refusing to send %s: it looks like a secret", filepath.Base(clean))
		}
	}
	return nil
}
