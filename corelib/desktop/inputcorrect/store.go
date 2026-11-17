package inputcorrect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A procedure is one confirmed input strategy plus the conditions it was
// confirmed under. It stores no typed text, recipient, cookie, key,
// coordinate, or window title. Remembering it does not grant an approval.

const procedureVersion = 1

// Conditions are the environment the strategy was confirmed in.
type Conditions struct {
	Environment   string
	FieldClass    string
	LayoutScale   int
	Semantic      bool
	Clipboard     bool
	Keys          bool
	EditorReplace bool
}

// Procedure is one stored strategy.
type Procedure struct {
	Strategy       string  `json:"strategy"`
	Environment    string  `json:"environment"`
	FieldClass     string  `json:"field_class"`
	LayoutScale    int     `json:"layout_scale"`
	NeedsSemantic  bool    `json:"needs_semantic,omitempty"`
	NeedsClipboard bool    `json:"needs_clipboard,omitempty"`
	NeedsKeys      bool    `json:"needs_keys,omitempty"`
	NeedsEditor    bool    `json:"needs_editor_replace,omitempty"`
	Provenance     string  `json:"provenance"`
	Successes      int     `json:"successes"`
	Failures       int     `json:"failures"`
	Confidence     float64 `json:"confidence"`
	Demoted        bool    `json:"demoted,omitempty"`
	Updated        int64   `json:"updated"`
}

// Scope isolates procedures by tenant, owner, and bot.
type Scope struct {
	Tenant string
	Owner  string
	Bot    string
}

type procedureFile struct {
	Version    int         `json:"version"`
	Procedures []Procedure `json:"procedures"`
}

// Store is one scope's file. Open does not create the file.
type Store struct {
	dir   string
	scope Scope
	path  string
}

// Open returns the store for scope. dir is the root directory.
func Open(dir string, scope Scope) *Store {
	sum := sha256.Sum256([]byte(strings.TrimSpace(scope.Tenant) + "\x00" + strings.TrimSpace(scope.Owner) + "\x00" + strings.TrimSpace(scope.Bot)))
	return &Store{dir: dir, scope: scope, path: filepath.Join(dir, hex.EncodeToString(sum[:12])+".json")}
}

func (s *Store) with(fn func(*procedureFile) error) error {
	if s == nil || s.dir == "" {
		return os.ErrInvalid
	}
	unlock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	f := s.load()
	if err := fn(f); err != nil {
		return err
	}
	return s.save(f)
}

func (s *Store) load() *procedureFile {
	f := &procedureFile{Version: procedureVersion}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return f
	}
	_ = json.Unmarshal(raw, f)
	f.Version = procedureVersion
	out := f.Procedures[:0]
	for _, p := range f.Procedures {
		if !AllowedKind(p.Strategy) || p.Strategy == ActionClearField {
			continue
		}
		if p.Provenance == "" {
			p.Provenance = "exact_readback"
		}
		out = append(out, p)
	}
	f.Procedures = out
	return f
}

func (s *Store) save(f *procedureFile) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	syncDir(s.dir)
	return nil
}

func needsOf(strategy string) (sem, clip, keys, editor bool) {
	switch strategy {
	case StrategySemantic:
		return true, false, false, false
	case StrategyClipboard:
		return false, true, false, false
	case StrategySegmented:
		return false, false, true, false
	case StrategyEditorReplace:
		return false, false, false, true
	}
	return false, false, false, false
}

func (c Conditions) complete() bool {
	return c.Environment != "" && c.LayoutScale > 0 && (c.FieldClass == FieldPlain || c.FieldClass == FieldRich)
}

func sameCond(p Procedure, c Conditions) bool {
	return p.Environment == c.Environment && p.FieldClass == c.FieldClass && p.LayoutScale == c.LayoutScale
}

func capsMatch(p Procedure, c Conditions) bool {
	if p.NeedsSemantic && !c.Semantic {
		return false
	}
	if p.NeedsClipboard && !c.Clipboard {
		return false
	}
	if p.NeedsKeys && !c.Keys {
		return false
	}
	if p.NeedsEditor && !c.EditorReplace {
		return false
	}
	return true
}

// Confirm records an exact read-back of strategy under cond.
// Incomplete conditions (unknown layout scale or environment) are not stored.
func (s *Store) Confirm(strategy string, cond Conditions) {
	if s == nil || !AllowedKind(strategy) || strategy == ActionClearField || !cond.complete() {
		return
	}
	_ = s.with(func(f *procedureFile) error {
		now := time.Now().Unix()
		sem, clip, keys, editor := needsOf(strategy)
		for i := range f.Procedures {
			p := &f.Procedures[i]
			if p.Strategy == strategy && sameCond(*p, cond) {
				p.Successes++
				p.Failures = 0
				p.Demoted = false
				p.Confidence = 1
				p.Provenance = "exact_readback"
				p.Updated = now
				p.NeedsSemantic, p.NeedsClipboard, p.NeedsKeys, p.NeedsEditor = sem, clip, keys, editor
				return nil
			}
		}
		f.Procedures = append(f.Procedures, Procedure{
			Strategy: strategy, Environment: cond.Environment, FieldClass: cond.FieldClass, LayoutScale: cond.LayoutScale,
			NeedsSemantic: sem, NeedsClipboard: clip, NeedsKeys: keys, NeedsEditor: editor,
			Provenance: "exact_readback", Successes: 1, Confidence: 1, Updated: now,
		})
		return nil
	})
}

// Demote marks a previously stored strategy failed under cond.
// A strategy that was never confirmed is left unstored.
func (s *Store) Demote(strategy string, cond Conditions) {
	if s == nil || !cond.complete() {
		return
	}
	_ = s.with(func(f *procedureFile) error {
		now := time.Now().Unix()
		for i := range f.Procedures {
			p := &f.Procedures[i]
			if p.Strategy == strategy && sameCond(*p, cond) {
				p.Failures++
				p.Demoted = true
				total := p.Successes + p.Failures
				if total > 0 {
					p.Confidence = float64(p.Successes) / float64(total)
				}
				p.Updated = now
			}
		}
		return nil
	})
}

// Best is the highest-confidence procedure whose conditions match and
// which is not demoted.
func (s *Store) Best(cond Conditions) (Procedure, bool) {
	if s == nil || !cond.complete() {
		return Procedure{}, false
	}
	var best Procedure
	found := false
	_ = s.withRead(func(f *procedureFile) {
		for _, p := range f.Procedures {
			if p.Demoted || !sameCond(p, cond) || !capsMatch(p, cond) {
				continue
			}
			if !found || p.Confidence > best.Confidence || (p.Confidence == best.Confidence && p.Updated > best.Updated) {
				best, found = p, true
			}
		}
	})
	return best, found
}

// IsDemoted reports whether strategy is stored and demoted for cond.
func (s *Store) IsDemoted(strategy string, cond Conditions) bool {
	if s == nil {
		return false
	}
	demoted := false
	_ = s.withRead(func(f *procedureFile) {
		for _, p := range f.Procedures {
			if p.Strategy == strategy && sameCond(p, cond) && p.Demoted {
				demoted = true
			}
		}
	})
	return demoted
}

func (s *Store) withRead(fn func(*procedureFile)) error {
	if s == nil || s.dir == "" {
		return os.ErrInvalid
	}
	unlock, err := lockFile(s.path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	fn(s.load())
	return nil
}

// Rewrite drops JSON keys this store does not own (drafts, secrets,
// approvals, coordinates) by loading and saving the known fields only.
func (s *Store) Rewrite() error {
	return s.with(func(*procedureFile) error { return nil })
}
