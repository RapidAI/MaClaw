package skillmarket

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

// SearchResult 是搜索结果条目。
type SearchResult struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Tags          []string `json:"tags"`
	Score         float64  `json:"score"`
	Price         int64    `json:"price"`
	Status        string   `json:"status"`
	AvgRating     float64  `json:"avg_rating"`
	DownloadCount int      `json:"download_count"`
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
	return nil
}

// IndexSkill 将 Skill 索引到 FTS5（发布/更新时调用）。
func (s *SearchService) IndexSkill(ctx context.Context, id, name, description string, tags []string, avgRating float64, downloads int, price int64, status, createdAt string) error {
	tagsStr := strings.Join(tags, " ")
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Upsert 索引元数据
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sm_skill_index (skill_id, name, description, tags, avg_rating, downloads, price, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(skill_id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			tags = excluded.tags,
			avg_rating = excluded.avg_rating,
			downloads = excluded.downloads,
			price = excluded.price,
			status = excluded.status,
			updated_at = datetime('now')`,
		id, name, description, tagsStr, avgRating, downloads, price, status, createdAt)
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

// RemoveSkill 从索引中移除 Skill。
func (s *SearchService) RemoveSkill(ctx context.Context, id string) error {
	_, _ = s.store.db.ExecContext(ctx, `DELETE FROM sm_skill_fts WHERE skill_id = ?`, id)
	_, err := s.store.db.ExecContext(ctx, `DELETE FROM sm_skill_index WHERE skill_id = ?`, id)
	return err
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

	if trimmedQuery == "" && len(tags) == 0 {
		// 无搜索词：按 downloads 降序
		rows, err = s.store.readDB.QueryContext(ctx, `
			SELECT skill_id, name, description, tags, avg_rating, downloads, price, status
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
			SELECT skill_id, name, description, tags, avg_rating, downloads, price, status
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
				SELECT skill_id, name, description, tags, avg_rating, downloads, price, status
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
			baseQuery := `
				SELECT skill_id, name, description, tags, avg_rating, downloads, price, status, rank
				FROM (
					SELECT i.skill_id, i.name, i.description, i.tags, i.avg_rating, i.downloads, i.price, i.status, f.rank
					FROM sm_skill_fts f
					JOIN sm_skill_index i ON i.skill_id = f.skill_id
					WHERE sm_skill_fts MATCH ? AND i.status IN ('trial', 'published')
				  UNION
					SELECT skill_id, name, description, tags, avg_rating, downloads, price, status, 0 as rank
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
	hasRankCol := trimmedQuery != ""

	for rows.Next() {
		var r SearchResult
		var tagsStr string
		var ftsRank float64

		if hasRankCol {
			if err := rows.Scan(&r.ID, &r.Name, &r.Description, &tagsStr, &r.AvgRating, &r.DownloadCount, &r.Price, &r.Status, &ftsRank); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&r.ID, &r.Name, &r.Description, &tagsStr, &r.AvgRating, &r.DownloadCount, &r.Price, &r.Status); err != nil {
				return nil, err
			}
		}

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
			if err := s.IndexSkill(ctx, m.ID, m.Name, m.Description, m.Tags, m.AvgRating, m.Downloads, 0, "published", m.CreatedAt); err != nil {
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
