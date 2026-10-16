package guiapp

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOversizedPDFStillCountsAsChanged(t *testing.T) {
	root := t.TempDir()
	before := snapshotWorkspaceDocuments(root)
	pdf := filepath.Join(root, "album.pdf")
	f, err := os.Create(pdf)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(semanticOfficeArtifactMaxBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got := changedWorkspaceDocuments(before, snapshotWorkspaceDocuments(root))
	if len(got) != 1 || got[0] != pdf {
		t.Fatalf("oversized pdf hidden from this turn: %v", got)
	}
}

func TestUnreadableWorkspaceIsNotAnEmptyBaseline(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshotWorkspaceDocumentsIfReadable(file); ok {
		t.Fatal("a file path must not snapshot as an empty workspace")
	}
}

func TestChangedWorkspaceDocumentsSeesNewPDFOnly(t *testing.T) {
	root := t.TempDir()
	before := snapshotWorkspaceDocuments(root)
	if err := os.WriteFile(filepath.Join(root, "notes.py"), []byte("print(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(root, "album.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := changedWorkspaceDocuments(before, snapshotWorkspaceDocuments(root))
	if len(got) != 1 || got[0] != pdf {
		t.Fatalf("changed = %v, want %s", got, pdf)
	}
}

func TestTaskWorkspaceDeliversUnsentPDFNotHostCopy(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "album.pdf")
	already := filepath.Join(root, "text.pdf")
	copy := filepath.Join(root, "text_162533_939.pdf")
	deck := filepath.Join(root, "deck.pptx")
	if err := os.WriteFile(real, []byte("%PDF-images"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(already, []byte("%PDF-text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copy, []byte("%PDF-text-copy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deck, []byte("pptx"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:         root,
		workspaceDocBaseline:     snapshotWorkspaceDocuments(root),
		semanticDeliveryFileData: "already-registered",
		deliveredPaths:           []string{real},
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || cb.deliveredPaths[0] != real {
		t.Fatalf("delivered = %v, want only %s", cb.deliveredPaths, real)
	}
	cb.deliveredPaths = nil
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 0 {
		t.Fatalf("earlier pdf attached again: %v", cb.deliveredPaths)
	}
}

func TestProducedPDFOutranksHostCopy(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{workspaceDocRoot: root, workspaceDocBaseline: snapshotWorkspaceDocuments(root)}
	real := filepath.Join(root, "album.pdf")
	copy := filepath.Join(root, "album_162533_939.pdf")
	if err := os.WriteFile(copy, []byte("%PDF-text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("%PDF-images"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || cb.deliveredPaths[0] != real {
		t.Fatalf("delivered = %v, want only %s", cb.deliveredPaths, real)
	}
}

func TestTaskIdentityReadsWorkspaceChild(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data", "tasks", "job")
	ws := filepath.Join(root, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(ws, "old.pdf")
	if err := os.WriteFile(old, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:       root,
		workspaceDocBaseline:   snapshotWorkspaceDocuments(root),
		workspaceDocBaselineAt: time.Now(),
	}
	pdf := filepath.Join(ws, "album.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-images"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || cb.deliveredPaths[0] != pdf {
		t.Fatalf("delivered = %v, want only the file written after the baseline", cb.deliveredPaths)
	}
}

// Channel delivery writes the PDF after this turn's directory diff. The next
// turn's baseline already contains it, so the card stays on the new file
// (2026-09-26: 崇州天气预报.pdf reappeared beside 布偶宝鉴).
func TestMaterializedPDFIsNotAttachedOnTheNextTurn(t *testing.T) {
	app := newProjectSearchTestApp(t)
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	const owner = "desktop-user"
	app.assistantSessionWorkingDirs.Store(owner, root)
	h := &IMMessageHandler{app: app}

	projectTurn := func(name, body string) *IMAgentResponse {
		t.Helper()
		cb := &sharedAgentLoopCallbacks{
			handler:                  h,
			userID:                   owner,
			platform:                 "desktop",
			workspaceDocRoot:         root,
			workspaceDocBaseline:     snapshotWorkspaceDocuments(root),
			semanticDeliveryFileData: base64.StdEncoding.EncodeToString([]byte(body)),
			semanticDeliveryFileName: name,
		}
		resp := &IMAgentResponse{FileData: cb.semanticDeliveryFileData, FileName: cb.semanticDeliveryFileName}
		cb.attachProducedWorkspaceDocuments()
		if len(cb.deliveredPaths) > 0 {
			resp.LocalFilePaths = append([]string(nil), cb.deliveredPaths...)
			resp.LocalFilePath = cb.deliveredPaths[0]
		}
		materializeSemanticDeliveryFileForLocalChat(resp, cb)
		return resp
	}

	first := projectTurn("崇州天气预报.pdf", "%PDF-weather")
	if got := baseNames(first.LocalFilePaths); len(got) != 1 || got[0] != "崇州天气预报.pdf" {
		t.Fatalf("turn 1 paths = %v", first.LocalFilePaths)
	}
	second := projectTurn("布偶宝鉴.pdf", "%PDF-ragdoll")
	if got := baseNames(second.LocalFilePaths); len(got) != 1 || got[0] != "布偶宝鉴.pdf" {
		t.Fatalf("turn 2 mixed earlier files into this round: %v", second.LocalFilePaths)
	}
}

// A PDF already on disk at baseline belongs to the earlier round.
func TestUnchangedEarlierPDFIsNotAttachedWithoutLedger(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "崇州天气预报.pdf")
	if err := os.WriteFile(old, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseline := snapshotWorkspaceDocuments(root)
	pending := &sharedAgentLoopCallbacks{
		workspaceDocRoot:         root,
		workspaceDocBaseline:     baseline,
		semanticDeliveryFileData: "pending-new-pdf",
	}
	pending.attachProducedWorkspaceDocuments()
	if len(pending.deliveredPaths) != 0 {
		t.Fatalf("earlier pdf attached before this turn wrote one: %v", pending.deliveredPaths)
	}
	fresh := filepath.Join(root, "布偶宝鉴.pdf")
	if err := os.WriteFile(fresh, []byte("%PDF-new"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:         root,
		workspaceDocBaseline:     baseline,
		semanticDeliveryFileData: "pending-new-pdf",
	}
	cb.attachProducedWorkspaceDocuments()
	if got := baseNames(cb.deliveredPaths); len(got) != 1 || got[0] != "布偶宝鉴.pdf" {
		t.Fatalf("delivered = %v, want only this turn's pdf", cb.deliveredPaths)
	}
}

func TestChangedWorkspaceDocumentsIgnoresDriveLetterCase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(root, "album.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshotWorkspaceDocuments(root)
	alt := root
	if len(alt) >= 2 && alt[1] == ':' {
		if alt[0] >= 'A' && alt[0] <= 'Z' {
			alt = strings.ToLower(alt[:1]) + alt[1:]
		} else {
			alt = strings.ToUpper(alt[:1]) + alt[1:]
		}
	}
	if alt == root {
		t.Skip("path has no drive letter to flip")
	}
	got := changedWorkspaceDocuments(before, snapshotWorkspaceDocuments(alt))
	if len(got) != 0 {
		t.Fatalf("drive-letter case looked like a new file: %v", got)
	}
}

func TestHostCopyDoesNotHideADifferentDocument(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:     root,
		workspaceDocBaseline: snapshotWorkspaceDocuments(root),
	}
	for _, name := range []string{"album.pdf", "notes_162533_939.pdf"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("%PDF-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cb.attachProducedWorkspaceDocuments()
	got := map[string]bool{}
	for _, path := range cb.deliveredPaths {
		got[filepath.Base(path)] = true
	}
	if len(got) != 2 || !got["album.pdf"] || !got["notes_162533_939.pdf"] {
		t.Fatalf("delivered = %v, want both documents", cb.deliveredPaths)
	}
}

func TestRewrittenPDFStillAttaches(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	pdf := filepath.Join(root, "album.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:     root,
		workspaceDocBaseline: snapshotWorkspaceDocuments(root),
	}
	if err := os.WriteFile(pdf, []byte("%PDF-rewritten"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || filepath.Base(cb.deliveredPaths[0]) != "album.pdf" {
		t.Fatalf("rewritten pdf = %v", cb.deliveredPaths)
	}
}

func baseNames(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, filepath.Base(path))
	}
	return out
}

// pdflatex writes next to the .tex, which for a template lives under a
// subdirectory. The sample PDFs shipped in that template, and anything under
// node_modules, must stay off the card.
func TestLatexPDFUnderTemplateDirIsDelivered(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	paper := filepath.Join(root, "elsarticle")
	if err := os.MkdirAll(filepath.Join(paper, "doc"), 0o755); err != nil {
		t.Fatal(err)
	}
	sample := filepath.Join(paper, "doc", "elsdoc.pdf")
	if err := os.WriteFile(sample, []byte("%PDF-sample"), 0o644); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(junk, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junk, "bundle.pdf"), []byte("%PDF-junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:     root,
		workspaceDocBaseline: snapshotWorkspaceDocuments(root),
	}
	produced := filepath.Join(paper, "elsarticle-template-num.pdf")
	if err := os.WriteFile(produced, []byte("%PDF-paper"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(junk, "later.pdf"), []byte("%PDF-later"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || cb.deliveredPaths[0] != produced {
		t.Fatalf("delivered = %v, want %s", cb.deliveredPaths, produced)
	}
}

func TestWorkspaceDocumentWalkDepthAndHiddenDirs(t *testing.T) {
	root := t.TempDir()
	visible := filepath.Join(root, "a", "b", "c", "d", "ok.pdf")
	tooDeep := filepath.Join(root, "a", "b", "c", "d", "e", "too.pdf")
	hidden := filepath.Join(root, ".cache", "secret.pdf")
	for _, path := range []string{visible, tooDeep, hidden} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("%PDF"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := snapshotWorkspaceDocuments(root)
	if _, ok := got[deliveredWorkspaceDocKey(visible)]; !ok {
		t.Fatalf("shallow pdf missing: %v", keysOf(got))
	}
	if _, ok := got[deliveredWorkspaceDocKey(tooDeep)]; ok {
		t.Fatal("pdf past the depth limit was recorded")
	}
	if _, ok := got[deliveredWorkspaceDocKey(hidden)]; ok {
		t.Fatal("pdf under a dot directory was recorded")
	}
}

func TestWorkspaceDocumentWalkSkipsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.pdf")
	if err := os.WriteFile(secret, []byte("%PDF-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "alias.pdf")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, stamp := range snapshotWorkspaceDocuments(root) {
		if filepath.Base(stamp.path) == "secret.pdf" || filepath.Base(stamp.path) == "alias.pdf" {
			t.Fatalf("symlink leaked into the snapshot: %s", stamp.path)
		}
	}
}

func TestWorkspaceDocumentBudgetKeepsAlphabeticalChild(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"b", "a"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".pdf"), []byte("%PDF-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scan, ok := scanWorkspaceDocuments(root, 2)
	if !ok {
		t.Fatal("root should still be readable")
	}
	if !scan.truncated {
		t.Fatal("the later sibling should be left outside the budget")
	}
	kept := filepath.Join(root, "a", "a.pdf")
	dropped := filepath.Join(root, "b", "b.pdf")
	if _, ok := scan.files[deliveredWorkspaceDocKey(kept)]; !ok {
		t.Fatalf("alphabetical child missing: %v", keysOf(scan.files))
	}
	if _, ok := scan.files[deliveredWorkspaceDocKey(dropped)]; ok {
		t.Fatal("later sibling was read past the budget")
	}
}

func TestWorkspaceDocumentScanStopsAtDirectoryBudget(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one", "two", "three"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".pdf"), []byte("%PDF"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scan, ok := scanWorkspaceDocuments(root, 1)
	if !ok {
		t.Fatal("root should still be readable")
	}
	if !scan.truncated {
		t.Fatal("budget of one directory should truncate before the children")
	}
	if len(scan.files) != 0 {
		t.Fatalf("child pdfs were read past the budget: %v", keysOf(scan.files))
	}
}

func TestTruncatedBaselineDoesNotResurfaceOldPDF(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".maclaw", "data", "tasks", "job", "workspace")
	if err := os.MkdirAll(filepath.Join(root, "elsarticle"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "elsarticle", "sample.pdf")
	if err := os.WriteFile(old, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:              root,
		workspaceDocBaseline:          map[string]workspaceDocumentStamp{},
		workspaceDocBaselineAt:        time.Now().Add(-time.Second),
		workspaceDocBaselineTruncated: true,
	}
	fresh := filepath.Join(root, "elsarticle", "paper.pdf")
	if err := os.WriteFile(fresh, []byte("%PDF-paper"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 1 || cb.deliveredPaths[0] != fresh {
		t.Fatalf("delivered = %v, want only %s", cb.deliveredPaths, fresh)
	}
}

func keysOf(stamps map[string]workspaceDocumentStamp) []string {
	out := make([]string, 0, len(stamps))
	for key := range stamps {
		out = append(out, key)
	}
	return out
}

func TestProjectWorkspaceDoesNotDumpOldPDFs(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "manual.pdf")
	if err := os.WriteFile(old, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{
		workspaceDocRoot:         root,
		workspaceDocBaseline:     snapshotWorkspaceDocuments(root),
		semanticDeliveryFileData: "weather-card",
	}
	cb.attachProducedWorkspaceDocuments()
	if len(cb.deliveredPaths) != 0 {
		t.Fatalf("project pdf was attached without being produced this turn: %v", cb.deliveredPaths)
	}
}
