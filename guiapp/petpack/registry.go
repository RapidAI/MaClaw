package petpack

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
)

// Registry discovers and resolves pet packs (bundled embed + user dir).
type Registry struct {
	mu        sync.RWMutex
	ready     bool
	packs     map[string]*PetPackManifest // id → last scan (user wins over bundled)
	allowlist map[string]bool
	userRoot  string
	bundled   fs.FS
}

var (
	globalReg  *Registry
	globalOnce sync.Once
	globalMu   sync.Mutex
)

// UserPacksDir returns {MaclawBaseDir}/pet-packs (or env override).
func UserPacksDir() string {
	if env := strings.TrimSpace(os.Getenv("MACLAW_PET_PACKS_DIR")); env != "" {
		return env
	}
	base := corelib.MaclawBaseDir()
	if base == "" {
		return filepath.Join(".", "pet-packs")
	}
	return filepath.Join(base, "pet-packs")
}

// EnsureGlobal returns the process-wide registry, scanning once on first use.
func EnsureGlobal() *Registry {
	globalOnce.Do(func() {
		globalMu.Lock()
		// SetGlobalForTest may have installed a registry before first Do.
		if globalReg == nil {
			globalReg = NewRegistry(UserPacksDir(), BundledFS())
			_ = globalReg.Scan()
		}
		globalMu.Unlock()
	})
	globalMu.Lock()
	r := globalReg
	globalMu.Unlock()
	return r
}

// ResetGlobalForTest replaces the global registry (tests only).
// Marks Once as consumed so a later EnsureGlobal will not rebuild over r.
func ResetGlobalForTest(r *Registry) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalReg = r
	globalOnce = sync.Once{}
	globalOnce.Do(func() {})
}

// SetGlobalForTest forces EnsureGlobal to return r without re-scan once.
func SetGlobalForTest(r *Registry) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalReg = r
	// Mark Once as done so EnsureGlobal will not overwrite the injected registry.
	globalOnce = sync.Once{}
	globalOnce.Do(func() {})
}

// NewRegistry constructs a registry without scanning.
func NewRegistry(userRoot string, bundled fs.FS) *Registry {
	return &Registry{
		packs:     make(map[string]*PetPackManifest),
		allowlist: make(map[string]bool),
		userRoot:  userRoot,
		bundled:   bundled,
	}
}

// Ready reports whether at least one Scan completed.
func (r *Registry) Ready() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.ready
}

// Allowlist returns a copy of installed pack ids (ok + invalid installed).
func (r *Registry) Allowlist() map[string]bool {
	if r == nil {
		return OfficialAllowlist()
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(r.allowlist)+4)
	for k, v := range r.allowlist {
		out[k] = v
	}
	for _, id := range OfficialPackIDs {
		out[id] = true
	}
	return out
}

// isInstallStagingDir reports install swap leftovers that must never become packs.
func isInstallStagingDir(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.HasSuffix(name, ".tmp-install") || strings.HasSuffix(name, ".bak-install")
}

// Scan rediscovers bundled + user packs and rebuilds allowlist.
// Serialized with install/uninstall so a concurrent List cannot observe half-written trees.
func (r *Registry) Scan() error {
	installMu.Lock()
	defer installMu.Unlock()
	return r.scanUnlocked()
}

// scanUnlocked is the body of Scan; caller must hold installMu when mutating the user tree.
func (r *Registry) scanUnlocked() error {
	if r == nil {
		return fmt.Errorf("nil registry")
	}
	packs := make(map[string]*PetPackManifest)
	allow := OfficialAllowlist()

	// Bundled first
	if r.bundled != nil {
		_ = fs.WalkDir(r.bundled, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			base := filepath.Base(path)
			if d.IsDir() || (base != "pet-pack.yaml" && base != "pet-pack.yml") {
				return nil
			}
			data, err := fs.ReadFile(r.bundled, path)
			if err != nil {
				return nil
			}
			m, err := parseManifest(data)
			if err != nil {
				return nil
			}
			m.Scope = ScopeBundled
			m.Dir = filepath.ToSlash(filepath.Dir(path))
			// Historical bundled skins are kept in the source tree only as legacy
			// assets. They are not exposed as official 2.0 choices.
			if m.ID != DefaultPackID {
				return nil
			}
			m.Status = StatusOK
			// Validate native presence for figurative default
			if err := r.annotateStatus(m, r.bundled, m.Dir); err != nil {
				m.Status = StatusInvalid
				m.Error = err.Error()
			}
			packs[m.ID] = m
			allow[m.ID] = true
			return nil
		})
	}

	// User packs override bundled on same id
	if r.userRoot != "" {
		entries, err := os.ReadDir(r.userRoot)
		if err == nil {
			for _, e := range entries {
				if e == nil || !e.IsDir() {
					continue
				}
				// Never treat install swap directories as packs (even if they contain yaml).
				if isInstallStagingDir(e.Name()) {
					continue
				}
				// skip symlinks
				info, err := e.Info()
				if err != nil {
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 {
					continue
				}
				dir := filepath.Join(r.userRoot, e.Name())
				manPath := filepath.Join(dir, "pet-pack.yaml")
				data, err := os.ReadFile(manPath)
				if err != nil {
					manPath = filepath.Join(dir, "pet-pack.yml")
					data, err = os.ReadFile(manPath)
					if err != nil {
						// directory exists without valid manifest still allowlist by folder name if valid id
						id := e.Name()
						if IsValidPackID(id) {
							allow[id] = true
							packs[id] = &PetPackManifest{
								ID: id, Name: id, Scope: ScopeUser, Dir: dir,
								Status: StatusInvalid, Error: "missing pet-pack.yaml",
							}
						}
						continue
					}
				}
				m, err := parseManifest(data)
				if err != nil {
					id := e.Name()
					if IsValidPackID(id) {
						allow[id] = true
						packs[id] = &PetPackManifest{
							ID: id, Name: id, Scope: ScopeUser, Dir: dir,
							Status: StatusInvalid, Error: err.Error(),
						}
					}
					continue
				}
				m.Scope = ScopeUser
				m.Dir = dir
				// User packs must live in <userRoot>/<id>/ so uninstall/list stay consistent.
				if m.ID != e.Name() {
					m.Status = StatusInvalid
					m.Error = fmt.Sprintf("directory name %q must equal pack id %q", e.Name(), m.ID)
					// Do not clobber a valid install of the same id from the correct folder.
					if existing, ok := packs[m.ID]; ok && existing != nil && existing.Status == StatusOK {
						continue
					}
					packs[m.ID] = m
					allow[m.ID] = true
					continue
				}
				m.Status = StatusOK
				osFS := os.DirFS(dir)
				if err := r.annotateStatus(m, osFS, "."); err != nil {
					m.Status = StatusInvalid
					m.Error = err.Error()
				}
				packs[m.ID] = m
				allow[m.ID] = true
			}
		}
	}

	// Ensure official IDs always present as procedural fallbacks if embed missing
	for _, id := range OfficialPackIDs {
		if _, ok := packs[id]; !ok {
			packs[id] = builtinProceduralManifest(id)
			allow[id] = true
		}
	}

	r.mu.Lock()
	r.packs = packs
	r.allowlist = allow
	r.ready = true
	r.mu.Unlock()
	return nil
}

func (r *Registry) annotateStatus(m *PetPackManifest, fsys fs.FS, root string) error {
	// Validate every declared skeleton presentation, not just the active one.
	// This matches Pet Store publication and prevents an invalid custom variant
	// from sitting unnoticed until a later client version exposes it.
	for _, rigAssets := range SkeletonRigAssets(m) {
		if err := validateRigAssets(fsys, root, rigAssets); err != nil {
			return err
		}
	}
	for _, presentation := range CharacterPresentations(m) {
		if err := validateCharacterDefinitionAssets(fsys, root, presentation.Character, presentation.Rig); err != nil {
			return err
		}
	}
	// Figurative packs need a decodable static idle frame. Its fallback is used
	// by unsupported platforms, renderer failures, and reduced-motion mode, so
	// merely checking that a path exists would defer a broken-pack error until
	// the user enables it.
	renderer, idleRel := DefaultPresentation(m)
	if renderer == RendererProcedural {
		return nil
	}
	if idleRel == "" {
		// procedural fallback still allowed for official
		if IsOfficialPackID(m.ID) {
			return nil
		}
		return fmt.Errorf("missing native idle frame")
	}
	idleRel = safeRel(idleRel)
	if idleRel == "" {
		return fmt.Errorf("native idle asset has unsafe path")
	}
	path := filepath.ToSlash(filepath.Join(root, idleRel))
	if root == "." {
		path = filepath.ToSlash(idleRel)
	}
	err := ValidateStaticFrame(fsys, path)
	if err != nil && root != "." {
		// Some embedded FS layouts already expose the pack directory as root.
		err = ValidateStaticFrame(fsys, filepath.ToSlash(idleRel))
	}
	if err != nil {
		return fmt.Errorf("invalid idle asset %s: %w", idleRel, err)
	}
	return nil
}

// CharacterPresentation describes one native-character asset pair. It keeps
// each performer bound to its own rig even when a manifest has variants.
type CharacterPresentation struct {
	Character *PetPackCharacterAssets
	Rig       *PetPackRigAssets
}

// CharacterPresentations returns every v3 performer/rig pair selectable from
// a manifest or variant. It is shared by desktop scanning and Pet Store review.
func CharacterPresentations(m *PetPackManifest) []CharacterPresentation {
	if m == nil {
		return nil
	}
	out := make([]CharacterPresentation, 0, 1+len(m.Variants))
	if m.Renderer == RendererCharacter {
		out = append(out, CharacterPresentation{Character: m.Assets.Character, Rig: m.Assets.Rig})
	}
	for i := range m.Variants {
		renderer := m.Variants[i].Renderer
		if renderer == "" {
			renderer = m.Renderer
		}
		if renderer == RendererCharacter {
			out = append(out, CharacterPresentation{Character: m.Variants[i].Assets.Character, Rig: m.Variants[i].Assets.Rig})
		}
	}
	return out
}

func validateCharacterDefinitionAssets(fsys fs.FS, root string, assets *PetPackCharacterAssets, rigAssets *PetPackRigAssets) error {
	if assets == nil {
		return fmt.Errorf("native-character missing character assets")
	}
	definition := safeRel(assets.Definition)
	if definition == "" || !strings.HasSuffix(strings.ToLower(definition), ".json") {
		return fmt.Errorf("native-character has unsafe character definition")
	}
	path := filepath.ToSlash(filepath.Join(root, definition))
	if root == "." {
		path = definition
	}
	raw, err := fs.ReadFile(fsys, path)
	if err != nil && root != "." {
		raw, err = fs.ReadFile(fsys, definition)
	}
	if err != nil {
		return fmt.Errorf("character definition not found: %s", definition)
	}
	// The performer references clips in the current presentation rig. The rig
	// itself is already validated above; read it once for cross-reference.
	if rigAssets == nil {
		return fmt.Errorf("native-character missing rig assets")
	}
	rigPath := safeRel(rigAssets.Definition)
	rigRaw, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(root, rigPath)))
	if err != nil && root != "." {
		rigRaw, err = fs.ReadFile(fsys, rigPath)
	}
	if err != nil {
		return fmt.Errorf("native-character rig definition not found")
	}
	if _, err := ValidateCharacterDefinition(raw, rigRaw, rigAssets); err != nil {
		return fmt.Errorf("invalid character definition: %w", err)
	}
	return nil
}

func validateRigAssets(fsys fs.FS, root string, rigAssets *PetPackRigAssets) error {
	if rigAssets == nil {
		return fmt.Errorf("native-skeleton missing rig assets")
	}
	definition := safeRel(rigAssets.Definition)
	if definition == "" {
		return fmt.Errorf("native-skeleton has unsafe rig definition")
	}
	path := filepath.ToSlash(filepath.Join(root, definition))
	if root == "." {
		path = definition
	}
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("rig definition not found: %s", definition)
	}
	var rig Rig
	if err := json.Unmarshal(raw, &rig); err != nil {
		return fmt.Errorf("invalid rig definition: %w", err)
	}
	if err := ValidateRig(&rig, rigAssets); err != nil {
		return err
	}
	textureFS := fsys
	if root != "." && root != "" {
		if sub, err := fs.Sub(fsys, filepath.ToSlash(root)); err == nil {
			textureFS = sub
		}
	}
	if _, err := ReadRigTextureData(textureFS, rigAssets); err != nil {
		return err
	}
	return nil
}

func parseManifest(data []byte) (*PetPackManifest, error) {
	var m PetPackManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if err := ValidateManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func builtinProceduralManifest(id string) *PetPackManifest {
	return &PetPackManifest{
		SchemaVersion: 1,
		ID:            id,
		Name:          id,
		Version:       "1.0.0",
		Author:        "MaClaw Official",
		Tier:          "classic",
		Tone:          toneForOfficial(id),
		Renderer:      RendererProcedural,
		FaceOverlay:   true,
		DefaultSize:   88,
		Scope:         ScopeBundled,
		Status:        StatusOK,
		Label:         map[string]string{"en": id, "zh-Hans": id},
		Variants: []PetPackVariant{
			{ID: VariantClassic, Tier: "classic", Renderer: RendererProcedural},
			{ID: VariantDefault, Tier: "figurative", Renderer: RendererNative},
		},
		Motion: PetPackMotion{
			IdleMs: 4000, ListeningMs: 1200, ThinkingMs: 1800, SpeakingMs: 950,
			Amplitude: 0.85, SoundProfile: "classic", Pitch: pitchForOfficial(id),
		},
	}
}

func toneForOfficial(id string) string {
	switch id {
	case "mini-claw":
		return "compact"
	case "dev-claw":
		return "developer"
	case "focus-claw":
		return "focus"
	default:
		return "balanced"
	}
}

func pitchForOfficial(id string) float64 {
	switch id {
	case "mini-claw":
		return 1.2
	case "dev-claw":
		return 0.92
	case "focus-claw":
		return 0.78
	default:
		return 1.0
	}
}

// Get returns a pack by id.
func (r *Registry) Get(id string) (*PetPackManifest, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.packs[strings.TrimSpace(id)]
	return m, ok
}

// List returns all known packs, stable-sorted:
// official IDs first (catalog order), then remaining ids alphabetically.
func (r *Registry) List() []PackInfo {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PackInfo, 0, len(r.packs))
	seen := make(map[string]bool, len(r.packs))
	for _, id := range OfficialPackIDs {
		if m, ok := r.packs[id]; ok {
			out = append(out, packInfoFrom(m))
			seen[id] = true
		}
	}
	rest := make([]string, 0, len(r.packs))
	for id := range r.packs {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	sort.Strings(rest)
	for _, id := range rest {
		out = append(out, packInfoFrom(r.packs[id]))
	}
	return out
}

func packInfoFrom(m *PetPackManifest) PackInfo {
	variants := make([]string, 0, len(m.Variants))
	for _, v := range m.Variants {
		if v.ID != "" {
			variants = append(variants, v.ID)
		}
	}
	if len(variants) == 0 {
		variants = []string{VariantClassic, VariantDefault}
	}
	preview := m.Preview
	if preview == "" && m.Assets.Preview != "" {
		preview = m.Assets.Preview
	}
	if preview == "" && m.Assets.Native != nil {
		if idle := m.Assets.Native["idle"]; idle != "" {
			preview = idle
		}
	}
	previewPath := ""
	if m.Scope == ScopeUser && m.Dir != "" && preview != "" {
		previewPath = filepath.Join(m.Dir, filepath.FromSlash(preview))
	}
	canUninstall := m.Scope == ScopeUser
	source := ""
	if m.Scope == ScopeUser {
		source = packSourceForDir(m.Dir)
	}
	hasPreview := preview != "" || (m.Assets.Native != nil && m.Assets.Native["idle"] != "")
	return PackInfo{
		ID: m.ID, Name: m.Name, Version: m.Version, Author: m.Author,
		Scope: m.Scope, Status: m.Status, Error: m.Error, Tier: m.Tier, Tone: m.Tone,
		Label: m.Label, DescriptionText: m.Description, Description: m.DescriptionI18n, Variants: variants,
		DefaultSize: m.DefaultSize, FaceOverlay: m.FaceOverlay,
		PreviewPath: previewPath, Dir: m.Dir,
		CanUninstall: canUninstall, Source: source, HasPreview: hasPreview,
		Renderer: m.Renderer,
	}
}

// Resolve picks variant assets for packID + variant.
func (r *Registry) Resolve(packID, variant string) (*ResolvedPack, error) {
	if r == nil {
		return nil, fmt.Errorf("nil registry")
	}
	packID = strings.TrimSpace(packID)
	if packID == "" {
		packID = DefaultPackID
	}
	variant = ResolveVariantForRuntime(variant)

	r.mu.RLock()
	m, ok := r.packs[packID]
	bundled := r.bundled
	r.mu.RUnlock()
	if !ok || m == nil {
		m = builtinProceduralManifest(packID)
	}

	res := &ResolvedPack{
		Manifest:    m,
		VariantID:   variant,
		Renderer:    m.Renderer,
		FaceOverlay: m.FaceOverlay,
		Native:      map[string]string{},
		Motion:      m.Motion,
		Rig:         m.Assets.Rig,
		Character:   m.Assets.Character,
	}

	// Apply top-level assets
	if m.Assets.Native != nil {
		for k, v := range m.Assets.Native {
			res.Native[k] = v
		}
	}

	// ResolveVariantForRuntime normalizes every stored choice to the pack's
	// default presentation. Keep its selection in one helper shared with
	// preview and static-fallback validation.
	var matched *PetPackVariant
	if variant == VariantDefault {
		matched = defaultPresentationVariant(m)
	}
	if matched != nil {
		if matched.Renderer != "" {
			res.Renderer = matched.Renderer
		}
		if matched.FaceOverlay != nil {
			res.FaceOverlay = *matched.FaceOverlay
		}
		if matched.Assets.Native != nil {
			res.Native = map[string]string{}
			for k, v := range matched.Assets.Native {
				res.Native[k] = v
			}
		}
		if matched.Assets.Rig != nil {
			res.Rig = matched.Assets.Rig
		}
		if matched.Assets.Character != nil {
			res.Character = matched.Assets.Character
		}
		if variant == VariantClassic || matched.Renderer == RendererProcedural {
			res.Renderer = RendererProcedural
		}
	} else if variant == VariantClassic {
		res.Renderer = RendererProcedural
	}

	// Asset source
	if m.Scope == ScopeUser && m.Dir != "" {
		res.AssetRoot = m.Dir
		res.AssetFS = os.DirFS(m.Dir)
	} else if bundled != nil && m.Dir != "" {
		// embed sub FS
		sub, err := fs.Sub(bundled, m.Dir)
		if err == nil {
			res.AssetFS = sub
		} else {
			res.AssetFS = bundled
		}
	} else if bundled != nil {
		// try id path
		sub, err := fs.Sub(bundled, m.ID)
		if err == nil {
			res.AssetFS = sub
			res.Manifest.Dir = m.ID
		}
	}

	return res, nil
}
