package llmpool

import (
	"errors"
	"strconv"
	"strings"
)

// This file temporarily restores the pre-HeadSlots package-level head API.
// The in-flight HeadSlots refactor (head_version.go) removed these functions
// while hub/hubcenter callers (llm_class_head.go, class_head.go) still use
// them, which breaks the build. These wrappers preserve the exact legacy
// behavior until the callers migrate to HeadSlots; delete this file once the
// migration lands.

// legacyHeadRoleCurrent keeps the historical "current" role label on the wire;
// the HeadSlots API renamed the serving role to HeadRoleServing.
const legacyHeadRoleCurrent = "current"

// HeadRoleCurrent is the historical role label kept for callers and tests that
// predate the HeadSlots refactor. Remove with this file.
const HeadRoleCurrent = legacyHeadRoleCurrent

// NextHeadVersion returns the next unused classification head version.
func NextHeadVersion(current, previous *ClassificationHead, history []ClassHeadVersionInfo) int {
	max := 0
	if current != nil && current.Version > max {
		max = current.Version
	}
	if previous != nil && previous.Version > max {
		max = previous.Version
	}
	for _, item := range history {
		if item.Version > max {
			max = item.Version
		}
	}
	return max + 1
}

// RotateClassificationHead promotes next to current and demotes the existing
// current to previous, archiving a ready previous head into history.
func RotateClassificationHead(current, previous **ClassificationHead, currentSrc, previousSrc *string, history *[]ClassHeadVersionInfo, next *ClassificationHead, source string) {
	if current == nil || previous == nil || currentSrc == nil || previousSrc == nil || history == nil || next == nil {
		return
	}
	if *previous != nil && (*previous).Ready() {
		*history = ArchiveRetiredHead(*history, VersionInfoFromHead(HeadRoleHistory, *previousSrc, *previous))
	}
	if *current != nil && (*current).Ready() {
		*previous = (*current).Clone()
		*previousSrc = *currentSrc
	}
	*current = next
	*currentSrc = strings.TrimSpace(source)
}

// CollectHeadVersions lists current, previous, and historical head versions
// without duplicates, in that order.
func CollectHeadVersions(current, previous *ClassificationHead, currentSrc, previousSrc string, history []ClassHeadVersionInfo) []ClassHeadVersionInfo {
	seen := map[int]struct{}{}
	out := make([]ClassHeadVersionInfo, 0, 2+len(history))
	if current != nil && current.Version > 0 {
		out = append(out, VersionInfoFromHead(legacyHeadRoleCurrent, currentSrc, current))
		seen[current.Version] = struct{}{}
	}
	if previous != nil && previous.Version > 0 {
		out = append(out, VersionInfoFromHead(HeadRolePrevious, previousSrc, previous))
		seen[previous.Version] = struct{}{}
	}
	for _, item := range history {
		if item.Version <= 0 {
			continue
		}
		if _, ok := seen[item.Version]; ok {
			continue
		}
		item.Role = HeadRoleHistory
		out = append(out, item)
		seen[item.Version] = struct{}{}
	}
	return out
}

// ResolveHeadSlot resolves a client-supplied slot selector to a ready head.
func ResolveHeadSlot(slot string, current, previous *ClassificationHead) (string, *ClassificationHead, error) {
	slot = strings.ToLower(strings.TrimSpace(slot))
	switch slot {
	case "", legacyHeadRoleCurrent, "serving":
		if current == nil || !current.Ready() {
			return "", nil, errors.New("current head is not ready")
		}
		return legacyHeadRoleCurrent, current, nil
	case HeadRolePrevious, "prev":
		if previous == nil || !previous.Ready() {
			return "", nil, errors.New("previous head is not ready")
		}
		return HeadRolePrevious, previous, nil
	}
	n, err := strconv.Atoi(slot)
	if err != nil || n <= 0 {
		return "", nil, errors.New("unknown head slot")
	}
	if current != nil && current.Version == n && current.Ready() {
		return legacyHeadRoleCurrent, current, nil
	}
	if previous != nil && previous.Version == n && previous.Ready() {
		return HeadRolePrevious, previous, nil
	}
	return "", nil, errors.New("retired head versions are metadata only")
}
