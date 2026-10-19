package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
// user at a time. Session reset drops the note. It does not drop a grant.
//
// Granted calls live in mcpServerWriteGrantStore: one record per user and
// server, kept until that server is removed. A new chat and a process
// restart both keep it. Decline writes nothing, so the next call asks again.
// One server's confirmation never authorizes a sibling or another user.

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

// mcpServerWriteGrantRecord is one durable authorization. It names the user
// and the server. It is not a tool grant and carries no arguments.
type mcpServerWriteGrantRecord struct {
	UserID    string    `json:"user_id"`
	ServerID  string    `json:"server_id"`
	GrantedAt time.Time `json:"granted_at"`
}

type mcpServerWriteGrantFile struct {
	Grants []mcpServerWriteGrantRecord `json:"grants"`
}

// mcpServerWriteGrantTable is process-wide so every handler reads the same
// authorization. The file under MaclawDataDir is the record that survives a
// restart. path is the file that produced records. A data-dir change must
// reload; keeping the previous table would authorize the old directory and
// the next save would write that table into the new one. Unregister must not
// call back into the MCP registry: those methods already hold the registry lock.
type mcpServerWriteGrantTable struct {
	mu      sync.Mutex
	loaded  bool
	path    string
	records map[string]time.Time
}

var mcpServerWriteGrantStore = &mcpServerWriteGrantTable{records: map[string]time.Time{}}

func mcpServerWriteGrantPath() string {
	return filepath.Join(corelib.MaclawDataDir(), "mcp_server_write_grants.json")
}

// loadLocked reads the grant file for the current data directory. A missing
// file is an empty store. Any other read error leaves the store unloaded and
// drops whatever table was in memory, so a later save cannot replace a file
// this process did not read and cannot keep authorizing another directory.
// Corrupt JSON is moved aside and the store starts empty; the original bytes
// stay next to that file.
func (s *mcpServerWriteGrantTable) loadLocked() error {
	if s == nil {
		return fmt.Errorf("mcp write grant store is unavailable")
	}
	path := mcpServerWriteGrantPath()
	if s.loaded && s.path == path {
		return nil
	}
	records, err := readMCPServerWriteGrantFile(path)
	if err != nil {
		s.loaded = false
		s.path = ""
		s.records = map[string]time.Time{}
		return err
	}
	s.records = records
	s.path = path
	s.loaded = true
	return nil
}

func readMCPServerWriteGrantFile(path string) (map[string]time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]time.Time{}, nil
		}
		return nil, fmt.Errorf("read write grants: %w", err)
	}
	var file mcpServerWriteGrantFile
	if err := json.Unmarshal(data, &file); err != nil {
		if qerr := quarantineMCPServerWriteGrantFile(path); qerr != nil {
			return nil, fmt.Errorf("write grants unreadable: %v; quarantine: %w", err, qerr)
		}
		log.Printf("[mcp] write grants unreadable; quarantined and started empty: %v", err)
		return map[string]time.Time{}, nil
	}
	records := make(map[string]time.Time, len(file.Grants))
	for _, rec := range file.Grants {
		userID := strings.TrimSpace(rec.UserID)
		serverID := strings.TrimSpace(rec.ServerID)
		if userID == "" || serverID == "" {
			continue
		}
		records[mcpServerWriteGrantKey(userID, serverID)] = rec.GrantedAt.UTC()
	}
	return records, nil
}

// quarantineMCPServerWriteGrantFile renames a file this process cannot parse.
// The next save then creates a new grant file and does not overwrite the
// bytes that failed to parse.
func quarantineMCPServerWriteGrantFile(path string) error {
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	var last error
	for n := 0; n < 8; n++ {
		dest := path + ".unreadable-" + stamp
		if n > 0 {
			dest = fmt.Sprintf("%s-%d", dest, n)
		}
		err := os.Rename(path, dest)
		if err == nil || os.IsNotExist(err) {
			return nil
		}
		last = err
	}
	return last
}

func (s *mcpServerWriteGrantTable) saveLocked() error {
	if s == nil {
		return fmt.Errorf("mcp write grant store is unavailable")
	}
	path := mcpServerWriteGrantPath()
	if !s.loaded || s.path != path {
		return fmt.Errorf("mcp write grants were not loaded")
	}
	if s.records == nil {
		s.records = map[string]time.Time{}
	}
	keys := make([]string, 0, len(s.records))
	for key := range s.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	file := mcpServerWriteGrantFile{Grants: make([]mcpServerWriteGrantRecord, 0, len(keys))}
	for _, key := range keys {
		userID, serverID, ok := strings.Cut(key, "\x00")
		if !ok || strings.TrimSpace(userID) == "" || strings.TrimSpace(serverID) == "" {
			continue
		}
		file.Grants = append(file.Grants, mcpServerWriteGrantRecord{
			UserID:    userID,
			ServerID:  serverID,
			GrantedAt: s.records[key].UTC(),
		})
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return replaceMCPServerWriteGrantFile(path, data)
}

// replaceMCPServerWriteGrantFile publishes a complete temp file by rename.
// os.Rename replaces an existing file on the same volume. A failed rename
// removes only the temp. The shared atomic writer deletes the destination
// before its fallback rename, and that would drop grants already on disk.
func replaceMCPServerWriteGrantFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create grant dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp_mcp_grant_*")
	if err != nil {
		return fmt.Errorf("create grant temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write grant temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync grant temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close grant temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace grant file: %w", err)
	}
	cleanup = false
	return nil
}

// resetMCPServerWriteGrantStore drops the in-memory table so the next read
// loads the file again. Tests use it with a temporary Maclaw base directory.
func resetMCPServerWriteGrantStore() {
	mcpServerWriteGrantStore.mu.Lock()
	defer mcpServerWriteGrantStore.mu.Unlock()
	mcpServerWriteGrantStore.loaded = false
	mcpServerWriteGrantStore.path = ""
	mcpServerWriteGrantStore.records = map[string]time.Time{}
}

// clearMCPServerWriteState drops the pending confirmation for a user whose
// conversation is being reset. The durable server grant stays: a new chat
// must not ask again for a server this user already allowed.
func (h *IMMessageHandler) clearMCPServerWriteState(userID string) {
	if h == nil {
		return
	}
	h.mcpServerWriteNotes.Delete(strings.TrimSpace(userID))
}

func (h *IMMessageHandler) mcpServerWriteConfirmed(userID, serverID string) bool {
	if h == nil {
		return false
	}
	userID = strings.TrimSpace(userID)
	serverID = strings.TrimSpace(serverID)
	if userID == "" || serverID == "" {
		return false
	}
	mcpServerWriteGrantStore.mu.Lock()
	defer mcpServerWriteGrantStore.mu.Unlock()
	if err := mcpServerWriteGrantStore.loadLocked(); err != nil {
		log.Printf("[mcp] write grants unavailable: %v", err)
		return false
	}
	_, ok := mcpServerWriteGrantStore.records[mcpServerWriteGrantKey(userID, serverID)]
	return ok
}

// confirmMCPServerWrite records a new grant only after the file accepts it.
// A grant already loaded from that file stays granted: refreshing its
// timestamp must not turn a failed rewrite into a missing approval.
// A failed read or a failed first save leaves memory matching the last file
// this process successfully loaded.
func (h *IMMessageHandler) confirmMCPServerWrite(userID, serverID string) bool {
	userID = strings.TrimSpace(userID)
	serverID = strings.TrimSpace(serverID)
	if h == nil || userID == "" || serverID == "" {
		return false
	}
	mcpServerWriteGrantStore.mu.Lock()
	defer mcpServerWriteGrantStore.mu.Unlock()
	if err := mcpServerWriteGrantStore.loadLocked(); err != nil {
		log.Printf("[mcp] write grant was not stored: %v", err)
		return false
	}
	if mcpServerWriteGrantStore.records == nil {
		mcpServerWriteGrantStore.records = map[string]time.Time{}
	}
	key := mcpServerWriteGrantKey(userID, serverID)
	if _, had := mcpServerWriteGrantStore.records[key]; had {
		return true
	}
	mcpServerWriteGrantStore.records[key] = time.Now().UTC()
	if err := mcpServerWriteGrantStore.saveLocked(); err != nil {
		delete(mcpServerWriteGrantStore.records, key)
		log.Printf("[mcp] write grant was not stored: %v", err)
		return false
	}
	return true
}

// forgetMCPServerWriteGrants drops every user's authorization for serverID.
// Call it only after that server has been removed. It does not touch the
// registry.
func forgetMCPServerWriteGrants(serverID string) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return
	}
	mcpServerWriteGrantStore.mu.Lock()
	defer mcpServerWriteGrantStore.mu.Unlock()
	if err := mcpServerWriteGrantStore.loadLocked(); err != nil {
		log.Printf("[mcp] write grant was not cleared for server %q: %v", serverID, err)
		return
	}
	next := make(map[string]time.Time, len(mcpServerWriteGrantStore.records))
	removed := false
	for key, grantedAt := range mcpServerWriteGrantStore.records {
		_, id, ok := strings.Cut(key, "\x00")
		if ok && id == serverID {
			removed = true
			continue
		}
		next[key] = grantedAt
	}
	if !removed {
		return
	}
	previous := mcpServerWriteGrantStore.records
	mcpServerWriteGrantStore.records = next
	if err := mcpServerWriteGrantStore.saveLocked(); err != nil {
		mcpServerWriteGrantStore.records = previous
		log.Printf("[mcp] write grant was not cleared for server %q: %v", serverID, err)
	}
}

// pauseUnconfirmedMCPServerWrite returns a pre-admission outcome for an
// external-effect MCP call. A confirmation question and a catalog refusal
// are not executions. Admit consumes the one-shot grant and leaves a running
// execution for a tool that never ran; a later retry of that same grant is
// already consumed. Read-only calls, reviewed bindings, and calls the durable
// server grant already allows return false so admission proceeds.
//
// Inventory is loaded even when that server grant already exists. Incomplete
// coverage is the same non-execution, and skipping the load would admit it.
// The executor loads the inventory again when it actually dispatches; that
// second load is the binding revalidation.
func (c *sharedAgentLoopCallbacks) pauseUnconfirmedMCPServerWrite(selection tool.PlannedSelection) (tool.SelectionExecutionResult, bool) {
	if c == nil || c.handler == nil || c.semanticSurface == nil {
		return tool.SelectionExecutionResult{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "mcp") || !tool.SelectionRequiresExternalReceipt(selection) {
		return tool.SelectionExecutionResult{}, false
	}
	ctx, cancel := c.semanticDynamicExecutionContext()
	defer cancel()
	inventory, err := c.handler.semanticDynamicInventoryForPrincipal(ctx, semanticDesktopContractPrincipal())
	if rejected, ok := semanticDynamicCatalogRejection(inventory, err, "mcp"); ok {
		return rejected, true
	}
	return c.mcpServerWriteGateForSelection(selection, inventory.mcpEntries)
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
			"MCP 服务器 %q 的工具 %q 将执行对外部系统生效的操作（写入、发送或修改）。是否允许此后对 %q 的此类调用？授权会保留到该服务器被移除。",
			serverLabel, toolName, serverLabel,
		),
		Options:   []string{"确认", "取消"},
		InputType: "confirm",
		// The nonce binds this card to the pending note. Nothing after it is
		// user-facing: the question already says how long the grant lasts.
		Context: mcpServerWriteAskContext + nonce + ";",
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
// Approve records the per-user, per-server grant and tells the model to call
// the function listed on this turn. The paused call's name was that turn's
// grant token; the next turn mints a new one and does not accept the old
// token. The grant stays until that server is removed.
// Cancel denies this call without writing a permanent denial. Anything else
// leaves the note un-granted so the next call asks again (fail closed on
// ambiguous intake — no grant is consumed on malformed input).
func (h *IMMessageHandler) finishMCPServerWriteAskUser(userID, text string, write *pendingMCPServerWrite) (string, bool) {
	if h == nil || write == nil || strings.TrimSpace(write.ServerID) == "" {
		return "", false
	}
	serverLabel := strings.TrimSpace(write.ServerName)
	if serverLabel == "" {
		serverLabel = strings.TrimSpace(write.ServerID)
	}
	if mailbox.IsApproveText(text) {
		if !h.confirmMCPServerWrite(userID, write.ServerID) {
			// A failed store must not leave an earlier tool on the continuation.
			h.dropDiscoveredMCPContinuation(userID)
			return fmt.Sprintf(
				"[Context hint] The user approved external-effect calls through installed MCP server %q, but that approval was not stored. Do not call the server yet. Tell the user the approval was not saved and that the next call will ask again.",
				serverLabel,
			), true
		}
		// The durable record only skips the next question. The invocation
		// grant died with the paused loop, so the continuation has to
		// publish this tool again through the discovery bind.
		h.stashDiscoveredMCPContinuation(userID, discoveredMCPBind{ServerID: write.ServerID, ToolName: write.ToolName})
		return mcpServerWriteApprovedHint(serverLabel, write.ServerID, write.ToolName), true
	}
	if mailbox.IsCancelText(text) {
		h.dropDiscoveredMCPContinuation(userID)
		return fmt.Sprintf(
			"[Context hint] The user declined external-effect calls through installed MCP server %q. Do not call that server for writes; explain that the operation was declined and offer alternatives that do not write through it.",
			serverLabel,
		), true
	}
	// Ambiguous intake does not approve. Drop a leftover so this consumed
	// question cannot republish a tool the user did not confirm.
	h.dropDiscoveredMCPContinuation(userID)
	return "", false
}

// mcpServerWriteApprovedHint points at the function this turn will list.
// Unreviewed MCP renders as invoke_<grant>. That token dies with the turn.
// The description prefix is stable, and it is the same sentence the renderer
// puts on the successor. Naming the old function would send the model at a
// token this turn rejects.
func mcpServerWriteApprovedHint(serverLabel, serverID, toolName string) string {
	match := "Call the function listed in this turn whose description names that installed MCP tool."
	if summary := agentservice.InstalledMCPToolPromptSummary(serverID, toolName); summary != "" {
		match = fmt.Sprintf("Call the function listed in this turn whose description starts with %q.", summary)
	}
	return fmt.Sprintf(
		"[Context hint] The user explicitly approved external-effect calls through installed MCP server %q until that server is removed. %s Do not reuse a function name from an earlier turn. The approval applies only to server %q, and the grant does not cover other servers or tools outside it.",
		serverLabel, match, serverID,
	)
}
