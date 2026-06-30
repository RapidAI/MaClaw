package testfixtures

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	maclawappcontract "github.com/RapidAI/CodeClaw/internal/maclawappcontract"
)

// ReadyEnterpriseApprovalMaclawAppSubmitPackage returns a publish-ready
// enterprise approval MaClaw App package used by Hub and GUI integration tests.
func ReadyEnterpriseApprovalMaclawAppSubmitPackage() map[string]any {
	appSkillRef := "enterprise_hub://capabilities/cap-approval-ready-app-skill@1.0.0"
	workflowRef := "enterprise_hub://capabilities/cap-approval-ready-workflow@1.0.0"
	dependencyVerificationSkills := []any{
		map[string]any{"id": "approval-ready-app-skill", "version": "1.0.0", "kind": "app_skill", "source": "enterprise_hub", "install_ref": appSkillRef},
		map[string]any{"id": "approval-ready-workflow", "version": "1.0.0", "kind": "workflow_skill", "source": "enterprise_hub", "install_ref": workflowRef},
	}
	layout := map[string]any{
		"schema":             "maclaw.app.ui.v1",
		"entry":              "approval_workspace",
		"template":           "left_nav",
		"density":            "compact",
		"primaryRegion":      "center",
		"outputRegion":       "bottom",
		"regionCount":        4,
		"visibleRegionCount": 3,
		"regionIds":          []any{"approval_inbox", "request_form", "approval_detail", "result_panel"},
		"studio":             map[string]any{"editable": true, "savedInManifest": true, "updatedBy": "app_studio"},
		"regions": []any{
			map[string]any{"id": "approval_inbox", "role": "instance_list", "placement": "left", "order": 1},
			map[string]any{"id": "request_form", "role": "input", "placement": "center", "order": 2},
			map[string]any{"id": "approval_detail", "role": "detail", "placement": "right", "visible": false, "order": 3},
			map[string]any{"id": "result_panel", "role": "output", "placement": "bottom", "order": 4},
		},
	}
	layout["fingerprint"] = maclawappcontract.WorkspaceLayoutFingerprint("approval_workspace", layout)
	return map[string]any{
		"schema":        "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": []any{map[string]any{
			"schema":        "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": map[string]any{
				"id":      "approval-ready-app",
				"name":    "Approval Ready App",
				"kind":    "enterprise_approval_app",
				"version": "1.0.0",
				"ui": map[string]any{
					"schema":  "maclaw.app.ui.v1",
					"entry":   "approval_workspace",
					"layouts": map[string]any{"approval_workspace": cloneFixtureMap(layout)},
				},
				"binding": map[string]any{
					"appSkill": map[string]any{"id": "approval-ready-app-skill", "version": "1.0.0", "source": "enterprise_hub", "install_ref": appSkillRef},
					"datasrv":  map[string]any{"domain": "finance", "datasetID": "finance.expense_forms", "templateID": "finance.expense_form", "objectRole": "expense_request", "blueprintID": "finance.expense.v1"},
					"mis": map[string]any{"approvalBindings": []any{
						map[string]any{"event": "finance.submitted", "objectRole": "expense_request", "workflowSkillId": "approval-ready-workflow", "workflowVersion": "1.0.0"},
					}},
					"workflow": map[string]any{
						"schema":        "maclaw.app.workflow.v1",
						"submitNode":    "expense.submit",
						"approvalNode":  "expense.manager_review",
						"resultNode":    "expense.result",
						"attentionNode": "expense.attention",
					},
					"dependencies": map[string]any{"skills": []any{
						map[string]any{"id": "approval-ready-app-skill", "version": "1.0.0", "kind": "app_skill", "required": true, "source": "enterprise_hub", "install_ref": appSkillRef},
						map[string]any{"id": "approval-ready-workflow", "version": "1.0.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": workflowRef, "capabilities": []any{"approval.workflow"}},
					}},
					"ui": map[string]any{
						"schema":  "maclaw.app.ui.v1",
						"entry":   "approval_workspace",
						"layouts": map[string]any{"approval_workspace": cloneFixtureMap(layout)},
					},
					"resultContract": map[string]any{"schema": "maclaw.app.result.v1", "primary": "approval_result", "types": []any{"approval_result", "business_status", "content", "artifact"}, "delivery": map[string]any{"inlineContent": true, "artifacts": true}},
				},
				"governance": map[string]any{
					"workspaceLayout": cloneFixtureMap(layout),
					"resultContract":  map[string]any{"schema": "maclaw.app.result.v1", "primary": "approval_result", "types": []any{"approval_result", "business_status", "content", "artifact"}, "delivery": map[string]any{"inlineContent": true, "artifacts": true}},
					"workflowContract": map[string]any{
						"schema":          "maclaw.app.workflow_contract.v1",
						"workflowSkillId": "approval-ready-workflow",
						"workflowVersion": "1.0.0",
						"objectRole":      "expense_request",
						"requiredInputs":  []any{"record_ref", "applicant", "business_payload"},
						"decisionOutputs": []any{"approved", "rejected", "attention"},
						"requiredOutputs": []any{"workflow_result", "approval_instance", "outputs", "artifacts"},
						"statusMapping":   map[string]any{"pending": "finance_pending", "approved": "finance_approved", "rejected": "finance_rejected", "attention": "finance_attention"},
					},
					"testProtocol": map[string]any{
						"schema":         "maclaw.app.test_protocol.v1",
						"fingerprint":    "proto-ready-approval",
						"sampleInput":    map[string]any{"amount": 860, "applicant": "alice"},
						"expectedOutput": map[string]any{"approval_result": "approved", "business_status": "finance_approved"},
						"requiredRoles":  []any{"tester"},
						"requiredScopes": []any{"app.run"},
						"riskLevel":      "medium",
					},
					"testEvidence": map[string]any{
						"runId":                        "run-ready-approval",
						"testProtocol":                 map[string]any{"schema": "maclaw.app.test_protocol.v1", "fingerprint": "proto-ready-approval", "sampleInput": map[string]any{"amount": 860, "applicant": "alice"}, "expectedOutput": map[string]any{"approval_result": "approved", "business_status": "finance_approved"}, "requiredRoles": []any{"tester"}, "requiredScopes": []any{"app.run"}, "riskLevel": "medium"},
						"testProtocolFingerprint":      "proto-ready-approval",
						"approvalInstanceViewVerified": true,
						"primaryResult":                "approval_result",
						"resultPayload":                map[string]any{"approval_result": "approved", "business_status": "finance_approved", "business_record": map[string]any{"id": "expense-ready-1"}},
						"outputs":                      []any{map[string]any{"kind": "approval_result", "title": "Approved", "status": "approved"}},
						"artifacts":                    []any{map[string]any{"id": "approval-file", "uri": "artifact://approval/file.pdf", "name": "approval.pdf"}},
						"resultCoverage":               map[string]any{"ok": true, "primary": "approval_result", "coveredTypes": []any{"approval_result", "business_status", "content", "artifact"}, "missingTypes": []any{}},
						"approvalInstance": map[string]any{
							"instanceId":                   "wf-ready-approval-1",
							"approvalID":                   "approval-ready-1",
							"recordID":                     "expense-ready-1",
							"datasetID":                    "finance.expense_forms",
							"blueprintID":                  "finance.expense.v1",
							"objectRole":                   "expense_request",
							"approvalEvent":                "finance.submitted",
							"status":                       "approved",
							"currentNode":                  "expense.result",
							"workflowSkillId":              "approval-ready-workflow",
							"workflowVersion":              "1.0.0",
							"businessStatus":               "finance_approved",
							"resultStatus":                 "approved",
							"resultPayload":                map[string]any{"approval_result": "approved", "business_status": "finance_approved", "business_record": map[string]any{"id": "expense-ready-1"}},
							"outputs":                      []any{map[string]any{"kind": "approval_result", "title": "Approved", "text": "approved", "status": "approved"}},
							"artifacts":                    []any{map[string]any{"id": "approval-file", "uri": "artifact://approval/file.pdf", "name": "approval.pdf", "status": "ready"}},
							"approvalInstanceViewVerified": true,
							"viewVerified":                 true,
						},
					},
					"dependencyVerification": map[string]any{
						"schema":          "maclaw.app.install_plan.v1",
						"runId":           "dep-run-ready-approval",
						"dependencyCount": 2,
						"requiredCount":   2,
						"installedCount":  2,
						"missingCount":    0,
						"blockedCount":    0,
						"ok":              true,
						"blocked":         false,
						"skills":          cloneFixtureSlice(dependencyVerificationSkills),
						"dependencies":    cloneFixtureSlice(dependencyVerificationSkills),
					},
				},
			},
		}},
	}
}

// ReadyEnterpriseApprovalMaclawAppPublishedHubPackage returns the same approval
// app fixture shaped as a Hub package download after review and publish.
func ReadyEnterpriseApprovalMaclawAppPublishedHubPackage(capabilityID, versionKey string) map[string]any {
	if capabilityID == "" {
		capabilityID = "cap-approval-ready-app"
	}
	if versionKey == "" {
		versionKey = "enterprise_hub:skill:maclaw-app:approval-ready-app@pkg"
	}
	pkg := ReadyEnterpriseApprovalMaclawAppSubmitPackage()
	ApplyPublishedMaclawAppHubDownloadGovernance(pkg, "approval-ready-app", capabilityID, versionKey, nil)
	return pkg
}

// ApplyPublishedMaclawAppHubDownloadGovernance annotates a MaClaw App package
// with the published Hub metadata that GUI download/install tests must trust.
func ApplyPublishedMaclawAppHubDownloadGovernance(pkg map[string]any, appID, capabilityID, versionKey string, reviewEvidence map[string]any) {
	if pkg == nil {
		return
	}
	if appID == "" {
		appID = "approval-ready-app"
	}
	if capabilityID == "" {
		capabilityID = "cap-" + appID
	}
	if versionKey == "" {
		versionKey = "enterprise_hub:skill:maclaw-app:" + appID + "@pkg"
	}
	if reviewEvidence == nil {
		reviewEvidence = map[string]any{
			appID: map[string]any{
				"run_id":                        "run-ready-approval",
				"test_protocol_fingerprint":     "proto-ready-approval",
				"result_contract_primary":       "approval_result",
				"result_coverage_primary":       "approval_result",
				"result_coverage_covered_count": 3,
				"result_coverage_missing_count": 0,
				"output_count":                  1,
				"artifact_count":                1,
				"approval_status":               "approved",
				"current_node":                  "expense.result",
				"has_workspace_layout":          true,
				"has_dependency_verification":   true,
				"has_blocking_dependency":       false,
			},
		}
	}
	pkg["source"] = "enterprise_hub"
	pkg["capability_id"] = capabilityID
	pkg["capability"] = map[string]any{
		"id":                  capabilityID,
		"capability_id":       appID,
		"display_name":        appID,
		"status":              "published",
		"current_version_key": versionKey,
	}
	pkg["review_evidence"] = reviewEvidence
	pkg["maclaw_app_review_evidence"] = reviewEvidence
	resolvedDependencies := []any{
		map[string]any{"id": "approval-ready-app-skill", "version": "1.0.0", "kind": "app_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-approval-ready-app-skill@1.0.0", "app_ids": []string{appID}},
		map[string]any{"id": "approval-ready-workflow", "version": "1.0.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-approval-ready-workflow@1.0.0", "capabilities": []any{"approval.workflow"}, "app_ids": []string{appID}},
	}
	pkg["resolved_dependencies"] = resolvedDependencies

	apps, _ := pkg["apps"].([]any)
	if len(apps) == 0 {
		return
	}
	entry, _ := apps[0].(map[string]any)
	entry["resolved_dependencies"] = resolvedDependencies
	app, _ := entry["app"].(map[string]any)
	if app == nil {
		return
	}
	governance, _ := app["governance"].(map[string]any)
	if governance == nil {
		governance = map[string]any{}
		app["governance"] = governance
	}
	governance["submission"] = map[string]any{
		"schema":          "maclaw.app.hub_submission.v1",
		"status":          "published",
		"capability_id":   capabilityID,
		"version_key":     versionKey,
		"submitted_at":    "2026-07-01T00:50:00Z",
		"approved_at":     "2026-07-01T00:55:00Z",
		"published_at":    "2026-07-01T01:00:00Z",
		"review_evidence": reviewEvidence,
	}
}

// SignPublishedMaclawAppHubPackage adds a deterministic Hub package signature
// and mirrors it into the first app entry submission metadata.
func SignPublishedMaclawAppHubPackage(pkg map[string]any, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey, packageSHA, versionKey, signedAt, signedBy string) map[string]any {
	if pkg == nil || len(publicKey) == 0 || len(privateKey) == 0 {
		return nil
	}
	if packageSHA == "" {
		packageSHA = "test-package-sha256"
	}
	if versionKey == "" {
		if capability, _ := pkg["capability"].(map[string]any); capability != nil {
			if value, _ := capability["current_version_key"].(string); value != "" {
				versionKey = value
			}
		}
	}
	if signedAt == "" {
		signedAt = "2026-07-01T01:00:00Z"
	}
	if signedBy == "" {
		signedBy = "hub-admin"
	}
	payload := "maclaw-app\n" + packageSHA + "\n" + versionKey + "\n" + signedAt + "\n" + signedBy
	signature := map[string]any{
		"schema":                 "maclaw.app.package_signature.v1",
		"algorithm":              "ed25519",
		"payload":                payload,
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": MaclawAppHubPackagePublicKeyFingerprint(publicKey),
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(payload))),
		"package_sha256":         packageSHA,
		"version_key":            versionKey,
		"signed_at":              signedAt,
		"signed_by":              signedBy,
	}
	pkg["package_sha256"] = packageSHA
	pkg["package_signature"] = signature
	if submission := firstMaclawAppSubmission(pkg); submission != nil {
		submission["package_sha256"] = packageSHA
		submission["package_signature"] = signature
	}
	return signature
}

// MaclawAppHubPackagePublicKeyFingerprint matches GUI package trust checks.
func MaclawAppHubPackagePublicKeyFingerprint(publicKey []byte) string {
	return maclawappcontract.HubPackagePublicKeyFingerprint(publicKey)
}

// SignedEnterpriseHubSkillPackage returns a signed Skill JSON package body and
// signature metadata compatible with GUI Enterprise Hub dependency installs.
func SignedEnterpriseHubSkillPackage(id, version, instructions string, publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) ([]byte, string, string, error) {
	if version == "" {
		version = "1.0.0"
	}
	if instructions == "" {
		instructions = "run enterprise dependency"
	}
	body, err := json.Marshal(map[string]any{
		"id":          id,
		"name":        id,
		"description": "signed " + id,
		"version":     version,
		"steps":       []any{map[string]any{"action": "craft_tool", "params": map[string]any{"instructions": instructions}}},
		"files":       map[string]any{"SKILL.md": "IyBTaWduZWQgU2tpbGwK"},
	})
	if err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	signature := map[string]any{
		"algorithm":              "ed25519",
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"public_key_fingerprint": maclawappcontract.HubPackagePublicKeyFingerprint(publicKey),
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, body)),
		"package_sha256":         sha,
	}
	signatureJSON, err := json.Marshal(signature)
	if err != nil {
		return nil, "", "", err
	}
	return body, sha, string(signatureJSON), nil
}

// PublishedEnterpriseHubSkillCapability returns a capability summary JSON object
// for a published signed Skill dependency.
func PublishedEnterpriseHubSkillCapability(skillID, capabilityID, version, packageSHA, packageSignature string) map[string]any {
	if capabilityID == "" {
		capabilityID = "cap-" + skillID
	}
	if version == "" {
		version = "1.0.0"
	}
	metadata, _ := json.Marshal(map[string]any{
		"skill_id":          skillID,
		"hub_skill_id":      skillID,
		"package_sha256":    packageSHA,
		"package_signature": packageSignature,
	})
	return map[string]any{
		"id":                  capabilityID,
		"capability_id":       skillID,
		"capability_type":     "skill",
		"status":              "published",
		"current_version_key": version,
		"package_sha256":      packageSHA,
		"package_signature":   packageSignature,
		"metadata_json":       string(metadata),
	}
}

func firstMaclawAppSubmission(pkg map[string]any) map[string]any {
	apps, _ := pkg["apps"].([]any)
	if len(apps) == 0 {
		return nil
	}
	entry, _ := apps[0].(map[string]any)
	app, _ := entry["app"].(map[string]any)
	governance, _ := app["governance"].(map[string]any)
	submission, _ := governance["submission"].(map[string]any)
	return submission
}

func cloneFixtureMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneFixtureValue(value)
	}
	return out
}

func cloneFixtureSlice(in []any) []any {
	if in == nil {
		return nil
	}
	out := make([]any, len(in))
	for i, value := range in {
		out[i] = cloneFixtureValue(value)
	}
	return out
}

func cloneFixtureValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneFixtureMap(typed)
	case []any:
		return cloneFixtureSlice(typed)
	default:
		return typed
	}
}
