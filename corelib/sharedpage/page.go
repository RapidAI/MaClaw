// Package sharedpage stores versioned co-editable output Pages that a bot
// group and the user can read, append, and edit by section.
package sharedpage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const storeFileName = "shared_pages.json"

// Section is one titled block inside a Page.
type Section struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	UpdatedBy string `json:"updated_by,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// Version is an immutable snapshot after a mutation.
type Version struct {
	Number    int       `json:"number"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	SectionID string    `json:"section_id,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	At        time.Time `json:"at"`
}

// Page is a shared document.
type Page struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	GroupID     string    `json:"group_id,omitempty"`
	Sections    []Section `json:"sections"`
	Versions    []Version `json:"versions"`
	Version     int       `json:"version"`
	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type fileStore struct {
	Version int             `json:"version"`
	Pages   map[string]Page `json:"pages"`
}

// Store is a JSON-backed shared page repository under dataDir.
type Store struct {
	mu   sync.Mutex
	path string
	data map[string]Page
	now  func() time.Time
	newID func() string
}

// Open loads or creates the store.
func Open(dataDir string) (*Store, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("data dir is required")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dataDir, storeFileName),
		data: map[string]Page{},
		now:  time.Now,
		newID: func() string {
			return fmt.Sprintf("sp-%d", time.Now().UnixNano())
		},
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var file fileStore
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if file.Pages != nil {
		s.data = file.Pages
	}
	return s, nil
}

func (s *Store) persistLocked() error {
	file := fileStore{Version: 1, Pages: s.data}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func clonePage(p Page) Page {
	cp := p
	cp.Sections = append([]Section(nil), p.Sections...)
	cp.Versions = append([]Version(nil), p.Versions...)
	return cp
}

// Create starts a new shared page.
func (s *Store) Create(title, groupID, actor string) (Page, error) {
	if s == nil {
		return Page{}, fmt.Errorf("store is nil")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return Page{}, fmt.Errorf("title is required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "user"
	}
	now := s.now()
	p := Page{
		ID:        s.newID(),
		Title:     title,
		GroupID:   strings.TrimSpace(groupID),
		Sections:  []Section{},
		Versions:  []Version{{Number: 1, Actor: actor, Action: "create", Summary: title, At: now}},
		Version:   1,
		CreatedBy: actor,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[p.ID] = p
	if err := s.persistLocked(); err != nil {
		return Page{}, err
	}
	return clonePage(p), nil
}

// Get returns one page.
func (s *Store) Get(id string) (Page, error) {
	if s == nil {
		return Page{}, fmt.Errorf("store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[strings.TrimSpace(id)]
	if !ok {
		return Page{}, fmt.Errorf("page not found")
	}
	return clonePage(p), nil
}

// List returns pages newest-first, optionally filtered by groupID.
func (s *Store) List(groupID string) []Page {
	if s == nil {
		return nil
	}
	groupID = strings.TrimSpace(groupID)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Page, 0, len(s.data))
	for _, p := range s.data {
		if groupID != "" && p.GroupID != groupID {
			continue
		}
		out = append(out, clonePage(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (s *Store) bump(p *Page, actor, action, sectionID, summary string) {
	p.Version++
	now := s.now()
	p.UpdatedAt = now
	p.Versions = append(p.Versions, Version{
		Number: p.Version, Actor: actor, Action: action, SectionID: sectionID, Summary: summary, At: now,
	})
}

// AppendSection adds a section at the end.
func (s *Store) AppendSection(pageID, title, body, actor string) (Page, error) {
	if s == nil {
		return Page{}, fmt.Errorf("store is nil")
	}
	pageID = strings.TrimSpace(pageID)
	title = strings.TrimSpace(title)
	actor = strings.TrimSpace(actor)
	if pageID == "" {
		return Page{}, fmt.Errorf("page id is required")
	}
	if title == "" {
		return Page{}, fmt.Errorf("section title is required")
	}
	if actor == "" {
		actor = "user"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[pageID]
	if !ok {
		return Page{}, fmt.Errorf("page not found")
	}
	now := s.now().Format(time.RFC3339)
	sec := Section{
		ID:        fmt.Sprintf("sec-%d", len(p.Sections)+1),
		Title:     title,
		Body:      body,
		UpdatedBy: actor,
		UpdatedAt: now,
	}
	// Prefer stable unique ids.
	sec.ID = fmt.Sprintf("sec-%d", s.now().UnixNano())
	p.Sections = append(p.Sections, sec)
	s.bump(&p, actor, "append", sec.ID, title)
	s.data[pageID] = p
	if err := s.persistLocked(); err != nil {
		return Page{}, err
	}
	return clonePage(p), nil
}

// EditSection replaces title/body of an existing section.
func (s *Store) EditSection(pageID, sectionID, title, body, actor string) (Page, error) {
	if s == nil {
		return Page{}, fmt.Errorf("store is nil")
	}
	pageID = strings.TrimSpace(pageID)
	sectionID = strings.TrimSpace(sectionID)
	actor = strings.TrimSpace(actor)
	if pageID == "" || sectionID == "" {
		return Page{}, fmt.Errorf("page id and section id are required")
	}
	if actor == "" {
		actor = "user"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data[pageID]
	if !ok {
		return Page{}, fmt.Errorf("page not found")
	}
	found := false
	for i := range p.Sections {
		if p.Sections[i].ID != sectionID {
			continue
		}
		if strings.TrimSpace(title) != "" {
			p.Sections[i].Title = strings.TrimSpace(title)
		}
		p.Sections[i].Body = body
		p.Sections[i].UpdatedBy = actor
		p.Sections[i].UpdatedAt = s.now().Format(time.RFC3339)
		found = true
		break
	}
	if !found {
		return Page{}, fmt.Errorf("section not found")
	}
	s.bump(&p, actor, "edit", sectionID, strings.TrimSpace(title))
	s.data[pageID] = p
	if err := s.persistLocked(); err != nil {
		return Page{}, err
	}
	return clonePage(p), nil
}

// Delete removes a page.
func (s *Store) Delete(pageID string) error {
	if s == nil {
		return fmt.Errorf("store is nil")
	}
	pageID = strings.TrimSpace(pageID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[pageID]; !ok {
		return fmt.Errorf("page not found")
	}
	delete(s.data, pageID)
	return s.persistLocked()
}

// ToolSpecs returns JSON-ish tool descriptors bots can call.
func ToolSpecs() []map[string]any {
	return []map[string]any{
		{
			"name":        "shared_page_read",
			"description": "Read a shared output Page (title, sections, version) by page_id.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"page_id": map[string]any{"type": "string"},
				},
				"required": []string{"page_id"},
			},
		},
		{
			"name":        "shared_page_append",
			"description": "Append a titled section to a shared output Page.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"page_id": map[string]any{"type": "string"},
					"title":   map[string]any{"type": "string"},
					"body":    map[string]any{"type": "string"},
				},
				"required": []string{"page_id", "title"},
			},
		},
		{
			"name":        "shared_page_edit",
			"description": "Edit an existing section of a shared output Page.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"page_id":    map[string]any{"type": "string"},
					"section_id": map[string]any{"type": "string"},
					"title":      map[string]any{"type": "string"},
					"body":       map[string]any{"type": "string"},
				},
				"required": []string{"page_id", "section_id"},
			},
		},
		{
			"name":        "shared_page_list",
			"description": "List shared output Pages, optionally filtered by group_id.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_id": map[string]any{"type": "string"},
				},
			},
		},
	}
}
