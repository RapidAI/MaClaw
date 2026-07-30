package main

import (
	"archive/zip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"gopkg.in/yaml.v3"
)

const maclawAppOneClickPublishTimeout = 3 * time.Minute

const maclawAppHubTLSPreflightCacheTTL = 20 * time.Second

type maclawAppHubTLSPreflightCacheEntry struct {
	err       error
	expiresAt time.Time
	done      chan struct{}
}

var (
	maclawAppHubTLSPreflightMu    sync.Mutex
	maclawAppHubTLSPreflightCache = map[string]*maclawAppHubTLSPreflightCacheEntry{}
	maclawAppHubTLSCheck          = maclawAppHubTLSPreflight
)

// ReviewMaclawAppPackage is the single authority for App Studio's local
// publish review. It resolves a package's evidence from the durable run store
// before applying the same governance rules enforced at submission time.
//
// The browser must not reconstruct this decision from its UI cache: runtime
// panel IDs and persisted Skill-definition IDs are aliases of the same app,
// and only the backend has the complete identity and fingerprint rules.
func (a *App) ReviewMaclawAppPackage(packageJSON string) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	packageJSON = strings.TrimSpace(packageJSON)
	if packageJSON == "" {
		return nil, fmt.Errorf("package payload is empty")
	}
	pkg, _, _, err := parseMaclawAppPackage(packageJSON)
	if err != nil {
		return map[string]any{
			"schema":        "maclaw.app.review.v1",
			"ok":            false,
			"hydrated":      false,
			"review_issues": []maclawAppReviewIssue{{Path: "package", Severity: "error", Message: err.Error()}},
		}, nil
	}

	hydrated := a.hydrateMaclawAppPackageRunEvidence(pkg)
	if hydrated {
		if packageJSON, err = maclawAppStableJSON(pkg); err != nil {
			return nil, err
		}
	}
	plan, planErr := a.PlanMaclawAppInstall(packageJSON)
	issues := []maclawAppReviewIssue{}
	if planErr != nil {
		issues = append(issues, maclawAppReviewIssue{
			Path: "package.installPlan", Severity: "error", Message: planErr.Error(),
			Suggestion: "resolve the package dependency plan before publishing",
		})
	} else {
		issues = maclawAppReadyReviewIssuesForPackage(pkg, plan)
	}
	issues = normalizeMaclawAppReviewIssues(issues)
	return map[string]any{
		"schema":        "maclaw.app.review.v1",
		"ok":            firstBlockingMaclawAppReviewIssue(issues) == nil,
		"hydrated":      hydrated,
		"package_json":  packageJSON,
		"review_issues": issues,
	}, nil
}

// PreflightMaclawAppOneClickPublish inspects a package JSON without publishing.
// Returns structured checks for local queue readiness and remote targets so the
// UI can warn before the user starts a long one-click run.
func (a *App) PreflightMaclawAppOneClickPublish(packageJSON string) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	packageJSON = strings.TrimSpace(packageJSON)
	if packageJSON == "" {
		return nil, fmt.Errorf("package payload is empty")
	}
	pkg, _, _, err := parseMaclawAppPackage(packageJSON)
	if err != nil {
		return a.buildMaclawAppOneClickPreflight(packageJSON, nil, err), nil
	}
	// The review UI owns the authoritative durable run-history store. Older
	// frontend snapshots could build a package before that history hydration
	// completed, producing the contradictory state "10/10 passed" locally but
	// "missing run evidence" here. Reconcile the package snapshot from the
	// durable store before running the same package governance checks. Only an
	// exact definition/protocol/workspace match is accepted, so stale evidence
	// still fails closed.
	if a.hydrateMaclawAppPackageRunEvidence(pkg) {
		if hydratedJSON, marshalErr := maclawAppStableJSON(pkg); marshalErr == nil {
			packageJSON = hydratedJSON
		}
	}
	return a.buildMaclawAppOneClickPreflight(packageJSON, pkg, nil), nil
}

// PreflightMaclawAppSubmissionOneClick inspects an existing local-queue row.
func (a *App) PreflightMaclawAppSubmissionOneClick(submissionID string) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return nil, fmt.Errorf("submission id is required")
	}
	record, err := a.GetMaclawAppPackageSubmission(submissionID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("submission %s not found", submissionID)
	}
	if record.Package == nil {
		return nil, fmt.Errorf("submission %s has no package payload", submissionID)
	}
	packageJSON, err := maclawAppStableJSON(record.Package)
	if err != nil {
		return nil, err
	}
	out := a.buildMaclawAppOneClickPreflight(packageJSON, record.Package, nil)
	out["submission_id"] = record.SubmissionID
	out["channel"] = record.Channel
	out["status"] = record.Status
	return out, nil
}

// hydrateMaclawAppPackageRunEvidence reconciles governance.testEvidence with
// the authoritative durable run-history store. A package assembled by the UI
// may still contain a sparse/stale Skill governance stamp or omit evidence due
// to a local cache race. When a durable run proves this exact definition,
// protocol and workspace, it is always the stronger source and replaces that
// package snapshot. Stale durable records still fail closed below.
func (a *App) hydrateMaclawAppPackageRunEvidence(pkg map[string]any) bool {
	if a == nil || pkg == nil {
		return false
	}
	entries, err := parseMaclawAppPackageEntriesFromMap(pkg, false)
	if err != nil || len(entries) == 0 {
		return false
	}
	identitySets := make([]map[string]struct{}, len(entries))
	allIdentities := map[string]struct{}{}
	for i, entry := range entries {
		identitySets[i] = maclawAppRunEvidenceIdentityKeys(entry)
		for identity := range identitySets[i] {
			allIdentities[identity] = struct{}{}
		}
	}
	history, err := a.listMaclawAppRunHistoryMatchingIdentities(allIdentities)
	if err != nil || len(history) == 0 {
		return false
	}
	changed := false
	for i, entry := range entries {
		governance := anyMap(entry.App["governance"])
		if governance == nil {
			continue
		}
		identities := identitySets[i]
		definitionHashes := maclawAppDefinitionFingerprintCandidatesForEntry(entry)
		workspaceHash := maclawAppCurrentWorkspaceLayoutFingerprint(entry, governance)
		protocol := maclawAppTestProtocolMap(governance, nil)
		protocolHash := maclawAppTestProtocolFingerprint(protocol)
		for _, run := range history {
			if !maclawAppRunEvidenceMatchesCurrentDefinition(run, identities, definitionHashes, workspaceHash, protocolHash) {
				continue
			}
			evidence := maclawAppRunHistoryTestEvidence(run)
			if protocol != nil {
				evidence["testProtocol"] = cloneMapAny(protocol)
			}
			governance["testEvidence"] = evidence
			delete(governance, "test_evidence")
			changed = true
			break
		}
	}
	return changed
}

// listMaclawAppRunHistoryMatchingIdentities reads the durable store once and
// returns every run for the requested app/Skill aliases, newest first. It does
// not use ListAllMaclawAppRunHistory's UI-oriented global limit: a valid run
// must not disappear merely because other apps produced more than 200 newer
// history rows.
func (a *App) listMaclawAppRunHistoryMatchingIdentities(identities map[string]struct{}) ([]maclawAppRunHistoryEntry, error) {
	if a == nil || len(identities) == 0 {
		return []maclawAppRunHistoryEntry{}, nil
	}
	maclawAppRunHistoryMu.RLock()
	defer maclawAppRunHistoryMu.RUnlock()
	store, err := a.readMaclawAppRunHistoryStore()
	if err != nil {
		return nil, err
	}
	matches := make([]maclawAppRunHistoryEntry, 0, 8)
	for appID, list := range store.ByApp {
		_, bucketMatches := identities[strings.ToLower(strings.TrimSpace(appID))]
		for _, run := range list {
			if strings.TrimSpace(run.AppID) == "" {
				run.AppID = appID
			}
			if !bucketMatches && !maclawAppRunEvidenceIdentityMatches(run, identities) {
				continue
			}
			matches = append(matches, run)
		}
	}
	sortMaclawAppRunHistoryNewestFirst(matches)
	return matches, nil
}

func maclawAppRunEvidenceIdentityKeys(entry parsedMaclawAppEntry) map[string]struct{} {
	keys := map[string]struct{}{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			keys[value] = struct{}{}
		}
	}
	add(entry.ID)
	binding := anyMap(entry.App["binding"])
	skill := anyMap(binding["skill"])
	appSkill := anyMap(binding["appSkill"])
	if appSkill == nil {
		appSkill = anyMap(binding["app_skill"])
	}
	for _, value := range []string{
		maclawAppStringValue(skill, "id"),
		maclawAppStringValue(appSkill, "id"),
	} {
		add(value)
		if value != "" && entry.ID != "" {
			add("skill-app-" + value + "-" + entry.ID)
		}
	}
	return keys
}

func maclawAppRunEvidenceIdentityMatches(run maclawAppRunHistoryEntry, keys map[string]struct{}) bool {
	for _, value := range []string{run.AppID, run.SkillName} {
		if _, ok := keys[strings.ToLower(strings.TrimSpace(value))]; ok {
			return true
		}
	}
	return false
}

func maclawAppRunEvidenceSuccessful(run maclawAppRunHistoryEntry) bool {
	status := strings.ToLower(strings.TrimSpace(run.Status))
	return status == "done" || status == "success" || status == "completed"
}

func maclawAppRunEvidenceMatchesCurrentDefinition(run maclawAppRunHistoryEntry, identities, definitionHashes map[string]struct{}, workspaceHash, protocolHash string) bool {
	if !maclawAppRunEvidenceSuccessful(run) || !maclawAppRunEvidenceIdentityMatches(run, identities) {
		return false
	}
	// Durable evidence is authoritative only when it proves the exact package
	// definition being preflighted. Missing stamps are not guessed and stale
	// stamps are never upgraded into current evidence.
	if _, ok := definitionHashes[strings.TrimSpace(run.DefinitionHash)]; len(definitionHashes) == 0 || !ok {
		return false
	}
	if workspaceHash != "" && strings.TrimSpace(run.WorkspaceLayoutFingerprint) != workspaceHash {
		return false
	}
	if protocolHash != "" && strings.TrimSpace(run.TestProtocolFingerprint) != protocolHash {
		return false
	}
	return true
}

func maclawAppRunHistoryTestEvidence(run maclawAppRunHistoryEntry) map[string]any {
	artifacts := append([]any(nil), run.Artifacts...)
	if len(artifacts) == 0 && (run.ArtifactName != "" || run.ArtifactPath != "" || run.ArtifactURI != "") {
		artifacts = []any{map[string]any{
			"id": run.ArtifactID, "uri": run.ArtifactURI, "name": run.ArtifactName,
			"path": run.ArtifactPath, "download_state": run.ArtifactDownloadState,
		}}
	}
	evidence := compactPayload(map[string]any{
		"runId": run.RunID, "definitionHash": run.DefinitionHash,
		"testProtocolFingerprint":    run.TestProtocolFingerprint,
		"workspaceLayoutFingerprint": run.WorkspaceLayoutFingerprint,
		"artifactPresent":            len(artifacts) > 0, "artifactCount": len(artifacts),
		"artifactName": run.ArtifactName, "artifacts": artifacts,
		"outputCount": len(run.Outputs), "outputs": append([]any(nil), run.Outputs...),
		"resultPayload": cloneMapAny(run.ResultPayload), "resultCoverage": cloneMapAny(run.ResultCoverage),
		"dependencyVerification": cloneMapAny(run.DependencyVerification),
		"approvalInstance":       cloneMapAny(run.ApprovalInstance), "verifiedAt": run.At,
	})
	return evidence
}

// PublishMaclawAppOneClick is the GUI one-click publish:
//  1. queue a local maclaw.app.pack.v1 submission
//  2. upload a skill-market package to HubCenter (and enterprise skill API if policy says so)
//  3. sync the pack to enterprise Hub when marketplace URL+token are configured
//
// Skill-market runs before the enterprise Hub pack so dependency skills can obtain
// HubSkillID / market refs before the publish gate (bundled deps also satisfy the gate).
// Partial success is returned as a structured map rather than a hard error whenever
// the local queue step succeeds.
func (a *App) PublishMaclawAppOneClick(packageJSON string) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	local, err := a.SubmitMaclawAppPackage(packageJSON)
	if err != nil {
		return nil, err
	}
	submissionID := strings.TrimSpace(stringFromAnyMap(local, "submission_id"))
	if submissionID == "" {
		return nil, fmt.Errorf("local submission id missing after queue")
	}
	ctx, cancel := context.WithTimeout(context.Background(), maclawAppOneClickPublishTimeout)
	defer cancel()
	// finish reloads durable package (enriched) for skill-market materialization.
	return a.finishMaclawAppOneClickPublish(ctx, submissionID, packageJSON, local)
}

// PublishMaclawAppSubmissionOneClick continues one-click publish for an existing
// local queue submission (sync to Hub + upload skill-market targets).
func (a *App) PublishMaclawAppSubmissionOneClick(submissionID string) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	submissionID = strings.TrimSpace(submissionID)
	if submissionID == "" {
		return nil, fmt.Errorf("submission id is required")
	}
	record, err := a.GetMaclawAppPackageSubmission(submissionID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("submission %s not found", submissionID)
	}
	if record.Package == nil {
		return nil, fmt.Errorf("submission %s has no package payload", submissionID)
	}
	packageJSON, err := maclawAppStableJSON(record.Package)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(packageJSON) == "" {
		return nil, fmt.Errorf("submission %s has empty package payload", submissionID)
	}
	// Reject terminal states that should not be re-driven.
	switch strings.TrimSpace(strings.ToLower(record.Status)) {
	case "revoked", "deprecated", "published":
		return nil, fmt.Errorf("submission %s is %s and cannot be one-click published", submissionID, record.Status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), maclawAppOneClickPublishTimeout)
	defer cancel()
	return a.finishMaclawAppOneClickPublish(ctx, submissionID, packageJSON, maclawAppSubmissionLocalMap(record))
}

func (a *App) finishMaclawAppOneClickPublish(ctx context.Context, submissionID, packageJSON string, local map[string]any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if local == nil {
		local = map[string]any{}
	}
	out := map[string]any{
		"schema":              "maclaw.app.one_click_publish.v1",
		"local_submission":    local,
		"local_submission_id": submissionID,
		"targets":             map[string]any{},
	}
	targets := out["targets"].(map[string]any)

	// Capture durable package (resolved/bundled stamps) before any remote rewrite.
	record, _ := a.GetMaclawAppPackageSubmission(submissionID)
	channel := strings.TrimSpace(stringFromAnyMap(local, "channel"))
	if record != nil {
		if ch := strings.TrimSpace(record.Channel); ch != "" {
			channel = ch
		}
		if record.Package != nil {
			if raw, err := maclawAppStableJSON(record.Package); err == nil {
				if q := strings.TrimSpace(raw); q != "" {
					packageJSON = q
				}
			}
		}
	}

	// 1) Skill-market first so dependency skills get HubSkillID / market refs
	// before the enterprise App pack gate runs (avoids chicken-and-egg).
	// Skip re-upload when the primary skill is already published (common on
	// partial retry after skill market succeeded and only Hub pack failed).
	if err := ctx.Err(); err != nil {
		targets["skill_market"] = maclawAppOneClickTargetFail(err)
	} else if strings.TrimSpace(packageJSON) == "" {
		targets["skill_market"] = maclawAppOneClickTargetFail(fmt.Errorf("package payload is empty"))
	} else if a.maclawAppOneClickCanSkipSkillMarket(packageJSON) {
		targets["skill_market"] = map[string]any{
			"ok":      true,
			"skipped": true,
			"reason":  "primary_skill_already_published",
		}
	} else {
		marketID, marketErr := a.uploadMaclawAppPackageToSkillMarkets(ctx, packageJSON)
		if marketErr != nil {
			targets["skill_market"] = maclawAppOneClickTargetFail(marketErr)
		} else {
			targets["skill_market"] = map[string]any{"ok": true, "submission_id": marketID}
		}
	}

	// Best-effort: always re-stamp resolved_dependencies before Hub pack so
	// MarkUploaded / prior HubSkillIDs surface even when skill-market was
	// skipped or failed (partial retry, offline market, etc.).
	// Skip when the one-click deadline is already exhausted — stamp is local but
	// PlanMaclawAppInstall can still be expensive.
	if err := ctx.Err(); err != nil {
		if t, ok := targets["skill_market"].(map[string]any); ok {
			t["stamp_skipped"] = err.Error()
		}
	} else if stamped, stampErr := a.refreshMaclawAppSubmissionPublishStamps(submissionID); stampErr != nil {
		log.Printf("[maclaw-app] refresh publish stamps before hub pack: %v", stampErr)
		if t, ok := targets["skill_market"].(map[string]any); ok {
			t["stamp_error"] = stampErr.Error()
		}
	} else if stamped > 0 {
		if t, ok := targets["skill_market"].(map[string]any); ok {
			t["stamped_deps"] = stamped
		}
	}

	// 2) Enterprise Hub pack (maclaw-apps/submit). Bundled deps are accepted by
	// the publish gate; HubSkillIDs / resolved install_refs from step 1 also
	// satisfy non-bundled deps.
	if err := ctx.Err(); err != nil {
		targets["enterprise_hub_pack"] = maclawAppOneClickTargetFail(err)
	} else if channel == "" || channel == "local" {
		targets["enterprise_hub_pack"] = a.syncMaclawAppOneClickHubPack(ctx, submissionID, targets)
	} else {
		targets["enterprise_hub_pack"] = map[string]any{
			"ok":      true,
			"skipped": true,
			"reason":  "already_" + channel,
		}
	}

	// Hub sync may rewrite the durable submission_id to the hub id.
	effectiveID := a.resolveMaclawAppSubmissionIDAfterTargets(submissionID, targets)
	partial := maclawAppOneClickTargetsPartial(targets)
	message := summarizeMaclawAppOneClickPublish(targets)
	// Message-only stamp: no status/event/reviewed_at side effects.
	a.stampMaclawAppOneClickMessage(effectiveID, message)

	if refreshed := a.maclawAppSubmissionSummaryMap(effectiveID); refreshed != nil {
		refreshed["message"] = message
		out["local_submission"] = refreshed
		if id := strings.TrimSpace(stringFromAnyMap(refreshed, "submission_id")); id != "" {
			out["local_submission_id"] = id
		}
	} else if localMap, ok := out["local_submission"].(map[string]any); ok {
		localMap["message"] = message
		// Keep response id aligned with effective durable id when summary reload fails.
		if effectiveID != "" {
			localMap["submission_id"] = effectiveID
			out["local_submission_id"] = effectiveID
		}
	}

	// Local queue already succeeded; remote targets may partially fail.
	out["ok"] = true
	out["partial"] = partial
	out["published_at"] = time.Now().UTC().Format(time.RFC3339)
	out["message"] = message
	// Attach post-run preflight snapshot for UI diagnostics.
	if rec, _ := a.GetMaclawAppPackageSubmission(effectiveID); rec != nil && rec.Package != nil {
		if raw, err := maclawAppStableJSON(rec.Package); err == nil {
			out["preflight"] = a.buildMaclawAppOneClickPreflight(raw, rec.Package, nil)
		}
	} else {
		out["preflight"] = a.buildMaclawAppOneClickPreflight(packageJSON, nil, nil)
	}
	return out, nil
}

// resolveMaclawAppSubmissionIDAfterTargets prefers the hub-rewritten submission id
// when SyncMaclawAppPackageSubmissionToHub renames the durable queue entry.
func (a *App) resolveMaclawAppSubmissionIDAfterTargets(originalID string, targets map[string]any) string {
	originalID = strings.TrimSpace(originalID)
	if a == nil {
		return originalID
	}
	if t, ok := targets["enterprise_hub_pack"].(map[string]any); ok && t["ok"] == true {
		if res, ok := t["result"].(map[string]any); ok {
			// Prefer hub submission id, then source id (pre-rename local id).
			for _, key := range []string{"submission_id", "source_submission_id"} {
				if cand := strings.TrimSpace(stringFromAnyMap(res, key)); cand != "" {
					if rec, _ := a.GetMaclawAppPackageSubmission(cand); rec != nil {
						return rec.SubmissionID
					}
				}
			}
		}
	}
	if rec, _ := a.GetMaclawAppPackageSubmission(originalID); rec != nil {
		return rec.SubmissionID
	}
	return originalID
}

func maclawAppOneClickTargetsPartial(targets map[string]any) bool {
	for _, key := range []string{"enterprise_hub_pack", "skill_market"} {
		t, ok := targets[key].(map[string]any)
		if !ok {
			continue
		}
		if t["ok"] != true {
			return true
		}
	}
	return false
}

// maclawAppOneClickCanSkipSkillMarket reports whether the primary app skill is
// already published (HubSkillID present). Used to avoid re-uploading on partial
// retries when only the enterprise Hub pack still needs to succeed.
//
// Packs without a bound primary skill still use the materialize-to-market path
// (uploadMaclawAppPackAsSkillMarketZip), so they must NOT skip.
func (a *App) maclawAppOneClickCanSkipSkillMarket(packageJSON string) bool {
	if a == nil {
		return false
	}
	skillID := strings.TrimSpace(maclawAppPrimarySkillIDFromPackageJSON(packageJSON))
	if skillID == "" {
		// No bound skill id → materialize the pack as a maclaw_app_skill product.
		return false
	}
	name := a.findInstalledNLSkillName(skillID)
	if name == "" {
		// Not installed locally → still need materialize upload path.
		return false
	}
	installed := a.installedMaclawAppSkillIndex()
	for _, key := range []string{name, skillID} {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		if m, ok := installed[key]; ok && strings.TrimSpace(m.HubSkillID) != "" {
			return true
		}
	}
	return false
}

// syncMaclawAppOneClickHubPack runs enterprise Hub pack submit with one
// stamp-and-retry when the publish gate reports unpublished deps.
func (a *App) syncMaclawAppOneClickHubPack(ctx context.Context, submissionID string, targets map[string]any) map[string]any {
	if ctx == nil {
		ctx = context.Background()
	}
	hubRes, hubErr := a.syncMaclawAppPackageSubmissionToHub(ctx, submissionID)
	if hubErr == nil {
		return map[string]any{"ok": true, "result": hubRes}
	}
	// Stamp-and-retry covers races where MarkUploaded landed after the pre-hub stamp.
	if classifyMaclawAppOneClickError(hubErr) == "dep_not_published" {
		if err := ctx.Err(); err != nil {
			return maclawAppOneClickTargetFail(err)
		}
		if stamped, stampErr := a.refreshMaclawAppSubmissionPublishStamps(submissionID); stampErr == nil && stamped > 0 {
			if t, ok := targets["skill_market"].(map[string]any); ok {
				prev := 0
				switch v := t["stamped_deps"].(type) {
				case int:
					prev = v
				case int64:
					prev = int(v)
				case float64:
					prev = int(v)
				}
				t["stamped_deps"] = prev + stamped
			}
			if hubRes2, hubErr2 := a.syncMaclawAppPackageSubmissionToHub(ctx, submissionID); hubErr2 == nil {
				return map[string]any{"ok": true, "result": hubRes2, "retried_after_stamp": true}
			} else {
				hubErr = hubErr2
			}
		}
	}
	return maclawAppOneClickTargetFail(hubErr)
}

func maclawAppOneClickTargetFail(err error) map[string]any {
	if err == nil {
		return map[string]any{"ok": false, "error": "unknown error", "error_code": "target_failed"}
	}
	out := map[string]any{
		"ok":         false,
		"error":      err.Error(),
		"error_code": classifyMaclawAppOneClickError(err),
	}
	if detail := maclawAppOneClickErrorDetail(err); detail != nil {
		out["error_detail"] = detail
	}
	return out
}

// maclawAppOneClickErrorDetail extracts the actionable Hub review issue from a
// remote response. The raw response remains available for diagnostics, but the
// publish summary must not expose an unformatted JSON blob to operators.
func maclawAppOneClickErrorDetail(err error) map[string]any {
	if err == nil {
		return nil
	}
	raw := strings.TrimSpace(err.Error())
	start := strings.Index(raw, "{")
	if start < 0 {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal([]byte(raw[start:]), &payload) != nil {
		return nil
	}
	nested := anyMap(payload["error"])
	detail := map[string]any{
		"code":    firstNonEmpty(stringFromAnyMap(payload, "code"), "REMOTE_PUBLISH_FAILED"),
		"message": firstNonEmpty(stringFromAnyMap(payload, "message"), stringFromAnyMap(nested, "message")),
	}
	if nested != nil {
		if code := stringFromAnyMap(nested, "code"); code != "" {
			detail["code"] = code
		}
		if issues := nested["issues"]; issues != nil {
			detail["issues"] = issues
		}
	}
	if strings.TrimSpace(stringFromAnyMap(detail, "message")) == "" {
		detail["message"] = "Remote publish rejected the package"
	}
	return detail
}

// classifyMaclawAppOneClickError maps remote/target failures to stable UI codes.
func classifyMaclawAppOneClickError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "install_ref") || strings.Contains(msg, "dependencyverification") || strings.Contains(msg, "dependency verification"):
		return "dep_not_published"
	case strings.Contains(msg, "neither bundled") || strings.Contains(msg, "not been published") || strings.Contains(msg, "neither bundled nor published"):
		return "dep_not_published"
	case strings.Contains(msg, "认证失败") || strings.Contains(msg, "session refresh") ||
		(strings.Contains(msg, "session") && (strings.Contains(msg, "expired") || strings.Contains(msg, "invalid"))) ||
		strings.Contains(msg, "unauthorized") || strings.Contains(msg, " 401") || strings.HasSuffix(msg, "(401)"):
		return "auth_expired"
	case strings.Contains(msg, "marketplace url") || strings.Contains(msg, "auth token is not configured") ||
		strings.Contains(msg, "enterprise hub marketplace"):
		return "hub_not_configured"
	case strings.Contains(msg, "remote_email") || strings.Contains(msg, "hub enrollment incomplete"):
		return "email_not_configured"
	case strings.Contains(msg, "fingerprint"):
		return "fingerprint_mismatch"
	case strings.Contains(msg, "x509:") || strings.Contains(msg, "certificate has expired") ||
		strings.Contains(msg, "certificate is not yet valid") || strings.Contains(msg, "tls: failed to verify certificate"):
		return "hub_tls_certificate_invalid"
	case strings.Contains(msg, "no reachable hubcenter") || strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") || strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "context deadline exceeded"):
		return "network"
	case strings.Contains(msg, "package payload is empty"):
		return "empty_package"
	default:
		return "target_failed"
	}
}

// buildMaclawAppOneClickPreflight returns a structured readiness report.
// parseErr, when non-nil, marks package_parse as blocking.
func (a *App) buildMaclawAppOneClickPreflight(packageJSON string, pkg map[string]any, parseErr error) map[string]any {
	if a == nil {
		return map[string]any{
			"schema":  "maclaw.app.one_click_preflight.v1",
			"ok":      false,
			"message": "app is not initialized",
			"checks":  []map[string]any{},
		}
	}
	checks := make([]map[string]any, 0, 12)
	var blocking, warnings []string
	add := func(id, severity, message string, ok bool, extra map[string]any) {
		row := map[string]any{
			"id":       id,
			"ok":       ok,
			"severity": severity,
			"message":  message,
		}
		for k, v := range extra {
			row[k] = v
		}
		checks = append(checks, row)
		if !ok {
			if severity == "error" {
				blocking = append(blocking, id+": "+message)
			} else {
				warnings = append(warnings, id+": "+message)
			}
		}
	}

	if parseErr != nil {
		add("package_parse", "error", parseErr.Error(), false, map[string]any{"error_code": "package_invalid"})
		return map[string]any{
			"schema":                 "maclaw.app.one_click_preflight.v1",
			"ok":                     false,
			"ready_for_local":        false,
			"ready_for_skill_market": false,
			"ready_for_hub_pack":     false,
			"checks":                 checks,
			"blocking":               blocking,
			"warnings":               warnings,
			"message":                "package parse failed",
		}
	}

	if pkg == nil && strings.TrimSpace(packageJSON) != "" {
		var err error
		pkg, _, _, err = parseMaclawAppPackage(packageJSON)
		if err != nil {
			return a.buildMaclawAppOneClickPreflight(packageJSON, nil, err)
		}
	}
	if pkg == nil {
		add("package_parse", "error", "package payload is empty", false, map[string]any{"error_code": "empty_package"})
	} else {
		add("package_parse", "info", "package JSON is valid", true, nil)
	}

	var plan maclawAppInstallPlan
	if strings.TrimSpace(packageJSON) != "" {
		if p, err := a.PlanMaclawAppInstall(packageJSON); err != nil {
			add("install_plan", "error", err.Error(), false, map[string]any{"error_code": "plan_failed"})
		} else {
			plan = p
			add("install_plan", "info", fmt.Sprintf("%d dependencies planned", len(plan.Dependencies)), true, map[string]any{
				"dependency_count": len(plan.Dependencies),
			})
		}
	}

	if pkg != nil {
		if issue := firstBlockingMaclawAppReviewIssue(maclawAppReadyReviewIssuesForPackage(pkg, plan)); issue != nil {
			add("package_ready", "error", issue.Path+": "+issue.Message, false, map[string]any{
				"error_code": "package_not_ready",
				"path":       issue.Path,
			})
		} else {
			add("package_ready", "info", "package passes local readiness review", true, nil)
		}
	}

	// Dependency resolvability for Hub pack gate.
	depRows := make([]map[string]any, 0)
	missingDeps := 0
	bundledCount := 0
	publishedCount := 0
	bundleFailed := 0
	var bundleFailureRows []map[string]any
	var missingIDs []string
	if pkg != nil {
		bundled := maclawAppBundledDependenciesFromDoc(pkg)
		installed := map[string]NLSkillDefinition{}
		if a != nil {
			installed = a.installedMaclawAppSkillIndex()
		}
		// Mirror the submit path: stamp HubSkillID refs and simulate bundling so
		// preflight reports what SubmitMaclawAppPackage would actually embed
		// instead of silently dropping the dependency onto the external install
		// path.
		simDeps := cloneMaclawAppPlanDependencies(plan.Dependencies)
		a.enrichDependenciesWithHubSkillID(simDeps)
		liveBundled, bundleFailures := a.maclawAppBundleDependenciesForPlanDetailed(simDeps)
		failureByDep := map[string]string{}
		for _, f := range bundleFailures {
			failureByDep[strings.ToLower(strings.TrimSpace(f.ID))] = f.Err
		}
		for i, dep := range plan.Dependencies {
			if !dep.Required {
				continue
			}
			simDep := dep
			if i < len(simDeps) && strings.EqualFold(simDeps[i].ID, dep.ID) {
				simDep = simDeps[i]
			}
			row := map[string]any{"id": dep.ID, "required": true}
			switch {
			case maclawAppDependencyIsBundled(bundled, simDep) || maclawAppDependencyIsBundled(liveBundled, simDep):
				row["status"] = "bundled"
				bundledCount++
			case maclawAppDependencyHasRemoteInstallRef(simDep):
				row["status"] = "remote_ref"
				publishedCount++
			case simDep.SkillID != "" && cskill.IsValidSkillID(simDep.SkillID):
				row["status"] = "skill_id"
				publishedCount++
			case maclawAppDependencyHasPublishedHubSkillID(installed, simDep):
				row["status"] = "hub_skill_id"
				publishedCount++
			default:
				if bundleErr, failed := failureByDep[strings.ToLower(strings.TrimSpace(dep.ID))]; failed {
					row["status"] = "bundle_failed"
					row["error"] = bundleErr
					bundleFailed++
					bundleFailureRows = append(bundleFailureRows, map[string]any{"id": dep.ID, "error": bundleErr})
				} else {
					row["status"] = "missing"
					missingDeps++
					missingIDs = append(missingIDs, dep.ID)
				}
			}
			depRows = append(depRows, row)
		}
	}
	if bundleFailed > 0 {
		add("dependency_bundle", "error",
			fmt.Sprintf("%d required skill(s) installed locally but failed to embed into the package — receivers would depend on external skill install", bundleFailed),
			false, map[string]any{
				"error_code": "dep_bundle_failed",
				"failures":   bundleFailureRows,
			})
	}
	if missingDeps > 0 {
		add("dependencies", "warn",
			fmt.Sprintf("%d required skill(s) not bundled/published — Hub pack may fail until upload or bundle", missingDeps),
			false, map[string]any{
				"error_code":      "dep_not_published",
				"missing_count":   missingDeps,
				"missing_ids":     missingIDs,
				"bundled_count":   bundledCount,
				"published_count": publishedCount,
				"dependencies":    depRows,
			})
	} else if len(depRows) > 0 {
		add("dependencies", "info",
			fmt.Sprintf("all %d required deps resolvable (bundled=%d published=%d)", len(depRows), bundledCount, publishedCount),
			true, map[string]any{
				"bundled_count":   bundledCount,
				"published_count": publishedCount,
				"dependencies":    depRows,
			})
	} else {
		add("dependencies", "info", "no required skill dependencies", true, nil)
	}

	// Config readiness for remote targets (warnings — local queue still works).
	cfg, cfgErr := a.LoadConfig()
	if cfgErr != nil {
		add("config", "warn", "cannot load config: "+cfgErr.Error(), false, nil)
	} else {
		add("config", "info", "config loaded", true, nil)
		email := strings.TrimSpace(cfg.RemoteEmail)
		if email == "" || !strings.Contains(email, "@") {
			add("skill_market_email", "warn", "remote_email not configured — SkillMarket upload will fail", false, map[string]any{
				"error_code": "email_not_configured",
			})
		} else {
			add("skill_market_email", "info", "remote_email configured", true, nil)
		}
		machineID := strings.TrimSpace(cfg.RemoteMachineID)
		viewer := strings.TrimSpace(cfg.RemoteViewerToken)
		if machineID == "" || viewer == "" {
			add("hub_enrollment", "warn", "Hub enrollment incomplete (machine_id/viewer_token) — SkillMarket session refresh may fail", false, map[string]any{
				"error_code": "email_not_configured",
			})
		} else {
			add("hub_enrollment", "info", "Hub enrollment credentials present", true, nil)
		}
		if strings.TrimSpace(cfg.SkillMarketSessionToken) != "" {
			add("skill_market_session", "info", "SkillMarket session token present", true, nil)
		} else if viewer != "" {
			add("skill_market_session", "info", "will use viewer token / machine-login for SkillMarket", true, nil)
		} else {
			add("skill_market_session", "warn", "no SkillMarket session or viewer token", false, map[string]any{
				"error_code": "auth_expired",
			})
		}
		hubBase := capabilityMarketBaseURL(cfg)
		hubToken := capabilityMarketAuthToken(cfg)
		if hubBase == "" || hubToken == "" {
			add("enterprise_hub_market", "warn", "enterprise Hub marketplace URL/token not configured — Hub pack will fail", false, map[string]any{
				"error_code": "hub_not_configured",
			})
		} else if err := maclawAppHubTLSPreflightCached(hubBase); err != nil {
			add("enterprise_hub_tls", "warn", "enterprise Hub TLS certificate is invalid — Hub pack will fail", false, map[string]any{
				"error_code": "hub_tls_certificate_invalid",
				"detail":     err.Error(),
			})
		} else {
			add("enterprise_hub_market", "info", "enterprise Hub marketplace configured", true, map[string]any{
				"base_url": hubBase,
			})
		}
		if center := strings.TrimSpace(cfg.ConfiguredHubCenterBaseURL()); center == "" {
			add("hubcenter_identity", "warn", "no public HubCenter enrollment URL — SkillMarket may use discovery seeds only", false, nil)
		} else {
			add("hubcenter_identity", "info", "HubCenter enrollment: "+center, true, map[string]any{"base_url": center})
		}
	}

	skipMarket := a.maclawAppOneClickCanSkipSkillMarket(packageJSON)
	if skipMarket {
		add("skill_market_upload", "info", "primary skill already published — skill market upload can be skipped", true, map[string]any{
			"can_skip": true,
		})
	} else {
		add("skill_market_upload", "info", "skill market upload will run for primary skill product", true, map[string]any{
			"can_skip": false,
		})
	}

	readyLocal := len(blocking) == 0
	readySkill := readyLocal
	readyHub := readyLocal && missingDeps == 0
	for _, c := range checks {
		id, _ := c["id"].(string)
		ok, _ := c["ok"].(bool)
		if ok {
			continue
		}
		switch id {
		case "skill_market_email", "hub_enrollment", "skill_market_session":
			// Already-published primary skill does not need SkillMarket upload.
			if !skipMarket {
				readySkill = false
			}
		case "enterprise_hub_market", "enterprise_hub_tls", "dependencies":
			readyHub = false
		}
	}
	if skipMarket && readyLocal {
		readySkill = true
	}

	msg := "ready for one-click"
	if !readyLocal {
		msg = "blocked: fix package readiness before publish"
	} else if !readySkill && !readyHub {
		msg = "local queue OK; remote targets will partially fail"
	} else if !readySkill {
		msg = "local queue OK; SkillMarket may fail"
	} else if !readyHub {
		msg = "local queue OK; enterprise Hub pack may fail"
	} else if skipMarket {
		msg = "ready for one-click (skill market can be skipped)"
	}

	return map[string]any{
		"schema":                 "maclaw.app.one_click_preflight.v1",
		"ok":                     readyLocal,
		"ready_for_local":        readyLocal,
		"ready_for_skill_market": readySkill,
		"ready_for_hub_pack":     readyHub,
		"checks":                 checks,
		"blocking":               blocking,
		"warnings":               warnings,
		"message":                msg,
	}
}

// maclawAppHubTLSPreflight verifies the configured Hub's TLS certificate
// before one-click publish. It opens only a short handshake (no HTTP request
// and no credentials) so operators see certificate expiry/clock faults before
// the local queue records a partial remote failure.
func maclawAppHubTLSPreflight(baseURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return fmt.Errorf("invalid Enterprise Hub URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("Enterprise Hub URL has no host")
	}
	port := strings.TrimSpace(parsed.Port())
	if port == "" {
		port = "443"
	}
	const timeout = 4 * time.Second
	dialer := &net.Dialer{Timeout: timeout}
	rawConn, err := dialer.DialContext(context.Background(), "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	defer rawConn.Close()
	// Dialer.Timeout limits only opening the TCP socket. Bound the TLS
	// handshake too: a reachable but unhealthy Hub must not leave every
	// publish preflight waiting indefinitely.
	if err := rawConn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	conn := tls.Client(rawConn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(context.Background()); err != nil {
		return err
	}
	return conn.Close()
}

// maclawAppHubTLSPreflightCached avoids repeating the same TLS handshake for
// every visible publish card. Both healthy and failed checks are cached only
// briefly; callers arriving together share one in-flight handshake, while a
// certificate renewal or server-clock correction becomes visible promptly.
func maclawAppHubTLSPreflightCached(baseURL string) error {
	key := strings.TrimSpace(baseURL)
	for {
		now := time.Now()
		maclawAppHubTLSPreflightMu.Lock()
		entry := maclawAppHubTLSPreflightCache[key]
		if entry != nil && entry.done == nil && now.Before(entry.expiresAt) {
			err := entry.err
			maclawAppHubTLSPreflightMu.Unlock()
			return err
		}
		if entry != nil && entry.done != nil {
			done := entry.done
			maclawAppHubTLSPreflightMu.Unlock()
			<-done
			continue
		}
		entry = &maclawAppHubTLSPreflightCacheEntry{done: make(chan struct{})}
		maclawAppHubTLSPreflightCache[key] = entry
		maclawAppHubTLSPreflightMu.Unlock()

		err := maclawAppHubTLSCheck(key)

		maclawAppHubTLSPreflightMu.Lock()
		entry.err = err
		entry.expiresAt = time.Now().Add(maclawAppHubTLSPreflightCacheTTL)
		close(entry.done)
		entry.done = nil
		maclawAppHubTLSPreflightMu.Unlock()
		return err
	}
}

func maclawAppSubmissionLocalMap(record *maclawAppSubmissionRecord) map[string]any {
	if record == nil {
		return map[string]any{}
	}
	return map[string]any{
		"submission_id":     record.SubmissionID,
		"status":            record.Status,
		"channel":           record.Channel,
		"submitted_at":      record.SubmittedAt,
		"package_sha":       record.PackageSHA,
		"package_sha256":    record.PackageSHA,
		"hub_capability_id": record.HubCapabilityID,
		"message":           record.Message,
	}
}

func (a *App) maclawAppSubmissionSummaryMap(submissionID string) map[string]any {
	if a == nil {
		return nil
	}
	record, err := a.GetMaclawAppPackageSubmission(submissionID)
	if err != nil || record == nil {
		return nil
	}
	return maclawAppSubmissionLocalMap(record)
}

// stampMaclawAppOneClickMessage patches only the durable queue message field.
// It intentionally avoids UpdateMaclawAppPackageSubmissionStatus, which would
// append audit events and may rewrite reviewed_at for non-submitted statuses.
func (a *App) stampMaclawAppOneClickMessage(submissionID, message string) {
	if a == nil {
		return
	}
	submissionID = strings.TrimSpace(submissionID)
	message = strings.TrimSpace(message)
	if submissionID == "" || message == "" {
		return
	}
	// Locked read-modify-write: avoid TOCTOU with concurrent queue writers
	// (hub sync, fingerprint refresh, status updates).
	maclawAppSubmissionQueueMu.Lock()
	defer maclawAppSubmissionQueueMu.Unlock()
	queue, err := a.readMaclawAppSubmissionQueueUnlocked()
	if err != nil {
		return
	}
	for i := range queue.Submissions {
		if queue.Submissions[i].SubmissionID != submissionID {
			continue
		}
		if strings.TrimSpace(queue.Submissions[i].Message) == message {
			return
		}
		queue.Submissions[i].Message = message
		queue.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_ = a.writeMaclawAppSubmissionQueueUnlocked(queue)
		return
	}
}

func summarizeMaclawAppOneClickPublish(targets map[string]any) string {
	parts := make([]string, 0, 3)
	// Hub already-synced retries are still "local queue ready", not a fresh enqueue.
	if t, ok := targets["enterprise_hub_pack"].(map[string]any); ok && t["ok"] == true && t["skipped"] == true {
		parts = append(parts, "local queue ready")
	} else {
		parts = append(parts, "queued locally")
	}
	if t, ok := targets["enterprise_hub_pack"].(map[string]any); ok {
		if t["ok"] == true {
			if t["skipped"] == true {
				parts = append(parts, "enterprise hub pack already synced")
			} else if t["retried_after_stamp"] == true {
				parts = append(parts, "enterprise hub pack submitted (after dep stamp retry)")
			} else {
				parts = append(parts, "enterprise hub pack submitted")
			}
		} else if err := strings.TrimSpace(stringFromAnyMap(t, "error")); err != "" {
			parts = append(parts, "enterprise hub pack failed: "+maclawAppOneClickHumanizeError(t))
		}
	}
	if t, ok := targets["skill_market"].(map[string]any); ok {
		if t["ok"] == true {
			var msg string
			if t["skipped"] == true {
				msg = "skill market skipped (" + firstNonEmpty(stringFromAnyMap(t, "reason"), "already_published") + ")"
			} else {
				msg = "skill market upload ok (" + stringFromAnyMap(t, "submission_id") + ")"
			}
			if n := stringFromAnyMap(t, "stamped_deps"); n != "" && n != "0" {
				msg += ", stamped " + n + " dep ref"
			}
			parts = append(parts, msg)
		} else if err := strings.TrimSpace(stringFromAnyMap(t, "error")); err != "" {
			parts = append(parts, "skill market failed: "+maclawAppOneClickHumanizeError(t))
		}
	}
	return strings.Join(parts, "; ")
}

// maclawAppOneClickHumanizeError prefers a short code-based hint, then raw error.
func maclawAppOneClickHumanizeError(t map[string]any) string {
	raw := strings.TrimSpace(stringFromAnyMap(t, "error"))
	switch strings.TrimSpace(stringFromAnyMap(t, "error_code")) {
	case "dep_not_published":
		return maclawAppOneClickDependencyErrorHint(t, raw)
	case "auth_expired":
		return "SkillMarket session expired — re-login Hub/SkillMarket (" + truncateOneClickErr(raw, 120) + ")"
	case "hub_not_configured":
		return "enterprise Hub marketplace URL/token not configured"
	case "email_not_configured":
		return "remote_email / Hub enrollment incomplete"
	case "network":
		return "network/unreachable HubCenter (" + truncateOneClickErr(raw, 120) + ")"
	case "hub_tls_certificate_invalid":
		return "enterprise Hub TLS certificate is expired or not yet valid; renew the certificate or correct the Hub server clock before retrying"
	case "fingerprint_mismatch":
		return "package fingerprint mismatch (" + truncateOneClickErr(raw, 120) + ")"
	case "empty_package":
		return "package payload is empty"
	default:
		return raw
	}
}

func maclawAppOneClickDependencyErrorHint(t map[string]any, fallback string) string {
	detail := anyMap(t["error_detail"])
	for _, rawIssue := range anySlice(detail["issues"]) {
		issue := anyMap(rawIssue)
		if issue == nil {
			continue
		}
		path := strings.TrimSpace(stringFromAny(issue["path"]))
		message := strings.TrimSpace(stringFromAny(issue["message"]))
		suggestion := strings.TrimSpace(stringFromAny(issue["suggestion"]))
		if strings.Contains(strings.ToLower(path+" "+message), "install_ref") {
			if suggestion == "" {
				suggestion = "publish the dependency Skill, refresh dependency verification, then retry"
			}
			return "a required Skill is missing its install reference; " + suggestion
		}
	}
	return "required Skill dependencies need to be bundled or published before retrying"
}

func truncateOneClickErr(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// uploadMaclawAppPackageToSkillMarkets prefers packaging the bound local skill
// (full assets) and submitting to configured targets. Falls back to a materialised
// skill zip from the app pack JSON so HubCenter still receives a maclaw_app_skill product.
// Does not use UploadNLSkillToMarket (workflow gate + lifecycle queue) so GUI one-click
// stays direct and consistent with the materialised path.
func (a *App) uploadMaclawAppPackageToSkillMarkets(ctx context.Context, packageJSON string) (string, error) {
	var installedErr error
	if skillID := maclawAppPrimarySkillIDFromPackageJSON(packageJSON); skillID != "" {
		if installedName := a.findInstalledNLSkillName(skillID); installedName != "" {
			id, err := a.uploadInstalledNLSkillToSkillMarkets(ctx, installedName)
			if err == nil {
				return id, nil
			}
			installedErr = err
			// Fall through to materialised pack when quality gate / packaging fails.
		}
	}
	id, err := a.uploadMaclawAppPackAsSkillMarketZip(ctx, packageJSON)
	if err == nil {
		return id, nil
	}
	if installedErr != nil {
		return "", fmt.Errorf("installed skill upload failed: %w; materialize upload failed: %w", installedErr, err)
	}
	return "", err
}

func (a *App) findInstalledNLSkillName(skillName string) string {
	if a == nil {
		return ""
	}
	a.ensureInteractionInfra()
	if a.skillExecutor == nil {
		return ""
	}
	name := strings.TrimSpace(skillName)
	if name == "" {
		return ""
	}
	for _, s := range a.skillExecutor.loadSkills() {
		if strings.EqualFold(strings.TrimSpace(s.Name), name) && strings.TrimSpace(s.SkillDir) != "" {
			return strings.TrimSpace(s.Name)
		}
	}
	return ""
}

func (a *App) uploadInstalledNLSkillToSkillMarkets(ctx context.Context, skillName string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.ensureSkillMarketClient()
	if a.skillMarketClient == nil {
		return "", fmt.Errorf("skill market client not initialized")
	}
	email, err := a.skillMarketUploaderEmail()
	if err != nil {
		return "", err
	}
	// findInstalledNLSkillName already ensures interaction infra; re-check before package.
	if a.skillExecutor == nil {
		a.ensureInteractionInfra()
	}
	if a.skillExecutor == nil {
		return "", fmt.Errorf("skill executor not initialized")
	}
	zipPath, tmpDir, err := a.packageSkillForMarketWithDirForOutbound(skillName)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = os.Remove(zipPath)
		_ = os.RemoveAll(tmpDir)
	}()
	id, err := a.skillMarketClient.SubmitSkillToConfiguredTargets(ctx, zipPath, email)
	if err != nil {
		return "", err
	}
	// Stamp HubSkillID so the enterprise Hub pack gate can accept non-bundled
	// deps after skill-market succeeds first in one-click publish.
	if a.skillExecutor != nil && strings.TrimSpace(id) != "" {
		if markErr := a.skillExecutor.MarkUploaded(skillName, id); markErr != nil {
			// Upload already succeeded; gate may still pass via bundled_dependencies.
			log.Printf("[maclaw-app] mark skill %q uploaded after market submit failed: %v", skillName, markErr)
		}
	}
	return id, nil
}

func (a *App) skillMarketUploaderEmail() (string, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	email := strings.TrimSpace(cfg.RemoteEmail)
	if email == "" || !strings.Contains(email, "@") {
		return "", fmt.Errorf("remote_email is not configured (required for SkillMarket upload)")
	}
	return email, nil
}

func maclawAppPrimarySkillIDFromPackageJSON(packageJSON string) string {
	var pkg map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &pkg); err != nil {
		return ""
	}
	// Single app entry.
	if id := maclawAppSkillIDFromEntryMap(pkg); id != "" {
		return id
	}
	// Pack with apps[] — anySlice covers []any and []map[string]any.
	for _, raw := range anySlice(pkg["apps"]) {
		if id := maclawAppSkillIDFromEntryMap(anyMap(raw)); id != "" {
			return id
		}
	}
	return ""
}

func maclawAppSkillIDFromEntryMap(entry map[string]any) string {
	if entry == nil {
		return ""
	}
	app, _ := entry["app"].(map[string]any)
	if app == nil {
		app = entry
	}
	binding, _ := app["binding"].(map[string]any)
	if binding == nil {
		binding, _ = entry["binding"].(map[string]any)
	}
	// Prefer runtime skill id, then appSkill (common GUI pack shape).
	for _, holder := range []map[string]any{binding, app, entry} {
		if holder == nil {
			continue
		}
		if skill, _ := holder["skill"].(map[string]any); skill != nil {
			if id := strings.TrimSpace(stringFromAnyMap(skill, "id")); id != "" {
				return id
			}
		}
		if appSkill, _ := holder["appSkill"].(map[string]any); appSkill != nil {
			if id := strings.TrimSpace(stringFromAnyMap(appSkill, "id")); id != "" {
				return id
			}
		}
	}
	return ""
}

func (a *App) uploadMaclawAppPackAsSkillMarketZip(ctx context.Context, packageJSON string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.ensureSkillMarketClient()
	if a.skillMarketClient == nil {
		return "", fmt.Errorf("skill market client not initialized")
	}
	email, err := a.skillMarketUploaderEmail()
	if err != nil {
		return "", err
	}

	zipPath, cleanup, err := materializeMaclawAppSkillZipForGUI(packageJSON)
	if err != nil {
		return "", err
	}
	defer cleanup()

	id, err := a.skillMarketClient.SubmitSkillToConfiguredTargets(ctx, zipPath, email)
	if err != nil {
		return "", err
	}
	// Materialized zip may still map to a local primary skill — stamp when present
	// so the Hub pack dependency gate can see a published identity.
	if a.skillExecutor != nil && strings.TrimSpace(id) != "" {
		if skillID := maclawAppPrimarySkillIDFromPackageJSON(packageJSON); skillID != "" {
			if installedName := a.findInstalledNLSkillName(skillID); installedName != "" {
				if markErr := a.skillExecutor.MarkUploaded(installedName, id); markErr != nil {
					log.Printf("[maclaw-app] mark skill %q uploaded after materialize market submit failed: %v", installedName, markErr)
				}
			}
		}
	}
	return id, nil
}

var guiMaclawAppSkillNameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func materializeMaclawAppSkillZipForGUI(packageJSON string) (zipPath string, cleanup func(), err error) {
	noop := func() {}
	var pkg map[string]any
	if err := json.Unmarshal([]byte(packageJSON), &pkg); err != nil {
		return "", noop, fmt.Errorf("decode package: %w", err)
	}
	entry, app := firstMaclawAppEntryMaps(pkg)
	if app == nil {
		return "", noop, fmt.Errorf("package has no app entry")
	}
	appID := firstNonEmpty(
		strings.TrimSpace(stringFromAnyMap(app, "id")),
		strings.TrimSpace(stringFromAnyMap(entry, "id")),
		"maclaw-app",
	)
	appName := firstNonEmpty(
		strings.TrimSpace(stringFromAnyMap(app, "name")),
		appID,
	)
	description := firstNonEmpty(
		strings.TrimSpace(stringFromAnyMap(app, "description")),
		appName,
	)
	// Prefer bound skill id as package skill name so market rows match installed skill names.
	skillName := sanitizeGUIMaclawAppSkillName(firstNonEmpty(
		maclawAppSkillIDFromEntryMap(entry),
		maclawAppSkillIDFromEntryMap(map[string]any{"app": app}),
		appID,
	))

	// Prefer full entry JSON as maclaw.app.json when present.
	appEntry := entry
	if appEntry == nil {
		appEntry = map[string]any{
			"schema":        "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app":           app,
		}
	}
	appEntryBytes, err := json.Marshal(appEntry)
	if err != nil {
		return "", noop, err
	}
	triggers := []string{skillName}
	if appName != "" && !strings.EqualFold(appName, skillName) {
		triggers = append(triggers, appName)
	}
	skillYAML, err := yaml.Marshal(map[string]any{
		"name":        skillName,
		"description": description,
		"triggers":    triggers,
		"steps": []map[string]any{
			{
				"action": "craft_tool",
				"params": map[string]any{
					"instructions": "MaClaw App skill package published from GUI one-click publish",
				},
			},
		},
		"maclaw_app": map[string]any{"entry": "maclaw.app.json"},
	})
	if err != nil {
		return "", noop, err
	}
	packageManifest := map[string]any{
		"package_kind":           "maclaw-skill-market",
		"product_kind":           "maclaw_app_skill",
		"is_maclaw_app":          true,
		"maclaw_app_count":       1,
		"maclaw_app_entry":       "maclaw.app.json",
		"maclaw_app_id":          appID,
		"maclaw_app_name":        appName,
		"maclaw_app_description": description,
		"maclaw_app_kind":        firstNonEmpty(strings.TrimSpace(stringFromAnyMap(app, "kind")), "tool_app"),
		"maclaw_app_category":    strings.TrimSpace(stringFromAnyMap(app, "category")),
		"maclaw_app_icon":        strings.TrimSpace(stringFromAnyMap(app, "icon")),
		"source":                 "gui_one_click",
		"skill_name":             skillName,
	}
	// Preserve pack-level install stamps from durable queue enrichment so market
	// consumers can resolve dependencies without re-deriving them from app entry alone.
	for _, key := range []string{"resolved_dependencies", "bundled_dependencies"} {
		if v, ok := pkg[key]; ok && v != nil {
			packageManifest[key] = v
		}
	}
	manifestBytes, err := json.Marshal(packageManifest)
	if err != nil {
		return "", noop, err
	}

	tmp, err := os.CreateTemp("", "maclaw-app-gui-publish-*.zip")
	if err != nil {
		return "", noop, err
	}
	tmpPath := tmp.Name()
	cleanup = func() { _ = os.Remove(tmpPath) }
	zw := zip.NewWriter(tmp)
	write := func(name string, body []byte) error {
		// Deflate shrinks multi-KB app JSON/manifest before multipart upload.
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = w.Write(body)
		return err
	}
	for _, f := range []struct {
		name string
		body []byte
	}{
		{"skill.yaml", skillYAML},
		{"skill_package_manifest.json", manifestBytes},
		{"maclaw.app.json", appEntryBytes},
	} {
		if err := write(f.name, f.body); err != nil {
			_ = zw.Close()
			_ = tmp.Close()
			cleanup()
			return "", noop, err
		}
	}
	if err := zw.Close(); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", noop, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", noop, err
	}
	return tmpPath, cleanup, nil
}

func firstMaclawAppEntryMaps(pkg map[string]any) (entry map[string]any, app map[string]any) {
	if pkg == nil {
		return nil, nil
	}
	if strings.TrimSpace(stringFromAnyMap(pkg, "schema")) == "maclaw.app.v1" {
		entry = pkg
		if a, _ := pkg["app"].(map[string]any); a != nil {
			return entry, a
		}
		return entry, pkg
	}
	// anySlice accepts both JSON []any and in-memory []map[string]any.
	for _, raw := range anySlice(pkg["apps"]) {
		e := anyMap(raw)
		if e == nil {
			continue
		}
		if a := anyMap(e["app"]); a != nil {
			return e, a
		}
		if strings.TrimSpace(stringFromAnyMap(e, "id")) != "" {
			return e, e
		}
	}
	return nil, nil
}

func sanitizeGUIMaclawAppSkillName(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, " ", "-")
	raw = guiMaclawAppSkillNameRe.ReplaceAllString(raw, "-")
	raw = strings.Trim(raw, "-._")
	if raw == "" {
		return "maclaw-app"
	}
	if len(raw) > 80 {
		raw = strings.Trim(raw[:80], "-._")
	}
	return strings.ToLower(raw)
}

func stringFromAnyMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}
