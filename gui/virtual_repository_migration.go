package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const remoteVirtualRepositoryMigrationCleanupTimeout = 15 * time.Second

// VirtualRepositoryRootMigrationRequest deliberately contains only a target
// root. The repository identity is resolved from the recent-repository index,
// so a client cannot move an arbitrary directory by posting a forged manifest.
type VirtualRepositoryRootMigrationRequest struct {
	RepositoryID    string `json:"repository_id"`
	DestinationRoot string `json:"destination_root"`
	Password        string `json:"password,omitempty"`
	TrustHostKey    bool   `json:"trust_host_key,omitempty"`
}

type VirtualRepositoryRootMigrationPreview struct {
	RepositoryID           string `json:"repository_id"`
	SourceRoot             string `json:"source_root"`
	DestinationRoot        string `json:"destination_root"`
	Remote                 bool   `json:"remote"`
	SourceFileCount        int64  `json:"source_file_count"`
	SourceSizeBytes        int64  `json:"source_size_bytes"`
	DestinationFileCount   int64  `json:"destination_file_count"`
	DestinationSizeBytes   int64  `json:"destination_size_bytes"`
	DestinationExists      bool   `json:"destination_exists"`
	DestinationHasManifest bool   `json:"destination_has_manifest"`
	CanMigrate             bool   `json:"can_migrate"`
	Reason                 string `json:"reason,omitempty"`
}

func (a *App) virtualRepositoryMigrationSource(id string) (virtualRepositoryIndexEntry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return virtualRepositoryIndexEntry{}, errors.New("virtual repository id is required")
	}
	items, err := a.loadVirtualRepositoryIndexItems()
	if err != nil {
		return virtualRepositoryIndexEntry{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return virtualRepositoryIndexEntry{}, errors.New("virtual repository was not found in recent repositories")
}

func parseVirtualRepositoryRootMigrationRequest(inputJSON string) (VirtualRepositoryRootMigrationRequest, error) {
	var request VirtualRepositoryRootMigrationRequest
	if err := unmarshalVirtualRepositoryInput(inputJSON, "virtual repository root migration", &request); err != nil {
		return request, err
	}
	request.RepositoryID = strings.TrimSpace(request.RepositoryID)
	request.DestinationRoot = strings.TrimSpace(request.DestinationRoot)
	if len(request.RepositoryID) > virtualRepositoryNameMaxLength || containsControlCharacter(request.RepositoryID) || strings.ContainsRune(request.RepositoryID, ':') {
		return request, errors.New("virtual repository id is invalid")
	}
	if len(request.Password) > virtualRepositoryFieldMaxLength || strings.ContainsAny(request.Password, "\r\n\x00") {
		return request, errors.New("remote virtual repository password is invalid")
	}
	return request, nil
}

func localMigrationStats(root string) (fileCount, size int64, err error) {
	err = filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		fileCount++
		size += info.Size()
		if fileCount > virtualRepositoryStatsMaxEntries {
			return fmt.Errorf("virtual repository contains more than %d files", virtualRepositoryStatsMaxEntries)
		}
		return nil
	})
	return
}

func localMigrationDestinationStats(root string) (fileCount, size int64, exists bool, err error) {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	if !info.IsDir() {
		return 0, 0, false, errors.New("destination root path is not a directory")
	}
	fileCount, size, err = localMigrationStats(root)
	return fileCount, size, true, err
}

func (a *App) PreviewVirtualRepositoryRootMigration(inputJSON string) (string, error) {
	request, err := parseVirtualRepositoryRootMigrationRequest(inputJSON)
	if err != nil {
		return "", err
	}
	item, err := a.virtualRepositoryMigrationSource(request.RepositoryID)
	if err != nil {
		return "", err
	}
	preview := VirtualRepositoryRootMigrationPreview{RepositoryID: item.ID, SourceRoot: item.RootPath, DestinationRoot: request.DestinationRoot, Remote: item.Remote != nil}
	if item.Remote != nil {
		return a.previewRemoteVirtualRepositoryRootMigration(item, request, preview)
	}
	source, err := cleanVirtualRepositoryRoot(item.RootPath)
	if err != nil {
		return "", fmt.Errorf("open source root: %w", err)
	}
	destination, err := cleanVirtualRepositoryMigrationDestinationRoot(request.DestinationRoot)
	if err != nil {
		return "", fmt.Errorf("open destination root: %w", err)
	}
	preview.SourceRoot, preview.DestinationRoot = source, destination
	if sameVirtualRepositoryPath(source, destination) {
		preview.Reason = "the destination is already this repository's root"
	} else if same, pathErr := virtualRepositoryPathContains(source, destination); pathErr != nil {
		return "", pathErr
	} else if same {
		preview.Reason = "the destination cannot be inside the current repository root"
	} else if same, pathErr := virtualRepositoryPathContains(destination, source); pathErr != nil {
		return "", pathErr
	} else if same {
		preview.Reason = "the current repository root cannot be inside the destination"
	}
	preview.SourceFileCount, preview.SourceSizeBytes, err = localMigrationStats(source)
	if err != nil {
		return "", err
	}
	preview.DestinationFileCount, preview.DestinationSizeBytes, preview.DestinationExists, err = localMigrationDestinationStats(destination)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Lstat(virtualRepositoryManifestPath(destination)); statErr == nil {
		preview.DestinationHasManifest = true
		preview.Reason = "the destination already contains a virtual repository"
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("check destination manifest: %w", statErr)
	}
	if preview.Reason == "" && preview.DestinationExists {
		if err := validateLocalVirtualRepositoryMigrationDestination(source, destination); err != nil {
			preview.Reason = err.Error()
		}
	}
	preview.CanMigrate = preview.Reason == ""
	data, err := json.Marshal(preview)
	return string(data), err
}

func virtualRepositoryPathContains(parent, child string) (bool, error) {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false, err
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
}

func cleanVirtualRepositoryMigrationDestinationRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("destination root directory is required")
	}
	if len(root) > virtualRepositoryFieldMaxLength || containsControlCharacter(root) {
		return "", errors.New("destination root directory path is invalid")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve destination root directory: %w", err)
	}
	info, statErr := os.Lstat(abs)
	if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("destination root path must not be a symbolic link")
	}
	if statErr == nil && !info.IsDir() {
		return "", errors.New("destination root path is not a directory")
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		return "", fmt.Errorf("destination root directory: %w", statErr)
	}
	return filepath.Clean(abs), nil
}

// MigrateVirtualRepositoryRoot copies the repository to the confirmed target,
// verifies the copied manifest, and only then switches the local index. The
// source is intentionally retained: cleaning it up is a separate user action.
func (a *App) MigrateVirtualRepositoryRoot(inputJSON string) (string, error) {
	request, err := parseVirtualRepositoryRootMigrationRequest(inputJSON)
	if err != nil {
		return "", err
	}
	virtualRepositoryRemoteSaveMu.Lock()
	defer virtualRepositoryRemoteSaveMu.Unlock()
	virtualRepositoryStateMu.Lock()
	defer virtualRepositoryStateMu.Unlock()
	item, err := a.virtualRepositoryMigrationSource(request.RepositoryID)
	if err != nil {
		return "", err
	}
	if item.Remote != nil {
		return a.migrateRemoteVirtualRepositoryRootLocked(item, request)
	}
	return a.migrateLocalVirtualRepositoryRootLocked(item, request)
}

func (a *App) migrateLocalVirtualRepositoryRootLocked(item virtualRepositoryIndexEntry, request VirtualRepositoryRootMigrationRequest) (string, error) {
	source, err := cleanVirtualRepositoryRoot(item.RootPath)
	if err != nil {
		return "", fmt.Errorf("open source root: %w", err)
	}
	destination, err := cleanVirtualRepositoryMigrationDestinationRoot(request.DestinationRoot)
	if err != nil {
		return "", fmt.Errorf("open destination root: %w", err)
	}
	if sameVirtualRepositoryPath(source, destination) {
		return "", errors.New("the destination is already this repository's root")
	}
	if inside, err := virtualRepositoryPathContains(source, destination); err != nil {
		return "", err
	} else if inside {
		return "", errors.New("the destination cannot be inside the current repository root")
	}
	if inside, err := virtualRepositoryPathContains(destination, source); err != nil {
		return "", err
	} else if inside {
		return "", errors.New("the current repository root cannot be inside the destination")
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", fmt.Errorf("create destination root directory: %w", err)
	}
	if _, err := os.Lstat(virtualRepositoryManifestPath(destination)); err == nil {
		return "", errors.New("the destination already contains a virtual repository")
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check destination manifest: %w", err)
	}
	if err := validateLocalVirtualRepositoryMigrationDestination(source, destination); err != nil {
		return "", err
	}
	repo, err := readVirtualRepository(source)
	if err != nil {
		return "", err
	}
	if repo.ID != item.ID {
		return "", errors.New("virtual repository index no longer matches its manifest")
	}
	if err := copyVirtualRepositoryTree(source, destination); err != nil {
		return "", err
	}
	migrated, err := readVirtualRepository(destination)
	if err != nil {
		return "", fmt.Errorf("verify migrated virtual repository: %w", err)
	}
	if migrated.ID != repo.ID || !migrated.UpdatedAt.Equal(repo.UpdatedAt) {
		return "", errors.New("the copied repository manifest does not match the source")
	}
	if err := a.updateVirtualRepositoryIndexLocked(migrated); err != nil {
		return "", fmt.Errorf("repository files were copied but the local index could not be switched: %w", err)
	}
	a.clearVirtualRepositorySyncTombstone("repo", migrated.ID)
	a.scheduleVirtualRepositorySync()
	data, err := json.Marshal(migrated)
	return string(data), err
}

func copyVirtualRepositoryTree(source, destination string) error {
	manifestRelative := filepath.Join(virtualRepositoryDirName, virtualRepositoryManifestName)
	if err := filepath.WalkDir(source, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, sourcePath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		// The manifest is copied last. If an I/O error occurs while copying the
		// data tree, the destination is never mistaken for a complete repository
		// and the recent-repository index continues to point at the source.
		if sameVirtualRepositoryPath(relative, manifestRelative) {
			return nil
		}
		return copyVirtualRepositoryTreeEntry(sourcePath, filepath.Join(destination, relative), relative, entry)
	}); err != nil {
		return err
	}
	manifestSource := filepath.Join(source, manifestRelative)
	manifestTarget := filepath.Join(destination, manifestRelative)
	info, err := os.Lstat(manifestSource)
	if err != nil {
		return fmt.Errorf("read source repository manifest: %w", err)
	}
	return copyVirtualRepositoryTreeEntry(manifestSource, manifestTarget, manifestRelative, dirEntryFromFileInfo{info})
}

type dirEntryFromFileInfo struct{ fs.FileInfo }

func (d dirEntryFromFileInfo) Type() fs.FileMode          { return d.FileInfo.Mode().Type() }
func (d dirEntryFromFileInfo) Info() (fs.FileInfo, error) { return d.FileInfo, nil }

func copyVirtualRepositoryTreeEntry(sourcePath, target, relative string, entry fs.DirEntry) error {
	if entry.IsDir() {
		if info, statErr := os.Lstat(target); statErr == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("migration conflict: destination path %q is not a directory", relative)
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
		info, err := os.Lstat(target)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration conflict: destination path %q changed while copying", relative)
		}
		return nil
	}
	if info, statErr := os.Lstat(target); statErr == nil {
		if entry.Type()&os.ModeSymlink != 0 && info.Mode()&os.ModeSymlink != 0 {
			sourceLink, sourceErr := os.Readlink(sourcePath)
			targetLink, targetErr := os.Readlink(target)
			if sourceErr != nil || targetErr != nil || sourceLink != targetLink {
				return fmt.Errorf("migration conflict: destination already has a different symbolic link at %q", relative)
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration conflict: destination path %q is a symbolic link", relative)
		}
		identical, compareErr := virtualRepositoryFilesEqual(sourcePath, target)
		if compareErr != nil {
			return compareErr
		}
		if !identical {
			return fmt.Errorf("migration conflict: destination already has a different file at %q", relative)
		}
		return nil
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if entry.Type()&os.ModeSymlink != 0 {
		link, err := os.Readlink(sourcePath)
		if err != nil {
			return err
		}
		return os.Symlink(link, target)
	}
	return copyVirtualRepositoryFile(sourcePath, target)
}

// validateLocalVirtualRepositoryMigrationDestination checks the whole merge
// before copying any content. This is deliberately separate from the copy
// loop: discovering a collision halfway through a tree would otherwise leave
// a misleading partial destination even though the source remains canonical.
func validateLocalVirtualRepositoryMigrationDestination(source, destination string) error {
	return filepath.WalkDir(source, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, sourcePath)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(destination, relative)
		info, statErr := os.Lstat(target)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if entry.IsDir() {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("migration conflict: destination path %q is not a directory", relative)
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("migration conflict: destination already has a different path at %q", relative)
			}
			sourceLink, sourceErr := os.Readlink(sourcePath)
			targetLink, targetErr := os.Readlink(target)
			if sourceErr != nil || targetErr != nil || sourceLink != targetLink {
				return fmt.Errorf("migration conflict: destination already has a different symbolic link at %q", relative)
			}
			return nil
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration conflict: destination already has a different path at %q", relative)
		}
		identical, compareErr := virtualRepositoryFilesEqual(sourcePath, target)
		if compareErr != nil {
			return compareErr
		}
		if !identical {
			return fmt.Errorf("migration conflict: destination already has a different file at %q", relative)
		}
		return nil
	})
}

func copyVirtualRepositoryFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return closeErr
	}
	if err := os.Chmod(destination, info.Mode().Perm()); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}

func virtualRepositoryFilesEqual(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, err
	}
	if leftInfo.IsDir() || rightInfo.IsDir() || leftInfo.Size() != rightInfo.Size() {
		return false, nil
	}
	leftHash, err := virtualRepositoryFileHash(left)
	if err != nil {
		return false, err
	}
	rightHash, err := virtualRepositoryFileHash(right)
	if err != nil {
		return false, err
	}
	return leftHash == rightHash, nil
}

func virtualRepositoryFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (a *App) previewRemoteVirtualRepositoryRootMigration(item virtualRepositoryIndexEntry, request VirtualRepositoryRootMigrationRequest, preview VirtualRepositoryRootMigrationPreview) (string, error) {
	if item.Remote == nil {
		return "", errors.New("remote virtual repository is required")
	}
	request.DestinationRoot = pathCleanRemoteMigrationRoot(request.DestinationRoot)
	if request.DestinationRoot == "" {
		return "", errors.New("remote destination root directory is required")
	}
	preview.DestinationRoot = request.DestinationRoot
	if request.DestinationRoot == item.RootPath {
		preview.Reason = "the destination is already this repository's root"
	} else if remoteVirtualRepositoryPathContains(item.RootPath, request.DestinationRoot) {
		preview.Reason = "the destination cannot be inside the current repository root"
	} else if remoteVirtualRepositoryPathContains(request.DestinationRoot, item.RootPath) {
		preview.Reason = "the current repository root cannot be inside the destination"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, _, _, err := a.dialRemoteVirtualRepository(ctx, item.ID, item.Remote, request.Password, request.TrustHostKey)
	if err != nil {
		return "", err
	}
	defer client.Close()
	stats, exists, manifest, err := remoteMigrationPreview(client, item.RootPath, request.DestinationRoot)
	if err != nil {
		return "", err
	}
	preview.SourceFileCount, preview.SourceSizeBytes = stats[0], stats[1]
	preview.DestinationExists, preview.DestinationHasManifest = exists, manifest
	if exists {
		preview.DestinationFileCount, preview.DestinationSizeBytes = stats[2], stats[3]
	}
	if manifest {
		preview.Reason = "the destination already contains a virtual repository"
	}
	preview.CanMigrate = preview.Reason == ""
	data, err := json.Marshal(preview)
	return string(data), err
}

func pathCleanRemoteMigrationRoot(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || containsControlCharacter(value) || len(value) > virtualRepositoryFieldMaxLength {
		return ""
	}
	clean := path.Clean(value)
	if clean == "/" {
		return ""
	}
	return clean
}

func remoteVirtualRepositoryPathContains(parent, child string) bool {
	parent, child = path.Clean(parent), path.Clean(child)
	return child == parent || strings.HasPrefix(child, strings.TrimRight(parent, "/")+"/")
}

func remoteMigrationPreview(client *ssh.Client, source, destination string) ([4]int64, bool, bool, error) {
	var values [4]int64
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return values, false, false, fmt.Errorf("start SFTP: %w", err)
	}
	defer sftpClient.Close()
	sourceCount, sourceSize, err := remoteMigrationStats(sftpClient, source)
	if err != nil {
		return values, false, false, err
	}
	values[0], values[1] = sourceCount, sourceSize
	info, err := sftpClient.Lstat(destination)
	if os.IsNotExist(err) {
		return values, false, false, nil
	}
	if err != nil {
		return values, false, false, err
	}
	if err := validateRemoteVirtualRepositoryRootDirectoryInfo(info); err != nil {
		return values, false, false, err
	}
	destinationCount, destinationSize, err := remoteMigrationStats(sftpClient, destination)
	if err != nil {
		return values, false, false, err
	}
	values[2], values[3] = destinationCount, destinationSize
	_, err = sftpClient.Lstat(path.Join(destination, virtualRepositoryDirName, virtualRepositoryManifestName))
	if err == nil {
		return values, true, true, nil
	}
	if !os.IsNotExist(err) {
		return values, true, false, err
	}
	return values, true, false, nil
}

func remoteMigrationStats(client *sftp.Client, root string) (int64, int64, error) {
	var count, size int64
	walker := client.Walk(root)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return 0, 0, err
		}
		info := walker.Stat()
		if info == nil || info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		count++
		size += info.Size()
		if count > virtualRepositoryStatsMaxEntries {
			return 0, 0, fmt.Errorf("virtual repository contains more than %d files", virtualRepositoryStatsMaxEntries)
		}
	}
	return count, size, nil
}

func (a *App) migrateRemoteVirtualRepositoryRootLocked(item virtualRepositoryIndexEntry, request VirtualRepositoryRootMigrationRequest) (string, error) {
	if item.Remote == nil {
		return "", errors.New("remote virtual repository is required")
	}
	destination := pathCleanRemoteMigrationRoot(request.DestinationRoot)
	if destination == "" {
		return "", errors.New("remote destination root directory is invalid")
	}
	if destination == item.RootPath {
		return "", errors.New("the destination is already this repository's root")
	}
	if remoteVirtualRepositoryPathContains(item.RootPath, destination) {
		return "", errors.New("the destination cannot be inside the current repository root")
	}
	if remoteVirtualRepositoryPathContains(destination, item.RootPath) {
		return "", errors.New("the current repository root cannot be inside the destination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	client, _, _, err := a.dialRemoteVirtualRepository(ctx, item.ID, item.Remote, request.Password, request.TrustHostKey)
	if err != nil {
		return "", err
	}
	defer client.Close()
	_, destinationExists, manifest, err := remoteMigrationPreview(client, item.RootPath, destination)
	if err != nil {
		return "", err
	} else if manifest {
		return "", errors.New("the destination already contains a virtual repository")
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("start SFTP: %w", err)
	}
	collision, err := remoteVirtualRepositoryMigrationCollision(sftpClient, item.RootPath, destination)
	_ = sftpClient.Close()
	if err != nil {
		return "", err
	}
	if collision != "" {
		return "", fmt.Errorf("remote migration conflict: destination already has a path at %q", collision)
	}
	// Re-read the source manifest after the filesystem preflight. This mirrors
	// the local migration's revision guard and prevents a concurrent remote save
	// from being silently migrated with stale metadata.
	sourceRepository, err := a.readRemoteVirtualRepositoryWithClient(client, item)
	if err != nil {
		return "", fmt.Errorf("reopen source remote repository before migration: %w", err)
	}
	if sourceRepository.ID != item.ID {
		return "", errors.New("virtual repository index no longer matches its remote manifest")
	}
	copyCommand, cleanupCommand := remoteVirtualRepositoryMigrationCopyCommand(item.RootPath, destination, item.ID, destinationExists)
	if _, err := runRemoteRepositoryCommand(ctx, client, copyCommand); err != nil {
		// The original root stays canonical. Cleanup is best-effort because the
		// primary transfer error must remain actionable.
		if cleanupCommand != "" {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), remoteVirtualRepositoryMigrationCleanupTimeout)
			_, _ = runRemoteRepositoryCommand(cleanupCtx, client, cleanupCommand)
			cleanupCancel()
		}
		return "", fmt.Errorf("copy remote repository: %w", err)
	}
	sftpClient, err = sftp.NewClient(client)
	if err != nil {
		return "", fmt.Errorf("verify remote migration: start SFTP: %w", err)
	}
	verifyErr := verifyRemoteVirtualRepositoryMigrationCopy(sftpClient, item.RootPath, destination)
	_ = sftpClient.Close()
	if verifyErr != nil {
		return "", fmt.Errorf("verify copied remote repository contents: %w", verifyErr)
	}
	migratedItem := item
	migratedItem.RootPath = destination
	migrated, err := a.readRemoteVirtualRepositoryWithClient(client, migratedItem)
	if err != nil {
		return "", fmt.Errorf("verify migrated remote repository: %w", err)
	}
	if migrated.ID != item.ID {
		return "", errors.New("the copied remote repository manifest does not match the source")
	}
	if !migrated.UpdatedAt.Equal(sourceRepository.UpdatedAt) {
		return "", errors.New("the copied remote repository manifest revision does not match the source")
	}
	if err := a.updateVirtualRepositoryIndexLocked(migrated); err != nil {
		return "", fmt.Errorf("remote repository files were copied but the local index could not be switched: %w", err)
	}
	a.clearVirtualRepositorySyncTombstone("repo", migrated.ID)
	a.scheduleVirtualRepositorySync()
	data, err := json.Marshal(migrated)
	return string(data), err
}

func remoteVirtualRepositoryMigrationCopyCommand(source, destination, repositoryID string, destinationExists bool) (copyCommand, cleanupCommand string) {
	if destinationExists {
		// The destination may contain unrelated files. Collision checks above
		// guarantee no source path overlaps. -n also protects the destination
		// if another process creates a source-named path between that check and
		// the copy: migration must fail verification rather than overwrite it.
		return "mkdir -p -- " + remoteShellQuote(destination) + " && cp -a -n " + remoteShellQuote(source+"/.") + " " + remoteShellQuote(destination+"/"), ""
	}
	staging := path.Join(path.Dir(destination), ".vrepo-migration-"+repositoryID+"-"+time.Now().UTC().Format("20060102150405.000000000"))
	quotedDestination := remoteShellQuote(destination)
	// Recheck both -e and -L before publishing. A plain mv would move the
	// staging folder *into* a concurrently-created directory, while a broken
	// symlink can evade -e alone. -T keeps the configured root exact.
	copyCommand = "mkdir -p -- " + remoteShellQuote(staging) + " && cp -a " + remoteShellQuote(source+"/.") + " " + remoteShellQuote(staging+"/") + " && test ! -e " + quotedDestination + " && test ! -L " + quotedDestination + " && mv -T -- " + remoteShellQuote(staging) + " " + quotedDestination
	return copyCommand, "rm -rf -- " + remoteShellQuote(staging)
}

// verifyRemoteVirtualRepositoryMigrationCopy makes the remote merge safe even
// when another process changes an existing target between the SFTP collision
// scan and cp. cp -n refuses to overwrite that new path; this check turns the
// resulting incomplete or divergent tree into an actionable failure instead
// of switching the repository index to an inconsistent copy.
func verifyRemoteVirtualRepositoryMigrationCopy(client *sftp.Client, source, destination string) error {
	walker := client.Walk(source)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return err
		}
		current := walker.Path()
		if current == source {
			continue
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(current, source), "/")
		if relative == "" {
			continue
		}
		sourceInfo, err := client.Lstat(current)
		if err != nil {
			return fmt.Errorf("read source path %q: %w", relative, err)
		}
		destinationInfo, err := client.Lstat(path.Join(destination, relative))
		if err != nil {
			return fmt.Errorf("read copied path %q: %w", relative, err)
		}
		if sourceInfo.IsDir() {
			if !destinationInfo.IsDir() || destinationInfo.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("copied path %q is not a directory", relative)
			}
			continue
		}
		if sourceInfo.Mode()&os.ModeSymlink != 0 {
			if destinationInfo.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("copied path %q is not the original symbolic link", relative)
			}
			sourceLink, sourceErr := client.ReadLink(current)
			destinationLink, destinationErr := client.ReadLink(path.Join(destination, relative))
			if sourceErr != nil || destinationErr != nil || sourceLink != destinationLink {
				return fmt.Errorf("copied symbolic link %q does not match the source", relative)
			}
			continue
		}
		if !sourceInfo.Mode().IsRegular() || !destinationInfo.Mode().IsRegular() {
			return fmt.Errorf("copied path %q has an unsupported file type", relative)
		}
		if sourceInfo.Size() != destinationInfo.Size() {
			return fmt.Errorf("copied file %q has a different size", relative)
		}
		sourceHash, err := remoteVirtualRepositoryMigrationFileHash(client, current)
		if err != nil {
			return fmt.Errorf("hash source file %q: %w", relative, err)
		}
		destinationHash, err := remoteVirtualRepositoryMigrationFileHash(client, path.Join(destination, relative))
		if err != nil {
			return fmt.Errorf("hash copied file %q: %w", relative, err)
		}
		if sourceHash != destinationHash {
			return fmt.Errorf("copied file %q does not match the source", relative)
		}
	}
	return nil
}

func remoteVirtualRepositoryMigrationFileHash(client *sftp.Client, filePath string) (string, error) {
	file, err := client.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func remoteVirtualRepositoryMigrationCollision(client *sftp.Client, source, destination string) (string, error) {
	walker := client.Walk(source)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return "", err
		}
		current := walker.Path()
		if current == source {
			continue
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(current, source), "/")
		if relative == "" {
			continue
		}
		sourceInfo, err := client.Lstat(current)
		if err != nil {
			return "", err
		}
		if destinationInfo, err := client.Lstat(path.Join(destination, relative)); err == nil {
			// Existing directories are merge points, not collisions. This is
			// what lets a migration target retain unrelated files such as
			// destination/docs/readme.txt while source/docs is copied in.
			if remoteVirtualRepositoryMigrationPathsConflict(sourceInfo, destinationInfo) {
				return relative, nil
			}
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", nil
}

func remoteVirtualRepositoryMigrationPathsConflict(sourceInfo, destinationInfo os.FileInfo) bool {
	if sourceInfo.IsDir() {
		return !destinationInfo.IsDir() || destinationInfo.Mode()&os.ModeSymlink != 0
	}
	// Files, symlinks, and special files are never merged. The remote shell
	// copy must not be allowed to overwrite an existing path of any kind.
	return true
}
