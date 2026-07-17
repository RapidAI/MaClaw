package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
)

// Shared allowlist for AI-assistant install slash commands.
// Source of truth (also imported by the frontend):
//
//	frontend/src/components/ai/installCommandAllowlist.json
//
//go:embed frontend/src/components/ai/installCommandAllowlist.json
var installCommandAllowlistJSON []byte

// Nested action groups may carry aliases (e.g. market → marketplace).
type installNestedSpec struct {
	Aliases []string `json:"aliases"`
	Actions []string `json:"actions"`
}

type installCommandSpec struct {
	Aliases []string                     `json:"aliases"`
	Actions []string                     `json:"actions"`
	Nested  map[string]installNestedSpec `json:"nested"`
}

type installCommandAllowlistFile struct {
	Version        int                           `json:"version"`
	BinaryPrefixes []string                      `json:"binary_prefixes"`
	MetaActions    []string                      `json:"meta_actions"`
	Commands       map[string]installCommandSpec `json:"commands"`
}

type installCommandAllowlist struct {
	// canonical command name → action set
	actions map[string]map[string]struct{}
	// nested[cmd][action] → sub-action set (e.g. plugin/marketplace/add)
	nested map[string]map[string]map[string]struct{}
	// alias or canonical → canonical
	normalize map[string]string
	// meta actions always allowed (help flags)
	meta map[string]struct{}
	// CLI binary first tokens (without .exe)
	binaries map[string]struct{}
}

var (
	installAllowlistOnce sync.Once
	installAllowlistVal  *installCommandAllowlist
	installAllowlistErr  error
)

func loadInstallCommandAllowlist() (*installCommandAllowlist, error) {
	installAllowlistOnce.Do(func() {
		var raw installCommandAllowlistFile
		if err := json.Unmarshal(installCommandAllowlistJSON, &raw); err != nil {
			installAllowlistErr = fmt.Errorf("parse install command allowlist: %w", err)
			return
		}
		if len(raw.Commands) == 0 {
			installAllowlistErr = fmt.Errorf("install command allowlist has no commands")
			return
		}
		al := &installCommandAllowlist{
			actions:   make(map[string]map[string]struct{}, len(raw.Commands)),
			nested:    make(map[string]map[string]map[string]struct{}),
			normalize: make(map[string]string),
			meta:      installAllowlistStringSet(raw.MetaActions),
			binaries:  make(map[string]struct{}, len(raw.BinaryPrefixes)),
		}
		for _, b := range raw.BinaryPrefixes {
			b = strings.ToLower(strings.TrimSpace(b))
			b = strings.TrimSuffix(b, ".exe")
			if b != "" {
				al.binaries[b] = struct{}{}
			}
		}
		for name, spec := range raw.Commands {
			canonical := strings.ToLower(strings.TrimSpace(name))
			if canonical == "" {
				continue
			}
			al.normalize[canonical] = canonical
			for _, alias := range spec.Aliases {
				a := strings.ToLower(strings.TrimSpace(alias))
				if a != "" {
					al.normalize[a] = canonical
				}
			}
			acts := installAllowlistStringSet(spec.Actions)
			for m := range al.meta {
				acts[m] = struct{}{}
			}

			if len(spec.Nested) > 0 {
				nestedActs := make(map[string]map[string]struct{}, len(spec.Nested)*2)
				for parent, nestedSpec := range spec.Nested {
					p := strings.ToLower(strings.TrimSpace(parent))
					if p == "" {
						continue
					}
					set := installAllowlistStringSet(nestedSpec.Actions)
					for m := range al.meta {
						set[m] = struct{}{}
					}
					// Nested parents (and aliases) are also top-level actions,
					// so JSON need not list "marketplace" twice.
					acts[p] = struct{}{}
					nestedActs[p] = set
					for _, alias := range nestedSpec.Aliases {
						a := strings.ToLower(strings.TrimSpace(alias))
						if a == "" {
							continue
						}
						acts[a] = struct{}{}
						nestedActs[a] = set
					}
				}
				al.nested[canonical] = nestedActs
			}
			al.actions[canonical] = acts
		}
		installAllowlistVal = al
	})
	return installAllowlistVal, installAllowlistErr
}

func installAllowlistStringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			out[v] = struct{}{}
		}
	}
	return out
}

func emptyInstallAllowlist() *installCommandAllowlist {
	return &installCommandAllowlist{
		actions:   map[string]map[string]struct{}{},
		nested:    map[string]map[string]map[string]struct{}{},
		normalize: map[string]string{},
		meta:      map[string]struct{}{},
		binaries:  map[string]struct{}{},
	}
}

var installAllowlistFailLogged sync.Once

func mustInstallAllowlist() *installCommandAllowlist {
	al, err := loadInstallCommandAllowlist()
	if err != nil || al == nil {
		installAllowlistFailLogged.Do(func() {
			// Avoid spamming; one warning is enough for operators.
			log.Printf("[install-allowlist] failed to load shared allowlist: %v", err)
		})
		// Fail closed: empty allowlist rejects everything.
		return emptyInstallAllowlist()
	}
	return al
}

// isKnownInstallCommand reports whether name (or alias) is a known install command.
func isKnownInstallCommand(name string) bool {
	al := mustInstallAllowlist()
	_, ok := al.normalize[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// isInstallMetaAction reports whether action is a global help/meta flag.
func isInstallMetaAction(action string) bool {
	al := mustInstallAllowlist()
	_, ok := al.meta[strings.ToLower(strings.TrimSpace(action))]
	return ok
}

// normalizeInstallCmd maps aliases (skills/plugins) to canonical command names.
func normalizeInstallCmd(head string) string {
	al := mustInstallAllowlist()
	key := strings.ToLower(strings.TrimSpace(head))
	if canon, ok := al.normalize[key]; ok {
		return canon
	}
	return key
}

// isInstallCLIBinaryPrefix reports whether token is a known CLI binary name
// (maclaw-tui, maclaw.exe, …) used when pasting full CLI lines into chat.
// Accepts bare names and path basenames (e.g. C:\bin\maclaw-tui.exe).
func isInstallCLIBinaryPrefix(token string) bool {
	al := mustInstallAllowlist()
	t := installCLIBinaryBaseName(token)
	_, ok := al.binaries[t]
	return ok
}

func installCLIBinaryBaseName(token string) string {
	t := strings.ToLower(strings.TrimSpace(token))
	// Strip surrounding quotes from paste.
	if len(t) >= 2 {
		if (t[0] == '"' && t[len(t)-1] == '"') || (t[0] == '\'' && t[len(t)-1] == '\'') {
			t = t[1 : len(t)-1]
		}
	}
	t = strings.TrimSuffix(t, ".exe")
	if i := strings.LastIndexAny(t, `/\`); i >= 0 {
		t = t[i+1:]
	}
	t = strings.TrimSuffix(t, ".exe")
	return strings.TrimSpace(t)
}

// isInstallNestedParent reports whether action is a nested parent for cmd
// (e.g. plugin + marketplace / market).
func isInstallNestedParent(cmd, action string) bool {
	al := mustInstallAllowlist()
	cmd = normalizeInstallCmd(cmd)
	action = strings.ToLower(strings.TrimSpace(action))
	nested, ok := al.nested[cmd]
	if !ok {
		return false
	}
	_, ok = nested[action]
	return ok
}

// installActionAllowed reports whether cmd+args is an allowed install slash action.
// cmd may be an alias; it is normalized internally.
func installActionAllowed(cmd string, args []string) bool {
	if len(args) == 0 {
		return false
	}
	al := mustInstallAllowlist()
	cmd = normalizeInstallCmd(cmd)
	action := strings.ToLower(strings.TrimSpace(args[0]))

	acts, ok := al.actions[cmd]
	if !ok {
		return false
	}
	// Meta help is allowed only for known install commands.
	if _, ok := al.meta[action]; ok {
		return true
	}
	if _, ok := acts[action]; !ok {
		return false
	}

	// Nested subcommands (e.g. plugin marketplace add).
	if nested, ok := al.nested[cmd]; ok {
		if subSet, ok := nested[action]; ok {
			// Bare `/plugin marketplace` is allowed (shows usage).
			if len(args) == 1 {
				return true
			}
			sub := strings.ToLower(strings.TrimSpace(args[1]))
			_, ok := subSet[sub]
			return ok
		}
	}
	return true
}
