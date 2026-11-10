package guiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// A shared work root belongs to the session that is already executing there.
// Status rows use the same rule (taskMayUseSharedDirectoryStatus): a directory
// claimed by more than one task contributes its live run only to the session
// that is executing. Tool cwd, the prompt's working directory, and the write
// boundary read EffectiveWorkingDirForOwner / projectTabWorkDirForOwner, so
// those must obey the same ownership.
//
// The later session keeps its working_dir tag. Its commands run in a detached
// checkout of HEAD, created outside both the task folder and the occupied
// tree. git does not stash or reset the live tree. The checkout stays at the
// HEAD from when that occupation started, so a commit the holder makes during
// the run is not pulled into the later session. The next occupation snapshots
// HEAD again. Repeat resolutions of the same occupation reuse that checkout
// and do not run git. When the earlier session leaves, the tag is the work
// root again. A directory that is not a git checkout cannot be snapshotted;
// the later session then uses its own workspace and still does not enter the
// live tree. That stable refusal is remembered for the occupation.

type exclusiveCheckout struct {
	gitRoot   string
	path      string
	requested string
	result    string
	holder    string
	entered   int64
	fallback  bool
}

var (
	exclusiveCheckouts      sync.Map
	exclusiveCheckoutLocks  sync.Map
	exclusiveFallbackLogged sync.Map
)

var (
	errExclusiveNotGit           = errors.New("not a git checkout")
	errExclusiveNoCommits        = errors.New("git checkout has no commits")
	errExclusiveGitContainsStore = errors.New("git root contains the task store")
)

func lockExclusiveCheckout(key string) func() {
	value, _ := exclusiveCheckoutLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

type liveWorkRootSession struct {
	taskPath string
	entered  int64
}

var workRootAgentOwners = struct {
	sync.Mutex
	counts    map[string]int
	enteredNs map[string]int64
}{
	counts:    make(map[string]int),
	enteredNs: make(map[string]int64),
}

func beginWorkRootAgentOwner(ownerID string) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		ownerID = "unknown"
	}
	workRootAgentOwners.Lock()
	defer workRootAgentOwners.Unlock()
	if workRootAgentOwners.counts[ownerID] == 0 {
		workRootAgentOwners.enteredNs[ownerID] = time.Now().UnixNano()
	}
	workRootAgentOwners.counts[ownerID]++
}

func endWorkRootAgentOwner(ownerID string) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		ownerID = "unknown"
	}
	workRootAgentOwners.Lock()
	defer workRootAgentOwners.Unlock()
	if workRootAgentOwners.counts[ownerID] <= 1 {
		delete(workRootAgentOwners.counts, ownerID)
		delete(workRootAgentOwners.enteredNs, ownerID)
		return
	}
	workRootAgentOwners.counts[ownerID]--
}

func liveWorkRootSessions() []liveWorkRootSession {
	workRootAgentOwners.Lock()
	defer workRootAgentOwners.Unlock()
	if len(workRootAgentOwners.counts) == 0 {
		return nil
	}
	out := make([]liveWorkRootSession, 0, len(workRootAgentOwners.counts))
	for owner, count := range workRootAgentOwners.counts {
		if count <= 0 {
			continue
		}
		taskPath := projectPathFromSessionOwnerID(owner)
		if taskPath == "" {
			continue
		}
		out = append(out, liveWorkRootSession{
			taskPath: taskPath,
			entered:  workRootAgentOwners.enteredNs[owner],
		})
	}
	return out
}

// earlierLiveWorkRootHolder is the other live task whose claimed directory
// contains dir and which entered before this task. Empty when this task may
// use dir: it is the only live session there, or it entered first.
func (a *App) earlierLiveWorkRootHolder(selfPath, dir string) (string, int64) {
	selfPath = normalizeProjectSessionPath(selfPath)
	if a == nil || selfPath == "" || strings.TrimSpace(dir) == "" {
		return "", 0
	}
	if isLocalCodingIdentityOrSandbox(selfPath, dir) {
		return "", 0
	}
	var selfEntered int64
	selfLive := false
	var holder string
	var holderEntered int64
	for _, session := range liveWorkRootSessions() {
		other := normalizeProjectSessionPath(session.taskPath)
		if other == "" || other == selfPath {
			if other == selfPath {
				selfLive = true
				selfEntered = session.entered
			}
			continue
		}
		// Only a directory this task itself claims can be held. The process
		// workspace is not a claim: a live session whose record is not here
		// would otherwise occupy every directory under that shared default.
		claimed, ok := a.claimedWorkRoot(other)
		if !ok || !pathInsideWorkRoot(claimed, dir) {
			continue
		}
		if holder == "" || session.entered < holderEntered || (session.entered == holderEntered && other < holder) {
			holder = other
			holderEntered = session.entered
		}
	}
	if holder == "" {
		return "", 0
	}
	if !selfLive {
		return holder, holderEntered
	}
	if holderEntered < selfEntered || (holderEntered == selfEntered && holder < selfPath) {
		return holder, holderEntered
	}
	return "", 0
}

// claimedWorkRoot is the directory a task record says it executes in.
// It does not fall back to the process workspace. A session with no
// resolvable claim holds nothing.
func (a *App) claimedWorkRoot(projectPath string) (string, bool) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return "", false
	}
	owner := projectSessionOwnerID(projectPath)
	if dir := a.BoundWorkingDirForOwner(owner); dir != "" && !isLocalCodingIdentityOrSandbox(projectPath, dir) {
		return dir, true
	}
	if wd := a.codingTaskTaggedWorkRoot(projectPath); wd != "" {
		return wd, true
	}
	if wd := a.recentTaskWorkingDir(projectPath); wd != "" {
		return wd, true
	}
	if a.projectPathIsLocalCodingWorkbench(projectPath) {
		dir := normalizeProjectSessionPath(a.EffectiveDesktopWorkingDir())
		if dir != "" && !isLocalCodingIdentityOrSandbox(projectPath, dir) {
			return dir, true
		}
	}
	if a.isManagedRecentTaskWorkspacePath(projectPath) {
		return filepath.Join(projectPath, "workspace"), true
	}
	if !looksLikeManagedTaskIdentity(projectPath) {
		return projectPath, true
	}
	return "", false
}

func pathInsideWorkRoot(root, child string) bool {
	_, ok := workRootRelative(root, child)
	return ok
}

// sameTaskWorkRoot reports that left and right are one task's work.
// The execution directory is not the conversation. A later task that shares
// the claimed tree is given a private checkout or sandbox; that path still
// belongs to the task that was already running there. A different repository
// does not.
func sameTaskWorkRoot(userID, left, right string) bool {
	left = normalizeProjectSessionPath(left)
	right = normalizeProjectSessionPath(right)
	if left == "" || right == "" {
		return false
	}
	if sameNormalizedPath(left, right) {
		return true
	}
	taskPath := projectPathFromSessionOwnerID(userID)
	// Two private copies match when one is inside the other: the sandbox and
	// a directory under it, or the checkout and a directory under it. The
	// sandbox and the checkout are different copies, and so are sibling
	// directories inside one copy.
	if taskPath != "" && taskPrivateExecution(taskPath, left) && taskPrivateExecution(taskPath, right) &&
		(pathInsideWorkRoot(left, right) || pathInsideWorkRoot(right, left)) {
		return true
	}
	// The occupation record is a map lookup. Git runs only when this task
	// has not already detached one of these paths from the other.
	if exclusiveCacheLinks(taskPath, left, right) {
		return true
	}
	return sameGitWork(left, right)
}

// sameGitWork reports that both paths are one project tree. A subdirectory
// of a worktree is that worktree. A linked checkout and the tree it was
// detached from share a common git directory, at the same place in that
// tree. Two folders that merely sit inside some parent repository, and two
// sibling directories of one repository, are not the same project.
func sameGitWork(left, right string) bool {
	a := gitPathIdentity(left)
	b := gitPathIdentity(right)
	if a.top == "" || b.top == "" || a.common == "" || b.common == "" {
		return false
	}
	if !sameNormalizedPath(a.common, b.common) {
		return false
	}
	if sameNormalizedPath(a.top, b.top) {
		return pathInsideWorkRoot(left, right) || pathInsideWorkRoot(right, left)
	}
	// Linked worktrees. The root of one corresponds to the root of the
	// other, and a directory corresponds to that same directory there.
	// A sibling directory is a different place.
	leftRel, leftOK := workRootRelative(a.top, left)
	rightRel, rightOK := workRootRelative(b.top, right)
	if !leftOK || !rightOK {
		return false
	}
	return sameTreeLocation(leftRel, rightRel)
}

// sameTreeLocation reports that two relative paths are one place in a pair
// of linked worktrees. The worktree root contains its subdirectories. A
// directory does not contain its sibling.
func sameTreeLocation(leftRel, rightRel string) bool {
	if leftRel == "." || rightRel == "." {
		return true
	}
	return leftRel == rightRel || strings.HasPrefix(leftRel, rightRel+"/") || strings.HasPrefix(rightRel, leftRel+"/")
}

type gitPathID struct {
	top    string
	common string
}

type gitPathCacheEntry struct {
	stamp string
	id    gitPathID
}

// gitPathIDCache remembers one rev-parse per directory that was actually
// probed. A path that does not exist yet is probed from its parent, so the
// key is that parent; once the directory exists it is probed on its own.
// gitPathIDByStamp reuses that answer for every other directory under the
// same .git marker, so a second path in one repository does not start git.
// The stamp is the .git file or directory git would walk to. Creating a
// repository, or adding a worktree, changes the stamp and the next
// comparison asks git again.
var (
	gitPathIDCache   sync.Map
	gitPathIDByStamp sync.Map
)

func gitPathIdentity(path string) gitPathID {
	probe := directoryForGitProbe(path)
	if strings.TrimSpace(probe) == "" {
		return gitPathID{}
	}
	key := normalizeCodingRuntimeRef(probe)
	stamp := gitProbeStamp(probe)
	if cached, ok := gitPathIDCache.Load(key); ok {
		entry, _ := cached.(gitPathCacheEntry)
		if entry.stamp == stamp {
			return entry.id
		}
	}
	// No .git marker anywhere above this directory. rev-parse would fail
	// the same way, and the next comparison sees a new marker if one appears.
	if stamp == "" {
		gitPathIDCache.Store(key, gitPathCacheEntry{})
		return gitPathID{}
	}
	if cached, ok := gitPathIDByStamp.Load(stamp); ok {
		id, _ := cached.(gitPathID)
		gitPathIDCache.Store(key, gitPathCacheEntry{stamp: stamp, id: id})
		return id
	}
	id := probeGitPathIdentity(probe)
	gitPathIDCache.Store(key, gitPathCacheEntry{stamp: stamp, id: id})
	if id.top != "" {
		gitPathIDByStamp.Store(stamp, id)
	}
	return id
}

// gitProbeStamp identifies the repository marker above probe. Empty when
// no .git file or directory is visible. The marker's path, kind, and
// modification time are part of the stamp, so a later git init is not
// answered from the old probe.
func gitProbeStamp(probe string) string {
	cur := probe
	for {
		gitPath := filepath.Join(cur, ".git")
		info, err := os.Stat(gitPath)
		if err == nil {
			kind := "d"
			if !info.IsDir() {
				kind = "f"
			}
			return normalizeCodingRuntimeRef(cur) + ":" + kind + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10) + ":" + strconv.FormatInt(info.Size(), 10)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func probeGitPathIdentity(probe string) gitPathID {
	if strings.TrimSpace(probe) == "" {
		return gitPathID{}
	}
	out, err := remote.RunGitOutput(probe, "rev-parse", "--show-toplevel", "--git-common-dir")
	if err != nil {
		return gitPathID{}
	}
	lines := strings.FieldsFunc(strings.TrimSpace(out), func(r rune) bool { return r == '\n' || r == '\r' })
	top, common := gitIdentityLines(lines)
	if top == "" || common == "" {
		return gitPathID{}
	}
	if !filepath.IsAbs(top) {
		top = filepath.Join(probe, top)
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(probe, common)
	}
	top = normalizeProjectSessionPath(top)
	common = normalizeProjectSessionPath(common)
	if !pathInsideWorkRoot(top, normalizeProjectSessionPath(probe)) {
		return gitPathID{}
	}
	return gitPathID{top: top, common: common}
}

// gitIdentityLines reads toplevel and common-dir from rev-parse. A hint on
// stderr is mixed into the combined output and is not one of those paths.
func gitIdentityLines(lines []string) (string, string) {
	paths := make([]string, 0, 2)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || gitIdentityNoise(line) {
			continue
		}
		paths = append(paths, line)
		if len(paths) == 2 {
			break
		}
	}
	if len(paths) < 2 {
		return "", ""
	}
	return paths[0], paths[1]
}

func gitIdentityNoise(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "warning:") || strings.HasPrefix(lower, "hint:") || strings.HasPrefix(lower, "fatal:")
}

// exclusiveCacheLinks reports that this process already detached one path
// from the other for this task. The cache is the occupation record, so a
// sandbox matches only the tree it was created to replace.
func exclusiveCacheLinks(taskPath, left, right string) bool {
	taskPath = normalizeProjectSessionPath(taskPath)
	if taskPath == "" {
		return false
	}
	cached, ok := exclusiveCheckouts.Load(normalizeRecentTaskPathKey(taskPath))
	if !ok {
		return false
	}
	got, ok := cached.(exclusiveCheckout)
	if !ok {
		return false
	}
	// requested is the directory this occupation replaced. gitRoot is the
	// repository that contains it; using that would also link sibling
	// directories. gitRoot remains the fallback when the request was not
	// recorded.
	claimed := got.requested
	if strings.TrimSpace(claimed) == "" {
		claimed = got.gitRoot
	}
	copy := got.result
	if strings.TrimSpace(copy) == "" {
		copy = got.path
	}
	if strings.TrimSpace(claimed) == "" || strings.TrimSpace(copy) == "" {
		return false
	}
	return cachePathsSamePlace(claimed, copy, left, right) || cachePathsSamePlace(copy, claimed, left, right)
}

// exclusiveCopyHolds reports that current sits inside the private copy this
// task's occupation record names. A checkout record does not hold the
// workspace sandbox, and a sandbox record does not hold a different directory.
func exclusiveCopyHolds(taskPath, current string) bool {
	taskPath = normalizeProjectSessionPath(taskPath)
	current = normalizeProjectSessionPath(current)
	if taskPath == "" || current == "" {
		return false
	}
	cached, ok := exclusiveCheckouts.Load(normalizeRecentTaskPathKey(taskPath))
	if !ok {
		return false
	}
	got, ok := cached.(exclusiveCheckout)
	if !ok {
		return false
	}
	copy := strings.TrimSpace(got.result)
	if copy == "" {
		copy = strings.TrimSpace(got.path)
	}
	return copy != "" && pathInsideWorkRoot(copy, current)
}

// taskSandboxExecution reports that dir is this task's workspace sandbox or
// the temp sandbox used when a checkout cannot be made. The exclusive
// checkout is a git worktree, not this sandbox.
func taskSandboxExecution(taskPath, dir string) bool {
	if !taskPrivateExecution(taskPath, dir) {
		return false
	}
	leaf := strings.ToLower(exclusiveTaskLeaf(taskPath))
	ref := normalizeCodingRuntimeRef(dir)
	return !pathHasPrivateLeaf(ref, "/exclusive/"+leaf)
}

// cachePathsSamePlace reports that left is inside rootA and right is inside
// rootB at the same place. A subdirectory of the occupied tree matches that
// subdirectory of the copy. It does not match a sibling there.
func cachePathsSamePlace(rootA, rootB, left, right string) bool {
	relLeft, okLeft := workRootRelative(rootA, left)
	relRight, okRight := workRootRelative(rootB, right)
	return okLeft && okRight && sameTreeLocation(relLeft, relRight)
}

// taskPrivateExecution reports that dir is this task's own copy of a tree
// another live session already holds: the workspace sandbox, the exclusive
// checkout, or the temp sandbox used when that checkout cannot be made.
// The task record itself is identity, not an execution copy.
func taskPrivateExecution(taskPath, dir string) bool {
	taskPath = normalizeProjectSessionPath(taskPath)
	dir = normalizeProjectSessionPath(dir)
	if taskPath == "" || dir == "" || sameNormalizedPath(dir, taskPath) {
		return false
	}
	if pathInsideWorkRoot(filepath.Join(taskPath, "workspace"), dir) {
		return true
	}
	leaf := strings.ToLower(exclusiveTaskLeaf(taskPath))
	ref := normalizeCodingRuntimeRef(dir)
	return pathHasPrivateLeaf(ref, "/exclusive/"+leaf) || pathHasPrivateLeaf(ref, "/maclaw-exclusive-sandbox/"+leaf)
}

func pathHasPrivateLeaf(ref, marker string) bool {
	for start := 0; start < len(ref); {
		idx := strings.Index(ref[start:], marker)
		if idx < 0 {
			return false
		}
		idx += start
		end := idx + len(marker)
		if end == len(ref) || ref[end] == '/' {
			return true
		}
		start = idx + 1
	}
	return false
}

func workRootRelative(root, child string) (string, bool) {
	r := normalizeCodingRuntimeRef(root)
	c := normalizeCodingRuntimeRef(child)
	if r == "" || c == "" {
		return "", false
	}
	if c == r {
		return ".", true
	}
	if strings.HasPrefix(c, r+"/") {
		return c[len(r)+1:], true
	}
	return "", false
}

// exclusiveWorkRoot returns dir when this session may use it. When an earlier
// live session already holds it, the result is a private checkout of that
// tree's HEAD, or this task's own workspace if that checkout cannot be made.
// The original spelling is kept when the directory is not detached: callers
// compare and log that string.
func (a *App) exclusiveWorkRoot(taskPath, dir string) string {
	if a == nil {
		return dir
	}
	original := dir
	taskPath = normalizeProjectSessionPath(taskPath)
	dir = normalizeProjectSessionPath(dir)
	if taskPath == "" || dir == "" || !a.isManagedRecentTaskWorkspacePath(taskPath) {
		return original
	}
	// A path inside this task's checkout is that snapshot. Use it only while
	// the occupation that created it is still the one holding the tree.
	// After the holder leaves and starts again, the old directory is the
	// previous HEAD.
	if own := a.exclusiveCheckoutDir(taskPath); pathInsideWorkRoot(own, dir) {
		if a.exclusiveCheckoutIsCurrent(taskPath, dir) {
			return original
		}
		claimed, ok := a.claimedWorkRoot(taskPath)
		if !ok || claimed == "" || pathInsideWorkRoot(own, claimed) {
			return original
		}
		fresh := a.exclusiveWorkRoot(taskPath, claimed)
		rel, inside := workRootRelative(own, dir)
		if !inside || rel == "." || fresh == "" {
			return fresh
		}
		return filepath.Join(fresh, filepath.FromSlash(rel))
	}
	holder, entered := a.earlierLiveWorkRootHolder(taskPath, dir)
	if holder == "" {
		return original
	}
	key := normalizeRecentTaskPathKey(taskPath)
	unlock := lockExclusiveCheckout(key)
	defer unlock()
	if reused, ok := reusableExclusiveCheckout(key, dir, holder, entered); ok {
		return reused
	}
	// A tool path outside the snapshot must not replace it. Rebuilding the
	// checkout would take the holder's commits from after this occupation began.
	if exclusiveCheckoutKeepsSnapshot(key, dir, holder, entered) {
		fallback := a.privateFallbackWorkRoot(taskPath, dir)
		logKey := taskPath + "\x00" + normalizeCodingRuntimeRef(dir) + "\x00" + holder + "\x00" + strconv.FormatInt(entered, 10)
		if _, loaded := exclusiveFallbackLogged.LoadOrStore(logKey, true); !loaded {
			log.Printf("[work-root] task %q keeps its checkout; %q is outside that snapshot, exec=%q", taskPath, dir, fallback)
		}
		return fallback
	}
	checkout, created, err := a.detachedWorkRoot(taskPath, dir, holder, entered)
	if err != nil {
		fallback := a.privateFallbackWorkRoot(taskPath, dir)
		if stableExclusiveFailure(err) && !exclusiveCheckoutExists(key, holder, entered) {
			exclusiveCheckouts.Store(key, exclusiveCheckout{
				requested: normalizeCodingRuntimeRef(dir),
				result:    fallback,
				holder:    holder,
				entered:   entered,
				fallback:  true,
			})
		}
		logKey := taskPath + "\x00" + normalizeCodingRuntimeRef(dir) + "\x00" + holder + "\x00" + strconv.FormatInt(entered, 10)
		if _, loaded := exclusiveFallbackLogged.LoadOrStore(logKey, true); !loaded {
			log.Printf("[work-root] task %q does not join %q held by %q (%v); exec=%q", taskPath, dir, holder, err, fallback)
		}
		return fallback
	}
	if created {
		log.Printf("[work-root] task %q does not join %q held by %q; exec=%q", taskPath, dir, holder, checkout)
	}
	return checkout
}

func stableExclusiveFailure(err error) bool {
	return errors.Is(err, errExclusiveNotGit) || errors.Is(err, errExclusiveNoCommits) || errors.Is(err, errExclusiveGitContainsStore)
}

func reusableExclusiveCheckout(key, dir, holder string, entered int64) (string, bool) {
	cached, ok := exclusiveCheckouts.Load(key)
	if !ok {
		return "", false
	}
	got, ok := cached.(exclusiveCheckout)
	if !ok || normalizeProjectSessionPath(got.holder) != holder || got.entered != entered {
		return "", false
	}
	if got.fallback {
		if got.requested == normalizeCodingRuntimeRef(dir) && got.result != "" {
			return got.result, true
		}
		return "", false
	}
	if got.path == "" {
		return "", false
	}
	info, err := os.Stat(got.path)
	if err != nil || !info.IsDir() {
		return "", false
	}
	if got.requested == normalizeCodingRuntimeRef(dir) && got.result != "" {
		return got.result, true
	}
	rel, inside := workRootRelative(got.gitRoot, dir)
	if !inside {
		return "", false
	}
	if rel == "." {
		return got.path, true
	}
	return filepath.Join(got.path, filepath.FromSlash(rel)), true
}

// exclusiveCheckoutKeepsSnapshot reports that this occupation already has a
// checkout and dir is not inside it. The checkout stays at the HEAD it was
// given.
func exclusiveCheckoutKeepsSnapshot(key, dir, holder string, entered int64) bool {
	got, ok := exclusiveCheckoutForOccupation(key, holder, entered)
	if !ok || got.gitRoot == "" {
		return false
	}
	_, inside := workRootRelative(got.gitRoot, dir)
	return !inside
}

// exclusiveCheckoutIsCurrent reports that dir is inside the checkout made
// for the holder who is still in this run.
func (a *App) exclusiveCheckoutIsCurrent(taskPath, dir string) bool {
	key := normalizeRecentTaskPathKey(taskPath)
	cached, ok := exclusiveCheckouts.Load(key)
	if !ok {
		return false
	}
	got, ok := cached.(exclusiveCheckout)
	if !ok || got.fallback || got.path == "" || got.gitRoot == "" || !pathInsideWorkRoot(got.path, dir) {
		return false
	}
	holder, entered := a.earlierLiveWorkRootHolder(taskPath, got.gitRoot)
	if holder == "" || entered != got.entered || normalizeProjectSessionPath(holder) != normalizeProjectSessionPath(got.holder) {
		return false
	}
	info, err := os.Stat(got.path)
	return err == nil && info.IsDir()
}

func exclusiveCheckoutExists(key, holder string, entered int64) bool {
	_, ok := exclusiveCheckoutForOccupation(key, holder, entered)
	return ok
}

func exclusiveCheckoutForOccupation(key, holder string, entered int64) (exclusiveCheckout, bool) {
	cached, ok := exclusiveCheckouts.Load(key)
	if !ok {
		return exclusiveCheckout{}, false
	}
	got, ok := cached.(exclusiveCheckout)
	if !ok || got.fallback || got.path == "" || got.entered != entered || normalizeProjectSessionPath(got.holder) != holder {
		return exclusiveCheckout{}, false
	}
	info, err := os.Stat(got.path)
	if err != nil || !info.IsDir() {
		return exclusiveCheckout{}, false
	}
	return got, true
}

func (a *App) privateFallbackWorkRoot(taskPath, occupied string) string {
	sandbox := filepath.Join(taskPath, "workspace")
	if pathInsideWorkRoot(occupied, sandbox) {
		sandbox = filepath.Join(os.TempDir(), "maclaw-exclusive-sandbox", exclusiveTaskLeaf(taskPath))
	}
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		log.Printf("[work-root] private workspace mkdir failed task=%q path=%q err=%v", taskPath, sandbox, err)
	}
	return sandbox
}

func (a *App) detachedWorkRoot(taskPath, requested, holder string, entered int64) (string, bool, error) {
	gitRoot, ok := remote.DetectGitWorkspaceRoot(directoryForGitProbe(requested))
	if !ok {
		return "", false, errExclusiveNotGit
	}
	if !remote.GitRepoHasCommits(gitRoot) {
		return "", false, errExclusiveNoCommits
	}
	// A parent repository that contains the task store (a home directory that
	// happens to be a git repo) is not the project this task claimed.
	if pathInsideWorkRoot(gitRoot, a.GetDataDir()) {
		return "", false, fmt.Errorf("%w: %s", errExclusiveGitContainsStore, gitRoot)
	}
	root, created, err := a.ensureDetachedWorktree(taskPath, gitRoot, holder, entered)
	if err != nil {
		return "", false, err
	}
	result := root
	rel, inside := workRootRelative(gitRoot, requested)
	if inside && rel != "." {
		result = filepath.Join(root, filepath.FromSlash(rel))
	}
	exclusiveCheckouts.Store(normalizeRecentTaskPathKey(taskPath), exclusiveCheckout{
		gitRoot:   gitRoot,
		path:      root,
		requested: normalizeCodingRuntimeRef(requested),
		result:    result,
		holder:    holder,
		entered:   entered,
	})
	return result, created, nil
}

func (a *App) exclusiveCheckoutDir(taskPath string) string {
	taskPath = normalizeProjectSessionPath(taskPath)
	if a == nil || taskPath == "" {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "exclusive", exclusiveTaskLeaf(taskPath))
}

// directoryForGitProbe is the directory git can be asked about. A file path,
// or a directory that does not exist yet, is probed from its parent so the
// snapshot is the repository that contains it.
func directoryForGitProbe(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	cur := path
	for {
		info, err := os.Stat(cur)
		if err == nil {
			if info.IsDir() {
				return cur
			}
			return filepath.Dir(cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path
		}
		cur = parent
	}
}

func exclusiveTaskLeaf(taskPath string) string {
	taskPath = normalizeProjectSessionPath(taskPath)
	base := filepath.Base(taskPath)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "task"
	}
	// The folder name is unique per task path. Two tasks can share a base name.
	sum := sha256.Sum256([]byte(normalizeRecentTaskPathKey(taskPath)))
	return base + "-" + hex.EncodeToString(sum[:4])
}

func (a *App) ensureDetachedWorktree(taskPath, gitRoot, holder string, entered int64) (string, bool, error) {
	taskPath = normalizeProjectSessionPath(taskPath)
	gitRoot = normalizeProjectSessionPath(gitRoot)
	checkout := a.exclusiveCheckoutDir(taskPath)
	if checkout == "" || pathInsideWorkRoot(gitRoot, checkout) {
		return "", false, fmt.Errorf("checkout would sit inside %s", gitRoot)
	}
	key := normalizeRecentTaskPathKey(taskPath)
	source := normalizeCodingRuntimeRef(gitRoot)
	holder = normalizeProjectSessionPath(holder)

	if cached, ok := exclusiveCheckouts.Load(key); ok {
		if got, ok := cached.(exclusiveCheckout); ok &&
			got.path != "" &&
			normalizeCodingRuntimeRef(got.gitRoot) == source &&
			normalizeProjectSessionPath(got.holder) == holder &&
			got.entered == entered {
			if info, err := os.Stat(got.path); err == nil && info.IsDir() {
				return got.path, false, nil
			}
		} else if got, ok := cached.(exclusiveCheckout); ok && got.path != "" {
			// A new occupation, or a different repository, snapshots HEAD again.
			// Remove through the repository that registered the worktree.
			removeWorktree(got.gitRoot, got.path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
		return "", false, err
	}
	if _, err := os.Stat(checkout); err == nil {
		removeWorktree(gitRoot, checkout)
		if _, statErr := os.Stat(checkout); statErr == nil {
			return "", false, fmt.Errorf("exclusive path %s is already in use", checkout)
		}
	}
	if err := remote.RunGit(gitRoot, "worktree", "add", "--detach", checkout, "HEAD"); err != nil {
		removeWorktree(gitRoot, checkout)
		if err2 := remote.RunGit(gitRoot, "worktree", "add", "--detach", checkout, "HEAD"); err2 != nil {
			return "", false, err2
		}
	}
	return checkout, true, nil
}

func removeWorktree(gitRoot, checkout string) {
	if strings.TrimSpace(checkout) == "" || pathInsideWorkRoot(gitRoot, checkout) {
		return
	}
	_ = remote.RunGit(gitRoot, "worktree", "remove", "--force", checkout)
	_ = os.RemoveAll(checkout)
	if strings.TrimSpace(gitRoot) != "" {
		_ = remote.RunGit(gitRoot, "worktree", "prune")
	}
}
