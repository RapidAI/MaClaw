package skill

// HubSkillMeta 是 SkillHub 中 Skill 的元数据。
type HubSkillMeta struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	TrustLevel  string   `json:"trust_level"` // "official", "community", "unknown"
	Downloads   int      `json:"downloads"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
	Visible     bool     `json:"visible"`
	RatingSum   int      `json:"rating_sum"`
	RatingCount int      `json:"rating_count"`
	AvgRating   float64  `json:"avg_rating"`

	// SkillMarket 扩展字段
	Price              int      `json:"price,omitempty"`               // Credits 价格，0 = 免费
	UploaderID         string   `json:"uploader_id,omitempty"`         // 上传者 user_id
	UploaderEmail      string   `json:"uploader_email,omitempty"`      // 上传者 email
	DownloadCount      int      `json:"download_count,omitempty"`      // 下载计数（原子递增）
	Status             string   `json:"status,omitempty"`              // pending/trial/published/pending_review/rejected/withdrawn/superseded
	Fingerprint        string   `json:"fingerprint,omitempty"`         // uploader_email:skill_name
	PreWithdrawnStatus string   `json:"pre_withdrawn_status,omitempty"` // 下架前的状态
	TrialExpireAt      string   `json:"trial_expire_at,omitempty"`     // 试用期到期时间
	SecurityLabels     []string `json:"security_labels,omitempty"`     // 安全标签
	Permissions        []string `json:"permissions,omitempty"`         // 声明的权限
	RequiredEnv        []string `json:"required_env,omitempty"`        // 需要的环境变量/API Key
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
