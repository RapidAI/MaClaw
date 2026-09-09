package agentservice

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// SemanticBehaviorSnapshotUpdateEnv is the shared UPDATE flag both host tests
// must honor so a typo cannot refresh one golden and leave the other checked.
const SemanticBehaviorSnapshotUpdateEnv = "UPDATE_SEMANTIC_BEHAVIOR_SNAPSHOT"

// SemanticBehaviorSnapshotUpdateRequested reports whether snapshot goldens
// should be rewritten.
func SemanticBehaviorSnapshotUpdateRequested() bool {
	return os.Getenv(SemanticBehaviorSnapshotUpdateEnv) == "1"
}

// SemanticNeedFamilySnapshot is the host-comparable identity of one need
// family after resolution. Capability, polarity, and qualifiers identify the
// family; Required/Optional count the siblings. Need IDs and grant tokens are
// omitted: GUI prefixes need: / need:coding: and grant tokens are random.
type SemanticNeedFamilySnapshot struct {
	Capability string
	Polarity   string
	Qualifiers string
	Required   int
	Optional   int
}

// SnapshotSemanticNeedFamilies collapses resolved needs onto repeat families
// so GUI IM rules and reviewed headless rules can be compared without IDs.
func SnapshotSemanticNeedFamilies(needs []coretool.CapabilityNeed) []SemanticNeedFamilySnapshot {
	type acc struct {
		capability string
		polarity   string
		qualifiers string
		required   int
		optional   int
	}
	order := make([]string, 0, len(needs))
	byKey := make(map[string]*acc, len(needs))
	for _, need := range needs {
		polarity := string(need.Polarity)
		if polarity == "" {
			polarity = string(coretool.NeedRequire)
		}
		qualifiers := coretool.NeedQualifierKey(need.Qualifiers)
		key := string(need.Capability) + "\x00" + polarity + "\x00" + qualifiers
		entry, ok := byKey[key]
		if !ok {
			entry = &acc{capability: string(need.Capability), polarity: polarity, qualifiers: qualifiers}
			byKey[key] = entry
			order = append(order, key)
		}
		if need.Required {
			entry.required++
		} else {
			entry.optional++
		}
	}
	sort.Strings(order)
	out := make([]SemanticNeedFamilySnapshot, 0, len(order))
	for _, key := range order {
		entry := byKey[key]
		out = append(out, SemanticNeedFamilySnapshot{
			Capability: entry.capability,
			Polarity:   entry.polarity,
			Qualifiers: entry.qualifiers,
			Required:   entry.required,
			Optional:   entry.optional,
		})
	}
	return out
}

// FormatSemanticNeedFamilySnapshot renders one family as a stable snapshot line.
func FormatSemanticNeedFamilySnapshot(family SemanticNeedFamilySnapshot) string {
	return fmt.Sprintf("%s|%s|%s|req=%d|opt=%d", family.Capability, family.Polarity, snapshotQualifierKey(family.Qualifiers), family.Required, family.Optional)
}

// SemanticPlanSurfaceSnapshot is the host-comparable plan/first-wave face.
// Adapter names are included; grant tokens, selection IDs, and plan digests
// are omitted because tokens are random and digests embed catalog generation.
type SemanticPlanSurfaceSnapshot struct {
	Selections []string
	Unmet      []string
	Omitted    []string
	FirstWave  []string
}

// SnapshotSemanticPlanSurface projects one ToolPlan onto
// capability|qualifiers|adapter|phase lines. Repeat siblings collapse with n=.
// FirstWave is the NextExposedSelections closure of ReadySelections with an
// empty completed/granted table — the first model-visible grant wave before
// any token is issued.
func SnapshotSemanticPlanSurface(plan coretool.ToolPlan, needs []coretool.CapabilityNeed) SemanticPlanSurfaceSnapshot {
	capabilityOf := planNeedCapabilityLookup(plan, needs)
	selections := make([]string, 0, len(plan.Selections))
	for _, selection := range plan.Selections {
		selections = append(selections, snapshotSelectionLine(selection, capabilityOf))
	}
	unmet := make([]string, 0, len(plan.Unmet))
	for _, item := range plan.Unmet {
		unmet = append(unmet, capabilityOf(item.NeedID)+"|"+snapshotReason(item.ReasonCode))
	}
	omitted := make([]string, 0, len(plan.Omitted))
	for _, item := range plan.Omitted {
		omitted = append(omitted, capabilityOf(item.NeedID)+"|"+snapshotReason(item.ReasonCode))
	}
	ready := plan.ReadySelections(nil)
	exposed := coretool.NextExposedSelections(ready, nil, nil, nil, nil)
	first := make([]string, 0, len(exposed))
	for _, selection := range ready {
		if !exposed[selection.ID] {
			continue
		}
		first = append(first, snapshotFirstWaveLine(selection, capabilityOf))
	}
	return SemanticPlanSurfaceSnapshot{
		Selections: collapseSnapshotCounts(sortedSnapshotLines(selections)),
		Unmet:      collapseSnapshotCounts(sortedSnapshotLines(unmet)),
		Omitted:    collapseSnapshotCounts(sortedSnapshotLines(omitted)),
		FirstWave:  collapseSnapshotCounts(sortedSnapshotLines(first)),
	}
}

func planNeedCapabilityLookup(plan coretool.ToolPlan, needs []coretool.CapabilityNeed) func(string) string {
	byID := make(map[string]string, len(needs)+len(plan.Selections))
	byFamily := make(map[string]string, len(needs)+len(plan.Selections))
	add := func(id, capability string) {
		id = strings.TrimSpace(id)
		capability = strings.TrimSpace(capability)
		if id == "" || capability == "" {
			return
		}
		byID[id] = capability
		byFamily[coretool.RepeatFamilyID(id)] = capability
	}
	for _, need := range needs {
		add(need.ID, string(need.Capability))
	}
	for _, selection := range plan.Selections {
		add(selection.NeedID, string(selection.FitProof.MatchedCapability))
	}
	return func(needID string) string {
		needID = strings.TrimSpace(needID)
		if cap := byID[needID]; cap != "" {
			return cap
		}
		if cap := byFamily[coretool.RepeatFamilyID(needID)]; cap != "" {
			return cap
		}
		return snapshotCapabilityFromNeedID(needID)
	}
}

// snapshotCapabilityFromNeedID recovers the capability from a minted need ID
// (need:<capability>:<digest> or need:coding:<capability>:<digest>) so unmet
// lines stay comparable even when the caller did not pass the original needs.
func snapshotCapabilityFromNeedID(needID string) string {
	id := coretool.RepeatFamilyID(strings.TrimSpace(needID))
	if cap, ok := mintedNeedCapability(id); ok {
		return cap
	}
	return id
}

func mintedNeedCapability(id string) (string, bool) {
	const digestLen = 12
	for _, prefix := range []string{"need:coding:", "need:"} {
		rest, ok := strings.CutPrefix(id, prefix)
		if !ok {
			continue
		}
		cut := strings.LastIndex(rest, ":")
		if cut <= 0 {
			continue
		}
		digest := rest[cut+1:]
		capability := rest[:cut]
		if len(digest) != digestLen || strings.TrimSpace(capability) == "" {
			continue
		}
		return capability, true
	}
	return "", false
}

func snapshotSelectionLine(selection coretool.PlannedSelection, capabilityOf func(string) string) string {
	phase := string(selection.Phase)
	if phase == "" {
		phase = string(coretool.PlanPhaseExecution)
	}
	return snapshotSelectionCapability(selection, capabilityOf) + "|" + snapshotSelectionQualifiers(selection) + "|" + snapshotAdapter(selection.AdapterName) + "|" + phase
}

func snapshotFirstWaveLine(selection coretool.PlannedSelection, capabilityOf func(string) string) string {
	return snapshotSelectionCapability(selection, capabilityOf) + "|" + snapshotSelectionQualifiers(selection) + "|" + snapshotAdapter(selection.AdapterName)
}

func snapshotSelectionQualifiers(selection coretool.PlannedSelection) string {
	return snapshotQualifierKey(coretool.NeedQualifierKey(selection.FitProof.QualifierBindings))
}

func snapshotQualifierKey(key string) string {
	if key == "" {
		return "-"
	}
	return strings.ReplaceAll(key, "\x1f", ",")
}

func snapshotSelectionCapability(selection coretool.PlannedSelection, capabilityOf func(string) string) string {
	if cap := strings.TrimSpace(string(selection.FitProof.MatchedCapability)); cap != "" {
		return cap
	}
	return capabilityOf(selection.NeedID)
}

func snapshotAdapter(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "-"
	}
	return name
}

func snapshotReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "-"
	}
	return reason
}

func sortedSnapshotLines(lines []string) []string {
	out := append([]string(nil), lines...)
	sort.Strings(out)
	return out
}

func collapseSnapshotCounts(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	counts := make(map[string]int, len(lines))
	order := make([]string, 0, len(lines))
	for _, line := range lines {
		if counts[line] == 0 {
			order = append(order, line)
		}
		counts[line]++
	}
	out := make([]string, 0, len(order))
	for _, line := range order {
		if counts[line] == 1 {
			out = append(out, line)
			continue
		}
		out = append(out, fmt.Sprintf("%s|n=%d", line, counts[line]))
	}
	return out
}

// SemanticNeedFamilyRequiredIdentities is the overlapping-host contract:
// capability + polarity + qualifiers of every required family, minus
// catalog-specific office write / local launch. Optional bundle ceilings and
// office/launch qualifiers stay host-private.
func SemanticNeedFamilyRequiredIdentities(families []SemanticNeedFamilySnapshot) []string {
	out := make([]string, 0, len(families))
	for _, family := range families {
		if family.Required < 1 {
			continue
		}
		if catalogSpecificCapability(family.Capability) {
			continue
		}
		out = append(out, family.Capability+"|"+family.Polarity+"|"+snapshotQualifierKey(family.Qualifiers))
	}
	sort.Strings(out)
	return out
}

func catalogSpecificCapability(capability string) bool {
	switch coretool.CapabilityID(capability) {
	case coretool.CapabilityDocumentWriteOffice, coretool.CapabilitySystemLaunchLocal:
		return true
	default:
		return false
	}
}

// SemanticBehaviorSnapshotCase is one frozen classification in the GUI/srv
// behavior snapshot cohort. Names are the testdata keys.
type SemanticBehaviorSnapshotCase struct {
	Name   string
	Result intent.ClassificationResult
}

// SemanticBehaviorSnapshotCases is the shared frozen cohort. GUI and
// agentservice tests must iterate this list so a new family cannot land on
// only one host's snapshot.
func SemanticBehaviorSnapshotCases() []SemanticBehaviorSnapshotCase {
	const confidence = 0.98
	return []SemanticBehaviorSnapshotCase{
		{Name: "search", Result: intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: confidence}},
		{Name: "live_data", Result: intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: confidence}},
		{Name: "office", Result: intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: confidence}},
		{Name: "office_live_data", Result: intent.ClassificationResult{Primary: intent.LabelOffice, Secondary: []intent.IntentLabel{intent.LabelLiveData}, Confidence: confidence}},
		{Name: "document_generate", Result: intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: confidence}},
		{Name: "document_generate_live_data", Result: intent.ClassificationResult{Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: confidence}},
		{Name: "file_read", Result: intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: confidence}},
		{Name: "shell", Result: intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: confidence}},
		{Name: "coding", Result: intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: confidence}},
		{Name: "current_time", Result: intent.ClassificationResult{Primary: intent.LabelCurrentTime, Confidence: confidence}},
		{Name: "web_fetch", Result: intent.ClassificationResult{Primary: intent.LabelWebFetch, Confidence: confidence}},
		{Name: "screenshot", Result: intent.ClassificationResult{Primary: intent.LabelScreenshot, Confidence: confidence}},
		{Name: "knowledge_read", Result: intent.ClassificationResult{Primary: intent.LabelKnowledgeRead, Confidence: confidence}},
		{Name: "audio_deliver", Result: intent.ClassificationResult{Primary: intent.LabelAudioDeliver, Confidence: confidence}},
		{Name: "non_coding", Result: intent.ClassificationResult{Primary: intent.LabelNonCoding, Confidence: confidence}},
		{Name: "degraded_search", Result: intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: confidence, Degraded: true}},
		{Name: "low_confidence_search", Result: intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.50}},
	}
}

// SemanticBehaviorRequiredIdentityComparable reports whether IM and reviewed
// required-need identities must match for this classification. Office / launch
// qualifiers stay catalog-specific.
func SemanticBehaviorRequiredIdentityComparable(result intent.ClassificationResult) bool {
	im := IMSemanticIntentCapabilityNeedRules()
	reviewed := ReviewedDynamicIntentCapabilityNeedRules()
	for _, label := range result.Labels() {
		if label.IsNonCapabilityLabel() || catalogSpecificIntentLabel(label) {
			continue
		}
		if len(im[label]) > 0 && len(reviewed[label]) > 0 {
			return true
		}
	}
	return false
}

func catalogSpecificIntentLabel(label intent.IntentLabel) bool {
	switch label {
	case intent.LabelOffice, intent.LabelDocumentOpen, intent.LabelAppLaunch:
		return true
	default:
		return false
	}
}

// EncodeSemanticPlanSurfaceSnapshot renders one host's plan/surface snapshot
// file. Case order follows SemanticBehaviorSnapshotCases.
func EncodeSemanticPlanSurfaceSnapshot(host string, managedByCase map[string]bool, got map[string]SemanticPlanSurfaceSnapshot) []byte {
	var b strings.Builder
	b.WriteString("# GUI/srv plan and first-wave surface snapshot.\n")
	b.WriteString("# Lines are capability|qualifiers|adapter|phase (sel) or capability|qualifiers|adapter (first).\n")
	b.WriteString("# Grant tokens, selection IDs, and plan digests are omitted.\n")
	b.WriteString("# Update with " + SemanticBehaviorSnapshotUpdateEnv + "=1 after reviewing a face change.\n")
	for _, tc := range SemanticBehaviorSnapshotCases() {
		if !managedByCase[tc.Name] {
			fmt.Fprintf(&b, "%s\t%s\tunmanaged\n", host, tc.Name)
			continue
		}
		snap := got[tc.Name]
		wrote := false
		write := func(kind, line string) {
			fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", host, tc.Name, kind, line)
			wrote = true
		}
		for _, line := range snap.Selections {
			write("sel", line)
		}
		for _, line := range snap.FirstWave {
			write("first", line)
		}
		for _, line := range snap.Unmet {
			write("unmet", line)
		}
		for _, line := range snap.Omitted {
			write("omit", line)
		}
		if !wrote {
			fmt.Fprintf(&b, "%s\t%s\tmanaged\n", host, tc.Name)
		}
	}
	return []byte(b.String())
}

// DiffSnapshotBytes reports the first differing line of two snapshot files.
func DiffSnapshotBytes(want, got []byte) string {
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if bytes.Equal(want, got) {
		return ""
	}
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	n := len(wantLines)
	if len(gotLines) < n {
		n = len(gotLines)
	}
	for i := 0; i < n; i++ {
		if wantLines[i] == gotLines[i] {
			continue
		}
		return fmt.Sprintf("first mismatch at line %d\n- %s\n+ %s", i+1, wantLines[i], gotLines[i])
	}
	return fmt.Sprintf("line count want=%d got=%d", len(wantLines), len(gotLines))
}

// SyncSnapshotFile writes encoded when update is set, otherwise diffs against
// the file on disk. Both host tests use this so UPDATE_SEMANTIC_BEHAVIOR_SNAPSHOT
// cannot update one golden and leave the other stale-checked by a second copier.
func SyncSnapshotFile(path string, encoded []byte, update bool) error {
	if update {
		if existing, err := os.ReadFile(path); err == nil && DiffSnapshotBytes(existing, encoded) == "" {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return fmt.Errorf("write snapshot %s: %w", path, err)
		}
		return nil
	}
	want, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read snapshot %s: %w (run with %s=1 to create)", path, err, SemanticBehaviorSnapshotUpdateEnv)
	}
	if diff := DiffSnapshotBytes(want, encoded); diff != "" {
		return fmt.Errorf("snapshot %s drifted\n%s\nupdate with %s=1 only after reviewing the face change", path, diff, SemanticBehaviorSnapshotUpdateEnv)
	}
	return nil
}

// PlanSurfaceFirstWaveIdentities is the adapter-stripped first-wave contract:
// capability|qualifiers, minus catalog-specific office write / local launch.
func PlanSurfaceFirstWaveIdentities(lines []string) []string {
	out := make([]string, 0, len(lines))
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		if i := strings.LastIndex(line, "|n="); i >= 0 {
			line = line[:i]
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 || catalogSpecificCapability(parts[0]) {
			continue
		}
		id := parts[0] + "|" + parts[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// EncodedPlanSurfaceKindLines returns the payload column for host/case/kind.
func EncodedPlanSurfaceKindLines(encoded []byte, host, caseName, kind string) []string {
	encoded = bytes.ReplaceAll(encoded, []byte("\r\n"), []byte("\n"))
	var out []string
	for _, line := range strings.Split(string(encoded), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) < 4 {
			continue
		}
		if fields[0] == host && fields[1] == caseName && fields[2] == kind {
			out = append(out, fields[3])
		}
	}
	return out
}

// PlanSurfaceFirstWaveParityErrors reports overlapping first-wave identity
// drift (capability|qualifiers, adapters stripped) for cases both hosts
// actually planned. Screenshot/audio-deliver with no selections are skipped.
// If the left host planned comparable cases and the right encoding has no
// matching sel lines (wrong host column, empty peer, need-family file), that
// is drift — not a successful skip.
func PlanSurfaceFirstWaveParityErrors(leftHost string, left []byte, rightHost string, right []byte) []string {
	var errs []string
	planned, compared := 0, 0
	for _, tc := range SemanticBehaviorSnapshotCases() {
		if !SemanticBehaviorRequiredIdentityComparable(tc.Result) {
			continue
		}
		leftSel := EncodedPlanSurfaceKindLines(left, leftHost, tc.Name, "sel")
		if len(leftSel) == 0 {
			continue
		}
		planned++
		rightSel := EncodedPlanSurfaceKindLines(right, rightHost, tc.Name, "sel")
		if len(rightSel) == 0 {
			continue
		}
		compared++
		a := PlanSurfaceFirstWaveIdentities(EncodedPlanSurfaceKindLines(left, leftHost, tc.Name, "first"))
		b := PlanSurfaceFirstWaveIdentities(EncodedPlanSurfaceKindLines(right, rightHost, tc.Name, "first"))
		if !slices.Equal(a, b) {
			errs = append(errs, fmt.Sprintf("%s first-wave identities %s=%v %s=%v", tc.Name, leftHost, a, rightHost, b))
		}
	}
	if planned > 0 && compared == 0 {
		errs = append(errs, fmt.Sprintf("no overlapping first-wave cases compared (%s had selections, %s had none)", leftHost, rightHost))
	}
	return errs
}

// CheckPlanSurfaceFirstWaveParity loads the peer host's committed plan/surface
// golden and reports overlapping first-wave identity drift. Adapters may
// differ; capability|qualifiers of the first grant wave must not.
func CheckPlanSurfaceFirstWaveParity(thisHost string, encoded []byte, peerHost, peerPath string) error {
	peer, err := os.ReadFile(peerPath)
	if err != nil {
		return fmt.Errorf("read peer plan snapshot %s: %w", peerPath, err)
	}
	errs := PlanSurfaceFirstWaveParityErrors(thisHost, encoded, peerHost, peer)
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errs, "\n"))
}
