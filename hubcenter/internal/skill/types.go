package skill

// HubSkillMeta 是 SkillHub 中 Skill 的元数据。
type HubSkillMeta struct {
	ID          string   `json:"id"`
	SkillID     string   `json:"skill_id,omitempty"` // Stable external identifier (publisher.skill-name)
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	SemVer      string   `json:"semver,omitempty"` // Semantic version from skill.yaml (e.g. "1.3.0")
	Author      string   `json:"author"`
	TrustLevel  string   `json:"trust_level"` // "builtin", "trusted", "official", "community", "agent-created"
	Downloads   int      `json:"downloads"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Visible     bool     `json:"visible"`
	RatingSum   int      `json:"rating_sum"`
	RatingCount int      `json:"rating_count"`
	AvgRating   float64  `json:"avg_rating"`
	// VersionCount and VersionHistory are presentation fields populated when a
	// catalog is listed. They group revisions of the same stable SkillID while
	// keeping the current (latest) revision as the catalog item.
	VersionCount   int                   `json:"version_count,omitempty"`
	VersionHistory []SkillVersionSummary `json:"version_history,omitempty"`

	// SkillMarket 扩展字段
	Price                        int                    `json:"price,omitempty"`                          // Credits 价格，0 = 免费
	UploaderID                   string                 `json:"uploader_id,omitempty"`                    // 上传者 user_id
	UploaderEmail                string                 `json:"uploader_email,omitempty"`                 // 上传者 email
	DownloadCount                int                    `json:"download_count,omitempty"`                 // 下载计数（原子递增）
	Status                       string                 `json:"status,omitempty"`                         // pending/trial/published/pending_review/rejected/withdrawn/superseded
	Fingerprint                  string                 `json:"fingerprint,omitempty"`                    // uploader_email:skill_name
	PreWithdrawnStatus           string                 `json:"pre_withdrawn_status,omitempty"`           // 下架前的状态
	TrialExpireAt                string                 `json:"trial_expire_at,omitempty"`                // 试用期到期时间
	SecurityLabels               []string               `json:"security_labels,omitempty"`                // 安全标签
	Permissions                  []string               `json:"permissions,omitempty"`                    // 声明的权限
	RequiredEnv                  []string               `json:"required_env,omitempty"`                   // 需要的环境变量/API Key
	Platforms                    []string               `json:"platforms,omitempty"`                      // "windows","linux","macos"; empty = universal
	RequiresGUI                  bool                   `json:"requires_gui,omitempty"`                   // Linux 下是否需要 GUI 环境
	SourceURL                    string                 `json:"source_url,omitempty"`                     // 远程导入来源 URL
	ProductKind                  string                 `json:"product_kind,omitempty"`                   // maclaw_app_skill 等产品类型
	IsMaclawApp                  bool                   `json:"is_maclaw_app,omitempty"`                  // 是否为 MaClaw App Skill
	MaclawAppCount               int                    `json:"maclaw_app_count,omitempty"`               // App 定义数量
	MaclawAppEntry               string                 `json:"maclaw_app_entry,omitempty"`               // App 定义入口文件
	MaclawAppID                  string                 `json:"maclaw_app_id,omitempty"`                  // App ID
	MaclawAppName                string                 `json:"maclaw_app_name,omitempty"`                // App 展示名
	MaclawAppDescription         string                 `json:"maclaw_app_description,omitempty"`         // App 描述
	MaclawAppCategory            string                 `json:"maclaw_app_category,omitempty"`            // App 分类
	MaclawAppIcon                string                 `json:"maclaw_app_icon,omitempty"`                // App 图标
	MaclawAppInputMode           string                 `json:"maclaw_app_input_mode,omitempty"`          // App 输入模式
	MaclawAppOutputModes         []string               `json:"maclaw_app_output_modes,omitempty"`        // App 输出类型
	MaclawAppDefinitionSHA256    string                 `json:"maclaw_app_definition_sha256,omitempty"`   // App 描述文件 SHA256
	MaclawAppTestEvidence        *MaclawAppTestEvidence `json:"maclaw_app_test_evidence,omitempty"`       // App 测试证据摘要
	ArtifactContractRequired     bool                   `json:"artifact_contract_required,omitempty"`     // 是否声明输出产物
	ArtifactContractOutputModes  []string               `json:"artifact_contract_output_modes,omitempty"` // 输出产物类型
	ArtifactContractPresentation string                 `json:"artifact_contract_presentation,omitempty"` // 产物呈现方式
}

// SkillVersionSummary is the lightweight revision data needed to present a
// skill's version history. The complete definition remains available through
// the existing GET skill endpoint when a revision is selected.
type SkillVersionSummary struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	SemVer    string `json:"semver,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Status    string `json:"status,omitempty"`
	Visible   bool   `json:"visible"`
}

type MaclawAppTestEvidence struct {
	RunID                 string         `json:"run_id,omitempty"`
	VerifiedAt            string         `json:"verified_at,omitempty"`
	DefinitionFingerprint string         `json:"definition_fingerprint,omitempty"`
	ArtifactPresent       bool           `json:"artifact_present,omitempty"`
	ArtifactName          string         `json:"artifact_name,omitempty"`
	OutputCount           int            `json:"output_count,omitempty"`
	PrimaryResult         string         `json:"primary_result,omitempty"`
	ResultPayload         map[string]any `json:"result_payload,omitempty"`
}

// SkillRating 记录单个 MaClaw 对 Skill 的评分。
type SkillRating struct {
	SkillID   string `json:"skill_id"`
	MaclawID  string `json:"maclaw_id"`
	Score     int    `json:"score"` // 1-5
	CreatedAt string `json:"created_at"`
}

// HubSkillStep represents a single action within a Hub Skill.
type HubSkillStep struct {
	Action  string                 `json:"action"`
	Params  map[string]interface{} `json:"params"`
	OnError string                 `json:"on_error"`
}

// HubSkillFull 包含完整的 Skill 定义，用于下载。
type HubSkillFull struct {
	HubSkillMeta
	Triggers     []string          `json:"triggers"`
	Steps        []HubSkillStep    `json:"steps"`
	Manifest     SkillManifest     `json:"manifest"`
	Files        map[string]string `json:"files,omitempty"`
	AgentSkillMD string            `json:"agent_skill_md,omitempty"`
}

// SkillManifest 描述 Skill 的依赖和兼容性。
type SkillManifest struct {
	MinMaclawVersion string            `json:"min_maclaw_version,omitempty"`
	RequiredMCP      []string          `json:"required_mcp,omitempty"`
	Permissions      []string          `json:"permissions,omitempty"`
	Dependencies     []SkillDependency `json:"dependencies,omitempty"`
	Compatibility    string            `json:"compatibility,omitempty"`
}

// SkillDependency 描述一个运行时依赖。
type SkillDependency struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// SkillSearchResult 搜索结果的分页包装。
type SkillSearchResult struct {
	Skills []HubSkillMeta `json:"skills"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
}
