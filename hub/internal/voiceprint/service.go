package voiceprint

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"sort"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// Config holds RapidSpeech server connection settings.
type Config struct {
	Enabled    bool   `json:"enabled"`
	ServerURL  string `json:"server_url"`  // e.g. "http://localhost:8080"
	Threshold  float64 `json:"threshold"`  // cosine similarity threshold, default 0.6
}

// MatchResult represents a voiceprint match candidate.
type MatchResult struct {
	UserID     string  `json:"user_id"`
	Email      string  `json:"email"`
	Label      string  `json:"label"`
	Similarity float64 `json:"similarity"`
}

type Service struct {
	repo   store.VoiceprintRepository
	system store.SystemSettingsRepository
	client *http.Client
}

func NewService(repo store.VoiceprintRepository, system store.SystemSettingsRepository) *Service {
	return &Service{
		repo:   repo,
		system: system,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

const configKey = "voiceprint_config"

func (s *Service) LoadConfig(ctx context.Context) Config {
	raw, err := s.system.Get(ctx, configKey)
	if err != nil || raw == "" {
		return Config{Threshold: 0.6}
	}
	var cfg Config
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		return Config{Threshold: 0.6}
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = 0.6
	}
	return cfg
}

func (s *Service) SaveConfig(ctx context.Context, cfg Config) error {
	if cfg.Threshold <= 0 {
		cfg.Threshold = 0.6
	}
	data, _ := json.Marshal(cfg)
	return s.system.Set(ctx, configKey, string(data))
}

// extractEmbedding sends WAV audio to RapidSpeech server and returns the embedding.
func (s *Service) extractEmbedding(ctx context.Context, cfg Config, wavData []byte) ([]float32, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("voiceprint server URL not configured")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(wavData); err != nil {
		return nil, err
	}
	w.Close()

	url := cfg.ServerURL + "/v1/speaker-embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("speaker-embed request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2MB max response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("speaker-embed HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Dim       int       `json:"dim"`
		Embedding []float64 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse embedding response: %w", err)
	}
	if result.Dim <= 0 || len(result.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	emb := make([]float32, len(result.Embedding))
	for i, v := range result.Embedding {
		emb[i] = float32(v)
	}
	return emb, nil
}

// Enroll extracts a voiceprint from WAV audio and stores it for the given user.
func (s *Service) Enroll(ctx context.Context, userID, email, label string, wavData []byte) (*store.Voiceprint, error) {
	cfg := s.LoadConfig(ctx)
	if !cfg.Enabled {
		return nil, fmt.Errorf("voiceprint feature is disabled")
	}

	emb, err := s.extractEmbedding(ctx, cfg, wavData)
	if err != nil {
		return nil, err
	}

	vp := &store.Voiceprint{
		ID:        generateID(),
		UserID:    userID,
		Email:     email,
		Label:     label,
		Embedding: emb,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.Create(ctx, vp); err != nil {
		return nil, err
	}
	log.Printf("[voiceprint] enrolled user=%s email=%s label=%s dim=%d", userID, email, label, len(emb))
	return vp, nil
}

// Identify performs 1:N comparison against all stored voiceprints.
// Returns matches above the configured threshold, sorted by similarity descending.
func (s *Service) Identify(ctx context.Context, wavData []byte) ([]MatchResult, error) {
	cfg := s.LoadConfig(ctx)
	if !cfg.Enabled {
		return nil, fmt.Errorf("voiceprint feature is disabled")
	}

	queryEmb, err := s.extractEmbedding(ctx, cfg, wavData)
	if err != nil {
		return nil, err
	}

	all, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	matches := make([]MatchResult, 0)
	for _, vp := range all {
		sim := cosineSimilarity(queryEmb, vp.Embedding)
		if sim >= cfg.Threshold {
			matches = append(matches, MatchResult{
				UserID:     vp.UserID,
				Email:      vp.Email,
				Label:      vp.Label,
				Similarity: math.Round(sim*10000) / 10000,
			})
		}
	}

	// Sort descending by similarity
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Similarity > matches[j].Similarity
	})
	return matches, nil
}

// ListByUser returns all voiceprints for a user.
func (s *Service) ListByUser(ctx context.Context, userID string) ([]*store.Voiceprint, error) {
	return s.repo.ListByUserID(ctx, userID)
}

// ListAll returns all voiceprints.
func (s *Service) ListAll(ctx context.Context) ([]*store.Voiceprint, error) {
	return s.repo.ListAll(ctx)
}

// Delete removes a single voiceprint by ID.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// DeleteByUser removes all voiceprints for a user.
func (s *Service) DeleteByUser(ctx context.Context, userID string) (int64, error) {
	return s.repo.DeleteByUserID(ctx, userID)
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom < 1e-10 {
		return 0
	}
	return dot / denom
}

// generateID creates a unique ID with timestamp + random suffix.
func generateID() string {
	var b [4]byte
	rand.Read(b[:])
	return fmt.Sprintf("vp_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}
