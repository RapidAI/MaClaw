package archiveutil

import (
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Detect identifies a format from its signature first, with filename suffixes
// used only for composite streams such as .tar.gz and .tar.bz2.
func Detect(archivePath string) (Format, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return FormatUnknown, errorf(CodeIO, "open archive: %v", err)
	}
	defer f.Close()
	// ZIP central-directory records (including ZIP64 records) are normally at
	// the end of the file, while a self-extracting or sparse-offset ZIP need
	// not start with a local-file record. Keep the cheap signature check first,
	// then use archive/zip's own directory parser as a bounded fallback.
	info, statErr := f.Stat()
	if statErr != nil {
		return FormatUnknown, errorf(CodeIO, "stat archive: %v", statErr)
	}
	head := make([]byte, 600)
	n, readErr := io.ReadFull(f, head)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return FormatUnknown, errorf(CodeIO, "read archive header: %v", readErr)
	}
	head = head[:n]
	ext := strings.ToLower(filepath.Base(archivePath))
	if len(head) >= 4 && bytes.Equal(head[:4], []byte("PK\x03\x04")) || len(head) >= 4 && bytes.Equal(head[:4], []byte("PK\x05\x06")) || len(head) >= 4 && bytes.Equal(head[:4], []byte("PK\x07\x08")) {
		return FormatZIP, nil
	}
	if info.Size() >= 22 {
		if zr, zipErr := zip.OpenReader(archivePath); zipErr == nil {
			_ = zr.Close()
			return FormatZIP, nil
		}
	}
	if len(head) >= 6 && bytes.Equal(head[:6], []byte("\x37\x7a\xbc\xaf\x27\x1c")) {
		return Format7Z, nil
	}
	if len(head) >= 6 && bytes.Equal(head[:6], []byte("\xfd7zXZ\x00")) {
		return FormatXZ, nil
	}
	if len(head) >= 4 && bytes.Equal(head[:4], []byte("\x28\xb5\x2f\xfd")) {
		return FormatZSTD, nil
	}
	if len(head) >= 7 && bytes.Equal(head[:7], []byte("Rar!\x1a\x07\x00")) || len(head) >= 8 && bytes.Equal(head[:8], []byte("Rar!\x1a\x07\x01\x00")) {
		if isMultiVolumeArchiveName(ext) {
			return FormatRAR, errorf(CodeMultiVolumeUnsupported, "multi-volume RAR archives are not supported")
		}
		return FormatRAR, nil
	}
	if len(head) >= 3 && bytes.Equal(head[:3], []byte("\x1f\x8b\x08")) {
		if compressedStreamIsTar(archivePath, true) {
			return FormatTarGZIP, nil
		}
		return FormatGZIP, nil
	}
	if len(head) >= 3 && bytes.Equal(head[:3], []byte("BZh")) {
		if compressedStreamIsTar(archivePath, false) {
			return FormatTarBZ2, nil
		}
		return FormatBZIP2, nil
	}
	if len(head) >= 265 && bytes.Equal(head[257:262], []byte("ustar")) {
		return FormatTAR, nil
	}
	if strings.HasSuffix(ext, ".tar") {
		return FormatTAR, nil
	}
	return FormatUnknown, errorf(CodeFormatUnrecognized, "unrecognized archive format")
}

func isMultiVolumeArchiveName(name string) bool {
	name = strings.ToLower(filepath.Base(name))
	if strings.HasSuffix(name, ".part1.rar") || strings.HasSuffix(name, ".part01.rar") {
		return true
	}
	if len(name) >= 4 {
		suffix := name[len(name)-4:]
		if suffix[0] == '.' && suffix[1] == 'r' && suffix[2] >= '0' && suffix[2] <= '9' && suffix[3] >= '0' && suffix[3] <= '9' {
			return true
		}
	}
	return false
}

// compressedStreamIsTar looks through the outer standard-library compression
// stream before choosing a TAR adapter.  A suffix alone is not trustworthy:
// a plain gzip called "payload.tar.gz" must still extract as one file.
func compressedStreamIsTar(archivePath string, gzipStream bool) bool {
	f, err := os.Open(archivePath)
	if err != nil {
		return false
	}
	defer f.Close()
	var reader io.Reader = f
	if gzipStream {
		gz, openErr := gzip.NewReader(f)
		if openErr != nil {
			return false
		}
		defer gz.Close()
		reader = gz
	} else {
		reader = bzip2.NewReader(f)
	}
	header := make([]byte, 512)
	n, err := io.ReadFull(reader, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false
	}
	return n >= 262 && bytes.Equal(header[257:262], []byte("ustar"))
}

func defaultDestination(archivePath string) string {
	base := filepath.Base(archivePath)
	lower := strings.ToLower(base)
	for _, suffix := range []string{".tar.gz", ".tar.bz2", ".tgz", ".tbz2", ".tbz", ".zip", ".tar", ".gz", ".bz2", ".rar", ".7z", ".xz", ".zst"} {
		if strings.HasSuffix(lower, suffix) {
			return filepath.Join(filepath.Dir(archivePath), base[:len(base)-len(suffix)])
		}
	}
	return filepath.Join(filepath.Dir(archivePath), base+".out")
}

// DefaultDestination derives the output directory for extract requests that
// omit destination. It is also available to approved external adapters.
func DefaultDestination(archivePath string) string { return defaultDestination(archivePath) }

func externalFallback(format Format) Result {
	programs := map[Format][]string{
		Format7Z: {"7z", "7zz", "7za"}, FormatXZ: {"xz", "7z"}, FormatZSTD: {"zstd", "7z"}, FormatRAR: {"7z", "unrar"},
	}
	available := make([]string, 0, len(programs[format]))
	for _, program := range programs[format] {
		if path, err := exec.LookPath(program); err == nil {
			available = append(available, path)
		}
	}
	if len(programs[format]) == 0 {
		return Result{OK: false, Format: format, Code: CodeFormatUnsupported, Message: fmt.Sprintf("no external adapter is configured for %s", format)}
	}
	code := CodeExternalFallbackRequired
	message := fmt.Sprintf("embedded archive support is unavailable for %s", format)
	if len(available) == 0 {
		code = CodeExternalToolNotFound
		message += "; no supported external program was found"
	}
	return Result{OK: false, Format: format, Code: code, Message: message, Fallback: &Fallback{
		RecommendedPrograms: programs[format],
		AvailablePrograms:   available,
		CraftToolAllowed:    true,
		UserActionRequired:  len(available) == 0,
	}}
}
