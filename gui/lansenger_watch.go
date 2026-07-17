package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

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
	At      time.Time `json:"at"`
	JobID   string    `json:"job_id,omitempty"`
	Reason  string    `json:"reason,omitempty"`
	Channel string    `json:"channel"`
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	Preview string    `json:"preview,omitempty"`
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

func (s *lansengerWatchService) listJobsCached() []lansengerwatch.Job {
	if s == nil || s.store == nil {
		return nil
	}
	s.mu.Lock()
	if s.jobsCache != nil && time.Since(s.jobsCacheAt) < 2*time.Second {
		out := make([]lansengerwatch.Job, len(s.jobsCache))
		copy(out, s.jobsCache)
		s.mu.Unlock()
		return out
	}
	gen := s.jobsGen
	s.mu.Unlock()

	// Disk I/O outside service lock so invalidate/UI upserts are not stalled.
	jobs, err := s.store.ListJobs()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		log.Printf("[lansenger-watch] list jobs: %v", err)
		if s.jobsCache == nil {
			return nil
		}
		out := make([]lansengerwatch.Job, len(s.jobsCache))
		copy(out, s.jobsCache)
		return out
	}
	// Drop stale load if a writer invalidated while we were on disk.
	if gen != s.jobsGen {
		// Prefer a fresh read next call; still return latest disk snapshot we got.
		out := make([]lansengerwatch.Job, len(jobs))
		copy(out, jobs)
		return out
	}
	s.jobsCache = jobs
	s.jobsCacheAt = time.Now()
	out := make([]lansengerwatch.Job, len(jobs))
	copy(out, jobs)
	return out
}

// processMessage is the IM hot path: roster + record + keyword reply/CLI + private forward.
// It never claims the message for the agent; caller decides routing.
func (s *lansengerWatchService) processMessage(msg lansenger.IncomingMessage) {
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
	if groupID == "" || speakerID == "" {
		return
	}
	jobs := s.listJobsCached()
	// Fast exit when 盯人 is unused or this group has no enabled job.
	if !lansengerwatch.AnyActiveWatchForGroup(jobs, groupID) {
		return
	}
	// Learn roster only for groups with active watch jobs.
	// NoteMember skips redundant disk writes for frequent chatters.
	if err := s.store.NoteMember(groupID, msg.GroupName, speakerID, msg.SenderName, "message"); err != nil {
		log.Printf("[lansenger-watch] roster note: %v", err)
	} else {
		s.noteCachedRosterMember(groupID, msg.GroupName, speakerID, msg.SenderName)
	}
	// Include keyword-scope=anyone jobs even when speaker is not a watch target.
	if !lansengerwatch.GroupNeedsWatchMessage(jobs, groupID, speakerID) {
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
	res := s.engine.Process(ctx, jobs, lansengerwatch.Incoming{
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
		if reply == "" || s.recentGroupReply(groupID, reply) {
			continue
		}
		if err := s.sendWatchGroupText(msg, reply); err != nil {
			log.Printf("[lansenger-watch] group reply: %v", err)
			continue
		}
		// Only mark after a successful send so a failed delivery can retry.
		s.rememberGroupReply(groupID, reply)
	}
	// Forwards to owner IM channels — parallel; do not block gateway forever.
	s.deliverForwardsParallel(res.Forwards)
}

// recentGroupReply reports whether the same reply was sent recently (check only).
func (s *lansengerWatchService) recentGroupReply(groupID, reply string) bool {
	if s == nil {
		return false
	}
	key := strings.TrimSpace(groupID) + "\x00" + reply
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
	if s == nil {
		return
	}
	key := strings.TrimSpace(groupID) + "\x00" + reply
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

func (s *lansengerWatchService) deliverForwardsParallel(forwards []lansengerwatch.ForwardRequest) {
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
			if err := s.deliverToOwnerChannel(chSend, bodySend); err != nil {
				log.Printf("[lansenger-watch] forward job=%s reason=%s channel=%s: %v", jobID, reason, chSend, err)
				s.recordForwardResult(WatchForwardResult{
					At: time.Now(), JobID: jobID, Reason: reason, Channel: chSend,
					OK: false, Error: err.Error(), Preview: truncateWatchPreview(bodySend, 80),
				})
			} else {
				log.Printf("[lansenger-watch] forward job=%s reason=%s channel=%s ok", jobID, reason, chSend)
				s.recordForwardResult(WatchForwardResult{
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

func (s *lansengerWatchService) sendWatchGroupText(msg lansenger.IncomingMessage, text string) error {
	if s == nil || s.app == nil || s.app.lansengerGateway == nil {
		return fmt.Errorf("lansenger gateway unavailable")
	}
	m := s.app.lansengerGateway
	m.mu.Lock()
	gw := m.gateway
	m.mu.Unlock()
	if gw == nil {
		return fmt.Errorf("lansenger gateway not running")
	}
	// Keyword / CLI auto-replies should honor the same group-chat decorations
	// (auto-@ / native quote) as agent answers.
	return gw.SendText(context.Background(), buildLansengerOutgoingText(msg, text, m.currentGroupOpts()))
}

func (s *lansengerWatchService) recordForwardResult(r WatchForwardResult) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forwardResults = append(s.forwardResults, r)
	if len(s.forwardResults) > lansengerWatchMaxForwardResults {
		s.forwardResults = append([]WatchForwardResult(nil), s.forwardResults[len(s.forwardResults)-lansengerWatchMaxForwardResults:]...)
	}
}

func (s *lansengerWatchService) listForwardResults() []WatchForwardResult {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.forwardResults) == 0 {
		return nil
	}
	// Newest first for the panel.
	out := make([]WatchForwardResult, len(s.forwardResults))
	for i := range s.forwardResults {
		out[len(out)-1-i] = s.forwardResults[i]
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
		s.app.ensureLansengerGateway()
		if s.app.lansengerGateway == nil {
			return fmt.Errorf("蓝信未启用或网关不可用")
		}
		if !s.app.lansengerGateway.HasProactiveSession() {
			return fmt.Errorf("蓝信无可用私聊会话：请先用蓝信私聊机器人一次后再试")
		}
		if err := s.app.lansengerGateway.SendProactiveText(text); err != nil {
			return fmt.Errorf("蓝信推送失败: %w", err)
		}
		return nil
	case lansengerwatch.ChannelTelegram, lansengerwatch.ChannelQQ, lansengerwatch.ChannelHub:
		// Portable owner-channel path: Hub proactive → last active bound IM.
		hc := s.app.hubClient()
		if hc == nil || !hc.IsConnected() {
			// Try ensure for hub channel only
			if channel == lansengerwatch.ChannelHub {
				hc = s.app.ensureHubClient()
			}
		}
		if hc == nil || !hc.IsConnected() {
			return fmt.Errorf("%s 需要 Hub 已连接", channel)
		}
		if err := hc.SendIMProactiveMessage(text); err != nil {
			return fmt.Errorf("%s 推送失败: %w", channel, err)
		}
		return nil
	default:
		return fmt.Errorf("未知通道 %q", channel)
	}
}

// ---------------------------------------------------------------------------
// Wails bindings
// ---------------------------------------------------------------------------

// ListLansengerWatchJobs returns watch jobs JSON.
func (a *App) ListLansengerWatchJobs() (string, error) {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return "[]", nil
	}
	jobs, err := svc.store.ListJobs()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(jobs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// UpsertLansengerWatchJob creates or updates a job from JSON.
func (a *App) UpsertLansengerWatchJob(jobJSON string) (string, error) {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return "", fmt.Errorf("watch service unavailable")
	}
	var job lansengerwatch.Job
	if err := json.Unmarshal([]byte(jobJSON), &job); err != nil {
		return "", fmt.Errorf("invalid job json: %w", err)
	}
	saved, err := svc.store.UpsertJob(job)
	if err != nil {
		return "", err
	}
	svc.invalidateCache()
	data, err := json.Marshal(saved)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeleteLansengerWatchJob removes a job by id.
func (a *App) DeleteLansengerWatchJob(jobID string) error {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return fmt.Errorf("watch service unavailable")
	}
	if err := svc.store.DeleteJob(jobID); err != nil {
		return err
	}
	svc.invalidateCache()
	return nil
}

// ListLansengerWatchRoster fetches the current Lansenger group directory so the
// UI can offer real members as watch targets. It also merges local entries
// learned from inbound messages, which keeps recently seen display names usable
// when the directory is temporarily unavailable.
func (a *App) ListLansengerWatchRoster(groupID, query string) (string, error) {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		// Keep the Wails payload shape stable: the UI always expects a roster
		// object, including when the local watch store is not available yet.
		return `{"members":[],"directory_available":false,"note":"关注成员服务暂不可用。"}`, nil
	}
	roster, err := svc.store.LoadRoster(groupID)
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
	if cached, ok := svc.cachedRoster(groupID); ok {
		mergeCachedRosterMembers(membersByID, cached.members)
		if roster.GroupName == "" {
			roster.GroupName = cached.groupName
		}
		directoryTruncated = cached.truncated
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		gw, err := a.lansengerGatewayForWatch()
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
				svc.cacheRoster(groupID, roster.GroupName, members, directoryTruncated)
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
		"group_id":            roster.GroupID,
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

func (a *App) lansengerGatewayForWatch() (*lansenger.Gateway, error) {
	if a == nil {
		return nil, fmt.Errorf("蓝信服务不可用")
	}
	if a.lansengerGateway != nil {
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
	if strings.TrimSpace(cfg.LansengerAppID) == "" || strings.TrimSpace(cfg.LansengerAppSecret) == "" || strings.TrimSpace(cfg.LansengerApiGatewayURL()) == "" {
		return nil, fmt.Errorf("请先填写蓝信 App ID、App Secret 和网关地址")
	}
	return lansenger.NewGateway(lansenger.Config{
		AppID: cfg.LansengerAppID, AppSecret: cfg.LansengerAppSecret,
		ApiGatewayURL: cfg.LansengerApiGatewayURL(), WebSocketBaseURL: cfg.LansengerWebSocketGatewayURL(),
	}, nil), nil
}

// AddLansengerWatchMember manually adds a staff id to the group roster.
func (a *App) AddLansengerWatchMember(groupID, staffID, name string) error {
	svc := a.watchService()
	if svc == nil || svc.store == nil {
		return fmt.Errorf("watch service unavailable")
	}
	staffID = lansengerwatch.NormalizeStaffID(staffID)
	if strings.TrimSpace(groupID) == "" || staffID == "" {
		return fmt.Errorf("group_id and staff_id required")
	}
	if err := svc.store.NoteMember(groupID, "", staffID, name, "manual"); err != nil {
		return err
	}
	svc.invalidateRosterCache(groupID)
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
	lsStatus := "disconnected"
	lsOnline := false
	lsSession := false
	if a.lansengerGateway != nil {
		lsStatus = a.lansengerGateway.Status()
		lsOnline = strings.EqualFold(lsStatus, "connected")
		lsSession = a.lansengerGateway.HasProactiveSession()
	}
	tgStatus := "disconnected"
	if a.telegramGateway != nil {
		tgStatus = a.telegramGateway.Status()
	}
	qqStatus := "disconnected"
	if a.qqBotGateway != nil {
		qqStatus = a.qqBotGateway.Status()
	}

	wxEnabled := cfg.WeixinEnabled
	lsEnabled := cfg.LansengerEnabled
	tgEnabled := cfg.TelegramBotEnabled
	qqEnabled := cfg.QQBotEnabled

	channels := []WatchIMChannel{
		{
			ID: lansengerwatch.ChannelWeixin, Label: "微信",
			Enabled: wxEnabled, Online: wxEnabled && wxOnline, SessionReady: wxEnabled && wxOnline && wxSession,
			Detail: statusDetailWithSession(wxEnabled, wxStatus, wxSession, "请先用微信私聊机器人一次"),
		},
		{
			ID: lansengerwatch.ChannelLansenger, Label: "蓝信",
			Enabled: lsEnabled, Online: lsEnabled && lsOnline, SessionReady: lsEnabled && lsOnline && lsSession,
			Detail: statusDetailWithSession(lsEnabled, lsStatus, lsSession, "请先用蓝信私聊机器人一次"),
		},
		{
			ID: lansengerwatch.ChannelTelegram, Label: "Telegram",
			Enabled: tgEnabled, Online: tgEnabled && hubOK, SessionReady: tgEnabled && hubOK,
			Detail: statusDetailWithSession(tgEnabled, tgStatus, hubOK, "需 Hub 已连接（经 Hub 主动推送）"),
		},
		{
			ID: lansengerwatch.ChannelQQ, Label: "QQ",
			Enabled: qqEnabled, Online: qqEnabled && hubOK, SessionReady: qqEnabled && hubOK,
			Detail: statusDetailWithSession(qqEnabled, qqStatus, hubOK, "需 Hub 已连接（经 Hub 主动推送）"),
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

// ListLansengerWatchForwardResults returns recent self-forward attempts (newest first).
func (a *App) ListLansengerWatchForwardResults() (string, error) {
	svc := a.watchService()
	if svc == nil {
		return "[]", nil
	}
	results := svc.listForwardResults()
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
	svc := a.watchService()
	if svc == nil {
		return fmt.Errorf("watch service unavailable")
	}
	ch := lansengerwatch.NormalizeForwardChannel(channel)
	if ch == "" {
		return fmt.Errorf("unknown channel %q", channel)
	}
	body := "【盯人转发·测试】\n时间: " + time.Now().Local().Format("2006-01-02 15:04:05") + "\n若你收到此消息，说明该通道可推送。"
	err := svc.deliverToOwnerChannel(ch, body)
	res := WatchForwardResult{
		At: time.Now(), Reason: "test", Channel: ch, Preview: truncateWatchPreview(body, 80),
	}
	if err != nil {
		res.OK = false
		res.Error = err.Error()
		svc.recordForwardResult(res)
		return err
	}
	res.OK = true
	svc.recordForwardResult(res)
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


