package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maclawAppOneClickPublishTimeout = 3 * time.Minute

// PublishMaclawAppOneClick is the GUI one-click publish:
//  1. queue a local maclaw.app.pack.v1 submission
//  2. sync the pack to enterprise Hub when marketplace URL+token are configured
//  3. upload a skill-market package to HubCenter (and enterprise skill API if policy says so)
//
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

	// 1) Enterprise Hub pack path (maclaw-apps/submit) when still local-only.
	// Single queue read: also prefer durable package (resolved/bundled stamps) for skill market.
	// Capture package before hub sync may rewrite the submission id.
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
	if err := ctx.Err(); err != nil {
		targets["enterprise_hub_pack"] = map[string]any{"ok": false, "error": err.Error()}
	} else if channel == "" || channel == "local" {
		hubRes, hubErr := a.SyncMaclawAppPackageSubmissionToHub(submissionID)
		if hubErr != nil {
			targets["enterprise_hub_pack"] = map[string]any{"ok": false, "error": hubErr.Error()}
		} else {
			targets["enterprise_hub_pack"] = map[string]any{"ok": true, "result": hubRes}
		}
	} else {
		targets["enterprise_hub_pack"] = map[string]any{
			"ok":      true,
			"skipped": true,
			"reason":  "already_" + channel,
		}
	}

	// 2) Skill-market targets (HubCenter and/or enterprise skill submit per policy).
	if err := ctx.Err(); err != nil {
		targets["skill_market"] = map[string]any{"ok": false, "error": err.Error()}
	} else if strings.TrimSpace(packageJSON) == "" {
		targets["skill_market"] = map[string]any{"ok": false, "error": "package payload is empty"}
	} else {
		marketID, marketErr := a.uploadMaclawAppPackageToSkillMarkets(ctx, packageJSON)
		if marketErr != nil {
			targets["skill_market"] = map[string]any{"ok": false, "error": marketErr.Error()}
		} else {
			targets["skill_market"] = map[string]any{"ok": true, "submission_id": marketID}
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
	queue, err := a.readMaclawAppSubmissionQueue()
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
		_ = a.writeMaclawAppSubmissionQueue(queue)
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
			} else {
				parts = append(parts, "enterprise hub pack submitted")
			}
		} else if err := strings.TrimSpace(stringFromAnyMap(t, "error")); err != "" {
			parts = append(parts, "enterprise hub pack failed: "+err)
		}
	}
	if t, ok := targets["skill_market"].(map[string]any); ok {
		if t["ok"] == true {
			parts = append(parts, "skill market upload ok ("+stringFromAnyMap(t, "submission_id")+")")
		} else if err := strings.TrimSpace(stringFromAnyMap(t, "error")); err != "" {
			parts = append(parts, "skill market failed: "+err)
		}
	}
	return strings.Join(parts, "; ")
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
	return a.skillMarketClient.SubmitSkillToConfiguredTargets(ctx, zipPath, email)
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
	// Pack with apps[].
	rawApps, _ := pkg["apps"].([]any)
	for _, raw := range rawApps {
		entry, _ := raw.(map[string]any)
		if id := maclawAppSkillIDFromEntryMap(entry); id != "" {
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

	return a.skillMarketClient.SubmitSkillToConfiguredTargets(ctx, zipPath, email)
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
	rawApps, _ := pkg["apps"].([]any)
	for _, raw := range rawApps {
		e, _ := raw.(map[string]any)
		if e == nil {
			continue
		}
		if a, _ := e["app"].(map[string]any); a != nil {
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
