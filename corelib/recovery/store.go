package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type scopeFile struct {
	Goals   map[string]*Goal `json:"goals"`
	Recipes []*Recipe        `json:"recipes,omitempty"`
}

// Store keeps one JSON file per scope (tenant+user) under Root: goal
// ledgers (for budgets across runs and resumes) and verified recipes. One
// scope never reads another's file.
type Store struct {
	Root string
	mu   sync.Mutex
}

func NewStore(root string) *Store { return &Store{Root: root} }

const (
	maxGoalsPerScope   = 64
	maxRecipesPerScope = 32
	maxProvenance      = 6
)

func (s *Store) path(scope string) string {
	sum := sha256.Sum256([]byte("recovery\x00" + scope))
	return filepath.Join(s.Root, hex.EncodeToString(sum[:12])+".json")
}

func (s *Store) load(scope string) *scopeFile {
	f := &scopeFile{Goals: map[string]*Goal{}}
	if s == nil || s.Root == "" || scope == "" {
		return f
	}
	raw, err := os.ReadFile(s.path(scope))
	if err == nil && len(raw) < 4<<20 {
		_ = json.Unmarshal(raw, f)
	}
	if f.Goals == nil {
		f.Goals = map[string]*Goal{}
	}
	return f
}

func (s *Store) save(scope string, f *scopeFile) {
	if s == nil || s.Root == "" || scope == "" {
		return
	}
	if len(f.Goals) > maxGoalsPerScope {
		type kv struct {
			k string
			t time.Time
		}
		var all []kv
		for k, g := range f.Goals {
			all = append(all, kv{k, g.Updated})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.After(all[j].t) })
		for _, x := range all[maxGoalsPerScope:] {
			delete(f.Goals, x.k)
		}
	}
	if len(f.Recipes) > maxRecipesPerScope {
		sort.SliceStable(f.Recipes, func(i, j int) bool { return f.Recipes[i].Last.After(f.Recipes[j].Last) })
		f.Recipes = f.Recipes[:maxRecipesPerScope]
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return
	}
	if os.MkdirAll(s.Root, 0o700) != nil {
		return
	}
	p := s.path(scope)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// Goal returns a copy of the goal's ledger, nil when none or expired.
func (s *Store) Goal(scope, key string, ttl time.Duration, now time.Time) *Goal {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load(scope)
	g := f.Goals[key]
	if g == nil {
		return nil
	}
	if ttl > 0 && now.Sub(g.Updated) > ttl {
		return nil
	}
	cp := *g
	cp.Scope = scope
	return &cp
}

// Put writes a goal's ledger.
func (s *Store) Put(g *Goal) {
	if g == nil || g.Scope == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load(g.Scope)
	f.Goals[g.Key] = g
	s.save(g.Scope, f)
}

// Recipes lists the recipes still offered for surface+method, newest first.
func (s *Store) Recipes(scope, surface, method string, maxAge time.Duration, now time.Time) []Recipe {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Recipe
	for _, r := range s.load(scope).Recipes {
		if r.Surface != surface || r.Method != method || r.Invalid != "" || r.Verified < 1 {
			continue
		}
		if maxAge > 0 && now.Sub(r.Last) > maxAge {
			continue
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out
}

// AllRecipes lists every recipe of a scope (offered or not).
func (s *Store) AllRecipes(scope string) []Recipe {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Recipe
	for _, r := range s.load(scope).Recipes {
		out = append(out, *r)
	}
	return out
}

func recipeID(surface, method, sig string) string {
	sum := sha256.Sum256([]byte(surface + "\x00" + method + "\x00" + sig))
	return "r" + hex.EncodeToString(sum[:5])
}

// VerifyRecipe records a verified outcome reached with o.Params on
// o.Surface: a new recipe, or one more verification of the same one.
// Recipes for the same surface and method with other parameters are not
// deleted; the one verified most recently is offered first.
func (s *Store) VerifyRecipe(scope string, o Observation, now time.Time) *Recipe {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load(scope)
	sig := Sig(o.Params)
	id := recipeID(o.Surface, o.Method, sig)
	var r *Recipe
	for _, x := range f.Recipes {
		if x.ID == id {
			r = x
		}
	}
	if r == nil {
		r = &Recipe{ID: id, Surface: o.Surface, Method: o.Method, Params: o.Params, Created: now}
		f.Recipes = append(f.Recipes, r)
	}
	r.Verified++
	r.Invalid = ""
	r.Last = now
	r.Evidence = appendProv(r.Evidence, Provenance{At: now, Run: o.Run, OK: true, Facts: clip(factText(o.Facts), 200)})
	s.save(scope, f)
	cp := *r
	return &cp
}

// FailRecipe invalidates a recipe whose application did not verify.
func (s *Store) FailRecipe(scope, id, note string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load(scope)
	for _, r := range f.Recipes {
		if r.ID == id {
			r.Failed++
			r.Invalid = fmt.Sprintf("failed when applied at %s: %s", now.UTC().Format(time.RFC3339), clip(note, 120))
			r.Evidence = appendProv(r.Evidence, Provenance{At: now, OK: false, Note: clip(note, 160)})
		}
	}
	s.save(scope, f)
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
