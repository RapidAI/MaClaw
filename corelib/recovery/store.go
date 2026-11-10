package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Recipe is a verified strategy for a surface: declarative parameters only,
// with the provenance of every verification and failure. A recipe is offered
// to later runs as a candidate that is verified again; one failure while
// applied invalidates it.
type Recipe struct {
	ID       string            `json:"id"`
	Surface  string            `json:"surface"`
	Method   string            `json:"method"`
	Params   map[string]string `json:"params"`
	Verified int               `json:"verified"`
	Failed   int               `json:"failed,omitempty"`
	Invalid  string            `json:"invalid,omitempty"` // why it is no longer offered
	Created  time.Time         `json:"created"`
	Last     time.Time         `json:"last_verified"`
	Evidence []Provenance      `json:"evidence,omitempty"`
}

// Provenance is one verification or failure of a recipe.
type Provenance struct {
	At    time.Time `json:"at"`
	Run   string    `json:"run,omitempty"`
	OK    bool      `json:"ok"`
	Facts string    `json:"facts,omitempty"`
	Note  string    `json:"note,omitempty"`
}

// Lineage is one conversation's current logical task: goals carry its
// generation, so a renamed title, new files, a new strategy or a resumed
// run stay the same goal; only a new instruction from the person starts a
// new generation.
type Lineage struct {
	Gen     int       `json:"gen"`
	Anchor  string    `json:"anchor"`
	Updated time.Time `json:"updated"`
}

type scopeFile struct {
	Goals    map[string]*Goal    `json:"goals"`
	Recipes  []*Recipe           `json:"recipes,omitempty"`
	Lineages map[string]*Lineage `json:"lineages,omitempty"`
}

// Store keeps one JSON file per scope (tenant+user) under Root. Every change
// is a transaction: an exclusive OS file lock (shared by every process and
// every Store instance on the host), read, change, write to a temp file,
// fsync, rename, fsync the directory. A file that cannot be read is never
// overwritten; callers fail closed.
type Store struct {
	Root string
}

func NewStore(root string) *Store { return &Store{Root: root} }

// ErrLedger: the scope's ledger is unreadable; nothing may proceed on it.
var ErrLedger = errors.New("recovery ledger unreadable")

const (
	maxGoalsPerScope    = 96
	maxRecipesPerScope  = 32
	maxLineagesPerScope = 64
	maxProvenance       = 6
)

var pathMu sync.Map // path -> *sync.Mutex (in-process; the file lock covers processes)

func (s *Store) path(scope string) string {
	sum := sha256.Sum256([]byte("recovery\x00" + scope))
	return filepath.Join(s.Root, hex.EncodeToString(sum[:12])+".json")
}

func readScope(p string) (*scopeFile, error) {
	f := &scopeFile{}
	raw, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("%w: %v", ErrLedger, err)
	case len(raw) > 8<<20:
		return nil, fmt.Errorf("%w: %s is too large", ErrLedger, filepath.Base(p))
	default:
		if err := json.Unmarshal(raw, f); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrLedger, filepath.Base(p), err)
		}
	}
	if f.Goals == nil {
		f.Goals = map[string]*Goal{}
	}
	if f.Lineages == nil {
		f.Lineages = map[string]*Lineage{}
	}
	return f, nil
}

// update runs fn on the scope's file under the lock; fn reports whether the
// file must be written. fn's error is returned after a requested write.
func (s *Store) update(scope string, fn func(f *scopeFile) (bool, error)) error {
	if s == nil || s.Root == "" || scope == "" {
		return fmt.Errorf("%w: no store", ErrLedger)
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return fmt.Errorf("%w: %v", ErrLedger, err)
	}
	p := s.path(scope)
	m, _ := pathMu.LoadOrStore(p, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	unlock, err := lockFile(p + ".lock")
	if err != nil {
		return fmt.Errorf("%w: lock: %v", ErrLedger, err)
	}
	defer unlock()
	f, err := readScope(p)
	if err != nil {
		return err
	}
	write, ferr := fn(f)
	if write {
		if err := writeScope(p, f); err != nil {
			return err
		}
	}
	return ferr
}

func writeScope(p string, f *scopeFile) error {
	if len(f.Goals) > maxGoalsPerScope {
		type kv struct {
			k string
			t time.Time
		}
		var all []kv
		for k, g := range f.Goals {
			if g.Lease != nil {
				continue // never drop a goal with an attempt in flight
			}
			all = append(all, kv{k, g.Updated})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
		for len(f.Goals) > maxGoalsPerScope && len(all) > 0 {
			delete(f.Goals, all[len(all)-1].k)
			all = all[:len(all)-1]
		}
	}
	if len(f.Recipes) > maxRecipesPerScope {
		sort.SliceStable(f.Recipes, func(i, j int) bool { return f.Recipes[i].Last.After(f.Recipes[j].Last) })
		f.Recipes = f.Recipes[:maxRecipesPerScope]
	}
	if len(f.Lineages) > maxLineagesPerScope {
		type kv struct {
			k string
			t time.Time
		}
		var all []kv
		for k, l := range f.Lineages {
			all = append(all, kv{k, l.Updated})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
		for _, x := range all[maxLineagesPerScope:] {
			delete(f.Lineages, x.k)
		}
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLedger, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp*")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLedger, err)
	}
	name := tmp.Name()
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("%w: %v", ErrLedger, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("%w: %v", ErrLedger, err)
	}
	tmp.Close()
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return fmt.Errorf("%w: %v", ErrLedger, err)
	}
	syncDir(filepath.Dir(p))
	return nil
}

func liveGoal(f *scopeFile, key string, ttl time.Duration, now time.Time) *Goal {
	g := f.Goals[key]
	if g == nil {
		return nil
	}
	// An attempt in flight or an unconfirmed side effect never expires
	// silently.
	if ttl > 0 && now.Sub(g.Updated) > ttl && g.Lease == nil && len(g.Steps) == 0 && g.State != StateStopped {
		delete(f.Goals, key)
		return nil
	}
	return g
}

// Goal returns a copy of the goal's ledger, nil when none or expired.
func (s *Store) Goal(scope, key string, ttl time.Duration, now time.Time) (*Goal, error) {
	var out *Goal
	err := s.update(scope, func(f *scopeFile) (bool, error) {
		if g := liveGoal(f, key, ttl, now); g != nil {
			out = g.clone()
		}
		return false, nil
	})
	return out, err
}

// Goals lists the live goals whose key starts with prefix.
func (s *Store) Goals(scope, prefix string, ttl time.Duration, now time.Time) ([]*Goal, error) {
	var out []*Goal
	err := s.update(scope, func(f *scopeFile) (bool, error) {
		for k := range f.Goals {
			if strings.HasPrefix(k, prefix) {
				if g := liveGoal(f, k, ttl, now); g != nil {
					out = append(out, g.clone())
				}
			}
		}
		return false, nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, err
}

func (g *Goal) clone() *Goal {
	raw, _ := json.Marshal(g)
	var cp Goal
	_ = json.Unmarshal(raw, &cp)
	cp.Scope = g.Scope
	return &cp
}

// Recipes lists the recipes still offered for surface+method, newest first.
func (s *Store) Recipes(scope, surface, method string, maxAge time.Duration, now time.Time) []Recipe {
	var out []Recipe
	_ = s.update(scope, func(f *scopeFile) (bool, error) {
		for _, r := range f.Recipes {
			if r.Surface != surface || r.Method != method || r.Invalid != "" || r.Verified < 1 {
				continue
			}
			if maxAge > 0 && now.Sub(r.Last) > maxAge {
				continue
			}
			out = append(out, *r)
		}
		return false, nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

// AllRecipes lists every recipe of a scope (offered or not).
func (s *Store) AllRecipes(scope string) []Recipe {
	var out []Recipe
	_ = s.update(scope, func(f *scopeFile) (bool, error) {
		for _, r := range f.Recipes {
			out = append(out, *r)
		}
		return false, nil
	})
	return out
}

func recipeID(surface, method, sig string) string {
	sum := sha256.Sum256([]byte(surface + "\x00" + method + "\x00" + sig))
	return "r" + hex.EncodeToString(sum[:5])
}

// verifyRecipe records a verified outcome reached with o.Params on
// o.Surface (inside the goal's transaction).
func verifyRecipe(f *scopeFile, o Observation, now time.Time) *Recipe {
	id := recipeID(o.Surface, o.Method, Sig(o.Params))
	var r *Recipe
	for _, x := range f.Recipes {
		if x.ID == id {
			r = x
		}
	}
	if r == nil {
		params := map[string]string{}
		for k, v := range o.Params {
			params[k] = v
		}
		r = &Recipe{ID: id, Surface: o.Surface, Method: o.Method, Params: params, Created: now}
		f.Recipes = append(f.Recipes, r)
	}
	r.Verified++
	r.Invalid = ""
	r.Last = now
	r.Evidence = appendProv(r.Evidence, Provenance{At: now, Run: o.Run, OK: true, Facts: clip(factText(o.Facts), 200)})
	cp := *r
	return &cp
}

// failRecipe retires a recipe whose application did not verify.
func failRecipe(f *scopeFile, id, run, note string, now time.Time) {
	for _, r := range f.Recipes {
		if r.ID == id {
			r.Failed++
			r.Invalid = fmt.Sprintf("failed when applied at %s: %s", now.UTC().Format(time.RFC3339), clip(note, 120))
			r.Evidence = appendProv(r.Evidence, Provenance{At: now, Run: run, OK: false, Note: clip(note, 160)})
		}
	}
}

func appendProv(p []Provenance, x Provenance) []Provenance {
	p = append(p, x)
	if len(p) > maxProvenance {
		p = p[len(p)-maxProvenance:]
	}
	return p
}

// ScopeKey joins tenant and user (the only isolation key).
func ScopeKey(tenant, user string) string {
	return strings.TrimSpace(tenant) + "\x00" + strings.TrimSpace(user)
}
