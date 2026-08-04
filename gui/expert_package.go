package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// expertPackageFormat identifies the portable archive exchanged between MaClaw
// installations.  The package contains one custom expert and every local skill
// explicitly allowed for it (including pipeline child-skill dependencies).
const (
	expertPackageFormat       = "maclaw-expert-package"
	expertPackageVersion      = 1
	expertPackageManifestFile = "manifest.json"
	maxExpertPackageSkills    = 128
	// A package may contain many nested ZIPs. Keep their aggregate expanded
	// size within the same ceiling as a normal skill ZIP, rather than allowing
	// each of 128 nested archives to consume that full budget independently.
	maxExpertPackageNestedExpandedBytes = maxSkillZipTotalExpandedBytes
)

const expertPackageIDPrefix = "pkgexp-"

type expertPackageSkill struct {
	Name    string `json:"name"`
	Archive string `json:"archive"`
}

// expertPackageDependencyPlan is the fully preflighted set of skills that an
// import needs. Its archives are unpacked and parsed once before the guarded
// importer runs.
type expertPackageDependencyPlan struct {
	Skills []expertPackageSkill
}

type expertPackageManifest struct {
	Format          string               `json:"format"`
	Version         int                  `json:"version"`
	ExportedAt      string               `json:"exported_at"`
	Expert          ExpertDefinition     `json:"expert"`
	ExpertPackageID string               `json:"expert_package_id"`
	Skills          []expertPackageSkill `json:"skills,omitempty"`
}

// ExpertPackageImportResult is returned to the desktop UI after a package was
// imported. Existing skills are deliberately retained rather than overwritten.
type ExpertPackageImportResult struct {
	Expert          ExpertDefinition `json:"expert"`
	InstalledSkills []string         `json:"installed_skills"`
	SkippedSkills   []string         `json:"skipped_skills"`
	AlreadyImported bool             `json:"already_imported"`
}

// expertPackageImportMutex serializes expert-package imports. Each import can
// add several named skills, so concurrent imports must not both observe a
// missing name and race into the same destination directory.
var expertPackageImportMutex sync.Mutex

// ExportExpertPackage opens a save dialog and writes a portable package for a
// user-created expert. Builtin experts are intentionally excluded: recipients
// already ship those definitions and local overrides must stay local.
func (a *App) ExportExpertPackage(id string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", fmt.Errorf("desktop runtime is unavailable")
	}
	def, err := a.userExpertForPackage(id)
	if err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export AI Expert Package",
		DefaultFilename: fmt.Sprintf("maclaw-expert-%s-%s.zip", toKebabCase(def.Name), stamp),
		Filters: []runtime.FileFilter{
			{DisplayName: "MaClaw Expert Package (*.zip)", Pattern: "*.zip"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		path += ".zip"
	}
	if err := a.ExportExpertPackageToFile(id, path); err != nil {
		return "", err
	}
	return path, nil
}

// ExportExpertPackageToFile is the path-oriented export entry point used by
// tests and desktop callers that have already selected an output path.
func (a *App) ExportExpertPackageToFile(id, outputPath string) error {
	def, err := a.userExpertForPackage(id)
	if err != nil {
		return err
	}
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return fmt.Errorf("export path is required")
	}
	packages, err := a.expertPackageSkillClosure(def.Skills)
	if err != nil {
		return err
	}

	manifest := expertPackageManifest{
		Format:          expertPackageFormat,
		Version:         expertPackageVersion,
		ExportedAt:      time.Now().UTC().Format(time.RFC3339),
		Expert:          exportableExpertDefinition(def),
		ExpertPackageID: expertPackageIdentity(def),
	}
	if err := a.writeExpertPackageAtomic(outputPath, manifest, packages); err != nil {
		return err
	}
	return nil
}

// ImportExpertPackage opens a native picker and imports one expert package. An
// empty result means the user cancelled the picker.
func (a *App) ImportExpertPackage() (*ExpertPackageImportResult, error) {
	if a == nil || a.ctx == nil {
		return nil, fmt.Errorf("desktop runtime is unavailable")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import AI Expert Package",
		Filters: []runtime.FileFilter{
			{DisplayName: "MaClaw Expert Package (*.zip)", Pattern: "*.zip"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil || path == "" {
		return nil, err
	}
	return a.ImportExpertPackageFromFile(path)
}

// ImportExpertPackageFromFile validates a package, installs only missing skills
// through the normal guarded skill importer, then saves or updates its stable
// package-identity expert.
func (a *App) ImportExpertPackageFromFile(packagePath string) (*ExpertPackageImportResult, error) {
	return a.importExpertPackageFromFile(packagePath, false)
}

// importExpertPackageFromFile installs a package using the normal guarded
// dependency flow. Market downloads pass localOnly=true so their definitions
// stay on this device instead of entering the user's Hub-synced expert set.
func (a *App) importExpertPackageFromFile(packagePath string, localOnly bool) (*ExpertPackageImportResult, error) {
	if a == nil {
		return nil, fmt.Errorf("app is unavailable")
	}
	expertPackageImportMutex.Lock()
	defer expertPackageImportMutex.Unlock()
	manifest, archives, err := readExpertPackage(packagePath)
	if err != nil {
		return nil, err
	}
	if err := validateImportedExpertDefinition(manifest.Expert); err != nil {
		return nil, err
	}
	// Normalize lists before any dependency checks so whitespace and duplicate
	// entries cannot create an expert that saves successfully but fails its
	// import validation. The package identity stays stable across exports and
	// imports, allowing an existing imported expert to be updated in place.
	manifest.Expert = exportableExpertDefinition(manifest.Expert)
	a.ensureRemoteInfra()
	// Load the installed skill names before considering a duplicate package a
	// no-op. A user can remove a dependency after a previous import; importing
	// the exact same package again must then repair that missing dependency.
	// This also keeps genuinely repeated imports fast and side-effect free.
	existingSkillEntries := make(map[string]corelib.NLSkillEntry)
	if a.skillExecutor != nil {
		for _, entry := range a.skillExecutor.loadSkills() {
			existingSkillEntries[entry.Name] = entry
		}
	}
	dependencyPlan, err := a.expertPackageDependencyPlan(manifest.Expert.Skills, existingSkillEntries, manifest.Skills, archives)
	if err != nil {
		return nil, err
	}
	selectedSkills := dependencyPlan.Skills
	var existingExpert *ExpertDefinition
	if existing, ok, err := defaultExpertStore.Get(manifest.ExpertPackageID); err != nil {
		return nil, err
	} else if ok {
		if expertPackageDefinitionsEqual(existing, manifest.Expert) && len(selectedSkills) == 0 {
			if localOnly {
				if err := defaultExpertStore.MarkMarketInstall(existing.ID); err != nil {
					return nil, err
				}
			}
			return &ExpertPackageImportResult{Expert: existing, AlreadyImported: true}, nil
		}
		existingCopy := existing
		existingExpert = &existingCopy
	}
	manifest.Expert.ID = manifest.ExpertPackageID
	// Packages that actually need to install a skill follow the same policy gate
	// as the standalone skill importer. A re-import whose bundled skills are
	// already available only writes the expert definition, so it does not need
	// the unrelated skill-management capability.
	if expertPackageHasMissingSkills(existingSkillEntries, selectedSkills) {
		if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "import", "source": "expert_package"}); err != nil {
			return nil, err
		}
	}

	if a.skillExecutor == nil && len(selectedSkills) > 0 {
		return nil, fmt.Errorf("skill executor is not initialized")
	}
	result := &ExpertPackageImportResult{}
	for _, item := range selectedSkills {
		if _, installed := existingSkillEntries[item.Name]; installed {
			result.SkippedSkills = append(result.SkippedSkills, item.Name)
			continue
		}
		archive, ok := archives[item.Archive]
		if !ok {
			return nil, fmt.Errorf("package is missing skill archive %q", item.Archive)
		}
		// dependencyPlan has already validated that this archive declares this
		// exact skill and safely inspected its Pipeline dependencies. The
		// guarded importer below remains the sole state-mutating operation.
		tmpPath, err := writeExpertPackageSkillTemp(a.GetTempDir(), archive)
		if err != nil {
			a.cleanupImportedExpertPackageSkills(result.InstalledSkills)
			return nil, fmt.Errorf("write temporary skill package: %w", err)
		}
		importedName, importErr := a.importNLSkillZipPath(tmpPath)
		_ = os.Remove(tmpPath)
		if importErr != nil {
			a.cleanupImportedExpertPackageSkills(result.InstalledSkills)
			return nil, fmt.Errorf("import dependent skill %q: %w", item.Name, importErr)
		}
		// Record immediately so a mismatched archive is also cleaned up.
		result.InstalledSkills = append(result.InstalledSkills, importedName)
		if importedName != item.Name {
			a.cleanupImportedExpertPackageSkills(result.InstalledSkills)
			return nil, fmt.Errorf("skill archive %q declares %q, expected %q", item.Archive, importedName, item.Name)
		}
		existingSkillEntries[item.Name] = corelib.NLSkillEntry{Name: item.Name}
	}

	def := manifest.Expert
	if existingExpert != nil {
		// SaveExpert preserves the original created_at for a matching id. Keep a
		// copy for rollback in case the Hub/local save fails after new skills were
		// installed during this package update.
		def.CreatedAt = existingExpert.CreatedAt
	}
	raw, err := json.Marshal(def)
	if err != nil {
		a.cleanupImportedExpertPackageSkills(result.InstalledSkills)
		return nil, fmt.Errorf("encode imported expert: %w", err)
	}
	if !localOnly {
		savedRaw, err := a.SaveExpert(string(raw))
		if err != nil {
			a.cleanupImportedExpertPackageSkills(result.InstalledSkills)
			if existingExpert != nil {
				if rollbackErr := defaultExpertStore.Save(*existingExpert); rollbackErr != nil {
					log.Printf("[experts] rollback updated expert %q failed: %v", existingExpert.ID, rollbackErr)
				}
			}
			return nil, fmt.Errorf("save imported expert: %w", err)
		}
		if err := json.Unmarshal([]byte(savedRaw), &result.Expert); err != nil {
			a.cleanupImportedExpertPackageSkills(result.InstalledSkills)
			return nil, fmt.Errorf("decode imported expert: %w", err)
		}
	} else {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if strings.TrimSpace(def.CreatedAt) == "" {
			def.CreatedAt = now
		}
		def.UpdatedAt = now
		def.Builtin = false
		def = normalizeExpertLists(def)
		if err := defaultExpertStore.SaveMarketInstall(def); err != nil {
			a.cleanupImportedExpertPackageSkills(result.InstalledSkills)
			if existingExpert != nil {
				if rollbackErr := defaultExpertStore.SaveMarketInstall(*existingExpert); rollbackErr != nil {
					log.Printf("[experts] rollback local-only expert %q failed: %v", existingExpert.ID, rollbackErr)
				}
			}
			return nil, fmt.Errorf("save imported expert: %w", err)
		}
		invalidateExpertDefCache(def.ID)
		result.Expert = def
	}
	sort.Strings(result.InstalledSkills)
	sort.Strings(result.SkippedSkills)
	return result, nil
}

// validateExpertPackageSkillArchive rejects archives that would expand into
// multiple skills or a skill whose declared name does not match the package
// manifest. This prevents a crafted expert bundle from installing unrelated
// skills through the general multi-skill ZIP importer.
func (a *App) readExpertPackageSkillArchive(archive []byte, expectedName string) (corelib.NLSkillEntry, error) {
	if a == nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("app is unavailable")
	}
	tmpPath, err := writeExpertPackageSkillTemp(a.GetTempDir(), archive)
	if err != nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("write temporary skill package: %w", err)
	}
	defer os.Remove(tmpPath)
	tmpDir, err := os.MkdirTemp(a.GetTempDir(), "expert-skill-validate-*")
	if err != nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("create temporary skill directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := a.unzip(tmpPath, tmpDir); err != nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("unpack dependent skill %q: %w", expectedName, err)
	}
	roots, err := resolveImportedSkillPackageRoots(tmpDir)
	if err != nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("validate dependent skill %q: %w", expectedName, err)
	}
	if len(roots) != 1 {
		return corelib.NLSkillEntry{}, fmt.Errorf("dependent skill archive %q must contain exactly one skill", expectedName)
	}
	entry, err := loadImportedSkillEntry(roots[0])
	if err != nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("read dependent skill %q: %w", expectedName, err)
	}
	if entry.Name != expectedName {
		return corelib.NLSkillEntry{}, fmt.Errorf("dependent skill archive declares %q, expected %q", entry.Name, expectedName)
	}
	// Keep the package preflight in parity with the guarded importer: syntax
	// validation alone is insufficient because security policy can reject a
	// skill during installation after earlier dependencies were already added.
	return *entry, nil
}

// validateExpertPackageSkillArchive is retained for callers that only need
// validation. Dependency resolution uses readExpertPackageSkillArchive so it
// can follow a bundled Pipeline skill's declared children before installing it.
func (a *App) validateExpertPackageSkillArchive(archive []byte, expectedName string) error {
	_, err := a.readExpertPackageSkillArchive(archive, expectedName)
	return err
}

func writeExpertPackageSkillTemp(dir string, archive []byte) (string, error) {
	tmp, err := os.CreateTemp(dir, "expert-skill-import-*.zip")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	if _, err := tmp.Write(archive); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// cleanupImportedExpertPackageSkills best-effort rolls back skills installed
// during the current import when a later dependency or expert save fails.
func (a *App) cleanupImportedExpertPackageSkills(names []string) {
	if a == nil || a.skillExecutor == nil {
		return
	}
	for i := len(names) - 1; i >= 0; i-- {
		if err := a.skillExecutor.Delete(names[i]); err != nil {
			log.Printf("[experts] rollback imported skill %q failed: %v", names[i], err)
		}
	}
}

func (a *App) userExpertForPackage(id string) (ExpertDefinition, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ExpertDefinition{}, fmt.Errorf("expert id is required")
	}
	if builtinExpertByID(id) != nil {
		return ExpertDefinition{}, fmt.Errorf("builtin experts cannot be exported")
	}
	def, ok, err := defaultExpertStore.Get(id)
	if err != nil {
		return ExpertDefinition{}, err
	}
	if !ok {
		return ExpertDefinition{}, fmt.Errorf("expert %q not found", id)
	}
	if def.Builtin {
		return ExpertDefinition{}, fmt.Errorf("builtin experts cannot be exported")
	}
	if strings.TrimSpace(def.Name) == "" || strings.TrimSpace(def.SystemPrompt) == "" {
		return ExpertDefinition{}, fmt.Errorf("expert %q is incomplete and cannot be exported", id)
	}
	return normalizeExpertLists(def), nil
}

func exportableExpertDefinition(def ExpertDefinition) ExpertDefinition {
	def.ID = ""
	def.Builtin = false
	def.CreatedAt = ""
	def.UpdatedAt = ""
	def.Tools = uniqueExpertStrings(def.Tools)
	def.Skills = uniqueExpertStrings(def.Skills)
	return normalizeExpertLists(def)
}

// expertPackageDefinitionsEqual compares the portable expert payload only.
// Its local id and timestamps are intentionally excluded: an identical import
// should be a no-op, while a package from the same origin with changed expert
// content updates that existing expert in place rather than creating a copy.
func expertPackageDefinitionsEqual(local, incoming ExpertDefinition) bool {
	local = exportableExpertDefinition(local)
	incoming = exportableExpertDefinition(incoming)
	left, leftErr := json.Marshal(local)
	right, rightErr := json.Marshal(incoming)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

// expertPackageDependenciesInstalled reports whether every skill explicitly
// required by an expert is present locally. It intentionally ignores bundled
// but unreferenced archives: they are not dependencies of the expert and must
// not turn an otherwise idempotent import into a mutation.
func expertPackageDependenciesInstalled(installed map[string]bool, required []string) bool {
	for _, name := range required {
		if !installed[name] {
			return false
		}
	}
	return true
}

func expertPackageHasMissingSkills(installed map[string]corelib.NLSkillEntry, bundled []expertPackageSkill) bool {
	for _, item := range bundled {
		if _, ok := installed[item.Name]; !ok {
			return true
		}
	}
	return false
}

// expertPackageRequiredSkills derives the exact dependency closure required to
// run an imported expert. A valid exporter bundles that same closure, but an
// externally crafted archive may contain extra skills. Those extras are never
// installed: they are neither an expert dependency nor a child of one.
func (a *App) expertPackageRequiredSkills(roots []string, installed map[string]corelib.NLSkillEntry, bundled []expertPackageSkill, archives map[string][]byte) ([]expertPackageSkill, error) {
	plan, err := a.expertPackageDependencyPlan(roots, installed, bundled, archives)
	if err != nil {
		return nil, err
	}
	return plan.Skills, nil
}

func (a *App) expertPackageDependencyPlan(roots []string, installed map[string]corelib.NLSkillEntry, bundled []expertPackageSkill, archives map[string][]byte) (expertPackageDependencyPlan, error) {
	plan := expertPackageDependencyPlan{}
	byName := make(map[string]expertPackageSkill, len(bundled))
	for _, item := range bundled {
		byName[item.Name] = item
	}
	queued := append([]string(nil), roots...)
	seen := make(map[string]bool)
	plan.Skills = make([]expertPackageSkill, 0, len(roots))
	// Resolve a nested archive's declared size before unpacking it to inspect
	// its Pipeline. Without this incremental preflight, a crafted chain of
	// individually valid archives could be expanded into temporary storage
	// before validateExpertPackageNestedArchives gets a chance to enforce the
	// package-wide expansion ceiling.
	checkedArchives := make(map[string]bool)
	var expandedTotal uint64
	for len(queued) > 0 {
		name := strings.TrimSpace(queued[0])
		queued = queued[1:]
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		// The package itself is capped at maxExpertPackageSkills, but its roots
		// can point at already-installed Pipeline skills. Bound that full graph
		// as well so a malformed local chain cannot make an otherwise small
		// package import walk an unbounded number of nodes.
		if len(seen) > maxExpertPackageSkills {
			return expertPackageDependencyPlan{}, fmt.Errorf("expert has too many dependent skills (maximum %d)", maxExpertPackageSkills)
		}
		entry, installedLocally := installed[name]
		if !installedLocally {
			item, bundledHere := byName[name]
			if !bundledHere {
				return expertPackageDependencyPlan{}, fmt.Errorf("expert requires skill %q, but it was not included or installed", name)
			}
			archive, ok := archives[item.Archive]
			if !ok {
				return expertPackageDependencyPlan{}, fmt.Errorf("package is missing skill archive %q", item.Archive)
			}
			if !checkedArchives[item.Archive] {
				expanded, err := expertPackageNestedArchiveExpandedSize(archive)
				if err != nil {
					return expertPackageDependencyPlan{}, fmt.Errorf("validate dependent skill archive %q: %w", item.Archive, err)
				}
				if expanded > maxExpertPackageNestedExpandedBytes-expandedTotal {
					return expertPackageDependencyPlan{}, fmt.Errorf("expert package dependent skills expand to too much data: maximum %d bytes", maxExpertPackageNestedExpandedBytes)
				}
				expandedTotal += expanded
				checkedArchives[item.Archive] = true
			}
			var err error
			entry, err = a.readExpertPackageSkillArchive(archive, name)
			if err != nil {
				return expertPackageDependencyPlan{}, err
			}
			plan.Skills = append(plan.Skills, item)
		}
		for _, step := range entry.Pipeline {
			if child := strings.TrimSpace(step.Skill); child != "" && !seen[child] {
				queued = append(queued, child)
			}
		}
	}
	sort.Slice(plan.Skills, func(i, j int) bool { return plan.Skills[i].Name < plan.Skills[j].Name })
	return plan, nil
}

// expertPackageIdentity is deterministic from the original local expert id.
// Re-exported imports keep their package id, preserving deduplication across
// chains of users and devices without exposing the original local record id.
func expertPackageIdentity(def ExpertDefinition) string {
	if id := strings.TrimSpace(def.ID); strings.HasPrefix(id, expertPackageIDPrefix) && expertIDPattern.MatchString(id) {
		return id
	}
	if id := strings.TrimSpace(def.ID); id != "" {
		sum := sha256.Sum256([]byte(id))
		return expertPackageIDPrefix + fmt.Sprintf("%x", sum[:16])
	}
	return legacyExpertPackageIdentity(def)
}

// legacyExpertPackageIdentity keeps packages exported before package IDs were
// introduced idempotent too. It fingerprints the normalized, shareable expert
// definition, so repeated imports of the same older package also deduplicate.
func legacyExpertPackageIdentity(def ExpertDefinition) string {
	def = exportableExpertDefinition(def)
	data, _ := json.Marshal(def)
	sum := sha256.Sum256(data)
	return expertPackageIDPrefix + fmt.Sprintf("%x", sum[:16])
}

func uniqueExpertStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func validateImportedExpertDefinition(def ExpertDefinition) error {
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("package expert name is required")
	}
	if strings.TrimSpace(def.SystemPrompt) == "" {
		return fmt.Errorf("package expert system_prompt is required")
	}
	if builtinExpertByID(strings.TrimSpace(def.ID)) != nil || def.Builtin {
		return fmt.Errorf("package must contain a user-created expert")
	}
	return nil
}

func (a *App) expertPackageSkillClosure(roots []string) ([]corelib.NLSkillEntry, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor is not initialized")
	}
	all := a.skillExecutor.loadSkills()
	byName := make(map[string]corelib.NLSkillEntry, len(all))
	for _, entry := range all {
		byName[entry.Name] = entry
	}
	queued := append([]string(nil), roots...)
	seen := make(map[string]bool)
	var out []corelib.NLSkillEntry
	for len(queued) > 0 {
		name := strings.TrimSpace(queued[0])
		queued = queued[1:]
		if name == "" || seen[name] {
			continue
		}
		entry, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("expert dependency skill %q is not installed", name)
		}
		seen[name] = true
		out = append(out, entry)
		if len(out) > maxExpertPackageSkills {
			return nil, fmt.Errorf("expert has too many dependent skills (maximum %d)", maxExpertPackageSkills)
		}
		for _, step := range entry.Pipeline {
			if child := strings.TrimSpace(step.Skill); child != "" && !seen[child] {
				queued = append(queued, child)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (a *App) writeExpertPackageAtomic(outputPath string, manifest expertPackageManifest, skills []corelib.NLSkillEntry) (err error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create export directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(outputPath), ".expert-package-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	zw := zip.NewWriter(tmp)
	for index, entry := range skills {
		archive, err := a.buildExpertExchangeSkillArchive(entry)
		if err != nil {
			_ = zw.Close()
			return fmt.Errorf("package dependent skill %q: %w", entry.Name, err)
		}
		name := fmt.Sprintf("skills/%03d-%s.zip", index+1, toKebabCase(entry.Name))
		writer, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := writer.Write(archive); err != nil {
			_ = zw.Close()
			return err
		}
		manifest.Skills = append(manifest.Skills, expertPackageSkill{Name: entry.Name, Archive: name})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		return err
	}
	writer, err := zw.Create(expertPackageManifestFile)
	if err != nil {
		_ = zw.Close()
		return err
	}
	if _, err := writer.Write(data); err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFileWithTemp(tmpPath, outputPath); err != nil {
		return err
	}
	committed = true
	return nil
}

func (a *App) buildExpertExchangeSkillArchive(entry corelib.NLSkillEntry) ([]byte, error) {
	pkgDir, err := os.MkdirTemp("", "expert-skill-package-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(pkgDir)
	target := entry
	if strings.TrimSpace(entry.SkillDir) != "" {
		if err := copyDirContents(entry.SkillDir, pkgDir); err != nil {
			return nil, fmt.Errorf("copy skill directory: %w", err)
		}
		target = *skill.PackageViewFromRuntimeEntry(&entry, entry.SkillDir)
	}
	target.SkillDir = ""
	target.LastError = ""
	if err := writePackageViewSkillYAML(pkgDir, &target); err != nil {
		return nil, err
	}
	// Outbound packages must use the application's current security policy.
	// Calling without a would only verify that the scanner ran, not block a
	// package according to the user's configured risk guardrails.
	if err := scanSkillDirForOutboundPackage(pkgDir, a); err != nil {
		return nil, err
	}
	zipFile, err := os.CreateTemp("", "expert-skill-archive-*.zip")
	if err != nil {
		return nil, err
	}
	zipPath := zipFile.Name()
	if err := zipFile.Close(); err != nil {
		_ = os.Remove(zipPath)
		return nil, err
	}
	defer os.Remove(zipPath)
	if err := zipDirectory(pkgDir, zipPath); err != nil {
		return nil, err
	}
	return os.ReadFile(zipPath)
}

// validateExpertPackageNestedArchives checks the resource declarations of all
// nested archives that would be installed before mutating local skill state.
// Existing skills are deliberately skipped and their bundled archive is never
// opened or installed.
func validateExpertPackageNestedArchives(items []expertPackageSkill, archives map[string][]byte) error {
	var total uint64
	for _, item := range items {
		archive, ok := archives[item.Archive]
		if !ok {
			return fmt.Errorf("package is missing skill archive %q", item.Archive)
		}
		expanded, err := expertPackageNestedArchiveExpandedSize(archive)
		if err != nil {
			return fmt.Errorf("validate dependent skill archive %q: %w", item.Archive, err)
		}
		if expanded > maxExpertPackageNestedExpandedBytes-total {
			return fmt.Errorf("expert package dependent skills expand to too much data: maximum %d bytes", maxExpertPackageNestedExpandedBytes)
		}
		total += expanded
	}
	return nil
}

func expertPackageNestedArchiveExpandedSize(archive []byte) (uint64, error) {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return 0, fmt.Errorf("open nested skill archive: %w", err)
	}
	if err := validateSkillZipResourceLimits(reader.File); err != nil {
		return 0, err
	}
	var total uint64
	for _, file := range reader.File {
		if file != nil && !file.FileInfo().IsDir() {
			total += file.UncompressedSize64
		}
	}
	return total, nil
}

func readExpertPackage(packagePath string) (expertPackageManifest, map[string][]byte, error) {
	var manifest expertPackageManifest
	reader, err := zip.OpenReader(strings.TrimSpace(packagePath))
	if err != nil {
		return manifest, nil, fmt.Errorf("open expert package: %w", err)
	}
	defer reader.Close()
	if err := validateSkillZipResourceLimits(reader.File); err != nil {
		return manifest, nil, err
	}
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().Mode()&os.ModeSymlink != 0 || file.FileInfo().IsDir() {
			return manifest, nil, fmt.Errorf("expert package contains unsupported entry %q", file.Name)
		}
		if _, cleanName, err := safeZipEntryTarget(os.TempDir(), file.Name); err != nil || cleanName != file.Name {
			return manifest, nil, fmt.Errorf("expert package contains illegal entry %q", file.Name)
		}
		if _, exists := files[file.Name]; exists {
			return manifest, nil, fmt.Errorf("expert package contains duplicate entry %q", file.Name)
		}
		files[file.Name] = file
	}
	manifestFile := files[expertPackageManifestFile]
	if manifestFile == nil {
		return manifest, nil, fmt.Errorf("expert package is missing %s", expertPackageManifestFile)
	}
	data, err := readZipFileLimited(manifestFile, 1<<20)
	if err != nil {
		return manifest, nil, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, nil, fmt.Errorf("parse expert package manifest: %w", err)
	}
	if manifest.Format != expertPackageFormat || manifest.Version != expertPackageVersion {
		return manifest, nil, fmt.Errorf("unsupported expert package format or version")
	}
	manifest.ExpertPackageID = strings.TrimSpace(manifest.ExpertPackageID)
	if manifest.ExpertPackageID == "" {
		manifest.ExpertPackageID = legacyExpertPackageIdentity(manifest.Expert)
	}
	if !expertIDPattern.MatchString(manifest.ExpertPackageID) || !strings.HasPrefix(manifest.ExpertPackageID, expertPackageIDPrefix) {
		return manifest, nil, fmt.Errorf("expert package has an invalid expert_package_id")
	}
	if len(manifest.Skills) > maxExpertPackageSkills {
		return manifest, nil, fmt.Errorf("expert package has too many skills")
	}
	archives := make(map[string][]byte, len(manifest.Skills))
	seenNames := make(map[string]bool, len(manifest.Skills))
	seenArchives := make(map[string]bool, len(manifest.Skills))
	validatedSkills := make([]expertPackageSkill, 0, len(manifest.Skills))
	for _, item := range manifest.Skills {
		item.Name = strings.TrimSpace(item.Name)
		item.Archive = strings.TrimSpace(item.Archive)
		if item.Name == "" || item.Archive == "" || !strings.HasPrefix(item.Archive, "skills/") || !strings.HasSuffix(item.Archive, ".zip") || seenNames[item.Name] || seenArchives[item.Archive] {
			return manifest, nil, fmt.Errorf("expert package has an invalid skill entry")
		}
		seenNames[item.Name] = true
		seenArchives[item.Archive] = true
		file := files[item.Archive]
		if file == nil {
			return manifest, nil, fmt.Errorf("expert package is missing skill archive %q", item.Archive)
		}
		archive, err := readZipFileLimited(file, maxSkillZipFileBytes)
		if err != nil {
			return manifest, nil, err
		}
		archives[item.Archive] = archive
		validatedSkills = append(validatedSkills, item)
	}
	if len(files) != len(manifest.Skills)+1 {
		return manifest, nil, fmt.Errorf("expert package contains unexpected files")
	}
	manifest.Skills = validatedSkills
	return manifest, archives, nil
}

func readZipFileLimited(file *zip.File, limit uint64) ([]byte, error) {
	if file.UncompressedSize64 > limit {
		return nil, fmt.Errorf("package entry %q is too large", file.Name)
	}
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var out bytes.Buffer
	if _, err := io.Copy(&out, io.LimitReader(rc, int64(limit)+1)); err != nil {
		return nil, err
	}
	if uint64(out.Len()) > limit {
		return nil, fmt.Errorf("package entry %q is too large", file.Name)
	}
	return out.Bytes(), nil
}
