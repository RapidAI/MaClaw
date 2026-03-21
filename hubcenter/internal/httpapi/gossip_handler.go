package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// ── Public API (anonymous — machine_id from body) ──────────────────────

type gossipPublishRequest struct {
	MachineID string `json:"machine_id"`
	UserEmail string `json:"user_email"` // optional
	Content   string `json:"content"`
	Category  string `json:"category"` // "owner" | "project" | "news"
}

func generateNickname(machineID string) string {
	h := sha256.Sum256([]byte(machineID + "gossip-salt"))
	return "MaClaw-" + hex.EncodeToString(h[:])[:6]
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

func GossipPublishHandler(gossip store.GossipRepository, cache *GossipCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req gossipPublishRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON")
			return
		}
		machineID := strings.TrimSpace(req.MachineID)
		if machineID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "machine_id is required")
			return
		}
		content := strings.TrimSpace(req.Content)
		if content == "" || utf8.RuneCountInString(content) > 2000 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Content must be 1-2000 characters")
			return
		}
		category := req.Category
		if category == "" {
			category = "owner"
		}
		if category != "owner" && category != "project" && category != "news" && category != "gossip" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Category must be owner, project, news, or gossip")
			return
		}
		post := &store.GossipPost{
			ID:        generateID(),
			MachineID: machineID,
			UserEmail: strings.TrimSpace(req.UserEmail),
			Nickname:  generateNickname(machineID),
			Content:   content,
			Category:  category,
			CreatedAt: time.Now().UTC(),
		}
		if err := gossip.CreatePost(r.Context(), post); err != nil {
			writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
			return
		}
		go cache.Refresh(context.Background())
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"post": map[string]any{
				"id":         post.ID,
				"nickname":   post.Nickname,
				"content":    post.Content,
				"category":   post.Category,
				"created_at": post.CreatedAt.Format(time.RFC3339),
			},
		})
	}
}

// gossipPostToPublicMap converts a GossipPost to a public-facing map (no machine_id/user_email).
func gossipPostToPublicMap(p *store.GossipPost) map[string]any {
	return map[string]any{
		"id":         p.ID,
		"nickname":   p.Nickname,
		"content":    p.Content,
		"category":   p.Category,
		"score":      p.Score,
		"votes":      p.Votes,
		"locked":     p.Locked,
		"created_at": p.CreatedAt.Format(time.RFC3339),
	}
}

// gossipPostToAdminMap converts a GossipPost to an admin map (includes machine_id/user_email).
func gossipPostToAdminMap(p *store.GossipPost) map[string]any {
	m := gossipPostToPublicMap(p)
	m["machine_id"] = p.MachineID
	m["user_email"] = p.UserEmail
	return m
}

func GossipBrowseHandler(gossip store.GossipRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 100
		offset := (page - 1) * limit
		posts, total, err := gossip.ListPosts(r.Context(), offset, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		items := make([]map[string]any, 0, len(posts))
		for _, p := range posts {
			items = append(items, gossipPostToPublicMap(p))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "posts": items, "total": total, "page": page,
		})
	}
}

type gossipCommentRequest struct {
	MachineID string `json:"machine_id"`
	UserEmail string `json:"user_email"` // optional
	PostID    string `json:"post_id"`
	Content   string `json:"content"`
	Rating    int    `json:"rating"` // 0 = comment only, 1-5 = rating
}

func GossipCommentHandler(gossip store.GossipRepository, cache *GossipCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req gossipCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON")
			return
		}
		machineID := strings.TrimSpace(req.MachineID)
		if machineID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "machine_id is required")
			return
		}
		if strings.TrimSpace(req.PostID) == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "post_id required")
			return
		}
		postID := strings.TrimSpace(req.PostID)
		content := strings.TrimSpace(req.Content)
		if content == "" || utf8.RuneCountInString(content) > 1000 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Content must be 1-1000 characters")
			return
		}
		if req.Rating < 0 || req.Rating > 5 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Rating must be 0-5")
			return
		}
		post, err := gossip.GetPost(r.Context(), postID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Post not found")
			return
		}
		if post.Locked {
			writeError(w, http.StatusForbidden, "LOCKED", "This post is locked for comments/ratings")
			return
		}
		// Prevent duplicate ratings from the same machine
		if req.Rating > 0 {
			if rated, err := gossip.HasRated(r.Context(), postID, machineID); err == nil && rated {
				writeError(w, http.StatusConflict, "ALREADY_RATED", "You have already rated this post")
				return
			}
		}
		comment := &store.GossipComment{
			ID:        generateID(),
			PostID:    postID,
			MachineID: machineID,
			UserEmail: strings.TrimSpace(req.UserEmail),
			Nickname:  generateNickname(machineID),
			Content:   content,
			Rating:    req.Rating,
			CreatedAt: time.Now().UTC(),
		}
		if err := gossip.CreateComment(r.Context(), comment); err != nil {
			writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
			return
		}
		if req.Rating > 0 {
			_ = gossip.UpdatePostScore(r.Context(), postID)
			go cache.Refresh(context.Background())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true,
			"comment": map[string]any{
				"id":         comment.ID,
				"nickname":   comment.Nickname,
				"content":    comment.Content,
				"rating":     comment.Rating,
				"created_at": comment.CreatedAt.Format(time.RFC3339),
			},
		})
	}
}

func GossipRateHandler(gossip store.GossipRepository, cache *GossipCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MachineID string `json:"machine_id"`
			UserEmail string `json:"user_email"`
			PostID    string `json:"post_id"`
			Rating    int    `json:"rating"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON")
			return
		}
		machineID := strings.TrimSpace(req.MachineID)
		if machineID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "machine_id is required")
			return
		}
		if strings.TrimSpace(req.PostID) == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "post_id required")
			return
		}
		postID := strings.TrimSpace(req.PostID)
		if req.Rating < 1 || req.Rating > 5 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Rating must be 1-5")
			return
		}
		post, err := gossip.GetPost(r.Context(), postID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Post not found")
			return
		}
		if post.Locked {
			writeError(w, http.StatusForbidden, "LOCKED", "This post is locked for ratings")
			return
		}
		// Prevent duplicate ratings from the same machine
		if rated, err := gossip.HasRated(r.Context(), postID, machineID); err == nil && rated {
			writeError(w, http.StatusConflict, "ALREADY_RATED", "You have already rated this post")
			return
		}
		comment := &store.GossipComment{
			ID:        generateID(),
			PostID:    postID,
			MachineID: machineID,
			UserEmail: strings.TrimSpace(req.UserEmail),
			Nickname:  generateNickname(machineID),
			Content:   "Rated " + strconv.Itoa(req.Rating) + "/5",
			Rating:    req.Rating,
			CreatedAt: time.Now().UTC(),
		}
		if err := gossip.CreateComment(r.Context(), comment); err != nil {
			writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
			return
		}
		_ = gossip.UpdatePostScore(r.Context(), postID)
		go cache.Refresh(context.Background())
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func GossipCommentsListHandler(gossip store.GossipRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		postID := r.URL.Query().Get("post_id")
		if postID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "post_id required")
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 100
		offset := (page - 1) * limit
		comments, total, err := gossip.ListComments(r.Context(), postID, offset, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		items := make([]map[string]any, 0, len(comments))
		for _, c := range comments {
			items = append(items, map[string]any{
				"id":         c.ID,
				"nickname":   c.Nickname,
				"content":    c.Content,
				"rating":     c.Rating,
				"created_at": c.CreatedAt.Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "comments": items, "total": total, "page": page,
		})
	}
}

// ── Admin API ──────────────────────────────────────────────────────────

func AdminListGossipHandler(gossip store.GossipRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 100
		offset := (page - 1) * limit
		posts, total, err := gossip.ListPosts(r.Context(), offset, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		items := make([]map[string]any, 0, len(posts))
		for _, p := range posts {
			items = append(items, gossipPostToAdminMap(p))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "posts": items, "total": total, "page": page,
		})
	}
}

func AdminDeleteGossipHandler(gossip store.GossipRepository, cache *GossipCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id required")
			return
		}
		if err := gossip.DeletePost(r.Context(), req.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		go cache.Refresh(context.Background())
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminLockGossipHandler(gossip store.GossipRepository, cache *GossipCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string `json:"id"`
			Locked bool   `json:"locked"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id required")
			return
		}
		if err := gossip.LockPost(r.Context(), req.ID, req.Locked); err != nil {
			writeError(w, http.StatusInternalServerError, "LOCK_FAILED", err.Error())
			return
		}
		go cache.Refresh(context.Background())
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func AdminListGossipCommentsHandler(gossip store.GossipRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		postID := r.URL.Query().Get("post_id")
		if postID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "post_id required")
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		limit := 100
		offset := (page - 1) * limit
		comments, total, err := gossip.ListComments(r.Context(), postID, offset, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		items := make([]map[string]any, 0, len(comments))
		for _, c := range comments {
			items = append(items, map[string]any{
				"id":         c.ID,
				"post_id":    c.PostID,
				"machine_id": c.MachineID,
				"user_email": c.UserEmail,
				"nickname":   c.Nickname,
				"content":    c.Content,
				"rating":     c.Rating,
				"created_at": c.CreatedAt.Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "comments": items, "total": total, "page": page,
		})
	}
}

func AdminDeleteGossipCommentHandler(gossip store.GossipRepository, cache *GossipCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string `json:"id"`
			PostID string `json:"post_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "id required")
			return
		}
		if err := gossip.DeleteComment(r.Context(), req.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		if req.PostID != "" {
			_ = gossip.UpdatePostScore(r.Context(), req.PostID)
			go cache.Refresh(context.Background())
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
