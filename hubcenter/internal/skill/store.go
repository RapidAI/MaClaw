package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const pageSize = 40

// SkillStore 管理 Hub Center 侧的 Skill 存储。
// 使用 JSON 文件存储，每个 Skill 一个 JSON 文件。
type SkillStore struct {
	mu      sync.RWMutex
	dir     string
	index   []HubSkillMeta
	skills  map[string]*HubSkillFull
	ratings map[string][]SkillRating
}

func NewSkillStore(dir string) *SkillStore {
	s := &SkillStore{
		dir:     dir,
		skills:  make(map[string]*HubSkillFull),
		ratings: make(map[string][]SkillRating),
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = s.RebuildIndex()
	s.loadAllRatings()
	return s
}

func (s *SkillStore) Search(query string, tags []string, page int) SkillSearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	queryLower := strings.ToLower(query)
	queryTerms := strings.Fields(queryLower)
	var matched []HubSkillMeta
	for _, meta := range s.index {
		if !meta.Visible {
			continue
		}
		if matchesSkill(meta, queryTerms, tags) {
			matched = append(matched, meta)
		}
	}
	total := len(matched)
	start := (page - 1) * pageSize
	if start >= total {
		return SkillSearchResult{Skills: []HubSkillMeta{}, Total: total, Page: page}
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return SkillSearchResult{Skills: matched[start:end], Total: total, Page: page}
}

func (s *SkillStore) ListAll(page int) SkillSearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	total := len(s.index)
	start := (page - 1) * pageSize
	if start >= total {
		return SkillSearchResult{Skills: []HubSkillMeta{}, Total: total, Page: page}
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return SkillSearchResult{Skills: s.index[start:end], Total: total, Page: page}
}

func (s *SkillStore) Get(id string) (*HubSkillFull, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	skill, ok := s.skills[id]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", id)
	}
	return skill, nil
}

func (s *SkillStore) Publish(sk HubSkillFull) error {
	sk.Visible = true

	s.mu.Lock()
	defer s.mu.Unlock()

	// Upsert：如果同 ID 已存在，保留下载量和评分
	if existing, ok := s.skills[sk.ID]; ok {
		sk.Downloads = existing.Downloads
		sk.DownloadCount = existing.DownloadCount
		sk.RatingSum = existing.RatingSum
		sk.RatingCount = existing.RatingCount
		sk.AvgRating = existing.AvgRating
		sk.CreatedAt = existing.CreatedAt // 保留首次创建时间
		sk.UpdatedAt = fmtTimeNow()
	}

	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skill: %w", err)
	}
	path := filepath.Join(s.dir, sk.ID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}
	s.skills[sk.ID] = &sk
	s.rebuildIndexFromSkills()
	return nil
}

// GetByID 根据 ID 查找 Skill（返回 nil 表示不存在）。
func (s *SkillStore) GetByID(id string) *HubSkillMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sk, ok := s.skills[id]; ok {
		m := sk.HubSkillMeta
		return &m
	}
	return nil
}

func fmtTimeNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (s *SkillStore) RebuildIndex() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read skill dir: %w", err)
	}
	skills := make(map[string]*HubSkillFull)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var sk HubSkillFull
		if err := json.Unmarshal(data, &sk); err != nil {
			continue
		}
		if sk.ID == "" {
			continue
		}
		skills[sk.ID] = &sk
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skills = skills
	s.rebuildIndexFromSkills()
	return nil
}

func (s *SkillStore) rebuildIndexFromSkills() {
	index := make([]HubSkillMeta, 0, len(s.skills))
	for _, sk := range s.skills {
		index = append(index, sk.HubSkillMeta)
	}
	s.index = index
}

func (s *SkillStore) TopByDownloads(n int) []HubSkillMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n <= 0 || len(s.index) == 0 {
		return nil
	}
	var visible []HubSkillMeta
	for _, m := range s.index {
		if m.Visible {
			visible = append(visible, m)
		}
	}
	sort.Slice(visible, func(i, j int) bool {
		return visible[i].Downloads > visible[j].Downloads
	})
	if n > len(visible) {
		n = len(visible)
	}
	return visible[:n]
}

func (s *SkillStore) SetVisibility(id string, visible bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[id]
	if !ok {
		return fmt.Errorf("skill not found: %s", id)
	}
	sk.Visible = visible
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skill: %w", err)
	}
	path := filepath.Join(s.dir, id+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}
	s.rebuildIndexFromSkills()
	return nil
}

func (s *SkillStore) DeleteSkill(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.skills[id]; !ok {
		return fmt.Errorf("skill not found: %s", id)
	}
	_ = os.Remove(filepath.Join(s.dir, id+".json"))
	_ = os.Remove(filepath.Join(s.dir, id+"_ratings.json"))
	delete(s.skills, id)
	delete(s.ratings, id)
	s.rebuildIndexFromSkills()
	return nil
}

func (s *SkillStore) Rate(skillID, maclawID string, score int) error {
	if score < 1 || score > 5 {
		return fmt.Errorf("score must be between 1 and 5")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[skillID]
	if !ok {
		return fmt.Errorf("skill not found: %s", skillID)
	}
	ratings := s.ratings[skillID]
	found := false
	oldScore := 0
	for i, r := range ratings {
		if r.MaclawID == maclawID {
			oldScore = r.Score
			ratings[i].Score = score
			ratings[i].CreatedAt = time.Now().Format(time.RFC3339)
			found = true
			break
		}
	}
	if found {
		sk.RatingSum = sk.RatingSum - oldScore + score
	} else {
		ratings = append(ratings, SkillRating{
			SkillID: skillID, MaclawID: maclawID, Score: score,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		sk.RatingSum += score
		sk.RatingCount++
	}
	if sk.RatingCount > 0 {
		sk.AvgRating = float64(sk.RatingSum) / float64(sk.RatingCount)
	}
	s.ratings[skillID] = ratings
	if err := s.saveRatings(skillID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, skillID+".json"), data, 0o644)
}

func (s *SkillStore) saveRatings(skillID string) error {
	ratings := s.ratings[skillID]
	data, err := json.MarshalIndent(ratings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ratings: %w", err)
	}
	return os.WriteFile(filepath.Join(s.dir, skillID+"_ratings.json"), data, 0o644)
}

func (s *SkillStore) loadAllRatings() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, "_ratings.json") {
			continue
		}
		skillID := strings.TrimSuffix(name, "_ratings.json")
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		var ratings []SkillRating
		if json.Unmarshal(data, &ratings) == nil {
			s.ratings[skillID] = ratings
		}
	}
}

func matchesSkill(meta HubSkillMeta, queryTerms []string, tags []string) bool {
	if len(queryTerms) == 0 && len(tags) == 0 {
		return true
	}
	if len(tags) > 0 {
		tagSet := make(map[string]struct{}, len(meta.Tags))
		for _, t := range meta.Tags {
			tagSet[strings.ToLower(t)] = struct{}{}
		}
		for _, t := range tags {
			if _, ok := tagSet[strings.ToLower(t)]; !ok {
				return false
			}
		}
	}
	if len(queryTerms) == 0 {
		return true
	}
	searchText := strings.ToLower(meta.Name + " " + meta.Description + " " + strings.Join(meta.Tags, " "))
	for _, term := range queryTerms {
		if !strings.Contains(searchText, term) {
			return false
		}
	}
	return true
}

// IncrementDownloadCount 原子递增 Skill 的下载计数。
func (s *SkillStore) IncrementDownloadCount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[id]
	if !ok {
		return fmt.Errorf("skill not found: %s", id)
	}
	sk.DownloadCount++
	sk.Downloads = sk.DownloadCount // 同步两个字段
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, id+".json"), data, 0o644)
}

// UpdateStatus 更新 Skill 状态（使用乐观锁）。
func (s *SkillStore) UpdateStatus(id, expectedStatus, newStatus string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[id]
	if !ok {
		return fmt.Errorf("skill not found: %s", id)
	}
	if expectedStatus != "" && sk.Status != expectedStatus {
		return fmt.Errorf("concurrent conflict: expected status %s, got %s", expectedStatus, sk.Status)
	}
	sk.Status = newStatus
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return err
	}
	s.rebuildIndexFromSkills()
	return os.WriteFile(filepath.Join(s.dir, id+".json"), data, 0o644)
}

// GetByFingerprint 根据 fingerprint 查找 Skill。
func (s *SkillStore) GetByFingerprint(fingerprint string) *HubSkillMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.index {
		if m.Fingerprint == fingerprint {
			return &m
		}
	}
	return nil
}

// FindBySourceURL 根据 source_url 和 name 查找已存在的 Skill（用于覆盖更新）。
func (s *SkillStore) FindBySourceURL(sourceURL, name string) *HubSkillMeta {
	if sourceURL == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.index {
		if m.SourceURL == sourceURL && m.Name == name {
			return &m
		}
	}
	return nil
}
