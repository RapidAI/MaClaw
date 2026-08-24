package tool

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A turn whose outcome is a single fact — one search, one screenshot, one
// commit — is fully described by one selection holding one one-time grant.
// An iterative turn is not: editing code means reading a file, changing it,
// running a check, then reading again. Re-arming a spent grant would dissolve
// the property the whole execution plane rests on, so a bounded repeat is
// expressed the other way around: the plan carries one sibling selection per
// permitted invocation, and the host exposes them one at a time.
//
// Every invariant therefore survives untouched. Each sibling is an immutable
// planned node, carries its own signed grant, and owns exactly one durable
// execution record. The budget is not a runtime counter that a caller could
// raise; it is the number of nodes the plan was published with, visible to
// review and to audit.
const repeatSiblingSeparator = "#"

// RepeatSiblingBudgetLimit caps how many invocations one need may claim. The
// bound exists because the budget materializes as real plan nodes: a rule with
// a runaway count would inflate every plan, revision, and audit record built
// from it.
const RepeatSiblingBudgetLimit = 32

// RepeatSiblingNeedID names the index-th invocation of a repeatable need.
// Index 0 returns the base identity unchanged, so a single-invocation need
// keeps the exact ID it had before repeats existed and no already-published
// plan, durable execution key, or stored grant shifts underneath.
func RepeatSiblingNeedID(baseID string, index int) string {
	baseID = strings.TrimSpace(baseID)
	if index <= 0 {
		return baseID
	}
	return baseID + repeatSiblingSeparator + fmt.Sprintf("%02d", index+1)
}

// RepeatFamilyID collapses a need or selection identity onto the family its
// siblings share. Hosts group by this to keep one live invocation per family;
// an identity without a sibling suffix is its own family of one.
func RepeatFamilyID(id string) string {
	id = strings.TrimSpace(id)
	cut := strings.LastIndex(id, repeatSiblingSeparator)
	if cut <= 0 {
		return id
	}
	if !repeatSiblingIndex(id[cut+len(repeatSiblingSeparator):]) {
		return id
	}
	return id[:cut]
}

// repeatSiblingIndex reports whether the suffix is one this package minted.
// A capability, adapter, or qualifier value is free to contain "#", so the
// suffix only splits a family when it is exactly the generated shape.
func repeatSiblingIndex(suffix string) bool {
	if len(suffix) != 2 {
		return false
	}
	value, err := strconv.Atoi(suffix)
	return err == nil && value >= 2
}

// RepeatSiblingBudget normalizes a declared budget. Zero and one both mean the
// historical single-invocation need, so a rule that says nothing about repeats
// plans exactly as it always has.
func RepeatSiblingBudget(maxInvocations int) int {
	if maxInvocations < 1 {
		return 1
	}
	if maxInvocations > RepeatSiblingBudgetLimit {
		return RepeatSiblingBudgetLimit
	}
	return maxInvocations
}

// RepeatExposure is what a host currently knows about its plan's selections,
// stated in the terms every host already keeps.
type RepeatExposure struct {
	// Ready is the plan's currently ready selections.
	Ready []PlannedSelection
	// Completed marks selections with a durable success.
	Completed map[string]bool
	// Granted marks selections already handed a grant, whether or not that
	// grant is still live.
	Granted map[string]bool
	// Live marks selections whose grant has not been consumed yet.
	Live map[string]bool
	// Unsettled optionally reports that a spent selection has no settled
	// outcome. Hosts back it with the durable execution record; leaving it nil
	// means the host cannot tell, and no family is held back.
	Unsettled func(selectionID string) bool
}

// NextRepeatSelections chooses which ready selections may hold a grant right
// now. It lives here rather than in either host because the two hosts kept
// separate copies of this closure, and that duplication is exactly how they
// drifted: a budget honored on one and ignored on the other would expose a
// whole invocation allowance at once.
//
// The model must see one call per family at a time. Handing over a whole
// budget would let a single round spend it, and would render one outcome
// repeatedly as if the copies were different actions.
//
// A family of exactly one sibling — every need that declares no budget —
// resolves to the same selection a host picked before repeats existed, which
// is why both hosts can adopt this without changing any existing family.
func NextRepeatSelections(exposure RepeatExposure) map[string]bool {
	liveFamilies := make(map[string]bool, len(exposure.Live))
	for selectionID := range exposure.Live {
		liveFamilies[RepeatFamilyID(selectionID)] = true
	}
	families := make(map[string][]string, len(exposure.Ready))
	for _, selection := range exposure.Ready {
		// A completed node stays in the plan as immutable decision evidence.
		// It must not re-enter the exposure closure merely because nothing
		// depends on it.
		if exposure.Completed[selection.ID] {
			continue
		}
		family := RepeatFamilyID(selection.ID)
		families[family] = append(families[family], selection.ID)
	}
	next := make(map[string]bool, len(families))
	for family, siblings := range families {
		if liveFamilies[family] || repeatFamilyIsUnsettled(exposure, siblings) {
			continue
		}
		sort.Strings(siblings)
		for _, selectionID := range siblings {
			if !exposure.Granted[selectionID] {
				next[selectionID] = true
				break
			}
		}
	}
	return next
}

// repeatFamilyIsUnsettled holds a family back while one of its spent siblings
// has no settled outcome. A failed attempt costs its budget and the family
// moves on, but an operation awaiting a transport receipt, still running, or
// whose result was lost must never be followed by another attempt: spending
// budget on a new call is not the same act as retrying an effect that may
// already have happened.
func repeatFamilyIsUnsettled(exposure RepeatExposure, siblings []string) bool {
	if exposure.Unsettled == nil {
		return false
	}
	for _, selectionID := range siblings {
		if !exposure.Granted[selectionID] || exposure.Completed[selectionID] {
			continue
		}
		if exposure.Unsettled(selectionID) {
			return true
		}
	}
	return false
}
