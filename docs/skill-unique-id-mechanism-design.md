# Skill 唯一标识机制设计

## 1. 问题陈述

当前 Skill 系统缺乏类似 Android `applicationId` 的全局唯一标识机制，导致：

1. **名字冲突无检测**：两个不同来源的 skill 同名时 first-wins 静默丢弃
2. **依赖声明不确定**：`manage_skill(action="run", name="pdf-tool")` 可能匹配到不同功能的同名 skill
3. **Publisher 无法声明**：`skill.yaml` 中无 `publisher` 字段，作者无法控制 namespace
4. **版本约束缺失**：App 依赖 skill 时无法指定版本范围
5. **签名/真实性验证缺失**：任何人可以制作同名 skill 替换正版

## 2. 设计目标

- **全局唯一**：每个 skill 有一个不可冲突的标识
- **不可变**：发布后 ID 不可修改（防止替换攻击）
- **作者可控**：skill 作者自行声明 ID（无需等服务端分配）
- **向后兼容**：未迁移的旧 skill 继续通过 name 匹配工作
- **版本感知**：依赖声明可指定版本约束
- **渐进采用**：不强制所有 skill 立即迁移

## 3. 核心概念：Skill ID

### 3.1 格式

```
<publisher>.<skill-name>
```

规则：
- `publisher`：反向域名或组织标识，3-64 字符，`[a-z0-9][a-z0-9-]{1,62}[a-z0-9]`
- `skill-name`：技能名称，2-64 字符，`[a-z0-9][a-z0-9-]{0,62}[a-z0-9]`
- 完整 ID：`<publisher>.<skill-name>`，6-129 字符（3+1+2 最小，64+1+64 最大）
- 示例：`lovstudio.any2pdf`、`rapidai.paper-translator`、`community.drawio-export`

### 3.2 与现有标识的关系

| 新字段 | 现有字段 | 关系 |
|--------|---------|------|
| `id` (skill.yaml) | `name` | id 是权威标识；name 是显示名（可含中文/空格） |
| `id` (skill.yaml) | `Publisher` + `Name` | 替代 `QualifiedID()` 的运行时拼接 |
| `id` (skill.yaml) | `HubSkillID` | HubSkillID 是服务端分配的内部 UUID，id 是作者声明的稳定外部标识 |
| `id` (skill.yaml) | `MaclawAppID` | MaclawAppID 是 App 层概念；skill id 是 Skill 层概念 |

### 3.3 不可变性保证

- **首次上传时**：Hub/SkillMarket 将 `id` 绑定到上传者账户
- **后续上传时**：Hub 校验 `id` 归属权（同一账户才允许上传新版本）
- **本地使用时**：`id` 在 skill.yaml 中声明即生效，无需网络

## 4. skill.yaml Schema 扩展

```yaml
# ──── 新增必需字段 ────
id: lovstudio.any2pdf           # Skill 唯一标识（发布后不可变）
version: "1.3.0"                # 语义版本号（semver）

# ──── 现有字段（不变）────
name: "Any2PDF 文档转换"          # 显示名（人类可读，可含中文）
description: "将各种文档格式转换为 PDF"
triggers: [pdf, 转换, convert]
steps:
  - action: bash
    params: {command: "python {baseDir}/scripts/convert.py {{input}}"}
```

### 4.1 `id` 字段规范

- **位置**：skill.yaml 顶层字段
- **必需性**：新 skill 推荐声明；上传到 Hub/SkillMarket 时**必需**
- **验证**：`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]\.[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`（publisher 3-64 chars，name 2-64 chars，总长 6-129）
- **不可变**：首次上传后绑定到账户，后续版本必须使用相同 id
- **唯一性**：Hub/SkillMarket 全局唯一（先到先得）

### 4.2 `version` 字段规范

- **格式**：语义版本号（semver），如 `1.3.0`、`2.0.0-beta.1`
- **位置**：skill.yaml 顶层字段（已存在但未强制使用）
- **必需性**：上传到 Hub/SkillMarket 时必需；本地可选
- **自增规则**：Hub 拒绝相同或更低版本号的重复上传

## 5. 依赖声明机制

### 5.1 Skill 间依赖（Pipeline 模式）

```yaml
id: rapidai.research-report
version: "2.0.0"
pipeline:
  - skill: lovstudio.any2pdf       # 使用 skill id 引用
    version: ">=1.2.0"             # 新增：版本约束
    params: {input: "{{draft_md}}"}
  - skill: rapidai.paper-translator
    version: "^1.0.0"
    params: {input: "{{pdf_path}}"}
```

### 5.2 MaClaw App 依赖 Skill

```yaml
# maclaw_app.yaml
app_id: enterprise.expense-approval
dependencies:
  - id: lovstudio.any2pdf
    version: ">=1.2.0,<2.0.0"
    required: true
  - id: rapidai.ocr-extract
    version: ">=3.0.0"
    required: false              # 可选依赖，缺失时降级
```

### 5.3 版本约束语法

| 语法 | 含义 | 示例 |
|------|------|------|
| `>=X.Y.Z` | 大于等于 | `>=1.2.0` |
| `<X.Y.Z` | 小于 | `<2.0.0` |
| `^X.Y.Z` | 兼容（同 major） | `^1.2.0` = `>=1.2.0,<2.0.0` |
| `~X.Y.Z` | 补丁兼容（同 minor） | `~1.2.0` = `>=1.2.0,<1.3.0` |
| `X.Y.Z` | 精确版本 | `1.2.0` |
| `*` | 任意版本 | `*` |
| 组合 | 逗号分隔 | `>=1.2.0,<2.0.0` |

## 6. 解析优先级与向后兼容

### 6.1 Skill 查找优先级（修改后的 `MatchesName`）

```
1. 精确 id 匹配：query == entry.ID（如 "lovstudio.any2pdf"）
2. 精确 QualifiedID 匹配：query == publisher:name（旧格式向后兼容）
3. 精确 Name 匹配：query == entry.Name
4. 精确 DirName 匹配：query == entry.DirName
5. SkillDir basename 匹配（兜底）
```

### 6.2 向后兼容策略

| 场景 | 行为 |
|------|------|
| 旧 skill 无 `id` 字段 | 按 name 匹配（现有行为不变）|
| 新 skill 有 `id` 字段 | 优先按 id 匹配；name 作为 fallback |
| `manage_skill(name="any2pdf")` | 先按 id 搜索 `*.any2pdf`，再按 name 搜索 |
| 安装同 name 不同 id 的 skill | 允许并存（不同 id = 不同 skill）|
| 安装同 id 不同版本 | 按版本管理规则处理（升级/降级/并存策略）|

### 6.3 名字冲突处理（新机制）

```
场景：本地有两个 skill，name 都是 "pdf-converter"
  - id: lovstudio.pdf-converter (version 1.3.0)
  - id: community.pdf-converter (version 2.0.0)

行为：
  - manage_skill(name="pdf-converter") → 返回歧义错误，列出两个 id
  - manage_skill(name="lovstudio.pdf-converter") → 精确匹配第一个
  - manage_skill(id="lovstudio.pdf-converter") → 精确匹配第一个（推荐）
```

## 7. Hub/SkillMarket/HubCenter 服务端变更

### 7.1 现状分析

当前服务端（`hubcenter/internal/skillmarket/processor.go`）使用：
- **`Fingerprint`** = `email:skill_name`（归属判断——同 email + 同 name = 同一个 skill 的更新）
- **`ID`**（UUID）= 服务端分配或包内携带的 UUID（内部主键）
- 归属校验逻辑：包内 UUID 存在时，检查 `existing.Fingerprint == fingerprint`，匹配则复用 ID（视为更新），不匹配则视为新 skill

**问题**：
1. `Fingerprint` 基于 `name`——改名后被视为新 skill，旧版本被遗弃
2. UUID 是机器生成的内部标识，用户无法记忆或声明依赖
3. 无归属权转移机制
4. 不同 SkillMarket 实例（企业 Hub vs 公共 HubCenter）之间 UUID 不互通

### 7.2 新增：Skill ID 归属绑定机制

#### 核心不变量

> **一个 `skill_id`（如 `lovstudio.any2pdf`）一旦被某个账户首次上传，就永久绑定到该账户。其他账户无法上传同一 `skill_id` 的新版本。**

这与 npm 的 package scope、Docker Hub 的 repository ownership、Google Play 的 applicationId 是同一个模式。

#### 7.2.1 `processor.processOne` 改造（核心变更）

```go
func (p *Processor) processOne(ctx context.Context, subID string) error {
    // ... 现有的 unzip、validate、security scan ...
    
    // ──── 新增：Skill ID 归属校验 ────
    skillID := meta.SkillID  // 从 skill.yaml 的 id 字段读取（如 "lovstudio.any2pdf"）
    
    if skillID == "" {
        // 旧 skill 无 id 字段 → 回退到现有 fingerprint 逻辑（向后兼容）
        // ... 现有的 UUID 生成/复用逻辑不变 ...
    } else {
        // 新 skill 有 id 字段 → 走归属绑定逻辑
        if !IsValidSkillID(skillID) {
            return p.failSubmission(ctx, sub, fmt.Sprintf(
                "skill id %q 格式无效（要求: <publisher>.<skill-name>，仅小写字母、数字、连字符）", skillID))
        }
        
        // 查询归属表
        owner, err := p.store.GetSkillIDOwner(ctx, skillID)
        if err != nil {
            return p.failSubmission(ctx, sub, "归属查询失败: "+err.Error())
        }
        
        if owner == nil {
            // ━━ 首次上传：注册归属 ━━
            if err := p.store.RegisterSkillIDOwnership(ctx, skillID, sub.UserID, sub.Email); err != nil {
                return p.failSubmission(ctx, sub, "归属注册失败: "+err.Error())
            }
            log.Printf("[skillmarket] skill_id %s registered to user %s (%s)", skillID, sub.UserID, sub.Email)
        } else {
            // ━━ 后续上传：校验归属权 ━━
            if owner.UserID != sub.UserID {
                return p.failSubmission(ctx, sub, fmt.Sprintf(
                    "skill_id %q 已被其他用户注册（所有者: %s），无法上传。如果这是你的 skill，请联系管理员。",
                    skillID, owner.MaskedEmail()))
                // MaskedEmail(): "lov***@gmail.com"
            }
            log.Printf("[skillmarket] skill_id %s ownership verified for user %s", skillID, sub.UserID)
        }
        
        // skill_id 作为 skill 的 stable external identifier
        // UUID 仍然作为内部存储主键（向后兼容）
    }
    
    // ... 继续现有的 version 管理、publish 逻辑 ...
}
```

#### 7.2.2 版本号校验（防止降级覆盖）

```go
// 在归属校验通过后，检查版本号
if skillID != "" && meta.Version != "" {
    latestVersion, err := p.store.GetLatestVersionForSkillID(ctx, skillID)
    if err == nil && latestVersion != "" {
        if !isVersionGreater(meta.Version, latestVersion) {
            return p.failSubmission(ctx, sub, fmt.Sprintf(
                "版本号 %s 必须大于已发布的最新版本 %s", meta.Version, latestVersion))
        }
    }
}
```

#### 7.2.3 数据库 Schema 变更

```sql
-- ━━ 归属绑定表（核心新增）━━
-- 每个 skill_id 只有一条记录，绑定到注册者
CREATE TABLE IF NOT EXISTS sm_skill_id_ownership (
    skill_id      VARCHAR(128) PRIMARY KEY,    -- "lovstudio.any2pdf"
    user_id       VARCHAR(64) NOT NULL,        -- SkillMarket user ID
    email         VARCHAR(256) NOT NULL,       -- 注册时的 email
    registered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- 归属转移时更新以下字段
    transferred_from VARCHAR(64),              -- 前任所有者（审计）
    transferred_at   TIMESTAMP
);
CREATE INDEX idx_skill_ownership_user ON sm_skill_id_ownership(user_id);

-- ━━ 版本历史表（skill_id 维度）━━
-- 一个 skill_id 对应多个版本，每个版本对应一个 internal UUID
CREATE TABLE IF NOT EXISTS sm_skill_versions (
    skill_id      VARCHAR(128) NOT NULL,       -- "lovstudio.any2pdf"
    version       VARCHAR(32) NOT NULL,        -- "1.3.0" (semver)
    internal_id   VARCHAR(64) NOT NULL,        -- 内部 UUID（指向 skill files）
    package_sha256 VARCHAR(64),                -- 包 SHA256
    uploaded_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    uploader_id   VARCHAR(64) NOT NULL,
    PRIMARY KEY (skill_id, version)
);
CREATE INDEX idx_skill_versions_internal ON sm_skill_versions(internal_id);

-- ━━ 现有 skill JSON 文件中新增字段 ━━
-- HubSkillMeta.SkillID = "lovstudio.any2pdf"（新增，与 ID(UUID) 并存）
-- HubSkillMeta.SemVer  = "1.3.0"（新增，与 Version(int) 并存）
```

#### 7.2.4 `HubSkillMeta` 类型扩展

```go
// hubcenter/internal/skill/types.go
type HubSkillMeta struct {
    ID          string `json:"id"`                        // 内部 UUID（保留，向后兼容）
    SkillID     string `json:"skill_id,omitempty"`        // 新增：外部稳定标识 "publisher.name"
    SemVer      string `json:"semver,omitempty"`          // 新增：语义版本号 "1.3.0"
    // ... 现有字段不变 ...
}
```

#### 7.2.5 `SkillMetadata` 扩展（validator.go）

```go
// hubcenter/internal/skillmarket/metadata.go / validator.go
type SkillMetadata struct {
    // ... 现有字段 ...
    SkillID string `yaml:"id,omitempty" json:"skill_id,omitempty"`  // 从 skill.yaml 的 id 字段读取
    SemVer  string `yaml:"version,omitempty" json:"semver,omitempty"` // 从 skill.yaml 的 version 字段读取
}
```

### 7.3 搜索/下载 API 扩展

#### 搜索结果包含 skill_id

```json
// GET /api/v1/skills/search?q=pdf
{
    "skills": [
        {
            "id": "uuid-xxx",
            "skill_id": "lovstudio.any2pdf",   // 新增
            "name": "Any2PDF",
            "semver": "1.3.0",                 // 新增
            "version": "3",                    // 保留（内部版本计数）
            "author": "lovstudio",
            ...
        }
    ]
}
```

#### 按 skill_id 下载

```
GET /api/v1/skills/by-skill-id/{skill_id}/download
GET /api/v1/skills/by-skill-id/{skill_id}/download?version=1.3.0
GET /api/v1/skills/by-skill-id/{skill_id}/download?constraint=>=1.2.0,<2.0.0
```

回退路径：按 UUID 下载仍然工作（`GET /api/v1/skills/{uuid}/download`），向后兼容。

### 7.4 管理员操作

#### 归属权转移（管理员 API）

```
POST /api/v1/admin/skill-ownership/transfer
{
    "skill_id": "lovstudio.any2pdf",
    "from_user_id": "user-old",
    "to_user_id": "user-new",
    "reason": "组织变更"
}
```

- 仅管理员可操作
- 记录审计日志（`transferred_from`、`transferred_at`）
- 自动更新所有关联的 `UploaderEmail`/`Fingerprint`

#### 归属权注销（废弃 skill_id）

```
POST /api/v1/admin/skill-ownership/revoke
{
    "skill_id": "lovstudio.any2pdf",
    "reason": "违规"
}
```

- 软删除：标记为 `revoked`，不物理删除
- 该 skill_id 不可被任何人重新注册（防止冒用）

### 7.5 Hub（企业内部）的归属策略

企业 Hub 与公共 SkillMarket 的差异：

| 方面 | 公共 SkillMarket | 企业 Hub |
|------|-----------------|---------|
| 归属绑定粒度 | 个人账户 | 企业租户（tenant_id） |
| publisher 前缀 | 自由声明（先到先得） | 限制为企业域名 |
| 归属转移 | 需管理员 | 租户管理员可操作 |
| 跨实例同步 | 不同步 | HubCenter ↔ Hub 同步归属表 |

企业 Hub 配置选项：
```json
{
    "skill_id_policy": {
        "publisher_prefix_required": "enterprise",  // 强制前缀
        "allow_community_prefix": true,             // 是否允许 community.* 前缀
        "ownership_scope": "tenant"                 // "tenant" 或 "user"
    }
}
```

### 7.6 完整的上传流程（修改后）

```
客户端                              SkillMarket/Hub 服务端
────────                           ────────────────────────
1. skill.yaml 声明 id + version
2. PrepareSkillForUpload()
   - 校验 id 格式 ✓
   - 校验 version 格式 ✓
   - 可移植性检查 ✓
3. 打包 zip
4. POST /api/v1/skills/submit
   (zip + email/token)
                                   5. 解压 + ValidatePackage
                                      - 读取 skill.yaml 中的 id/version
                                   6. 安全扫描
                                   7. Skill ID 归属校验 ←── 核心新增
                                      ├── 首次上传 → RegisterOwnership
                                      ├── 归属匹配 → 继续
                                      └── 归属不匹配 → 拒绝（403）
                                   8. 版本号校验
                                      └── version <= latest → 拒绝（409）
                                   9. Publish（写入 skill store + FTS 索引）
                                   10. 返回 submission_id + skill_id
```

### 7.7 Patch/Update 时维护 ID 不变性

当用户通过 `manage_skill(action="patch")` 修改 skill 定义时：

```go
// gui/im_tools_misc.go toolPatchSkill()
func (h *IMMessageHandler) toolPatchSkill(args map[string]interface{}) string {
    // ... 现有 patch 逻辑 ...
    
    // ──── 新增：ID 不可变保护 ────
    if patchedEntry.ID != "" && originalEntry.ID != "" && patchedEntry.ID != originalEntry.ID {
        return "错误：skill id 不可修改（当前 id: " + originalEntry.ID + "）。" +
               "如果需要更换 id，请创建新 skill 并迁移。"
    }
    // 确保 patch 后 id 字段不被清空
    if originalEntry.ID != "" && patchedEntry.ID == "" {
        patchedEntry.ID = originalEntry.ID
    }
}
```

同样，`write_file` 覆盖 `skill.yaml` 时需要校验：

```go
// skill.yaml 写入后的 post-validation hook
// 在 refreshSkillIndexesAfterMutation 中检查 id 一致性
func validateSkillIDConsistency(oldEntry, newEntry *corelib.NLSkillEntry) error {
    if oldEntry.ID != "" && newEntry.ID != "" && oldEntry.ID != newEntry.ID {
        return fmt.Errorf("skill id changed from %q to %q — this is not allowed for published skills", 
            oldEntry.ID, newEntry.ID)
    }
    return nil
}
```

## 8. 客户端变更

### 8.1 `corelib/types.go` — NLSkillEntry 新增字段

```go
type NLSkillEntry struct {
    // ──── 新增 ────
    ID      string `json:"id,omitempty" yaml:"id,omitempty"`       // 全局唯一标识（publisher.skill-name）
    Version string `json:"version,omitempty" yaml:"version,omitempty"` // 语义版本号（已有字段，提升为核心）
    
    // ──── 现有字段（不变）────
    Name        string `json:"name"`
    Publisher   string `json:"publisher,omitempty"` // 从 ID 中解析，运行时填充
    HubSkillID  string `json:"hub_skill_id,omitempty"` // 服务端内部 UUID（保留向后兼容）
    HubVersion  string `json:"hub_version,omitempty"`  // 保留向后兼容
    // ...
}
```

### 8.2 `corelib/skill/scanner.go` — SkillYAMLFile 新增字段

```go
type SkillYAMLFile struct {
    // ──── 新增 ────
    ID      string `yaml:"id,omitempty"`      // 全局唯一标识
    
    // ──── 现有字段（不变）────
    Name    string `yaml:"name"`
    Version string `yaml:"version,omitempty"` // 已存在，提升重要性
    // ...
}
```

### 8.3 `QualifiedID()` 方法改造

```go
// QualifiedID returns the canonical unique identifier for this skill.
// Priority: ID field (publisher.name format) > Publisher:Name > bare Name.
func (e *NLSkillEntry) QualifiedID() string {
    // 优先使用新的 id 字段
    if id := strings.TrimSpace(e.ID); id != "" {
        return id
    }
    // 向后兼容：旧格式 publisher:name
    name := strings.TrimSpace(e.Name)
    if name == "" {
        return ""
    }
    publisher := strings.TrimSpace(e.Publisher)
    if publisher != "" {
        return publisher + ":" + name
    }
    return name
}
```

### 8.4 ID 不可变保护——本地修改时的校验

#### 8.4.1 `toolPatchSkill` 保护

```go
// gui/im_tools_misc.go — toolPatchSkill()
// patch 操作不允许修改已声明的 skill id
func (h *IMMessageHandler) toolPatchSkill(args map[string]interface{}) string {
    // ... 现有 patch 逻辑 ...
    
    if patchedEntry.ID != "" && originalEntry.ID != "" && patchedEntry.ID != originalEntry.ID {
        return "错误：skill id 不可修改（当前 id: " + originalEntry.ID + "）。" +
               "如果需要更换 id，请创建新 skill 并迁移。"
    }
    if originalEntry.ID != "" && patchedEntry.ID == "" {
        patchedEntry.ID = originalEntry.ID  // 防止意外清空
    }
}
```

#### 8.4.2 `write_file` 覆盖 skill.yaml 时的 post-validation

当 LLM 或用户通过 `write_file` 直接覆盖 `skill.yaml` 时，`refreshSkillIndexesAfterMutation` 检测 id 变更：

```go
// gui/app_nl_skills.go — refreshSkillIndexesAfterMutation()
func (e *SkillExecutor) refreshSkillIndexesAfterMutation(name string) {
    // ... 现有 rescan 逻辑 ...
    
    // 新增：检查 id 一致性
    oldEntry := e.findSkillByName(name) // 内存中的旧版本
    newEntry := rescanFromDisk(name)    // 磁盘上的新版本
    
    if oldEntry != nil && oldEntry.ID != "" && newEntry != nil {
        if newEntry.ID == "" {
            // skill.yaml 被覆写后 id 丢失 → 自动恢复
            restoreSkillIDToYAML(newEntry.SkillDir, oldEntry.ID)
            log.Printf("[skill] restored id %q to skill.yaml after write_file overwrote it", oldEntry.ID)
        } else if newEntry.ID != oldEntry.ID {
            // id 被修改 → 回滚 skill.yaml 到 .bak 并警告
            log.Printf("[skill] WARNING: skill id changed from %q to %q, reverting", oldEntry.ID, newEntry.ID)
            rollbackSkillYAML(newEntry.SkillDir)
        }
    }
}
```

#### 8.4.3 Scanner 加载时自动填充 Publisher

```go
// corelib/skill/scanner.go — loadSkillFromDir()
func loadSkillFromDir(skillDir, fallbackName string) (*corelib.NLSkillEntry, string, error) {
    // ... 现有加载逻辑 ...
    
    // 新增：从 skill.yaml 的 id 字段读取
    if sf.ID != "" {
        entry.ID = sf.ID
        // 自动解析 publisher（id 格式: publisher.skill-name）
        if dot := strings.IndexByte(sf.ID, '.'); dot > 0 {
            entry.Publisher = sf.ID[:dot]
        }
    }
}
```

#### 8.4.4 安装时持久化 id

从 Hub/SkillMarket 下载安装时，将服务端返回的 `skill_id` 写入本地 `skill.yaml`：

```go
// corelib/skill/hub_search.go — DownloadSkillHub()
func (c *HubClient) DownloadSkillHub(ctx context.Context, hubURL, skillID string) (*corelib.NLSkillEntry, error) {
    // ... 现有下载逻辑 ...
    
    // 新增：持久化 skill_id 到本地条目
    entry.ID = full.SkillID  // 从服务端响应中读取 "skill_id" 字段
    if entry.ID != "" {
        if dot := strings.IndexByte(entry.ID, '.'); dot > 0 {
            entry.Publisher = entry.ID[:dot]
        }
    }
    return entry, nil
}
```

#### 8.4.5 本地创建 skill 时引导声明 id

当 LLM 通过 `craft_tool` 或 `manage_skill(action="patch")` 创建新 skill 时，在 skill.yaml 模板中预置 id 占位符：

```yaml
# 新 skill 的 skill.yaml 模板
id: ""  # TODO: 上传前必须填写，格式: publisher.skill-name
version: "1.0.0"
name: "{{skill_name}}"
```

上传 preflight 检查 id 为空时提示：
```
上传被阻止：skill.yaml 缺少 id 字段。
请声明 id（格式: publisher.skill-name，如 lovstudio.any2pdf）。
id 一旦上传将与你的账户绑定，不可更改。
```

### 8.5 版本约束解析（新包 `corelib/skill/semver.go`）

```go
package skill

// VersionConstraint represents a parsed version requirement.
type VersionConstraint struct {
    Raw        string   // 原始字符串，如 ">=1.2.0,<2.0.0"
    Ranges     []Range  // 解析后的范围列表（AND 关系）
}

// Satisfies checks if a version satisfies this constraint.
func (c *VersionConstraint) Satisfies(version string) bool

// ParseVersionConstraint parses a constraint string.
func ParseVersionConstraint(s string) (*VersionConstraint, error)
```

### 8.6 本地冲突检测（修改 `ScanSkillDir`）

```go
func scanSkillDirInternal(root string, filterPlatform bool) []corelib.NLSkillEntry {
    // ... existing scan logic ...
    
    // NEW: detect ID conflicts
    idMap := make(map[string]string) // id → first skillDir
    for _, entry := range result {
        if entry.ID == "" {
            continue
        }
        if firstDir, exists := idMap[entry.ID]; exists {
            log.Printf("[skill-scanner] WARNING: duplicate skill ID %q in %s and %s",
                entry.ID, firstDir, entry.SkillDir)
        } else {
            idMap[entry.ID] = entry.SkillDir
        }
    }
    return result
}
```

### 8.7 依赖解析器（新文件 `corelib/skill/dependency.go`）

```go
package skill

// SkillDependency declares a dependency on another skill.
type SkillDependency struct {
    ID         string `json:"id" yaml:"id"`                   // required: skill id
    Version    string `json:"version,omitempty" yaml:"version,omitempty"` // version constraint
    Required   bool   `json:"required,omitempty" yaml:"required,omitempty"`
}

// ResolveDependency finds the best local match for a dependency.
// Returns the matched entry and whether the version constraint is satisfied.
func ResolveDependency(dep SkillDependency, installed []corelib.NLSkillEntry) (*corelib.NLSkillEntry, bool, error)
```

### 8.8 upload_status.json 扩展

上传成功后，`upload_status.json` 记录 skill_id 用于后续更新：

```json
{
    "submission_id": "sub_xxx",
    "skill_id": "lovstudio.any2pdf",
    "version": "1.3.0",
    "uploaded_at": "2026-07-07T10:00:00Z"
}
```

下次上传时，客户端校验本地 `skill.yaml` 中的 id 与 `upload_status.json` 中的 `skill_id` 一致。

## 9. ID 维护矩阵——所有修改/更新场景的 ID 保护

### 9.1 全场景覆盖表

| # | 操作场景 | 谁触发 | ID 维护规则 | 实施位置 |
|---|---------|--------|------------|---------|
| 1 | 首次创建（craft_tool/手动） | LLM/用户 | id 可为空（本地使用） | `loadSkillFromDir` |
| 2 | 首次上传到 Hub | 用户 | id 必须非空且格式合法，归属绑定到账户 | `processor.processOne` + `PrepareSkillForUpload` |
| 3 | 更新上传（同 id 新 version） | 用户 | 归属校验（必须同一账户），version 必须递增 | `processor.processOne` |
| 4 | `manage_skill(action="patch")` | LLM | id 不可修改，清空则自动恢复 | `toolPatchSkill` |
| 5 | `write_file` 覆盖 skill.yaml | LLM | 检测 id 变更，变更则回滚；清空则恢复 | `refreshSkillIndexesAfterMutation` |
| 6 | 从 Hub/Market 安装 | 系统 | 继承服务端返回的 skill_id，写入本地 | `DownloadSkillHub/ClawHub/GitHub` |
| 7 | Hub 推送更新 | 系统 | skill_id 不变，version 递增 | `CheckUpdate` + `ApplyUpdate` |
| 8 | 目录重命名 | 用户 | id 不变（id 在 skill.yaml 内，不依赖目录名） | `ScanSkillDir` |
| 9 | skill 复制/fork | 用户 | 必须修改 id（否则冲突检测报警） | 本地冲突检测 |
| 10 | 旧 skill 迁移（无 id） | 系统 | 保持 name-based 匹配，不强制迁移 | `QualifiedID()` 回退 |

### 9.2 详细场景设计

#### 场景 2: 首次上传时 id 绑定

```
时间线：
T0: 用户创建 skill，skill.yaml 声明 id: "lovstudio.any2pdf", version: "1.0.0"
T1: 用户调用 manage_skill(action="upload", name="any2pdf")
T2: 客户端 PrepareSkillForUpload → 校验 id 格式 → 校验 version 格式 → 可移植性检查
T3: 客户端打包 zip，POST /api/v1/skills/submit
T4: 服务端 processOne:
    - 读取 skill.yaml 中 id = "lovstudio.any2pdf"
    - 查询 sm_skill_id_ownership 表：无记录
    - INSERT INTO sm_skill_id_ownership (skill_id, user_id, email) VALUES ("lovstudio.any2pdf", "user-123", "lov@gmail.com")
    - 正常发布
T5: 客户端收到成功，更新 upload_status.json:
    {"submission_id": "sub_xxx", "skill_id": "lovstudio.any2pdf", "version": "1.0.0"}
```

#### 场景 3: 后续更新上传

```
T0: 用户修改 skill，version 从 "1.0.0" 改为 "1.1.0"
T1: 用户调用 manage_skill(action="upload", name="any2pdf")
T2: 客户端上传
T3: 服务端 processOne:
    - 读取 id = "lovstudio.any2pdf"
    - 查询 sm_skill_id_ownership: owner = user-123
    - 当前上传者 = user-123 → 归属匹配 ✓
    - 查询 sm_skill_versions: latest = "1.0.0"
    - 新版本 "1.1.0" > "1.0.0" → 版本递增 ✓
    - 正常发布
```

#### 场景 3b: 他人尝试覆盖

```
T0: 攻击者制作同名 skill，id 声明为 "lovstudio.any2pdf"
T1: 攻击者调用 manage_skill(action="upload")
T2: 服务端 processOne:
    - 读取 id = "lovstudio.any2pdf"
    - 查询 sm_skill_id_ownership: owner = user-123
    - 当前上传者 = user-456 → 归属不匹配 ✗
    - 返回错误: "skill_id 'lovstudio.any2pdf' 已被其他用户注册，无法上传"
    - submission 标记为 failed
```

#### 场景 4: LLM patch 时 id 保护

```
T0: skill.yaml 中 id = "lovstudio.any2pdf"
T1: LLM 调用 manage_skill(action="patch", name="any2pdf", mode="text", find="id: lovstudio.any2pdf", replace="id: hacker.any2pdf")
T2: toolPatchSkill 检测到 id 变更: "lovstudio.any2pdf" → "hacker.any2pdf"
T3: 返回错误: "skill id 不可修改"，patch 被拒绝
```

#### 场景 5: write_file 意外覆盖 skill.yaml

```
T0: skill.yaml 中 id = "lovstudio.any2pdf"
T1: LLM 调用 write_file(path="skills/any2pdf/skill.yaml", content="name: Any2PDF\nsteps: ...")
    （LLM 忘记写 id 字段）
T2: write_file 执行成功，磁盘上 skill.yaml 现在没有 id 字段
T3: refreshSkillIndexesAfterMutation 触发
T4: 检测到 oldEntry.ID = "lovstudio.any2pdf"，newEntry.ID = ""
T5: 自动恢复：将 "id: lovstudio.any2pdf" 写回 skill.yaml 顶部
T6: 日志: "[skill] restored id lovstudio.any2pdf to skill.yaml after write_file overwrote it"
```

#### 场景 6: 安装时继承 skill_id

```
T0: 用户搜索 "any2pdf"，搜索结果包含 skill_id = "lovstudio.any2pdf"
T1: manage_skill(action="install", name="lovstudio.any2pdf")
T2: DownloadSkillHub 下载包，解压到 skills/any2pdf/
T3: skill.yaml 已包含 id 字段（上传时就有）
T4: ScanSkillDir 加载，entry.ID = "lovstudio.any2pdf"
T5: 后续 run/update 都通过 id 精确匹配
```

#### 场景 9: Fork/复制 skill 时的冲突处理

```
T0: 本地有 skills/any2pdf/ (id = "lovstudio.any2pdf")
T1: 用户 cp -r skills/any2pdf skills/my-pdf-tool
T2: ScanSkillDir 扫描到两个 skill，id 都是 "lovstudio.any2pdf"
T3: 冲突检测警告:
    "[skill-scanner] WARNING: duplicate skill ID lovstudio.any2pdf in skills/any2pdf and skills/my-pdf-tool"
T4: 用户收到提示: "请修改 skills/my-pdf-tool/skill.yaml 中的 id 为你自己的标识（如 myname.my-pdf-tool）"
```

### 9.3 ID 校验函数（单一实现）

```go
// corelib/skill/skill_id.go
package skill

import "regexp"

var skillIDRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?\.[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// IsValidSkillID checks if the given string is a valid skill ID
// in the format "publisher.skill-name".
func IsValidSkillID(id string) bool {
    if len(id) < 6 || len(id) > 129 {
        return false
    }
    return skillIDRe.MatchString(id)
}

// ParseSkillID splits a skill ID into publisher and name components.
// Returns ("", "", false) for invalid IDs.
func ParseSkillID(id string) (publisher, name string, valid bool) {
    if !IsValidSkillID(id) {
        return "", "", false
    }
    dot := strings.IndexByte(id, '.')
    return id[:dot], id[dot+1:], true
}
```

### 9.4 数据流完整性——从声明到消费

```
┌─────────────────────────────────────────────────────────────────────┐
│                         skill.yaml (源)                              │
│  id: lovstudio.any2pdf                                              │
│  version: "1.3.0"                                                   │
└──────────┬──────────────────────────────────────────────────────────┘
           │ loadSkillFromDir (scanner.go)
           ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    NLSkillEntry (内存)                                │
│  .ID = "lovstudio.any2pdf"                                          │
│  .Version = "1.3.0"                                                 │
│  .Publisher = "lovstudio" (从 ID 解析)                                │
│  .Name = "Any2PDF" (显示名)                                          │
│  .HubSkillID = "uuid-xxx" (服务端 UUID，兼容)                         │
└──────────┬───────────────┬──────────────────────────────────────────┘
           │               │
     ┌─────▼─────┐   ┌────▼────────────────────────────────────────┐
     │ 本地使用    │   │ 上传到 Hub/SkillMarket                       │
     │            │   │                                             │
     │ MatchesName│   │ PrepareSkillForUpload                       │
     │ MatchesID  │   │   → IsValidSkillID(id) ✓                   │
     │ 冲突检测    │   │   → 可移植性 ✓                               │
     │ 依赖解析    │   │                                             │
     └────────────┘   │ POST /api/v1/skills/submit                  │
                      │   → processOne                              │
                      │     → GetSkillIDOwner(id)                   │
                      │     → 首次: RegisterOwnership(id, user)     │
                      │     → 后续: 校验 owner == uploader           │
                      │     → 版本号 > latest ✓                     │
                      │     → Publish                               │
                      └─────────────────────────────────────────────┘
                                       │
                                       ▼
                      ┌─────────────────────────────────────────────┐
                      │            sm_skill_id_ownership              │
                      │  skill_id = "lovstudio.any2pdf"              │
                      │  user_id = "user-123"                        │
                      │  email = "lov@gmail.com"                     │
                      │  registered_at = 2026-07-07                  │
                      │                                             │
                      │  不可变（除管理员 transfer）                    │
                      └─────────────────────────────────────────────┘
```

## 10. 签名机制（Phase 2，可选）

### 10.1 包完整性校验

```yaml
# skill_package_manifest.json（上传时自动生成）
{
    "id": "lovstudio.any2pdf",
    "version": "1.3.0",
    "files_sha256": {
        "skill.yaml": "abc123...",
        "scripts/convert.py": "def456...",
        "SKILL.md": "ghi789..."
    },
    "package_sha256": "xyz...",
    "signed_by": "lovstudio",
    "signature": "base64..."
}
```

### 10.2 签名验证流程

```
安装时：
1. 下载包 + manifest
2. 验证 package_sha256（包完整性）
3. 验证每个文件的 sha256（文件级完整性）
4. 验证 signature（来源真实性，使用 publisher 的公钥）
5. 验证 signed_by == id 的 publisher 前缀

运行时：
- 可选配置 "only_run_signed_skills: true"（企业安全模式）
```

## 10. Hub/HubCenter 旧 Skill 自动迁移

### 10.1 迁移时机

Hub/HubCenter 启动时执行一次性迁移（幂等——已有 skill_id 的跳过）。

### 10.2 迁移逻辑

```go
// hubcenter/internal/skill/migrate_skill_id.go

// MigrateSkillIDs is a one-time idempotent migration that assigns skill_id
// to all existing skills that don't have one. It uses the same DerivePublisher
// + SanitizeSkillNameForID logic as the upload path, ensuring consistency.
//
// Called at startup. Safe to call multiple times (skips already-migrated).
func (s *SkillStore) MigrateSkillIDs(ownershipStore SkillIDOwnershipStore) MigrationReport {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    report := MigrationReport{}
    assigned := make(map[string]string) // skill_id → internal UUID (collision detection within batch)
    
    for uuid, sk := range s.skills {
        if sk.SkillID != "" {
            report.AlreadyMigrated++
            continue // 已有 id，跳过
        }
        
        email := strings.TrimSpace(sk.UploaderEmail)
        if email == "" {
            // 从 Fingerprint 提取 email（格式: "email:name"）
            if parts := strings.SplitN(sk.Fingerprint, ":", 2); len(parts) == 2 {
                email = parts[0]
            }
        }
        if email == "" {
            report.Skipped++
            report.SkippedReasons = append(report.SkippedReasons, 
                fmt.Sprintf("%s: no uploader email", uuid))
            continue
        }
        
        publisher := cskill.DerivePublisher(email)
        name := cskill.SanitizeSkillNameForID(sk.Name)
        skillID := publisher + "." + name
        
        // 批内冲突检测（同一 publisher 同一 name 的不同 skill）
        if existingUUID, exists := assigned[skillID]; exists && existingUUID != uuid {
            // 追加 UUID 前 4 位区分
            skillID = publisher + "." + name + "-" + uuid[:4]
        }
        
        // 全局冲突检测（归属表中已有其他人注册）
        if owner := ownershipStore.GetOwner(skillID); owner != nil {
            if owner.UserID != sk.UploaderID {
                // 不同人 → 加 UUID 后缀区分
                skillID = publisher + "." + name + "-" + uuid[:4]
            }
        }
        
        // 验证生成的 ID 合法
        if !cskill.IsValidSkillID(skillID) {
            report.Skipped++
            report.SkippedReasons = append(report.SkippedReasons,
                fmt.Sprintf("%s: generated id %q invalid", uuid, skillID))
            continue
        }
        
        // 赋值
        sk.SkillID = skillID
        assigned[skillID] = uuid
        
        // 注册归属（幂等：已存在则跳过）
        _ = ownershipStore.RegisterIfAbsent(skillID, sk.UploaderID, email)
        
        // 持久化
        s.skills[uuid] = sk
        _ = s.persistSkill(sk)
        
        report.Migrated++
    }
    
    log.Printf("[skill-id-migration] migrated=%d already=%d skipped=%d",
        report.Migrated, report.AlreadyMigrated, report.Skipped)
    return report
}

type MigrationReport struct {
    Migrated        int
    AlreadyMigrated int
    Skipped         int
    SkippedReasons  []string
}
```

### 10.3 冲突处理策略

| 场景 | 处理 |
|------|------|
| 同一用户上传了同名 skill 的多个版本 | 正常——同一 skill_id 不同 version（已由 VersionManager 管理） |
| 同一用户上传了两个不同功能的同名 skill | 不可能——Fingerprint 相同时复用 UUID（现有逻辑）|
| 不同用户上传了同名 skill（不同 Fingerprint）| publisher 不同 → id 自然不同（`alice-x1y2.pdf-tool` vs `bob-a3b4.pdf-tool`）|
| 同用户邮箱前缀相同但域不同 | hash 后缀不同 → publisher 不同 → id 不同 |
| 迁移时批内生成了相同 id | 追加 UUID 前 4 位：`zhangsan-a1b2.pdf-tool-8f3a` |

### 10.4 迁移调用入口

```go
// hubcenter/cmd/main.go 或 hubcenter/internal/httpapi/server.go 启动时
func (srv *Server) startupMigrations() {
    // ... 其他迁移 ...
    
    // Skill ID 迁移（幂等，安全重复调用）
    report := srv.skillStore.MigrateSkillIDs(srv.ownershipStore)
    if report.Migrated > 0 {
        log.Printf("[startup] skill ID migration complete: %d skills assigned IDs", report.Migrated)
    }
}
```

### 10.5 SkillMarket 搜索/下载 API 返回 skill_id

迁移后，搜索结果自动包含 `skill_id` 字段（因为写入了 `HubSkillMeta.SkillID`）。客户端无需升级即可收到——旧客户端忽略未知字段，新客户端使用 `skill_id` 做精确匹配。

### 10.6 按 skill_id 下载 API（新增端点）

```
GET /api/v1/skills/by-skill-id/{skill_id}/download
GET /api/v1/skills/by-skill-id/{skill_id}/download?version=1.3.0
GET /api/v1/skills/by-skill-id/{skill_id}/download?constraint=>=1.2.0,<2.0.0
```

处理逻辑：
```go
func (h *SkillHandlers) DownloadBySkillID(w http.ResponseWriter, r *http.Request) {
    skillID := r.PathValue("skill_id")  // "lovstudio.any2pdf"
    version := r.URL.Query().Get("version")
    constraint := r.URL.Query().Get("constraint")
    
    // 1. 按 skill_id 查找 skill 列表
    candidates := h.store.FindBySkillID(skillID)
    if len(candidates) == 0 {
        smError(w, http.StatusNotFound, "skill_id not found: "+skillID)
        return
    }
    
    // 2. 版本筛选
    var target *HubSkillFull
    if version != "" {
        target = findExactVersion(candidates, version)
    } else if constraint != "" {
        target = findBestMatchingVersion(candidates, constraint)
    } else {
        target = findLatestVersion(candidates)
    }
    
    if target == nil {
        smError(w, http.StatusNotFound, "no matching version for "+skillID)
        return
    }
    
    // 3. 返回与现有 /api/v1/skills/{uuid}/download 相同的响应格式
    writeJSON(w, http.StatusOK, target)
}
```

### 10.7 App 依赖安装时按 skill_id 从 Hub/HubCenter 下载

MaClaw App 的依赖声明使用 skill_id，安装时按 skill_id 向 Hub/能力市场精确下载依赖 skill。

#### 依赖声明格式

```json
{
    "dependencies": [
        {
            "skill_id": "lovstudio.any2pdf",
            "version": ">=1.2.0",
            "required": true,
            "name": "Any2PDF",
            "install_ref": "uuid-xxx"
        }
    ]
}
```

#### 安装流程

```
接收方安装 App 时：
  
  对每个 dependency:
    1. 检查本地已安装 → entry.ID == dep.SkillID
       ├── 已安装且版本满足 → 跳过
       └── 已安装但版本不满足 → 触发升级
    
    2. 本地未安装 → 从 Hub/能力市场下载
       ├── 有 skill_id → GET /api/v1/skills/by-skill-id/{skill_id}/download?constraint=...
       │   ├── Hub 有 → 下载安装
       │   └── Hub 没有 → 尝试 HubCenter（跨 Hub 查找）
       │
       └── 无 skill_id（旧 App 包兼容）→ 按 install_ref(UUID) 下载
    
    3. 全部下载源都找不到 → 报错
       ├── required=true → App 安装失败
       └── required=false → 标记为缺失，App 降级运行
```

#### HubClient 新增方法

```go
// corelib/skill/hub_search.go

// DownloadBySkillID downloads a skill by its stable skill_id from the Hub.
// Supports optional version constraint (semver range).
func (c *HubClient) DownloadBySkillID(ctx context.Context, hubURL, skillID, versionConstraint string) (*corelib.NLSkillEntry, error) {
    endpoint := fmt.Sprintf("%s/api/v1/skills/by-skill-id/%s/download",
        hubURL, url.PathEscape(skillID))
    if versionConstraint != "" {
        endpoint += "?constraint=" + url.QueryEscape(versionConstraint)
    }
    
    var full skillHubDownloadResponse
    if err := c.getJSON(ctx, endpoint, &full); err != nil {
        return nil, fmt.Errorf("download skill %s: %w", skillID, err)
    }
    
    entry := &corelib.NLSkillEntry{
        ID:          skillID,
        Name:        full.Name,
        Description: full.Description,
        Triggers:    full.Triggers,
        Version:     full.SemVer,
        Status:      "active",
        Source:      "hub",
        HubSkillID:  full.ID,         // 内部 UUID
        HubVersion:  full.Version,     // 内部版本号
        TrustLevel:  full.TrustLevel,
    }
    if pub, _, ok := ParseSkillID(skillID); ok {
        entry.Publisher = pub
    }
    return entry, nil
}
```

#### App 打包时 enrich 改造

```go
// gui/app_maclaw_apps.go — enrichDependenciesWithSkillID

func (a *App) enrichDependenciesWithSkillID(deps []maclawAppInstallPlanDependency) {
    installed := a.skillExecutor.loadSkills()
    for i := range deps {
        dep := &deps[i]
        match := findSkillForDependency(dep, installed)
        if match == nil {
            continue
        }
        // 优先填充 skill_id（确定性标识）
        if match.ID != "" {
            dep.SkillID = match.ID
        }
        // 保留 install_ref 作为兜底（旧客户端兼容）
        if match.HubSkillID != "" && dep.InstallRef == "" {
            dep.InstallRef = match.HubSkillID
        }
        // 填充版本
        if match.Version != "" && dep.Version == "" {
            dep.Version = ">=" + match.Version
        }
    }
}
```

#### validateAppDependenciesPublished 改造

上传 App 到 Market 前验证：

```go
func (a *App) validateAppDependenciesPublished(deps []maclawAppInstallPlanDependency) []string {
    var unpublished []string
    for _, dep := range deps {
        if dep.SkillID != "" {
            // 新方式：检查 skill_id 在 Hub 上可解析
            // （本地已安装 + 有 skill_id + Hub 有该 id = 接收方可下载）
            continue
        }
        // 旧方式兼容：检查 HubSkillID(UUID) 非空
        if dep.InstallRef != "" {
            continue
        }
        unpublished = append(unpublished, dep.ID)
    }
    return unpublished
}
```

### 10.8 企业 Hub 的迁移差异

企业 Hub 的归属粒度是 `tenant_id`（而非个人 user_id）。迁移时：
- `ownershipStore.RegisterIfAbsent(skillID, tenantID, uploaderEmail)`
- publisher 前缀可配置强制使用企业标识（如 `enterprise.xxx`）

## 11. 迁移计划

### Phase 1：基础支持（非破坏性，可立即实施）

1. `skill.yaml` schema 新增 `id` 字段（可选）
2. `NLSkillEntry` 新增 `ID` 字段
3. `QualifiedID()` 优先返回 `ID` 字段
4. `MatchesName()` 优先按 ID 匹配
5. 本地 scan 时检测 ID 冲突并警告
6. 上传 preflight 推荐（非强制）声明 id

**向后兼容保证**：无 `id` 的旧 skill 行为完全不变。

### Phase 2：Hub 强制（上传侧变更）

1. Hub/SkillMarket 上传 API 要求 id + version
2. Hub 实施 id 唯一性检查 + 账户绑定
3. Hub 搜索结果返回 id 字段
4. 下载 API 支持按 id + version constraint 查询
5. 上传 preflight 变为强制检查

### Phase 3：依赖解析（客户端增强）

1. Pipeline 依赖声明支持 `id` + `version` 约束
2. App 依赖声明支持 `id` + `version` 约束
3. 安装时自动解析依赖链
4. 版本冲突检测与报告

### Phase 4：签名验证（安全增强）

1. 上传时生成 package manifest + 签名
2. 安装时验证包完整性
3. 可选的签名验证模式
4. 企业模式：只运行签名验证通过的 skill

## 12. 示例：完整的 skill.yaml

```yaml
# ──── 标识 ────
id: lovstudio.any2pdf
version: "1.3.0"
name: "Any2PDF 万能文档转换"

# ──── 描述 ────
description: "将 Word、Markdown、HTML、图片等格式转换为 PDF"
triggers: [pdf, 转换, convert, word转pdf, md转pdf]

# ──── 平台 ────
platforms: [windows, linux, macos]

# ──── 依赖 ────
requires:
  python: ["markitdown>=0.1.0", "weasyprint>=60.0"]
  bins: [python3]

# ──── 参数 ────
params:
  - name: input
    description: "输入文件路径"
    required: true
    aliases: [file, source, 输入]
  - name: output
    description: "输出 PDF 路径（可选，默认同名 .pdf）"
    aliases: [out, dest, 输出]

# ──── 步骤 ────
steps:
  - action: bash
    params:
      command: "python3 {baseDir}/scripts/convert.py {{input}} --output {{output}}"
    capture:
      output_path: 'Output:\s*(.+\.pdf)'
```

## 13. 示例：App 依赖声明

```yaml
# maclaw_app.yaml
app_id: enterprise.contract-review
version: "2.0.0"
name: "合同审查助手"

dependencies:
  - id: lovstudio.any2pdf
    version: ">=1.2.0"
    required: true
    purpose: "将审查意见导出为 PDF"
    
  - id: rapidai.ocr-extract
    version: "^3.0.0"
    required: true
    purpose: "从扫描件中提取文本"
    
  - id: community.legal-dictionary
    version: "*"
    required: false
    purpose: "法律术语参考（缺失时降级为通用术语）"
```

## 14. 与 Android applicationId 的最终对比

| 能力 | Android | MacLaw Skill（设计后） |
|------|---------|---------------------|
| 全局唯一 ID | `com.google.maps` | `lovstudio.any2pdf` ✅ |
| 不可变 | Play Store 强制 | Hub 首次上传后绑定 ✅ |
| 格式 | 反向域名 | `publisher.skill-name` ✅ |
| 版本管理 | versionCode + versionName | semver (`1.3.0`) ✅ |
| 签名校验 | APK signing | Phase 4 package manifest ✅ |
| 依赖声明 | implementation 'lib:1.2.0' | `id: x.y, version: ">=1.2.0"` ✅ |
| 冲突检测 | 安装时拒绝 | scan 时警告，安装时按 id 区分 ✅ |
| 向后兼容 | N/A | 无 id 的旧 skill 按 name 匹配 ✅ |
| 本地开发 | 无需网络 | id 本地声明即生效 ✅ |
