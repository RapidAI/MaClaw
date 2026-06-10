package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	groupHubSyncChunkFlushInterval = 80 * time.Millisecond
	groupHubSyncChunkMaxBytes      = 2048
)

// GroupChatDispatcher routes group chat messages to the local AI agent
// when it is participating as a speaker in a VE group discussion.
// It maintains the active sessions where the local AI dispatcher is enabled.
type GroupChatDispatcher struct {
	app *App
	mu  sync.RWMutex
	// sessions tracks which discussion sessions have local AI enabled
	sessions map[string]*groupExecutorSession
	// cachedMachineID is lazily resolved and cached (immutable during runtime)
	cachedMachineID     string
	cachedMachineIDOnce sync.Once
}

func (d *GroupChatDispatcher) getLocalMachineID() string {
	d.cachedMachineIDOnce.Do(func() {
		if d.app == nil {
			return
		}
		cfg, err := d.app.LoadConfig()
		if err != nil {
			return
		}
		d.cachedMachineID = strings.TrimSpace(groupDiscussionAgentID(cfg))
	})
	return d.cachedMachineID
}

type groupExecutorSession struct {
	SessionID string
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewGroupChatDispatcher creates a new dispatcher.
func NewGroupChatDispatcher(app *App) *GroupChatDispatcher {
	return &GroupChatDispatcher{
		app:      app,
		sessions: make(map[string]*groupExecutorSession),
	}
}

// RegisterSession marks a session as having local AI enabled.
// After this call, incoming messages for this session will be routed to the main agent.
func (d *GroupChatDispatcher) RegisterSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.sessions[sessionID]; exists {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.sessions[sessionID] = &groupExecutorSession{
		SessionID: sessionID,
		ctx:       ctx,
		cancel:    cancel,
	}
	log.Printf("[group-dispatcher] registered session %s for local executor", sessionID)
}

// UnregisterSession removes a session from the dispatcher.
func (d *GroupChatDispatcher) UnregisterSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sess, exists := d.sessions[sessionID]; exists {
		sess.cancel()
		delete(d.sessions, sessionID)
		log.Printf("[group-dispatcher] unregistered session %s", sessionID)
	}
}

// IsRegistered returns true if the session has local AI enabled.
func (d *GroupChatDispatcher) IsRegistered(sessionID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, exists := d.sessions[sessionID]
	return exists
}

// HandleGroupMessage processes an incoming group discussion message for a session
// where local AI is enabled. It routes the message through the main
// IMMessageHandler with full tool access.
// localDispatch indicates the message was dispatched directly from tryLocalExecutorDispatch
// (bypassing Hub), so responses should be emitted directly to the frontend.
func (d *GroupChatDispatcher) HandleGroupMessage(sessionID string, msg a2a.GroupDiscussionMessage, localDispatch bool) {
	if d.app == nil {
		return
	}

	// Skip own messages (only relevant for Hub-routed messages)
	machineID := d.getLocalMachineID()
	if machineID != "" && veGroupParticipantIdentityMatches(msg.FromID, machineID) {
		return
	}

	// Only respond to substantive messages (questions, statements)
	if !shouldExecutorRespond(msg) {
		return
	}

	d.mu.RLock()
	sess, exists := d.sessions[sessionID]
	d.mu.RUnlock()
	if !exists {
		return
	}

	// Check if context is cancelled
	if sess.ctx.Err() != nil {
		return
	}

	// Route to main agent in a goroutine (non-blocking)
	go d.routeToMainAgent(sess, msg, localDispatch)
}

func (d *GroupChatDispatcher) routeToMainAgent(sess *groupExecutorSession, msg a2a.GroupDiscussionMessage, localDispatch bool) {
	var hubSyncCh chan a2a.GroupDiscussionMessage
	var hubSyncDone chan struct{}
	finishHubSync := func() {}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[group-dispatcher] panic in routeToMainAgent for session %s: %v", sess.SessionID, r)
			finishHubSync()
		}
	}()
	if localDispatch {
		hubSyncCh = make(chan a2a.GroupDiscussionMessage, 64)
		hubSyncDone = make(chan struct{})
		go func() {
			defer close(hubSyncDone)
			d.sendQueuedGroupMessages(sess.SessionID, hubSyncCh)
		}()
		var closeOnce sync.Once
		finishHubSync = func() {
			closeOnce.Do(func() {
				close(hubSyncCh)
				<-hubSyncDone
			})
		}
		if targetedInput, ok := d.app.localTargetedGroupMessage(msg); ok {
			hubSyncCh <- targetedInput
		}
	}

	hubClient := d.app.hubClient()
	if hubClient == nil {
		log.Printf("[group-dispatcher] hub client unavailable for session %s", sess.SessionID)
		finishHubSync()
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		log.Printf("[group-dispatcher] IM handler unavailable for session %s", sess.SessionID)
		finishHubSync()
		return
	}

	// Build context with sender info and shared group history for the system prompt.
	senderContext := groupExecutorSenderContext(msg.FromID)
	discussionContext := d.groupExecutorDiscussionContext(sess.SessionID, msg)

	content := msg.Content
	if HasAttachments(msg) {
		content += NewVEMessageHandler(d.app).ProcessMessageAttachmentsForSession(sess.SessionID, msg)
	}

	imMsg := IMUserMessage{
		UserID:   fmt.Sprintf("ve-group-executor:%s", sess.SessionID),
		Platform: "ve_group_executor",
		Text:     senderContext + discussionContext + content,
		Lang:     "zh",
	}

	// Group-executor LLM rounds may produce assistant content before choosing
	// tool calls. Stream deltas to the frontend in real-time so the user sees
	// progressive output instead of a static "思考中..." indicator.

	// hubSyncCh serializes Hub sync messages to avoid goroutine explosion and
	// preserve final response ordering.

	var streamedAny int32
	var onToken llm.TokenCallback
	if localDispatch {
		onToken = func(delta string) {
			if delta == "" {
				return
			}
			atomic.StoreInt32(&streamedAny, 1)
			d.emitStreamToFrontend(sess.SessionID, delta)
		}
	}

	resp := handler.HandleIMMessageWithProgressAndStream(imMsg, nil, onToken, nil, nil)
	if resp != nil {
		chunk := strings.TrimSpace(resp.Text)
		if chunk != "" {
			if localDispatch {
				// Only send the final response as a chunk if streaming didn't already
				// deliver it (e.g. non-streaming fallback or tool-only rounds).
				if atomic.LoadInt32(&streamedAny) == 0 {
					d.emitStreamToFrontend(sess.SessionID, chunk)
				}
				// Queue for ordered Hub sync (single goroutine, preserves order)
				select {
				case hubSyncCh <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk, Content: chunk}:
				default:
					// Channel full - drop chunk for Hub sync (frontend already has it).
					log.Printf("[group-dispatcher] hub sync channel full, dropping chunk for session %s", sess.SessionID)
				}
			} else {
				// Hub-routed message: send final response through Hub.
				d.sendToGroup(sess.SessionID, a2a.GroupDiscussionMessage{
					Kind:    a2a.MessageStreamChunk,
					Content: chunk,
				})
			}
		}
	}

	// Send stream end
	if resp != nil {
		d.forwardAgentResponseFiles(sess.SessionID, resp, localDispatch)
		if localDispatch {
			d.emitStreamToFrontend(sess.SessionID, "")
			// Always preserve stream_end so Hub history can collapse streamed chunks reliably.
			hubSyncCh <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamEnd, Content: ""}
			finishHubSync() // Wait for all Hub syncs to complete
		} else {
			d.sendToGroup(sess.SessionID, a2a.GroupDiscussionMessage{
				Kind:    a2a.MessageStreamEnd,
				Content: "",
			})
		}
	} else if localDispatch {
		// No response — if streaming was in progress, close it so the frontend
		// doesn't hang in streaming state with a blinking cursor.
		if atomic.LoadInt32(&streamedAny) != 0 {
			d.emitStreamToFrontend(sess.SessionID, "")
		}
		// Still need to close the sync channel.
		finishHubSync()
	}
}

func (d *GroupChatDispatcher) forwardAgentResponseFiles(sessionID string, resp *IMAgentResponse, localDispatch bool) {
	if d == nil || d.app == nil || resp == nil {
		return
	}
	paths := append([]string{}, resp.LocalFilePaths...)
	if strings.TrimSpace(resp.LocalFilePath) != "" {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, path := range uniqueVEFilePaths(paths) {
		if localDispatch {
			localMsg, _, localErr := buildLocalVEFileAttachmentMessage(path, "", "")
			if localErr == nil {
				d.emitAttachmentToFrontend(sessionID, localMsg)
			} else {
				log.Printf("[group-dispatcher] failed to prepare local response file %s for session %s: %v", path, sessionID, localErr)
			}
			go d.syncAgentResponseFileToHub(sessionID, path)
			continue
		}
		d.syncAgentResponseFileToHub(sessionID, path)
	}
}

func uniqueVEFilePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}

func (d *GroupChatDispatcher) syncAgentResponseFileToHub(sessionID, path string) {
	if d == nil || d.app == nil {
		return
	}
	msg, err := d.app.buildVEFileAttachmentMessage(sessionID, path, "", "")
	if err != nil {
		log.Printf("[group-dispatcher] failed to prepare response file %s for session %s: %v", path, sessionID, err)
		return
	}
	d.sendToGroup(sessionID, msg)
}

func (d *GroupChatDispatcher) emitAttachmentToFrontend(sessionID string, msg a2a.GroupDiscussionMessage) {
	if d == nil || d.app == nil || d.app.ctx == nil {
		return
	}
	senderID := d.getLocalMachineID()
	if senderID == "" {
		senderID = "local-maclaw"
	}
	payload := map[string]any{
		"session_id":  sessionID,
		"content":     msg.Content,
		"chunk":       msg.Content,
		"sender_name": "本机AI",
		"sender_id":   senderID,
		"from_id":     senderID,
		"attachments": groupDiscussionMessageAttachmentsPayload(msg),
	}
	runtime.EventsEmit(d.app.ctx, "ve:stream_chunk", payload)
}

// emitStreamToFrontend sends stream events directly to the frontend via Wails runtime,
// bypassing Hub network round-trip. Used for local executor dispatch.
func (d *GroupChatDispatcher) emitStreamToFrontend(sessionID, chunk string) {
	if d.app == nil || d.app.ctx == nil {
		return
	}
	senderID := d.getLocalMachineID()
	if senderID == "" {
		senderID = "local-maclaw"
	}
	if chunk == "" {
		// Stream end
		runtime.EventsEmit(d.app.ctx, "ve:stream_end", map[string]any{
			"session_id":  sessionID,
			"content":     "",
			"chunk":       "",
			"sender_name": "本机AI",
			"sender_id":   senderID,
		})
	} else {
		// Stream chunk
		runtime.EventsEmit(d.app.ctx, "ve:stream_chunk", map[string]any{
			"session_id":  sessionID,
			"content":     chunk,
			"chunk":       chunk,
			"sender_name": "本机AI",
			"sender_id":   senderID,
		})
	}
}

func (d *GroupChatDispatcher) sendQueuedGroupMessages(sessionID string, messages <-chan a2a.GroupDiscussionMessage) {
	if d.app == nil {
		for range messages {
		}
		return
	}
	client, cfg, err := d.app.veA2AHubClient()
	if err != nil {
		log.Printf("[group-dispatcher] group discussion client unavailable for session %s: %v", sessionID, err)
		for range messages {
		}
		return
	}
	fromID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	send := func(msg a2a.GroupDiscussionMessage) {
		if strings.TrimSpace(msg.FromID) == "" {
			msg.FromID = fromID
		}
		ctx, cancel := groupDiscussionContext()
		err := client.SendDiscussionMessage(ctx, sessionID, msg)
		cancel()
		if err != nil {
			log.Printf("[group-dispatcher] failed to sync queued message to session %s: %v", sessionID, err)
		}
	}

	var pendingChunk a2a.GroupDiscussionMessage
	var pendingContent strings.Builder
	flushPendingChunk := func() {
		if pendingContent.Len() == 0 {
			return
		}
		msg := pendingChunk
		msg.Kind = a2a.MessageStreamChunk
		msg.Content = pendingContent.String()
		send(msg)
		pendingChunk = a2a.GroupDiscussionMessage{}
		pendingContent.Reset()
	}
	timer := time.NewTimer(groupHubSyncChunkFlushInterval)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false
	stopTimer := func() {
		if !timerActive {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerActive = false
	}
	resetTimer := func() {
		if !timer.Stop() && timerActive {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(groupHubSyncChunkFlushInterval)
		timerActive = true
	}
	defer stopTimer()

	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				stopTimer()
				flushPendingChunk()
				return
			}
			if msg.Kind == a2a.MessageStreamChunk {
				if pendingContent.Len() == 0 {
					pendingChunk = msg
					resetTimer()
				}
				pendingContent.WriteString(msg.Content)
				if pendingContent.Len() >= groupHubSyncChunkMaxBytes {
					stopTimer()
					flushPendingChunk()
				}
				continue
			}
			stopTimer()
			flushPendingChunk()
			send(msg)
		case <-timer.C:
			timerActive = false
			flushPendingChunk()
		}
	}
}

func (d *GroupChatDispatcher) sendToGroup(sessionID string, msg a2a.GroupDiscussionMessage) {
	if d.app == nil {
		return
	}
	if err := d.app.sendVEA2AMessage(sessionID, msg); err != nil {
		log.Printf("[group-dispatcher] failed to send message to session %s: %v", sessionID, err)
	}
}

func (d *GroupChatDispatcher) groupExecutorDiscussionContext(sessionID string, current a2a.GroupDiscussionMessage) string {
	if d == nil || d.app == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	client, cfg, err := d.app.veA2AHubClient()
	if err != nil || client == nil {
		localID := d.getLocalMachineID()
		if cached, ok := d.cachedGroupExecutorDiscussionDetail(sessionID); ok {
			return buildGroupExecutorDiscussionContext(cached, current, localID)
		}
		return ""
	}
	localID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if localID == "" {
		localID = d.getLocalMachineID()
	}
	if localID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	detail, err := client.GetConsultationDetailForAgent(ctx, strings.TrimSpace(sessionID), localID)
	cancel()
	if err != nil {
		if cached, ok := d.cachedGroupExecutorDiscussionDetail(sessionID); ok {
			return buildGroupExecutorDiscussionContext(cached, current, localID)
		}
		return ""
	}
	return buildGroupExecutorDiscussionContext(detail, current, localID)
}

func (d *GroupChatDispatcher) cachedGroupExecutorDiscussionDetail(sessionID string) (a2a.HubDiscussionDetail, bool) {
	if d == nil || d.app == nil || strings.TrimSpace(sessionID) == "" {
		return a2a.HubDiscussionDetail{}, false
	}
	store, err := d.app.openGroupDiscussionHistoryStore()
	if err != nil {
		return a2a.HubDiscussionDetail{}, false
	}
	defer store.Close()
	detail, ok, err := store.CachedDetail(context.Background(), strings.TrimSpace(sessionID))
	if err != nil || !ok {
		return a2a.HubDiscussionDetail{}, false
	}
	return detail, true
}

func buildGroupExecutorDiscussionContext(detail a2a.HubDiscussionDetail, current a2a.GroupDiscussionMessage, localID string) string {
	messages := groupExecutorRecentContextMessages(detail)
	if len(messages) == 0 && strings.TrimSpace(detail.Discussion.Topic) == "" && strings.TrimSpace(detail.Discussion.Question) == "" && detail.Session == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("[shared group chat context]\n")
	b.WriteString("You are participating in a multi-person VE group chat. You must use the shared group history below when answering the current message. If the history contains relevant material, do not say you cannot see prior context or have no material.\n")
	if topic := firstNonEmptyGroupString(detail.Discussion.Topic, sessionTopic(detail.Session)); strings.TrimSpace(topic) != "" {
		b.WriteString("Topic: ")
		b.WriteString(strings.TrimSpace(topic))
		b.WriteString("\n")
	}
	if question := firstNonEmptyGroupString(detail.Discussion.Question, sessionGoal(detail.Session)); strings.TrimSpace(question) != "" {
		b.WriteString("Goal: ")
		b.WriteString(strings.TrimSpace(question))
		b.WriteString("\n")
	}
	if participants := groupExecutorParticipantLines(detail, localID); len(participants) > 0 {
		b.WriteString("Participants:\n")
		for _, line := range participants {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if summary := groupExecutorContextSummary(detail); summary != "" {
		b.WriteString("Shared compressed memory:\n")
		b.WriteString(summary)
		b.WriteString("\n")
	}
	if recent := groupExecutorRecentMessageLines(messages, current); len(recent) > 0 {
		b.WriteString("Recent group messages:\n")
		for _, line := range recent {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("[/shared group chat context]\n")
	return b.String()
}

func groupExecutorRecentContextMessages(detail a2a.HubDiscussionDetail) []a2a.Message {
	messages := groupExecutorDiscussionMessages(detail)
	if groupExecutorContextSummary(detail) == "" || detail.Session == nil {
		return messages
	}
	return groupExecutorMessagesAfterSummary(messages, detail.Session.SummaryUpToID)
}

func groupExecutorContextSummary(detail a2a.HubDiscussionDetail) string {
	if detail.Session == nil {
		return ""
	}
	return strings.TrimSpace(detail.Session.ContextSummary)
}

func groupExecutorDiscussionMessages(detail a2a.HubDiscussionDetail) []a2a.Message {
	if len(detail.Messages) > 0 {
		return detail.Messages
	}
	if detail.Session != nil && len(detail.Session.Messages) > 0 {
		return detail.Session.Messages
	}
	return nil
}

func groupExecutorMessagesAfterSummary(messages []a2a.Message, summaryUpToID string) []a2a.Message {
	summaryUpToID = strings.TrimSpace(summaryUpToID)
	if summaryUpToID == "" {
		return messages
	}
	for i, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.ID), summaryUpToID) {
			return messages[i+1:]
		}
	}
	return messages
}

func groupExecutorParticipantLines(detail a2a.HubDiscussionDetail, localID string) []string {
	ids := make([]string, 0)
	roles := map[string]string{}
	if detail.Session != nil {
		for _, participant := range detail.Session.Participants {
			id := strings.TrimSpace(participant.ID)
			if id == "" {
				continue
			}
			ids = append(ids, id)
			roles[groupDiscussionCanonicalIdentityKey(id)] = strings.TrimSpace(participant.RoleCode)
		}
	}
	for _, id := range detail.Discussion.ParticipantIDs {
		ids = append(ids, strings.TrimSpace(id))
	}
	ids = dedupeVEGroupParticipantIDs(ids)
	lines := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		label := id
		if veGroupParticipantIdentityMatches(id, "local-maclaw") {
			label += " (local AI)"
		}
		if role := strings.TrimSpace(roles[groupDiscussionCanonicalIdentityKey(id)]); role != "" {
			label += " [" + role + "]"
		}
		lines = append(lines, label)
	}
	return lines
}

func groupExecutorRecentMessageLines(messages []a2a.Message, current a2a.GroupDiscussionMessage) []string {
	lines := make([]string, 0, len(messages))
	currentID := strings.TrimSpace(current.ID)
	currentIndex := groupExecutorCurrentMessageIndex(messages, current)
	var streamFrom string
	var streamContent strings.Builder
	flushStream := func() {
		content := strings.TrimSpace(streamContent.String())
		if content != "" {
			fromID := strings.TrimSpace(streamFrom)
			if fromID == "" {
				fromID = "unknown"
			}
			lines = append(lines, fmt.Sprintf("[%s] %s", fromID, truncateGroupExecutorContextText(content, 1200)))
		}
		streamFrom = ""
		streamContent.Reset()
	}
	for i, msg := range messages {
		if i == currentIndex {
			continue
		}
		if msg.Kind == a2a.MessageStreamEnd {
			flushStream()
			continue
		}
		if msg.Kind == a2a.MessageHandoff {
			flushStream()
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if currentID != "" && strings.EqualFold(strings.TrimSpace(msg.ID), currentID) {
			continue
		}
		fromID := strings.TrimSpace(msg.FromID)
		if fromID == "" {
			fromID = "unknown"
		}
		if msg.Kind == a2a.MessageStreamChunk {
			chunk := msg.Content
			if chunk == "" {
				continue
			}
			if streamFrom != "" && !veGroupParticipantIdentityMatches(streamFrom, fromID) {
				flushStream()
			}
			if streamFrom == "" {
				streamFrom = fromID
			}
			streamContent.WriteString(chunk)
			continue
		}
		if content == "" || strings.HasPrefix(strings.ToLower(content), "invitation ") {
			continue
		}
		flushStream()
		lines = append(lines, fmt.Sprintf("[%s] %s", fromID, truncateGroupExecutorContextText(content, 1200)))
	}
	flushStream()
	if len(lines) > 14 {
		lines = lines[len(lines)-14:]
	}
	return lines
}

func groupExecutorCurrentMessageIndex(messages []a2a.Message, current a2a.GroupDiscussionMessage) int {
	if strings.TrimSpace(current.ID) != "" {
		return -1
	}
	currentContent := strings.TrimSpace(current.Content)
	if currentContent == "" {
		return -1
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.Content) != currentContent {
			continue
		}
		if current.Kind != "" && msg.Kind != current.Kind {
			continue
		}
		if strings.TrimSpace(current.FromID) != "" && !veGroupParticipantIdentityMatches(msg.FromID, current.FromID) {
			continue
		}
		return i
	}
	return -1
}

func truncateGroupExecutorContextText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}

func sessionTopic(session *a2a.Session) string {
	if session == nil {
		return ""
	}
	return session.Topic
}

func sessionGoal(session *a2a.Session) string {
	if session == nil {
		return ""
	}
	return session.Goal
}

func groupExecutorSenderContext(fromID string) string {
	fromID = strings.TrimSpace(fromID)
	if fromID == "" {
		return ""
	}
	return fmt.Sprintf("[from group participant %s; reply only with group-visible content, no hidden reasoning or meta notes]\n", fromID)
}

// shouldExecutorRespond determines if the executor should respond to this message.
// Only responds to questions and statements (not stream chunks, invitations, etc.)
func shouldExecutorRespond(msg a2a.GroupDiscussionMessage) bool {
	switch msg.Kind {
	case a2a.MessageQuestion, a2a.MessageStatement, "":
		return strings.TrimSpace(msg.Content) != "" || HasAttachments(msg)
	default:
		return false
	}
}
