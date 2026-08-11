package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/lansengerwatch"
)

// lansengerWatchService is the desktop-side watch engine (jobs + roster + logs).
type lansengerWatchService struct {
	mu     sync.Mutex
	app    *App
	store  *lansengerwatch.Store
	engine *lansengerwatch.Engine
	// jobsCache avoids reading config.json on every IM message.
	jobsCache   []lansengerwatch.Job
	jobsCacheAt time.Time
	// jobsGen bumps on invalidate so a slow ListJobs cannot repopulate stale data.
	jobsGen uint64
	// replyDedupe limits identical group auto-replies (key -> last sent).
	replyDedupe map[string]time.Time
	// rosterCache avoids repeatedly paging through large group directories when
	// the watch panel is reopened or remounted within a short interval.
	rosterCache map[string]lansengerWatchRosterCacheEntry
	// forwardResults retains recent self-forward attempts for the UI diagnostics.
	forwardResults []WatchForwardResult
}

type lansengerWatchRosterCacheEntry struct {
	groupName string
	members   []lansengerwatch.Member
	truncated bool
	expiresAt time.Time
}

// lansengerWatchRosterStoreKey prevents two bots in the same group from
// merging their learned/manual target directory. The default profile retains
// the historic raw group ID so pre-existing roster files remain usable.
func lansengerWatchRosterStoreKey(botProfileID, groupID string) string {
	botProfileID = strings.TrimSpace(botProfileID)
	groupID = strings.TrimSpace(groupID)
	if botProfileID == "" || botProfileID == corelib.DefaultLansengerBotProfileID {
		return groupID
	}
	// Store sanitizes roster names for filesystem use, which can collapse
	// distinct external group IDs (for example "a/b" and "a?b"). Keep the
	// profile readable and append a fixed hash of the full logical identity.
	// The legacy/default path above is deliberately unchanged for migration.
	sum := sha256.Sum256([]byte(botProfileID + "\x00" + groupID))
	return fmt.Sprintf("bot-%s-%x", botProfileID, sum[:12])
}

const (
	// Max time the gateway goroutine may spend in watch Process (CLI+I/O).
	lansengerWatchProcessTimeout = 25 * time.Second
	// Suppress identical group replies within this window.
	lansengerWatchReplyDedupeTTL = 30 * time.Second
	// Group directories can contain thousands of people; cache a successful
	// directory fetch briefly, then refresh on the next natural panel open.
	lansengerWatchRosterCacheTTL = 2 * time.Minute
	// Cap the desktop picker fetch. Above this, the UI still supports manual
	// staff IDs rather than holding an unbounded in-memory directory.
	lansengerWatchMaxRosterMembers = 2000
	// Avoid retaining several full, large-group directories if an operator
	// quickly inspects many groups in one session.
	lansengerWatchMaxCachedRosters = 8
	// How many recent forward attempts to keep for the watch panel.
	lansengerWatchMaxForwardResults = 20
)

// WatchForwardResult is one owner-channel push attempt (success or failure).
type WatchForwardResult struct {
	At           time.Time `json:"at"`
	BotProfileID string    `json:"bot_profile_id,omitempty"`
	JobID        string    `json:"job_id,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	Channel      string    `json:"channel"`
	OK           bool      `json:"ok"`
	Error        string    `json:"error,omitempty"`
	Preview      string    `json:"preview,omitempty"`
}

func normalizeLansengerWatchBotProfileID(botProfileID string) string {
	if botProfileID = strings.TrimSpace(botProfileID); botProfileID != "" {
		return botProfileID
	}
	return corelib.DefaultLansengerBotProfileID
}

// requireLansengerWatchBotProfile verifies the selected bot before a scoped
// watch API can mutate local state. Without this guard, stale UI state or an
// invalid Wails call could leave orphaned jobs/rosters that no running bot owns.
func (a *App) requireLansengerWatchBotProfile(botProfileID string) (string, error) {
	botProfileID = normalizeLansengerWatchBotProfileID(botProfileID)
	if !validLansengerBotProfileID(botProfileID) {
		return "", fmt.Errorf("invalid Lansenger bot id")
	}
	if a == nil {
		return "", fmt.Errorf("Lansenger service unavailable")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	if _, ok := lansengerBotProfileFromConfig(cfg, botProfileID); !ok {
		return "", fmt.Errorf("Lansenger bot %q was not found", botProfileID)
	}
	return botProfileID, nil
}

func (a *App) watchService() *lansengerWatchService {
	if a == nil {
		return nil
	}
	a.lansengerWatchOnce.Do(func() {
		base := a.getMaclawBaseDir()
		store := lansengerwatch.NewStore(base)
		a.lansengerWatch = &lansengerWatchService{
			app:         a,
			store:       store,
			engine:      &lansengerwatch.Engine{Store: store},
			replyDedupe: make(map[string]time.Time),
			rosterCache: make(map[string]lansengerWatchRosterCacheEntry),
		}
	})
	return a.lansengerWatch
}

func (s *lansengerWatchService) cachedRoster(groupID string) (lansengerWatchRosterCacheEntry, bool) {
	if s == nil {
		return lansengerWatchRosterCacheEntry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.rosterCache[strings.TrimSpace(groupID)]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.rosterCache, strings.TrimSpace(groupID))
		return lansengerWatchRosterCacheEntry{}, false
	}
	entry.members = append([]lansengerwatch.Member(nil), entry.members...)
	return entry, true
}

func (s *lansengerWatchService) cacheRoster(groupID, groupName string, members []lansengerwatch.Member, truncated bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rosterCache == nil {
		s.rosterCache = make(map[string]lansengerWatchRosterCacheEntry)
	}
	now := time.Now()
	for id, entry := range s.rosterCache {
		if now.After(entry.expiresAt) {
			delete(s.rosterCache, id)
		}
	}
	groupID = strings.TrimSpace(groupID)
	if _, exists := s.rosterCache[groupID]; !exists && len(s.rosterCache) >= lansengerWatchMaxCachedRosters {
		var oldestID string
		var oldestExpiry time.Time
		for id, entry := range s.rosterCache {
			if oldestID == "" || entry.expiresAt.Before(oldestExpiry) {
				oldestID, oldestExpiry = id, entry.expiresAt
			}
		}
		delete(s.rosterCache, oldestID)
	}
	s.rosterCache[groupID] = lansengerWatchRosterCacheEntry{
		groupName: strings.TrimSpace(groupName),
		members:   append([]lansengerwatch.Member(nil), members...),
		truncated: truncated,
		expiresAt: now.Add(lansengerWatchRosterCacheTTL),
	}
}

func (s *lansengerWatchService) invalidateRosterCache(groupID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.rosterCache, strings.TrimSpace(groupID))
	s.mu.Unlock()
}

// mergeCachedRosterMembers fills gaps from a successful directory cache without
// overwriting newer local observations or manually maintained names.
func mergeCachedRosterMembers(local map[string]lansengerwatch.Member, cached []lansengerwatch.Member) {
	for _, member := range cached {
		id := lansengerwatch.NormalizeStaffID(member.StaffID)
		if id == "" {
			continue
		}
		if _, exists := local[id]; exists {
			continue
		}
		member.StaffID = id
		local[id] = member
	}
}

func isWatchTargetMember(member lansenger.GroupMember) bool {
	return member.FromType == 0 && member.Status != 3 && lansengerwatch.NormalizeStaffID(member.StaffID) != ""
}

// noteCachedRosterMember keeps a warm directory cache consistent with inbound
// traffic. It deliberately does nothing when the group has not been cached:
// live messages must not create a partial directory cache.
func (s *lansengerWatchService) noteCachedRosterMember(groupID, groupName, staffID, name string) {
	if s == nil {
		return
	}
	groupID = strings.TrimSpace(groupID)
	staffID = lansengerwatch.NormalizeStaffID(staffID)
	if groupID == "" || staffID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.rosterCache[groupID]
	if !ok || time.Now().After(entry.expiresAt) {
		return
	}
	for i := range entry.members {
		if entry.members[i].StaffID != staffID {
			continue
		}
		if strings.TrimSpace(name) != "" {
			entry.members[i].Name = strings.TrimSpace(name)
		}
		s.rosterCache[groupID] = entry
		return
	}
	// A truncated directory deliberately represents only the platform's first
	// bounded page range. Do not let inbound traffic grow it without limit.
	if entry.truncated || len(entry.members) >= lansengerWatchMaxRosterMembers {
		return
	}
	entry.members = append(entry.members, lansengerwatch.Member{StaffID: staffID, Name: strings.TrimSpace(name), Source: "message"})
	if strings.TrimSpace(entry.groupName) == "" {
		entry.groupName = strings.TrimSpace(groupName)
	}
	s.rosterCache[groupID] = entry
}

func (s *lansengerWatchService) invalidateCache() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.jobsCache = nil
	s.jobsCacheAt = time.Time{}
	s.jobsGen++
	s.mu.Unlock()
}

// putJobInCache makes an upsert visible to processMessage immediately.
// Warm cache: patch in place (avoids serving a 2s-stale target list).
// Cold cache: only bump jobsGen — next listJobsCached loads disk (already written).
func (s *lansengerWatchService) putJobInCache(job lansengerwatch.Job) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobsGen++
	if s.jobsCache == nil {
		s.jobsCacheAt = time.Time{}
		return
	}
	next := make([]lansengerwatch.Job, len(s.jobsCache))
	copy(next, s.jobsCache)
	found := false
	for i := range next {
		if next[i].ID == job.ID {
			next[i] = job
			found = true
			break
		}
	}
	if !found {
		next = append(next, job)
	}
	s.jobsCache = next
	s.jobsCacheAt = time.Now()
}

// removeJobFromCache drops a job id from the hot-path cache (or just bumps gen if cold).
func (s *lansengerWatchService) removeJobFromCache(jobID string) {
	if s == nil {
		return
	}
	jobID = strings.TrimSpace(jobID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobsGen++
	if s.jobsCache == nil {
		s.jobsCacheAt = time.Time{}
		return
	}
	next := make([]lansengerwatch.Job, 0, len(s.jobsCache))
	for _, j := range s.jobsCache {
		if j.ID != jobID {
			next = append(next, j)
		}
	}
	s.jobsCache = next
	s.jobsCacheAt = time.Now()
}

// copyWatchJobs returns a shallow copy of the jobs slice (Job structs are value-
// copied; nested slices are shared read-only on the hot path).
func copyWatchJobs(jobs []lansengerwatch.Job) []lansengerwatch.Job {
	if jobs == nil {
		return nil
	}
	out := make([]lansengerwatch.Job, len(jobs))
	copy(out, jobs)
	// Jobs created before multi-bot support had no profile identity. Treat them
	// as the migrated default bot at the boundary without rewriting disk merely
	// because configuration was read.
	for i := range out {
		if strings.TrimSpace(out[i].BotProfileID) == "" {
			out[i].BotProfileID = corelib.DefaultLansengerBotProfileID
		}
	}
	return out
}

func (s *lansengerWatchService) listJobsCached() []lansengerwatch.Job {
	if s == nil || s.store == nil {
		return nil
	}
	s.mu.Lock()
	if s.jobsCache != nil && time.Since(s.jobsCacheAt) < 2*time.Second {
		out := copyWatchJobs(s.jobsCache)
		s.mu.Unlock()
		return out
	}
	gen := s.jobsGen
	s.mu.Unlock()

	// Disk I/O outside service lock so invalidate/UI upserts are not stalled.
	// At most one re-read if a writer bumped gen while we were on disk (avoids
	// returning a pre-upsert snapshot that still listed removed 盯人对象).
	jobs, err := s.store.ListJobs()
	for attempt := 0; attempt < 2; attempt++ {
		s.mu.Lock()
		if err != nil {
			log.Printf("[lansenger-watch] list jobs: %v", err)
			out := copyWatchJobs(s.jobsCache)
			s.mu.Unlock()
			return out
		}
		if gen != s.jobsGen {
			// Prefer whatever a concurrent put/remove installed — authoritative.
			if s.jobsCache != nil {
				out := copyWatchJobs(s.jobsCache)
				s.mu.Unlock()
				return out
			}
			if attempt == 0 {
				gen = s.jobsGen
				s.mu.Unlock()
				jobs, err = s.store.ListJobs()
				continue
			}
			// Second pass still cold+racing: install this snapshot and return.
		}
		s.jobsCache = copyWatchJobs(jobs)
		s.jobsCacheAt = time.Now()
		out := copyWatchJobs(s.jobsCache)
		s.mu.Unlock()
		return out
	}
	return nil
}

// processMessage is the IM hot path: roster + record + keyword reply/CLI + private forward.
// It never claims the message for the agent; caller decides routing.
func (s *lansengerWatchService) processMessage(msg lansenger.IncomingMessage) {
	s.processMessageForBot(corelib.DefaultLansengerBotProfileID, nil, msg)
}

func (s *lansengerWatchService) processMessageForBot(botProfileID string, manager *lansengerGatewayManager, msg lansenger.IncomingMessage) {
	if s == nil || s.store == nil || s.engine == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[lansenger-watch] processMessage panic: %v", r)
		}
	}()
	if !isLansengerGroupMessage(msg) {
		return
	}
	groupID := strings.TrimSpace(msg.GroupID)
	speakerID := strings.TrimSpace(msg.FromUserID)
	botProfileID = normalizeLansengerWatchBotProfileID(botProfileID)
	if groupID == "" || speakerID == "" {
		return
	}
	jobs := s.listJobsCached()
	// Fast exit when 盯人 is unused or this group has no enabled job.
	if !lansengerwatch.AnyActiveWatchForBotGroup(jobs, botProfileID, groupID) {
		return
	}
	rosterKey := lansengerWatchRosterStoreKey(botProfileID, groupID)
	// Learn roster only for groups with active watch jobs.
	// NoteMember skips redundant disk writes for frequent chatters.
	if err := s.store.NoteMember(rosterKey, msg.GroupName, speakerID, msg.SenderName, "message"); err != nil {
		log.Printf("[lansenger-watch] roster note: %v", err)
	} else {
		s.noteCachedRosterMember(rosterKey, msg.GroupName, speakerID, msg.SenderName)
	}
	// Include keyword-scope=anyone jobs even when speaker is not a watch target.
	if !lansengerwatch.BotGroupNeedsWatchMessage(jobs, botProfileID, groupID, speakerID) {
		return
	}

	text := strings.TrimSpace(msg.Text)
	// Prefer text without self-@ noise when present.
	if stripped := stripLansengerBotMentions(msg); strings.TrimSpace(stripped) != "" {
		text = strings.TrimSpace(stripped)
	}
	if text == "" {
		return
	}

	// Bound work on the gateway path so a slow CLI cannot stall all IM traffic.
	ctx, cancel := context.WithTimeout(context.Background(), lansengerWatchProcessTimeout)
	defer cancel()
	res := s.engine.ProcessForBot(ctx, jobs, botProfileID, lansengerwatch.Incoming{
		IsGroup:     true,
		GroupID:     groupID,
		GroupName:   msg.GroupName,
		SpeakerID:   speakerID,
		SpeakerName: msg.SenderName,
		Text:        text,
		MessageID:   msg.MessageID,
		ReceivedAt:  time.Now(),
	})
	for _, line := range res.CLILogs {
		log.Printf("[lansenger-watch] %s", line)
	}
	if len(res.KeywordHits) > 0 || len(res.Forwards) > 0 || res.RecordedAll {
		log.Printf("[lansenger-watch] process group=%s speaker=%s jobs=%v keyword_hits=%d replies=%d forwards=%d recorded=%v",
			groupID, speakerID, res.MatchedJobIDs, len(res.KeywordHits), len(res.Replies), len(res.Forwards), res.RecordedAll)
		// Help diagnose "group reply ok but no WeChat/Lansenger package".
		if len(res.KeywordHits) > 0 && len(res.Forwards) == 0 {
			log.Printf("[lansenger-watch] keyword hit but no owner-channel forward (check forward_channels / speech-forward / rule.forward_on_match)")
		}
	}
	// Group replies (static text / CLI stdout) go back to the source group.
	for _, reply := range res.Replies {
		reply = strings.TrimSpace(reply)
		if reply == "" || s.recentGroupReplyForBot(botProfileID, groupID, reply) {
			continue
		}
		if err := s.sendWatchGroupText(manager, msg, reply); err != nil {
			log.Printf("[lansenger-watch] group reply: %v", err)
			continue
		}
		// Only mark after a successful send so a failed delivery can retry.
		s.rememberGroupReplyForBot(botProfileID, groupID, reply)
	}
	// Forwards to owner IM channels — parallel; do not block gateway forever.
	s.deliverForwardsParallel(botProfileID, manager, res.Forwards)
}

// recentGroupReply reports whether the same reply was sent recently (check only).
func (s *lansengerWatchService) recentGroupReply(groupID, reply string) bool {
	return s.recentGroupReplyForBot(corelib.DefaultLansengerBotProfileID, groupID, reply)
}

func (s *lansengerWatchService) recentGroupReplyForBot(botProfileID, groupID, reply string) bool {
	if s == nil {
		return false
	}
	key := strings.TrimSpace(botProfileID) + "\x00" + strings.TrimSpace(groupID) + "\x00" + reply
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replyDedupe == nil {
		return false
	}
	t, ok := s.replyDedupe[key]
	return ok && now.Sub(t) < lansengerWatchReplyDedupeTTL
}

func (s *lansengerWatchService) rememberGroupReply(groupID, reply string) {
	s.rememberGroupReplyForBot(corelib.DefaultLansengerBotProfileID, groupID, reply)
}

func (s *lansengerWatchService) rememberGroupReplyForBot(botProfileID, groupID, reply string) {
	if s == nil {
		return
	}
	key := strings.TrimSpace(botProfileID) + "\x00" + strings.TrimSpace(groupID) + "\x00" + reply
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replyDedupe == nil {
		s.replyDedupe = make(map[string]time.Time)
	}
	if len(s.replyDedupe) > 256 {
		for k, t := range s.replyDedupe {
			if now.Sub(t) > lansengerWatchReplyDedupeTTL {
				delete(s.replyDedupe, k)
			}
		}
	}
	s.replyDedupe[key] = now
}

const lansengerWatchForwardBudget = 8 * time.Second

func (s *lansengerWatchService) deliverForwardsParallel(botProfileID string, manager *lansengerGatewayManager, forwards []lansengerwatch.ForwardRequest) {
	if len(forwards) == 0 {
		return
	}
	// Collapse hub+local duplicates: if any concrete local channel is selected,
	// still send both (user may want hub for other platforms), but drop exact
	// channel+body duplicates across jobs.
	seen := make(map[string]struct{}, len(forwards))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for _, fwd := range forwards {
		body := strings.TrimSpace(fwd.Text)
		ch := lansengerwatch.NormalizeForwardChannel(fwd.Channel)
		if body == "" || ch == "" {
			continue
		}
		key := ch + "\x00" + body
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		// Capture for goroutine (explicit params avoid loop-variable pitfalls).
		jobID, reason, chSend, bodySend := fwd.JobID, fwd.Reason, ch, body
		wg.Add(1)
		go func(jobID, reason, chSend, bodySend string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[lansenger-watch] forward panic channel=%s: %v", chSend, r)
				}
			}()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := s.deliverToOwnerChannelForBot(botProfileID, manager, chSend, bodySend); err != nil {
				log.Printf("[lansenger-watch] forward job=%s reason=%s channel=%s: %v", jobID, reason, chSend, err)
				s.recordForwardResultForBot(botProfileID, WatchForwardResult{
					At: time.Now(), JobID: jobID, Reason: reason, Channel: chSend,
					OK: false, Error: err.Error(), Preview: truncateWatchPreview(bodySend, 80),
				})
			} else {
				log.Printf("[lansenger-watch] forward job=%s reason=%s channel=%s ok", jobID, reason, chSend)
				s.recordForwardResultForBot(botProfileID, WatchForwardResult{
					At: time.Now(), JobID: jobID, Reason: reason, Channel: chSend,
					OK: true, Preview: truncateWatchPreview(bodySend, 80),
				})
			}
		}(jobID, reason, chSend, bodySend)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(lansengerWatchForwardBudget):
		log.Printf("[lansenger-watch] forward delivery budget exceeded (%v), continuing async", lansengerWatchForwardBudget)
	}
}

func (s *lansengerWatchService) sendWatchGroupText(manager *lansengerGatewayManager, msg lansenger.IncomingMessage, text string) error {
	if s == nil || manager == nil {
		return fmt.Errorf("lansenger gateway unavailable")
	}
	manager.mu.Lock()
	gw := manager.gateway
	manager.mu.Unlock()
	if gw == nil {
		return fmt.Errorf("lansenger gateway not running")
	}
	// Keyword / CLI auto-replies should honor the same group-chat decorations
	// (auto-@ / native quote) as agent answers.
	return gw.SendText(context.Background(), buildLansengerOutgoingText(msg, text, manager.currentGroupOpts()))
}

func (s *lansengerWatchService) recordForwardResult(r WatchForwardResult) {
	s.recordForwardResultForBot(corelib.DefaultLansengerBotProfileID, r)
}

func (s *lansengerWatchService) recordForwardResultForBot(botProfileID string, r WatchForwardResult) {
	if s == nil {
		return
	}
	r.BotProfileID = normalizeLansengerWatchBotProfileID(botProfileID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forwardResults = append(s.forwardResults, r)
	if len(s.forwardResults) > lansengerWatchMaxForwardResults {
		s.forwardResults = append([]WatchForwardResult(nil), s.forwardResults[len(s.forwardResults)-lansengerWatchMaxForwardResults:]...)
	}
}

func (s *lansengerWatchService) listForwardResults() []WatchForwardResult {
	return s.listForwardResultsForBot(corelib.DefaultLansengerBotProfileID)
}

func (s *lansengerWatchService) listForwardResultsForBot(botProfileID string) []WatchForwardResult {
	if s == nil {
		return nil
	}
	botProfileID = normalizeLansengerWatchBotProfileID(botProfileID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.forwardResults) == 0 {
		return nil
	}
	// Newest first for the panel.
	out := make([]WatchForwardResult, 0, len(s.forwardResults))
	for i := len(s.forwardResults) - 1; i >= 0; i-- {
		r := s.forwardResults[i]
		// Results written before profile scoping are historical default-bot data.
		if normalizeLansengerWatchBotProfileID(r.BotProfileID) == botProfileID {
			out = append(out, r)
		}
	}
	return out
}

func truncateWatchPreview(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 || s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}

// deliverToOwnerChannel pushes text onto one of the owner's IM pathways.
func (s *lansengerWatchService) deliverToOwnerChannel(channel, text string) error {
	return s.deliverToOwnerChannelForBot(corelib.DefaultLansengerBotProfileID, nil, channel, text)
}

func (s *lansengerWatchService) deliverToOwnerChannelForBot(botProfileID string, manager *lansengerGatewayManager, channel, text string) error {
	if s == nil || s.app == nil {
		return fmt.Errorf("app unavailable")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty text")
	}
	switch channel {
	case lansengerwatch.ChannelWeixin:
		s.app.ensureWeixinGateway()
		if s.app.weixinGateway == nil {
			return fmt.Errorf("微信未启用或网关不可用")
		}
		if !s.app.weixinGateway.HasProactiveSession() {
			return fmt.Errorf("微信无可用私聊会话：请先用微信私聊机器人一次后再试")
		}
		if err := s.app.weixinGateway.SendProactiveText(text); err != nil {
			return fmt.Errorf("微信推送失败: %w", err)
		}
		return nil
	case lansengerwatch.ChannelLansenger:
		if manager == nil {
			s.app.ensureLansengerGateway()
			if s.app.lansengerGateways != nil {
				manager = s.app.lansengerGateways.manager(botProfileID)
			}
			// A custom profile must never silently use the default bot's private
			// peer. Its own unavailable connection should fail visibly instead.
			if manager == nil && normalizeLansengerWatchBotProfileID(botProfileID) == corelib.DefaultLansengerBotProfileID {
				manager = s.app.lansengerGateway
			}
			if manager == nil {
				return fmt.Errorf("蓝信未启用或网关不可用")
			}
		}
		if !manager.HasProactiveSession() {
			return fmt.Errorf("蓝信无可用私聊会话：请先用蓝信私聊机器人一次后再试")
		}
		if err := manager.SendProactiveText(text); err != nil {
			return fmt.Errorf("蓝信推送失败: %w", err)
		}
		return nil
	case lansengerwatch.ChannelQQ:
		// Prefer local QQ bot (config owner openid or last chat); fall back to Hub.
		return s.deliverQQOwnerChannel(text)
	case lansengerwatch.ChannelTelegram:
		// Prefer local Telegram last chat; fall back to Hub.
		return s.deliverTelegramOwnerChannel(text)
	case lansengerwatch.ChannelHub:
		// Explicit Hub channel may spin up the client if configured.
		if err := s.sendHubProactive(text, true); err != nil {
			return fmt.Errorf("hub 推送失败: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("未知通道 %q", channel)
	}
}

// sendHubProactive pushes via Hub. When ensure is true, may start the Hub client
// (used for the dedicated "hub" channel). Fallback paths pass ensure=false so
// 盯人 does not cold-start Hub infrastructure unexpectedly.
func (s *lansengerWatchService) sendHubProactive(text string, ensure bool) error {
	if s == nil || s.app == nil {
		return fmt.Errorf("app unavailable")
	}
	hc := s.app.hubClient()
	if (hc == nil || !hc.IsConnected()) && ensure {
		hc = s.app.ensureHubClient()
	}
	if hc == nil || !hc.IsConnected() {
		return fmt.Errorf("Hub 未连接")
	}
	return hc.SendIMProactiveMessage(text)
}

// deliverLocalOrHub tries local push first (when tryLocal != nil), then Hub
// (only if already connected — does not cold-start Hub).
// noPathHint is returned when there is no local path and Hub is also unavailable.
func (s *lansengerWatchService) deliverLocalOrHub(label, text string, tryLocal func() error, noPathHint string) error {
	var localErr error
	if tryLocal != nil {
		if err := tryLocal(); err == nil {
			return nil
		} else {
			localErr = err
			log.Printf("[lansenger-watch] %s local push failed, trying Hub: %v", label, err)
		}
	}
	if err := s.sendHubProactive(text, false); err != nil {
		if localErr != nil {
			return fmt.Errorf("%s 推送失败: local=%v; hub=%w", label, localErr, err)
		}
		if tryLocal != nil {
			return fmt.Errorf("%s 推送失败: %w", label, err)
		}
		return fmt.Errorf("%s", noPathHint)
	}
	return nil
}

// deliverQQOwnerChannel tries local QQ bot first, then Hub proactive.
func (s *lansengerWatchService) deliverQQOwnerChannel(text string) error {
	s.app.ensureQQBotGateway()
	var tryLocal func() error
	if s.app.qqBotGateway != nil && s.app.qqBotGateway.HasProactiveSession() {
		tryLocal = func() error {
			_, err := s.app.qqBotGateway.SendProactiveText("self", text)
			return err
		}
	}
	hint := "QQ 无可用私聊会话：请在设置中填写 owner openid，或先用 QQ 私聊机器人一次"
	if s.app.qqBotGateway == nil {
		hint = "QQ 未启用本地网关且 Hub 未连接"
	}
	return s.deliverLocalOrHub("QQ", text, tryLocal, hint)
}

// deliverTelegramOwnerChannel tries local last chat first, then Hub proactive.
func (s *lansengerWatchService) deliverTelegramOwnerChannel(text string) error {
	s.app.ensureTelegramGateway()
	var tryLocal func() error
	if s.app.telegramGateway != nil && s.app.telegramGateway.HasProactiveSession() {
		tryLocal = func() error {
			_, err := s.app.telegramGateway.SendProactiveText(0, text)
			return err
		}
	}
	hint := "Telegram 无可用私聊会话：请在设置中填写 owner chat_id，或先给机器人发一条消息"
	if s.app.telegramGateway == nil {
		hint = "Telegram 未启用本地网关且 Hub 未连接"
	}
	return s.deliverLocalOrHub("Telegram", text, tryLocal, hint)
}

// ---------------------------------------------------------------------------
// Wails bindings
// ---------------------------------------------------------------------------

// ListLansengerWatchJobs returns watch jobs JSON.
func (a *App) ListLansengerWatchJobs() (string, error) {
	return a.ListLansengerWatchJobsForBot(corelib.DefaultLansengerBotProfileID)
}

// ListLansengerWatchJobsForBot returns jobs belonging to one bot profile.
func (a *App) ListLansengerWatchJobsForBot(botProfileID string) (string, error) {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return "[]", nil
	}
	jobs, err := svc.store.ListJobs()
	if err != nil {
		return "", err
	}
	if botProfileID, err = a.requireLansengerWatchBotProfile(botProfileID); err != nil {
		return "", err
	}
	filtered := make([]lansengerwatch.Job, 0, len(jobs))
	for _, job := range copyWatchJobs(jobs) {
		if job.BotProfileID == botProfileID {
			filtered = append(filtered, job)
		}
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// UpsertLansengerWatchJob creates or updates a job from JSON.
func (a *App) UpsertLansengerWatchJob(jobJSON string) (string, error) {
	return a.UpsertLansengerWatchJobForBot(corelib.DefaultLansengerBotProfileID, jobJSON)
}

// UpsertLansengerWatchJobForBot creates or updates a job scoped to one bot.
func (a *App) UpsertLansengerWatchJobForBot(botProfileID, jobJSON string) (string, error) {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return "", fmt.Errorf("watch service unavailable")
	}
	var job lansengerwatch.Job
	if err := json.Unmarshal([]byte(jobJSON), &job); err != nil {
		return "", fmt.Errorf("invalid job json: %w", err)
	}
	var err error
	if botProfileID, err = a.requireLansengerWatchBotProfile(botProfileID); err != nil {
		return "", err
	}
	job.BotProfileID = botProfileID
	saved, err := svc.store.UpsertJob(job)
	if err != nil {
		return "", err
	}
	// Hot path must see the new target list immediately after save.
	svc.putJobInCache(saved)
	data, err := json.Marshal(saved)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeleteLansengerWatchJob removes a job by id.
func (a *App) DeleteLansengerWatchJob(jobID string) error {
	return a.DeleteLansengerWatchJobForBot(corelib.DefaultLansengerBotProfileID, jobID)
}

// DeleteLansengerWatchJobForBot refuses to delete a job owned by another bot.
func (a *App) DeleteLansengerWatchJobForBot(botProfileID, jobID string) error {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return fmt.Errorf("watch service unavailable")
	}
	job, found, err := svc.store.GetJob(jobID)
	if err != nil {
		return err
	}
	if botProfileID, err = a.requireLansengerWatchBotProfile(botProfileID); err != nil {
		return err
	}
	if !found || copyWatchJobs([]lansengerwatch.Job{job})[0].BotProfileID != botProfileID {
		return fmt.Errorf("watch job %q was not found for this bot", strings.TrimSpace(jobID))
	}
	if err := svc.store.DeleteJob(jobID); err != nil {
		return err
	}
	svc.removeJobFromCache(jobID)
	return nil
}

// ListLansengerWatchRoster fetches the current Lansenger group directory so the
// UI can offer real members as watch targets. It also merges local entries
// learned from inbound messages, which keeps recently seen display names usable
// when the directory is temporarily unavailable.
func (a *App) ListLansengerWatchRoster(groupID, query string) (string, error) {
	return a.ListLansengerWatchRosterForBot(corelib.DefaultLansengerBotProfileID, groupID, query)
}

// ListLansengerWatchRosterForBot reads the directory through the selected bot
// and keeps same-ID groups owned by different bots isolated in the local roster.
func (a *App) ListLansengerWatchRosterForBot(botProfileID, groupID, query string) (string, error) {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		// Keep the Wails payload shape stable: the UI always expects a roster
		// object, including when the local watch store is not available yet.
		return `{"members":[],"directory_available":false,"note":"关注成员服务暂不可用。"}`, nil
	}
	var err error
	if botProfileID, err = a.requireLansengerWatchBotProfile(botProfileID); err != nil {
		return "", err
	}
	rosterKey := lansengerWatchRosterStoreKey(botProfileID, groupID)
	roster, err := svc.store.LoadRoster(rosterKey)
	if err != nil {
		return "", err
	}

	membersByID := make(map[string]lansengerwatch.Member, len(roster.Members))
	for _, member := range roster.Members {
		id := lansengerwatch.NormalizeStaffID(member.StaffID)
		if id == "" {
			continue
		}
		member.StaffID = id
		membersByID[id] = member
	}
	// Preserve the local roster separately. If the remote directory fails on the
	// very first page, return only this known-good local data rather than an
	// empty or misleading directory view.
	localMembersByID := make(map[string]lansengerwatch.Member, len(membersByID))
	for id, member := range membersByID {
		localMembersByID[id] = member
	}

	// The documented group-member endpoint is paginated at 100 members. Load
	// enough pages for a practical picker while keeping this Wails call bounded.
	const pageSize = 100
	gwErr := error(nil)
	directoryTruncated := false
	// directoryPartial marks a directory fetch that loaded some pages and then
	// failed mid-way. The partial directory is kept (marked truncated) instead
	// of falling back to only locally learned members.
	directoryPartial := false
	if cached, ok := svc.cachedRoster(rosterKey); ok {
		mergeCachedRosterMembers(membersByID, cached.members)
		if roster.GroupName == "" {
			roster.GroupName = cached.groupName
		}
		directoryTruncated = cached.truncated
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		gw, err := a.lansengerGatewayForWatch(botProfileID)
		gwErr = err
		if gwErr != nil {
			log.Printf("[lansenger-watch] roster %s: gateway unavailable: %v", groupID, gwErr)
		}
		if gwErr == nil {
			pagesFetched := 0
			// rawSeen tracks every staff ID the directory returned during this
			// fetch, including bots and deleted entries. Two things must not
			// count as replay: local roster overlap (a page of locally known
			// members is legitimate progress) and a page of only ineligible
			// entries (bots/deleted), which also says nothing about the offset.
			rawSeen := make(map[string]struct{}, pageSize)
			// dirMerged counts watch-eligible members this fetch added to the
			// merged map. The member cap bounds the directory fetch, so locally
			// learned members preloaded before it must not count against it.
			dirMerged := 0
			for offset := 0; offset < lansengerWatchMaxRosterMembers; {
				page, pageErr := gw.GetGroupMembers(ctx, groupID, offset, pageSize)
				if pageErr != nil {
					if pagesFetched == 0 {
						// Without one successful page there is no usable directory
						// view; fall back to the locally learned members.
						gwErr = pageErr
					} else {
						directoryTruncated = true
						directoryPartial = true
					}
					log.Printf("[lansenger-watch] roster %s: directory pagination stopped at offset %d after %d page(s): %v", groupID, offset, pagesFetched, pageErr)
					break
				}
				pagesFetched++
				newInPage := 0
				for _, member := range page.Members {
					id := lansengerwatch.NormalizeStaffID(member.StaffID)
					if id == "" {
						continue
					}
					if _, dup := rawSeen[id]; !dup {
						rawSeen[id] = struct{}{}
						newInPage++
					}
					// Bots are returned by the directory too, but cannot be a watch
					// target for a person's speech.
					// Deleted directory entries must never be offered as watch
					// targets. Inactive users remain selectable because they can
					// still be present in a group before activating their account.
					if !isWatchTargetMember(member) {
						continue
					}
					if _, exists := membersByID[id]; !exists {
						dirMerged++
					}
					membersByID[id] = lansengerwatch.Member{StaffID: id, Name: member.Name, Source: "directory"}
				}
				// Advance by the raw number of entries the server returned. Some
				// deployments cap the page size below the requested value, so a
				// "short" page is NOT a reliable end-of-directory signal; only an
				// empty page (or the reported total) is.
				offset += page.PageCount
				if page.PageCount == 0 {
					break
				}
				// Some deployments omit totalMembers; when present it is the
				// authoritative end-of-directory signal.
				if page.TotalMembers > 0 && offset >= page.TotalMembers {
					break
				}
				// A page that only repeats entries already returned in this fetch
				// means the server ignored the offset, or the directory shifted
				// under us mid-fetch (bulk head insertion). Either way the result
				// is not provably complete: treat it as partial (not cached, user
				// can retry) instead of silently caching a "complete" roster.
				// Pages with zero usable entries (e.g. all empty staff IDs) say
				// nothing about the offset and must not trigger this.
				if newInPage == 0 && len(page.Members) > 0 {
					directoryTruncated = true
					directoryPartial = true
					log.Printf("[lansenger-watch] roster %s: directory replayed already-seen entries at offset %d after %d page(s); treating as partial", groupID, offset, pagesFetched)
					break
				}
				// Bound the directory payload itself: a deployment returning pages
				// larger than requested must not push the fetch past the cap. It
				// is only truncation when unfetched directory entries may remain.
				if dirMerged >= lansengerWatchMaxRosterMembers {
					directoryTruncated = page.TotalMembers == 0 || offset < page.TotalMembers
					if directoryTruncated {
						log.Printf("[lansenger-watch] roster %s: directory truncated at %d members (reported total %d)", groupID, lansengerWatchMaxRosterMembers, page.TotalMembers)
					}
					break
				}
				if offset >= lansengerWatchMaxRosterMembers {
					directoryTruncated = page.TotalMembers == 0 || page.TotalMembers > lansengerWatchMaxRosterMembers
					if directoryTruncated {
						log.Printf("[lansenger-watch] roster %s: directory truncated at %d members (reported total %d)", groupID, lansengerWatchMaxRosterMembers, page.TotalMembers)
					}
					break
				}
			}
			if gwErr == nil && !directoryPartial {
				// Only cache complete bounded fetches. An interrupted partial
				// directory must NOT be cached: the panel's refresh button is
				// the user's retry path and must actually re-hit the directory.
				members := make([]lansengerwatch.Member, 0, len(membersByID))
				for _, member := range membersByID {
					members = append(members, member)
				}
				svc.cacheRoster(rosterKey, roster.GroupName, members, directoryTruncated)
			}
		}
	}
	if gwErr != nil {
		membersByID = localMembersByID
		directoryTruncated = false
		directoryPartial = false
	}

	members := make([]lansengerwatch.Member, 0, len(membersByID))
	for _, member := range membersByID {
		members = append(members, member)
	}
	// Map iteration order is intentionally randomized. A stable order keeps the
	// picker from jumping between refreshes and makes the first rendered page
	// predictable for large groups.
	sort.SliceStable(members, func(i, j int) bool {
		left := strings.TrimSpace(members[i].Name)
		right := strings.TrimSpace(members[j].Name)
		if left == right {
			return members[i].StaffID < members[j].StaffID
		}
		if left == "" {
			return false
		}
		if right == "" {
			return true
		}
		return left < right
	})
	members = lansengerwatch.FilterMembers(members, query)
	note := "成员由蓝信群成员目录提供。"
	if gwErr != nil {
		note = "蓝信群成员目录暂不可用，正在显示本地已学习成员。"
	} else if directoryPartial {
		note = "蓝信群成员目录未完整加载，当前列表可能不完整；请稍后刷新重试，或手动添加成员。"
	} else if directoryTruncated {
		note = fmt.Sprintf("群成员较多，仅加载前 %d 位；其他成员请手动添加 staffId。", lansengerWatchMaxRosterMembers)
	}
	data, err := json.Marshal(map[string]any{
		"group_id":            strings.TrimSpace(groupID),
		"group_name":          roster.GroupName,
		"members":             members,
		"updated_at":          roster.UpdatedAt,
		"directory_available": gwErr == nil,
		"directory_truncated": directoryTruncated,
		"note":                note,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (a *App) lansengerGatewayForWatch(botProfileID string) (*lansenger.Gateway, error) {
	if a == nil {
		return nil, fmt.Errorf("蓝信服务不可用")
	}
	var err error
	if botProfileID, err = a.requireLansengerWatchBotProfile(botProfileID); err != nil {
		return nil, err
	}
	if a.lansengerGateways != nil {
		if manager := a.lansengerGateways.manager(botProfileID); manager != nil {
			manager.mu.Lock()
			gw := manager.gateway
			manager.mu.Unlock()
			if gw != nil {
				return gw, nil
			}
		}
	}
	// The compatibility gateway is only safe for the migrated default profile.
	// Custom profiles must never query another bot's group directory.
	if botProfileID == corelib.DefaultLansengerBotProfileID && a.lansengerGateway != nil {
		a.lansengerGateway.mu.Lock()
		gw := a.lansengerGateway.gateway
		a.lansengerGateway.mu.Unlock()
		if gw != nil {
			return gw, nil
		}
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	profile, ok := lansengerBotProfileFromConfig(cfg, botProfileID)
	if !ok || strings.TrimSpace(profile.AppID) == "" || strings.TrimSpace(profile.AppSecret) == "" {
		return nil, fmt.Errorf("请先填写蓝信 App ID、App Secret 和网关地址")
	}
	apiGatewayURL := strings.TrimSpace(profile.GatewayURL)
	if apiGatewayURL == "" {
		apiGatewayURL = cfg.LansengerApiGatewayURL()
	}
	if apiGatewayURL == "" {
		return nil, fmt.Errorf("请先填写蓝信 App ID、App Secret 和网关地址")
	}
	wssURL := strings.TrimSpace(profile.WSSURL)
	if wssURL == "" {
		wssURL = cfg.LansengerWebSocketGatewayURL()
	}
	return lansenger.NewGateway(lansenger.Config{
		AppID: profile.AppID, AppSecret: profile.AppSecret,
		ApiGatewayURL: apiGatewayURL, WebSocketBaseURL: wssURL,
	}, nil), nil
}

// AddLansengerWatchMember manually adds a staff id to the group roster.
func (a *App) AddLansengerWatchMember(groupID, staffID, name string) error {
	return a.AddLansengerWatchMemberForBot(corelib.DefaultLansengerBotProfileID, groupID, staffID, name)
}

func (a *App) AddLansengerWatchMemberForBot(botProfileID, groupID, staffID, name string) error {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return fmt.Errorf("watch service unavailable")
	}
	staffID = lansengerwatch.NormalizeStaffID(staffID)
	if strings.TrimSpace(groupID) == "" || staffID == "" {
		return fmt.Errorf("group_id and staff_id required")
	}
	var err error
	if botProfileID, err = a.requireLansengerWatchBotProfile(botProfileID); err != nil {
		return err
	}
	rosterKey := lansengerWatchRosterStoreKey(botProfileID, groupID)
	if err := svc.store.NoteMember(rosterKey, "", staffID, name, "manual"); err != nil {
		return err
	}
	svc.invalidateRosterCache(rosterKey)
	return nil
}

// ListLansengerWatchTranscripts lists log file paths for a job.
func (a *App) ListLansengerWatchTranscripts(jobID string) (string, error) {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return "[]", nil
	}
	files, err := svc.store.ListTranscriptFiles(jobID)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadLansengerWatchTranscript returns file content (path must be under watch store).
func (a *App) ReadLansengerWatchTranscript(path string) (string, error) {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return "", fmt.Errorf("watch service unavailable")
	}
	return svc.store.ReadTranscriptFile(path)
}

// GetLansengerWatchStorePath returns the on-disk root for watch data.
func (a *App) GetLansengerWatchStorePath() string {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return ""
	}
	return svc.store.Root()
}

// WatchIMChannel is one selectable owner IM pathway for 盯人 forward.
type WatchIMChannel struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Online       bool   `json:"online"`
	SessionReady bool   `json:"session_ready"` // can proactive-push now (private chat known)
	Detail       string `json:"detail,omitempty"`
	Enabled      bool   `json:"enabled,omitempty"` // configured/on in app settings
}

// ListLansengerWatchChannels returns IM channels that can receive self-forwards.
func (a *App) ListLansengerWatchChannels() (string, error) {
	cfg, _ := a.LoadConfig()
	hubOK := false
	if hc := a.hubClient(); hc != nil && hc.IsConnected() {
		hubOK = true
	}

	wxStatus := "disconnected"
	wxOnline := false
	wxSession := false
	if a.weixinGateway != nil {
		wxStatus = a.weixinGateway.Status()
		wxOnline = strings.EqualFold(wxStatus, "connected")
		wxSession = a.weixinGateway.HasProactiveSession()
	}
	lsStatus := a.GetLansengerStatus()
	lsOnline := strings.EqualFold(lsStatus, "connected")
	lsSession := false
	if a.lansengerGateways != nil {
		for _, manager := range a.lansengerGateways.managers() {
			if manager.HasProactiveSession() {
				lsSession = true
				break
			}
		}
	} else if a.lansengerGateway != nil {
		lsSession = a.lansengerGateway.HasProactiveSession()
	}
	tgStatus := "disconnected"
	tgOnline := false
	tgSession := false
	if a.telegramGateway != nil {
		tgStatus = a.telegramGateway.Status()
		tgOnline = strings.EqualFold(tgStatus, "connected")
		tgSession = a.telegramGateway.HasProactiveSession()
	} else if cfg.TelegramBotOwnerChatID.Int64() != 0 {
		// Configured peer known even before gateway manager is created.
		tgSession = true
	}
	qqStatus := "disconnected"
	qqOnline := false
	qqSession := false
	if a.qqBotGateway != nil {
		qqStatus = a.qqBotGateway.Status()
		qqOnline = strings.EqualFold(qqStatus, "connected")
		qqSession = a.qqBotGateway.HasProactiveSession()
	} else if strings.TrimSpace(cfg.QQBotOwnerOpenID) != "" {
		// Configured peer known even before gateway manager is created.
		qqSession = true
	}

	wxEnabled := cfg.WeixinEnabled
	lsEnabled := cfg.LansengerEnabled || (a.lansengerGateways != nil && !a.lansengerGateways.isEmpty())
	tgEnabled := cfg.TelegramBotEnabled
	qqEnabled := cfg.QQBotEnabled

	// Telegram/QQ: "online" if local gateway connected or Hub can forward.
	tgLocalReady := tgEnabled && tgOnline && tgSession
	tgHubReady := tgEnabled && hubOK
	qqLocalReady := qqEnabled && qqOnline && qqSession
	qqHubReady := qqEnabled && hubOK

	channels := []WatchIMChannel{
		{
			ID: lansengerwatch.ChannelWeixin, Label: "微信",
			Enabled: wxEnabled, Online: wxEnabled && wxOnline, SessionReady: wxEnabled && wxOnline && wxSession,
			Detail: statusDetailWithSession(wxEnabled, wxStatus, wxSession, "请先用微信私聊机器人一次（会话失效后也需再聊）"),
		},
		{
			ID: lansengerwatch.ChannelLansenger, Label: "蓝信",
			Enabled: lsEnabled, Online: lsEnabled && lsOnline, SessionReady: lsEnabled && lsOnline && lsSession,
			Detail: statusDetailWithSession(lsEnabled, lsStatus, lsSession, "请先用蓝信私聊机器人一次"),
		},
		{
			ID: lansengerwatch.ChannelTelegram, Label: "Telegram",
			Enabled: tgEnabled, Online: tgEnabled && (tgOnline || hubOK), SessionReady: tgLocalReady || tgHubReady,
			Detail: statusDetailLocalOrHub(tgEnabled, tgStatus, tgLocalReady, hubOK, "请在设置填写 owner chat_id，或先给机器人发一条消息"),
		},
		{
			ID: lansengerwatch.ChannelQQ, Label: "QQ",
			Enabled: qqEnabled, Online: qqEnabled && (qqOnline || hubOK), SessionReady: qqLocalReady || qqHubReady,
			Detail: statusDetailLocalOrHub(qqEnabled, qqStatus, qqLocalReady, hubOK, "请在设置填写 owner openid，或先私聊机器人一次"),
		},
		{
			ID: lansengerwatch.ChannelHub, Label: "Hub（最近活跃 IM）",
			Enabled: true, Online: hubOK, SessionReady: hubOK,
			Detail: func() string {
				if hubOK {
					return "可推送 · 推到账号最近活跃的绑定 IM"
				}
				return "Hub 未连接"
			}(),
		},
	}
	data, err := json.Marshal(channels)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ListLansengerWatchChannelsForBot returns the owner-forward channels for one
// selected bot. Only the Lansenger row is bot-specific; other local IM and Hub
// routes are shared by the desktop app.
func (a *App) ListLansengerWatchChannelsForBot(botProfileID string) (string, error) {
	raw, err := a.ListLansengerWatchChannels()
	if err != nil {
		return "", err
	}
	botProfileID = normalizeLansengerWatchBotProfileID(botProfileID)
	channels := []WatchIMChannel{}
	if err := json.Unmarshal([]byte(raw), &channels); err != nil {
		return "", err
	}

	configured := false
	enabled := false
	if cfg, err := a.LoadConfig(); err == nil {
		if profile, ok := lansengerBotProfileFromConfig(cfg, botProfileID); ok {
			configured = true
			enabled = profile.Enabled
		}
	}
	status := a.GetLansengerBotStatus(botProfileID)
	online := strings.EqualFold(status, gatewayConnectionStatusConnected.String())
	sessionReady := false
	if a.lansengerGateways != nil {
		if manager := a.lansengerGateways.manager(botProfileID); manager != nil {
			sessionReady = manager.HasProactiveSession()
		}
	}
	if !sessionReady && botProfileID == corelib.DefaultLansengerBotProfileID && a.lansengerGateway != nil {
		sessionReady = a.lansengerGateway.HasProactiveSession()
	}

	for i := range channels {
		if channels[i].ID != lansengerwatch.ChannelLansenger {
			continue
		}
		channels[i].Enabled = configured && enabled
		channels[i].Online = configured && enabled && online
		channels[i].SessionReady = configured && enabled && online && sessionReady
		channels[i].Detail = statusDetailWithSession(channels[i].Enabled, status, sessionReady, "请先用蓝信私聊机器人一次")
		break
	}
	data, err := json.Marshal(channels)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ListLansengerWatchForwardResults returns recent self-forward attempts (newest first).
func (a *App) ListLansengerWatchForwardResults() (string, error) {
	return a.ListLansengerWatchForwardResultsForBot(corelib.DefaultLansengerBotProfileID)
}

// ListLansengerWatchForwardResultsForBot returns diagnostics for only one bot.
func (a *App) ListLansengerWatchForwardResultsForBot(botProfileID string) (string, error) {
	svc := a.watchService()
	if svc == nil {
		return "[]", nil
	}
	results := svc.listForwardResultsForBot(botProfileID)
	if results == nil {
		results = []WatchForwardResult{}
	}
	data, err := json.Marshal(results)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TestLansengerWatchForward sends a short probe message to one owner channel so
// the operator can verify private-session readiness without waiting for speech.
func (a *App) TestLansengerWatchForward(channel string) error {
	return a.TestLansengerWatchForwardForBot(corelib.DefaultLansengerBotProfileID, channel)
}

// TestLansengerWatchForwardForBot sends a probe using the selected bot for the
// Lansenger private channel, so it can never succeed through another bot.
func (a *App) TestLansengerWatchForwardForBot(botProfileID, channel string) error {
	svc := a.watchService()
	if svc == nil {
		return fmt.Errorf("watch service unavailable")
	}
	ch := lansengerwatch.NormalizeForwardChannel(channel)
	if ch == "" {
		return fmt.Errorf("unknown channel %q", channel)
	}
	botProfileID = normalizeLansengerWatchBotProfileID(botProfileID)
	body := "【盯人转发·测试】\n时间: " + time.Now().Local().Format("2006-01-02 15:04:05") + "\n若你收到此消息，说明该通道可推送。"
	var manager *lansengerGatewayManager
	if a.lansengerGateways != nil {
		manager = a.lansengerGateways.manager(botProfileID)
	}
	if manager == nil && botProfileID == corelib.DefaultLansengerBotProfileID {
		manager = a.lansengerGateway
	}
	err := svc.deliverToOwnerChannelForBot(botProfileID, manager, ch, body)
	res := WatchForwardResult{
		At: time.Now(), Reason: "test", Channel: ch, Preview: truncateWatchPreview(body, 80),
	}
	if err != nil {
		res.OK = false
		res.Error = err.Error()
		svc.recordForwardResultForBot(botProfileID, res)
		return err
	}
	res.OK = true
	svc.recordForwardResultForBot(botProfileID, res)
	return nil
}

func statusDetailWithSession(enabled bool, status string, sessionReady bool, needSessionHint string) string {
	if !enabled {
		return "未在设置中启用"
	}
	st := strings.TrimSpace(status)
	if st == "" {
		st = "unknown"
	}
	if !strings.EqualFold(st, "connected") {
		return "状态:" + st + " · " + needSessionHint
	}
	if sessionReady {
		return "在线 · 可推送"
	}
	return "在线 · 不可推送：" + needSessionHint
}

// statusDetailLocalOrHub describes QQ/Telegram pathways that can push via
// local gateway and/or Hub fallback.
func statusDetailLocalOrHub(enabled bool, localStatus string, localReady, hubOK bool, needSessionHint string) string {
	if !enabled {
		return "未在设置中启用"
	}
	if localReady {
		return "本地可推送"
	}
	if hubOK {
		return "可经 Hub 推送"
	}
	st := strings.TrimSpace(localStatus)
	if st == "" {
		st = "disconnected"
	}
	if strings.EqualFold(st, "connected") {
		return "在线 · 不可推送：" + needSessionHint
	}
	return "状态:" + st + " · " + needSessionHint + "（或连接 Hub）"
}
