package guiapp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Companion library folders are one level of local categories for 文档管理.
// The file sits beside library.json, so a cloud refresh of the library does
// not drop a folder or which document is in it.
//
// A folder that still holds documents is not deleted until the caller says
// how: mode "merge" moves those documents into another folder, and mode
// "remove" drops them from 文档管理. The source file on disk stays either way.

const (
	fileCompanionFolderCap           = 40
	fileCompanionFolderNameCap       = 40
	fileCompanionFolderIDPrefix      = "fld_"
	fileCompanionFolderUncategorized = "none"
)

var fileCompanionCatalogMu sync.Mutex

// FileCompanionFolder is one single-level category in 文档管理.
type FileCompanionFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FileCompanionCatalog is the folder tree and the document placements.
// Placements use item:<library id> and path:<library path key>.
// item:<id> = "none" means that row was moved to 未分类 and must not inherit
// the path key. Rev moves on every write so a stale list cannot cover a newer edit.
type FileCompanionCatalog struct {
	Rev        int64                 `json:"rev"`
	Folders    []FileCompanionFolder `json:"folders"`
	Placements map[string]string     `json:"placements"`
	// DefaultID is the folder a browser add offers first. Empty means 未分类.
	// It is not a placement: a new document stays uncategorized until the
	// user confirms the dialog.
	DefaultID string `json:"defaultId,omitempty"`
}

func (a *App) fileCompanionCatalogPath() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "library-folders.json")
}

func (a *App) fileCompanionLibraryItems() ([]FileCompanionLibraryItem, error) {
	if a == nil {
		return nil, errors.New("app unavailable")
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok {
		return nil, errors.New("文档列表无法读取")
	}
	if items == nil {
		return []FileCompanionLibraryItem{}, nil
	}
	return items, nil
}

func fileCompanionFolderItemKey(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return "item:" + id
}

func fileCompanionFolderPathKey(path string) string {
	key := fileCompanionLibraryPathKey(path)
	if key == "" {
		return ""
	}
	return "path:" + key
}

func newFileCompanionFolderID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return fileCompanionFolderIDPrefix + hex.EncodeToString(buf[:]), nil
}

func fileCompanionFolderNameError(name string, folders []FileCompanionFolder, ignoreID string) (string, string) {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return "", "请填写文件夹名称"
	}
	if utf8.RuneCountInString(cleaned) > fileCompanionFolderNameCap {
		return "", "文件夹名称过长"
	}
	if strings.ContainsAny(cleaned, "\r\n\x00") {
		return "", "请填写文件夹名称"
	}
	if fileCompanionFolderNameReserved(cleaned) {
		return "", "全部和未分类不能用作文件夹名称"
	}
	for _, folder := range folders {
		if folder.ID == ignoreID {
			continue
		}
		if strings.EqualFold(folder.Name, cleaned) {
			return "", "已有同名文件夹"
		}
	}
	return cleaned, ""
}

// fileCompanionFolderNameReserved reports a name the folder tree already uses
// for 全部 or 未分类. The English and Mongolian labels are reserved too, so a
// folder cannot sit in the rail with the same caption as those two rows.
func fileCompanionFolderNameReserved(name string) bool {
	switch strings.ToLower(name) {
	case "全部", "未分类", "all", "uncategorized", "бүгд", "ангилаагүй":
		return true
	default:
		return false
	}
}

func (a *App) readFileCompanionCatalogLocked() (FileCompanionCatalog, error) {
	empty := FileCompanionCatalog{Folders: []FileCompanionFolder{}, Placements: map[string]string{}}
	path := a.fileCompanionCatalogPath()
	if path == "" {
		return empty, errors.New("app unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, nil
		}
		return empty, errors.New("文档类别无法读取")
	}
	var stored FileCompanionCatalog
	if json.Unmarshal(raw, &stored) != nil {
		return empty, errors.New("文档类别无法读取")
	}
	return normalizeFileCompanionCatalog(stored), nil
}

func normalizeFileCompanionCatalog(cat FileCompanionCatalog) FileCompanionCatalog {
	if cat.Rev < 0 {
		cat.Rev = 0
	}
	seen := map[string]bool{}
	folders := make([]FileCompanionFolder, 0, len(cat.Folders))
	for _, folder := range cat.Folders {
		folder.ID = strings.TrimSpace(folder.ID)
		folder.Name = strings.TrimSpace(folder.Name)
		// "none" is the uncategorized sentinel, never a folder id.
		if folder.ID == "" || folder.ID == fileCompanionFolderUncategorized || folder.Name == "" || seen[folder.ID] {
			continue
		}
		if utf8.RuneCountInString(folder.Name) > fileCompanionFolderNameCap {
			folder.Name = string([]rune(folder.Name)[:fileCompanionFolderNameCap])
		}
		seen[folder.ID] = true
		folders = append(folders, folder)
		if len(folders) >= fileCompanionFolderCap {
			break
		}
	}
	defaultID := strings.TrimSpace(cat.DefaultID)
	if defaultID == fileCompanionFolderUncategorized || !seen[defaultID] {
		defaultID = ""
	}
	placements := map[string]string{}
	for key, folderID := range cat.Placements {
		key = strings.TrimSpace(key)
		folderID = strings.TrimSpace(folderID)
		if key == "" || folderID == "" {
			continue
		}
		// An explicit 未分类 stays on the row. A path key is never "none":
		// prune republishes it from a sibling that is still in a folder.
		if strings.HasPrefix(key, "item:") && folderID == fileCompanionFolderUncategorized {
			if strings.TrimPrefix(key, "item:") == "" {
				continue
			}
			placements[key] = fileCompanionFolderUncategorized
			continue
		}
		if !seen[folderID] {
			continue
		}
		if !strings.HasPrefix(key, "item:") && !strings.HasPrefix(key, "path:") {
			continue
		}
		placements[key] = folderID
	}
	cat.Folders = folders
	cat.Placements = placements
	cat.DefaultID = defaultID
	return cat
}

// pruneFileCompanionCatalog drops placements whose document is gone and
// copies a path placement onto a library row that has no item placement.
// An item placement wins. item:<id> = "none" is an explicit 未分类 and does
// not inherit the path. The folder value is removed with the folder, so a
// deleted category cannot come back through a path.
func pruneFileCompanionCatalog(cat *FileCompanionCatalog, items []FileCompanionLibraryItem) bool {
	before := len(cat.Folders) + len(cat.Placements)
	next := normalizeFileCompanionCatalog(*cat)
	known := map[string]bool{}
	for _, folder := range next.Folders {
		known[folder.ID] = true
	}
	byItem := map[string]string{}
	cleared := map[string]bool{}
	for key, folderID := range next.Placements {
		if !strings.HasPrefix(key, "item:") {
			continue
		}
		id := strings.TrimPrefix(key, "item:")
		if id == "" {
			continue
		}
		if folderID == fileCompanionFolderUncategorized {
			cleared[id] = true
			continue
		}
		if known[folderID] {
			byItem[id] = folderID
		}
	}
	pathFolder := map[string]string{}
	for key, folderID := range next.Placements {
		if !known[folderID] || !strings.HasPrefix(key, "path:") {
			continue
		}
		pathFolder[key] = folderID
	}
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" || cleared[id] {
			continue
		}
		if _, set := byItem[id]; set {
			continue
		}
		if key := fileCompanionFolderPathKey(item.Path); key != "" {
			if folderID := pathFolder[key]; folderID != "" {
				byItem[id] = folderID
			}
		}
	}
	// A path key is published when the placed rows of that file agree.
	// An explicit 未分类 does not inherit it and does not hide it, so a
	// replaced id of the placed row keeps the folder. A row with no
	// placement at all still hides the path key: a paper restored beside
	// a failed delete must not be pulled back into the folder.
	barePath := map[string]bool{}
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" || cleared[id] {
			continue
		}
		key := fileCompanionFolderPathKey(item.Path)
		if key == "" {
			continue
		}
		if _, placed := byItem[id]; !placed {
			barePath[key] = true
		}
	}
	placements := map[string]string{}
	conflict := map[string]bool{}
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if cleared[id] {
			placements[fileCompanionFolderItemKey(id)] = fileCompanionFolderUncategorized
			continue
		}
		folderID := byItem[id]
		if folderID == "" || !known[folderID] {
			continue
		}
		placements[fileCompanionFolderItemKey(id)] = folderID
		key := fileCompanionFolderPathKey(item.Path)
		if key == "" || conflict[key] || barePath[key] {
			continue
		}
		if prev, ok := placements[key]; ok && prev != folderID {
			delete(placements, key)
			conflict[key] = true
			continue
		}
		placements[key] = folderID
	}
	changed := before != len(next.Folders)+len(placements) || len(next.Placements) != len(placements)
	if !changed {
		for key, folderID := range placements {
			if next.Placements[key] != folderID {
				changed = true
				break
			}
		}
	}
	cat.Folders = next.Folders
	cat.Placements = placements
	if cat.Rev < next.Rev {
		cat.Rev = next.Rev
	}
	return changed
}

func (a *App) writeFileCompanionCatalogLocked(cat FileCompanionCatalog) (FileCompanionCatalog, error) {
	path := a.fileCompanionCatalogPath()
	if path == "" {
		return FileCompanionCatalog{}, errors.New("app unavailable")
	}
	cat = normalizeFileCompanionCatalog(cat)
	cat.Rev++
	body, err := json.Marshal(cat)
	if err != nil {
		return FileCompanionCatalog{}, errors.New("文档类别无法保存")
	}
	if err := fileCompanionWriteAtomicFile(path, body, 0o600); err != nil {
		return FileCompanionCatalog{}, errors.New("文档类别无法保存")
	}
	return cat, nil
}

// ListFileCompanionCatalog returns the single-level folders and placements.
func (a *App) ListFileCompanionCatalog() (FileCompanionCatalog, error) {
	empty := FileCompanionCatalog{Folders: []FileCompanionFolder{}, Placements: map[string]string{}}
	if a == nil {
		return empty, errors.New("app unavailable")
	}
	items, itemsErr := a.fileCompanionLibraryItems()
	fileCompanionCatalogMu.Lock()
	defer fileCompanionCatalogMu.Unlock()
	cat, err := a.readFileCompanionCatalogLocked()
	if err != nil {
		return empty, err
	}
	// A failed library read is not an empty library. Pruning against it would
	// drop every placement and save that.
	if itemsErr != nil {
		return cat, nil
	}
	if pruneFileCompanionCatalog(&cat, items) {
		return a.writeFileCompanionCatalogLocked(cat)
	}
	return cat, nil
}

func (a *App) mutateFileCompanionCatalog(items []FileCompanionLibraryItem, fn func(*FileCompanionCatalog) error) (FileCompanionCatalog, error) {
	empty := FileCompanionCatalog{Folders: []FileCompanionFolder{}, Placements: map[string]string{}}
	if a == nil {
		return empty, errors.New("app unavailable")
	}
	fileCompanionCatalogMu.Lock()
	defer fileCompanionCatalogMu.Unlock()
	cat, err := a.readFileCompanionCatalogLocked()
	if err != nil {
		return empty, err
	}
	pruneFileCompanionCatalog(&cat, items)
	if err := fn(&cat); err != nil {
		return empty, err
	}
	pruneFileCompanionCatalog(&cat, items)
	return a.writeFileCompanionCatalogLocked(cat)
}

// CreateFileCompanionFolder adds one folder at the single level.
func (a *App) CreateFileCompanionFolder(name string) (FileCompanionCatalog, error) {
	items, err := a.fileCompanionLibraryItems()
	if err != nil {
		return FileCompanionCatalog{}, err
	}
	return a.mutateFileCompanionCatalog(items, func(cat *FileCompanionCatalog) error {
		if len(cat.Folders) >= fileCompanionFolderCap {
			return errors.New("文件夹数量已达上限")
		}
		cleaned, errText := fileCompanionFolderNameError(name, cat.Folders, "")
		if errText != "" {
			return errors.New(errText)
		}
		id, err := newFileCompanionFolderID()
		if err != nil {
			return errors.New("文档类别无法保存")
		}
		cat.Folders = append(cat.Folders, FileCompanionFolder{ID: id, Name: cleaned})
		return nil
	})
}

// RenameFileCompanionFolder changes one folder's name. The id stays.
func (a *App) RenameFileCompanionFolder(id, name string) (FileCompanionCatalog, error) {
	id = strings.TrimSpace(id)
	items, err := a.fileCompanionLibraryItems()
	if err != nil {
		return FileCompanionCatalog{}, err
	}
	return a.mutateFileCompanionCatalog(items, func(cat *FileCompanionCatalog) error {
		cleaned, errText := fileCompanionFolderNameError(name, cat.Folders, id)
		if errText != "" {
			return errors.New(errText)
		}
		for i := range cat.Folders {
			if cat.Folders[i].ID != id {
				continue
			}
			cat.Folders[i].Name = cleaned
			return nil
		}
		return errors.New("没有这个文件夹")
	})
}

func fileCompanionFolderMembers(cat FileCompanionCatalog, items []FileCompanionLibraryItem, folderID string) []FileCompanionLibraryItem {
	out := make([]FileCompanionLibraryItem, 0)
	for _, item := range items {
		if fileCompanionItemFolder(cat, item) == folderID {
			out = append(out, item)
		}
	}
	return out
}

func fileCompanionItemFolder(cat FileCompanionCatalog, item FileCompanionLibraryItem) string {
	if key := fileCompanionFolderItemKey(item.ID); key != "" {
		switch folderID := strings.TrimSpace(cat.Placements[key]); folderID {
		case "":
		case fileCompanionFolderUncategorized:
			return ""
		default:
			return folderID
		}
	}
	if key := fileCompanionFolderPathKey(item.Path); key != "" {
		folderID := strings.TrimSpace(cat.Placements[key])
		if folderID == fileCompanionFolderUncategorized {
			return ""
		}
		return folderID
	}
	return ""
}

func fileCompanionCatalogHasFolder(cat FileCompanionCatalog, id string) bool {
	for _, folder := range cat.Folders {
		if folder.ID == id {
			return true
		}
	}
	return false
}

// fileCompanionKeepFailedFolderMembers leaves the folder in place and keeps
// a placement only for rows whose delete failed. Every other placement for
// this folder is dropped, including a path key that would otherwise pull a
// restored local row back into the folder on the next list.
func fileCompanionKeepFailedFolderMembers(cat *FileCompanionCatalog, items, failed []FileCompanionLibraryItem, folderID string) {
	if cat.Placements == nil {
		cat.Placements = map[string]string{}
	}
	for key, id := range cat.Placements {
		if id == folderID {
			delete(cat.Placements, key)
		}
	}
	present := map[string]FileCompanionLibraryItem{}
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			present[id] = item
		}
	}
	for _, item := range failed {
		current, ok := present[strings.TrimSpace(item.ID)]
		if !ok {
			continue
		}
		if key := fileCompanionFolderItemKey(current.ID); key != "" {
			cat.Placements[key] = folderID
		}
	}
}

func dropFileCompanionFolder(cat *FileCompanionCatalog, id string) {
	kept := make([]FileCompanionFolder, 0, len(cat.Folders))
	for _, folder := range cat.Folders {
		if folder.ID != id {
			kept = append(kept, folder)
		}
	}
	cat.Folders = kept
	if cat.DefaultID == id {
		cat.DefaultID = ""
	}
	for key, folderID := range cat.Placements {
		if folderID == id {
			delete(cat.Placements, key)
		}
	}
}

// DeleteFileCompanionFolder removes one folder.
// An empty folder deletes directly. A folder that still holds documents
// requires mode "merge" plus the destination folder, or mode "remove".
// remove drops those documents from 文档管理 and leaves the source files.
func (a *App) DeleteFileCompanionFolder(id, mode, targetID string) (FileCompanionCatalog, error) {
	empty := FileCompanionCatalog{Folders: []FileCompanionFolder{}, Placements: map[string]string{}}
	if a == nil {
		return empty, errors.New("app unavailable")
	}
	id = strings.TrimSpace(id)
	mode = strings.TrimSpace(mode)
	targetID = strings.TrimSpace(targetID)
	items, itemsErr := a.fileCompanionLibraryItems()
	if itemsErr != nil {
		return empty, itemsErr
	}
	fileCompanionCatalogMu.Lock()
	cat, err := a.readFileCompanionCatalogLocked()
	fileCompanionCatalogMu.Unlock()
	if err != nil {
		return empty, err
	}
	pruneFileCompanionCatalog(&cat, items)
	if !fileCompanionCatalogHasFolder(cat, id) {
		return empty, errors.New("没有这个文件夹")
	}
	members := fileCompanionFolderMembers(cat, items, id)
	if len(members) > 0 && mode != "merge" && mode != "remove" {
		return empty, errors.New("文件夹里还有文档")
	}
	if mode == "merge" && len(members) > 0 {
		if targetID == "" || targetID == id || !fileCompanionCatalogHasFolder(cat, targetID) {
			return empty, errors.New("请选择要合并到的文件夹")
		}
	}
	// failed keeps the original rows whose delete did not succeed. A share
	// cancel that puts the owner's file back uses a new local id, so that
	// restored row is not a failure and must not keep the folder alive.
	failed := make([]FileCompanionLibraryItem, 0)
	var removeErr error
	if mode == "remove" && len(members) > 0 {
		for _, item := range members {
			if err := a.deleteFileCompanionFolderMember(item); err != nil {
				if removeErr == nil {
					removeErr = err
				}
				failed = append(failed, item)
			}
		}
		var rereadErr error
		items, rereadErr = a.fileCompanionLibraryItems()
		if rereadErr != nil {
			if removeErr == nil {
				removeErr = rereadErr
			}
			return empty, removeErr
		}
	}
	saved, err := a.mutateFileCompanionCatalog(items, func(cat *FileCompanionCatalog) error {
		if !fileCompanionCatalogHasFolder(*cat, id) {
			return errors.New("没有这个文件夹")
		}
		if mode == "merge" {
			still := fileCompanionFolderMembers(*cat, items, id)
			if len(still) == 0 {
				dropFileCompanionFolder(cat, id)
				return nil
			}
			if targetID == "" || targetID == id || !fileCompanionCatalogHasFolder(*cat, targetID) {
				return errors.New("请选择要合并到的文件夹")
			}
			for _, item := range still {
				fileCompanionPlaceItem(cat, item, targetID)
			}
			dropFileCompanionFolder(cat, id)
			return nil
		}
		if mode == "remove" {
			if len(failed) == 0 {
				dropFileCompanionFolder(cat, id)
				return nil
			}
			fileCompanionKeepFailedFolderMembers(cat, items, failed, id)
			return nil
		}
		if len(fileCompanionFolderMembers(*cat, items, id)) > 0 {
			return errors.New("文件夹里还有文档")
		}
		dropFileCompanionFolder(cat, id)
		return nil
	})
	if err != nil {
		return empty, err
	}
	if removeErr != nil {
		return saved, removeErr
	}
	return saved, nil
}

func fileCompanionPlaceItem(cat *FileCompanionCatalog, item FileCompanionLibraryItem, folderID string) {
	if cat.Placements == nil {
		cat.Placements = map[string]string{}
	}
	itemKey := fileCompanionFolderItemKey(item.ID)
	pathKey := fileCompanionFolderPathKey(item.Path)
	// Record 未分类 on this row and drop the path key. prune republishes
	// the path key from a sibling that is still in a folder, without
	// pulling this row back.
	if folderID == "" {
		if itemKey != "" {
			cat.Placements[itemKey] = fileCompanionFolderUncategorized
		}
		if pathKey != "" {
			delete(cat.Placements, pathKey)
		}
		return
	}
	if itemKey != "" {
		cat.Placements[itemKey] = folderID
	}
	if pathKey != "" {
		cat.Placements[pathKey] = folderID
	}
}

// deleteFileCompanionFolderMember removes one library row the way 文档管理
// does. An owned live share is cancelled on the hub. Anything else uses the
// project delete, which keeps the source file. An owned paper whose share
// was cancelled comes back as a local row outside this folder.
func (a *App) deleteFileCompanionFolderMember(item FileCompanionLibraryItem) error {
	if item.Source == "cloud" && item.Mine && strings.TrimSpace(item.Token) != "" && !fileCompanionShareExpiredAt(item.ExpiresAt, time.Now().UTC()) {
		_, err := a.DeleteFileCompanionShare(item.Token)
		return err
	}
	_, err := a.DeleteFileCompanionProject(item.ID, item.Path)
	return err
}

// SetFileCompanionDefaultFolder remembers which folder the browser-add
// dialog offers first. An empty id or "none" means 未分类.
func (a *App) SetFileCompanionDefaultFolder(folderID string) (FileCompanionCatalog, error) {
	folderID = strings.TrimSpace(folderID)
	if folderID == fileCompanionFolderUncategorized {
		folderID = ""
	}
	items, err := a.fileCompanionLibraryItems()
	if err != nil {
		return FileCompanionCatalog{}, err
	}
	return a.mutateFileCompanionCatalog(items, func(cat *FileCompanionCatalog) error {
		if folderID != "" && !fileCompanionCatalogHasFolder(*cat, folderID) {
			return errors.New("没有这个文件夹")
		}
		cat.DefaultID = folderID
		return nil
	})
}

// MoveFileCompanionLibraryItem puts one document in a folder. An empty
// folderID returns it to 未分类.
func (a *App) MoveFileCompanionLibraryItem(itemID, path, folderID string) (FileCompanionCatalog, error) {
	itemID = strings.TrimSpace(itemID)
	path = strings.TrimSpace(path)
	folderID = strings.TrimSpace(folderID)
	if itemID == "" && path == "" {
		return FileCompanionCatalog{}, errors.New("没有找到这个项目")
	}
	items, err := a.fileCompanionLibraryItems()
	if err != nil {
		return FileCompanionCatalog{}, err
	}
	return a.mutateFileCompanionCatalog(items, func(cat *FileCompanionCatalog) error {
		if folderID != "" && !fileCompanionCatalogHasFolder(*cat, folderID) {
			return errors.New("没有这个文件夹")
		}
		// The id is the row the user moved. A sibling of the same file can sit
		// earlier in the library and share the path; taking that sibling would
		// record the move on the other card. The path is only the fallback
		// when this id is no longer in the library, and it names the row the
		// library list shows.
		item := fileCompanionCatalogTarget(items, itemID, path)
		// A path that is not a library row yet must not be stored. prune
		// rebuilds placements from the rows it can see, so a path key written
		// before rememberFileCompanionDocument is deleted by the next list.
		if strings.TrimSpace(item.ID) == "" {
			return errors.New("没有找到这个项目")
		}
		fileCompanionPlaceItem(cat, item, folderID)
		return nil
	})
}

// fileCompanionCatalogTarget is the row Move records. The id wins, including
// a row the list has hidden. The path is the document on screen. The library
// list drops a local row when a cloud project owns that file, and it drops a
// later local duplicate. An empty-id move from the category dialog changes
// that visible row. An explicit 未分类 on it does not inherit a path key that
// still belongs to a hidden sibling.
func fileCompanionCatalogTarget(items []FileCompanionLibraryItem, itemID, path string) FileCompanionLibraryItem {
	itemID = strings.TrimSpace(itemID)
	path = strings.TrimSpace(path)
	if itemID != "" {
		for _, current := range items {
			if current.ID == itemID {
				return current
			}
		}
	}
	if path == "" {
		return FileCompanionLibraryItem{}
	}
	visible, _ := foldFileCompanionPromotedLocals(items)
	for _, current := range visible {
		if fileCompanionSameLibraryPath(current.Path, path) {
			return current
		}
	}
	return FileCompanionLibraryItem{}
}

// FileCompanionFolderForPath is the folder an empty-id move of path would
// change. Empty means that visible row is uncategorized, or the path is not
// a row. An explicit 未分类 on the visible row does not inherit a hidden
// sibling's path key. This read does not write the catalog and does not
// apply the default.
func (a *App) FileCompanionFolderForPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if a == nil {
		return "", errors.New("app unavailable")
	}
	if path == "" {
		return "", nil
	}
	items, err := a.fileCompanionLibraryItems()
	if err != nil {
		return "", err
	}
	fileCompanionCatalogMu.Lock()
	defer fileCompanionCatalogMu.Unlock()
	cat, err := a.readFileCompanionCatalogLocked()
	if err != nil {
		return "", err
	}
	pruneFileCompanionCatalog(&cat, items)
	item := fileCompanionCatalogTarget(items, "", path)
	if strings.TrimSpace(item.ID) == "" {
		return "", nil
	}
	return fileCompanionItemFolder(cat, item), nil
}
