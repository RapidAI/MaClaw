package user

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Dimension represents a single profile dimension with confidence tracking.
type Dimension struct {
	Value         string     `json:"value"`
	Confidence    float64    `json:"confidence"` // [0, 1]
	Evidence      []Evidence `json:"evidence"`
	UserConfirmed bool       `json:"user_confirmed"`
}

// Evidence records an observation that informed a dimension.
type Evidence struct {
	Observation string    `json:"observation"`
	Timestamp   time.Time `json:"timestamp"`
	Source      string    `json:"source"` // "pattern", "llm", "user"
}

// Profile is the complete user model.
type Profile struct {
	CommunicationStyle Dimension `json:"communication_style"`
	TechnicalLevel     Dimension `json:"technical_level"`
	PreferredLanguages Dimension `json:"preferred_languages"`
	DomainExpertise    Dimension `json:"domain_expertise"`
	WorkPatterns       Dimension `json:"work_patterns"`
	ToolPreferences    Dimension `json:"tool_preferences"`
}

// Model manages the user profile lifecycle.
type Model struct {
	profile  Profile
	filePath string
	store    *userModelStore
	mu       sync.RWMutex
}

// NewModel loads or initializes the user profile from SQLite next to filePath.
// The legacy JSON file is imported once; a missing or corrupted file produces
// an empty profile, matching the original file-backed behavior.
func NewModel(filePath string) (*Model, error) {
	store, err := openUserModelStore(filePath)
	if err != nil {
		return nil, err
	}
	profile, err := loadUserProfile(store.db)
	if err != nil {
		_ = store.db.Close()
		return nil, err
	}
	return &Model{profile: profile, filePath: filePath, store: store}, nil
}

// GetProfile returns a snapshot of the current profile.
func (m *Model) GetProfile() Profile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy
	p := m.profile
	// Deep copy evidence slices to prevent mutation
	p.CommunicationStyle.Evidence = copyEvidence(m.profile.CommunicationStyle.Evidence)
	p.TechnicalLevel.Evidence = copyEvidence(m.profile.TechnicalLevel.Evidence)
	p.PreferredLanguages.Evidence = copyEvidence(m.profile.PreferredLanguages.Evidence)
	p.DomainExpertise.Evidence = copyEvidence(m.profile.DomainExpertise.Evidence)
	p.WorkPatterns.Evidence = copyEvidence(m.profile.WorkPatterns.Evidence)
	p.ToolPreferences.Evidence = copyEvidence(m.profile.ToolPreferences.Evidence)
	return p
}

// maxEvidencePerDimension is the maximum number of evidence entries retained
// per dimension. Older entries are dropped to prevent unbounded memory growth.
const maxEvidencePerDimension = 50

// trimEvidence caps the evidence slice to the most recent maxEvidencePerDimension entries.
func trimEvidence(ev []Evidence) []Evidence {
	if len(ev) <= maxEvidencePerDimension {
		return ev
	}
	return ev[len(ev)-maxEvidencePerDimension:]
}

// UpdateDimension applies dialectic reconciliation when new evidence arrives.
// If the new value contradicts the existing value, confidence is lowered.
// If the new value matches, confidence is slightly increased.
func (m *Model) UpdateDimension(dimension string, newValue string, evidence Evidence) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dim := m.getDimension(dimension)
	if dim == nil {
		return fmt.Errorf("unknown dimension: %s", dimension)
	}

	if dim.Value == "" {
		// No existing value — set directly
		dim.Value = newValue
		dim.Confidence = 0.5
		dim.Evidence = append(dim.Evidence, evidence)
		dim.Evidence = trimEvidence(dim.Evidence)
		return nil
	}

	if dim.Value == newValue {
		// Reinforcement — increase confidence slightly
		dim.Confidence = dim.Confidence + (1.0-dim.Confidence)*0.1
		if dim.Confidence > 1.0 {
			dim.Confidence = 1.0
		}
		dim.Evidence = append(dim.Evidence, evidence)
		dim.Evidence = trimEvidence(dim.Evidence)
		return nil
	}

	// Contradiction — dialectic reconciliation
	// Thesis (existing) + Antithesis (new) → Synthesis (updated with lower confidence)
	dim.Value = newValue
	dim.Confidence = dim.Confidence * 0.5
	dim.Evidence = append(dim.Evidence, evidence)
	dim.Evidence = trimEvidence(dim.Evidence)
	return nil
}

// CorrectDimension sets a dimension to a user-confirmed value (confidence=1.0).
func (m *Model) CorrectDimension(dimension string, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dim := m.getDimension(dimension)
	if dim == nil {
		return fmt.Errorf("unknown dimension: %s", dimension)
	}

	dim.Value = value
	dim.Confidence = 1.0
	dim.UserConfirmed = true
	dim.Evidence = append(dim.Evidence, Evidence{
		Observation: fmt.Sprintf("User explicitly set to: %s", value),
		Timestamp:   time.Now(),
		Source:      "user",
	})
	dim.Evidence = trimEvidence(dim.Evidence)
	return nil
}

// ResetDimension clears a dimension back to empty state.
func (m *Model) ResetDimension(dimension string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dim := m.getDimension(dimension)
	if dim == nil {
		return fmt.Errorf("unknown dimension: %s", dimension)
	}

	dim.Value = ""
	dim.Confidence = 0
	dim.Evidence = nil
	dim.UserConfirmed = false
	return nil
}

// Save persists dimensions and evidence rows in one transaction.
func (m *Model) Save() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.store == nil || m.store.db == nil {
		return fmt.Errorf("user model store is closed")
	}
	return saveUserProfile(m.store.db, m.profile)
}

// Close releases the SQLite connection without changing the profile.
func (m *Model) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil || m.store.db == nil {
		return nil
	}
	err := m.store.db.Close()
	m.store.db = nil
	return err
}

// FormatForPrompt returns the profile formatted for system prompt injection.
// Only non-empty dimensions are included.
func (m *Model) FormatForPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("## User Profile\n\n")

	dimensions := []struct {
		name string
		dim  Dimension
	}{
		{"Communication Style", m.profile.CommunicationStyle},
		{"Technical Level", m.profile.TechnicalLevel},
		{"Preferred Languages", m.profile.PreferredLanguages},
		{"Domain Expertise", m.profile.DomainExpertise},
		{"Work Patterns", m.profile.WorkPatterns},
		{"Tool Preferences", m.profile.ToolPreferences},
	}

	hasContent := false
	for _, d := range dimensions {
		if d.dim.Value == "" {
			continue
		}
		hasContent = true
		confirmed := ""
		if d.dim.UserConfirmed {
			confirmed = " [confirmed]"
		}
		sb.WriteString(fmt.Sprintf("- **%s**: %s (confidence: %.2f)%s\n", d.name, d.dim.Value, d.dim.Confidence, confirmed))
	}

	if !hasContent {
		return ""
	}

	return sb.String()
}

// getDimension returns a pointer to the named dimension field on the Profile struct.
// Returns nil if the dimension name is not recognized.
func (m *Model) getDimension(name string) *Dimension {
	switch name {
	case "communication_style":
		return &m.profile.CommunicationStyle
	case "technical_level":
		return &m.profile.TechnicalLevel
	case "preferred_languages":
		return &m.profile.PreferredLanguages
	case "domain_expertise":
		return &m.profile.DomainExpertise
	case "work_patterns":
		return &m.profile.WorkPatterns
	case "tool_preferences":
		return &m.profile.ToolPreferences
	default:
		return nil
	}
}

// copyEvidence creates a deep copy of an evidence slice.
func copyEvidence(src []Evidence) []Evidence {
	if src == nil {
		return nil
	}
	dst := make([]Evidence, len(src))
	copy(dst, src)
	return dst
}
