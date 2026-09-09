package database

import (
	"fmt"
	"strings"
	"unicode"
)

// ProfileMatch is a non-secret hint used to pick among multiple profiles
// from conversational text (host, catalog, username, or id).
type ProfileMatch struct {
	ID       string
	Host     string
	Database string
	Username string
	Type     SourceType
}

func (m *Manager) MatchProfiles(hint ProfileMatch) []ProfileSummary {
	all := m.ProfileSummaries()
	id := strings.ToLower(strings.TrimSpace(hint.ID))
	host := strings.ToLower(strings.TrimSpace(hint.Host))
	catalog := strings.ToLower(strings.TrimSpace(hint.Database))
	user := strings.ToLower(strings.TrimSpace(hint.Username))
	typ := SourceType(strings.ToLower(strings.TrimSpace(string(hint.Type))))
	if id == "" && host == "" && catalog == "" && user == "" && typ == "" {
		return all
	}
	out := make([]ProfileSummary, 0, len(all))
	for _, p := range all {
		if id != "" && strings.ToLower(p.ID) != id && strings.ToLower(p.Name) != id {
			continue
		}
		if typ != "" && p.Type != typ {
			continue
		}
		if host != "" && strings.ToLower(p.Host) != host {
			continue
		}
		if catalog != "" && strings.ToLower(p.Database) != catalog {
			continue
		}
		if user != "" && strings.ToLower(p.Username) != user {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ResolveProfileID returns a unique profile id for connect. Empty id with a
// host/catalog hint selects among configured sources; 0 or 2+ matches return
// the candidates so the caller can ask the user or propose a new profile.
func (m *Manager) ResolveProfileID(hint ProfileMatch) (string, []ProfileSummary, error) {
	if id := strings.TrimSpace(hint.ID); id != "" {
		for _, p := range m.ProfileSummaries() {
			if strings.EqualFold(p.ID, id) || strings.EqualFold(p.Name, id) {
				return p.ID, []ProfileSummary{p}, nil
			}
		}
		return "", nil, fmt.Errorf("profile_not_found")
	}
	matched := m.MatchProfiles(hint)
	if len(matched) == 1 {
		return matched[0].ID, matched, nil
	}
	if len(matched) == 0 {
		return "", nil, fmt.Errorf("profile_not_found")
	}
	return "", matched, fmt.Errorf("ambiguous_profile")
}

func slugProfileID(typ SourceType, host string) string {
	raw := strings.TrimSpace(string(typ)) + "-" + strings.TrimSpace(host)
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		id = "db"
	}
	if len(id) > 64 {
		id = id[:64]
	}
	if id[0] == '-' || id[0] == '.' || id[0] == '_' {
		id = "db" + id
	}
	return id
}

func readyProposedProfile(manager *Manager, draft Profile) (ProfileSummary, bool) {
	if manager == nil {
		return ProfileSummary{}, false
	}
	candidates := []ProfileMatch{{ID: draft.ID}}
	if strings.TrimSpace(draft.Host) != "" {
		candidates = append(candidates, ProfileMatch{Host: draft.Host, Username: draft.Username, Type: draft.Type})
	}
	for _, hint := range candidates {
		items := manager.MatchProfiles(hint)
		var ready []ProfileSummary
		for _, item := range items {
			if item.Status == "configured" && manager.HasBoundSecret(item.ID) {
				ready = append(ready, item)
			}
		}
		if len(ready) == 1 {
			return ready[0], true
		}
	}
	return ProfileSummary{}, false
}

// DraftProposedProfile builds the non-secret profile the host should offer
// after propose_profile or a failed connect/list for a known host. ID and
// display name are derived the same way for GUI, TUI and srv.
func DraftProposedProfile(args map[string]interface{}) Profile {
	return draftProposedProfile(args)
}

func draftProposedProfile(args map[string]interface{}) Profile {
	typ := SourceType(strings.ToLower(stringArg(args, "type")))
	if typ == "" {
		typ = SourceMySQL
	}
	host := stringArg(args, "host")
	id := stringArg(args, "profile_id")
	if id == "" {
		id = slugProfileID(typ, host)
	}
	name := stringArg(args, "name")
	if name == "" {
		name = strings.TrimSpace(host)
	}
	if name == "" {
		name = id
	}
	return Profile{
		ID:                id,
		Name:              name,
		Type:              typ,
		Host:              host,
		Port:              intArg(args, "port", 0),
		Database:          stringArg(args, "database"),
		Username:          stringArg(args, "username"),
		ReadOnly:          true,
		AllowExternalHost: boolArg(args, "allow_external_host", false),
	}
}
