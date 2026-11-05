package guiapp

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type fileHopJITResultStore struct {
	root string
}

var _ tool.HopJITResultStore = (*fileHopJITResultStore)(nil)

func newFileHopJITResultStore(root string) *fileHopJITResultStore {
	return &fileHopJITResultStore{root: root}
}

func hopJITResultRoot(dataDir string, identity hopJITExecutionIdentity) string {
	owner := strings.TrimSpace(identity.PrincipalID)
	if owner == "" {
		owner = "local"
	}
	digest := sha256.Sum256([]byte(owner))
	return filepath.Join(dataDir, "hopjit", "tool-results", fmt.Sprintf("%x", digest[:16]))
}

func (s *fileHopJITResultStore) resultPath(scope tool.InvocationScope, selectionID, requestDigest string) (string, error) {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return "", errors.New("HopJIT result store is unavailable")
	}
	encoded, err := json.Marshal(struct {
		Scope         tool.InvocationScope `json:"scope"`
		SelectionID   string               `json:"selection_id"`
		RequestDigest string               `json:"request_digest"`
	}{Scope: scope, SelectionID: selectionID, RequestDigest: requestDigest})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return filepath.Join(s.root, fmt.Sprintf("%x", digest[:])+".txt"), nil
}

func (s *fileHopJITResultStore) SaveHopJITResult(scope tool.InvocationScope, selectionID, requestDigest, value string) error {
	path, err := s.resultPath(scope, selectionID, requestDigest)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (s *fileHopJITResultStore) LoadHopJITResult(scope tool.InvocationScope, selectionID, requestDigest string) (string, bool, error) {
	path, err := s.resultPath(scope, selectionID, requestDigest)
	if err != nil {
		return "", false, err
	}
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}
