package guiapp

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

const (
	dataDirectoryWorkspaceSearchMaxWorkspaces = 256
	dataDirectoryWorkspaceSearchMaxEntries    = 8000
)

// DataDirectoryWorkspaceHit is one file inside a managed task sandbox
// ({dataDir}/tasks/<slug>/workspace) whose name or text matches header search.
type DataDirectoryWorkspaceHit struct {
	ID           string   `json:"id"`
	TaskName     string   `json:"task_name"`
	ProjectPath  string   `json:"project_path"`
	RelativePath string   `json:"relative_path"`
	Title        string   `json:"title"`
	Preview      string   `json:"preview"`
	Match        string   `json:"match"`
	Tags         []string `json:"tags,omitempty"`
}

type dataDirectoryWorkspaceRoot struct {
	Name        string
	ProjectPath string
	Workspace   string
	Tags        []string
}

// SearchDataDirectoryWorkspaces searches file names and UTF-8 text under each
// visible managed task's data-directory workspace folder. It does not read
// office documents, follow symlinks, or leave that sandbox.
func (a *App) SearchDataDirectoryWorkspaces(query string, limit int) []DataDirectoryWorkspaceHit {
	if a == nil {
		return nil
	}
	seq := a.headerDataDirSearchSeq.Add(1)
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	return searchDataDirectoryWorkspaces(a.dataDirectoryWorkspaceSearchRoots(), query, limit, func() bool {
		return a.headerDataDirSearchSeq.Load() == seq
	})
}

func searchDataDirectoryWorkspaces(roots []dataDirectoryWorkspaceRoot, query string, limit int, still func() bool) []DataDirectoryWorkspaceHit {
	query = strings.TrimSpace(query)
	if query == "" || len(roots) == 0 {
		return nil
	}
	limit = cloudWorkspaceContentSearchLimit(limit)
	queryLower := strings.ToLower(query)
	type listedFile struct {
		root dataDirectoryWorkspaceRoot
		rel  string
	}
	files := make([]listedFile, 0, 64)
	entriesLeft := dataDirectoryWorkspaceSearchMaxEntries
	for _, root := range roots {
		if entriesLeft <= 0 {
			break
		}
		listed, stopped := dataDirectoryWorkspaceFiles(root.Workspace, &entriesLeft, still)
		if stopped {
			return nil
		}
		for _, rel := range listed {
			files = append(files, listedFile{root: root, rel: rel})
		}
	}
	hits := make([]cloudWorkspaceScoredHit, 0, limit)
	matched := make(map[string]struct{}, len(files))
	for _, file := range files {
		score := cloudWorkspaceNameScore(file.rel, queryLower)
		if score == 0 {
			continue
		}
		key := file.root.ProjectPath + "\x00" + file.rel
		matched[key] = struct{}{}
		hits = append(hits, cloudWorkspaceScoredHit{
			hit:   dataDirectoryWorkspaceSearchHit(file.root, file.rel, "name", file.rel),
			score: score,
		})
	}
	// Every name score is above the content score, so a full page of name hits
	// cannot change if the bodies are read.
	if len(hits) < limit {
		contentFiles := 0
		contentBytes := 0
		for _, file := range files {
			if contentFiles >= cloudWorkspaceSearchMaxContentFiles || contentBytes >= cloudWorkspaceSearchMaxContentBytes {
				break
			}
			key := file.root.ProjectPath + "\x00" + file.rel
			if _, ok := matched[key]; ok {
				continue
			}
			if !cloudWorkspaceSearchTextName(path.Base(file.rel)) {
				continue
			}
			if !headerSearchStillCurrent(still) {
				return nil
			}
			text, n, ok := cloudWorkspaceSearchFileText(file.root.Workspace, file.rel)
			if n > 0 {
				contentFiles++
				contentBytes += n
			}
			if !ok {
				continue
			}
			snippet := cloudWorkspaceSearchSnippet(text, query)
			if snippet == "" {
				continue
			}
			hits = append(hits, cloudWorkspaceScoredHit{
				hit:   dataDirectoryWorkspaceSearchHit(file.root, file.rel, "content", snippet),
				score: 40,
			})
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if hits[i].hit.WorkspaceName != hits[j].hit.WorkspaceName {
			return hits[i].hit.WorkspaceName < hits[j].hit.WorkspaceName
		}
		return hits[i].hit.RelativePath < hits[j].hit.RelativePath
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]DataDirectoryWorkspaceHit, 0, len(hits))
	for _, item := range hits {
		out = append(out, dataDirectoryHitFromCloud(item.hit))
	}
	return out
}

func dataDirectoryWorkspaceSearchHit(root dataDirectoryWorkspaceRoot, rel, matchKind, detail string) CloudWorkspaceContentHit {
	name := strings.TrimSpace(root.Name)
	if name == "" {
		name = filepath.Base(root.ProjectPath)
	}
	return CloudWorkspaceContentHit{
		ID:            filepath.Base(root.ProjectPath) + "/" + rel,
		WorkspaceName: name,
		ProjectPath:   root.ProjectPath,
		RelativePath:  rel,
		Title:         path.Base(rel),
		Preview:       cloudWorkspaceSearchPreview(name, detail),
		Match:         matchKind,
		Tags:          append([]string(nil), root.Tags...),
	}
}

func dataDirectoryHitFromCloud(hit CloudWorkspaceContentHit) DataDirectoryWorkspaceHit {
	return DataDirectoryWorkspaceHit{
		ID:           hit.ID,
		TaskName:     hit.WorkspaceName,
		ProjectPath:  hit.ProjectPath,
		RelativePath: hit.RelativePath,
		Title:        hit.Title,
		Preview:      hit.Preview,
		Match:        hit.Match,
		Tags:         hit.Tags,
	}
}

var dataDirectoryWorkspaceSkipDirs = map[string]struct{}{
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
	"build":        {},
	"__pycache__":  {},
	"target":       {},
}

func dataDirectoryWorkspaceFiles(root string, budget *int, still func() bool) ([]string, bool) {
	if budget == nil || *budget <= 0 || strings.TrimSpace(root) == "" {
		return nil, false
	}
	stopped := false
	files := make([]string, 0, 32)
	_ = filepath.WalkDir(root, func(abs string, d fs.DirEntry, err error) error {
		if !headerSearchStillCurrent(still) {
			stopped = true
			return fs.SkipAll
		}
		if *budget <= 0 {
			return fs.SkipAll
		}
		if err != nil {
			if abs == root {
				return err
			}
			return nil
		}
		if abs == root {
			return nil
		}
		name := d.Name()
		if name == "" || strings.HasPrefix(name, ".") || d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if _, skip := dataDirectoryWorkspaceSkipDirs[strings.ToLower(name)]; skip {
				return fs.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, ok := cloudWorkspaceSearchRel(rel); !ok {
			return nil
		}
		files = append(files, rel)
		*budget--
		return nil
	})
	if stopped {
		return nil, true
	}
	return files, false
}

func (a *App) dataDirectoryWorkspaceSearchRoots() []dataDirectoryWorkspaceRoot {
	if a == nil {
		return nil
	}
	a.ensureMemoryStore()
	tasksRoot := filepath.Join(a.GetDataDir(), "tasks")
	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		return nil
	}
	type candidate struct {
		name string
		mod  int64
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		candidates = append(candidates, candidate{name: entry.Name(), mod: info.ModTime().UnixNano()})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].mod != candidates[j].mod {
			return candidates[i].mod > candidates[j].mod
		}
		return candidates[i].name < candidates[j].name
	})
	var pi *memory.ProjectIndex
	if a.memoryStore != nil {
		pi = a.memoryStore.ProjectIndex()
	}
	roots := make([]dataDirectoryWorkspaceRoot, 0, len(candidates))
	for _, candidate := range candidates {
		if len(roots) >= dataDirectoryWorkspaceSearchMaxWorkspaces {
			break
		}
		taskDir := normalizeProjectSessionPath(filepath.Join(tasksRoot, candidate.name))
		root, ok := a.dataDirectoryWorkspaceRoot(pi, taskDir)
		if !ok {
			continue
		}
		roots = append(roots, root)
	}
	return roots
}

func (a *App) dataDirectoryWorkspaceRoot(pi *memory.ProjectIndex, taskDir string) (dataDirectoryWorkspaceRoot, bool) {
	taskDir = normalizeProjectSessionPath(taskDir)
	tasksRoot := ""
	if a != nil {
		tasksRoot = normalizeProjectSessionPath(filepath.Join(a.GetDataDir(), "tasks"))
	}
	if taskDir == "" || tasksRoot == "" || normalizeProjectSessionPath(filepath.Dir(taskDir)) != tasksRoot {
		return dataDirectoryWorkspaceRoot{}, false
	}
	name := stripTaskDirTimestampSuffix(filepath.Base(taskDir))
	var tags []string
	if pi == nil {
		return dataDirectoryWorkspaceRoot{}, false
	}
	if pi.IsHidden(taskDir) || pi.IsArchived(taskDir) {
		return dataDirectoryWorkspaceRoot{}, false
	}
	rec := pi.Get(taskDir)
	if rec == nil || !isTaskManagementRecord(*rec) || omittedFromTaskSidebar(*rec) {
		return dataDirectoryWorkspaceRoot{}, false
	}
	if custom := strings.TrimSpace(pi.CustomName(taskDir)); custom != "" {
		name = custom
	} else if strings.TrimSpace(rec.Name) != "" {
		name = strings.TrimSpace(rec.Name)
	}
	tags = append([]string(nil), rec.Tags...)
	workspace := filepath.Join(taskDir, "workspace")
	info, err := os.Lstat(workspace)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return dataDirectoryWorkspaceRoot{}, false
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return dataDirectoryWorkspaceRoot{}, false
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	return dataDirectoryWorkspaceRoot{
		Name:        name,
		ProjectPath: taskDir,
		Workspace:   abs,
		Tags:        tags,
	}, true
}

// GetDataDirectoryWorkspaceFilePreview reads one text file from a managed
// task sandbox. The path stays inside {task}/workspace.
func (a *App) GetDataDirectoryWorkspaceFilePreview(projectPath, relativePath string) (CodingWorkbenchFilePreview, error) {
	abs, rel, err := a.dataDirectoryWorkspaceOpenPath(projectPath, relativePath)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	content, truncated, err := readCodingWorkbenchBrowserTextFile(abs)
	if err != nil {
		return CodingWorkbenchFilePreview{}, err
	}
	return CodingWorkbenchFilePreview{
		Path:      rel,
		Content:   content,
		Language:  detectLanguageFromExt(abs),
		Truncated: truncated,
	}, nil
}

// OpenDataDirectoryWorkspaceFileLocally opens one sandbox file with the OS
// default application. The absolute path is not returned to the frontend.
func (a *App) OpenDataDirectoryWorkspaceFileLocally(projectPath, relativePath string) error {
	abs, _, err := a.dataDirectoryWorkspaceOpenPath(projectPath, relativePath)
	if err != nil {
		return err
	}
	return a.OpenFileOrShowInFolder(abs)
}

func (a *App) dataDirectoryWorkspaceOpenPath(projectPath, relativePath string) (string, string, error) {
	if a == nil {
		return "", "", fmt.Errorf("task workspace is unavailable")
	}
	a.ensureMemoryStore()
	var pi *memory.ProjectIndex
	if a.memoryStore != nil {
		pi = a.memoryStore.ProjectIndex()
	}
	root, ok := a.dataDirectoryWorkspaceRoot(pi, normalizeProjectSessionPath(projectPath))
	if !ok {
		return "", "", fmt.Errorf("task workspace is unavailable")
	}
	rel, err := cleanCodingWorkbenchBrowserPath(relativePath)
	if err != nil || rel == "" {
		if err == nil {
			err = fmt.Errorf("file path is required")
		}
		return "", "", err
	}
	abs, err := codingWorkbenchBrowserLocalPath(root.Workspace, rel)
	if err != nil {
		return "", "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("path is not a file")
	}
	return abs, rel, nil
}
