package database

import (
	"strings"
	"unicode"
)

const maxCatalogNamesPerProfile = 32

var skippedCatalogNames = map[string]struct{}{
	"information_schema": {},
	"performance_schema": {},
	"sys":                {},
}

// ProfileSearchTokens returns non-secret profile identifiers that the tool
// router should fold into database / database_query retrieval text. Hosts call
// this at startup and whenever profiles change so a short follow-up like
// "查看 rapidbi库" can match a persisted host, id, or schema name.
func ProfileSearchTokens(profiles []Profile) []string {
	seen := make(map[string]struct{}, len(profiles)*4)
	out := make([]string, 0, len(profiles)*4)
	add := func(raw string) {
		if token, ok := searchToken(raw); ok {
			key := strings.ToLower(token)
			if _, exists := seen[key]; exists {
				return
			}
			seen[key] = struct{}{}
			out = append(out, token)
		}
	}
	for _, profile := range profiles {
		if profile.Disabled {
			continue
		}
		add(profile.ID)
		add(profile.Name)
		add(string(profile.Type))
		add(profile.Host)
		add(profile.Database)
		add(profile.DefaultSchema)
		for _, schema := range profile.AllowedSchemas {
			add(schema)
		}
	}
	return out
}

func searchToken(raw string) (string, bool) {
	token := strings.TrimSpace(raw)
	if token == "" {
		return "", false
	}
	if _, skip := skippedCatalogNames[strings.ToLower(token)]; skip {
		return "", false
	}
	if isNumericToken(token) {
		return "", false
	}
	return token, true
}

func isNumericToken(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func catalogNamesFromSchema(info SchemaInfo) []string {
	out := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(raw string) {
		token, ok := searchToken(raw)
		if !ok {
			return
		}
		key := strings.ToLower(token)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		out = append(out, token)
	}
	for _, table := range info.Tables {
		add(table.Schema)
	}
	return out
}
