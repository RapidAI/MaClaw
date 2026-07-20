package main

import (
	"strings"
	"testing"
)

// TestMaclawAppAssembledPackagePassesGovernanceGate locks in the cross-side
// evidence contract: a tool-app package assembled the way the GUI publishes it
// (frontend-shaped governance.testEvidence + dependencyVerification, with
// fingerprints computed by the shared algorithms) must pass the backend
// governance review with zero error-severity issues. This guards against
// fingerprint/shape drift between the App Studio package builder and the
// backend publish gate.
func TestMaclawAppAssembledPackagePassesGovernanceGate(t *testing.T) {
	app := map[string]any{
		"id":          "app-pdf",
		"name":        "PDF翻译工具",
		"description": "使用 paper_pdf_translator skill 生成 PDF 翻译工具。",
		"category":    "文档处理",
		"kind":        "tool_app",
		"icon":        "pdf",
		"version":     9,
		"launchMode":  "fixed_skill_ui",
		"binding": map[string]any{
			"skill": map[string]any{
				"id":                "paper_pdf_translator",
				"appDefinitionFile": "maclaw.app.json",
				"inputMode":         "file",
				"multipleFiles":     false,
				"outputModes":       []any{"pdf"},
				"fields":            []any{},
			},
			"appSkill": map[string]any{"id": "paper_pdf_translator", "source": "local", "version": "1.0.0"},
			"resultContract": map[string]any{
				"schema":      "maclaw.app.result.v1",
				"primary":     "artifact",
				"types":       []any{"content", "document", "artifact"},
				"outputModes": []any{"pdf"},
				"delivery":    map[string]any{"inlineContent": true, "artifacts": true, "businessRecord": false, "notifications": false},
			},
			"testProtocol": map[string]any{
				"schema":         "maclaw.app.test_protocol.v1",
				"sampleInput":    map[string]any{"file": "sample.pdf", "params": ""},
				"expectedOutput": map[string]any{"primary": "artifact", "status": "ok"},
				"requiredRoles":  []any{},
				"requiredScopes": []any{},
				"riskLevel":      "low",
			},
			"ui": map[string]any{
				"schema": "maclaw.app.ui.v1",
				"entry":  "tool_workspace",
				"layouts": map[string]any{
					"tool_workspace": map[string]any{
						"template":      "document_workspace",
						"density":       "comfortable",
						"primaryRegion": "left",
						"outputRegion":  "right",
						"regions": []any{
							map[string]any{"id": "file_queue", "role": "input", "placement": "left", "order": 1},
							map[string]any{"id": "settings_panel", "role": "parameters", "placement": "right", "order": 2},
							map[string]any{"id": "preview_panel", "role": "preview", "placement": "center", "order": 3},
							map[string]any{"id": "output_panel", "role": "output", "placement": "right", "order": 4},
						},
					},
				},
			},
		},
	}
	pkg := map[string]any{
		"schema":        "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit":   "skill",
		"app":           app,
	}
	pack := map[string]any{
		"schema":        "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps":          []any{pkg},
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(pack, true)
	if err != nil {
		t.Fatalf("parse entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	protocol := entry.App["binding"].(map[string]any)["testProtocol"].(map[string]any)
	protocolFingerprint := maclawAppTestProtocolFingerprint(protocol)
	layout := maclawAppUILayoutForEntry(anyMap(app["ui"]), "tool_workspace")
	layoutFingerprint := maclawAppWorkspaceLayoutFingerprint("tool_workspace", layout)
	// Stamp declared fingerprints exactly like the GUI does at save time.
	protocol["fingerprint"] = protocolFingerprint
	layout["fingerprint"] = layoutFingerprint
	// The GUI computes the definition hash over the stamped manifest (save
	// writes both fingerprints before any run records evidence), so the hash
	// must be taken after the stamps land, not before.
	definitionHash := maclawAppDefinitionFingerprintForEntry(entry)
	if definitionHash == "" {
		t.Fatalf("definition fingerprint should be computable")
	}

	governance := map[string]any{
		"status":         "local_tested",
		"riskLevel":      "low",
		"requiredScopes": []any{"paper_pdf_translator"},
		"dependencies": map[string]any{
			"installPolicy": "install_on_app_install",
			"requiredCount": 1,
			"optionalCount": 0,
			"skills": []any{
				map[string]any{"id": "paper_pdf_translator", "version": "1.0.0", "kind": "app_skill", "required": true, "source": "local"},
			},
		},
		"dependencyVerification": map[string]any{
			"schema":                   "maclaw.app.install_plan.v1",
			"verifiedAt":               "2026-07-19T14:00:00Z",
			"appCount":                 1,
			"dependencyCount":          1,
			"hasMissingRequired":       false,
			"hasBlockingDependency":    false,
			"hasWorkflowContractIssue": false,
			"hasGovernanceReviewIssue": false,
			"dependencies": []any{
				map[string]any{
					"id": "paper_pdf_translator", "version": "1.0.0", "kind": "app_skill",
					"required": true, "source": "local", "installed": true,
					"health": "ready", "action": "skip", "app_ids": []any{"app-pdf"},
				},
			},
		},
		"workspaceLayout": map[string]any{
			"schema": "maclaw.app.ui.v1", "entry": "tool_workspace", "template": "document_workspace",
			"density": "comfortable", "primaryRegion": "left", "outputRegion": "right",
			"regionCount": 4, "visibleRegionCount": 4,
			"regionIds": []any{"file_queue", "settings_panel", "preview_panel", "output_panel"},
			// The frontend summary (appWorkspaceLayoutEvidence) also carries the
			// ordered regions; the backend recomputes the fingerprint from them.
			"regions":         layout["regions"],
			"fingerprint":     layoutFingerprint,
			"savedInManifest": true,
		},
		"resultContract": app["binding"].(map[string]any)["resultContract"],
		"testProtocol":   protocol,
		"testEvidence": map[string]any{
			"runId":                             "run-probe-1",
			"verifiedAt":                        "2026-07-19T14:00:00Z",
			"definitionHash":                    definitionHash,
			"testProtocol":                      protocol,
			"testProtocolFingerprint":           protocolFingerprint,
			"workspaceLayoutFingerprint":        layoutFingerprint,
			"currentWorkspaceLayoutFingerprint": layoutFingerprint,
			"artifactPresent":                   true,
			"artifactCount":                     1,
			"artifactName":                      "translated.pdf",
			"artifacts": []any{
				map[string]any{"id": "artifact-probe-1", "uri": "artifact://skill-run/run-probe-1/translated.pdf", "name": "translated.pdf", "status": "ready"},
			},
			"outputCount":       1,
			"outputs":           []any{map[string]any{"kind": "artifact", "title": "Translated PDF", "text": "translated.pdf", "artifactId": "artifact-probe-1", "status": "ready"}},
			"resultPayload":     map[string]any{"content": "ok", "artifact_id": "artifact-probe-1"},
			"primaryResult":     "ok",
			"resultCoverage":    map[string]any{"ok": true, "primary": "artifact", "coveredTypes": []any{"content", "document", "artifact"}, "missingTypes": []any{}},
			"designConsistency": map[string]any{"ok": true},
		},
	}
	app["governance"] = governance

	issues := maclawAppGovernanceReviewIssuesFromPackage(pack)
	for _, issue := range issues {
		if strings.EqualFold(issue.Severity, "error") {
			t.Errorf("unexpected gate error: %s: %s (suggestion: %s)", issue.Path, issue.Message, issue.Suggestion)
		}
	}
}
