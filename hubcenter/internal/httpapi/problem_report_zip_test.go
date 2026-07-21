package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
)

func TestIsSafeProblemReportZIPEntryName(t *testing.T) {
	for _, name := range []string{"logs/app.log", "trajectories/run.json", "logs/../logs/app.log", "logs\\app.log"} {
		if !isSafeProblemReportZIPEntryName(name) {
			t.Fatalf("expected safe ZIP entry %q", name)
		}
	}
	for _, name := range []string{"", "/etc/passwd", "../secret", "logs/../../secret", "logs\\..\\..\\secret", `\\server\\share`, "C:/temp/diagnostics.log"} {
		if isSafeProblemReportZIPEntryName(name) {
			t.Fatalf("expected unsafe ZIP entry %q", name)
		}
	}
}

func TestProblemReportOriginAttachmentSignature(t *testing.T) {
	h := &ProblemReportHandlers{haClusterSecret: func() string { return "cluster-test-secret" }}
	expiresAt := time.Now().UTC().Add(30 * time.Second).Unix()
	signature := h.originAttachmentSignature("BR-1", "diagnostics.zip", expiresAt)
	if signature == "" || !h.validOriginAttachmentSignature("BR-1", "diagnostics.zip", expiresAt, signature) {
		t.Fatal("expected a valid signed origin attachment link")
	}
	if h.validOriginAttachmentSignature("BR-1", "other.zip", expiresAt, signature) {
		t.Fatal("signature must be bound to the requested attachment")
	}
	if h.validOriginAttachmentSignature("BR-1", "diagnostics.zip", time.Now().UTC().Add(-time.Second).Unix(), signature) {
		t.Fatal("expired signature must be rejected")
	}
	clockSkewExpiry := time.Now().UTC().Add(problemReportOriginLinkTTL + 15*time.Second).Unix()
	clockSkewSignature := h.originAttachmentSignature("BR-1", "diagnostics.zip", clockSkewExpiry)
	if !h.validOriginAttachmentSignature("BR-1", "diagnostics.zip", clockSkewExpiry, clockSkewSignature) {
		t.Fatal("small HA clock skew must be accepted")
	}
}

func TestNormalizeProblemReportOriginURL(t *testing.T) {
	if got := normalizeProblemReportOriginURL(" https://origin.example.com/center/ "); got != "https://origin.example.com/center" {
		t.Fatalf("unexpected normalized origin: %q", got)
	}
	for _, raw := range []string{"", "ftp://origin.example.com", "https://user:pass@origin.example.com", "https://origin.example.com?next=x", "https://origin.example.com#fragment"} {
		if got := normalizeProblemReportOriginURL(raw); got != "" {
			t.Fatalf("unsafe origin accepted %q as %q", raw, got)
		}
	}
}

func TestProblemReportOriginRecognizesLocalPublicURL(t *testing.T) {
	h := &ProblemReportHandlers{
		publicBaseURL: func(context.Context) (string, error) {
			return "https://hubs.maclaw.top", nil
		},
	}
	report := &skillmarket.ProblemReport{OriginURL: "https://hubs.maclaw.top/"}
	if h.isRemoteOrigin(report) {
		t.Fatal("report stored on this HubCenter must not be treated as remote")
	}
}

func TestProblemReportOriginRecognizesRemotePublicURL(t *testing.T) {
	h := &ProblemReportHandlers{
		publicBaseURL: func(context.Context) (string, error) {
			return "https://hubs.maclaw.top", nil
		},
	}
	report := &skillmarket.ProblemReport{OriginURL: "https://hubs2.maclaw.top"}
	if !h.isRemoteOrigin(report) {
		t.Fatal("report from another HubCenter must be treated as remote")
	}
}

func TestProblemReportStorageRootRejectsUnsafeIdentifiers(t *testing.T) {
	if root, ok := problemReportStorageRoot(t.TempDir(), "BR-20260721-ABC123"); !ok || filepath.Base(root) != "BR-20260721-ABC123" {
		t.Fatalf("valid report ID rejected: %q, %v", root, ok)
	}
	for _, id := range []string{"", ".", "..", "../outside", `..\\outside`, "BR/child", "BR:child", "BR\u0000child"} {
		if root, ok := problemReportStorageRoot(t.TempDir(), id); ok || root != "" {
			t.Fatalf("unsafe report ID accepted %q as %q", id, root)
		}
	}
}

func TestCopyUploadedFileDoesNotOverwriteExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.zip")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyUploadedFile(path, bytes.NewBufferString("replacement")); err == nil {
		t.Fatal("existing attachment must not be overwritten")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original" {
		t.Fatalf("existing attachment changed: %q, %v", got, err)
	}
}

func TestStageProblemReportStorageCanRestore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "BR-test")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "diagnostics.zip"), []byte("diagnostics"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := stageProblemReportStorage(root)
	if err != nil || staged == "" {
		t.Fatalf("stage failed: %q, %v", staged, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("live root must be absent after stage: %v", err)
	}
	restoreStagedProblemReportStorage(staged, root)
	if got, err := os.ReadFile(filepath.Join(root, "diagnostics.zip")); err != nil || string(got) != "diagnostics" {
		t.Fatalf("staged storage was not restored: %q, %v", got, err)
	}
}

func TestValidateProblemReportZIPRejectsSymbolicLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(out)
	header := &zip.FileHeader{Name: "logs/latest.log"}
	header.SetMode(os.ModeSymlink | 0o777)
	writer, err := archive.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("../sensitive-file")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateProblemReportZIP(path); err == nil {
		t.Fatal("symbolic link entry must be rejected")
	}
}

func TestValidateProblemReportZIPRejectsNonRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(out)
	header := &zip.FileHeader{Name: "logs/device"}
	header.SetMode(os.ModeDevice | 0o600)
	if _, err := archive.CreateHeader(header); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateProblemReportZIP(path); err == nil {
		t.Fatal("non-regular ZIP entry must be rejected")
	}
}

func TestValidateProblemReportZIPRejectsDuplicateNormalizedPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(out)
	for _, name := range []string{"logs/latest.log", "logs/./latest.log"} {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("log")); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateProblemReportZIP(path); err == nil {
		t.Fatal("duplicate normalized ZIP paths must be rejected")
	}
}

func TestValidateProblemReportZIPRejectsCaseOnlyAndFileDirectoryConflicts(t *testing.T) {
	for _, names := range [][]string{
		{"logs/App.log", "logs/app.log"},
		{"logs", "logs/app.log"},
	} {
		path := filepath.Join(t.TempDir(), "diagnostics.zip")
		out, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		archive := zip.NewWriter(out)
		for _, name := range names {
			writer, err := archive.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := writer.Write([]byte("log")); err != nil {
				t.Fatal(err)
			}
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := out.Close(); err != nil {
			t.Fatal(err)
		}
		if err := validateProblemReportZIP(path); err == nil {
			t.Fatalf("conflicting ZIP paths %v must be rejected", names)
		}
	}
}

func TestProblemReportScreenshotNamesRemainContiguousAfterInvalidFiles(t *testing.T) {
	root := t.TempDir()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	invalid, err := form.CreateFormFile("screenshots", "invalid.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalid.Write([]byte("not an image")); err != nil {
		t.Fatal(err)
	}
	valid, err := form.CreateFormFile("screenshots", "valid.png")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Black)
	if err := png.Encode(valid, img); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := multipart.NewReader(&body, form.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	defer req.RemoveAll()
	screenshots := []string{}
	for _, fh := range req.File["screenshots"] {
		format, err := validatedProblemScreenshotFormat(fh)
		if err != nil {
			continue
		}
		in, err := fh.Open()
		if err != nil {
			t.Fatal(err)
		}
		name := "screenshot-" + "01" + "." + format
		if len(screenshots) > 0 {
			name = "screenshot-02." + format
		}
		if err := copyUploadedFile(filepath.Join(root, name), in); err != nil {
			in.Close()
			t.Fatal(err)
		}
		in.Close()
		screenshots = append(screenshots, name)
	}
	if len(screenshots) != 1 || screenshots[0] != "screenshot-01.png" {
		t.Fatalf("unexpected screenshot names: %v", screenshots)
	}
}
