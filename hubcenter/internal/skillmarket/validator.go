package skillmarket

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ValidationError 描述一个验证错误。
type ValidationError struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (e ValidationError) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Message)
}

// ValidationResult 是包验证的汇总结果。
type ValidationResult struct {
	Valid       bool              `json:"valid"`
	Errors      []ValidationError `json:"errors,omitempty"`
	Metadata    *SkillMetadata    `json:"metadata,omitempty"` // 解析成功时填充
	PackageRoot string            `json:"package_root"`       // 实际包根目录（可能是子目录）
}

// ValidatePackage 验证解压后的 Skill 包目录。
// 检查 skill.yaml 元数据、元数据必填字段、脚本语法。
func ValidatePackage(sandboxDir string) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	pkgRoot := resolvePackageRoot(sandboxDir)
	result.PackageRoot = pkgRoot

	meta, metaSource, err := parsePackageMetadata(pkgRoot)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			File:    metaSource,
			Message: err.Error(),
		})
		return result, nil
	}
	result.Metadata = meta

	if metaErrs := ValidateMetadata(meta); len(metaErrs) > 0 {
		result.Valid = false
		for _, msg := range metaErrs {
			result.Errors = append(result.Errors, ValidationError{
				File:    metaSource,
				Message: msg,
			})
		}
	}

	err = filepath.Walk(pkgRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(pkgRoot, path)
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".py":
			if errs := ValidatePython(path); len(errs) > 0 {
				result.Valid = false
				for i := range errs {
					errs[i].File = rel
				}
				result.Errors = append(result.Errors, errs...)
			}
		case ".sh", ".bash":
			if errs := ValidateShell(path); len(errs) > 0 {
				result.Valid = false
				for i := range errs {
					errs[i].File = rel
				}
				result.Errors = append(result.Errors, errs...)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk sandbox: %w", err)
	}

	return result, nil
}

func parsePackageMetadata(pkgRoot string) (*SkillMetadata, string, error) {
	// One directory listing: exact basenames matter on Windows (skill.md ≠ SKILL.md).
	files := listExactBasenames(pkgRoot)
	yamlPath, yamlSource := findSkillYAMLPathFrom(files, pkgRoot)
	if yamlPath == "" {
		// Lowercase skill.md alone is accepted as primary metadata.
		// Uppercase SKILL.md without skill.yaml remains a rejected legacy layout.
		if files["skill.md"] {
			if mdMeta, mdErr := parseNamedSkillMarkdownMetadata(pkgRoot, "skill.md"); mdErr == nil && mdMeta != nil {
				enrichMetadataFromPackageManifest(pkgRoot, mdMeta)
				return mdMeta, "skill.md", nil
			}
		}
		if files["SKILL.md"] {
			return nil, "SKILL.md", fmt.Errorf("legacy skill package is no longer supported; please migrate to skill.yaml or skill.md")
		}
		if files["_meta.json"] {
			return nil, "_meta.json", fmt.Errorf("legacy skill package is no longer supported; please migrate to skill.yaml or skill.md")
		}
		return nil, "skill.yaml", fmt.Errorf("skill.yaml not found in package root")
	}
	yamlErrs := ValidateYAML(yamlPath)
	if len(yamlErrs) > 0 {
		return nil, yamlSource, fmt.Errorf("%s", yamlErrs[0].Message)
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, yamlSource, fmt.Errorf("read %s: %w", yamlSource, err)
	}
	meta, err := ParseSkillYAML(data)
	if err != nil {
		return nil, yamlSource, err
	}
	// Enterprise Hub packages often put the human-readable title/description in
	// skill.md while skill.yaml only has a technical slug. Fill gaps so validation
	// and market listing succeed after Hub → HubCenter upload.
	enrichMetadataFromSkillMarkdown(pkgRoot, meta)
	enrichMetadataFromPackageManifest(pkgRoot, meta)
	return meta, yamlSource, nil
}

// findSkillYAMLPath returns the first skill definition YAML path (skill.yaml or skill.yml).
func findSkillYAMLPath(pkgRoot string) (path, source string) {
	return findSkillYAMLPathFrom(listExactBasenames(pkgRoot), pkgRoot)
}

func findSkillYAMLPathFrom(files map[string]bool, pkgRoot string) (path, source string) {
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		if !files[name] {
			continue
		}
		return filepath.Join(pkgRoot, name), name
	}
	return "", ""
}

// listExactBasenames returns a set of exact file basenames under dir (one ReadDir).
func listExactBasenames(dir string) map[string]bool {
	out := make(map[string]bool)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		out[entry.Name()] = true
	}
	return out
}

// hasExactFile reports whether dir contains a file with the exact basename
// (case-sensitive match against ReadDir entry names). This matters on Windows
// where os.Stat("skill.md") may open "SKILL.md".
func hasExactFile(dir, name string) bool {
	return listExactBasenames(dir)[name]
}

// enrichMetadataFromSkillMarkdown fills missing name/description from skill.md / SKILL.md.
// Enterprise packages often keep a technical slug in skill.yaml and the human title
// in skill.md — prefer the human title for market listing when yaml name is slug-like.
func enrichMetadataFromSkillMarkdown(pkgRoot string, meta *SkillMetadata) {
	if meta == nil {
		return
	}
	mdMeta, _, err := parseSkillMarkdownMetadata(pkgRoot)
	if err != nil || mdMeta == nil {
		return
	}
	yamlName := strings.TrimSpace(meta.Name)
	mdName := strings.TrimSpace(mdMeta.Name)
	if mdName != "" {
		if yamlName == "" || (isLikelySkillSlug(yamlName) && !isLikelySkillSlug(mdName)) {
			meta.Name = mdName
		}
	}
	if strings.TrimSpace(meta.Description) == "" {
		meta.Description = strings.TrimSpace(mdMeta.Description)
	}
	if len(meta.Tags) == 0 && len(mdMeta.Tags) > 0 {
		meta.Tags = append([]string(nil), mdMeta.Tags...)
	}
	if strings.TrimSpace(meta.Author) == "" {
		meta.Author = strings.TrimSpace(mdMeta.Author)
	}
}

// isLikelySkillSlug reports technical package identifiers (e.g. "ppt-master")
// vs human display titles (e.g. "PPT 演示文稿大师").
// Slugs are pure lowercase alphanumerics with - _ .
func isLikelySkillSlug(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 0x4e00 && r <= 0x9fff:
			return false // CJK → human title
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			// technical slug charset
		default:
			// Uppercase / punctuation / other scripts → treat as human title.
			return false
		}
	}
	return true
}

// parseSkillMarkdownMetadata reads name/description from skill.md or SKILL.md.
// Prefer lowercase skill.md (enterprise layout); SKILL.md is only used as enrichment fallback.
func parseSkillMarkdownMetadata(pkgRoot string) (*SkillMetadata, string, error) {
	files := listExactBasenames(pkgRoot)
	for _, name := range []string{"skill.md", "SKILL.md"} {
		if !files[name] {
			continue
		}
		meta, err := parseNamedSkillMarkdownMetadata(pkgRoot, name)
		if err != nil || meta == nil {
			continue
		}
		return meta, name, nil
	}
	return nil, "", fmt.Errorf("skill.md not found")
}

func parseNamedSkillMarkdownMetadata(pkgRoot, name string) (*SkillMetadata, error) {
	path := filepath.Join(pkgRoot, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	meta, parseErr := parseMarkdownFrontmatterMetadata(data)
	if parseErr != nil {
		return nil, parseErr
	}
	if meta == nil || (strings.TrimSpace(meta.Name) == "" && strings.TrimSpace(meta.Description) == "") {
		return nil, fmt.Errorf("%s has no usable metadata", name)
	}
	return meta, nil
}

func parseMarkdownFrontmatterMetadata(data []byte) (*SkillMetadata, error) {
	text := string(data)
	// Strip optional UTF-8 BOM.
	text = strings.TrimPrefix(text, "\ufeff")
	// Normalize newlines so CRLF packages parse the same as LF.
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	trimmed := strings.TrimSpace(normalized)
	if !strings.HasPrefix(trimmed, "---") {
		// No frontmatter: use first markdown heading as name, first paragraph as description.
		return markdownBodyMetadata(normalized), nil
	}
	// Locate closing --- after the opening marker.
	rest := trimmed[3:]
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return markdownBodyMetadata(normalized), nil
	}
	front := rest[:end]
	body := rest[end+4:]
	var meta SkillMetadata
	if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
		// Fall back to body-derived metadata rather than failing the whole package.
		return markdownBodyMetadata(normalized), nil
	}
	if strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
		bodyMeta := markdownBodyMetadata(body)
		if bodyMeta != nil {
			if strings.TrimSpace(meta.Name) == "" {
				meta.Name = bodyMeta.Name
			}
			if strings.TrimSpace(meta.Description) == "" {
				meta.Description = bodyMeta.Description
			}
		}
	}
	return &meta, nil
}

func markdownBodyMetadata(body string) *SkillMetadata {
	meta := &SkillMetadata{}
	lines := strings.Split(body, "\n")
	var para []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			if meta.Description == "" && len(para) > 0 {
				meta.Description = strings.Join(para, " ")
				para = nil
			}
			continue
		}
		if meta.Name == "" && strings.HasPrefix(trim, "#") {
			meta.Name = strings.TrimSpace(strings.TrimLeft(trim, "#"))
			continue
		}
		if meta.Description == "" && !strings.HasPrefix(trim, "#") && !strings.HasPrefix(trim, "```") {
			para = append(para, trim)
		}
	}
	if meta.Description == "" && len(para) > 0 {
		meta.Description = strings.Join(para, " ")
	}
	if meta.Name == "" && meta.Description == "" {
		return nil
	}
	return meta
}

func enrichMetadataFromPackageManifest(pkgRoot string, meta *SkillMetadata) {
	if meta == nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(pkgRoot, "skill_package_manifest.json"))
	if err != nil {
		return
	}
	var manifest struct {
		ProductKind                  string                 `json:"product_kind"`
		IsMaclawApp                  bool                   `json:"is_maclaw_app"`
		MaclawAppCount               int                    `json:"maclaw_app_count"`
		MaclawAppEntry               string                 `json:"maclaw_app_entry"`
		MaclawAppID                  string                 `json:"maclaw_app_id"`
		MaclawAppName                string                 `json:"maclaw_app_name"`
		MaclawAppDescription         string                 `json:"maclaw_app_description"`
		MaclawAppCategory            string                 `json:"maclaw_app_category"`
		MaclawAppIcon                string                 `json:"maclaw_app_icon"`
		MaclawAppInputMode           string                 `json:"maclaw_app_input_mode"`
		MaclawAppOutputModes         []string               `json:"maclaw_app_output_modes"`
		MaclawAppDefinitionSHA256    string                 `json:"maclaw_app_definition_sha256"`
		MaclawAppTestEvidence        *MaclawAppTestEvidence `json:"maclaw_app_test_evidence"`
		ArtifactContractRequired     bool                   `json:"artifact_contract_required"`
		ArtifactContractOutputModes  []string               `json:"artifact_contract_output_modes"`
		ArtifactContractPresentation string                 `json:"artifact_contract_presentation"`
		DeclaredPermissions          []string               `json:"declared_permissions"`
		DeclaredRequiredEnv          []string               `json:"declared_required_env"`
		DeclaredRequiresGUI          bool                   `json:"declared_requires_gui"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return
	}
	if strings.TrimSpace(manifest.ProductKind) != "" {
		meta.ProductKind = strings.TrimSpace(manifest.ProductKind)
	}
	if manifest.IsMaclawApp || strings.EqualFold(meta.ProductKind, "maclaw_app_skill") {
		meta.IsMaclawApp = true
		if meta.ProductKind == "" {
			meta.ProductKind = "maclaw_app_skill"
		}
	}
	if manifest.MaclawAppCount > 0 {
		meta.MaclawAppCount = manifest.MaclawAppCount
	}
	if strings.TrimSpace(manifest.MaclawAppEntry) != "" {
		meta.MaclawAppEntry = strings.TrimSpace(manifest.MaclawAppEntry)
	}
	if strings.TrimSpace(manifest.MaclawAppID) != "" {
		meta.MaclawAppID = strings.TrimSpace(manifest.MaclawAppID)
	}
	if strings.TrimSpace(manifest.MaclawAppName) != "" {
		meta.MaclawAppName = strings.TrimSpace(manifest.MaclawAppName)
	}
	if strings.TrimSpace(manifest.MaclawAppDescription) != "" {
		meta.MaclawAppDescription = strings.TrimSpace(manifest.MaclawAppDescription)
	}
	if strings.TrimSpace(manifest.MaclawAppCategory) != "" {
		meta.MaclawAppCategory = strings.TrimSpace(manifest.MaclawAppCategory)
	}
	if strings.TrimSpace(manifest.MaclawAppIcon) != "" {
		meta.MaclawAppIcon = strings.TrimSpace(manifest.MaclawAppIcon)
	}
	if strings.TrimSpace(manifest.MaclawAppInputMode) != "" {
		meta.MaclawAppInputMode = strings.TrimSpace(manifest.MaclawAppInputMode)
	}
	if len(manifest.MaclawAppOutputModes) > 0 {
		meta.MaclawAppOutputModes = append([]string(nil), manifest.MaclawAppOutputModes...)
	}
	if strings.TrimSpace(manifest.MaclawAppDefinitionSHA256) != "" {
		meta.MaclawAppDefinitionSHA256 = strings.TrimSpace(manifest.MaclawAppDefinitionSHA256)
	}
	if manifest.MaclawAppTestEvidence != nil {
		meta.MaclawAppTestEvidence = manifest.MaclawAppTestEvidence
	}
	if manifest.ArtifactContractRequired {
		meta.ArtifactContractRequired = true
	}
	if len(manifest.ArtifactContractOutputModes) > 0 {
		meta.ArtifactContractOutputModes = append([]string(nil), manifest.ArtifactContractOutputModes...)
	}
	if strings.TrimSpace(manifest.ArtifactContractPresentation) != "" {
		meta.ArtifactContractPresentation = strings.TrimSpace(manifest.ArtifactContractPresentation)
	}
	if len(meta.Permissions) == 0 && len(manifest.DeclaredPermissions) > 0 {
		meta.Permissions = append([]string(nil), manifest.DeclaredPermissions...)
	}
	if len(meta.RequiredEnv) == 0 && len(manifest.DeclaredRequiredEnv) > 0 {
		meta.RequiredEnv = append([]string(nil), manifest.DeclaredRequiredEnv...)
	}
	if manifest.DeclaredRequiresGUI {
		meta.RequiresGUI = true
	}
}

// ValidateYAML 验证 YAML 文件语法。
func ValidateYAML(path string) []ValidationError {
	data, err := os.ReadFile(path)
	if err != nil {
		return []ValidationError{{
			File:    filepath.Base(path),
			Message: fmt.Sprintf("cannot read file: %v", err),
		}}
	}
	var doc any
	if err := yamlUnmarshal(data, &doc); err != nil {
		return []ValidationError{{
			File:    filepath.Base(path),
			Message: fmt.Sprintf("YAML syntax error: %v", err),
		}}
	}
	return nil
}

// yamlUnmarshal 是对 yaml.Unmarshal 的间接引用，方便测试替换。
var yamlUnmarshal = yamlUnmarshalDefault

func yamlUnmarshalDefault(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

// ValidatePython 使用 py_compile 验证 Python 文件语法。
// 如果 python3 不可用，跳过验证。
func ValidatePython(path string) []ValidationError {
	pythonBin := findPython()
	if pythonBin == "" {
		return nil // python 不可用，跳过
	}
	cmd := exec.Command(pythonBin, "-m", "py_compile", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = fmt.Sprintf("py_compile failed: %v", err)
		}
		return []ValidationError{{
			File:    filepath.Base(path),
			Message: msg,
		}}
	}
	return nil
}

// ValidateShell 使用 bash -n 验证 Shell 脚本语法。
// 如果 bash 不可用，跳过验证。
func ValidateShell(path string) []ValidationError {
	bashBin, err := exec.LookPath("bash")
	if err != nil {
		return nil // bash 不可用，跳过
	}
	cmd := exec.Command(bashBin, "-n", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = fmt.Sprintf("bash -n failed: %v", err)
		}
		return []ValidationError{{
			File:    filepath.Base(path),
			Message: msg,
		}}
	}
	return nil
}

// findPython 查找可用的 Python 解释器。
func findPython() string {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// resolvePackageRoot 解析实际的包根目录。
// 如果 sandboxDir 根目录直接包含 skill.yaml / skill.md，返回 sandboxDir。
// 否则，如果根目录只有一个有效子目录且该子目录包含上述任一定义文件，返回该子目录。
// 跳过 __MACOSX 等打包工具产生的垃圾目录。
// 其他情况返回 sandboxDir（后续校验会报错）。
func resolvePackageRoot(sandboxDir string) string {
	if hasPackageDefinition(sandboxDir) {
		return sandboxDir
	}
	entries, err := os.ReadDir(sandboxDir)
	if err != nil {
		return sandboxDir
	}
	var soleDir string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "__MACOSX" {
			continue
		}
		if soleDir != "" {
			return sandboxDir
		}
		soleDir = e.Name()
	}
	if soleDir == "" {
		return sandboxDir
	}
	candidate := filepath.Join(sandboxDir, soleDir)
	if hasPackageDefinition(candidate) {
		return candidate
	}
	return sandboxDir
}

func hasPackageDefinition(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch entry.Name() {
		case "skill.yaml", "skill.yml", "skill.md", "SKILL.md":
			return true
		}
	}
	return false
}
