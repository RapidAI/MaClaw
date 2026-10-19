package remote

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

type countingWriteCloser struct {
	closes int
}

func (c *countingWriteCloser) Write(p []byte) (int, error) { return len(p), nil }

func (c *countingWriteCloser) Close() error {
	c.closes++
	return nil
}

func TestSFTPSessionWriterReleasesTheSessionOnce(t *testing.T) {
	var released int
	pipe := &countingWriteCloser{}
	writer := &sftpSessionWriter{WriteCloser: pipe, release: releaseOnce(func() { released++ })}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if released != 1 || pipe.closes != 2 {
		t.Fatalf("session releases=%d pipe closes=%d", released, pipe.closes)
	}
}

func TestNoCopyableDownloadRejectsASkippedDirectory(t *testing.T) {
	if err := noCopyableDownload(0, 0); err != nil {
		t.Fatal("an empty directory is a successful copy")
	}
	if err := noCopyableDownload(2, 1); err != nil {
		t.Fatal("copied files stay successful when another entry is skipped")
	}
	err := noCopyableDownload(0, 3)
	if !errors.Is(err, errNoCopyableDownload) {
		t.Fatal("skipping every entry must not report a successful download")
	}
	got := noCopyableDownloadError(err, "/data/out", []string{"/data/out/results"}).Error()
	if !strings.Contains(got, "/data/out/results") {
		t.Fatalf("failure must name the skipped entry: %s", got)
	}
}

func TestDiscardCreatedDownloadRootRemovesOnlyAnEmptyNewDirectory(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "docs")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	discardCreatedDownloadRoot(dest, true, 0, errors.New("copy failed"))
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("a failed copy must remove the directory it created")
	}

	kept := filepath.Join(root, "kept")
	if err := os.Mkdir(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kept, "a.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	discardCreatedDownloadRoot(kept, true, 1, errors.New("later file failed"))
	if _, err := os.Stat(filepath.Join(kept, "a.pdf")); err != nil {
		t.Fatal("files already copied must stay")
	}
	discardCreatedDownloadRoot(kept, false, 0, errors.New("copy failed"))
	if _, err := os.Stat(kept); err != nil {
		t.Fatal("a directory that already existed must stay")
	}
	discardCreatedDownloadRoot(kept, true, 0, nil)
	if _, err := os.Stat(kept); err != nil {
		t.Fatal("a successful copy must keep the directory")
	}
}

func TestSameIncompleteDownloadKeepsTheOriginalDirectory(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "docs")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MarkIncompleteDownload(dest, "/home/gpu84/out", "/data/home/gpu84/out"); err != nil {
		t.Fatal(err)
	}
	if !SameIncompleteDownload(dest, "/home/gpu84/out") || !SameIncompleteDownload(dest, "/data/home/gpu84/out/") || SameIncompleteDownload(dest, "/data/other") {
		t.Fatal("the mark must match the requested path and the resolved directory, not a different remote path")
	}
	discardCreatedDownloadRoot(dest, true, 0, errors.New("copy failed"))
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("failed copy must remove the directory")
	}
	if SameIncompleteDownload(dest, "/data/out") {
		t.Fatal("failed copy must remove the unfinished-copy mark")
	}
	kept := filepath.Join(root, "partial")
	if err := os.Mkdir(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MarkIncompleteDownload(kept, "/data/out"); err != nil {
		t.Fatal(err)
	}
	discardCreatedDownloadRoot(kept, true, 2, errors.New("later file failed"))
	if !SameIncompleteDownload(kept, "/data/out") {
		t.Fatal("a partial copy must keep the unfinished-copy mark")
	}
}

func TestCanRemoveCreatedDownloadRootLeavesVolumeRoot(t *testing.T) {
	if canRemoveCreatedDownloadRoot("") || canRemoveCreatedDownloadRoot(".") || canRemoveCreatedDownloadRoot(string(filepath.Separator)) {
		t.Fatal("a filesystem root must not be removed")
	}
	if vol := filepath.VolumeName(`C:\Windows`); vol != "" && canRemoveCreatedDownloadRoot(vol+`\`) {
		t.Fatal("a volume root must not be removed")
	}
	if !canRemoveCreatedDownloadRoot(filepath.Join(t.TempDir(), "docs")) {
		t.Fatal("a nested directory can be removed")
	}
}

func TestOpenSFTPClientRequiresConnection(t *testing.T) {
	client, err := openSFTPClient(nil)
	if err == nil || client != nil {
		t.Fatal("nil ssh client must not open an sftp session")
	}
}

func TestStableSessionDownloadDestDoesNotNestOnRetry(t *testing.T) {
	retarget := func(_, local string) string {
		return filepath.Join(local, "out")
	}
	dest, again := stableSessionDownloadDest("", `F:\docs`, retarget)
	if again == nil || dest != `F:\docs` {
		t.Fatalf("first attempt dest=%q retarget nil=%v", dest, again == nil)
	}
	// The failed attempt has already created F:\docs. Choosing again would
	// put the files in F:\docs\out.
	dest, again = stableSessionDownloadDest(dest, `F:\docs`, retarget)
	if again != nil || dest != `F:\docs` {
		t.Fatalf("retry dest=%q retargeted=%v", dest, again != nil)
	}
}

func TestRemoteSSHLinkLostIgnoresMissingFiles(t *testing.T) {
	missing := fmt.Errorf("stat remote /tmp/eof.txt: %w", os.ErrNotExist)
	if remoteSSHLinkLost(missing) {
		t.Fatal("a missing file must not be treated as a dropped connection")
	}
	if remoteSSHLinkLost(&sftp.StatusError{Code: 2}) {
		t.Fatal("an sftp status must not be treated as a dropped connection")
	}
	if remoteSSHLinkLost(nil) {
		t.Fatal("nil error is not a dropped connection")
	}
	lost := []error{
		io.EOF,
		fmt.Errorf("sftp client: %w", io.EOF),
		errors.New("read tcp 10.0.0.1:22: use of closed network connection"),
		errors.New("wsarecv: An existing connection was forcibly closed by the remote host"),
		fmt.Errorf("download /data/connection reset/a.pdf: unexpected EOF"),
	}
	for _, err := range lost {
		if !remoteSSHLinkLost(err) {
			t.Fatalf("expected a lost link: %v", err)
		}
	}
}

func TestCopyWithProgressCopiesAllBytesAndReportsChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 300*1024)
	var dst bytes.Buffer
	var progressed int64
	n, err := copyWithProgress(&dst, bytes.NewReader(payload), func(delta int64) { progressed += delta })
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(payload)) || progressed != n || !bytes.Equal(dst.Bytes(), payload) {
		t.Fatalf("copy n=%d progress=%d bytes=%d, want %d", n, progressed, dst.Len(), len(payload))
	}
}

type writerToReader struct {
	r    io.Reader
	used bool
}

func (w *writerToReader) Read(p []byte) (int, error) { return w.r.Read(p) }

func (w *writerToReader) WriteTo(dst io.Writer) (int64, error) {
	w.used = true
	return io.Copy(dst, struct{ io.Reader }{w.r})
}

func TestCopyRemoteFileUsesWriterToWithoutProgress(t *testing.T) {
	payload := []byte("gpu84-report")
	src := &writerToReader{r: bytes.NewReader(payload)}
	var dst bytes.Buffer
	n, err := copyRemoteFile(&dst, src, nil)
	if err != nil || n != int64(len(payload)) || !src.used || !bytes.Equal(dst.Bytes(), payload) {
		t.Fatalf("pipelined copy n=%d used=%v err=%v body=%q", n, src.used, err, dst.Bytes())
	}
	src.used = false
	src.r = bytes.NewReader(payload)
	dst.Reset()
	var progressed int64
	n, err = copyRemoteFile(&dst, src, func(delta int64) { progressed += delta })
	if err != nil || src.used || n != int64(len(payload)) || progressed != n || !bytes.Equal(dst.Bytes(), payload) {
		t.Fatalf("progress copy n=%d progress=%d used=%v err=%v", n, progressed, src.used, err)
	}
}

func TestLimitedReaderAllowsExactLimitAndDetectsOverflow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		payload  int
		limit    int64
		wantRead int64
	}{
		{name: "exact limit", payload: 64, limit: 64, wantRead: 64},
		{name: "overflow", payload: 65, limit: 64, wantRead: 65},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := io.LimitReader(bytes.NewReader(bytes.Repeat([]byte("x"), tc.payload)), tc.limit+1)
			n, err := io.Copy(io.Discard, reader)
			if err != nil || n != tc.wantRead {
				t.Fatalf("read n=%d err=%v, want %d", n, err, tc.wantRead)
			}
			if overflow := n > tc.limit; overflow != (tc.payload > int(tc.limit)) {
				t.Fatalf("overflow=%v for payload=%d limit=%d", overflow, tc.payload, tc.limit)
			}
		})
	}
}
