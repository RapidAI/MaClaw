package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func (r *newsRepo) Create(ctx context.Context, article *store.NewsArticle) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO news_articles (id, title, content, category, pinned, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		article.ID, article.Title, article.Content, article.Category,
		boolToInt(article.Pinned),
		article.CreatedAt.Format(time.RFC3339),
		article.UpdatedAt.Format(time.RFC3339))
	return err
}

func (r *newsRepo) Update(ctx context.Context, article *store.NewsArticle) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE news_articles SET title=?, content=?, category=?, pinned=?, updated_at=?
		WHERE id=?`,
		article.Title, article.Content, article.Category,
		boolToInt(article.Pinned),
		article.UpdatedAt.Format(time.RFC3339),
		article.ID)
	return err
}

func (r *newsRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM news_articles WHERE id=?`, id)
	return err
}

func (r *newsRepo) GetByID(ctx context.Context, id string) (*store.NewsArticle, error) {
	row := r.readDB.QueryRowContext(ctx, `
		SELECT id, title, content, category, pinned, created_at, updated_at
		FROM news_articles WHERE id=?`, id)
	return scanNewsArticle(row)
}

func (r *newsRepo) List(ctx context.Context, offset, limit int) ([]*store.NewsArticle, int, error) {
	var total int
	if err := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM news_articles`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, title, content, category, pinned, created_at, updated_at
		FROM news_articles ORDER BY pinned DESC, created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []*store.NewsArticle
	for rows.Next() {
		a, err := scanNewsArticleRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func (r *newsRepo) ListLatest(ctx context.Context, limit int) ([]*store.NewsArticle, error) {
	rows, err := r.readDB.QueryContext(ctx, `
		SELECT id, title, content, category, pinned, created_at, updated_at
		FROM news_articles ORDER BY pinned DESC, created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.NewsArticle
	for rows.Next() {
		a, err := scanNewsArticleRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanNewsArticle(row *sql.Row) (*store.NewsArticle, error) {
	var a store.NewsArticle
	var pinned int
	var createdAt, updatedAt string
	if err := row.Scan(&a.ID, &a.Title, &a.Content, &a.Category, &pinned, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	a.Pinned = pinned != 0
	a.CreatedAt = mustParseTime(createdAt)
	a.UpdatedAt = mustParseTime(updatedAt)
	return &a, nil
}

type newsScanner interface {
	Scan(dest ...any) error
}

func scanNewsArticleRow(row newsScanner) (*store.NewsArticle, error) {
	var a store.NewsArticle
	var pinned int
	var createdAt, updatedAt string
	if err := row.Scan(&a.ID, &a.Title, &a.Content, &a.Category, &pinned, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	a.Pinned = pinned != 0
	a.CreatedAt = mustParseTime(createdAt)
	a.UpdatedAt = mustParseTime(updatedAt)
	return &a, nil
}
