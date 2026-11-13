package botmgmt

import (
	"context"
	"errors"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// ErrDesktopMemory means the container is over the admission line. The run
// is queued and starts when a later read of the same stats is under the line.
var ErrDesktopMemory = errors.New("这台电脑内存紧张，这项先排着，空出来再做。")

const desktopMemoryLine = "这台电脑内存紧张，这项先排着，空出来再做。"

// desktopMemorySource is the desktop service that can read container memory.
// A desktop that does not report memory does not block admission.
type desktopMemorySource interface {
	ContainerMemory(ctx context.Context, tenantID, userID string) (used, limit int64, err error)
}

func memoryPressured(used, limit int64, fraction float64) bool {
	if limit <= 0 || fraction <= 0 {
		return false
	}
	return float64(used)/float64(limit) >= fraction
}

func (s *Service) desktopMemoryPressured(ctx context.Context, tenantID, userID string) bool {
	if s == nil || s.Desktop == nil {
		return false
	}
	src, ok := s.Desktop.(desktopMemorySource)
	if !ok {
		return false
	}
	used, limit, err := src.ContainerMemory(ctx, tenantID, userID)
	if err != nil {
		return false
	}
	return desktop.MemoryPressured(used, limit)
}
