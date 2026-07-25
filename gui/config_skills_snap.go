package main

import "github.com/RapidAI/CodeClaw/corelib"

// NLSkills live in a separate atomic snapshot so:
//  1. Hot-path config snaps stay free of large skill tables
//  2. Token/provider PatchConfig does not deep-clone every skill on each write
//  3. Frontend LoadConfigForUI can omit skills (uses ListNLSkills instead)
//
// Disk format is unchanged: commitConfigToDisk reattaches skills before write.

// PeekNLSkills returns the immutable published skill table (shared slice).
// Callers MUST NOT mutate elements or the slice itself.
func (a *App) PeekNLSkills() []corelib.NLSkillEntry {
	if a == nil {
		return nil
	}
	p := a.nlSkillsSnap.Load()
	if p == nil {
		return nil
	}
	return *p
}

// publishedNLSkillsClone returns an owned shallow copy of skill entries
// (entry structs copied by value; nested step slices still shared — writers
// that mutate Steps must deep-clone that entry).
func (a *App) publishedNLSkillsClone() []corelib.NLSkillEntry {
	sk := a.PeekNLSkills()
	if len(sk) == 0 {
		return nil
	}
	return append([]corelib.NLSkillEntry(nil), sk...)
}

func (a *App) publishNLSkillsLocked(skills []corelib.NLSkillEntry) {
	if len(skills) == 0 {
		empty := []corelib.NLSkillEntry{}
		a.nlSkillsSnap.Store(&empty)
		return
	}
	// Entry-value copy into a fresh backing array; never share the caller's slice header.
	cp := append([]corelib.NLSkillEntry(nil), skills...)
	a.nlSkillsSnap.Store(&cp)
}

func (a *App) attachPublishedSkills(cfg corelib.AppConfig) corelib.AppConfig {
	if sk := a.PeekNLSkills(); len(sk) > 0 {
		cfg.NLSkills = sk
	} else if cfg.NLSkills == nil {
		cfg.NLSkills = nil
	}
	return cfg
}
