package cloudagent

import "unicode/utf8"

// Preview baselines are the first pre-write text of a path, kept so a later
// cloud_read_file can return the original beside the current bytes. The
// desktop preview then shows a real diff. Creates have no baseline. A second
// write must not replace the first snapshot with intermediate bytes.
const (
	previewBaselinePerFile = 1 << 20
	previewBaselineTotal   = 8 << 20
)

// rememberPreviewBaseline stores text the first time rel is seen. Empty text,
// invalid UTF-8, and snapshots over the per-file or total cap are skipped:
// a clipped baseline would paint a diff that is not the file the turn started
// from. The caller must not hold jailMu; this method only takes previewMu.
func (inst *instance) rememberPreviewBaseline(rel, text string) {
	if inst == nil || rel == "" || text == "" || !utf8.ValidString(text) || len(text) > previewBaselinePerFile {
		return
	}
	inst.previewMu.Lock()
	defer inst.previewMu.Unlock()
	if inst.previewBaselines == nil {
		inst.previewBaselines = map[string]string{}
	}
	if _, exists := inst.previewBaselines[rel]; exists {
		return
	}
	total := len(text)
	for _, existing := range inst.previewBaselines {
		total += len(existing)
	}
	if total > previewBaselineTotal {
		return
	}
	inst.previewBaselines[rel] = text
}

func (inst *instance) previewBaseline(rel string) (string, bool) {
	if inst == nil || rel == "" {
		return "", false
	}
	inst.previewMu.Lock()
	defer inst.previewMu.Unlock()
	text, ok := inst.previewBaselines[rel]
	if !ok || text == "" {
		return "", false
	}
	return text, true
}

// previewOriginalForRead is the stashed baseline for a file read, or "" when
// the read itself is truncated. A truncated body paired with a full baseline
// is not a diff the preview can show.
func previewOriginalForRead(inst *instance, root, abs string, contentTruncated bool) string {
	if inst == nil || contentTruncated {
		return ""
	}
	text, ok := inst.previewBaseline(canonicalArtifactRel(root, abs))
	if !ok || !utf8.ValidString(text) || len(text) > previewBaselinePerFile {
		return ""
	}
	return text
}
