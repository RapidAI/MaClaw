package main

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// Temporary reproduction: parse the on-disk pdf-word package and compare the
// stored evidence definitionHash against canonical and candidate hashes.
func TestReproPdfWordDefinitionHash(t *testing.T) {
	path := `D:\workprj\aicoder\.tmp\pdf-word-pack.json`
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no local package: %v", err)
	}
	var pkg map[string]any
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for i, entry := range entries {
		governance := anyMap(entry.App["governance"])
		te := maclawAppTestEvidenceMap(governance)
		declared := maclawAppStringValue(te, "definitionHash", "definition_hash")
		canonical := maclawAppDefinitionFingerprintForEntry(entry)
		candidates := maclawAppDefinitionFingerprintCandidatesForEntry(entry)
		names := make([]string, 0, len(candidates))
		for h := range candidates {
			names = append(names, h)
		}
		sort.Strings(names)
		payload, _ := maclawAppStableJSON(maclawAppDefinitionFingerprintPayloadForEntry(entry))
		t.Logf("entry[%d] declared=%s canonical=%s candidates=%v", i, declared, canonical, names)
		t.Logf("payload=%s", payload)
		if declared == canonical {
			t.Logf("MATCH: canonical")
		} else if _, ok := candidates[declared]; ok {
			t.Logf("MATCH: candidate (not canonical)")
		} else {
			t.Logf("MISMATCH: declared matches nothing")
		}
	}
}
