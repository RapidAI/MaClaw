package expert

// Expert 是租户级专家定义（多端同步资源，Hub 为权威存储）。
type Expert struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"-"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Icon         string   `json:"icon"`
	SystemPrompt string   `json:"system_prompt"`
	Tools        []string `json:"tools"`
	Skills       []string `json:"skills"`
	CreatedAt    string   `json:"created_at"` // RFC3339
	UpdatedAt    string   `json:"updated_at"` // RFC3339
}

// CreateInput 创建/同步输入。ID 可由客户端指定（多设备同步保持 id 一致），空则服务端生成 uuid。
// UpdatedAt 可选：客户端携带时参与 LWW（last-write-wins）冲突裁决，空则服务端取当前时间。
type CreateInput struct {
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Icon         string   `json:"icon"`
	SystemPrompt string   `json:"system_prompt"`
	Tools        []string `json:"tools"`
	Skills       []string `json:"skills"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

// UpdateInput 局部更新，全字段可选（指针区分“未提供”与“显式置空”）。
type UpdateInput struct {
	Name         *string   `json:"name,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Icon         *string   `json:"icon,omitempty"`
	SystemPrompt *string   `json:"system_prompt,omitempty"`
	Tools        *[]string `json:"tools,omitempty"`
	Skills       *[]string `json:"skills,omitempty"`
}
