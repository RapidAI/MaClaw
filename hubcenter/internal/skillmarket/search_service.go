package skillmarket

import (
	"context"
	"database/sql"
	"fmt"
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

// Search 执行全文搜索，返回按质量排序的结果。
// 排序公式: score = fts_rank * -0.5 + avg_rating * 0.2 + log(downloads+1) * 0.2 + recency * 0.1
func (s *SearchService) Search(ctx context.Context, query string, tags []string, topN int) ([]SearchResult, error) {
	if topN <= 0 || topN > 100 {
		topN = 20
	}

	var rows *sql.Rows
	var err error

	if strings.TrimSpace(query) == "" && len(tags) == 0 {
		// 无搜索词：按 downloads 降序
		rows, err = s.store.readDB.QueryContext(ctx, `
			SELECT skill_id, name, description, tags, avg_rating, downloads, price, status
			FROM sm_skill_index
			WHERE status IN ('trial', 'published')
			ORDER BY downloads DESC
			LIMIT ?`, topN)
	} else if strings.TrimSpace(query) == "" {
		// 仅 tags 过滤
		tagClauses := make([]string, len(tags))
		args := make([]any, len(tags))
		for i, t := range tags {
			tagClauses[i] = "tags LIKE ?"
			args[i] = "%" + t + "%"
		}
		args = append(args, topN)
		rows, err = s.store.readDB.QueryContext(ctx, `
			SELECT skill_id, name, description, tags, avg_rating, downloads, price, status
			FROM sm_skill_index
			WHERE status IN ('trial', 'published') AND `+strings.Join(tagClauses, " AND ")+`
			ORDER BY downloads DESC
			LIMIT ?`, args...)
	} else {
		// FTS5 搜索 + tags 过滤
		baseQuery := `
			SELECT i.skill_id, i.name, i.description, i.tags, i.avg_rating, i.downloads, i.price, i.status, f.rank
			FROM sm_skill_fts f
			JOIN sm_skill_index i ON i.skill_id = f.skill_id
			WHERE sm_skill_fts MATCH ? AND i.status IN ('trial', 'published')`
		args := []any{query}

		if len(tags) > 0 {
			for _, t := range tags {
				baseQuery += " AND i.tags LIKE ?"
				args = append(args, "%"+t+"%")
			}
		}
		baseQuery += " ORDER BY f.rank LIMIT ?"
		args = append(args, topN)
		rows, err = s.store.readDB.QueryContext(ctx, baseQuery, args...)
	}

	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	hasFTSRank := strings.TrimSpace(query) != ""

	for rows.Next() {
		var r SearchResult
		var tagsStr string
		var ftsRank float64

		if hasFTSRank {
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
		// 计算综合得分
		r.Score = ftsRank*-0.5 + r.AvgRating*0.2 + math.Log(float64(r.DownloadCount)+1)*0.2
		results = append(results, r)
	}

	// 按综合得分降序排序
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
			_ = s.IndexSkill(ctx, m.ID, m.Name, m.Description, m.Tags, m.AvgRating, m.Downloads, 0, "published", m.CreatedAt)
		}
		if page*skillPageSize >= total {
			break
		}
		page++
	}
	return nil
}
