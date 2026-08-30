package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The 2026-08-27 birthday-deck incident: the model downloaded a photo yet
// shipped "📸 [布偶猫照片]" placeholders because the office contract had no
// image channel. Admission must keep slide images intact so the renderer
// receives them.
func TestTrustedOfficeWriteArgsAllowedKeepsSlideImages(t *testing.T) {
	args := map[string]interface{}{
		"path": "deck.pptx",
		"slides": []interface{}{
			map[string]interface{}{
				"title":   "相册",
				"bullets": []interface{}{"第一张"},
				"images": []interface{}{
					map[string]interface{}{"path": "cat.png"},
				},
			},
		},
	}
	path, data, err := semanticTrustedOfficeWriteArgsAllowed(args)
	if err != nil {
		t.Fatalf("admission rejected slide images: %v", err)
	}
	if path != "deck.pptx" {
		t.Fatalf("path lost: %q", path)
	}
	slides, ok := data["slides"].([]interface{})
	if !ok || len(slides) != 1 {
		t.Fatalf("slides lost: %#v", data["slides"])
	}
	slide := slides[0].(map[string]interface{})
	images, ok := slide["images"].([]interface{})
	if !ok || len(images) != 1 {
		t.Fatalf("images stripped by admission: %#v", slide)
	}
}

// Image-path validation happens at canonicalization time, BEFORE admission
// consumes the one-shot office grant (2026-08-27 birthday-deck turn: the
// same check inside the adapter burned the grant on a path typo and the
// model then read the next refusal as "office is gone"). The refusal must
// keep its actionable detail and must say the tool stays available.
func TestSemanticOfficeSlideImageCheckIsPreExecution(t *testing.T) {
	workspace := t.TempDir()
	photo := filepath.Join(workspace, "cat.jpg")
	if err := os.WriteFile(photo, []byte("jpg"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := semanticOfficeSlideImageCheck(workspace, `{"path":"deck.pptx","slides":[{"title":"t","images":[{"path":"cat.jpg"}]}]}`); err != nil {
		t.Fatalf("existing image must pass: %v", err)
	}
	if err := semanticOfficeSlideImageCheck(workspace, `{"path":"deck.pptx","slides":[{"title":"t"}]}`); err != nil {
		t.Fatalf("slides without images must pass: %v", err)
	}
	if err := semanticOfficeSlideImageCheck(workspace, `{"path":"book.xlsx","sheets":[]}`); err != nil {
		t.Fatalf("spreadsheet form must pass: %v", err)
	}

	err := semanticOfficeSlideImageCheck(workspace, `{"path":"deck.pptx","slides":[{"title":"t","images":[{"path":"nope.jpg"}]}]}`)
	if err == nil || !strings.Contains(err.Error(), "trusted_office_write_image_missing") {
		t.Fatalf("missing image must be rejected: %v", err)
	}
	if !strings.Contains(err.Error(), "remains available") {
		t.Fatalf("rejection must state the grant is unconsumed: %v", err)
	}
	// The typed rejection keeps its detail through the canonical boundary;
	// generic validation failures still narrow to the oracle-safe text.
	if got := semanticCanonicalRejectionText(err); !strings.Contains(got, "trusted_office_write_image_missing") {
		t.Fatalf("detailed rejection must survive: %s", got)
	}
	if got := semanticCanonicalRejectionText(errors.New("some validator detail")); got != "[system rejected] parameter_schema_invalid" {
		t.Fatalf("generic failure must narrow: %s", got)
	}

	err = semanticOfficeSlideImageCheck(workspace, `{"path":"deck.pptx","slides":[{"title":"t","images":[{"path":"../outside.png"}]}]}`)
	if err == nil || !strings.Contains(err.Error(), "trusted_office_write_image_path_rejected") {
		t.Fatalf("escaping path must be rejected: %v", err)
	}
}

// Image paths are resolved against the bound workspace with the same
// containment rule as file writes; escaping or missing files fail closed.
func TestResolveOfficeSlideImages(t *testing.T) {
	workspace := t.TempDir()
	photo := filepath.Join(workspace, "cat.png")
	if err := os.WriteFile(photo, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	data := map[string]interface{}{
		"slides": []interface{}{
			map[string]interface{}{
				"title":  "相册",
				"images": []interface{}{map[string]interface{}{"path": "cat.png"}},
			},
		},
	}
	if err := resolveOfficeSlideImages(workspace, data); err != nil {
		t.Fatalf("resolve existing image: %v", err)
	}
	got := data["slides"].([]interface{})[0].(map[string]interface{})["images"].([]interface{})[0].(map[string]interface{})["path"]
	if got != photo {
		t.Fatalf("image path not rewritten to workspace abs: %v", got)
	}

	missing := map[string]interface{}{
		"slides": []interface{}{
			map[string]interface{}{"images": []interface{}{map[string]interface{}{"path": "nope.jpg"}}},
		},
	}
	if err := resolveOfficeSlideImages(workspace, missing); err == nil || !strings.Contains(err.Error(), "trusted_office_write_image_missing") {
		t.Fatalf("missing image must fail closed, got %v", err)
	}

	escaping := map[string]interface{}{
		"slides": []interface{}{
			map[string]interface{}{"images": []interface{}{map[string]interface{}{"path": "../outside.png"}}},
		},
	}
	if err := resolveOfficeSlideImages(workspace, escaping); err == nil || !strings.Contains(err.Error(), "trusted_office_write_image_path_rejected") {
		t.Fatalf("escaping image path must be rejected, got %v", err)
	}
}

// Desktop deliveries land in the bound workspace (user-visible), never only
// in the hidden artifact store (2026-08-27 weather-PDF turns: the reply
// showed a ~/.maclaw path the user never opens).
func TestSaveBase64FileToDirMaterializesUserFacingCopy(t *testing.T) {
	dir := t.TempDir()
	path, err := saveBase64FileToDir(dir, "报告.pdf", "aGVsbG8=")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "hello" {
		t.Fatalf("content: %q err=%v", body, err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("must land in the given dir: %s", path)
	}
	// A name collision yields a distinct suffixed path instead of overwrite.
	second, err := saveBase64FileToDir(dir, "报告.pdf", "aGVsbG8=")
	if err != nil || second == path {
		t.Fatalf("collision handling: %q vs %q err=%v", second, path, err)
	}
}

// An image_missing rejection must be self-correcting: it lists the image
// files actually present at the workspace root so the next call names a real
// file instead of another guess (2026-08-27: three rejections on "cat.jpg"
// while the artifact was "cat").
func TestSemanticOfficeSlideImageCheckListsWorkspaceImages(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"cat.jpg", "dog.png", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := semanticOfficeSlideImageCheck(workspace, `{"path":"deck.pptx","slides":[{"title":"t","images":[{"path":"nope.jpg"}]}]}`)
	if err == nil {
		t.Fatal("missing image must be rejected")
	}
	if !strings.Contains(err.Error(), "cat.jpg") || !strings.Contains(err.Error(), "dog.png") {
		t.Fatalf("rejection must list available images: %v", err)
	}
	if strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("non-image files must stay out: %v", err)
	}

	empty := t.TempDir()
	err = semanticOfficeSlideImageCheck(empty, `{"path":"deck.pptx","slides":[{"title":"t","images":[{"path":"nope.jpg"}]}]}`)
	if err == nil || !strings.Contains(err.Error(), "No image files") {
		t.Fatalf("empty workspace must say so: %v", err)
	}
}

// The 2026-08-27 birthday-deck death: a slide-level subtitle failed canonical
// validation, but the refusal was narrowed to bare parameter_schema_invalid,
// so the model guessed the wrong field three times and the no-progress
// breaker killed the turn. Shape errors against the fully rendered schema are
// no oracle — the refusal must localize the offending field and keep that
// detail through the model-boundary guidance.
func TestSemanticParameterRejectionKeepsShapeErrorPath(t *testing.T) {
	shapeErr := &tool.ParameterError{Code: "parameter_unknown_field", Path: "slides[5].subtitle", Hint: "field is not declared in this tool's rendered schema; remove it or check the spelling"}
	text := semanticCanonicalRejectionText(shapeErr)
	if !strings.Contains(text, "parameter_unknown_field: slides[5].subtitle") {
		t.Fatalf("shape error lost its field path: %s", text)
	}
	model := semanticModelParameterRejection(text)
	if !strings.Contains(model, "slides[5].subtitle") || !strings.Contains(model, "remains available") {
		t.Fatalf("model-facing refusal must keep the path and add guidance: %s", model)
	}
	// Authorization-closure codes carry no path and still narrow to the
	// fixed generic text.
	generic := semanticModelParameterRejection(semanticCanonicalRejectionText(&tool.ParameterError{Code: "parameter_target_not_authorized"}))
	if !strings.HasPrefix(generic, "[system rejected] parameter_schema_invalid.") {
		t.Fatalf("closure errors must stay narrowed: %s", generic)
	}
}
