package remote

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SCPUploader abstracts the file upload and command execution capabilities
// needed for credential mounting. This decouples CredentialMounter from
// the concrete SSHSessionManager, enabling easier testing and future changes.
type SCPUploader interface {
	// UploadFile uploads a local file to the remote host associated with the session.
	UploadFile(sessionID string, localPath string, remotePath string) error
	// RunCommand executes a command on the remote host associated with the session.
	RunCommand(sessionID string, command string) error
}

// sshManagerUploader adapts SSHSessionManager to the SCPUploader interface.
type sshManagerUploader struct {
	mgr *SSHSessionManager
}

func (u *sshManagerUploader) UploadFile(sessionID, localPath, remotePath string) error {
	_, err := u.mgr.SFTPTransfer(sessionID, "upload", localPath, remotePath)
	return err
}

func (u *sshManagerUploader) RunCommand(sessionID, command string) error {
	_, err := u.mgr.WriteInputChecked(sessionID, command)
	return err
}

// CredentialMounter handles SCP upload and cleanup of credential files
// for remote skill execution.
type CredentialMounter struct {
	uploader SCPUploader
}

// NewCredentialMounter creates a CredentialMounter using the given SSHSessionManager.
func NewCredentialMounter(sshMgr *SSHSessionManager) *CredentialMounter {
	return &CredentialMounter{
		uploader: &sshManagerUploader{mgr: sshMgr},
	}
}

// NewCredentialMounterWithUploader creates a CredentialMounter with a custom SCPUploader.
// Useful for testing or alternative upload implementations.
func NewCredentialMounterWithUploader(uploader SCPUploader) *CredentialMounter {
	return &CredentialMounter{uploader: uploader}
}

// ExpandCredentialPath expands ~ to the user's home directory and expands
// environment variables ($HOME, %USERPROFILE%, etc.) in a credential file path.
// Works cross-platform on Windows, macOS, and Linux.
func ExpandCredentialPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("credential path is empty")
	}

	// Expand ~ to home directory
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		// Handle both ~/path and ~\path (Windows)
		path = home + path[1:]
	}

	// Expand environment variables ($HOME, %USERPROFILE%, etc.)
	path = os.ExpandEnv(path)

	// Clean the path for the current OS
	path = filepath.Clean(path)

	// Convert to absolute path if not already
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve absolute path: %w", err)
		}
		path = abs
	}

	return path, nil
}

// ValidateCredentialFiles checks that all declared credential files exist locally
// after path expansion. Returns a list of missing or inaccessible file paths
// (using the original unexpanded paths for user-friendly error messages).
// Both missing files and permission errors are treated as validation failures.
func ValidateCredentialFiles(files []string) []string {
	var missing []string
	for _, f := range files {
		expanded, err := ExpandCredentialPath(f)
		if err != nil {
			missing = append(missing, f)
			continue
		}
		if _, err := os.Stat(expanded); err != nil {
			// Catch both ENOENT and permission denied — either way the
			// credential is not usable.
			missing = append(missing, f)
		}
	}
	return missing
}

// MountCredentials uploads credential files to the remote host via SFTP,
// sets 0600 permissions on each uploaded file, and returns a cleanup function
// that deletes all uploaded files from the remote host.
//
// The cleanup function MUST be deferred by the caller to ensure credentials
// are removed regardless of skill execution outcome.
func (cm *CredentialMounter) MountCredentials(sessionID string, files []string) (cleanup func(), err error) {
	if len(files) == 0 {
		return func() {}, nil
	}

	var uploadedRemotePaths []string

	for _, f := range files {
		localPath, err := ExpandCredentialPath(f)
		if err != nil {
			return func() {}, fmt.Errorf("expand credential path %q: %w", f, err)
		}

		// Verify file exists before attempting upload
		if _, err := os.Stat(localPath); err != nil {
			return func() {}, fmt.Errorf("credential file %q not found: %w", f, err)
		}

		// Determine remote path: place in /tmp/.maclaw_creds_<sessionID>/
		// preserving the original filename
		remotePath := remoteCredentialPath(sessionID, localPath)

		// Upload the file
		if err := cm.uploader.UploadFile(sessionID, localPath, remotePath); err != nil {
			// Cleanup any files already uploaded before returning error
			cleanupUploaded(cm.uploader, sessionID, uploadedRemotePaths)
			return func() {}, fmt.Errorf("upload credential %q: %w", f, err)
		}

		// Set restrictive permissions (0600)
		chmodCmd := fmt.Sprintf("chmod 600 %s", shellQuote(remotePath))
		if err := cm.uploader.RunCommand(sessionID, chmodCmd); err != nil {
			// Log but don't fail — file is uploaded, permissions are best-effort
			log.Printf("[credential-mount] warning: chmod 600 failed for %s: %v", remotePath, err)
		}

		uploadedRemotePaths = append(uploadedRemotePaths, remotePath)
	}

	// Return cleanup function that removes all uploaded credential files
	cleanup = func() {
		cleanupUploaded(cm.uploader, sessionID, uploadedRemotePaths)
	}

	return cleanup, nil
}

// cleanupUploaded removes uploaded credential files from the remote host.
// Errors are logged but do not cause failure.
func cleanupUploaded(uploader SCPUploader, sessionID string, remotePaths []string) {
	for _, rp := range remotePaths {
		rmCmd := fmt.Sprintf("rm -f %s", shellQuote(rp))
		if err := uploader.RunCommand(sessionID, rmCmd); err != nil {
			log.Printf("[credential-mount] cleanup warning: failed to remove %s: %v", rp, err)
		}
	}

	// Also try to remove the credential directory if empty
	if len(remotePaths) > 0 {
		dir := filepath.Dir(remotePaths[0])
		// Use POSIX path separator for remote
		dir = strings.ReplaceAll(dir, "\\", "/")
		rmdirCmd := fmt.Sprintf("rmdir %s 2>/dev/null", shellQuote(dir))
		_ = uploader.RunCommand(sessionID, rmdirCmd)
	}
}

// remoteCredentialPath computes the remote path for a credential file.
// Files are placed in /tmp/.maclaw_creds_<sessionID>/ to isolate per-session.
func remoteCredentialPath(sessionID, localPath string) string {
	filename := filepath.Base(localPath)
	// Always use POSIX paths for remote (Linux/macOS SSH targets)
	return fmt.Sprintf("/tmp/.maclaw_creds_%s/%s", sessionID, filename)
}

// shellQuote wraps a string in single quotes for safe shell usage.
func shellQuote(s string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}

// CurrentPlatformName returns the platform name mapped from runtime.GOOS.
// This is a utility used by credential path expansion on different platforms.
func CurrentPlatformName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return runtime.GOOS
	}
}
