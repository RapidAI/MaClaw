package security

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectConstraints defines project-level constraint rules loaded from
// .maclaw/project-constraints.json.
type ProjectConstraints struct {
	ForbiddenPaths   []string `json:"forbidden_paths"`
	RequiredFiles    []string `json:"required_files"`
	ForbiddenImports []string `json:"forbidden_imports"`
}

// Violation represents a single constraint violation found during output check.
type Violation struct {
	Rule   string // constraint type: "forbidden_path", "required_file", "forbidden_import"
	Detail string // human-readable violation detail
	File   string // related file path (or "(missing)" for required files)
}

// HarnessGate extends SecurityFirewall with project constraint checks.
// It validates coding tool output against project-defined rules.
type HarnessGate struct {
	firewall    *Firewall
	constraints *ProjectConstraints
	audit       *AuditLog
}

// NewHarnessGate creates a HarnessGate. It attempts to load constraints from
// the project path; if the constraints file is missing or invalid, constraints
// remain nil and only security checks are performed.
func NewHarnessGate(firewall *Firewall, projectPath string) *HarnessGate {
	g := &HarnessGate{
		firewall: firewall,
	}
	if firewall != nil {
		g.audit = firewall.audit
	}
	if projectPath != "" {
		if err := g.LoadConstraints(projectPath); err != nil {
			log.Printf("[HarnessGate] load constraints: %v", err)
		}
	}
	return g
}

// LoadConstraints loads project constraints from .maclaw/project-constraints.json.
// Returns nil if the file doesn't exist (constraints remain nil).
func (g *HarnessGate) LoadConstraints(projectPath string) error {
	constraintPath := filepath.Join(projectPath, ".maclaw", "project-constraints.json")
	data, err := os.ReadFile(constraintPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No constraints file → constraints stay nil, only security checks
			return nil
		}
		return fmt.Errorf("harness gate: read constraints: %w", err)
	}
	var pc ProjectConstraints
	if err := json.Unmarshal(data, &pc); err != nil {
		log.Printf("[HarnessGate] invalid constraints JSON, skipping: %v", err)
		return nil
	}
	g.constraints = &pc
	return nil
}

// CheckOutput validates changed files against project constraints and logs
// results to the AuditLog. Returns empty slice when no constraints are loaded.
func (g *HarnessGate) CheckOutput(sessionID string, changedFiles []string) []Violation {
	var violations []Violation

	if g.constraints == nil {
		g.logCheck(sessionID, "harness_gate_check", "no_constraints", "allow")
		return violations
	}

	// 1. Check forbidden paths
	for _, cf := range changedFiles {
		for _, pattern := range g.constraints.ForbiddenPaths {
			matched, err := filepath.Match(pattern, cf)
			if err != nil {
				log.Printf("[HarnessGate] bad glob pattern %q: %v", pattern, err)
				continue
			}
			if matched {
				violations = append(violations, Violation{
					Rule:   "forbidden_path",
					Detail: fmt.Sprintf("修改了禁止文件 %s", cf),
					File:   cf,
				})
			}
		}
	}

	// 2. Check required files — each pattern must match at least one changed file
	for _, reqPattern := range g.constraints.RequiredFiles {
		found := false
		for _, cf := range changedFiles {
			matched, err := filepath.Match(reqPattern, cf)
			if err != nil {
				log.Printf("[HarnessGate] bad glob pattern %q: %v", reqPattern, err)
				continue
			}
			if matched {
				found = true
				break
			}
		}
		if !found {
			violations = append(violations, Violation{
				Rule:   "required_file",
				Detail: fmt.Sprintf("缺少测试文件 %s", reqPattern),
				File:   "(missing)",
			})
		}
	}

	// 3. Check forbidden imports (file-name level check for now)
	for _, cf := range changedFiles {
		base := filepath.Base(cf)
		for _, forbidden := range g.constraints.ForbiddenImports {
			if strings.Contains(base, forbidden) {
				violations = append(violations, Violation{
					Rule:   "forbidden_import",
					Detail: fmt.Sprintf("文件名包含禁止的依赖 %s", forbidden),
					File:   cf,
				})
			}
		}
	}

	// Log results
	result := "pass"
	if len(violations) > 0 {
		result = fmt.Sprintf("violations:%d", len(violations))
	}
	g.logCheck(sessionID, "harness_gate_check", result, "audit")

	return violations
}

// BuildViolationReport generates a formatted violation report string.
// Returns empty string when violations is empty.
func (g *HarnessGate) BuildViolationReport(violations []Violation) string {
	if len(violations) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[⚠️ 产出违规报告]\n")
	for _, v := range violations {
		sb.WriteString(fmt.Sprintf("- 违规: %s | 详情: %s | 文件: %s\n", v.Rule, v.Detail, v.File))
	}
	sb.WriteString("[/违规报告]")
	return sb.String()
}

// logCheck records a harness gate check result to the audit log.
func (g *HarnessGate) logCheck(sessionID, toolName, result, action string) {
	if g.audit == nil {
		return
	}
	_ = g.audit.Log(AuditEntry{
		Timestamp:    time.Now(),
		SessionID:    sessionID,
		ToolName:     toolName,
		RiskLevel:    RiskLow,
		PolicyAction: PolicyAudit,
		Result:       result,
		Action:       AuditAction(action),
	})
}
