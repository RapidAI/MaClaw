package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPWrittenFile is one regular file this transfer created or replaced.
// A destination directory may already contain other files; those are not
// part of the copy and must not be counted as transferred bytes.
type SFTPWrittenFile struct {
	Path  string `json:"path,omitempty"`
	Bytes int64  `json:"bytes,omitempty"`
}

// SFTPTransferResult 是文件传输的结果。
type SFTPTransferResult struct {
	Files       int               `json:"files"`
	Bytes       int64             `json:"bytes"`
	Elapsed     string            `json:"elapsed"`
	BytesPerSec int64             `json:"bytes_per_sec"`
	Written     []SFTPWrittenFile `json:"written,omitempty"`
}

// String 返回人类可读的传输结果。
func (r SFTPTransferResult) String() string {
	return fmt.Sprintf("%d 个文件, %s, 耗时 %s (%s/s)",
		r.Files, humanSize(r.Bytes), r.Elapsed, humanSize(r.BytesPerSec))
}

// SFTPUpload 通过 SFTP 上传本地文件/目录到远程服务器。
// localPath 可以是文件或目录，remotePath 是远程目标路径。
// onProgress 可选，报告已传输字节数。
func SFTPUpload(client *ssh.Client, localPath, remotePath string, onProgress func(bytes int64)) (*SFTPTransferResult, error) {
	sftpClient, err := openSFTPClient(client)
	if err != nil {
		return nil, fmt.Errorf("sftp client: %w", err)
	}
	defer sftpClient.Close()

	info, err := os.Stat(localPath)
	if err != nil {
		return nil, fmt.Errorf("stat local %s: %w", localPath, err)
	}

	start := time.Now()
	var totalBytes int64
	var totalFiles int

	if info.IsDir() {
		totalFiles, totalBytes, err = uploadDir(sftpClient, localPath, remotePath, onProgress)
	} else {
		totalBytes, err = uploadFile(sftpClient, localPath, remotePath, onProgress)
		if err == nil {
			totalFiles = 1
		}
	}
	if err != nil {
		return nil, err
	}

	elapsed := time.Since(start)
	bps := int64(0)
	if elapsed.Seconds() > 0 {
		bps = int64(float64(totalBytes) / elapsed.Seconds())
	}
	return &SFTPTransferResult{
		Files: totalFiles, Bytes: totalBytes,
		Elapsed: elapsed.Round(time.Millisecond).String(), BytesPerSec: bps,
	}, nil
}

// sftpRemotePath is a download path that has passed the checks which do not
// need a server. needsHome means the SFTP server must expand "~" itself.
type sftpRemotePath struct {
	path      string
	needsHome bool
}

// prepareSFTPRemotePath checks a download path before a channel is opened.
// "~" and "~/..." are kept. OpenSSH expands those against the SFTP server's
// working directory, which is the login home, including inside a chroot.
// "~otheruser" is refused. An absolute path is only cleaned.
func prepareSFTPRemotePath(remotePath string) (sftpRemotePath, error) {
	slash := filepath.ToSlash(strings.TrimSpace(remotePath))
	if slash == "" || strings.ContainsAny(slash, "\r\n\x00") {
		return sftpRemotePath{}, fmt.Errorf("remote path is required")
	}
	if namesOtherRemoteAccount(slash) {
		return sftpRemotePath{}, fmt.Errorf("remote path %s names another account; pass an absolute path", slash)
	}
	if usesLoginHome(slash) {
		return sftpRemotePath{path: slash, needsHome: true}, nil
	}
	cleaned := path.Clean(slash)
	if cleaned == "." || cleaned == "" {
		return sftpRemotePath{}, fmt.Errorf("remote path is required")
	}
	return sftpRemotePath{path: cleaned}, nil
}

// resolve asks the SFTP server for its working directory when the path uses
// "~". loginDir is not called for an absolute path.
func (p sftpRemotePath) resolve(loginDir func() (string, error)) (string, error) {
	if !p.needsHome {
		return p.path, nil
	}
	if loginDir == nil {
		return "", fmt.Errorf("remote home is unavailable")
	}
	dir, err := loginDir()
	if err != nil {
		return "", fmt.Errorf("remote home: %w", err)
	}
	return joinRemoteHome(dir, p.path)
}

func usesLoginHome(slash string) bool {
	return slash == "~" || strings.HasPrefix(slash, "~/")
}

func namesOtherRemoteAccount(slash string) bool {
	return strings.HasPrefix(slash, "~") && slash != "~" && !strings.HasPrefix(slash, "~/")
}

// joinRemoteHome joins the SFTP server's working directory with "~" or
// "~/...". ".." is resolved after that directory is known. path.Clean on
// "~/../x" would erase the tilde and leave a path relative to whatever
// directory the server happened to start in.
func joinRemoteHome(home, remotePath string) (string, error) {
	home, ok := absoluteRemoteHome(home)
	if !ok {
		return "", fmt.Errorf("remote home is unavailable")
	}
	slash := filepath.ToSlash(strings.TrimSpace(remotePath))
	if slash == "~" {
		return home, nil
	}
	if !strings.HasPrefix(slash, "~/") {
		return "", fmt.Errorf("remote path is required")
	}
	rest := strings.TrimPrefix(slash, "~/")
	if rest == "" {
		return home, nil
	}
	joined := path.Clean(home + "/" + rest)
	if joined == "." || joined == "" {
		return "", fmt.Errorf("remote path is required")
	}
	return joined, nil
}

func absoluteRemoteHome(home string) (string, bool) {
	home = strings.TrimSpace(home)
	if home == "" || strings.ContainsAny(home, "\r\n\x00") {
		return "", false
	}
	home = path.Clean(filepath.ToSlash(home))
	if home == "." || home == "" || !strings.HasPrefix(home, "/") {
		return "", false
	}
	return home, true
}

// openSFTPClient starts one SFTP subsystem session and releases that session
// when the client is closed or when startup fails. sftp.NewClient leaves the
// session open on a failed subsystem request, and a normal close only shuts
// stdin, so a jump host keeps the slot until its own session limit is full.
func openSFTPClient(conn *ssh.Client) (*sftp.Client, error) {
	if conn == nil {
		return nil, fmt.Errorf("ssh client is required")
	}
	session, err := conn.NewSession()
	if err != nil {
		return nil, err
	}
	release := releaseOnce(func() { _ = session.Close() })
	started := false
	defer func() {
		if !started {
			release()
		}
	}()
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := session.RequestSubsystem("sftp"); err != nil {
		return nil, err
	}
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()
	client, err := sftp.NewClientPipe(stdout, &sftpSessionWriter{WriteCloser: stdin, release: release})
	if err != nil {
		return nil, err
	}
	started = true
	return client, nil
}

// releaseOnce runs fn on the first call. Later calls do nothing.
func releaseOnce(fn func()) func() {
	var once sync.Once
	return func() { once.Do(fn) }
}

// sftpSessionWriter closes the SSH session when the SFTP client closes its
// write pipe. Closing stdin alone does not free the session slot.
type sftpSessionWriter struct {
	io.WriteCloser
	release func()
}

func (w *sftpSessionWriter) Close() error {
	err := w.WriteCloser.Close()
	if w.release != nil {
		w.release()
	}
	return err
}

// SFTPDownload 通过 SFTP 从远程服务器下载文件/目录到本地。
func SFTPDownload(client *ssh.Client, remotePath, localPath string, onProgress func(bytes int64)) (*SFTPTransferResult, error) {
	_, _, result, err := sftpDownload(client, remotePath, localPath, onProgress, nil)
	return result, err
}

// sftpDownload copies one remote file or directory. retarget runs after "~"
// has been expanded and may move localPath, for example to place the remote
// base name inside an existing local directory. The returned paths are the
// ones that were copied.
func sftpDownload(client *ssh.Client, remotePath, localPath string, onProgress func(int64), retarget func(resolvedRemote, localPath string) string) (string, string, *SFTPTransferResult, error) {
	prepared, err := prepareSFTPRemotePath(remotePath)
	if err != nil {
		return "", "", nil, err
	}
	sftpClient, err := openSFTPClient(client)
	if err != nil {
		return "", "", nil, fmt.Errorf("sftp client: %w", err)
	}
	defer sftpClient.Close()
	remotePath, err = prepared.resolve(sftpClient.Getwd)
	if err != nil {
		return "", "", nil, err
	}
	if retarget != nil {
		localPath = retarget(remotePath, localPath)
		if strings.TrimSpace(localPath) == "" {
			return remotePath, "", nil, fmt.Errorf("local path is required")
		}
	}

	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return remotePath, localPath, nil, fmt.Errorf("stat remote %s: %w", remotePath, err)
	}

	start := time.Now()
	var totalBytes int64
	var totalFiles int
	var written []SFTPWrittenFile

	if info.IsDir() {
		// Stat follows a symlink. Walk uses Lstat, so a link that did not
		// resolve would copy nothing and still look successful.
		// The path above is the one the next call will ask for, after "~"
		// expansion. RealPath may return a different directory when a parent
		// is a symlink. The unfinished-copy mark has to answer to both.
		requestedRemote := remotePath
		root, rootErr := realDownloadRoot(remotePath, sftpClient.RealPath, sftpClient.Lstat)
		if rootErr != nil {
			return remotePath, localPath, nil, rootErr
		}
		remotePath = root
		totalFiles, totalBytes, written, err = downloadDir(sftpClient, remotePath, localPath, requestedRemote, onProgress)
	} else {
		totalBytes, err = downloadFile(sftpClient, remotePath, localPath, onProgress)
		if err == nil {
			totalFiles = 1
			written = []SFTPWrittenFile{{Path: localPath, Bytes: totalBytes}}
		}
	}
	if err != nil {
		return remotePath, localPath, nil, err
	}

	elapsed := time.Since(start)
	bps := int64(0)
	if elapsed.Seconds() > 0 {
		bps = int64(float64(totalBytes) / elapsed.Seconds())
	}
	return remotePath, localPath, &SFTPTransferResult{
		Files: totalFiles, Bytes: totalBytes, Written: written,
		Elapsed: elapsed.Round(time.Millisecond).String(), BytesPerSec: bps,
	}, nil
}

// canonicalDownloadDir asks the server for the real directory. A missing or
// relative answer keeps the path Stat already accepted.
func canonicalDownloadDir(remotePath string, realPath func(string) (string, error)) string {
	if realPath == nil {
		return remotePath
	}
	canon, err := realPath(remotePath)
	if err != nil {
		return remotePath
	}
	if cleaned, ok := absoluteRemoteHome(canon); ok {
		return cleaned
	}
	return remotePath
}

// realDownloadRoot is the directory Walk will read. Walk uses Lstat, so the
// root must already be a real directory. A symlink whose target did not
// resolve is an error, not an empty copy.
func realDownloadRoot(remotePath string, realPath func(string) (string, error), lstat func(string) (os.FileInfo, error)) (string, error) {
	resolved := canonicalDownloadDir(remotePath, realPath)
	if lstat == nil {
		return "", fmt.Errorf("remote directory %s has no resolvable target", remotePath)
	}
	info, err := lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat remote %s: %w", remotePath, err)
	}
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("remote directory %s has no resolvable target", remotePath)
	}
	return resolved, nil
}

type walkEntryKind int

const (
	walkMissing walkEntryKind = iota
	walkDir
	walkFile
	walkFollow
)

// classifyWalkEntry separates directories, regular files, and everything
// Lstat does not treat as a file. Symlinks, pipes, and devices are walkFollow:
// opening them can fail the whole copy or block on a pipe.
func classifyWalkEntry(info os.FileInfo) walkEntryKind {
	if info == nil {
		return walkMissing
	}
	if info.IsDir() {
		return walkDir
	}
	if info.Mode().IsRegular() {
		return walkFile
	}
	return walkFollow
}

// SFTPDownloadFileLimited downloads exactly one remote file while enforcing a
// byte limit during the copy. It is intended for UI conveniences such as local
// editor previews, where a stale remote stat must not allow an unexpectedly
// large transfer to consume local storage.
func SFTPDownloadFileLimited(client *ssh.Client, remotePath, localPath string, maxBytes int64) (*SFTPTransferResult, error) {
	if maxBytes == int64(^uint64(0)>>1) {
		return nil, fmt.Errorf("download limit is too large")
	}
	prepared, err := prepareSFTPRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	sftpClient, err := openSFTPClient(client)
	if err != nil {
		return nil, fmt.Errorf("sftp client: %w", err)
	}
	defer sftpClient.Close()
	remotePath, err = prepared.resolve(sftpClient.Getwd)
	if err != nil {
		return nil, err
	}

	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return nil, fmt.Errorf("stat remote %s: %w", remotePath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("remote path is not a regular file: %s", remotePath)
	}
	if maxBytes >= 0 && info.Size() > maxBytes {
		return nil, fmt.Errorf("remote file exceeds the %d byte download limit", maxBytes)
	}

	start := time.Now()
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(localPath), err)
	}
	src, err := sftpClient.Open(remotePath)
	if err != nil {
		return nil, fmt.Errorf("open remote %s: %w", remotePath, err)
	}
	defer src.Close()
	dst, err := os.OpenFile(localPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create local %s: %w", localPath, err)
	}
	dstClosed := false
	defer func() {
		if !dstClosed {
			_ = dst.Close()
		}
	}()

	reader := io.Reader(src)
	if maxBytes >= 0 {
		// Read one extra byte to distinguish an exact-size file from overflow.
		reader = io.LimitReader(src, maxBytes+1)
	}
	n, err := io.Copy(dst, reader)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", remotePath, err)
	}
	if maxBytes >= 0 && n > maxBytes {
		return nil, fmt.Errorf("remote file exceeds the %d byte download limit", maxBytes)
	}
	// Surface buffered filesystem errors before the caller atomically renames
	// and launches the local copy. A deferred Close alone would discard them.
	if err := dst.Close(); err != nil {
		return nil, fmt.Errorf("close local %s: %w", localPath, err)
	}
	dstClosed = true
	elapsed := time.Since(start)
	bps := int64(0)
	if elapsed.Seconds() > 0 {
		bps = int64(float64(n) / elapsed.Seconds())
	}
	return &SFTPTransferResult{Files: 1, Bytes: n, Elapsed: elapsed.Round(time.Millisecond).String(), BytesPerSec: bps}, nil
}

func uploadFile(client *sftp.Client, localPath, remotePath string, onProgress func(int64)) (int64, error) {
	// 远程路径统一用 /（SFTP 协议要求 POSIX 路径）
	remotePath = filepath.ToSlash(remotePath)
	remoteDir := remotePath[:strings.LastIndex(remotePath, "/")+1]
	if remoteDir != "" {
		if err := client.MkdirAll(remoteDir); err != nil {
			return 0, fmt.Errorf("mkdir %s: %w", remoteDir, err)
		}
	}

	src, err := os.Open(localPath)
	if err != nil {
		return 0, fmt.Errorf("open local %s: %w", localPath, err)
	}
	defer src.Close()

	dst, err := client.Create(remotePath)
	if err != nil {
		return 0, fmt.Errorf("create remote %s: %w", remotePath, err)
	}
	defer dst.Close()

	n, err := copyWithProgress(dst, src, onProgress)
	if err != nil {
		return n, fmt.Errorf("upload %s: %w", localPath, err)
	}
	return n, nil
}

func uploadDir(client *sftp.Client, localDir, remoteDir string, onProgress func(int64)) (int, int64, error) {
	var totalFiles int
	var totalBytes int64
	remoteDir = filepath.ToSlash(remoteDir)

	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(localDir, path)
		// 远程路径必须用 /，不能用 Windows 的 \
		remoteDest := remoteDir + "/" + filepath.ToSlash(relPath)

		if info.IsDir() {
			return client.MkdirAll(remoteDest)
		}

		n, err := uploadFile(client, path, remoteDest, onProgress)
		if err != nil {
			return err
		}
		totalFiles++
		totalBytes += n
		return nil
	})
	return totalFiles, totalBytes, err
}

func downloadFile(client *sftp.Client, remotePath, localPath string, onProgress func(int64)) (int64, error) {
	// 确保本地目录存在
	localDir := filepath.Dir(localPath)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return 0, fmt.Errorf("mkdir %s: %w", localDir, err)
	}

	src, err := client.Open(remotePath)
	if err != nil {
		return 0, fmt.Errorf("open remote %s: %w", remotePath, err)
	}
	defer src.Close()

	// The partial name is random. A fixed suffix is itself a legal remote file
	// name, so a directory copy could replace that neighbor with the partial.
	dst, err := os.CreateTemp(localDir, ".maclawpart-*")
	if err != nil {
		return 0, fmt.Errorf("create local %s: %w", localPath, err)
	}
	tmp := dst.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tmp)
		}
	}()
	// WriteTo keeps many reads in flight. A Read loop waits out each buffer,
	// which is most of the time on a ProxyJump path. Progress still uses the loop.
	n, copyErr := copyRemoteFile(dst, src, onProgress)
	closeErr := dst.Close()
	if copyErr != nil {
		return n, fmt.Errorf("download %s: %w", remotePath, copyErr)
	}
	if closeErr != nil {
		return n, fmt.Errorf("close local %s: %w", localPath, closeErr)
	}
	// Replace the destination only after the copy finishes. A failed transfer
	// must not leave a short file where a previous copy was.
	if err := replaceLocalFile(tmp, localPath); err != nil {
		return n, fmt.Errorf("save local %s: %w", localPath, err)
	}
	keep = true
	_ = os.Chmod(localPath, 0o644)
	return n, nil
}

// replaceLocalFile moves tmp onto dest. Windows refuses to rename over an
// existing file, so a failed rename removes dest only when that file is
// already there and tries once more.
func replaceLocalFile(tmp, dest string) error {
	err := os.Rename(tmp, dest)
	if err == nil {
		return nil
	}
	if _, statErr := os.Stat(dest); statErr != nil {
		return err
	}
	if rmErr := os.Remove(dest); rmErr != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func downloadDir(client *sftp.Client, remoteDir, localDir, requestedRemote string, onProgress func(int64)) (totalFiles int, totalBytes int64, written []SFTPWrittenFile, err error) {
	var skipped int
	var skippedPaths []string
	createdRoot := false
	// A directory this attempt creates is an unfinished copy until the walk
	// succeeds. The next call must keep writing into it. Without that record
	// the new directory looks like one the user already had, and the remote
	// name is nested one level deeper.
	defer func() {
		if err == nil {
			clearIncompleteDownload(localDir)
			return
		}
		discardCreatedDownloadRoot(localDir, createdRoot, totalFiles, err)
	}()

	info, statErr := os.Stat(localDir)
	if statErr != nil {
		if !os.IsNotExist(statErr) {
			return 0, 0, nil, fmt.Errorf("stat local %s: %w", localDir, statErr)
		}
		createdRoot = true
	} else if !info.IsDir() {
		return 0, 0, nil, fmt.Errorf("local path %s is not a directory", localDir)
	}
	if err = os.MkdirAll(localDir, 0755); err != nil {
		return 0, 0, nil, fmt.Errorf("mkdir %s: %w", localDir, err)
	}
	if createdRoot {
		// requestedRemote is what the next call compares. remoteDir is the
		// directory Walk reads, and it is also the path a failure result shows.
		if err = MarkIncompleteDownload(localDir, requestedRemote, remoteDir); err != nil {
			return 0, 0, nil, err
		}
	}

	walker := client.Walk(remoteDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return totalFiles, totalBytes, written, fmt.Errorf("walk %s: %w", walker.Path(), err)
		}
		localDest, err := localDownloadDest(localDir, remoteDir, walker.Path())
		if errors.Is(err, errSkipDownloadRoot) {
			continue
		}
		if err != nil {
			return totalFiles, totalBytes, written, err
		}

		switch classifyWalkEntry(walker.Stat()) {
		case walkMissing:
			return totalFiles, totalBytes, written, fmt.Errorf("walk %s: missing file info", walker.Path())
		case walkDir:
			if err := os.MkdirAll(localDest, 0755); err != nil {
				return totalFiles, totalBytes, written, err
			}
			continue
		case walkFollow:
			followed, statErr := client.Stat(walker.Path())
			if statErr != nil || followed == nil || !followed.Mode().IsRegular() {
				// A link to a directory, a broken link, or a pipe is not one
				// of the documents in this folder. Leave it out and keep copying.
				skipped++
				if len(skippedPaths) < maxSkippedDownloadPaths {
					skippedPaths = append(skippedPaths, walker.Path())
				}
				continue
			}
		}

		n, err := downloadFile(client, walker.Path(), localDest, onProgress)
		if err != nil {
			return totalFiles, totalBytes, written, err
		}
		totalFiles++
		totalBytes += n
		written = append(written, SFTPWrittenFile{Path: localDest, Bytes: n})
	}
	if err = noCopyableDownload(totalFiles, skipped); err != nil {
		return totalFiles, totalBytes, written, noCopyableDownloadError(err, remoteDir, skippedPaths)
	}
	return totalFiles, totalBytes, written, nil
}

const maxSkippedDownloadPaths = 8

func noCopyableDownloadError(err error, remoteDir string, entries []string) error {
	if len(entries) == 0 {
		return fmt.Errorf("%w: %s", err, remoteDir)
	}
	return fmt.Errorf("%w: %s (%s)", err, remoteDir, strings.Join(entries, ", "))
}

// discardCreatedDownloadRoot removes a destination this attempt created when
// the copy failed before any file was written. An existing directory and a
// partial copy stay where they are.
func discardCreatedDownloadRoot(localDir string, created bool, files int, err error) {
	if err == nil || !created || files != 0 || !canRemoveCreatedDownloadRoot(localDir) {
		return
	}
	_ = os.RemoveAll(filepath.Clean(localDir))
	clearIncompleteDownload(localDir)
}

const incompleteDownloadSuffix = ".maclaw-incomplete"

func incompleteDownloadMarkPath(localDir string) string {
	return filepath.Clean(localDir) + incompleteDownloadSuffix
}

// MarkIncompleteDownload records that localDir is an unfinished copy of each
// remote path. One copy has two names when RealPath resolves a symlink: the
// path the caller asked for, and the directory Walk reads.
func MarkIncompleteDownload(localDir string, remotePaths ...string) error {
	seen := make(map[string]struct{}, len(remotePaths))
	lines := make([]string, 0, len(remotePaths))
	for _, remoteDir := range remotePaths {
		remoteDir = path.Clean(filepath.ToSlash(strings.TrimSpace(remoteDir)))
		if remoteDir == "" || remoteDir == "." {
			continue
		}
		if _, ok := seen[remoteDir]; ok {
			continue
		}
		seen[remoteDir] = struct{}{}
		lines = append(lines, remoteDir)
	}
	if len(lines) == 0 {
		return fmt.Errorf("remote path is required")
	}
	return os.WriteFile(incompleteDownloadMarkPath(localDir), []byte(strings.Join(lines, "\n")), 0o644)
}

func clearIncompleteDownload(localDir string) {
	_ = os.Remove(incompleteDownloadMarkPath(localDir))
}

// SameIncompleteDownload reports that localDir is an unfinished copy of
// remoteDir. A later copy of that same remote directory keeps using localDir.
// The mark may list both the requested path and the RealPath target.
func SameIncompleteDownload(localDir, remoteDir string) bool {
	data, err := os.ReadFile(incompleteDownloadMarkPath(localDir))
	if err != nil {
		return false
	}
	want := path.Clean(filepath.ToSlash(strings.TrimSpace(remoteDir)))
	if want == "" || want == "." {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		got := path.Clean(filepath.ToSlash(strings.TrimSpace(line)))
		if got == want {
			return true
		}
	}
	return false
}

func canRemoveCreatedDownloadRoot(localDir string) bool {
	cleaned := filepath.Clean(localDir)
	if cleaned == "" || cleaned == "." || filepath.Dir(cleaned) == cleaned {
		return false
	}
	return true
}

// errNoCopyableDownload is a directory whose walk saw entries and copied none.
// An empty directory has nothing to skip and is a successful copy.
var errNoCopyableDownload = errors.New("remote directory contains only links or non-regular entries; pass the document path")

// noCopyableDownload reports a copy that skipped every entry. Success with
// zero files would tell the caller the documents arrived.
func noCopyableDownload(files, skipped int) error {
	if files == 0 && skipped > 0 {
		return errNoCopyableDownload
	}
	return nil
}

// errSkipDownloadRoot marks the directory the walk started at. Its children
// are the files to copy.
var errSkipDownloadRoot = errors.New("skip download root")

// localDownloadDest maps one remote walk path into localRoot. A remote name
// that cleans outside the requested directory must not be joined onto the
// destination: filepath.Join would follow ".." or an absolute path out of
// the folder the user named.
func localDownloadDest(localRoot, remoteRoot, remotePath string) (string, error) {
	remoteRoot = strings.TrimRight(path.Clean(filepath.ToSlash(remoteRoot)), "/")
	remotePath = path.Clean(filepath.ToSlash(remotePath))
	if remoteRoot == "." || remoteRoot == "" {
		remoteRoot = "/"
	}
	if remotePath == remoteRoot {
		return "", errSkipDownloadRoot
	}
	var rel string
	switch {
	case remoteRoot == "/":
		rel = strings.TrimPrefix(remotePath, "/")
	case strings.HasPrefix(remotePath, remoteRoot+"/"):
		rel = strings.TrimPrefix(remotePath, remoteRoot+"/")
	default:
		return "", fmt.Errorf("remote path %s is outside %s", remotePath, remoteRoot)
	}
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || rel == "" || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("remote path %s escapes the download directory", remotePath)
	}
	dest := filepath.Join(localRoot, rel)
	root := filepath.Clean(localRoot)
	relLocal, err := filepath.Rel(root, dest)
	if err != nil || relLocal == ".." || strings.HasPrefix(relLocal, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("remote path %s escapes the download directory", remotePath)
	}
	return dest, nil
}

// copyRemoteFile uses sftp.File.WriteTo when the caller does not need
// per-chunk progress. io.Copy selects WriteTo, which pipelines up to 64
// packets. copyWithProgress only reads one buffer at a time.
func copyRemoteFile(dst io.Writer, src io.Reader, onProgress func(int64)) (int64, error) {
	if onProgress == nil {
		return io.Copy(dst, src)
	}
	return copyWithProgress(dst, src, onProgress)
}

// copyWithProgress 带进度回调的 io.Copy，使用 256KB 缓冲区（适合大文件传输）。
func copyWithProgress(dst io.Writer, src io.Reader, onProgress func(int64)) (int64, error) {
	buf := make([]byte, 256*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			written, wErr := dst.Write(buf[:n])
			total += int64(written)
			if onProgress != nil {
				onProgress(int64(written))
			}
			if wErr != nil {
				return total, wErr
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

// callWithSessionClient runs fn on the session's pooled connection.
// A lost link is tried once more. A live shell keeps its connection and
// only the SFTP channel is opened again. A shell that does not answer is
// reconnected first. A missing remote file is not a lost link.
func (m *SSHSessionManager) callWithSessionClient(s *SSHManagedSession, fn func(*ssh.Client) error) error {
	if m == nil || s == nil {
		return fmt.Errorf("ssh session not found")
	}
	cfg := s.Spec.HostConfig
	cfg.Defaults()
	call := func() error {
		client, err := m.pool.Acquire(cfg)
		if err != nil {
			return fmt.Errorf("acquire connection: %w", err)
		}
		defer m.pool.Release(cfg, client)
		return fn(client)
	}
	err := call()
	if err == nil || !remoteSSHLinkLost(err) {
		return err
	}
	if s.Handle != nil && s.Handle.IsAlive() {
		return call()
	}
	if recErr := m.reconnectSession(s); recErr != nil {
		return err
	}
	return call()
}

// remoteSSHLinkLost reports a dead SSH transport. SFTP status errors, including
// a missing file, are not link failures: reconnecting would drop a live shell.
func remoteSSHLinkLost(err error) bool {
	if err == nil {
		return false
	}
	var status *sftp.StatusError
	if errors.As(err, &status) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, part := range []string{
		"use of closed network connection",
		"connection reset",
		"broken pipe",
		"connection refused",
		"i/o timeout",
		"unexpected eof",
		"forcibly closed",
		"network name is no longer available",
		"connection attempt failed",
		"ssh: disconnect",
	} {
		if strings.Contains(msg, part) {
			return true
		}
	}
	return false
}

// SFTPTransfer 通过 SSHSessionManager 执行文件传输（复用已有会话的连接）。
func (m *SSHSessionManager) SFTPTransfer(sessionID, direction, localPath, remotePath string) (string, error) {
	result, err := m.SFTPTransferOutcome(sessionID, direction, localPath, remotePath)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("empty transfer result")
	}
	return result.String(), nil
}

// SFTPTransferOutcome is SFTPTransfer with the byte count kept. The session's
// HostConfig, including ProxyJump, is the pooled connection key.
func (m *SSHSessionManager) SFTPTransferOutcome(sessionID, direction, localPath, remotePath string) (*SFTPTransferResult, error) {
	s, ok := m.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("ssh session %s not found", sessionID)
	}

	var result *SFTPTransferResult
	err := m.callWithSessionClient(s, func(client *ssh.Client) error {
		var callErr error
		switch direction {
		case "upload":
			result, callErr = SFTPUpload(client, localPath, remotePath, nil)
		case "download":
			result, callErr = SFTPDownload(client, remotePath, localPath, nil)
		default:
			callErr = fmt.Errorf("unknown direction: %s (use upload/download)", direction)
		}
		return callErr
	})
	return result, err
}

// SFTPDownloadForSession copies from an existing session. retarget sees the
// expanded remote path, so a login-home copy lands under the home directory's
// real name instead of a file named "~".
func (m *SSHSessionManager) SFTPDownloadForSession(sessionID, localPath, remotePath string, retarget func(resolvedRemote, localPath string) string) (string, string, *SFTPTransferResult, error) {
	s, ok := m.Get(sessionID)
	if !ok {
		return "", "", nil, fmt.Errorf("ssh session %s not found", sessionID)
	}
	var resolved, chosen string
	var result *SFTPTransferResult
	err := m.callWithSessionClient(s, func(client *ssh.Client) error {
		// The first attempt creates the destination directory before every
		// file is in place. Choosing again on the retry would see that
		// directory and nest the remote name one level deeper.
		dest, again := stableSessionDownloadDest(chosen, localPath, retarget)
		gotRemote, gotLocal, gotResult, callErr := sftpDownload(client, remotePath, dest, nil, again)
		if strings.TrimSpace(gotRemote) != "" {
			resolved = gotRemote
		}
		if strings.TrimSpace(gotLocal) != "" {
			chosen = gotLocal
		}
		result = gotResult
		return callErr
	})
	return resolved, chosen, result, err
}

// stableSessionDownloadDest keeps the destination a copy already chose.
// An empty choice means this is the first attempt.
func stableSessionDownloadDest(chosen, requested string, retarget func(resolvedRemote, localPath string) string) (string, func(resolvedRemote, localPath string) string) {
	if strings.TrimSpace(chosen) != "" {
		return chosen, nil
	}
	return requested, retarget
}

// SFTPDownloadFileLimited uses a managed session's existing SSH connection to
// download one regular file while enforcing maxBytes during the transfer.
func (m *SSHSessionManager) SFTPDownloadFileLimited(sessionID, localPath, remotePath string, maxBytes int64) (*SFTPTransferResult, error) {
	s, ok := m.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("ssh session %s not found", sessionID)
	}
	var result *SFTPTransferResult
	err := m.callWithSessionClient(s, func(client *ssh.Client) error {
		var callErr error
		result, callErr = SFTPDownloadFileLimited(client, remotePath, localPath, maxBytes)
		return callErr
	})
	return result, err
}

func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
