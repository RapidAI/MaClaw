package maclawappcontract

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SelectHubPackageApps returns a cloned MaClaw App package scoped to the
// selected app IDs. Empty selectedAppIDs returns the full cloned package.
func SelectHubPackageApps(pkg map[string]any, selectedAppIDs []string) (map[string]any, error) {
	if pkg == nil {
		return nil, fmt.Errorf("maclaw app package is empty")
	}
	entries := packageAppEntries(pkg)
	selected := selectionIDSet(selectedAppIDs)
	if len(selected) == 0 {
		return cloneMap(pkg), nil
	}
	filteredEntries := make([]packageAppEntry, 0, len(entries))
	filteredApps := make([]any, 0, len(entries))
	for _, entry := range entries {
		if !selectionMatches(selected, entry.ID) {
			continue
		}
		filteredEntries = append(filteredEntries, entry)
		entryClone := cloneMap(entry.Entry)
		filterEntryReviewEvidence(entryClone, []packageAppEntry{entry})
		if filtered := resolvedDependenciesForEntries(entryClone["resolved_dependencies"], []packageAppEntry{entry}); filtered != nil {
			entryClone["resolved_dependencies"] = filtered
		} else {
			delete(entryClone, "resolved_dependencies")
		}
		filteredApps = append(filteredApps, entryClone)
	}
	if len(filteredEntries) == 0 {
		return nil, fmt.Errorf("selected MaClaw App package has no matching apps")
	}
	installPackage := cloneMap(pkg)
	installPackage["apps"] = filteredApps
	if filtered := resolvedDependenciesForEntries(pkg["resolved_dependencies"], filteredEntries); filtered != nil {
		installPackage["resolved_dependencies"] = filtered
	} else {
		delete(installPackage, "resolved_dependencies")
	}
	if filtered := reviewEvidenceForEntries(installPackage["review_evidence"], filteredEntries); filtered != nil {
		installPackage["review_evidence"] = filtered
	}
	if filtered := reviewEvidenceForEntries(installPackage["maclaw_app_review_evidence"], filteredEntries); filtered != nil {
		installPackage["maclaw_app_review_evidence"] = filtered
	}
	return installPackage, nil
}

type packageAppEntry struct {
	Entry map[string]any
	App   map[string]any
	ID    string
}

func packageAppEntries(pkg map[string]any) []packageAppEntry {
	apps := anySlice(pkg["apps"])
	out := make([]packageAppEntry, 0, len(apps))
	for _, raw := range apps {
		entry := anyMap(raw)
		app := anyMap(entry["app"])
		id := strings.TrimSpace(firstString(app["id"]))
		if entry == nil || app == nil || id == "" {
			continue
		}
		out = append(out, packageAppEntry{Entry: entry, App: app, ID: id})
	}
	return out
}

func filterEntryReviewEvidence(entry map[string]any, entries []packageAppEntry) {
	app := anyMap(entry["app"])
	governance := anyMap(app["governance"])
	submission := anyMap(governance["submission"])
	if len(submission) == 0 {
		return
	}
	if filtered := reviewEvidenceForEntries(submission["review_evidence"], entries); filtered != nil {
		submission["review_evidence"] = filtered
	}
	if filtered := reviewEvidenceForEntries(submission["maclaw_app_review_evidence"], entries); filtered != nil {
		submission["maclaw_app_review_evidence"] = filtered
	}
}

func reviewEvidenceForEntries(raw any, entries []packageAppEntry) map[string]any {
	evidence := anyMap(raw)
	if len(evidence) == 0 || len(entries) == 0 {
		return nil
	}
	selected := map[string]struct{}{}
	for _, entry := range entries {
		for key := range selectionIDSet([]string{entry.ID}) {
			selected[key] = struct{}{}
		}
	}
	filtered := map[string]any{}
	for key, value := range evidence {
		if _, ok := selected[strings.ToLower(strings.TrimSpace(key))]; !ok {
			continue
		}
		if valueMap := anyMap(value); valueMap != nil {
			filtered[key] = cloneMap(valueMap)
		} else {
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		return cloneMap(evidence)
	}
	return filtered
}

func resolvedDependenciesForEntries(raw any, entries []packageAppEntry) []any {
	items := anySlice(raw)
	if len(items) == 0 || len(entries) == 0 {
		return nil
	}
	selectedAppIDs := map[string]struct{}{}
	selectedDependencyIDs := map[string]struct{}{}
	for _, entry := range entries {
		selectedAppIDs[strings.ToLower(strings.TrimSpace(entry.ID))] = struct{}{}
		for _, depID := range dependencyIDsForEntry(entry) {
			selectedDependencyIDs[strings.ToLower(strings.TrimSpace(depID))] = struct{}{}
		}
	}
	filtered := make([]any, 0, len(items))
	for _, item := range items {
		m := anyMap(item)
		if m == nil {
			continue
		}
		appIDs := stringList(firstAny(m["app_ids"], m["appIDs"]))
		if len(appIDs) > 0 {
			for _, appID := range appIDs {
				if _, ok := selectedAppIDs[strings.ToLower(strings.TrimSpace(appID))]; ok {
					filtered = append(filtered, cloneMap(m))
					break
				}
			}
			continue
		}
		id := strings.ToLower(strings.TrimSpace(fmt.Sprint(m["id"])))
		if _, ok := selectedDependencyIDs[id]; ok {
			filtered = append(filtered, cloneMap(m))
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func dependencyIDsForEntry(entry packageAppEntry) []string {
	out := []string{}
	add := func(raw any) {
		for _, item := range anySlice(anyMap(raw)["skills"]) {
			if id := firstString(anyMap(item)["id"]); id != "" {
				out = append(out, id)
			}
		}
	}
	if binding := anyMap(entry.App["binding"]); binding != nil {
		add(binding["dependencies"])
	}
	if governance := anyMap(entry.App["governance"]); governance != nil {
		add(governance["dependencies"])
		add(governance["dependencyVerification"])
		add(governance["dependency_verification"])
	}
	return out
}

func selectionIDSet(appIDs []string) map[string]struct{} {
	out := map[string]struct{}{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	for _, appID := range appIDs {
		trimmed := strings.TrimSpace(appID)
		add(trimmed)
		if strings.HasPrefix(strings.ToLower(trimmed), "market-") {
			add(trimmed[len("market-"):])
		} else if trimmed != "" {
			add("market-" + trimmed)
		}
	}
	return out
}

func selectionMatches(selected map[string]struct{}, appID string) bool {
	for key := range selectionIDSet([]string{appID}) {
		if _, ok := selected[key]; ok {
			return true
		}
	}
	return false
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(firstString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return value
	}
	return cloned
}
