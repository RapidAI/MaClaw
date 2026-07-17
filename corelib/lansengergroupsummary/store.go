package lansengergroupsummary

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store persists per-group message logs and summary cursors under baseDir.
type Store struct {
	mu      sync.Mutex
	baseDir string
	ready   bool

	MaxMessagesPerGroup int
	MaxMessageAge       time.Duration
	PruneEveryNAppends  int

	// stateCache avoids re-reading state JSON on every Append (hot path).
	stateCache map[string]GroupState
	// recentIDs is a process-local set of recent platform message IDs per group
	// for cheap redelivery dedup.
	recentIDs map[string]map[string]struct{}
}

const recentMessageIDRing = 256

// NewStore creates a store rooted at maclawBase/lansenger_group_summary.
func NewStore(maclawBase string) *Store {
	return &Store{
		baseDir:             filepath.Join(strings.TrimSpace(maclawBase), StoreDirName),
		MaxMessagesPerGroup: DefaultMaxMessagesPerGroup,
		MaxMessageAge:       DefaultMaxMessageAge,
		PruneEveryNAppends:  DefaultPruneEveryNAppends,
		stateCache:          make(map[string]GroupState),
		recentIDs:           make(map[string]map[string]struct{}),
	}
}

// Root returns the store directory.
func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.baseDir
}

func (s *Store) EnsureReady() error {
	if s == nil {
		return fmt.Errorf("lansengergroupsummary: nil store")
	}
	if s.ready {
		return nil
	}
	for _, d := range []string{
		s.baseDir,
		filepath.Join(s.baseDir, MessagesDirName),
		filepath.Join(s.baseDir, StateDirName),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	s.ready = true
	return nil
}

func (s *Store) messagesPath(groupID string) string {
	return filepath.Join(s.baseDir, MessagesDirName, sanitizeFilePart(groupID)+".jsonl")
}

func (s *Store) statePath(groupID string) string {
	return filepath.Join(s.baseDir, StateDirName, sanitizeFilePart(groupID)+".json")
}

// Append records a group message. Empty text is ignored. Returns the assigned seq.
// Duplicate MessageID values (when non-empty) are ignored for idempotent redelivery
// within the recent in-memory set.
func (s *Store) Append(groupID, groupName, messageID, speakerID, speakerName, text string, at time.Time) (Message, error) {
	groupID = strings.TrimSpace(groupID)
	text = strings.TrimSpace(text)
	if groupID == "" || text == "" {
		return Message{}, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	messageID = strings.TrimSpace(messageID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.EnsureReady(); err != nil {
		return Message{}, err
	}

	st, err := s.loadStateLocked(groupID)
	if err != nil {
		return Message{}, err
	}
	if gn := strings.TrimSpace(groupName); gn != "" {
		st.GroupName = gn
	}

	if messageID != "" && s.hasRecentMessageIDLocked(groupID, messageID) {
		return Message{}, nil
	}

	// Reserve seq in state first so a failed append only leaves a gap (safe).
	st.NextSeq++
	st.AppendsSince++
	st.UpdatedAt = time.Now().UTC()
	msg := Message{
		Seq:         st.NextSeq,
		MessageID:   messageID,
		SpeakerID:   strings.TrimSpace(speakerID),
		SpeakerName: strings.TrimSpace(speakerName),
		Text:        text,
		At:          at.UTC(),
	}
	if err := s.saveStateLocked(st); err != nil {
		return Message{}, err
	}

	path := s.messagesPath(groupID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Message{}, err
	}
	line, err := json.Marshal(msg)
	if err != nil {
		f.Close()
		return Message{}, err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return Message{}, err
	}
	if err := f.Close(); err != nil {
		return Message{}, err
	}
	if messageID != "" {
		s.rememberMessageIDLocked(groupID, messageID)
	}

	// Periodic prune: only when counter hits threshold (not every hot-path write).
	every := s.PruneEveryNAppends
	if every <= 0 {
		every = DefaultPruneEveryNAppends
	}
	if st.AppendsSince >= every {
		if err := s.pruneLocked(groupID, st); err == nil {
			st.AppendsSince = 0
			_ = s.saveStateLocked(st)
		}
	}
	return msg, nil
}

func (s *Store) hasRecentMessageIDLocked(groupID, messageID string) bool {
	set := s.recentIDs[groupID]
	if set == nil {
		return false
	}
	_, ok := set[messageID]
	return ok
}

func (s *Store) rememberMessageIDLocked(groupID, messageID string) {
	if messageID == "" {
		return
	}
	if s.recentIDs == nil {
		s.recentIDs = make(map[string]map[string]struct{})
	}
	set := s.recentIDs[groupID]
	if set == nil {
		set = make(map[string]struct{}, recentMessageIDRing)
		s.recentIDs[groupID] = set
	}
	if len(set) >= recentMessageIDRing {
		// Soft reset: keep the set from growing without scanning. Redelivery
		// windows are short; occasional re-append after reset is acceptable.
		clear(set)
	}
	set[messageID] = struct{}{}
}

// LoadNew returns messages with Seq > lastSummarySeq (and not older than MaxMessageAge).
// Age-expired unsummarized lines are skipped for summarization and the cursor is
// advanced past a contiguous expired-only prefix so they do not block forever
// (prune would otherwise keep them while LoadNew never returns them).
func (s *Store) LoadNew(groupID string) ([]Message, GroupState, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, GroupState{}, fmt.Errorf("group_id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.EnsureReady(); err != nil {
		return nil, GroupState{}, err
	}
	st, err := s.loadStateLocked(groupID)
	if err != nil {
		return nil, GroupState{}, err
	}
	all, err := s.readAllLocked(groupID)
	if err != nil {
		return nil, st, err
	}
	cutoff := time.Time{}
	if s.MaxMessageAge > 0 {
		cutoff = time.Now().UTC().Add(-s.MaxMessageAge)
	}
	out := make([]Message, 0, len(all))
	var maxExpiredSeq int64
	for _, m := range all {
		if m.Seq <= st.LastSummarySeq {
			continue
		}
		if !cutoff.IsZero() && !m.At.IsZero() && m.At.Before(cutoff) {
			if m.Seq > maxExpiredSeq {
				maxExpiredSeq = m.Seq
			}
			continue
		}
		out = append(out, m)
	}
	// Reclaim expired-only backlog so empty LoadNew can still move the cursor.
	// Only advance through expired seqs strictly below the first live message
	// (or all expired when there is no live message).
	if maxExpiredSeq > st.LastSummarySeq {
		reclaimTo := maxExpiredSeq
		if len(out) > 0 {
			firstLive := out[0].Seq
			for i := 1; i < len(out); i++ {
				if out[i].Seq < firstLive {
					firstLive = out[i].Seq
				}
			}
			// Highest expired seq that is still before any live unsummarized msg.
			reclaimTo = st.LastSummarySeq
			for _, m := range all {
				if m.Seq <= st.LastSummarySeq || m.Seq >= firstLive {
					continue
				}
				expired := !cutoff.IsZero() && !m.At.IsZero() && m.At.Before(cutoff)
				if expired && m.Seq > reclaimTo {
					reclaimTo = m.Seq
				}
			}
		}
		if reclaimTo > st.LastSummarySeq {
			st.LastSummarySeq = reclaimTo
			st.UpdatedAt = time.Now().UTC()
			if err := s.saveStateLocked(st); err != nil {
				return out, st, err
			}
		}
	}
	return out, st, nil
}

// MarkSummarized advances the cursor so subsequent LoadNew starts after maxSeq
// and records LastSummaryAt as a completed summary time.
func (s *Store) MarkSummarized(groupID string, maxSeq int64, at time.Time) error {
	return s.markCursor(groupID, maxSeq, at, true)
}

// MarkCursor advances LastSummarySeq without updating LastSummaryAt.
// Used by /summary start so empty follow-ups do not claim a prior "摘要" finished.
func (s *Store) MarkCursor(groupID string, maxSeq int64) error {
	return s.markCursor(groupID, maxSeq, time.Time{}, false)
}

func (s *Store) markCursor(groupID string, maxSeq int64, at time.Time, setSummaryAt bool) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return fmt.Errorf("group_id required")
	}
	if setSummaryAt && at.IsZero() {
		at = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadStateLocked(groupID)
	if err != nil {
		return err
	}
	// No-op when cursor does not move and we are not recording a summary time.
	if maxSeq <= st.LastSummarySeq && !setSummaryAt {
		return nil
	}
	changed := false
	if maxSeq > st.LastSummarySeq {
		st.LastSummarySeq = maxSeq
		changed = true
	}
	if setSummaryAt {
		st.LastSummaryAt = at.UTC()
		changed = true
	}
	if !changed {
		return nil
	}
	st.UpdatedAt = time.Now().UTC()
	if err := s.saveStateLocked(st); err != nil {
		return err
	}
	// Free already-passed aged lines after a cursor advance.
	_ = s.pruneLocked(groupID, st)
	return nil
}

// LoadState returns the cursor state for a group.
func (s *Store) LoadState(groupID string) (GroupState, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return GroupState{}, fmt.Errorf("group_id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadStateLocked(groupID)
}

func (s *Store) loadStateLocked(groupID string) (GroupState, error) {
	if s.stateCache != nil {
		if st, ok := s.stateCache[groupID]; ok {
			return st, nil
		}
	}
	path := s.statePath(groupID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			st := GroupState{GroupID: groupID}
			s.cacheStateLocked(st)
			return st, nil
		}
		return GroupState{}, err
	}
	var st GroupState
	if err := json.Unmarshal(data, &st); err != nil {
		return GroupState{}, err
	}
	st.GroupID = groupID
	s.cacheStateLocked(st)
	return st, nil
}

func (s *Store) cacheStateLocked(st GroupState) {
	if s.stateCache == nil {
		s.stateCache = make(map[string]GroupState)
	}
	s.stateCache[st.GroupID] = st
}

func (s *Store) saveStateLocked(st GroupState) error {
	if err := s.EnsureReady(); err != nil {
		return err
	}
	data, err := json.Marshal(st) // compact JSON — hot path writes often
	if err != nil {
		return err
	}
	path := s.statePath(st.GroupID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	s.cacheStateLocked(st)
	return nil
}

func (s *Store) readAllLocked(groupID string) ([]Message, error) {
	path := s.messagesPath(groupID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []Message
	sc := bufio.NewScanner(f)
	// Allow long chat lines (default 64K is often too small for media-path prefixes).
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue // skip corrupt lines
		}
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) pruneLocked(groupID string, st GroupState) error {
	all, err := s.readAllLocked(groupID)
	if err != nil || len(all) == 0 {
		return err
	}
	maxN := s.MaxMessagesPerGroup
	if maxN <= 0 {
		maxN = DefaultMaxMessagesPerGroup
	}
	cutoff := time.Time{}
	if s.MaxMessageAge > 0 {
		cutoff = time.Now().UTC().Add(-s.MaxMessageAge)
	}

	// Drop age-expired messages that are already past the summary cursor.
	// Always retain unsummarized lines (Seq > LastSummarySeq) even if old.
	kept := make([]Message, 0, len(all))
	for _, m := range all {
		if !cutoff.IsZero() && !m.At.IsZero() && m.At.Before(cutoff) && m.Seq <= st.LastSummarySeq {
			continue
		}
		kept = append(kept, m)
	}
	if len(kept) > maxN {
		// Prefer keeping the unsummarized tail: drop from the front.
		kept = kept[len(kept)-maxN:]
	}
	if len(kept) == len(all) {
		return nil
	}
	return s.rewriteMessagesLocked(groupID, kept)
}

func (s *Store) rewriteMessagesLocked(groupID string, msgs []Message) error {
	path := s.messagesPath(groupID)
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil { // Encode adds newline
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sanitizeFilePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		return "unknown"
	}
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}
