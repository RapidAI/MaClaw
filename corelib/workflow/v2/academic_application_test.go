package v2

import (
	"slices"
	"strings"
	"testing"
)

// TestBuildAcademicApplicationTemplate_AllProfiles verifies that every registered
// FundingProfile produces a valid WorkflowTemplate with the expected structure.
func TestBuildAcademicApplicationTemplate_AllProfiles(t *testing.T) {
	for wfType, profile := range academicProfiles {
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)

			// Basic identity
			if tmpl.Type != profile.Type {
				t.Errorf("Type mismatch: got %q, want %q", tmpl.Type, profile.Type)
			}
			if tmpl.Name != profile.Name {
				t.Errorf("Name mismatch: got %q, want %q", tmpl.Name, profile.Name)
			}
			if len(tmpl.Keywords) == 0 {
				t.Error("Keywords should not be empty")
			}

			// Must have exactly 5 phases
			if len(tmpl.Phases) != 5 {
				t.Fatalf("expected 5 phases, got %d", len(tmpl.Phases))
			}

			// Phase 1 must have InputSchema with AcceptsResume
			p1 := tmpl.Phases[0]
			if p1.InputSchema == nil {
				t.Fatal("Phase 1 must have InputSchema")
			}
			if !p1.InputSchema.AcceptsResume {
				t.Error("Phase 1 InputSchema.AcceptsResume should be true")
			}
			if p1.InputSchema.AcceptsSupplementary == nil {
				t.Fatal("Phase 1 must declare supplementary document support")
			}
			for _, ext := range []string{".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx"} {
				if !slices.Contains(p1.InputSchema.AcceptsSupplementary.AcceptedTypes, ext) {
					t.Errorf("supplementary documents must accept %s", ext)
				}
			}
			// Fields are in Variants (resume_mode + manual_mode), not top-level Fields
			if len(p1.InputSchema.Variants) < 2 {
				t.Fatalf("Phase 1 must have at least 2 variants (resume_mode + manual_mode), got %d", len(p1.InputSchema.Variants))
			}
			manualVariant := p1.InputSchema.Variants[1] // manual_mode
			if len(manualVariant.Fields) == 0 {
				t.Error("manual_mode variant must have at least one form field")
			}

			// All phases must be NeedsConfirm; most are DocOnly except _foundation (Full for web_fetch)
			for i, phase := range tmpl.Phases {
				if !phase.NeedsConfirm {
					t.Errorf("Phase %d (%s) should have NeedsConfirm=true", i, phase.ID)
				}
				if strings.HasSuffix(phase.ID, "_foundation") {
					if phase.ToolPolicy != ToolPolicyFull {
						t.Errorf("Phase %d (%s) should have ToolPolicy=Full (for web_fetch), got %q", i, phase.ID, phase.ToolPolicy)
					}
				} else {
					if phase.ToolPolicy != ToolPolicyDocOnly {
						t.Errorf("Phase %d (%s) should have ToolPolicy=DocOnly, got %q", i, phase.ID, phase.ToolPolicy)
					}
				}
			}

			// Phase IDs must be unique and prefixed consistently
			prefix := inferPhasePrefix(profile.Type)
			seen := make(map[string]bool)
			for _, phase := range tmpl.Phases {
				if seen[phase.ID] {
					t.Errorf("duplicate phase ID: %s", phase.ID)
				}
				seen[phase.ID] = true
				if !strings.HasPrefix(phase.ID, prefix+"_") {
					t.Errorf("phase ID %q should start with %q", phase.ID, prefix+"_")
				}
			}
		})
	}
}

func TestPatentDisclosureFilePromptsDescribeSixOfficeFormats(t *testing.T) {
	for _, tmpl := range []*WorkflowTemplate{PatentApplicationTemplate(), USPatentApplicationTemplate()} {
		if tmpl == nil || len(tmpl.Phases) == 0 || tmpl.Phases[0].InputSchema == nil || len(tmpl.Phases[0].InputSchema.Variants) == 0 {
			t.Fatalf("invalid patent template: %#v", tmpl)
		}
		field := tmpl.Phases[0].InputSchema.Variants[0].Fields[0]
		for _, want := range []string{"PDF", "Word", "PowerPoint", "Excel"} {
			if !strings.Contains(field.Placeholder, want) {
				t.Errorf("%s disclosure placeholder missing %s: %q", tmpl.Type, want, field.Placeholder)
			}
		}
	}
}

// TestBuildAcademicApplicationTemplate_CommonFieldsPresent verifies that common
// academic fields (name, institution, etc.) are present in manual_mode variant
// unless explicitly omitted.
func TestBuildAcademicApplicationTemplate_CommonFieldsPresent(t *testing.T) {
	requiredCommon := []string{"name", "gender", "institution", "title", "discipline", "research_direction"}

	for wfType, profile := range academicProfiles {
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)
			// Fields are in manual_mode variant (index 1)
			manualFields := tmpl.Phases[0].InputSchema.Variants[1].Fields
			fieldNames := make(map[string]bool)
			for _, f := range manualFields {
				fieldNames[f.Name] = true
			}

			omitSet := make(map[string]bool)
			for _, name := range profile.OmitCommon {
				omitSet[name] = true
			}

			for _, required := range requiredCommon {
				if omitSet[required] {
					if fieldNames[required] {
						t.Errorf("field %q should be omitted (in OmitCommon) but is present", required)
					}
					continue
				}
				if !fieldNames[required] {
					t.Errorf("common field %q missing from manual_mode variant", required)
				}
			}
		})
	}
}

// TestBuildAcademicApplicationTemplate_ReusableFieldsMarked verifies that common
// fields have Reusable=true (for memory sediment/recall) in manual_mode variant.
func TestBuildAcademicApplicationTemplate_ReusableFieldsMarked(t *testing.T) {
	// These should always be Reusable when present
	reusableFieldNames := map[string]bool{
		"name": true, "gender": true, "birth_date": true, "institution": true,
		"title": true, "discipline": true, "research_direction": true,
		"h_index": true, "total_papers": true, "education": true,
	}

	for wfType, profile := range academicProfiles {
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)
			manualFields := tmpl.Phases[0].InputSchema.Variants[1].Fields
			for _, f := range manualFields {
				if reusableFieldNames[f.Name] && !f.Reusable {
					t.Errorf("field %q should be Reusable=true", f.Name)
				}
			}
		})
	}
}

// TestBuildAcademicApplicationTemplate_ExtraFieldsAppended verifies that
// profile-specific extra fields are appended in the manual_mode variant.
func TestBuildAcademicApplicationTemplate_ExtraFieldsAppended(t *testing.T) {
	for wfType, profile := range academicProfiles {
		if len(profile.ExtraFields) == 0 {
			continue
		}
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)
			manualFields := tmpl.Phases[0].InputSchema.Variants[1].Fields
			fieldNames := make(map[string]bool)
			for _, f := range manualFields {
				fieldNames[f.Name] = true
			}
			for _, extra := range profile.ExtraFields {
				if !fieldNames[extra.Name] {
					t.Errorf("extra field %q not found in manual_mode variant", extra.Name)
				}
			}
		})
	}
}

// TestBuildAcademicApplicationTemplate_OmitCommonWorks verifies that fields
// listed in OmitCommon are actually removed from the manual_mode variant.
func TestBuildAcademicApplicationTemplate_OmitCommonWorks(t *testing.T) {
	for wfType, profile := range academicProfiles {
		if len(profile.OmitCommon) == 0 {
			continue
		}
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)
			manualFields := tmpl.Phases[0].InputSchema.Variants[1].Fields
			fieldNames := make(map[string]bool)
			for _, f := range manualFields {
				fieldNames[f.Name] = true
			}
			for _, omitted := range profile.OmitCommon {
				if fieldNames[omitted] {
					t.Errorf("field %q should be omitted but is present", omitted)
				}
			}
		})
	}
}

// TestAcademicPhaseInstruction_AllPhasesHaveContent verifies that every phase
// of every profile generates a non-empty instruction.
func TestAcademicPhaseInstruction_AllPhasesHaveContent(t *testing.T) {
	for wfType, profile := range academicProfiles {
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)
			for _, phase := range tmpl.Phases {
				p := profile // copy for pointer
				instruction := AcademicPhaseInstruction(phase.ID, &p)
				if instruction == "" {
					t.Errorf("phase %q has empty instruction", phase.ID)
				}
				// Verify it contains the constraint block
				if !strings.Contains(instruction, "严禁") {
					t.Errorf("phase %q instruction missing constraint block", phase.ID)
				}
			}
		})
	}
}

// TestAcademicPhaseInstruction_TalentVsProject verifies that talent-type profiles
// generate different content than project-type profiles for the same phase positions.
func TestAcademicPhaseInstruction_TalentVsProject(t *testing.T) {
	talentProfile := NSFCDistinguishedYouthProfile()
	projectProfile := NSFCGeneralProfile()

	talentTmpl := BuildAcademicApplicationTemplate(talentProfile)
	projectTmpl := BuildAcademicApplicationTemplate(projectProfile)

	// Phase 2 instructions should differ (talent=学术影响力, project=研究基础)
	talentP2 := AcademicPhaseInstruction(talentTmpl.Phases[1].ID, &talentProfile)
	projectP2 := AcademicPhaseInstruction(projectTmpl.Phases[1].ID, &projectProfile)

	if talentP2 == projectP2 {
		t.Error("Phase 2 instructions should differ between talent and project types")
	}

	// Talent phase 2 should mention "原创性" or "影响力"
	if !strings.Contains(talentP2, "原创") && !strings.Contains(talentP2, "影响力") {
		t.Error("Talent Phase 2 should mention originality/impact")
	}

	// Project phase 2 should mention "可行性" or "研究基础"
	if !strings.Contains(projectP2, "可行") && !strings.Contains(projectP2, "研究基础") {
		t.Error("Project Phase 2 should mention feasibility/foundation")
	}
}

// TestAcademicPhaseInstruction_ReviewCriteriaInjected verifies that the
// ReviewCriteria from the profile is present in at least the first phase instruction.
func TestAcademicPhaseInstruction_ReviewCriteriaInjected(t *testing.T) {
	for wfType, profile := range academicProfiles {
		if profile.ReviewCriteria == "" {
			continue
		}
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)
			p := profile
			instruction := AcademicPhaseInstruction(tmpl.Phases[0].ID, &p)
			if !strings.Contains(instruction, "评审重点") {
				t.Error("Phase 1 instruction should contain ReviewCriteria")
			}
		})
	}
}

// TestIsAcademicApplicationPhase_Detection verifies the phaseID → profile lookup.
func TestIsAcademicApplicationPhase_Detection(t *testing.T) {
	tests := []struct {
		phaseID  string
		wantHit  bool
		wantType string
	}{
		{"cj_profile", true, "changjiang_scholar"},
		{"cj_foundation", true, "changjiang_scholar"},
		{"dy_profile", true, "nsfc_distinguished_youth"},
		{"dy_assembly", true, "nsfc_distinguished_youth"},
		{"ey_plan", true, "nsfc_excellent_youth"},
		{"gp_phase4", true, "nsfc_general"},
		{"kp_foundation", true, "nsfc_key"},
		{"yf_profile", true, "nsfc_youth"},
		// Non-academic phases
		{"requirements", false, ""},
		{"design", false, ""},
		{"pa_disclosure_parsing", false, ""},
		{"audience_goal", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.phaseID, func(t *testing.T) {
			profile, hit := IsAcademicApplicationPhase(tt.phaseID)
			if hit != tt.wantHit {
				t.Errorf("IsAcademicApplicationPhase(%q) hit=%v, want %v", tt.phaseID, hit, tt.wantHit)
			}
			if hit && profile.Type != tt.wantType {
				t.Errorf("IsAcademicApplicationPhase(%q) type=%q, want %q", tt.phaseID, profile.Type, tt.wantType)
			}
		})
	}
}

// TestInferPhasePrefix_KnownTypes verifies prefix generation for all known types.
func TestInferPhasePrefix_KnownTypes(t *testing.T) {
	tests := map[string]string{
		"changjiang_scholar":       "cj",
		"nsfc_distinguished_youth": "dy",
		"nsfc_excellent_youth":     "ey",
		"nsfc_youth":               "yf",
		"nsfc_general":             "gp",
		"nsfc_key":                 "kp",
	}
	for wfType, want := range tests {
		got := inferPhasePrefix(wfType)
		if got != want {
			t.Errorf("inferPhasePrefix(%q) = %q, want %q", wfType, got, want)
		}
	}
}

// TestBuildAcademicApplicationTemplate_FormTitleFromProfile verifies the form
// title comes from the profile, not hardcoded.
func TestBuildAcademicApplicationTemplate_FormTitleFromProfile(t *testing.T) {
	for wfType, profile := range academicProfiles {
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)
			if tmpl.Phases[0].InputSchema.Title != profile.FormTitle {
				t.Errorf("form title = %q, want %q", tmpl.Phases[0].InputSchema.Title, profile.FormTitle)
			}
		})
	}
}

// TestBuildAcademicApplicationTemplate_PhaseNamesFromEmphasis verifies phase
// names come from the Emphasis struct, not hardcoded.
func TestBuildAcademicApplicationTemplate_PhaseNamesFromEmphasis(t *testing.T) {
	for wfType, profile := range academicProfiles {
		t.Run(wfType, func(t *testing.T) {
			tmpl := BuildAcademicApplicationTemplate(profile)
			if tmpl.Phases[1].Name != profile.Emphasis.Phase2Title {
				t.Errorf("Phase 2 name = %q, want %q", tmpl.Phases[1].Name, profile.Emphasis.Phase2Title)
			}
			if tmpl.Phases[2].Name != profile.Emphasis.Phase3Title {
				t.Errorf("Phase 3 name = %q, want %q", tmpl.Phases[2].Name, profile.Emphasis.Phase3Title)
			}
			if tmpl.Phases[3].Name != profile.Emphasis.Phase4Title {
				t.Errorf("Phase 4 name = %q, want %q", tmpl.Phases[3].Name, profile.Emphasis.Phase4Title)
			}
			if tmpl.Phases[4].Name != profile.Emphasis.Phase5Title {
				t.Errorf("Phase 5 name = %q, want %q", tmpl.Phases[4].Name, profile.Emphasis.Phase5Title)
			}
		})
	}
}
