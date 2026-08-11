package codingruntime

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// WriteScope identifies the workspace a declared write set applies to. For a
// remote workspace RemoteTarget must be the stable host identity/pin digest,
// not a display name. It intentionally mirrors the bounded task policy data
// and contains no credentials or shell commands.
type WriteScope struct {
	Mode         string
	ProjectRef   string
	RemoteTarget string
}

// WriteClaim declares a file or directory a writer may change. Directory is
// explicit because a directory claim conflicts with all descendants, whereas
// two distinct file claims under the same directory may run independently.
type WriteClaim struct {
	Path      string
	Directory bool
}

// WriteSet is the P3 admission input for a writer. Unknown is deliberately
// fail-closed: it conflicts with every other writer in the same workspace.
// It does not itself grant concurrency or create an isolated workspace.
type WriteSet struct {
	Scope   WriteScope
	Claims  []WriteClaim
	Unknown bool
}

// WriteSetConflict explains why two declared writers cannot share a wave.
type WriteSetConflict struct {
	Conflicts bool
	Reason    string
	Left      WriteClaim
	Right     WriteClaim
}

// WriterAdmissionError preserves a machine-checkable denial while retaining
// the bounded reason suitable for an audit event or user-facing review.
type WriterAdmissionError struct{ Conflict WriteSetConflict }

func (e WriterAdmissionError) Error() string {
	if e.Conflict.Reason == "" {
		return ErrWriterConflict.Error()
	}
	return ErrWriterConflict.Error() + ": " + e.Conflict.Reason
}

func (e WriterAdmissionError) Unwrap() error { return ErrWriterConflict }

// CanAdmitParallelWriters is intentionally stricter than write-set overlap
// detection. P3 requires every writer to have an isolated workspace and a
// successful final diff/merge gate; until a host provides those capabilities,
// this function refuses parallel admission even for disjoint claims.
func CanAdmitParallelWriters(left, right WriteSet, leftIsolated, rightIsolated, finalDiffGate bool) WriteSetConflict {
	if !sameWriteScope(left.Scope, right.Scope) {
		return WriteSetConflict{}
	}
	if conflict := left.ConflictsWith(right); conflict.Conflicts {
		return conflict
	}
	if !leftIsolated || !rightIsolated {
		return WriteSetConflict{Conflicts: true, Reason: "isolated workspace required"}
	}
	if !finalDiffGate {
		return WriteSetConflict{Conflicts: true, Reason: "final diff gate required"}
	}
	return WriteSetConflict{}
}

// NormalizeWriterPolicy freezes the write declaration that will be persisted
// with an Attempt. Existing callers that do not declare a write set become
// Unknown (and are consequently serialized); they never become implicitly
// parallel-safe. Read-only policies need no write declaration.
func NormalizeWriterPolicy(task Task, policy PolicySnapshot) (PolicySnapshot, error) {
	if policy.ReadOnly {
		return policy, nil
	}
	if policy.WorkspaceIsolated && !policy.FinalDiffGateRequired {
		return PolicySnapshot{}, fmt.Errorf("coding runtime isolated writer requires final diff gate")
	}
	scope := WriteScope{
		Mode:         firstNonEmpty(policy.Mode, task.Mode),
		ProjectRef:   firstNonEmpty(policy.ProjectRoot, task.ProjectRef),
		RemoteTarget: policy.RemoteTarget,
	}
	if strings.TrimSpace(scope.Mode) == "" {
		scope.Mode = "local"
	}
	// A legacy task may lack a workspace entirely. Preserve that fact as a
	// global unknown writer, which conflicts with any other writer instead of
	// inventing a safe scope.
	if strings.TrimSpace(scope.ProjectRef) == "" {
		policy.WriteSet = WriteSet{Unknown: true}
		return policy, nil
	}
	// A remote policy can be used by a parent/legacy caller before remote
	// identity is available. Keep it as an unknown, serialized writer instead
	// of rejecting its lifecycle transition; it can never qualify for parallel
	// admission until the adapter supplies its stable target.
	if strings.EqualFold(strings.TrimSpace(scope.Mode), "remote") && strings.TrimSpace(scope.RemoteTarget) == "" {
		policy.WriteSet = WriteSet{Scope: WriteScope{Mode: strings.ToLower(strings.TrimSpace(scope.Mode)), ProjectRef: filepath.Clean(scope.ProjectRef)}, Unknown: true}
		policy.Mode, policy.ProjectRoot = policy.WriteSet.Scope.Mode, policy.WriteSet.Scope.ProjectRef
		return policy, nil
	}
	if policy.WriteSet.Unknown && len(policy.WriteSet.Claims) != 0 {
		return PolicySnapshot{}, fmt.Errorf("coding runtime write set cannot be both unknown and declared")
	}
	declared := make([]string, 0, len(policy.WriteSet.Claims))
	for _, claim := range policy.WriteSet.Claims {
		path := strings.TrimSpace(claim.Path)
		if claim.Directory && path != "" && !strings.HasSuffix(path, "/") {
			path += "/"
		}
		declared = append(declared, path)
	}
	if policy.WriteSet.Unknown {
		declared = nil
	}
	normalized, err := NormalizeWriteSet(scope, declared)
	if err != nil {
		return PolicySnapshot{}, err
	}
	policy.WriteSet = normalized
	policy.Mode, policy.ProjectRoot, policy.RemoteTarget = normalized.Scope.Mode, normalized.Scope.ProjectRef, normalized.Scope.RemoteTarget
	return policy, nil
}

// PolicyDigest returns the deterministic identity of every frozen field that
// controls execution authority. Hosts must use it when creating a Task and
// its first Attempt; otherwise a task could keep the same requested-work
// digest while later attempts silently change its write scope or isolation.
// It deliberately hashes only bounded, non-secret policy fields.
func PolicyDigest(policy PolicySnapshot) (string, error) {
	copy := policy
	if copy.ReadOnly {
		copy.WriteSet = WriteSet{}
	} else {
		var err error
		copy, err = NormalizeWriterPolicy(Task{ProjectRef: copy.ProjectRoot, Mode: copy.Mode}, copy)
		if err != nil {
			return "", err
		}
	}
	claims := append([]WriteClaim(nil), copy.WriteSet.Claims...)
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Path == claims[j].Path {
			return !claims[i].Directory && claims[j].Directory
		}
		return claims[i].Path < claims[j].Path
	})
	var b strings.Builder
	b.WriteString("mode=")
	b.WriteString(strings.ToLower(strings.TrimSpace(copy.Mode)))
	b.WriteString("\nproject=")
	b.WriteString(filepath.Clean(strings.TrimSpace(copy.ProjectRoot)))
	b.WriteString("\nremote=")
	b.WriteString(strings.TrimSpace(copy.RemoteTarget))
	b.WriteString("\nreadonly=")
	b.WriteString(fmt.Sprintf("%t", copy.ReadOnly))
	b.WriteString("\nisolated=")
	b.WriteString(fmt.Sprintf("%t", copy.WorkspaceIsolated))
	b.WriteString("\nfinal_diff_gate=")
	b.WriteString(fmt.Sprintf("%t", copy.FinalDiffGateRequired))
	b.WriteString("\nfinal_workspace_gate=")
	b.WriteString(fmt.Sprintf("%t", copy.FinalWorkspaceGateRequired))
	b.WriteString("\nunknown=")
	b.WriteString(fmt.Sprintf("%t", copy.WriteSet.Unknown))
	for _, claim := range claims {
		b.WriteString("\nclaim=")
		b.WriteString(claim.Path)
		b.WriteString(fmt.Sprintf("|%t", claim.Directory))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

// WriterAdmissionConflict compares two running or prospective writer
// attempts. Read-only attempts never take this lock. An unknown workspace is
// global and fail-closed because it cannot be proven disjoint from anything.
func WriterAdmissionConflict(leftTask Task, left PolicySnapshot, rightTask Task, right PolicySnapshot) WriteSetConflict {
	if left.ReadOnly || right.ReadOnly {
		return WriteSetConflict{}
	}
	left, leftErr := NormalizeWriterPolicy(leftTask, left)
	right, rightErr := NormalizeWriterPolicy(rightTask, right)
	if leftErr != nil || rightErr != nil {
		return WriteSetConflict{Conflicts: true, Reason: "invalid write policy"}
	}
	if strings.TrimSpace(left.WriteSet.Scope.ProjectRef) == "" || strings.TrimSpace(right.WriteSet.Scope.ProjectRef) == "" {
		return WriteSetConflict{Conflicts: true, Reason: "unknown write scope"}
	}
	return CanAdmitParallelWriters(left.WriteSet, right.WriteSet, left.WorkspaceIsolated, right.WorkspaceIsolated, left.FinalDiffGateRequired && right.FinalDiffGateRequired)
}

// NormalizeWriteSet validates an explicit write declaration against its
// project root and produces deterministic, relative claim paths. Empty input
// is unknown rather than an empty/harmless write set; callers must serialize
// such writers. A trailing slash declares a directory.
func NormalizeWriteSet(scope WriteScope, declared []string) (WriteSet, error) {
	scope.Mode = strings.ToLower(strings.TrimSpace(scope.Mode))
	scope.ProjectRef = filepath.Clean(strings.TrimSpace(scope.ProjectRef))
	scope.RemoteTarget = strings.TrimSpace(scope.RemoteTarget)
	if scope.ProjectRef == "." || scope.ProjectRef == "" {
		return WriteSet{}, fmt.Errorf("coding runtime write set requires project reference")
	}
	if scope.Mode == "remote" && scope.RemoteTarget == "" {
		return WriteSet{}, fmt.Errorf("remote coding runtime write set requires stable remote target")
	}
	if len(declared) == 0 {
		return WriteSet{Scope: scope, Unknown: true}, nil
	}
	claims := make([]WriteClaim, 0, len(declared))
	seen := map[string]bool{}
	for _, raw := range declared {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return WriteSet{}, fmt.Errorf("coding runtime write set contains empty path")
		}
		if strings.ContainsAny(raw, "*?[") || strings.Contains(raw, "]") {
			return WriteSet{}, fmt.Errorf("coding runtime write set path %q must not contain a wildcard", raw)
		}
		if strings.HasPrefix(raw, "~") || strings.Contains(raw, "${") || strings.Contains(raw, "$(") {
			return WriteSet{}, fmt.Errorf("coding runtime write set path %q must not contain shell expansion", raw)
		}
		directory := strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, `\`)
		path, err := normalizeWriteClaimPath(scope.ProjectRef, raw)
		if err != nil {
			return WriteSet{}, err
		}
		key := strings.ToLower(path) + fmt.Sprintf("|%t", directory)
		if seen[key] {
			continue
		}
		seen[key] = true
		claims = append(claims, WriteClaim{Path: path, Directory: directory})
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Path == claims[j].Path {
			return !claims[i].Directory && claims[j].Directory
		}
		return claims[i].Path < claims[j].Path
	})
	return WriteSet{Scope: scope, Claims: claims}, nil
}

func normalizeWriteClaimPath(projectRef, raw string) (string, error) {
	path := filepath.Clean(raw)
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(projectRef, path)
		if err != nil {
			return "", fmt.Errorf("coding runtime write set path %q: %w", raw, err)
		}
		path = rel
	}
	if path == "." {
		return "", fmt.Errorf("coding runtime write set cannot claim project root as a file; use an explicit directory declaration")
	}
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("coding runtime write set path %q escapes project root", raw)
	}
	return filepath.ToSlash(path), nil
}

// ConflictsWith is conservative. Different canonical workspaces are
// independent; unknown declarations and overlapping directory/file claims in
// the same workspace conflict. This is only a P3 precondition: callers still
// need isolated workspaces, per-target locks and post-merge diff checking.
func (s WriteSet) ConflictsWith(other WriteSet) WriteSetConflict {
	if !sameWriteScope(s.Scope, other.Scope) {
		return WriteSetConflict{}
	}
	if s.Unknown || other.Unknown {
		return WriteSetConflict{Conflicts: true, Reason: "unknown write set"}
	}
	for _, left := range s.Claims {
		for _, right := range other.Claims {
			if claimsOverlap(left, right) {
				return WriteSetConflict{Conflicts: true, Reason: "overlapping write claim", Left: left, Right: right}
			}
		}
	}
	return WriteSetConflict{}
}

func sameWriteScope(left, right WriteScope) bool {
	if !strings.EqualFold(left.Mode, right.Mode) || !strings.EqualFold(filepath.Clean(left.ProjectRef), filepath.Clean(right.ProjectRef)) {
		return false
	}
	if strings.EqualFold(left.Mode, "remote") {
		return left.RemoteTarget == right.RemoteTarget
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func claimsOverlap(left, right WriteClaim) bool {
	if strings.EqualFold(left.Path, right.Path) {
		return true
	}
	if left.Directory && isClaimDescendant(right.Path, left.Path) {
		return true
	}
	return right.Directory && isClaimDescendant(left.Path, right.Path)
}

func isClaimDescendant(path, directory string) bool {
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	directory = strings.Trim(strings.ReplaceAll(directory, "\\", "/"), "/")
	return strings.HasPrefix(strings.ToLower(path), strings.ToLower(directory)+"/")
}
