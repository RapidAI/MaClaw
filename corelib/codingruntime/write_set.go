package codingruntime

import (
	"crypto/sha256"
	"fmt"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
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
	if policy.FinalDiffGateRequired && !policy.WorkspaceIsolated {
		return PolicySnapshot{}, fmt.Errorf("coding runtime final diff gate requires isolated workspace")
	}
	scope := WriteScope{
		Mode:         firstNonEmpty(policy.Mode, task.Mode),
		ProjectRef:   firstNonEmpty(policy.ProjectRoot, task.ProjectRef),
		RemoteTarget: policy.RemoteTarget,
	}
	if strings.TrimSpace(scope.Mode) == "" {
		scope.Mode = "local"
	}
	scope.Mode = strings.ToLower(strings.TrimSpace(scope.Mode))
	if strings.EqualFold(scope.Mode, "remote") {
		if err := validateRemoteProjectReferenceSyntax(scope.ProjectRef); err != nil {
			return PolicySnapshot{}, err
		}
	}
	scope.ProjectRef = canonicalProjectRef(scope.Mode, scope.ProjectRef)
	// A legacy task may lack a workspace entirely. Preserve that fact as a
	// global unknown writer, which conflicts with any other writer instead of
	// inventing a safe scope.
	if strings.TrimSpace(scope.ProjectRef) == "" {
		if policy.FinalWorkspaceGateRequired || policy.WorkspaceIsolated || policy.FinalDiffGateRequired || len(policy.WriteSet.Claims) != 0 {
			if len(policy.WriteSet.Claims) != 0 {
				return PolicySnapshot{}, fmt.Errorf("coding runtime declared write claims require project reference")
			}
			return PolicySnapshot{}, fmt.Errorf("coding runtime gated writer requires project reference")
		}
		policy.WriteSet = WriteSet{Unknown: true}
		return policy, nil
	}
	if policy.WriteSet.Unknown && len(policy.WriteSet.Claims) != 0 {
		return PolicySnapshot{}, fmt.Errorf("coding runtime write set cannot be both unknown and declared")
	}
	// A remote policy can be used by a parent/legacy caller before remote
	// identity is available. Keep it as an unknown, serialized writer instead
	// of rejecting its lifecycle transition; it can never qualify for parallel
	// admission until the adapter supplies its stable target.
	if strings.EqualFold(strings.TrimSpace(scope.Mode), "remote") && strings.TrimSpace(scope.RemoteTarget) == "" {
		if policy.FinalWorkspaceGateRequired || policy.WorkspaceIsolated || policy.FinalDiffGateRequired || len(policy.WriteSet.Claims) != 0 {
			if len(policy.WriteSet.Claims) != 0 {
				return PolicySnapshot{}, fmt.Errorf("coding runtime declared remote write claims require stable remote target")
			}
			return PolicySnapshot{}, fmt.Errorf("coding runtime gated remote writer requires stable remote target")
		}
		policy.WriteSet = WriteSet{Scope: WriteScope{Mode: scope.Mode, ProjectRef: canonicalProjectRef(scope.Mode, scope.ProjectRef)}, Unknown: true}
		policy.Mode, policy.ProjectRoot = policy.WriteSet.Scope.Mode, policy.WriteSet.Scope.ProjectRef
		return policy, nil
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
	if policy.WorkspaceIsolated && (normalized.Unknown || len(normalized.Claims) == 0) {
		// An isolated writer is merged back under a frozen scope.  Without at
		// least one exact claim the merge gate cannot prove what it is allowed
		// to change, so treating the declaration as an unknown serialized writer
		// would defeat the isolation assertion.
		return PolicySnapshot{}, fmt.Errorf("coding runtime isolated writer requires declared write claims")
	}
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
	b.WriteString(canonicalProjectRef(copy.Mode, copy.ProjectRoot))
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
	rawProjectRef := strings.TrimSpace(scope.ProjectRef)
	scope.ProjectRef = canonicalProjectRef(scope.Mode, scope.ProjectRef)
	scope.RemoteTarget = strings.TrimSpace(scope.RemoteTarget)
	if scope.ProjectRef == "." || scope.ProjectRef == "" {
		return WriteSet{}, fmt.Errorf("coding runtime write set requires project reference")
	}
	if scope.Mode == "remote" {
		// Remote workdirs and claims are POSIX paths even when the host process
		// runs on Windows. Treating them with filepath.Clean/IsAbs would turn
		// "/srv/repo" into a drive-relative string and could miss an escape.
		if err := validateRemoteProjectReferenceSyntax(rawProjectRef); err != nil {
			return WriteSet{}, err
		}
		if !strings.HasPrefix(scope.ProjectRef, "/") || scope.ProjectRef == "/" {
			return WriteSet{}, fmt.Errorf("remote coding runtime write set requires a non-root absolute POSIX project reference")
		}
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
		path, err := normalizeWriteClaimPath(scope, raw, directory)
		if err != nil {
			return WriteSet{}, err
		}
		// Local Windows workspaces are case-insensitive, while remote POSIX
		// workspaces are case-sensitive.  Keep the same canonical identity for
		// de-duplication that admission uses for overlap checks; otherwise a
		// remote declaration containing both `Foo.go` and `foo.go` would silently
		// lose one of its exact claims.
		keyPath := path
		if !strings.EqualFold(scope.Mode, "remote") {
			keyPath = strings.ToLower(keyPath)
		}
		key := keyPath + fmt.Sprintf("|%t", directory)
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

func normalizeWriteClaimPath(scope WriteScope, raw string, directory bool) (string, error) {
	if strings.EqualFold(strings.TrimSpace(scope.Mode), "remote") {
		return normalizeRemoteWriteClaimPath(scope.ProjectRef, raw, directory)
	}
	projectRef := scope.ProjectRef
	path := filepath.Clean(raw)
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(projectRef, path)
		if err != nil {
			return "", fmt.Errorf("coding runtime write set path %q: %w", raw, err)
		}
		path = rel
	}
	if path == "." {
		if directory {
			// An explicit project-root directory claim is a bounded, conservative
			// fallback for dynamic implementation tasks whose planner cannot know
			// the target file until inspection. It remains a concrete claim and
			// conflicts with every descendant writer; it is not WriteSet.Unknown.
			return ".", nil
		}
		return "", fmt.Errorf("coding runtime write set cannot claim project root as a file; use an explicit directory declaration")
	}
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("coding runtime write set path %q escapes project root", raw)
	}
	return filepath.ToSlash(path), nil
}

// normalizeRemoteWriteClaimPath canonicalizes a claim relative to a remote
// POSIX project root. It deliberately does not use filepath: the GUI can run
// on Windows while the target workdir and changed-file list are POSIX.
func normalizeRemoteWriteClaimPath(projectRef, raw string, directory bool) (string, error) {
	projectRef = pathpkg.Clean(strings.TrimSpace(projectRef))
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("coding runtime write set contains empty path")
	}
	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("remote coding runtime write set path %q must use POSIX separators", raw)
	}
	if containsRemotePathControl(raw) {
		return "", fmt.Errorf("remote coding runtime write set path %q contains control characters", raw)
	}
	// Do not silently clean dot segments.  The frozen declaration is an
	// authorization boundary; accepting `src/../main.go` would make the
	// reviewed spelling differ from the path used at merge time.
	if hasRemoteDotSegment(raw) {
		return "", fmt.Errorf("remote coding runtime write set path %q contains traversal or normalization syntax", raw)
	}
	cleaned := pathpkg.Clean(raw)
	if strings.HasPrefix(cleaned, "/") {
		// Absolute claims are accepted only when they are inside the frozen
		// project root. A lexical prefix with a slash boundary is sufficient
		// after both paths have been path.Clean'ed.
		if cleaned == projectRef {
			return "", fmt.Errorf("coding runtime write set cannot claim remote project root as a file")
		}
		prefix := projectRef + "/"
		if !strings.HasPrefix(cleaned, prefix) {
			return "", fmt.Errorf("coding runtime write set path %q escapes remote project root", raw)
		}
		cleaned = strings.TrimPrefix(cleaned, prefix)
	}
	if cleaned == "." || cleaned == "" {
		if directory {
			return ".", nil
		}
		return "", fmt.Errorf("coding runtime write set cannot claim project root as a file; use an explicit directory declaration")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("coding runtime write set path %q escapes remote project root", raw)
	}
	return cleaned, nil
}

func hasRemoteDotSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return true
		}
	}
	return false
}

func containsRemotePathControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func validateRemoteProjectReferenceSyntax(projectRef string) error {
	if strings.Contains(projectRef, `\`) {
		return fmt.Errorf("remote coding runtime write set project reference must use POSIX separators")
	}
	if containsRemotePathControl(projectRef) || hasRemoteDotSegment(projectRef) {
		return fmt.Errorf("remote coding runtime write set project reference contains unsafe path syntax")
	}
	return nil
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
			if claimsOverlapForScope(s.Scope, left, right) {
				return WriteSetConflict{Conflicts: true, Reason: "overlapping write claim", Left: left, Right: right}
			}
		}
	}
	return WriteSetConflict{}
}

func sameWriteScope(left, right WriteScope) bool {
	leftMode := strings.ToLower(strings.TrimSpace(left.Mode))
	rightMode := strings.ToLower(strings.TrimSpace(right.Mode))
	if leftMode != rightMode {
		return false
	}
	leftProject, rightProject := canonicalProjectRef(leftMode, left.ProjectRef), canonicalProjectRef(rightMode, right.ProjectRef)
	// POSIX remote paths are case-sensitive; local Windows paths retain the
	// historical case-insensitive comparison used by the admission lock.
	projectEqual := leftProject == rightProject
	if leftMode != "remote" {
		projectEqual = strings.EqualFold(leftProject, rightProject)
	}
	if !projectEqual {
		return false
	}
	if leftMode == "remote" {
		return left.RemoteTarget == right.RemoteTarget
	}
	return true
}

// canonicalProjectRef uses the path grammar of the execution target. A
// remote workdir is always POSIX, regardless of the host OS running this
// package; local paths use the platform-native filepath rules.
func canonicalProjectRef(mode, projectRef string) string {
	projectRef = strings.TrimSpace(projectRef)
	if projectRef == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(mode), "remote") {
		return pathpkg.Clean(projectRef)
	}
	return filepath.Clean(projectRef)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// claimsOverlap is retained for callers that do not have a scope (legacy
// local semantics are case-insensitive). New admission paths should use
// claimsOverlapForScope so remote POSIX paths keep their case-sensitive
// identity.
func claimsOverlap(left, right WriteClaim) bool {
	return claimsOverlapForScope(WriteScope{Mode: "local"}, left, right)
}

func claimsOverlapForScope(scope WriteScope, left, right WriteClaim) bool {
	caseSensitive := strings.EqualFold(strings.TrimSpace(scope.Mode), "remote")
	if sameClaimPath(left.Path, right.Path, caseSensitive) {
		return true
	}
	if left.Directory && isClaimDescendantWithCase(right.Path, left.Path, caseSensitive) {
		return true
	}
	return right.Directory && isClaimDescendantWithCase(left.Path, right.Path, caseSensitive)
}

func isClaimDescendant(path, directory string) bool {
	return isClaimDescendantWithCase(path, directory, false)
}

func isClaimDescendantWithCase(path, directory string, caseSensitive bool) bool {
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	directory = strings.Trim(strings.ReplaceAll(directory, "\\", "/"), "/")
	if directory == "" || directory == "." {
		return path != "" && path != "."
	}
	if caseSensitive {
		return strings.HasPrefix(path, directory+"/")
	}
	return strings.HasPrefix(strings.ToLower(path), strings.ToLower(directory)+"/")
}

func sameClaimPath(left, right string, caseSensitive bool) bool {
	left = strings.Trim(strings.ReplaceAll(left, "\\", "/"), "/")
	right = strings.Trim(strings.ReplaceAll(right, "\\", "/"), "/")
	if caseSensitive {
		return left == right
	}
	return strings.EqualFold(left, right)
}
