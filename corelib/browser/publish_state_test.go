package browser

import "testing"

func TestEditorReadyRequiresRemotePicturesAndNoUpload(t *testing.T) {
	ready, why := editorReady(editorReadyFacts{Expected: 2, Remote: 2})
	if !ready || why != "" {
		t.Fatalf("ready=%v %q", ready, why)
	}
	ready, why = editorReady(editorReadyFacts{Expected: 10, Remote: 8, Pending: 2, UploadsSeen: 10})
	if ready || why == "" {
		t.Fatalf("local previews after an upload were ready: %v %q", ready, why)
	}
	ready, why = editorReady(editorReadyFacts{Expected: 10, Remote: 8})
	if ready || why == "" {
		t.Fatalf("a short picture count was ready: %v %q", ready, why)
	}
	// No upload was started: the editor holds the picture the submit sends.
	ready, why = editorReady(editorReadyFacts{Expected: 1, Pending: 1})
	if !ready || why != "" {
		t.Fatalf("local picture with no upload: ready=%v %q", ready, why)
	}
	ready, why = editorReady(editorReadyFacts{Expected: 10, Remote: 10, UploadsOpen: 1})
	if ready || why == "" {
		t.Fatalf("in-flight upload was ready: %v %q", ready, why)
	}
	ready, why = editorReady(editorReadyFacts{Expected: 1, Remote: 1, ProgressBusy: true})
	if ready {
		t.Fatal("busy progress was ready")
	}
	ready, _ = editorReady(editorReadyFacts{})
	if !ready {
		t.Fatal("a post with no pictures should be ready")
	}
}

func TestSubmitOutcomeIsIdempotent(t *testing.T) {
	if got := submitOutcome(submitFacts{WritesOpen: 1}); got != submitPending {
		t.Fatalf("open write: %s", got)
	}
	if got := submitOutcome(submitFacts{NetPublished: true}); got != submitPublished {
		t.Fatalf("net: %s", got)
	}
	if got := submitOutcome(submitFacts{ListKnown: true, Listed: true, EditorKnown: true, Editor: true}); got != submitPublished {
		t.Fatalf("listed: %s", got)
	}
	// The list was read and the post is not on it, and the editor is still
	// there: the one case a second click does not create a duplicate.
	if got := submitOutcome(submitFacts{EditorKnown: true, Editor: true, ListKnown: true, Listed: false}); got != submitInEditor {
		t.Fatalf("still editing: %s", got)
	}
	// No list was read: do not click again.
	if got := submitOutcome(submitFacts{EditorKnown: true, Editor: true}); got != submitUnknown {
		t.Fatalf("unlisted unknown: %s", got)
	}
	if got := submitOutcome(submitFacts{}); got != submitUnknown {
		t.Fatalf("empty: %s", got)
	}
}

func TestImageSrcRemote(t *testing.T) {
	if !imageSrcRemote("https://pic.example/a.png") || !imageSrcRemote("http://pic.example/a.jpg") {
		t.Fatal("remote")
	}
	for _, src := range []string{"", "data:image/png;base64,aaa", "blob:https://editor.example/1", "/static/placeholder.png"} {
		if imageSrcRemote(src) {
			t.Fatalf("%q counted as remote", src)
		}
	}
}
