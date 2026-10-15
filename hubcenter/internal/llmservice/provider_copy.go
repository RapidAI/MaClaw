package llmservice

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

var (
	copyProviderIDSuffix   = regexp.MustCompile(`(?i)-copy(?:-[0-9]+)?$`)
	copyProviderNameSuffix = regexp.MustCompile(`（副本(?:[-－][0-9]+)?）$`)
)

// ErrProviderCopyProtected is returned when the source is a Token Bank member
// or sits in a platform-owned array. A copy would join that array under a new
// id, take a share of traffic, and then settle nothing.
var ErrProviderCopyProtected = errors.New("token bank providers cannot be copied")

// CopyProvider creates a new provider in the source's array. The copy keeps
// the upstream configuration, including the API key and vendor login, and
// receives the next free name of the form 原名（副本） or 原名（副本-N）.
func (s *Service) CopyProvider(ctx context.Context, id string) (*llmpool.ProviderConfig, error) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return nil, fmt.Errorf("provider id required")
	}
	defer s.lockRegistryWrite()()
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	idx := providerIndex(reg, id)
	if idx < 0 {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
	}
	source := reg.Providers[idx]
	if IsTokenBankMemberID(source.ID) || providerArrayProtected(findProviderArray(reg, canonicalProviderArrayID(source))) {
		return nil, ErrProviderCopyProtected
	}
	source = s.fillProviderSecretFromStore(ctx, source)
	clones := cloneProviderConfigs([]llmpool.ProviderConfig{source})
	copy := clones[0]
	takenIDs := copyTakenIDs(reg)
	copy.ID = nextCopyProviderID(takenIDs, copy.ID)
	copy.Name = nextCopyProviderName(copyTakenNames(reg), copy.Name, source.ID)
	copy.ArrayID = canonicalProviderArrayID(reg.Providers[idx])
	copy.ArrayName = ""
	copy.ArrayIndependent = false
	copy.AllowedNodes = ""
	copy.WorkBuddySessionID = ""
	copy.QoderSessionID = ""
	copy.TraeSessionID = ""
	copy.LobsterAISessionID = ""
	if err := validateProviderDefaultBilling(copy); err != nil {
		return nil, err
	}
	copy.NormalizeBilling()
	if err := normalizeProviderServeWindows(&copy); err != nil {
		return nil, err
	}
	next := cloneRegistry(reg)
	placeCopiedProviderSequence(next, &copy, source.Sequence)
	next.Providers = append(next.Providers, copy)
	insertProviderCopyAfterSource(next, source.ID, copy.ID, copy.ArrayID)
	if err := s.persistRegistry(ctx, next); err != nil {
		return nil, err
	}
	saved, err := s.GetProvider(ctx, copy.ID)
	if err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, fmt.Errorf("copied provider %s was not stored", copy.ID)
	}
	return saved, nil
}

func copyProviderIDBase(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "provider"
	}
	loc := copyProviderIDSuffix.FindStringIndex(id)
	if loc == nil || loc[0] == 0 {
		return id
	}
	return id[:loc[0]]
}

func copyTakenIDs(reg *Registry) map[string]struct{} {
	taken := map[string]struct{}{}
	if reg == nil {
		return taken
	}
	for _, provider := range reg.Providers {
		if id := strings.ToLower(strings.TrimSpace(provider.ID)); id != "" {
			taken[id] = struct{}{}
		}
	}
	for _, array := range reg.ProviderArrays {
		if id := strings.ToLower(strings.TrimSpace(array.ID)); id != "" {
			taken[id] = struct{}{}
		}
	}
	return taken
}

func copyIDTaken(taken map[string]struct{}, id string) bool {
	_, ok := taken[strings.ToLower(strings.TrimSpace(id))]
	return ok
}

func nextCopyProviderID(taken map[string]struct{}, sourceID string) string {
	base := copyProviderIDBase(sourceID)
	candidate := base + "-copy"
	if !copyIDTaken(taken, candidate) {
		return candidate
	}
	for n := 2; n < 10000; n++ {
		candidate = fmt.Sprintf("%s-copy-%d", base, n)
		if !copyIDTaken(taken, candidate) {
			return candidate
		}
	}
	return fmt.Sprintf("%s-copy-%d", base, time.Now().UnixNano())
}

func copyProviderNameBase(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	loc := copyProviderNameSuffix.FindStringIndex(name)
	if loc == nil || loc[0] == 0 {
		return name
	}
	return strings.TrimSpace(name[:loc[0]])
}

func copyNameKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.ReplaceAll(name, "－", "-")
}

func copyTakenNames(reg *Registry) map[string]struct{} {
	taken := map[string]struct{}{}
	if reg == nil {
		return taken
	}
	for _, provider := range reg.Providers {
		if name := copyNameKey(provider.Name); name != "" {
			taken[name] = struct{}{}
		}
	}
	return taken
}

func copyNameTaken(taken map[string]struct{}, name string) bool {
	_, ok := taken[copyNameKey(name)]
	return ok
}

func placeCopiedProviderSequence(reg *Registry, copy *llmpool.ProviderConfig, sourceSequence int) {
	if copy == nil {
		return
	}
	if sourceSequence <= 0 {
		copy.Sequence = nextProviderSequence(reg)
		return
	}
	slot := sourceSequence + 1
	if reg != nil {
		for i := range reg.Providers {
			if reg.Providers[i].Sequence >= slot {
				reg.Providers[i].Sequence++
			}
		}
	}
	copy.Sequence = slot
}

func insertProviderCopyAfterSource(reg *Registry, sourceID, copyID, arrayID string) {
	arr := findProviderArray(reg, arrayID)
	if arr == nil {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	copyID = strings.TrimSpace(copyID)
	members := make([]string, 0, len(arr.MemberIDs)+1)
	inserted := false
	for _, id := range arr.MemberIDs {
		if strings.EqualFold(strings.TrimSpace(id), copyID) {
			continue
		}
		members = append(members, id)
		if !inserted && strings.EqualFold(strings.TrimSpace(id), sourceID) {
			members = append(members, copyID)
			inserted = true
		}
	}
	if !inserted && copyID != "" {
		members = append(members, copyID)
	}
	arr.MemberIDs = members
}

func nextCopyProviderName(taken map[string]struct{}, sourceName, sourceID string) string {
	base := copyProviderNameBase(sourceName)
	if base == "" {
		base = copyProviderNameBase(sourceID)
	}
	if base == "" {
		base = "provider"
	}
	candidate := base + "（副本）"
	if !copyNameTaken(taken, candidate) {
		return candidate
	}
	for n := 2; n < 10000; n++ {
		candidate = fmt.Sprintf("%s（副本-%d）", base, n)
		if !copyNameTaken(taken, candidate) {
			return candidate
		}
	}
	return fmt.Sprintf("%s（副本-%d）", base, time.Now().Unix())
}
