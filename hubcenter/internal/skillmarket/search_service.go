package skillmarket

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

// SearchResult 是搜索结果条目。
type SearchResult struct {
	ID                           string                       `json:"id"`
	Name                         string                       `json:"name"`
	Description                  string                       `json:"description"`
	Tags                         []string                     `json:"tags"`
	Score                        float64                      `json:"score"`
	Price                        int64                        `json:"price"`
	Status                       string                       `json:"status"`
	AvgRating                    float64                      `json:"avg_rating"`
	DownloadCount                int                          `json:"download_count"`
	Version                      string                       `json:"version,omitempty"`
	Author                       string                       `json:"author,omitempty"`
	CreatedAt                    string                       `json:"created_at,omitempty"`
	ProductKind                  string                       `json:"product_kind,omitempty"`
	IsMaclawApp                  bool                         `json:"is_maclaw_app,omitempty"`
	MaclawAppEntry               string                       `json:"maclaw_app_entry,omitempty"`
	MaclawAppID                  string                       `json:"maclaw_app_id,omitempty"`
	MaclawAppName                string                       `json:"maclaw_app_name,omitempty"`
	MaclawAppDescription         string                       `json:"maclaw_app_description,omitempty"`
	MaclawAppCategory            string                       `json:"maclaw_app_category,omitempty"`
	MaclawAppIcon                string                       `json:"maclaw_app_icon,omitempty"`
	MaclawAppInputMode           string                       `json:"maclaw_app_input_mode,omitempty"`
	MaclawAppOutputModes         []string                     `json:"maclaw_app_output_modes,omitempty"`
	MaclawAppDefinitionSHA256    string                       `json:"maclaw_app_definition_sha256,omitempty"`
	MaclawAppTestEvidence        *skill.MaclawAppTestEvidence `json:"maclaw_app_test_evidence,omitempty"`
	ArtifactContractRequired     bool                         `json:"artifact_contract_required,omitempty"`
	ArtifactContractOutputModes  []string                     `json:"artifact_contract_output_modes,omitempty"`
	ArtifactContractPresentation string                       `json:"artifact_contract_presentation,omitempty"`
	Permissions                  []string                     `json:"permissions,omitempty"`
	RequiredEnv                  []string                     `json:"required_env,omitempty"`
	RequiresGUI                  bool                         `json:"requires_gui,omitempty"`
	SecurityLabels               []string                     `json:"security_labels,omitempty"`
	MaclawAppManifestPreview     map[string]any               `json:"maclaw_app_manifest_preview,omitempty"`
}

// SearchService 提供 FTS5 全文搜索。
type SearchService struct {
	store      *Store
	skillStore *skill.SkillStore
}

// NewSearchService 创建 SearchService 并确保 FTS5 表存在。
func NewSearchService(store *Store, skillStore *skill.SkillStore) (*SearchService, error) {
	s := &SearchService{store: store, skillStore: skillStore}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SearchService) migrate() error {
	stmts := []string{
		// FTS5 虚拟表
		`CREATE VIRTUAL TABLE IF NOT EXISTS sm_skill_fts USING fts5(
			skill_id UNINDEXED,
			name,
			description,
			tags
		);`,
		// 索引元数据表（存储排序所需的数值字段）
		`CREATE TABLE IF NOT EXISTS sm_skill_index (
			skill_id       TEXT PRIMARY KEY,
			name           TEXT NOT NULL DEFAULT '',
			description    TEXT NOT NULL DEFAULT '',
			tags           TEXT NOT NULL DEFAULT '',
			avg_rating     REAL NOT NULL DEFAULT 0,
			downloads      INTEGER NOT NULL DEFAULT 0,
			price          INTEGER NOT NULL DEFAULT 0,
			status         TEXT NOT NULL DEFAULT '',
			created_at     TEXT NOT NULL DEFAULT '',
			updated_at     TEXT NOT NULL DEFAULT ''
		);`,
	}
	for _, stmt := range stmts {
		if _, err := s.store.db.Exec(stmt); err != nil {
			return fmt.Errorf("search migrate %q: %w", stmt[:min(len(stmt), 60)], err)
		}
	}
	// 增量迁移：为已有表添加新列（ALTER TABLE ADD COLUMN 在列已存在时会报错，忽略即可）
	for _, alter := range []string{
		`ALTER TABLE sm_skill_index ADD COLUMN version TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sm_skill_index ADD COLUMN author TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sm_skill_index ADD COLUMN product_kind TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sm_skill_index ADD COLUMN is_maclaw_app INTEGER NOT NULL DEFAULT 0`,
	} {
		_, _ = s.store.db.Exec(alter) // 列已存在时静默忽略
	}
	return nil
}

type skillSearchIndexProductOptions struct {
	ProductKind string
	IsMaclawApp bool
}

// IndexSkill 将 Skill 索引到 FTS5（发布/更新时调用）。
func (s *SearchService) IndexSkill(ctx context.Context, id, name, description string, tags []string, avgRating float64, downloads int, price int64, status, createdAt, version, author string) error {
	return s.IndexSkillWithProduct(ctx, id, name, description, tags, avgRating, downloads, price, status, createdAt, version, author, skillSearchIndexProductOptions{})
}

func (s *SearchService) IndexSkillWithProduct(ctx context.Context, id, name, description string, tags []string, avgRating float64, downloads int, price int64, status, createdAt, version, author string, product skillSearchIndexProductOptions) error {
	tagsStr := strings.Join(tags, " ")
	productKind := strings.TrimSpace(product.ProductKind)
	isMaclawApp := product.IsMaclawApp || strings.EqualFold(productKind, "maclaw_app_skill")
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Upsert 索引元数据
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sm_skill_index (skill_id, name, description, tags, avg_rating, downloads, price, status, created_at, updated_at, version, author, product_kind, is_maclaw_app)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), ?, ?, ?, ?)
		ON CONFLICT(skill_id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			tags = excluded.tags,
			avg_rating = excluded.avg_rating,
			downloads = excluded.downloads,
			price = excluded.price,
			status = excluded.status,
			version = excluded.version,
			author = excluded.author,
			product_kind = excluded.product_kind,
			is_maclaw_app = excluded.is_maclaw_app,
			updated_at = datetime('now')`,
		id, name, description, tagsStr, avgRating, downloads, price, status, createdAt, version, author, productKind, boolToSearchIndexInt(isMaclawApp))
	if err != nil {
		return err
	}

	// 删除旧 FTS 记录再插入新的
	_, _ = tx.ExecContext(ctx, `DELETE FROM sm_skill_fts WHERE skill_id = ?`, id)
	_, err = tx.ExecContext(ctx, `INSERT INTO sm_skill_fts (skill_id, name, description, tags) VALUES (?, ?, ?, ?)`,
		id, name, description, tagsStr)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func boolToSearchIndexInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// RemoveSkill 从索引中移除 Skill。
func (s *SearchService) RemoveSkill(ctx context.Context, id string) error {
	_, _ = s.store.db.ExecContext(ctx, `DELETE FROM sm_skill_fts WHERE skill_id = ?`, id)
	_, err := s.store.db.ExecContext(ctx, `DELETE FROM sm_skill_index WHERE skill_id = ?`, id)
	return err
}

// ReIndexSkill 从 SkillStore 重新读取 Skill 元数据并更新搜索索引。
// 用于 Skill 重新上架（SetVisibility(true) / AdminApprove）后恢复搜索可见性。
func (s *SearchService) ReIndexSkill(ctx context.Context, id string) error {
	if s.skillStore == nil {
		return nil
	}
	meta := s.skillStore.GetByID(id)
	if meta == nil || !meta.Visible {
		return nil
	}
	return s.IndexSkillWithProduct(ctx, meta.ID, meta.Name, meta.Description, meta.Tags,
		meta.AvgRating, meta.Downloads, int64(meta.Price), meta.Status, meta.CreatedAt,
		meta.Version, meta.Author, skillSearchIndexProductOptions{ProductKind: meta.ProductKind, IsMaclawApp: meta.IsMaclawApp})
}

// sanitizeFTS5Query 将用户输入转换为安全的 FTS5 前缀查询。
// 例如 "pyth form" → "pyth* form*"，支持部分匹配。
func sanitizeFTS5Query(raw string) string {
	var cleaned strings.Builder
	for _, r := range raw {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			cleaned.WriteRune(r)
		case r >= 0x4e00 && r <= 0x9fff: // CJK Unified Ideographs
			cleaned.WriteRune(r)
		default:
			cleaned.WriteRune(' ')
		}
	}
	words := strings.Fields(cleaned.String())
	if len(words) == 0 {
		return ""
	}
	for i, w := range words {
		words[i] = w + "*"
	}
	return strings.Join(words, " ")
}

// escapeLIKE 转义 SQL LIKE 通配符，防止用户输入中的 % 和 _ 被当作通配符。
func escapeLIKE(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

// Search 执行全文搜索，返回按质量排序的结果。
// 排序公式: score = fts_rank * -0.5 + avg_rating * 0.2 + log(downloads+1) * 0.2 + recency * 0.1
func (s *SearchService) Search(ctx context.Context, query string, tags []string, topN int) ([]SearchResult, error) {
	if topN <= 0 || topN > 100 {
		topN = 20
	}

	trimmedQuery := strings.TrimSpace(query)

	var rows *sql.Rows
	var err error
	var ftsQueryUsed bool

	if trimmedQuery == "" && len(tags) == 0 {
		// 无搜索词：按 downloads 降序
		rows, err = s.store.readDB.QueryContext(ctx, `
			SELECT skill_id, name, description, tags, avg_rating, downloads, price, status, version, author, created_at, product_kind, is_maclaw_app
			FROM sm_skill_index
			WHERE status IN ('trial', 'published')
			ORDER BY downloads DESC
			LIMIT ?`, topN)
	} else if trimmedQuery == "" {
		// 仅 tags 过滤
		tagClauses := make([]string, len(tags))
		args := make([]any, len(tags))
		for i, t := range tags {
			tagClauses[i] = "tags LIKE ? ESCAPE '\\'"
			args[i] = "%" + escapeLIKE(t) + "%"
		}
		args = append(args, topN)
		rows, err = s.store.readDB.QueryContext(ctx, `
			SELECT skill_id, name, description, tags, avg_rating, downloads, price, status, version, author, created_at, product_kind, is_maclaw_app
			FROM sm_skill_index
			WHERE status IN ('trial', 'published') AND `+strings.Join(tagClauses, " AND ")+`
			ORDER BY downloads DESC
			LIMIT ?`, args...)
	} else {
		ftsQuery := sanitizeFTS5Query(trimmedQuery)
		escapedLike := "%" + escapeLIKE(trimmedQuery) + "%"

		if ftsQuery == "" {
			// 输入全是特殊字符，回退到 LIKE 模糊搜索
			baseQuery := `
				SELECT skill_id, name, description, tags, avg_rating, downloads, price, status, version, author, created_at, product_kind, is_maclaw_app
				FROM sm_skill_index
				WHERE status IN ('trial', 'published')
				  AND (name LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\' OR tags LIKE ? ESCAPE '\')`
			args := []any{escapedLike, escapedLike, escapedLike}
			if len(tags) > 0 {
				for _, t := range tags {
					baseQuery += " AND tags LIKE ? ESCAPE '\\'"
					args = append(args, "%"+escapeLIKE(t)+"%")
				}
			}
			baseQuery += " ORDER BY downloads DESC LIMIT ?"
			args = append(args, topN)
			rows, err = s.store.readDB.QueryContext(ctx, baseQuery, args...)
		} else {
			// FTS5 前缀搜索 + LIKE 兜底（UNION 去重）
			ftsQueryUsed = true
			baseQuery := `
				SELECT skill_id, name, description, tags, avg_rating, downloads, price, status, version, author, created_at, product_kind, is_maclaw_app, rank
				FROM (
					SELECT i.skill_id, i.name, i.description, i.tags, i.avg_rating, i.downloads, i.price, i.status, i.version, i.author, i.created_at, i.product_kind, i.is_maclaw_app, f.rank
					FROM sm_skill_fts f
					JOIN sm_skill_index i ON i.skill_id = f.skill_id
					WHERE sm_skill_fts MATCH ? AND i.status IN ('trial', 'published')
				  UNION
					SELECT skill_id, name, description, tags, avg_rating, downloads, price, status, version, author, created_at, product_kind, is_maclaw_app, 0 as rank
					FROM sm_skill_index
					WHERE status IN ('trial', 'published')
					  AND (name LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\' OR tags LIKE ? ESCAPE '\')
					  AND skill_id NOT IN (
						SELECT f2.skill_id FROM sm_skill_fts f2 WHERE sm_skill_fts MATCH ?
					  )
				)`
			args := []any{ftsQuery, escapedLike, escapedLike, escapedLike, ftsQuery}

			if len(tags) > 0 {
				for _, t := range tags {
					baseQuery += " WHERE tags LIKE ? ESCAPE '\\'"
					args = append(args, "%"+escapeLIKE(t)+"%")
				}
			}
			baseQuery += " ORDER BY rank LIMIT ?"
			args = append(args, topN)
			rows, err = s.store.readDB.QueryContext(ctx, baseQuery, args...)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	// hasRankCol is true only for the FTS5 UNION query path which appends a rank column.
	// The LIKE-only fallback (ftsQuery == "") does NOT have a rank column even when
	// trimmedQuery is non-empty, so we cannot simply use trimmedQuery != "".
	hasRankCol := trimmedQuery != "" && ftsQueryUsed

	for rows.Next() {
		var r SearchResult
		var tagsStr string
		var ftsRank float64
		var isMaclawApp int

		if hasRankCol {
			if err := rows.Scan(&r.ID, &r.Name, &r.Description, &tagsStr, &r.AvgRating, &r.DownloadCount, &r.Price, &r.Status, &r.Version, &r.Author, &r.CreatedAt, &r.ProductKind, &isMaclawApp, &ftsRank); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&r.ID, &r.Name, &r.Description, &tagsStr, &r.AvgRating, &r.DownloadCount, &r.Price, &r.Status, &r.Version, &r.Author, &r.CreatedAt, &r.ProductKind, &isMaclawApp); err != nil {
				return nil, err
			}
		}
		r.IsMaclawApp = isMaclawApp != 0 || strings.EqualFold(strings.TrimSpace(r.ProductKind), "maclaw_app_skill")
		s.enrichSearchResultWithSkillMeta(&r)

		if tagsStr != "" {
			r.Tags = strings.Fields(tagsStr)
		}
		r.Score = ftsRank*-0.5 + r.AvgRating*0.2 + math.Log(float64(r.DownloadCount)+1)*0.2
		results = append(results, r)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// ReviewQueue 返回后台审核队列，包含试用和待审核状态，不走公开市场过滤。
func (s *SearchService) ReviewQueue(ctx context.Context, topN int) ([]SearchResult, error) {
	if topN <= 0 || topN > 1000 {
		topN = 100
	}
	rows, err := s.store.readDB.QueryContext(ctx, `
  SELECT skill_id, name, description, tags, avg_rating, downloads, price, status, version, author, created_at, product_kind, is_maclaw_app
  FROM sm_skill_index
  WHERE status IN ('pending_review', 'trial')
  ORDER BY created_at DESC
  LIMIT ?`, topN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]SearchResult, 0)
	for rows.Next() {
		var r SearchResult
		var tagsStr string
		var isMaclawApp int
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &tagsStr, &r.AvgRating, &r.DownloadCount, &r.Price, &r.Status, &r.Version, &r.Author, &r.CreatedAt, &r.ProductKind, &isMaclawApp); err != nil {
			return nil, err
		}
		r.IsMaclawApp = isMaclawApp != 0 || strings.EqualFold(strings.TrimSpace(r.ProductKind), "maclaw_app_skill")
		s.enrichSearchResultWithSkillMeta(&r)
		if tagsStr != "" {
			r.Tags = strings.Fields(tagsStr)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *SearchService) enrichSearchResultWithSkillMeta(r *SearchResult) {
	if s == nil || s.skillStore == nil || r == nil {
		return
	}
	meta := s.skillStore.GetByID(r.ID)
	if meta == nil {
		return
	}
	if r.ProductKind == "" {
		r.ProductKind = meta.ProductKind
	}
	r.IsMaclawApp = r.IsMaclawApp || meta.IsMaclawApp || strings.EqualFold(strings.TrimSpace(r.ProductKind), "maclaw_app_skill")
	r.MaclawAppEntry = meta.MaclawAppEntry
	r.MaclawAppID = meta.MaclawAppID
	r.MaclawAppName = meta.MaclawAppName
	r.MaclawAppDescription = meta.MaclawAppDescription
	r.MaclawAppCategory = meta.MaclawAppCategory
	r.MaclawAppIcon = meta.MaclawAppIcon
	r.MaclawAppInputMode = meta.MaclawAppInputMode
	r.MaclawAppOutputModes = append([]string(nil), meta.MaclawAppOutputModes...)
	r.MaclawAppDefinitionSHA256 = meta.MaclawAppDefinitionSHA256
	r.MaclawAppTestEvidence = cloneHubMaclawAppTestEvidence(meta.MaclawAppTestEvidence)
	r.ArtifactContractRequired = meta.ArtifactContractRequired
	r.ArtifactContractOutputModes = append([]string(nil), meta.ArtifactContractOutputModes...)
	r.ArtifactContractPresentation = meta.ArtifactContractPresentation
	r.Permissions = append([]string(nil), meta.Permissions...)
	r.RequiredEnv = append([]string(nil), meta.RequiredEnv...)
	r.RequiresGUI = meta.RequiresGUI
	r.SecurityLabels = append([]string(nil), meta.SecurityLabels...)
	r.MaclawAppManifestPreview = s.maclawAppManifestPreview(r.ID, r.MaclawAppEntry)
}

func (s *SearchService) maclawAppManifestPreview(skillID, entry string) map[string]any {
	if s == nil || s.skillStore == nil || strings.TrimSpace(skillID) == "" {
		return nil
	}
	full, err := s.skillStore.Get(skillID)
	if err != nil || full == nil || len(full.Files) == 0 {
		return nil
	}
	entry = strings.TrimSpace(entry)
	if entry == "" {
		entry = "maclaw.app.json"
	}
	encoded := full.Files[entry]
	if encoded == "" {
		encoded = full.Files["maclaw.app.json"]
	}
	if encoded == "" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > 256*1024 {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return nil
	}
	return sanitizeMaclawAppManifestPreview(raw).(map[string]any)
}

func sanitizeMaclawAppManifestPreview(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if isMaclawAppManifestPreviewSensitiveKey(key) {
				continue
			}
			out[key] = sanitizeMaclawAppManifestPreview(item)
		}
		return out
	case []any:
		if len(v) > 80 {
			v = v[:80]
		}
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, sanitizeMaclawAppManifestPreview(item))
		}
		return out
	case string:
		if len(v) > 2048 {
			return v[:2048] + "..."
		}
		return v
	default:
		return v
	}
}

func isMaclawAppManifestPreviewSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	return strings.Contains(k, "path") || strings.Contains(k, "secret") || strings.Contains(k, "token") || strings.Contains(k, "password")
}

func cloneHubMaclawAppTestEvidence(e *skill.MaclawAppTestEvidence) *skill.MaclawAppTestEvidence {
	if e == nil {
		return nil
	}
	copy := *e
	copy.ResultPayload = cloneSkillMarketAnyMap(e.ResultPayload)
	return &copy
}

// RebuildIndex 从 SkillStore 全量重建 FTS 索引。
func (s *SearchService) RebuildIndex(ctx context.Context) error {
	// 清空现有索引
	_, _ = s.store.db.ExecContext(ctx, `DELETE FROM sm_skill_fts`)
	_, _ = s.store.db.ExecContext(ctx, `DELETE FROM sm_skill_index`)

	// 从 SkillStore 获取所有 visible Skill
	result := s.skillStore.Search("", nil, 1)
	total := result.Total
	if total == 0 {
		return nil
	}

	// 分页遍历所有 Skill（pageSize=40 来自 skill.SkillStore）
	const skillPageSize = 40
	page := 1
	for {
		result = s.skillStore.Search("", nil, page)
		if len(result.Skills) == 0 {
			break
		}
		for _, m := range result.Skills {
			if err := s.IndexSkillWithProduct(ctx, m.ID, m.Name, m.Description, m.Tags, m.AvgRating, m.Downloads, 0, "published", m.CreatedAt, m.Version, m.Author, skillSearchIndexProductOptions{ProductKind: m.ProductKind, IsMaclawApp: m.IsMaclawApp}); err != nil {
				log.Printf("[skillmarket] rebuild index: skill %s error: %v", m.ID, err)
			}
		}
		if page*skillPageSize >= total {
			break
		}
		page++
	}
	return nil
}
