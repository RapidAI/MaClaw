package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedOfficeWriteAdapter        = "semantic_write_trusted_office"
	semanticTrustedOfficeWriteImplementation = "trusted-office-write-v1"
	// semanticOfficeArtifactMaxBytes bounds the read-back that registers a
	// written deck/sheet as a deliverable broker artifact. Larger files still
	// succeed as workspace writes; they just skip artifact delivery.
	semanticOfficeArtifactMaxBytes = 32 << 20
)

// semanticOfficeArtifactMIME maps the produced office file extension to the
// artifact contract MIME. The trusted office adapter only writes spreadsheets
// and presentations, so any other extension means "not an office artifact".
func semanticOfficeArtifactMIME(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation", true
	default:
		return "", false
	}
}

func semanticUnpublishedLegacyOfficeProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityDocumentWriteOffice {
			return true
		}
	}
	return false
}

func semanticTrustedOfficeWriteDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedOfficeWriteAdapter,
			"description": "Write a spreadsheet or a presentation deck into a workspace path. Spreadsheet: path + sheets. Presentation (.pptx): path + title + slides; each slide takes title, bullets, notes, optional images, and optional native editable charts (bar/column/bar_h/line/radar/pie/area — PowerPoint chart objects, not pictures). Content and destination are bound by the host. Pass either sheets or slides, never both; omit the unused key entirely.",
			"parameters":  semanticTrustedOfficeWriteInvocationSchema(),
		},
	}
}

func semanticTrustedOfficeWriteInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
			"sheets": map[string]interface{}{
				"type":        "array",
				"description": "Spreadsheet form (.xlsx). Pass ONLY when writing a spreadsheet; omit this key entirely for a presentation. Never pass both sheets and slides.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
						"rows": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type":  "array",
								"items": map[string]interface{}{"type": "string"},
							},
						},
					},
					"required":             []string{"name", "rows"},
					"additionalProperties": false,
				},
			},
			"title":    map[string]interface{}{"type": "string"},
			"subtitle": map[string]interface{}{"type": "string"},
			"slides": map[string]interface{}{
				"type":        "array",
				"description": "Presentation form (.pptx). Pass ONLY when writing a presentation; omit this key entirely for a spreadsheet. Never pass both sheets and slides.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title": map[string]interface{}{"type": "string"},
						"bullets": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"type": "string"},
						},
						"notes": map[string]interface{}{"type": "string"},
						"images": map[string]interface{}{
							"type":        "array",
							"description": "Embed local image files on this slide (e.g. photos fetched with download_file). path is a workspace-relative name or an absolute path; width/height are optional explicit sizes in inches (aspect ratio is preserved when omitted).",
							"maxItems":    pptx.MaxSlideImages,
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"path":   map[string]interface{}{"type": "string"},
									"width":  map[string]interface{}{"type": "number"},
									"height": map[string]interface{}{"type": "number"},
								},
								"required":             []string{"path"},
								"additionalProperties": false,
							},
						},
						"charts": map[string]interface{}{
							"type":        "array",
							"description": "Native PowerPoint charts. chart_type: bar, column, bar_h, line, radar, pie, area (柱状图/雷达图 aliases ok). categories length must match each series.values. pie takes one series.",
							"maxItems":    pptx.MaxSlideCharts,
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"chart_type": map[string]interface{}{"type": "string"},
									"title":      map[string]interface{}{"type": "string"},
									"categories": map[string]interface{}{
										"type":     "array",
										"maxItems": pptx.MaxChartCategories,
										"items":    map[string]interface{}{"type": "string"},
									},
									"series": map[string]interface{}{
										"type":     "array",
										"maxItems": pptx.MaxChartSeries,
										"items": map[string]interface{}{
											"type": "object",
											"properties": map[string]interface{}{
												"name": map[string]interface{}{"type": "string"},
												"values": map[string]interface{}{
													"type":     "array",
													"maxItems": pptx.MaxChartCategories,
													"items":    map[string]interface{}{"type": "number"},
												},
											},
											"required":             []string{"name", "values"},
											"additionalProperties": false,
										},
									},
								},
								"required":             []string{"chart_type", "categories", "series"},
								"additionalProperties": false,
							},
						},
					},
					"required":             []string{"title"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

// semanticTrustedOfficeWriteArgsAllowed admits exactly one document form:
// spreadsheet (path + sheets) or presentation (path + slides, optional
// title/subtitle). Mixing two non-empty forms or adding any other key is
// rejected before the adapter runs, so a model cannot smuggle legacy
// office-soup arguments.
//
// The rendered schema necessarily declares both forms at once, and models
// habitually fill the unused one with an empty array, a null, or a
// stringified JSON array (observed 2026-08-26: {"slides":[...],"sheets":[]}
// and later both forms as JSON strings). An empty unused form carries no
// intent, so it is dropped here instead of burning the one-shot grant on a
// correctable shape mistake; only two non-empty forms (a genuine mix) or
// two empty forms (ambiguous) are still rejected.
func semanticTrustedOfficeWriteArgsAllowed(args map[string]interface{}) (path string, data map[string]interface{}, err error) {
	var sheets, slides, title, subtitle interface{}
	hasPath, hasSheets, hasSlides := false, false, false
	for key, raw := range args {
		switch key {
		case "path":
			value, ok := raw.(string)
			if !ok {
				return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected: path must be a string")
			}
			path, hasPath = value, true
		case "sheets":
			sheets, hasSheets = raw, true
		case "slides":
			slides, hasSlides = raw, true
		case "title":
			title = raw
		case "subtitle":
			subtitle = raw
		default:
			return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected: unexpected key %q; pass path plus either sheets (spreadsheet) or slides (presentation)", key)
		}
	}
	sheetsForm := semanticOfficeDocumentForm(sheets, hasSheets)
	slidesForm := semanticOfficeDocumentForm(slides, hasSlides)
	switch {
	case sheetsForm.present && slidesForm.present:
		// Both keys arrived: admit only when exactly one carries content; an
		// empty unused form carries no intent and is dropped. Two non-empty
		// forms are a genuine mix, two empty forms are ambiguous — both are
		// rejected so the model restates a single clear form.
		switch {
		case sheetsForm.empty && !slidesForm.empty:
			sheetsForm.present = false
		case slidesForm.empty && !sheetsForm.empty:
			slidesForm.present = false
		default:
			return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected: pass exactly one of sheets (spreadsheet) or slides (presentation), and omit the other key entirely")
		}
	case !sheetsForm.present && !slidesForm.present:
		return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected: pass exactly one of sheets (spreadsheet) or slides (presentation), and omit the other key entirely")
	}
	if !hasPath || strings.TrimSpace(path) == "" {
		return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected: path is required")
	}
	path = strings.TrimSpace(path)
	if sheetsForm.present {
		return path, map[string]interface{}{"sheets": sheetsForm.value}, nil
	}
	data = map[string]interface{}{"slides": slidesForm.value}
	if title != nil {
		data["title"] = title
	}
	if subtitle != nil {
		data["subtitle"] = subtitle
	}
	return path, data, nil
}

// semanticOfficeDocumentForm normalizes one optional document form (sheets
// or slides). Absent, null, and empty-string values carry no intent and are
// reported as not present. A string holding a JSON array is unwrapped into
// the array itself; any other string is kept as-is so the writer downstream
// rejects it with its own descriptive error. An empty array stays present
// (an intentionally empty workbook/deck is admissible on its own) and is
// flagged via empty so the caller can drop it only when the other form
// carries the real content.
type semanticOfficeForm struct {
	value   interface{}
	present bool
	empty   bool
}

func semanticOfficeDocumentForm(raw interface{}, present bool) semanticOfficeForm {
	if !present || raw == nil {
		return semanticOfficeForm{}
	}
	if text, ok := raw.(string); ok {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return semanticOfficeForm{}
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return semanticOfficeForm{value: raw, present: true}
		}
		raw = parsed
	}
	if arr, ok := raw.([]interface{}); ok && len(arr) == 0 {
		return semanticOfficeForm{value: raw, present: true, empty: true}
	}
	return semanticOfficeForm{value: raw, present: true}
}

// semanticOfficeWriteInvocationArgs washes model-supplied office arguments
// before canonical schema validation, the same boundary role as
// semanticGeneratePDFInvocationArgs. The rendered schema declares sheets and
// slides side by side, so models habitually fill the unused half with an
// empty array or null, and weaker models stringify whole arrays (observed
// 2026-08-26 birthday-deck turn: {"slides":[...],"sheets":[]}, then both
// forms as JSON strings). Canonicalization would reject those shapes as
// parameter_schema_invalid and burn the one-shot grant on a correctable
// mistake, so they are normalized here first. Genuinely mixed, unknown, or
// mistyped content passes through unchanged so admission still fails closed.
func semanticOfficeWriteInvocationArgs(argsJSON string) string {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return argsJSON
	}
	_, sheetsRaw := parsed["sheets"]
	_, slidesRaw := parsed["slides"]
	if !sheetsRaw && !slidesRaw {
		return argsJSON
	}
	sheets := semanticOfficeDocumentForm(parsed["sheets"], sheetsRaw)
	slides := semanticOfficeDocumentForm(parsed["slides"], slidesRaw)
	changed := false
	// A form whose value carries no intent (null or empty string) is not a
	// schema-valid array; drop the key outright.
	if sheetsRaw && !sheets.present {
		delete(parsed, "sheets")
		sheetsRaw = false
		changed = true
	}
	if slidesRaw && !slides.present {
		delete(parsed, "slides")
		slidesRaw = false
		changed = true
	}
	// Both forms arrived: an empty-array form next to a content-bearing form
	// is the model filling the unused half of the schema, not a real mix.
	if sheetsRaw && slidesRaw {
		if sheets.empty && !slides.empty {
			delete(parsed, "sheets")
			changed = true
		} else if slides.empty && !sheets.empty {
			delete(parsed, "slides")
			changed = true
		}
	}
	// Stringified values are replaced by their parsed form: arrays become
	// schema-valid, anything else is rejected by canonicalization either way.
	if _, ok := parsed["sheets"].(string); ok && sheets.present {
		if _, stillString := sheets.value.(string); !stillString {
			parsed["sheets"] = sheets.value
			changed = true
		}
	}
	if _, ok := parsed["slides"].(string); ok && slides.present {
		if _, stillString := slides.value.(string); !stillString {
			parsed["slides"] = slides.value
			changed = true
		}
	}
	if semanticOfficeUnwrapSlideCharts(parsed) {
		changed = true
	}
	if !changed {
		return argsJSON
	}
	body, err := json.Marshal(parsed)
	if err != nil {
		return argsJSON
	}
	return string(body)
}

// semanticOfficeUnwrapSlideCharts replaces stringified chart arrays (and the
// nested categories/series/values arrays) with parsed arrays. Nested
// stringify is the same correctable mistake as top-level slides.
func semanticOfficeUnwrapSlideCharts(parsed map[string]interface{}) bool {
	slides, ok := parsed["slides"].([]interface{})
	if !ok {
		return false
	}
	changed := false
	for _, item := range slides {
		slide, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if semanticOfficeUnwrapJSONArrayField(slide, "charts") {
			changed = true
		}
		charts, ok := slide["charts"].([]interface{})
		if !ok {
			continue
		}
		for _, rawChart := range charts {
			chart, ok := rawChart.(map[string]interface{})
			if !ok {
				continue
			}
			if semanticOfficeUnwrapJSONArrayField(chart, "categories") {
				changed = true
			}
			if semanticOfficeUnwrapJSONArrayField(chart, "series") {
				changed = true
			}
			series, ok := chart["series"].([]interface{})
			if !ok {
				continue
			}
			for _, rawSeries := range series {
				seriesObj, ok := rawSeries.(map[string]interface{})
				if !ok {
					continue
				}
				if semanticOfficeUnwrapJSONArrayField(seriesObj, "values") {
					changed = true
				}
			}
		}
	}
	return changed
}

func semanticOfficeUnwrapJSONArrayField(obj map[string]interface{}, key string) bool {
	raw, ok := obj[key]
	if !ok {
		return false
	}
	if _, wasString := raw.(string); !wasString {
		return false
	}
	form := semanticOfficeDocumentForm(raw, true)
	if !form.present {
		return false
	}
	if _, isArray := form.value.([]interface{}); !isArray {
		return false
	}
	obj[key] = form.value
	return true
}

func (h *IMMessageHandler) writeTrustedOffice(principalID, path string, data map[string]interface{}) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_office_write_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_office_write_principal_required")
	}
	if h.semanticTrustedOfficeWrite != nil {
		return h.semanticTrustedOfficeWrite(principalID, path, data)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	absPath, err := trustedFileWriteResolvePath(workspace, path)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("trusted_office_write_path_is_directory")
	}
	if _, isPresentation := data["slides"]; isPresentation {
		if err := resolveOfficeSlideImages(workspace, data); err != nil {
			return "", err
		}
		if _, err := agent.WritePPTXDetailed(map[string]interface{}{"path": absPath, "data": data}); err != nil {
			return "", fmt.Errorf("trusted_office_write_failed: %w", err)
		}
		display := trustedFileWriteDisplayPath(workspace, absPath, path)
		return fmt.Sprintf("Wrote presentation %s", display), nil
	}
	if _, err := agent.WriteExcelDetailed(map[string]interface{}{"path": absPath, "data": data}); err != nil {
		return "", fmt.Errorf("trusted_office_write_failed: %w", err)
	}
	display := trustedFileWriteDisplayPath(workspace, absPath, path)
	return fmt.Sprintf("Wrote spreadsheet %s", display), nil
}

// resolveOfficeSlideImages rewrites every slide image path in a presentation
// payload to an absolute in-workspace file before the deck renders. Models
// reference photos by artifact name ("pexels-photo-….jpeg") or by the path
// they passed to download_file; both are resolved against the bound
// workspace with the same containment rule as file writes. A path that
// escapes the workspace or does not exist fails the write fail-closed, so a
// typo surfaces as an explicit error instead of a deck with silent gaps.
func resolveOfficeSlideImages(workspace string, data map[string]interface{}) error {
	raw, ok := data["slides"]
	if !ok {
		return nil
	}
	slides, ok := raw.([]interface{})
	if !ok {
		return nil // the writer downstream reports the malformed shape itself
	}
	for _, item := range slides {
		slide, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		images, ok := slide["images"].([]interface{})
		if !ok {
			continue
		}
		for _, rawImage := range images {
			image, ok := rawImage.(map[string]interface{})
			if !ok {
				continue
			}
			path, ok := image["path"].(string)
			if !ok || strings.TrimSpace(path) == "" {
				continue
			}
			abs, err := trustedFileWriteResolvePath(workspace, path)
			if err != nil {
				return fmt.Errorf("trusted_office_write_image_path_rejected: %s", strings.TrimSpace(path))
			}
			if info, statErr := os.Stat(abs); statErr != nil || info.IsDir() {
				return fmt.Errorf("trusted_office_write_image_missing: %s", strings.TrimSpace(path))
			}
			image["path"] = abs
		}
	}
	return nil
}

// semanticCanonicalDetailedRejection carries a pre-execution refusal whose
// detail is model-actionable: it echoes the model's own value (an image path
// it supplied), not schema internals, so narrowing it to the generic
// parameter_schema_invalid text would only hide the fix. The generic oracle
// narrowing in semanticModelParameterRejection stays in place for everything
// else; only this typed error bypasses it.
type semanticCanonicalDetailedRejection struct{ text string }

func (e *semanticCanonicalDetailedRejection) Error() string { return e.text }

// semanticCanonicalRejectionText maps a canonicalization failure to the
// model-facing refusal: detailed pre-execution rejections keep their text,
// shape errors against the rendered schema surface their field path (the
// schema is fully rendered to the model, so the path is no oracle — see
// tool.ParameterError), and only authorization-closure or internal failures
// narrow to the generic schema message.
func semanticCanonicalRejectionText(err error) string {
	var detailed *semanticCanonicalDetailedRejection
	if errors.As(err, &detailed) {
		return detailed.text
	}
	var paramErr *tool.ParameterError
	if errors.As(err, &paramErr) && paramErr.Path != "" {
		text := "[system rejected] " + paramErr.Code + ": " + paramErr.Path
		if hint := strings.TrimSpace(paramErr.Hint); hint != "" {
			text += " (" + hint + ")"
		}
		return text
	}
	return "[system rejected] parameter_schema_invalid"
}

// semanticOfficeSlideImageCheck validates presentation image paths at
// canonicalization time — BEFORE the admission transaction consumes the
// one-shot office grant. The paths are the model's own workspace-relative
// references (typically the Path line reported by download_file), so a
// missing or escaping file is a correctable argument mistake, exactly the
// class §4.12 says must never burn a grant. Running the same check inside
// the adapter instead consumed the grant on a path typo and stranded the
// whole deck turn (2026-08-27 birthday-deck turn: two image_missing
// failures, then "office was not available").
func semanticOfficeSlideImageCheck(workspace, argsJSON string) error {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return nil
	}
	slides, ok := parsed["slides"].([]interface{})
	if !ok {
		return nil
	}
	for _, item := range slides {
		slide, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		images, ok := slide["images"].([]interface{})
		if !ok {
			continue
		}
		for _, rawImage := range images {
			image, ok := rawImage.(map[string]interface{})
			if !ok {
				continue
			}
			path, ok := image["path"].(string)
			if !ok || strings.TrimSpace(path) == "" {
				continue
			}
			path = strings.TrimSpace(path)
			abs, err := trustedFileWriteResolvePath(workspace, path)
			if err != nil {
				return &semanticCanonicalDetailedRejection{text: fmt.Sprintf("[system rejected] trusted_office_write_image_path_rejected: %s. The call was refused before execution, so the tool remains available: reference the image by the workspace-relative Path reported when it was acquired, then call again.", path)}
			}
			if info, statErr := os.Stat(abs); statErr != nil || info.IsDir() {
				return &semanticCanonicalDetailedRejection{text: fmt.Sprintf("[system rejected] trusted_office_write_image_missing: %s. The call was refused before execution, so the tool remains available: reference the image by the workspace-relative Path reported when it was acquired, then call again.%s", path, semanticWorkspaceImageHint(workspace))}
			}
		}
	}
	return nil
}

// semanticOfficeSlideChartCheck validates native charts before the office
// grant is consumed, matching the slide-image pre-check.
func semanticOfficeSlideChartCheck(argsJSON string) error {
	var parsed map[string]interface{}
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return nil
	}
	slides, ok := parsed["slides"].([]interface{})
	if !ok {
		return nil
	}
	for i, item := range slides {
		slide, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		raw, ok := slide["charts"]
		if !ok || raw == nil {
			continue
		}
		body, err := json.Marshal(raw)
		if err != nil {
			return &semanticCanonicalDetailedRejection{text: fmt.Sprintf("[system rejected] trusted_office_write_chart_malformed: slide %d. The call was refused before execution, so the tool remains available: pass charts as objects with chart_type, categories, and series.", i+1)}
		}
		var charts []pptx.OutlineChart
		if err := json.Unmarshal(body, &charts); err != nil {
			return &semanticCanonicalDetailedRejection{text: fmt.Sprintf("[system rejected] trusted_office_write_chart_malformed: slide %d. The call was refused before execution, so the tool remains available: chart_type is a string; categories are strings; series.values are numbers of the same length as categories.", i+1)}
		}
		if err := pptx.ValidateSlideCharts(charts); err != nil {
			return &semanticCanonicalDetailedRejection{text: fmt.Sprintf("[system rejected] %s. The call was refused before execution, so the tool remains available: fix the chart payload and call again.", err.Error())}
		}
	}
	return nil
}

// semanticWorkspaceImageHint lists the image files currently at the workspace
// root so an image_missing rejection is self-correcting: the model sees what
// actually exists (typically the artifact name reported by download_file)
// instead of guessing suffixes for two more rounds (2026-08-27 birthday-deck
// turn: three rejections on "cat.jpg" while the file was "cat"). Non-image
// files and subdirectories stay out; the list is capped and sorted for
// determinism.
func semanticWorkspaceImageHint(workspace string) string {
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return ""
	}
	images := make([]string, 0, 8)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg":
			images = append(images, entry.Name())
		}
	}
	if len(images) == 0 {
		return " No image files currently exist at the workspace root."
	}
	sort.Strings(images)
	if len(images) > 8 {
		images = images[:8]
	}
	return fmt.Sprintf(" Image files currently at the workspace root: %s.", strings.Join(images, ", "))
}

func semanticTrustedOfficeWriteResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_office_write_delivery_token")
	}
	if strings.Contains(text, "\"action\"") || strings.Contains(text, "write_excel") {
		return "", fmt.Errorf("trusted_office_write_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_office_write_empty")
	}
	return text, nil
}
