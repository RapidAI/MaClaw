package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// SkillIDOwnershipRegistrar is the interface needed by the migration to
// register ownership. Implemented by skillmarket.Store.
type SkillIDOwnershipRegistrar interface {
	RegisterSkillIDOwnershipIfAbsent(ctx context.Context, skillID, userID, email string) error
}

// MigrateSkillIDsReport summarizes the one-time migration result.
type MigrateSkillIDsReport struct {
	Migrated        int
	AlreadyMigrated int
	Skipped         int
	SkippedReasons  []string
}

// MigrateSkillIDs assigns a publisher.skill-name skill_id to all existing
// skills that don't have one. Uses DerivePublisher + SanitizeSkillNameForID
// from corelib/skill. Idempotent — safe to call on every startup.
//
// ownershipReg may be nil (ownership registration is skipped silently).
func (s *SkillStore) MigrateSkillIDs(ownershipReg SkillIDOwnershipRegistrar) MigrateSkillIDsReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	report := MigrateSkillIDsReport{}
	assigned := make(map[string]string) // skill_id → internal UUID (batch collision detection)

	for uuid, sk := range s.skills {
		if sk.SkillID != "" {
			report.AlreadyMigrated++
			continue
		}

		email := strings.TrimSpace(sk.UploaderEmail)
		if email == "" {
			// Try extracting from Fingerprint (format: "email:name")
			if parts := strings.SplitN(sk.Fingerprint, ":", 2); len(parts) == 2 {
				email = strings.TrimSpace(parts[0])
			}
		}
		if email == "" {
			report.Skipped++
			report.SkippedReasons = append(report.SkippedReasons,
				fmt.Sprintf("%s (%s): no uploader email", uuid, sk.Name))
			continue
		}

		publisher := cskill.DerivePublisher(email)
		name := cskill.SanitizeSkillNameForID(sk.Name)
		if publisher == "" || name == "" {
			report.Skipped++
			report.SkippedReasons = append(report.SkippedReasons,
				fmt.Sprintf("%s (%s): could not derive publisher/name from %q", uuid, sk.Name, email))
			continue
		}

		skillID := publisher + "." + name

		// Batch collision: same skill_id already assigned to a different internal UUID
		if existingUUID, exists := assigned[skillID]; exists && existingUUID != uuid {
			// Disambiguate by appending first 4 chars of UUID
			skillID = publisher + "." + name + "-" + uuid[:4]
		}

		// Validate the generated ID
		if !cskill.IsValidSkillID(skillID) {
			report.Skipped++
			report.SkippedReasons = append(report.SkippedReasons,
				fmt.Sprintf("%s (%s): generated id %q is invalid", uuid, sk.Name, skillID))
			continue
		}

		// Assign
		sk.SkillID = skillID
		assigned[skillID] = uuid

		// Register ownership (best-effort, non-fatal)
		if ownershipReg != nil {
			uploaderID := strings.TrimSpace(sk.UploaderID)
			if uploaderID == "" {
				uploaderID = email // fallback
			}
			_ = ownershipReg.RegisterSkillIDOwnershipIfAbsent(context.Background(), skillID, uploaderID, email)
		}

		// Persist to disk
		s.skills[uuid] = sk
		if err := s.persistSkillLocked(sk); err != nil {
			log.Printf("[skill-id-migration] persist %s failed: %v", uuid, err)
		}

		report.Migrated++
	}

	if report.Migrated > 0 {
		log.Printf("[skill-id-migration] migrated=%d already=%d skipped=%d",
			report.Migrated, report.AlreadyMigrated, report.Skipped)
	}
	return report
}

// persistSkillLocked writes a single skill to disk. Must be called with s.mu held.
func (s *SkillStore) persistSkillLocked(sk *HubSkillFull) error {
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skill: %w", err)
	}
	path := filepath.Join(s.dir, sk.ID+".json")
	return os.WriteFile(path, data, 0o644)
}
