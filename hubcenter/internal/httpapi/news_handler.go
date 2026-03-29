package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// ── Public API ──────────────────────────────────────────────────────────

// NewsLatestHandler returns the latest N news articles (default 2).
// GET /api/news?limit=2
func NewsLatestHandler(repo store.NewsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setCORSNews(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		limit := 2
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20 {
				limit = n
			}
		}
		articles, err := repo.ListLatest(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "NEWS_FETCH_FAILED", err.Error())
			return
		}
		type item struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Content   string `json:"content"`
			Category  string `json:"category"`
			Pinned    bool   `json:"pinned"`
			CreatedAt string `json:"created_at"`
		}
		out := make([]item, 0, len(articles))
		for _, a := range articles {
			out = append(out, item{
				ID:        a.ID,
				Title:     a.Title,
				Content:   a.Content,
				Category:  a.Category,
				Pinned:    a.Pinned,
				CreatedAt: a.CreatedAt.Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"articles": out})
	}
}

// ── Admin API ───────────────────────────────────────────────────────────

func AdminListNewsHandler(repo store.NewsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}
		articles, total, err := repo.List(r.Context(), offset, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "NEWS_LIST_FAILED", err.Error())
			return
		}
		type adminItem struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			Content   string `json:"content"`
			Category  string `json:"category"`
			Pinned    bool   `json:"pinned"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
		}
		out := make([]adminItem, 0, len(articles))
		for _, a := range articles {
			out = append(out, adminItem{
				ID:        a.ID,
				Title:     a.Title,
				Content:   a.Content,
				Category:  a.Category,
				Pinned:    a.Pinned,
				CreatedAt: a.CreatedAt.Format(time.RFC3339),
				UpdatedAt: a.UpdatedAt.Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"articles": out, "total": total})
	}
}

func AdminCreateNewsHandler(repo store.NewsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Title    string `json:"title"`
			Content  string `json:"content"`
			Category string `json:"category"`
			Pinned   bool   `json:"pinned"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON")
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "title is required")
			return
		}
		cat := strings.TrimSpace(req.Category)
		if cat == "" {
			cat = "notice"
		}
		validCats := map[string]bool{"notice": true, "update": true, "tip": true, "alert": true}
		if !validCats[cat] {
			cat = "notice"
		}
		now := time.Now().UTC()
		// Enforce max 2 pinned articles
		if req.Pinned {
			pinnedCount, err := repo.CountPinned(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "NEWS_CREATE_FAILED", err.Error())
				return
			}
			if pinnedCount >= 2 {
				writeError(w, http.StatusBadRequest, "MAX_PINNED", "Maximum 2 pinned articles allowed")
				return
			}
		}
		article := &store.NewsArticle{
			ID:        generateNewsID(),
			Title:     title,
			Content:   req.Content,
			Category:  cat,
			Pinned:    req.Pinned,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := repo.Create(r.Context(), article); err != nil {
			writeError(w, http.StatusInternalServerError, "NEWS_CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, article)
	}
}

func AdminUpdateNewsHandler(repo store.NewsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
			return
		}
		existing, err := repo.GetByID(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "NEWS_FETCH_FAILED", err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "article not found")
			return
		}
		var req struct {
			Title    *string `json:"title"`
			Content  *string `json:"content"`
			Category *string `json:"category"`
			Pinned   *bool   `json:"pinned"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON")
			return
		}
		if req.Title != nil {
			existing.Title = strings.TrimSpace(*req.Title)
		}
		if req.Content != nil {
			existing.Content = *req.Content
		}
		if req.Category != nil {
			existing.Category = strings.TrimSpace(*req.Category)
		}
		wasPinned := existing.Pinned
		if req.Pinned != nil {
			existing.Pinned = *req.Pinned
		}
		// Enforce max 2 pinned articles (only when newly pinning)
		if existing.Pinned && !wasPinned {
			pinnedCount, err := repo.CountPinned(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "NEWS_UPDATE_FAILED", err.Error())
				return
			}
			if pinnedCount >= 2 {
				writeError(w, http.StatusBadRequest, "MAX_PINNED", "Maximum 2 pinned articles allowed")
				return
			}
		}
		existing.UpdatedAt = time.Now().UTC()
		if err := repo.Update(r.Context(), existing); err != nil {
			writeError(w, http.StatusInternalServerError, "NEWS_UPDATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, existing)
	}
}

func AdminDeleteNewsHandler(repo store.NewsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id is required")
			return
		}
		if err := repo.Delete(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "NEWS_DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func generateNewsID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("news-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

func setCORSNews(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}
