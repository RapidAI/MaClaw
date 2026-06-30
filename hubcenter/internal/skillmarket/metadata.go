package skillmarket

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SkillMetadata 是 skill.yaml 的结构化表示。
type SkillMetadata struct {
	ID          string   `yaml:"id,omitempty" json:"id,omitempty"` // UUID，首次创建时生成，重新上传时复用
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Triggers    []string `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Version     string   `yaml:"version,omitempty" json:"version,omitempty"`
	Author      string   `yaml:"author,omitempty" json:"author,omitempty"`
	Price       int      `yaml:"price,omitempty" json:"price,omitempty"`

	// 安全相关字段 (Req 37)
	Permissions []string `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	RequiredEnv []string `yaml:"required_env,omitempty" json:"required_env,omitempty"`

	// PricingMode 定价模式 (Req 36): auto|free|fixed
	PricingMode string `yaml:"pricing_mode,omitempty" json:"pricing_mode,omitempty"`

	// 平台兼容性
	Platforms                    []string               `yaml:"platforms,omitempty" json:"platforms,omitempty"`       // "windows","linux","macos"; empty = universal
	RequiresGUI                  bool                   `yaml:"requires_gui,omitempty" json:"requires_gui,omitempty"` // Linux 下是否需要 GUI 环境
	ProductKind                  string                 `yaml:"product_kind,omitempty" json:"product_kind,omitempty"`
	IsMaclawApp                  bool                   `yaml:"is_maclaw_app,omitempty" json:"is_maclaw_app,omitempty"`
	MaclawAppCount               int                    `yaml:"maclaw_app_count,omitempty" json:"maclaw_app_count,omitempty"`
	MaclawAppEntry               string                 `yaml:"maclaw_app_entry,omitempty" json:"maclaw_app_entry,omitempty"`
	MaclawAppID                  string                 `yaml:"maclaw_app_id,omitempty" json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                 `yaml:"maclaw_app_name,omitempty" json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                 `yaml:"maclaw_app_description,omitempty" json:"maclaw_app_description,omitempty"`
	MaclawAppCategory            string                 `yaml:"maclaw_app_category,omitempty" json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                 `yaml:"maclaw_app_icon,omitempty" json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                 `yaml:"maclaw_app_input_mode,omitempty" json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string               `yaml:"maclaw_app_output_modes,omitempty" json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                 `yaml:"maclaw_app_definition_sha256,omitempty" json:"maclaw_app_definition_sha256,omitempty"`
	MaclawAppTestEvidence        *MaclawAppTestEvidence `yaml:"maclaw_app_test_evidence,omitempty" json:"maclaw_app_test_evidence,omitempty"`
	ArtifactContractRequired     bool                   `yaml:"artifact_contract_required,omitempty" json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string               `yaml:"artifact_contract_output_modes,omitempty" json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                 `yaml:"artifact_contract_presentation,omitempty" json:"artifact_contract_presentation,omitempty"`

	// Extra 保留未识别字段，确保 round-trip 安全。
	Extra map[string]any `yaml:"-" json:"-"`
}

type MaclawAppTestEvidence struct {
	RunID                  string           `yaml:"run_id,omitempty" json:"run_id,omitempty"`
	VerifiedAt             string           `yaml:"verified_at,omitempty" json:"verified_at,omitempty"`
	DefinitionFingerprint  string           `yaml:"definition_fingerprint,omitempty" json:"definition_fingerprint,omitempty"`
	AppKind                string           `yaml:"app_kind,omitempty" json:"app_kind,omitempty"`
	ArtifactPresent        bool             `yaml:"artifact_present,omitempty" json:"artifact_present,omitempty"`
	ArtifactName           string           `yaml:"artifact_name,omitempty" json:"artifact_name,omitempty"`
	OutputCount            int              `yaml:"output_count,omitempty" json:"output_count,omitempty"`
	PrimaryResult          string           `yaml:"primary_result,omitempty" json:"primary_result,omitempty"`
	ResultPayload          map[string]any   `yaml:"result_payload,omitempty" json:"result_payload,omitempty"`
	ApprovalInstance       map[string]any   `yaml:"approval_instance,omitempty" json:"approval_instance,omitempty"`
	ProgressInstances      []map[string]any `yaml:"progress_instances,omitempty" json:"progress_instances,omitempty"`
	ApprovalViews          []string         `yaml:"approval_views,omitempty" json:"approval_views,omitempty"`
	DependencyVerification map[string]any   `yaml:"dependency_verification,omitempty" json:"dependency_verification,omitempty"`
	WorkspaceLayout        map[string]any   `yaml:"workspace_layout,omitempty" json:"workspace_layout,omitempty"`
	DataSrvRegistration    map[string]any   `yaml:"datasrv_registration,omitempty" json:"datasrv_registration,omitempty"`
	WorkflowContract       map[string]any   `yaml:"workflow_contract,omitempty" json:"workflow_contract,omitempty"`
}

func cloneSkillMarketAnyMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return value
	}
	return cloned
}

// ParseSkillYAML 解析 skill.yaml 为 SkillMetadata。
// 未识别字段保留在 Extra 中，确保 round-trip 安全。
func ParseSkillYAML(data []byte) (*SkillMetadata, error) {
	// 先解析到 map 获取所有字段
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse skill.yaml: %w", err)
	}
	if raw == nil {
		return nil, fmt.Errorf("parse skill.yaml: empty document")
	}

	// 解析已知字段
	var meta SkillMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse skill.yaml: %w", err)
	}

	// 收集未识别字段到 Extra
	knownKeys := map[string]bool{
		"id": true, "name": true, "description": true, "tags": true,
		"triggers": true, "version": true, "author": true,
		"price": true, "permissions": true, "required_env": true,
		"pricing_mode": true, "platforms": true, "requires_gui": true,
		"product_kind": true, "is_maclaw_app": true, "maclaw_app_count": true, "maclaw_app_entry": true,
		"maclaw_app_id": true, "maclaw_app_name": true, "maclaw_app_description": true,
		"maclaw_app_category": true, "maclaw_app_icon": true, "maclaw_app_input_mode": true,
		"maclaw_app_output_modes": true, "maclaw_app_definition_sha256": true,
		"maclaw_app_test_evidence":   true,
		"artifact_contract_required": true, "artifact_contract_output_modes": true,
		"artifact_contract_presentation": true,
	}
	extra := make(map[string]any)
	for k, v := range raw {
		if !knownKeys[k] {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		meta.Extra = extra
	}

	return &meta, nil
}

// FormatSkillYAML 将 SkillMetadata 格式化为 YAML 文本。
// Extra 中的未识别字段会被保留输出。
func FormatSkillYAML(meta *SkillMetadata) ([]byte, error) {
	// 先序列化已知字段到 map
	data, err := yaml.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("format skill.yaml: %w", err)
	}

	// 如果没有 Extra 字段，直接返回
	if len(meta.Extra) == 0 {
		return data, nil
	}

	// 有 Extra 字段时，合并到输出
	var known map[string]any
	if err := yaml.Unmarshal(data, &known); err != nil {
		return nil, fmt.Errorf("format skill.yaml: re-parse: %w", err)
	}
	if known == nil {
		known = make(map[string]any)
	}
	for k, v := range meta.Extra {
		known[k] = v
	}
	merged, err := yaml.Marshal(known)
	if err != nil {
		return nil, fmt.Errorf("format skill.yaml: merge: %w", err)
	}
	return merged, nil
}

// ValidateMetadata 检查 SkillMetadata 的必填字段。
func ValidateMetadata(meta *SkillMetadata) []string {
	var errs []string
	if meta.Name == "" {
		errs = append(errs, "name is required")
	}
	if meta.Description == "" {
		errs = append(errs, "description is required")
	}
	if meta.Price < 0 {
		errs = append(errs, "price must be non-negative")
	}
	return errs
}
