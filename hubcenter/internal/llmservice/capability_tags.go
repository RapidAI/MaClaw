package llmservice

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxCapabilityTags     = 24
	maxCapabilityTagRunes = 32
)

// ProviderCapabilityPresets are the tags shown on the provider editor.
var ProviderCapabilityPresets = []string{
	"chat", "streaming", "json", "tools", "reasoning", "vision",
	"document", "code", "search", "audio", "embedding", "rerank",
}

// ParseCapabilityTagList splits a human list such as "tools, vision, reasoning".
func ParseCapabilityTagList(raw string) ([]string, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '，' || r == ';' || r == '；'
	})
	return NormalizeCapabilityTags(parts)
}

// NormalizeCapabilityTags trims, lowercases, and de-duplicates tags.
// An empty result is nil, which clears the stored list.
func NormalizeCapabilityTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if !validCapabilityTag(tag) {
			return nil, fmt.Errorf("capability tag %q is not allowed", tag)
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) > maxCapabilityTags {
			return nil, fmt.Errorf("at most %d capability tags", maxCapabilityTags)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func validCapabilityTag(tag string) bool {
	if tag == "" || utf8.RuneCountInString(tag) > maxCapabilityTagRunes {
		return false
	}
	alnum := false
	for _, r := range tag {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			alnum = true
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return alnum
}

// effectiveMemberCapabilityTags prefers tags set on the service-group route.
// When that route leaves them empty, provider editor tags are added to the
// model tags. Provider tags alone must not drop capabilities the model already
// advertises. Both empty returns nil so dispatch still falls back to the model.
func effectiveMemberCapabilityTags(modelTags, providerTags, routeTags []string) []string {
	if len(routeTags) > 0 {
		return append([]string(nil), routeTags...)
	}
	if len(providerTags) == 0 {
		return nil
	}
	return unionCapabilityTags(modelTags, providerTags)
}

func unionCapabilityTags(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	add := func(tags []string) {
		for _, tag := range tags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			if _, ok := seen[tag]; ok {
				continue
			}
			seen[tag] = struct{}{}
			out = append(out, tag)
		}
	}
	add(base)
	add(extra)
	if len(out) == 0 {
		return nil
	}
	return out
}

// routeProviderCapabilityTags is the union of tags on members egress can dial.
// Paused, cooling, and quota-blocked members are omitted. An array with no
// dialable member does not advertise tags, so another model can be chosen.
func routeProviderCapabilityTags(reg *Registry, routeProviderID string) []string {
	_, _, members := lookupProviderArray(reg, routeProviderID, acceptLiveProvider)
	members = withoutCoolingArrayMembers(members)
	members = withoutQuotaBlockedMembers(members, time.Now())
	var tags []string
	for _, member := range members {
		tags = unionCapabilityTags(tags, member.CapabilityTags)
	}
	return tags
}

func arrayIDListed(reg *Registry, arrayID string) bool {
	if reg == nil {
		return false
	}
	arrayID = strings.TrimSpace(arrayID)
	if arrayID == "" {
		return false
	}
	for i := range reg.Providers {
		if strings.EqualFold(strings.TrimSpace(reg.Providers[i].ArrayID), arrayID) {
			return true
		}
	}
	return false
}

func canonicalListedArrayID(reg *Registry, arrayID string) string {
	arrayID = strings.TrimSpace(arrayID)
	if reg == nil || arrayID == "" {
		return arrayID
	}
	for _, arr := range reg.ProviderArrays {
		if strings.EqualFold(strings.TrimSpace(arr.ID), arrayID) {
			return strings.TrimSpace(arr.ID)
		}
	}
	for i := range reg.Providers {
		got := strings.TrimSpace(reg.Providers[i].ArrayID)
		if strings.EqualFold(got, arrayID) {
			return got
		}
	}
	return arrayID
}
