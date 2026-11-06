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
	fileCompanionFolderCap      = 40
	fileCompanionFolderNameCap  = 40
	fileCompanionFolderIDPrefix = "fld_"
)

var fileCompanionCatalogMu sync.Mutex

// FileCompanionFolder is one single-level category in 文档管理.
type FileCompanionFolder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FileCompanionCatalog is the folder tree and the document placements.
// Placements use item:<library id> and path:<library path key>. Rev moves on
// every write so a stale list cannot cover a newer edit.
type FileCompanionCatalog struct {
	Rev        int64                 `json:"rev"`
	Folders    []FileCompanionFolder `json:"folders"`
	Placements map[string]string     `json:"placements"`
}

func (a *App) fileCompanionCatalogPath() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "library-folders.json")
}

func (a *App) fileCompanionLibraryItems() []FileCompanionLibraryItem {
	if a == nil {
		return nil
	}
	fileCompanionLibraryMu.Lock()
	defer fileCompanionLibraryMu.Unlock()
	items, ok := a.readFileCompanionLibraryLocked()
	if !ok || items == nil {
		return []FileCompanionLibraryItem{}
	}
	return items
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
		if folder.ID == "" || folder.Name == "" || seen[folder.ID] {
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
	placements := map[string]string{}
	for key, folderID := range cat.Placements {
		key = strings.TrimSpace(key)
		folderID = strings.TrimSpace(folderID)
		if key == "" || !seen[folderID] {
			continue
		}
		if !strings.HasPrefix(key, "item:") && !strings.HasPrefix(key, "path:") {
			continue
		}
		placements[key] = folderID
	}
	cat.Folders = folders
	cat.Placements = placements
	return cat
}

// pruneFileCompanionCatalog drops placements whose document is gone and
// copies a path placement onto the library row that now owns that file.
// An item placement wins over a path placement. The folder value is removed
// with the folder, so a deleted category cannot come back through a path.
func pruneFileCompanionCatalog(cat *FileCompanionCatalog, items []FileCompanionLibraryItem) bool {
	before := len(cat.Folders) + len(cat.Placements)
	next := normalizeFileCompanionCatalog(*cat)
	known := map[string]bool{}
	for _, folder := range next.Folders {
		known[folder.ID] = true
	}
	byItem := map[string]string{}
	for key, folderID := range next.Placements {
		if !known[folderID] || !strings.HasPrefix(key, "item:") {
			continue
		}
		id := strings.TrimPrefix(key, "item:")
		if id != "" {
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
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		if _, set := byItem[item.ID]; set {
			continue
		}
		if key := fileCompanionFolderPathKey(item.Path); key != "" {
			if folderID := pathFolder[key]; folderID != "" {
				byItem[item.ID] = folderID
			}
		}
	}
	placements := map[string]string{}
	conflict := map[string]bool{}
	for _, item := range items {
		folderID := byItem[item.ID]
		if strings.TrimSpace(item.ID) == "" || folderID == "" || !known[folderID] {
			continue
		}
		placements[fileCompanionFolderItemKey(item.ID)] = folderID
		key := fileCompanionFolderPathKey(item.Path)
		if key == "" || conflict[key] {
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
		return FileCompanionCatalog{}, err
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
	items := a.fileCompanionLibraryItems()
	fileCompanionCatalogMu.Lock()
	defer fileCompanionCatalogMu.Unlock()
	cat, err := a.readFileCompanionCatalogLocked()
	if err != nil {
		return empty, err
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
	items := a.fileCompanionLibraryItems()
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
	items := a.fileCompanionLibraryItems()
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
		if folderID := cat.Placements[key]; folderID != "" {
			return folderID
		}
	}
	if key := fileCompanionFolderPathKey(item.Path); key != "" {
		return cat.Placements[key]
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

func dropFileCompanionFolder(cat *FileCompanionCatalog, id string) {
	kept := make([]FileCompanionFolder, 0, len(cat.Folders))
	for _, folder := range cat.Folders {
		if folder.ID != id {
			kept = append(kept, folder)
		}
	}
	cat.Folders = kept
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
	items := a.fileCompanionLibraryItems()
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
	var removeErr error
	if mode == "remove" && len(members) > 0 {
		for _, item := range members {
			if err := a.deleteFileCompanionFolderMember(item); err != nil && removeErr == nil {
				removeErr = err
			}
		}
		items = a.fileCompanionLibraryItems()
	}
	saved, err := a.mutateFileCompanionCatalog(items, func(cat *FileCompanionCatalog) error {
		if !fileCompanionCatalogHasFolder(*cat, id) {
			return errors.New("没有这个文件夹")
		}
		still := fileCompanionFolderMembers(*cat, items, id)
		if len(still) > 0 && mode == "merge" {
			if targetID == "" || targetID == id || !fileCompanionCatalogHasFolder(*cat, targetID) {
				return errors.New("请选择要合并到的文件夹")
			}
			for _, item := range still {
				fileCompanionPlaceItem(cat, item, targetID)
			}
			dropFileCompanionFolder(cat, id)
			return nil
		}
		if len(still) > 0 && mode != "remove" {
			return errors.New("文件夹里还有文档")
		}
		if len(still) == 0 {
			dropFileCompanionFolder(cat, id)
		}
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
	if folderID == "" {
		if itemKey != "" {
			delete(cat.Placements, itemKey)
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
		return a.DeleteFileCompanionShare(item.Token)
	}
	_, err := a.DeleteFileCompanionProject(item.ID, item.Path)
	return err
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
	items := a.fileCompanionLibraryItems()
	return a.mutateFileCompanionCatalog(items, func(cat *FileCompanionCatalog) error {
		if folderID != "" && !fileCompanionCatalogHasFolder(*cat, folderID) {
			return errors.New("没有这个文件夹")
		}
		item := FileCompanionLibraryItem{ID: itemID, Path: path}
		for _, current := range items {
			if itemID != "" && current.ID == itemID {
				item = current
				break
			}
			if path != "" && fileCompanionSameLibraryPath(current.Path, path) {
				item = current
				break
			}
		}
		if strings.TrimSpace(item.ID) == "" {
			item.ID = itemID
		}
		if strings.TrimSpace(item.Path) == "" {
			item.Path = path
		}
		fileCompanionPlaceItem(cat, item, folderID)
		return nil
	})
}
