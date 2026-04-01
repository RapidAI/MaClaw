package remote

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPTransferResult 是文件传输的结果。
type SFTPTransferResult struct {
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
	Elapsed     string `json:"elapsed"`
	BytesPerSec int64  `json:"bytes_per_sec"`
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
	sftpClient, err := sftp.NewClient(client)
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

// SFTPDownload 通过 SFTP 从远程服务器下载文件/目录到本地。
func SFTPDownload(client *ssh.Client, remotePath, localPath string, onProgress func(bytes int64)) (*SFTPTransferResult, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf("sftp client: %w", err)
	}
	defer sftpClient.Close()

	info, err := sftpClient.Stat(remotePath)
	if err != nil {
		return nil, fmt.Errorf("stat remote %s: %w", remotePath, err)
	}

	start := time.Now()
	var totalBytes int64
	var totalFiles int

	if info.IsDir() {
		totalFiles, totalBytes, err = downloadDir(sftpClient, remotePath, localPath, onProgress)
	} else {
		totalBytes, err = downloadFile(sftpClient, remotePath, localPath, onProgress)
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

	dst, err := os.Create(localPath)
	if err != nil {
		return 0, fmt.Errorf("create local %s: %w", localPath, err)
	}
	defer dst.Close()

	n, err := copyWithProgress(dst, src, onProgress)
	if err != nil {
		return n, fmt.Errorf("download %s: %w", remotePath, err)
	}
	return n, nil
}

func downloadDir(client *sftp.Client, remoteDir, localDir string, onProgress func(int64)) (int, int64, error) {
	var totalFiles int
	var totalBytes int64

	// 确保本地根目录存在
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return 0, 0, fmt.Errorf("mkdir %s: %w", localDir, err)
	}

	walker := client.Walk(remoteDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return totalFiles, totalBytes, fmt.Errorf("walk %s: %w", walker.Path(), err)
		}
		remotePath := walker.Path()
		relPath := strings.TrimPrefix(remotePath, remoteDir)
		relPath = strings.TrimPrefix(relPath, "/")
		if relPath == "" {
			continue // 跳过根目录本身
		}
		localDest := filepath.Join(localDir, filepath.FromSlash(relPath))

		if walker.Stat().IsDir() {
			if err := os.MkdirAll(localDest, 0755); err != nil {
				return totalFiles, totalBytes, err
			}
			continue
		}

		n, err := downloadFile(client, remotePath, localDest, onProgress)
		if err != nil {
			return totalFiles, totalBytes, err
		}
		totalFiles++
		totalBytes += n
	}
	return totalFiles, totalBytes, nil
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

// SFTPTransfer 通过 SSHSessionManager 执行文件传输（复用已有会话的连接）。
func (m *SSHSessionManager) SFTPTransfer(sessionID, direction, localPath, remotePath string) (string, error) {
	s, ok := m.Get(sessionID)
	if !ok {
		return "", fmt.Errorf("ssh session %s not found", sessionID)
	}

	cfg := s.Spec.HostConfig
	cfg.Defaults()

	client, err := m.pool.Acquire(cfg)
	if err != nil {
		return "", fmt.Errorf("acquire connection: %w", err)
	}
	defer m.pool.Release(cfg)

	var result *SFTPTransferResult
	switch direction {
	case "upload":
		result, err = SFTPUpload(client, localPath, remotePath, nil)
	case "download":
		result, err = SFTPDownload(client, remotePath, localPath, nil)
	default:
		return "", fmt.Errorf("unknown direction: %s (use upload/download)", direction)
	}
	if err != nil {
		return "", err
	}
	return result.String(), nil
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
