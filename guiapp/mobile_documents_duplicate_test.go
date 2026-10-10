package guiapp

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestImportMobileDocumentBytesSkipsCloudOriginalWhenProbeMissing(t *testing.T) {
	var uploads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/upload/duplicate":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/drafts":
			_, _ = io.WriteString(w, `{"drafts":[{"id":"d1","title":"book","has_original":true,"source_filename":"book.pdf","source_size":5,"source_download_url":"/api/mobile/documents/drafts/d1/source"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/drafts/d1/source":
			_, _ = io.WriteString(w, "hello")
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/upload":
			uploads++
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"draft":{"id":"new","title":"book"}}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	got, err := newMobileDocumentsTestApp(server.URL).ImportMobileDocumentBytes("book.pdf", base64.StdEncoding.EncodeToString([]byte("hello")))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if uploads != 0 || got == nil || !got.Duplicate || got.ID != "d1" || got.DuplicateOfTitle != "book" {
		t.Fatalf("uploads=%d got=%#v", uploads, got)
	}
}

func TestImportMobileDocumentBytesUploadsWhenCloudBytesDiffer(t *testing.T) {
	var uploads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/upload/duplicate":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/drafts":
			_, _ = io.WriteString(w, `{"drafts":[{"id":"d1","title":"book","has_original":true,"source_filename":"book.pdf","source_size":5,"source_download_url":"/api/mobile/documents/drafts/d1/source"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/drafts/d1/source":
			_, _ = io.WriteString(w, "world")
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/upload":
			uploads++
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"draft":{"id":"new","title":"book"}}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	got, err := newMobileDocumentsTestApp(server.URL).ImportMobileDocumentBytes("book.pdf", base64.StdEncoding.EncodeToString([]byte("hello")))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if uploads != 1 || got == nil || got.Duplicate || got.ID != "new" {
		t.Fatalf("uploads=%d got=%#v", uploads, got)
	}
}

func TestImportMobileDocumentBytesTrustsNegativeContentProbe(t *testing.T) {
	var uploads int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/upload/duplicate":
			_, _ = io.WriteString(w, `{"duplicate":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/upload":
			uploads++
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"draft":{"id":"new","title":"book"}}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	got, err := newMobileDocumentsTestApp(server.URL).ImportMobileDocumentBytes("book.pdf", base64.StdEncoding.EncodeToString([]byte("hello")))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if uploads != 1 || got == nil || got.Duplicate || got.ID != "new" {
		t.Fatalf("uploads=%d got=%#v", uploads, got)
	}
}

func TestImportMobileDocumentFromPathSkipsRenamedCloudOriginal(t *testing.T) {
	var uploads int
	dir := t.TempDir()
	path := filepath.Join(dir, "renamed.pdf")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/upload/duplicate":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/drafts":
			_, _ = io.WriteString(w, `{"drafts":[{"id":"d1","title":"book","has_original":true,"source_filename":"book.pdf","source_storage_size":5,"source_download_url":"/api/mobile/documents/drafts/d1/source"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/mobile/documents/drafts/d1/source":
			_, _ = io.WriteString(w, "hello")
		case r.Method == http.MethodPost && r.URL.Path == "/api/mobile/documents/upload":
			uploads++
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"draft":{"id":"new"}}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	got, err := newMobileDocumentsTestApp(server.URL).ImportMobileDocumentFromPath(path)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if uploads != 0 || got == nil || !got.Duplicate || got.ID != "d1" || got.DuplicateOfFilename != "book.pdf" {
		t.Fatalf("uploads=%d got=%#v", uploads, got)
	}
}
