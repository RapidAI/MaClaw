package agentservice

import (
	"context"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type UserMemoryListInput struct {
	Category string
	Query    string
	Limit    int
	Offset   int
}

const (
	UserMemoryMaxContentRunes = 20000
	UserMemoryMaxTags         = 32
	UserMemoryMaxTagRunes     = 80
)

type UserMemoryListResult struct {
	Items          []UserMemoryView `json:"items"`
	Total          int              `json:"total"`
	Offset         int              `json:"offset"`
	Limit          int              `json:"limit"`
	HasMore        bool             `json:"has_more"`
	NextOffset     int              `json:"next_offset"`
	CategoryCounts map[string]int   `json:"category_counts"`
}

type UserMemorySaveInput struct {
	Content  string   `json:"content"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
}

type UserMemoryView struct {
	ID          string          `json:"id"`
	Content     string          `json:"content"`
	Title       string          `json:"title,omitempty"`
	Category    memory.Category `json:"category"`
	Tags        []string        `json:"tags"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	AccessCount int             `json:"access_count"`
	SourceURL   string          `json:"source_url,omitempty"`
	SourceType  string          `json:"source_type,omitempty"`
	Stale       bool            `json:"stale,omitempty"`
	ReadOnly    bool            `json:"read_only"`
	Protected   bool            `json:"protected"`
}

func (s *Service) ListUserMemories(ctx context.Context, p Principal, in UserMemoryListInput) (*UserMemoryListResult, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	store, err := s.openUserMemoryStore(p)
	if err != nil {
		return nil, err
	}
	defer store.Stop()
	category, err := normalizeUserMemoryCategory(in.Category, true)
	if err != nil {
		return nil, err
	}
	entries := store.List("", strings.TrimSpace(in.Query))
	ownerID := memoryOwnerIDForPrincipal(p)
	owned := make([]memory.Entry, 0, len(entries))
	categoryCounts := map[string]int{}
	for _, entry := range entries {
		if entry.OwnerID != "" && entry.OwnerID != ownerID {
			continue
		}
		owned = append(owned, entry)
		categoryCounts[string(memory.MapToCanonical(entry.Category))]++
	}
	filtered := owned
	if category != "" {
		filtered = make([]memory.Entry, 0, len(owned))
		for _, entry := range owned {
			if memory.MapToCanonical(entry.Category) == category {
				filtered = append(filtered, entry)
			}
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt) })
	total := len(filtered)
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	} else if limit > 200 {
		limit = 200
	}
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(filtered) {
		offset = len(filtered)
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return &UserMemoryListResult{Items: userMemoryViews(filtered[offset:end], ownerID), Total: total, Offset: offset, Limit: limit, HasMore: end < total, NextOffset: end, CategoryCounts: categoryCounts}, nil
}

func (s *Service) SaveUserMemory(ctx context.Context, p Principal, in UserMemorySaveInput) (*UserMemoryView, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, fmt.Errorf("memory content is required")
	}
	if utf8.RuneCountInString(content) > UserMemoryMaxContentRunes {
		return nil, fmt.Errorf("memory content is too long, max %d characters", UserMemoryMaxContentRunes)
	}
	category, err := normalizeUserMemoryCategory(in.Category, false)
	if err != nil {
		return nil, err
	}
	if category.IsProtected() {
		return nil, fmt.Errorf("protected memory cannot be created here")
	}
	store, release, err := s.userMemoryStoreForWrite(p)
	if err != nil {
		return nil, err
	}
	defer release()
	tags, err := cleanMemoryTags(in.Tags)
	if err != nil {
		return nil, err
	}
	ownerID := memoryOwnerIDForPrincipal(p)
	entry := memory.Entry{ID: NewID("mem"), Content: content, Category: category, Tags: tags, OwnerID: ownerID, SourceType: "manual"}
	if err := store.UpsertEntriesByID([]memory.Entry{entry}); err != nil {
		return nil, err
	}
	if saved, ok := findMutableMemoryEntry(store, entry.ID, ownerID); ok {
		out := userMemoryView(saved, ownerID)
		return &out, nil
	}
	out := userMemoryView(entry, ownerID)
	return &out, nil
}

func (s *Service) UpdateUserMemory(ctx context.Context, p Principal, id string, in UserMemorySaveInput) (*UserMemoryView, error) {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("memory id is required")
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, fmt.Errorf("memory content is required")
	}
	if utf8.RuneCountInString(content) > UserMemoryMaxContentRunes {
		return nil, fmt.Errorf("memory content is too long, max %d characters", UserMemoryMaxContentRunes)
	}
	category, err := normalizeUserMemoryCategory(in.Category, false)
	if err != nil {
		return nil, err
	}
	if category.IsProtected() {
		return nil, fmt.Errorf("protected memory cannot be edited here")
	}
	store, release, err := s.userMemoryStoreForWrite(p)
	if err != nil {
		return nil, err
	}
	defer release()
	ownerID := memoryOwnerIDForPrincipal(p)
	if entry, ok := findMutableMemoryEntry(store, id, ownerID); !ok {
		return nil, ErrRecordNotFound
	} else if entry.Category.IsProtected() {
		return nil, fmt.Errorf("protected memory cannot be edited here")
	}
	tags, err := cleanMemoryTags(in.Tags)
	if err != nil {
		return nil, err
	}
	if err := store.Update(id, content, category, tags); err != nil {
		return nil, err
	}
	entry, _ := findMutableMemoryEntry(store, id, ownerID)
	out := userMemoryView(entry, ownerID)
	return &out, nil
}

func (s *Service) DeleteUserMemory(ctx context.Context, p Principal, id string) error {
	_ = ctx
	if _, err := s.store.GetUser(p.TenantID, p.UserID); err != nil {
		return err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("memory id is required")
	}
	store, release, err := s.userMemoryStoreForWrite(p)
	if err != nil {
		return err
	}
	defer release()
	if entry, ok := findMutableMemoryEntry(store, id, memoryOwnerIDForPrincipal(p)); !ok {
		return ErrRecordNotFound
	} else if entry.Category.IsProtected() {
		return fmt.Errorf("protected memory cannot be deleted here")
	}
	return store.Delete(id)
}

func (s *Service) openUserMemoryStore(p Principal) (*memory.Store, error) {
	dataDir := s.userDataRoot(p.TenantID, p.UserID)
	return memory.OpenDataDirStore(dataDir, memory.StoreModeAuto, filepath.Join(dataDir, "agent_memory.json"))
}

// sharedUserMemoryStore is implemented by executors that keep one long-lived
// memory.Store per user. The service prefers that instance for reads and writes
// so that the API and the worker observe the same state; opening a second store
// on the same directory silently diverges under the JSON backend, which has no
// cross-instance sync.
type sharedUserMemoryStore interface {
	SharedUserMemoryStore(tenantID, userID string) *memory.Store
}

// userMemoryStoreForWrite returns the store to record a change into, plus a
// release function that must be called when the change is done.
//
// Releasing has two distinct obligations. A store this call opened is closed,
// and Store.Stop flushes it. A shared store must not be closed, but it still
// has to be flushed: its persistLoop debounces writes by 5s, so without an
// explicit flush a preference would be readable in-process yet lost to a crash
// in that window, which is the durability the API contract promises.
func (s *Service) userMemoryStoreForWrite(p Principal) (*memory.Store, func(), error) {
	if s != nil {
		if owner, ok := s.executor.(sharedUserMemoryStore); ok {
			if store := owner.SharedUserMemoryStore(p.TenantID, p.UserID); store != nil {
				return store, func() { _ = store.Flush() }, nil
			}
		}
	}
	store, err := s.openUserMemoryStore(p)
	if err != nil {
		return nil, nil, err
	}
	return store, store.Stop, nil
}

func findMutableMemoryEntry(store *memory.Store, id, ownerID string) (memory.Entry, bool) {
	for _, entry := range store.List("", "") {
		if entry.ID == id && entry.OwnerID == ownerID {
			return entry, true
		}
	}
	return memory.Entry{}, false
}

func userMemoryViews(entries []memory.Entry, ownerID string) []UserMemoryView {
	out := make([]UserMemoryView, 0, len(entries))
	for _, entry := range entries {
		out = append(out, userMemoryView(entry, ownerID))
	}
	return out
}

func userMemoryView(entry memory.Entry, ownerID string) UserMemoryView {
	protected := entry.Category.IsProtected()
	readOnly := protected || entry.OwnerID == "" || entry.OwnerID != ownerID
	return UserMemoryView{
		ID:          entry.ID,
		Content:     entry.Content,
		Title:       entry.Title,
		Category:    memory.MapToCanonical(entry.Category),
		Tags:        append([]string(nil), entry.Tags...),
		CreatedAt:   entry.CreatedAt,
		UpdatedAt:   entry.UpdatedAt,
		AccessCount: entry.AccessCount,
		SourceURL:   entry.SourceURL,
		SourceType:  entry.SourceType,
		Stale:       entry.Stale,
		ReadOnly:    readOnly,
		Protected:   protected,
	}
}

func cleanMemoryTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > UserMemoryMaxTagRunes {
			return nil, fmt.Errorf("memory tag is too long, max %d characters", UserMemoryMaxTagRunes)
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) > UserMemoryMaxTags {
			return nil, fmt.Errorf("too many memory tags, max %d", UserMemoryMaxTags)
		}
	}
	return out, nil
}

func normalizeUserMemoryCategory(value string, allowEmpty bool) (memory.Category, error) {
	category := memory.MapToCanonical(memory.Category(strings.TrimSpace(value)))
	if category == "" {
		if allowEmpty {
			return "", nil
		}
		return memory.CategoryUserFact, nil
	}
	switch category {
	case memory.CategorySelfIdentity, memory.CategoryUserFact, memory.CategoryPreference, memory.CategoryProjectKnowledge, memory.CategoryInstruction, memory.CategoryConversationSummary, memory.CategorySessionCheckpoint, memory.CategoryTaskArtifact, memory.CategoryProfile:
		return category, nil
	default:
		return "", fmt.Errorf("unsupported memory category %q", value)
	}
}
