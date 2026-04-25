package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SyncRecorder interface {
	AppendSkillHubSnapshot(ctx context.Context, snapshot *Snapshot)
}

type Snapshot struct {
	Skills  []HubSkillFull           `json:"skills"`
	Ratings map[string][]SkillRating `json:"ratings"`
}

func (s *SkillStore) SetSyncRecorder(rec SyncRecorder) {
	if s == nil {
		return
	}
	s.sync = rec
}

func (s *SkillStore) emitSync(ctx context.Context) {
	if s == nil || s.sync == nil {
		return
	}
	snap, err := s.DumpSnapshot()
	if err != nil {
		return
	}
	s.sync.AppendSkillHubSnapshot(ctx, snap)
}

func (s *SkillStore) DumpSnapshot() (*Snapshot, error) {
	if s == nil {
		return &Snapshot{}, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := &Snapshot{
		Skills:  make([]HubSkillFull, 0, len(s.skills)),
		Ratings: make(map[string][]SkillRating, len(s.ratings)),
	}
	ids := make([]string, 0, len(s.skills))
	for id := range s.skills {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		item := s.skills[id]
		if item == nil {
			continue
		}
		cp := *item
		if cp.Tags != nil {
			cp.Tags = append([]string(nil), cp.Tags...)
		}
		if cp.Triggers != nil {
			cp.Triggers = append([]string(nil), cp.Triggers...)
		}
		if cp.Steps != nil {
			cp.Steps = append([]HubSkillStep(nil), cp.Steps...)
		}
		if cp.Files != nil {
			cp.Files = cloneStringMap(cp.Files)
		}
		if cp.Manifest.RequiredMCP != nil {
			cp.Manifest.RequiredMCP = append([]string(nil), cp.Manifest.RequiredMCP...)
		}
		if cp.Manifest.Permissions != nil {
			cp.Manifest.Permissions = append([]string(nil), cp.Manifest.Permissions...)
		}
		if cp.Manifest.Dependencies != nil {
			cp.Manifest.Dependencies = append([]SkillDependency(nil), cp.Manifest.Dependencies...)
		}
		snap.Skills = append(snap.Skills, cp)
	}
	ratingIDs := make([]string, 0, len(s.ratings))
	for id := range s.ratings {
		ratingIDs = append(ratingIDs, id)
	}
	sort.Strings(ratingIDs)
	for _, id := range ratingIDs {
		items := s.ratings[id]
		cp := make([]SkillRating, 0, len(items))
		cp = append(cp, items...)
		snap.Ratings[id] = cp
	}
	return snap, nil
}

func (s *SkillStore) LoadSnapshot(snap *Snapshot) error {
	if s == nil || snap == nil {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read skill dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".json") {
			_ = os.Remove(filepath.Join(s.dir, name))
		}
	}
	nextSkills := make(map[string]*HubSkillFull, len(snap.Skills))
	for _, item := range snap.Skills {
		data, err := json.MarshalIndent(item, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal skill %s: %w", item.ID, err)
		}
		if err := os.WriteFile(filepath.Join(s.dir, item.ID+".json"), data, 0o644); err != nil {
			return fmt.Errorf("write skill %s: %w", item.ID, err)
		}
		cp := item
		nextSkills[item.ID] = &cp
	}
	nextRatings := make(map[string][]SkillRating, len(snap.Ratings))
	ratingIDs := make([]string, 0, len(snap.Ratings))
	for id := range snap.Ratings {
		ratingIDs = append(ratingIDs, id)
	}
	sort.Strings(ratingIDs)
	for _, id := range ratingIDs {
		items := append([]SkillRating(nil), snap.Ratings[id]...)
		if len(items) > 0 {
			data, err := json.MarshalIndent(items, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal ratings %s: %w", id, err)
			}
			if err := os.WriteFile(filepath.Join(s.dir, id+"_ratings.json"), data, 0o644); err != nil {
				return fmt.Errorf("write ratings %s: %w", id, err)
			}
		}
		nextRatings[id] = items
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skills = nextSkills
	s.ratings = nextRatings
	s.rebuildIndexFromSkills()
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
