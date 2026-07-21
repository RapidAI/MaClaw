package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSkillExecutorListUsesScanCacheUntilInvalidated(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	externalDir := filepath.Join(tmpHome, "skills")
	if err := os.MkdirAll(filepath.Join(externalDir, "first"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "first", "skill.yaml"), []byte("name: first\ndescription: first skill\ntriggers: [first]\nsteps:\n  - action: bash\n    params:\n      command: echo first\nstatus: active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.ExternalSkillDirs = []string{externalDir}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	exec := NewSkillExecutor(app, nil, nil)
	if got := exec.List(); len(got) == 0 || got[0].Name != "first" {
		t.Fatalf("initial List() = %+v, want first skill", got)
	}
	if err := os.MkdirAll(filepath.Join(externalDir, "second"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "second", "skill.yaml"), []byte("name: second\ndescription: second skill\ntriggers: [second]\nsteps:\n  - action: bash\n    params:\n      command: echo second\nstatus: active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := exec.List(); len(got) != 1 {
		t.Fatalf("cached List() len = %d, want 1 before invalidation", len(got))
	}
	exec.invalidateSkillCache()
	if got := exec.List(); len(got) != 2 {
		t.Fatalf("List() after invalidation len = %d, want 2", len(got))
	}

	cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: "manual", Description: "manual", Status: "active"})
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := exec.List(); len(got) != 3 {
		t.Fatalf("List() after config key change len = %d, want 3", len(got))
	}
}

func TestSkillExecutorDoesNotForegroundScanWhileAppScannerWarms(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome, cachedSkillScanner: &CachedSkillScanner{}}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	externalDir := filepath.Join(tmpHome, "skills")
	if err := os.MkdirAll(filepath.Join(externalDir, "first"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "first", "skill.yaml"), []byte("name: first\ndescription: first skill\ntriggers: [first]\nsteps:\n  - action: bash\n    params:\n      command: echo first\nstatus: active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.ExternalSkillDirs = []string{externalDir}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	exec := NewSkillExecutor(app, nil, nil)
	if got := exec.List(); len(got) != 0 {
		t.Fatalf("List while scanner warms = %+v, want no foreground scan result", got)
	}

	app.cachedSkillScanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{{Name: "first", Description: "first skill", Status: "active", SkillDir: filepath.Join(externalDir, "first")}},
		createdAt: time.Now(),
	})
	if got := exec.List(); len(got) != 1 || got[0].Name != "first" {
		t.Fatalf("List after scanner ready = %+v, want first skill", got)
	}
}

func TestSkillExecutorCacheFollowsScannerVersion(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	skillDir := filepath.Join(tmpHome, "skills", "demo")
	scanner := &CachedSkillScanner{}
	scanner.cache.Store(&skillCacheEntry{
		skills: []corelib.NLSkillEntry{{
			Name:     "demo",
			Status:   "active",
			SkillDir: skillDir,
			Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "node old.js"}}},
		}},
		createdAt: time.Now(),
	})
	scanner.version.Store(1)
	app := &App{testHomeDir: tmpHome, cachedSkillScanner: scanner}
	exec := NewSkillExecutor(app, nil, nil)

	got := exec.List()
	if len(got) != 1 {
		t.Fatalf("initial List() = %+v, want one skill", got)
	}
	cmd, _ := got[0].Steps[0].Params["command"].(string)
	if cmd != "node old.js" {
		t.Fatalf("initial command = %q, want old command", cmd)
	}

	scanner.cache.Store(&skillCacheEntry{
		skills: []corelib.NLSkillEntry{{
			Name:     "demo",
			Status:   "active",
			SkillDir: skillDir,
			Steps:    []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "node new.js"}}},
		}},
		createdAt: time.Now(),
	})
	scanner.version.Add(1)

	got = exec.List()
	if len(got) != 1 {
		t.Fatalf("List() after scanner refresh = %+v, want one skill", got)
	}
	cmd, _ = got[0].Steps[0].Params["command"].(string)
	if cmd != "node new.js" {
		t.Fatalf("command after scanner refresh = %q, want new command", cmd)
	}
}

func TestAddSkillInvalidatesCachedSkillScanner(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	scanner := &CachedSkillScanner{}
	scanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{{Name: "old", Description: "old skill", Status: "active"}},
		createdAt: time.Now(),
	})
	scanner.scanning.Store(true)
	app := &App{testHomeDir: tmpHome, cachedSkillScanner: scanner}

	if err := app.AddSkill("new", "new skill", "address", "local-new", "claude"); err != nil {
		t.Fatalf("AddSkill() error = %v", err)
	}

	entry := scanner.cache.Load()
	if entry == nil || !entry.stale {
		t.Fatalf("cached scanner entry = %#v, want stale after AddSkill", entry)
	}
}

func TestCachedSkillScannerUpsertSkillsSurfacesImmediatelyAndSurvivesScan(t *testing.T) {
	scanner := &CachedSkillScanner{
		roots: []string{t.TempDir()}, // empty root: disk scan finds nothing
	}
	scanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{{Name: "existing", Status: "active", SkillDir: filepath.Join(t.TempDir(), "existing")}},
		createdAt: time.Now(),
		stale:     false,
	})

	importedDir := filepath.Join(t.TempDir(), "fresh")
	scanner.UpsertSkills([]corelib.NLSkillEntry{{
		Name:        "fresh",
		Description: "from upsert",
		Status:      "active",
		SkillDir:    importedDir,
	}})

	got := scanner.Get()
	if len(got) != 2 {
		t.Fatalf("Get() after Upsert len = %d, want 2", len(got))
	}
	found := false
	for _, s := range got {
		if s.Name == "fresh" && s.SkillDir == importedDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("Get() = %+v, want fresh skill", got)
	}
	entry := scanner.cache.Load()
	if entry == nil || !entry.stale {
		t.Fatalf("cache entry = %#v, want stale after Upsert", entry)
	}
	if len(scanner.pendingUpserts) != 1 {
		t.Fatalf("pendingUpserts = %d, want 1", len(scanner.pendingUpserts))
	}

	// Concurrent-style scan that misses the new dir must still keep the pending upsert.
	scanner.scan()
	got = scanner.Get()
	found = false
	for _, s := range got {
		if s.Name == "fresh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Get() after scan = %+v, want pending upsert retained", got)
	}
	if len(scanner.pendingUpserts) != 0 {
		t.Fatalf("pendingUpserts after scan = %d, want 0", len(scanner.pendingUpserts))
	}
}

func TestCachedSkillScannerRemoveByDirCancelsPendingUpsert(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skill-a")
	scanner := &CachedSkillScanner{}
	scanner.UpsertSkills([]corelib.NLSkillEntry{{
		Name:     "skill-a",
		Status:   "active",
		SkillDir: dir,
	}})
	if len(scanner.pendingUpserts) != 1 {
		t.Fatalf("pendingUpserts before remove = %d, want 1", len(scanner.pendingUpserts))
	}
	scanner.RemoveByDir(dir)
	if len(scanner.pendingUpserts) != 0 {
		t.Fatalf("pendingUpserts after remove = %d, want 0", len(scanner.pendingUpserts))
	}
	if _, removed := scanner.pendingRemovals[skillDirIdentityKey(dir)]; !removed {
		t.Fatalf("pendingRemovals missing %s", dir)
	}
	for _, s := range scanner.Get() {
		if s.Name == "skill-a" {
			t.Fatalf("Get() still has skill-a after RemoveByDir: %+v", scanner.Get())
		}
	}
}

func TestCachedSkillScannerRearmsScanWhenStaleAfterInFlightScan(t *testing.T) {
	// Contract: if a mutation marks the cache stale while scanning==true
	// (so triggerBackgroundScan CAS fails), clearing the scanning flag must
	// re-arm a scan so we do not sit on a permanent stale marker with no worker.
	root := t.TempDir()
	scanner := &CachedSkillScanner{roots: []string{root}}
	scanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{},
		createdAt: time.Now(),
		stale:     false,
	})

	// Simulate an in-flight scan holding the scanning flag.
	scanner.scanning.Store(true)
	scanner.UpsertSkills([]corelib.NLSkillEntry{{
		Name:     "late",
		Status:   "active",
		SkillDir: filepath.Join(root, "late"),
	}})
	if !scanner.scanning.Load() {
		t.Fatal("scanning flag should still be held by the simulated in-flight scan")
	}
	entry := scanner.cache.Load()
	if entry == nil || !entry.stale {
		t.Fatalf("entry = %#v, want stale after Upsert during in-flight scan", entry)
	}
	// Upsert could not start a nested scan (CAS blocked).
	// Finish the simulated worker the same way triggerBackgroundScan does.
	scanner.scanning.Store(false)
	if entry := scanner.cache.Load(); entry != nil && entry.stale {
		scanner.triggerBackgroundScan()
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !scanner.scanning.Load() {
			entry = scanner.cache.Load()
			if entry != nil && !entry.stale {
				// Rescan completed and cleared stale. Pending upsert must still be present
				// (empty root cannot discover "late" on disk).
				found := false
				for _, s := range entry.skills {
					if s.Name == "late" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("skills after re-armed scan = %+v, want late retained via pending upsert", entry.skills)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for re-armed scan; scanning=%v entry=%#v", scanner.scanning.Load(), scanner.cache.Load())
}

func TestCachedSkillScannerUpsertReplacesSameNameDifferentDir(t *testing.T) {
	oldDir := filepath.Join(t.TempDir(), "old")
	newDir := filepath.Join(t.TempDir(), "new")
	scanner := &CachedSkillScanner{}
	scanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{{Name: "Demo", Status: "active", SkillDir: oldDir}},
		createdAt: time.Now(),
	})
	scanner.UpsertSkills([]corelib.NLSkillEntry{{
		Name:        "demo", // case-insensitive same name
		Status:      "active",
		SkillDir:    newDir,
		Description: "replacement",
	}})
	got := scanner.Get()
	if len(got) != 1 {
		t.Fatalf("Get() len = %d, want 1 (no duplicate names)", len(got))
	}
	if got[0].SkillDir != newDir || got[0].Description != "replacement" {
		t.Fatalf("Get()[0] = %+v, want new dir replacement", got[0])
	}
}

func TestCachedSkillScannerScanKeepsReimportAfterPendingRemoval(t *testing.T) {
	// Disk scan finds skill-a; a concurrent Remove+re-Upsert of the same name
	// must survive scan() after pendingRemovals filter (seen map rebuild).
	root := t.TempDir()
	skillDir := filepath.Join(root, "skill-a")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(
		"name: skill-a\ndescription: on disk\nsteps:\n  - action: bash\n    params:\n      command: echo a\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	scanner := &CachedSkillScanner{roots: []string{root}}
	// Simulate delete-then-reimport while a background scan is in flight:
	// record removal of the on-disk dir, then upsert the re-imported entry.
	// (In production the re-import uses a new path under primary skills dir;
	// for this race we keep the same dir key so disk scan would also see it —
	// the key assertion is that pending removal rebuilds `seen` so upsert is kept
	// when the scan path was filtered out.)
	otherDir := filepath.Join(t.TempDir(), "skill-a-reimport")
	scanner.RemoveByDir(skillDir)
	scanner.UpsertSkills([]corelib.NLSkillEntry{{
		Name:        "skill-a",
		Description: "reimported",
		Status:      "active",
		SkillDir:    otherDir,
	}})
	// Drop the removal so disk scan keeps skill-a, then re-set only the
	// "scan started before reimport" scenario: removal applied to disk result
	// of skillDir while pending upsert at otherDir must remain.
	// Force pending state: removal of skillDir + upsert at otherDir.
	scanner.pendingRemovals = map[string]struct{}{skillDirIdentityKey(skillDir): {}}
	scanner.pendingUpserts = map[string]corelib.NLSkillEntry{
		skillDirIdentityKey(otherDir): {
			Name: "skill-a", Description: "reimported", Status: "active", SkillDir: otherDir,
		},
	}

	scanner.scan()
	got := scanner.Get()
	foundReimport := false
	foundOldDir := false
	for _, s := range got {
		if s.Name == "skill-a" && s.SkillDir == otherDir {
			foundReimport = true
		}
		if skillDirIdentityKey(s.SkillDir) == skillDirIdentityKey(skillDir) {
			foundOldDir = true
		}
	}
	if !foundReimport {
		t.Fatalf("Get() after scan = %+v, want reimported skill-a at otherDir", got)
	}
	if foundOldDir {
		t.Fatalf("Get() still has removed skillDir entry: %+v", got)
	}
}

func TestImportNLSkillZipPathRefreshesWarmSkillCacheImmediately(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	t.Setenv("AppData", filepath.Join(tmpHome, "AppData", "Roaming"))

	// Pre-warm both the file scanner cache and the executor list cache so a
	// naive disk write would otherwise leave ListNLSkills stale for up to the
	// skillLoadCacheTTL window (the bug users see in the skill management UI).
	scanner := &CachedSkillScanner{}
	scanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{},
		createdAt: time.Now(),
		stale:     false,
	})
	scanner.version.Store(1)
	// Block background rescan so the test only passes if UpsertSkills provides
	// the imported skill synchronously (not via a delayed disk scan).
	scanner.scanning.Store(true)

	app := &App{testHomeDir: tmpHome, cachedSkillScanner: scanner}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	// Prime executor cache with an empty skill list under the current scanner version.
	if got := app.skillExecutor.List(); len(got) != 0 {
		t.Fatalf("pre-import List() = %+v, want empty", got)
	}

	zipPath := filepath.Join(t.TempDir(), "fresh-import.zip")
	createSkillZip(t, zipPath, map[string]string{
		"fresh-import/skill.yaml": "name: fresh-import\ndescription: should appear immediately\nsteps:\n  - action: bash\n    params:\n      command: echo hi\n",
	})

	name, err := app.importNLSkillZipPath(zipPath)
	if err != nil {
		t.Fatalf("importNLSkillZipPath() error = %v", err)
	}
	if name != "fresh-import" {
		t.Fatalf("name = %q, want fresh-import", name)
	}

	// Immediate ListNLSkills path used by the skills management UI.
	got := app.skillExecutor.List()
	found := false
	for _, s := range got {
		if s.Name == "fresh-import" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("List() after zip import = %+v, want fresh-import immediately (warm cache must not hide it)", got)
	}

	// Scanner cache itself must also expose the skill for other consumers.
	cached := scanner.Get()
	found = false
	for _, s := range cached {
		if s.Name == "fresh-import" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("CachedSkillScanner.Get() = %+v, want fresh-import after UpsertSkills", cached)
	}
}

func TestCloneSkillEntriesDeepCopiesMutableFields(t *testing.T) {
	fallback := corelib.NLSkillStep{Action: "bash", Params: map[string]interface{}{"command": "echo fallback"}}
	original := []corelib.NLSkillEntry{{
		Name:       "deep",
		Triggers:   []string{"deep"},
		Operations: []corelib.NLSkillOperation{{Name: "op", Params: []string{"input"}, Labels: []string{"run"}}},
		Params:     []corelib.NLSkillParam{{Name: "input", Aliases: []string{"q"}}},
		Steps: []corelib.NLSkillStep{{
			Action:       "bash",
			Params:       map[string]interface{}{"args": []interface{}{"one"}, "nested": map[string]interface{}{"k": "v"}},
			Capture:      map[string]string{"id": "(.*)"},
			Poll:         &corelib.StepPollConfig{Interval: 1},
			Loop:         &corelib.StepLoopConfig{MaxIterations: 2},
			FallbackStep: &fallback,
		}},
		SolidificationCandidates: []corelib.SolidificationCandidate{{ParamSlots: []string{"input"}}},
		Pipeline:                 []corelib.SkillPipelineStep{{Skill: "next", Params: map[string]string{"input": "{{input}}"}}},
	}}

	cloned := cloneSkillEntries(original)
	cloned[0].Triggers[0] = "mutated"
	cloned[0].Operations[0].Params[0] = "mutated"
	cloned[0].Params[0].Aliases[0] = "mutated"
	cloned[0].Steps[0].Params["nested"].(map[string]interface{})["k"] = "mutated"
	cloned[0].Steps[0].Capture["id"] = "mutated"
	cloned[0].Steps[0].Poll.Interval = 99
	cloned[0].Steps[0].Loop.MaxIterations = 99
	cloned[0].Steps[0].FallbackStep.Params["command"] = "mutated"
	cloned[0].SolidificationCandidates[0].ParamSlots[0] = "mutated"
	cloned[0].Pipeline[0].Params["input"] = "mutated"

	if original[0].Triggers[0] != "deep" || original[0].Operations[0].Params[0] != "input" || original[0].Params[0].Aliases[0] != "q" {
		t.Fatal("top-level mutable skill fields were not deep-copied")
	}
	if original[0].Steps[0].Params["nested"].(map[string]interface{})["k"] != "v" || original[0].Steps[0].Capture["id"] != "(.*)" {
		t.Fatal("step maps were not deep-copied")
	}
	if original[0].Steps[0].Poll.Interval != 1 || original[0].Steps[0].Loop.MaxIterations != 2 || original[0].Steps[0].FallbackStep.Params["command"] != "echo fallback" {
		t.Fatal("step pointer fields were not deep-copied")
	}
	if original[0].SolidificationCandidates[0].ParamSlots[0] != "input" || original[0].Pipeline[0].Params["input"] != "{{input}}" {
		t.Fatal("pipeline or solidification fields were not deep-copied")
	}
}
func TestMarkUploadedFileSkillEagerHubSkillIDFromPackageSubmission(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlBody := "name: paper_pdf_translator\ndescription: test\nstatus: active\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: tmpHome}
	app.cachedSkillScanner = &CachedSkillScanner{}
	app.cachedSkillScanner.UpsertSkills([]corelib.NLSkillEntry{{
		Name:     "paper_pdf_translator",
		Status:   "active",
		Source:   "file",
		SkillDir: skillDir,
	}})
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	const submission = "sub-1783856848170-cbee8cd2135b3c8e;enterprise_hub=enterprise_hub:skill:paper_pdf_translator@6c2a9af36010"
	if err := app.skillExecutor.MarkUploaded("paper_pdf_translator", submission); err != nil {
		t.Fatalf("MarkUploaded: %v", err)
	}
	statusPath := filepath.Join(skillDir, "upload_status.json")
	if _, err := os.Stat(statusPath); err != nil {
		t.Fatalf("upload_status.json missing: %v", err)
	}
	// Eager cache patch so ListNLSkills / publish gate sees HubSkillID immediately.
	found := false
	for _, s := range app.ListNLSkills() {
		if s.Name == "paper_pdf_translator" {
			found = true
			if s.HubSkillID != "paper_pdf_translator" {
				t.Fatalf("HubSkillID = %q, want package id paper_pdf_translator", s.HubSkillID)
			}
		}
	}
	if !found {
		t.Fatal("skill missing from ListNLSkills after MarkUploaded")
	}
}
