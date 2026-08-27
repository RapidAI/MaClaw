package cloudworkspace

import "github.com/RapidAI/CodeClaw/corelib/cloudworkspaceignore"

// IgnoreFileName is the workspace-root ignore file.
const IgnoreFileName = cloudworkspaceignore.FileName

// ShouldIgnore reports whether relPath is excluded from cloud-workspace sync.
// corelib/cloudworkspaceignore is the source of truth.
func ShouldIgnore(relPath string, isDir bool, cloudignore string) bool {
	return cloudworkspaceignore.ShouldIgnore(relPath, isDir, cloudignore)
}

// ReadCloudignore returns the workspace-root .maclaw-cloudignore contents.
func ReadCloudignore(root string) (string, error) {
	return cloudworkspaceignore.ReadCloudignore(root)
}
