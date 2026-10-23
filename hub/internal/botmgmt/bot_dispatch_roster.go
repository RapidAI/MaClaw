package botmgmt

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// dispatchRosterKey lists every tenant|user that ever had cross-bot work.
// The settings store cannot list tenants, so this roster is what lets a
// freshly started Hub resume dispatch for all tenants without waiting for
// someone to touch each account again.
const dispatchRosterKey = "botmgmt:dispatch_users"

// dispatchRosterMax bounds the roster; the oldest entries drop off.
const dispatchRosterMax = 5000

// rememberDispatchUser adds one tenant|user to the persisted roster.
// It runs off the request path and only writes when the pair is new.
func (s *Service) rememberDispatchUser(tenantID, userID string) {
	if s == nil || s.System == nil || strings.TrimSpace(userID) == "" {
		return
	}
	key := dispatchUserKey(tenantID, userID)
	s.dispatch.mu.Lock()
	s.dispatch.init()
	if s.dispatch.saved[key] {
		s.dispatch.mu.Unlock()
		return
	}
	s.dispatch.saved[key] = true
	s.dispatch.mu.Unlock()
	go func() {
		s.dispatch.rosterMu.Lock()
		defer s.dispatch.rosterMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		list := s.loadDispatchRoster(ctx)
		for _, have := range list {
			if have == key {
				return
			}
		}
		list = append(list, key)
		if len(list) > dispatchRosterMax {
			list = list[len(list)-dispatchRosterMax:]
		}
		raw, _ := json.Marshal(list)
		if err := s.System.Set(ctx, dispatchRosterKey, string(raw)); err != nil {
			s.dispatch.mu.Lock()
			delete(s.dispatch.saved, key)
			s.dispatch.mu.Unlock()
		}
	}()
}

func (s *Service) loadDispatchRoster(ctx context.Context) []string {
	raw, err := s.System.Get(ctx, dispatchRosterKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	var list []string
	if json.Unmarshal([]byte(raw), &list) != nil {
		return nil
	}
	return list
}

// ResumeBotDispatch is the boot scan: after delay, kick dispatch for every
// tenant|user on the roster, so delegates and wakes that were pending when
// Hub stopped run again. It returns the number of users kicked.
func (s *Service) ResumeBotDispatch(ctx context.Context) int {
	if s == nil || !s.BotDispatch || s.System == nil {
		return 0
	}
	list := s.loadDispatchRoster(ctx)
	sort.Strings(list)
	n := 0
	for _, key := range list {
		tenantID, userID, ok := strings.Cut(key, "|")
		if !ok || strings.TrimSpace(userID) == "" {
			continue
		}
		s.dispatch.mu.Lock()
		s.dispatch.init()
		s.dispatch.saved[key] = true
		s.dispatch.mu.Unlock()
		s.kickDispatch(tenantID, userID)
		n++
	}
	return n
}
