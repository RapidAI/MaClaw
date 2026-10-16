package guiapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// pendingMCPServerWrite binds one paused unreviewed-MCP write confirmation to
// the exact server it names. It travels inside pendingAskUserState so the
// user's answer cannot be applied to a different provider — a grant is only
// recorded when the question that produced it settles.
type pendingMCPServerWrite struct {
	ServerID   string
	ServerName string
	ToolName   string
	// Nonce is the unforgeable binding between this note and the ask_user
	// card the gate emitted: the card's context carries it, and only a
	// committed question whose context repeats it may consume the note. A
	// tool result that merely echoes the sentinel — file content, remote
	// command output, a vendor payload — cannot name a nonce it never saw.
	Nonce string
}

// mcpServerWriteAwaitingConfirmation is the SelectionExecutionResult reason
// code for an unreviewed external-effect call paused on its per-server grant.
// It is not a failure: the ask_user pause carries the confirmation card.
const mcpServerWriteAwaitingConfirmation = "mcp_server_write_awaiting_confirmation"

// mcpServerWriteAskContext is the sentinel prefix on the AskUserRequest.Context
// the gate emits, followed by the server id the question names.
// commitPendingAskUser only binds a pending write to a pendingAskUserState
// carrying this marker and the matching server id, so a deferred or rewritten
// ask (e.g. the coding workflow confirmation gate, which never commits
// pending state) cannot leave the note to attach to an unrelated later
// question, and a context carrying the marker alone cannot bind a note that
// names a different server.
const mcpServerWriteAskContext = "mcp-server-write-confirm;"

// mcpServerWriteNotes (handler field) stashes the pending write between the
// moment the gate emits its __ASK_USER__ result and the moment
// commitPendingAskUser binds it to durable history — the same hop
// mailboxDraftNotes covers for sends. Keyed by userID; one pending write per
// user at a time.
//
// mcpServerWriteGrants (handler field) records per-user, per-server session
// grants. Process memory only: a session ends with the process or with an
// explicit session reset (clearPerUserSessionStateOpts), and a grant is per
// server — one server's confirmation never authorizes a sibling.
// Key: userID + "\x00" + serverID; value: time.Time the grant was recorded.

func mcpServerWriteGrantKey(userID, serverID string) string {
	return strings.TrimSpace(userID) + "\x00" + strings.TrimSpace(serverID)
}

// noteMCPServerWrite stashes the pending write and returns the nonce the
// caller must stamp into the emitted card's context — the nonce is what
// later binds the committed question to this note.
func (h *IMMessageHandler) noteMCPServerWrite(userID string, write pendingMCPServerWrite) string {
	if h == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(write.ServerID) == "" {
		return ""
	}
	write.Nonce = newConfirmNonce()
	if write.Nonce == "" {
		// No nonce, no bindable note: the emitted card can never carry the
		// binding token, so stash nothing rather than park a dead note.
		return ""
	}
	h.mcpServerWriteNotes.Store(strings.TrimSpace(userID), write)
	return write.Nonce
}

// takeMCPServerWriteNoteFor binds a stashed pending write to the ask_user
// question that is being committed, but only when that question repeats the
// nonce the gate stamped into its card (mcpServerWriteAskContext + nonce).
// A question carrying no marker — or a forged marker naming a different
// nonce — leaves the note in place so the gate's own commit can still find
// it; a matching nonce is unforgeable because the note never leaves the
// process.
func (h *IMMessageHandler) takeMCPServerWriteNoteFor(userID string, askReq *AskUserRequest) *pendingMCPServerWrite {
	if h == nil || askReq == nil {
		return nil
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(askReq.Context), mcpServerWriteAskContext)
	if !ok {
		return nil
	}
	questionNonce, _, _ := strings.Cut(rest, ";")
	if questionNonce == "" {
		return nil
	}
	raw, ok := h.mcpServerWriteNotes.Load(strings.TrimSpace(userID))
	if !ok {
		return nil
	}
	write, ok := raw.(pendingMCPServerWrite)
	if !ok || write.Nonce != questionNonce {
		// The question does not carry this pending write's nonce. Leave the
		// note in place: the gate's own commit may still arrive, and a
		// mismatched question must not consume the authority.
		return nil
	}
	// Consume atomically: a racing commit that already swapped or deleted the
	// note fails the compare, and this question binds nothing.
	if !h.mcpServerWriteNotes.CompareAndDelete(strings.TrimSpace(userID), raw) {
		return nil
	}
	return &write
}

// clearMCPServerWriteState drops both the pending note and every session
// grant for a user whose conversation state is being reset.
func (h *IMMessageHandler) clearMCPServerWriteState(userID string) {
	if h == nil {
		return
	}
	h.mcpServerWriteNotes.Delete(strings.TrimSpace(userID))
	prefix := strings.TrimSpace(userID) + "\x00"
	h.mcpServerWriteGrants.Range(func(key, _ interface{}) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			h.mcpServerWriteGrants.Delete(k)
		}
		return true
	})
}

func (h *IMMessageHandler) mcpServerWriteConfirmed(userID, serverID string) bool {
	if h == nil {
		return false
	}
	_, ok := h.mcpServerWriteGrants.Load(mcpServerWriteGrantKey(userID, serverID))
	return ok
}

func (h *IMMessageHandler) confirmMCPServerWrite(userID, serverID string) {
	if h == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(serverID) == "" {
		return
	}
	h.mcpServerWriteGrants.Store(mcpServerWriteGrantKey(userID, serverID), time.Now())
}

// mcpServerWriteGateForSelection decides whether a selected unreviewed MCP
// binding may run an external-effect call. Read-only dynamic calls never
// reach here (the receipt check upstream filters them); reviewed/trusted
// providers keep their own confirmation path — the gate fires only when the
// resolved contract classifies the binding as TrustTierUnreviewed. The gate
// emits the ask_user marker as the selection result — the shared loop turns
// it into the same interactive pause any ask_user question produces.
func (c *sharedAgentLoopCallbacks) mcpServerWriteGateForSelection(selection tool.PlannedSelection, mcpEntries []agentservice.MCPToolEntry) (tool.SelectionExecutionResult, bool) {
	if c == nil || c.handler == nil || c.semanticSurface == nil {
		return tool.SelectionExecutionResult{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "mcp") {
		return tool.SelectionExecutionResult{}, false
	}
	if !tool.SelectionRequiresExternalReceipt(selection) {
		return tool.SelectionExecutionResult{}, false
	}
	serverID := strings.TrimSpace(selection.Provider.ProviderID)
	toolName := strings.TrimSpace(selection.Provider.ImplementationID)
	if serverID == "" {
		return tool.SelectionExecutionResult{}, false
	}
	// Only an unreviewed contract is gated. A reviewed binding keeps the
	// confirmation machinery its reviewed contract already carries; and a
	// binding with no matching inventory entry fails closed downstream
	// without ever asking the user about a call that cannot execute anyway.
	unreviewed := false
	for _, entry := range mcpEntries {
		if strings.TrimSpace(entry.ServerID) != serverID || strings.TrimSpace(entry.ToolName) != toolName {
			continue
		}
		// Gate on "not approved" rather than == Unreviewed: a contract whose
		// tier is empty or a tier this build does not know must still confirm
		// before an external effect runs. Only a reviewed/approved binding
		// may pass ungated — it carries its own confirmation path.
		unreviewed = entry.Contract.TrustTier != tool.TrustTierApproved
		break
	}
	if !unreviewed {
		return tool.SelectionExecutionResult{}, false
	}
	ownerID := strings.TrimSpace(c.userID)
	if ownerID == "" {
		ownerID = strings.TrimSpace(c.semanticSurface.scope.PrincipalID)
	}
	if ownerID == "" {
		// No confirmable principal: the gate cannot bind a question to
		// anyone, so the unreviewed write must not pass. Failing open here
		// would let an anonymous loop skip the confirmation entirely.
		return tool.SelectionExecutionResult{
			Result:     "[system rejected] unreviewed MCP write confirmation requires a session owner",
			Succeeded:  false,
			ReasonCode: "mcp_server_write_unconfirmable",
		}, true
	}
	if c.handler.mcpServerWriteConfirmed(ownerID, serverID) {
		return tool.SelectionExecutionResult{}, false
	}
	serverLabel := serverID
	// Prefer the server display name the user installed under when the
	// inventory has one; the id stays the question's authoritative subject.
	for _, entry := range mcpServerDisplayNameCandidates(c.handler, serverID) {
		serverLabel = entry
		break
	}
	nonce := c.handler.noteMCPServerWrite(ownerID, pendingMCPServerWrite{
		ServerID:   serverID,
		ServerName: serverLabel,
		ToolName:   toolName,
	})
	ask := &agent.AskUserRequest{
		Question: fmt.Sprintf(
			"MCP 服务器 %q 的工具 %q 将执行对外部系统生效的操作（写入/发送/修改）。是否允许本会话内对 %q 的此类调用？",
			serverLabel, toolName, serverLabel,
		),
		Options:   []string{"确认", "取消"},
		InputType: "confirm",
		Context:   mcpServerWriteAskContext + nonce + "; unreviewed MCP server write confirmation; grant applies to this server only, for the rest of this session",
	}
	return tool.SelectionExecutionResult{
		Result:     agent.AskUserResultMarker(ask),
		Paused:     true,
		ReasonCode: mcpServerWriteAwaitingConfirmation,
	}, true
}

// mcpServerDisplayNameCandidates yields a friendlier server label for the
// confirmation card when the registry knows one.
func mcpServerDisplayNameCandidates(h *IMMessageHandler, serverID string) []string {
	if h == nil {
		return nil
	}
	if registry := h.getMCPRegistry(); registry != nil {
		for _, server := range registry.ListServers() {
			if strings.TrimSpace(server.ID) == serverID && strings.TrimSpace(server.Name) != "" {
				return []string{strings.TrimSpace(server.Name)}
			}
		}
		for _, server := range registry.ListLocalServers() {
			if strings.TrimSpace(server.ID) == serverID && strings.TrimSpace(server.Name) != "" {
				return []string{strings.TrimSpace(server.Name)}
			}
		}
	}
	return nil
}

// finishMCPServerWriteAskUser settles a paused server-write confirmation.
// Approve records the per-server session grant and asks the model to re-issue
// the paused call; cancel denies without granting; anything else leaves the
// note un-granted so the next call asks again (fail closed on ambiguous
// intake — no grant is consumed on malformed input).
func (h *IMMessageHandler) finishMCPServerWriteAskUser(userID, text string, write *pendingMCPServerWrite) (string, bool) {
	if h == nil || write == nil || strings.TrimSpace(write.ServerID) == "" {
		return "", false
	}
	serverLabel := strings.TrimSpace(write.ServerName)
	if serverLabel == "" {
		serverLabel = strings.TrimSpace(write.ServerID)
	}
	if mailbox.IsApproveText(text) {
		h.confirmMCPServerWrite(userID, write.ServerID)
		return fmt.Sprintf(
			"[Context hint] The user explicitly approved external-effect calls through installed MCP server %q for the rest of this session. Re-issue the intended tool call now; the approval applies only to server %q, and the grant does not cover other servers or tools outside it.",
			serverLabel, write.ServerID,
		), true
	}
	if mailbox.IsCancelText(text) {
		return fmt.Sprintf(
			"[Context hint] The user declined external-effect calls through installed MCP server %q. Do not call that server for writes; explain that the operation was declined and offer alternatives that do not write through it.",
			serverLabel,
		), true
	}
	return "", false
}
