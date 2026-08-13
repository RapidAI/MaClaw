package archiveutil

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

func Run(req Request) Result {
	switch req.Action {
	case ActionInspect:
		return Inspect(req)
	case ActionExtract:
		return Extract(req)
	case ActionExtractExternal:
		return ExtractExternal(req)
	case ActionCreateZIP:
		return CreateZIP(req)
	default:
		return failure(req.Action, FormatUnknown, errorf(CodeInvalidArgument, "unsupported archive action"))
	}
}

func Inspect(req Request) Result {
	if req.ArchivePath == "" {
		return failure(ActionInspect, FormatUnknown, errorf(CodeInvalidArgument, "archive_path is required"))
	}
	limits := req.Limits.normalized()
	before, err := captureSourceState(req.ArchivePath)
	if err != nil {
		return failure(ActionInspect, FormatUnknown, errorf(CodeIO, "stat archive: %v", err))
	}
	if before.size > limits.MaxInputBytes {
		return failure(ActionInspect, FormatUnknown, errorf(CodeLimitExceeded, "archive input exceeds size limit"))
	}
	format, err := Detect(req.ArchivePath)
	if err != nil {
		return failure(ActionInspect, format, err)
	}
	if !embedded(format) {
		r := externalFallback(format)
		r.Action, r.InputPath = ActionInspect, req.ArchivePath
		return r
	}
	warnings := archiveFormatNameWarnings(req.ArchivePath, format)
	r := Result{OK: true, Action: ActionInspect, Format: format, InputPath: req.ArchivePath, Warnings: warnings}
	if err := inspectEntries(req.ArchivePath, format, &r, limits); err != nil {
		return failure(ActionInspect, format, err)
	}
	if err := ensureSourceUnchanged(req.ArchivePath, before); err != nil {
		return failure(ActionInspect, format, err)
	}
	return r
}

func addInspectEntry(r *Result, name string, dir bool, size int64, limits Limits) {
	if dir {
		r.Directories++
	} else {
		r.Files++
	}
	if len(r.Entries) < limits.MaxListedEntries {
		r.Entries = append(r.Entries, Entry{Path: name, Dir: dir, Size: size})
	} else {
		r.Truncated = true
	}
}

func inspectEntries(path string, format Format, result *Result, limits Limits) error {
	if format == FormatZIP {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return errorf(CodeCorruptArchive, "open zip: %v", err)
		}
		defer zr.Close()
		for _, file := range zr.File {
			addInspectEntry(result, file.Name, file.FileInfo().IsDir(), int64(file.UncompressedSize64), limits)
		}
		return nil
	}
	if format == FormatRAR {
		rr, err := rardecode.OpenReader(path, rardecode.MaxDictionarySize(128<<20))
		if err != nil {
			if errors.Is(err, rardecode.ErrArchiveEncrypted) {
				return errorf(CodeEncryptedArchive, "encrypted RAR archives are not supported")
			}
			return errorf(CodeCorruptArchive, "open rar: %v", err)
		}
		defer rr.Close()
		for {
			header, err := rr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return errorf(CodeCorruptArchive, "read rar entry: %v", err)
			}
			size := int64(0)
			if !header.UnKnownSize {
				size = header.UnPackedSize
			}
			addInspectEntry(result, header.Name, header.IsDir, size, limits)
		}
	}
	if format == FormatGZIP || format == FormatBZIP2 {
		name := filepath.Base(path)
		if format == FormatGZIP && len(name) > 3 {
			name = name[:len(name)-3]
		}
		if format == FormatBZIP2 && len(name) > 4 {
			name = name[:len(name)-4]
		}
		addInspectEntry(result, name, false, 0, limits)
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return errorf(CodeIO, "open archive: %v", err)
	}
	defer f.Close()
	var reader io.Reader = f
	if format == FormatTarGZIP {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return errorf(CodeCorruptArchive, "open gzip: %v", err)
		}
		defer gz.Close()
		reader = gz
	}
	if format == FormatTarBZ2 {
		reader = bzip2.NewReader(f)
	}
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errorf(CodeCorruptArchive, "read tar entry: %v", err)
		}
		addInspectEntry(result, header.Name, header.FileInfo().IsDir(), header.Size, limits)
	}
}

func Extract(req Request) Result {
	if req.ArchivePath == "" {
		return failure(ActionExtract, FormatUnknown, errorf(CodeInvalidArgument, "archive_path is required"))
	}
	if err := validateConflictPolicy(req.ConflictPolicy); err != nil {
		return failure(ActionExtract, FormatUnknown, err)
	}
	limits := req.Limits.normalized()
	before, err := captureSourceState(req.ArchivePath)
	if err != nil {
		return failure(ActionExtract, FormatUnknown, errorf(CodeIO, "stat archive: %v", err))
	}
	if before.size > limits.MaxInputBytes {
		return failure(ActionExtract, FormatUnknown, errorf(CodeLimitExceeded, "archive input exceeds size limit"))
	}
	format, err := Detect(req.ArchivePath)
	if err != nil {
		return failure(ActionExtract, format, err)
	}
	if !embedded(format) {
		r := externalFallback(format)
		r.Action, r.InputPath = ActionExtract, req.ArchivePath
		return r
	}
	dest := req.Destination
	if dest == "" {
		dest = defaultDestination(req.ArchivePath)
	}
	stage, err := makeStaging(dest)
	if err != nil {
		return failure(ActionExtract, format, err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	warnings := []string(nil)
	c := &counters{limits: limits, root: stage, inputBytes: before.size, warnings: &warnings}
	if err := extractInto(req.ArchivePath, format, stage, c); err != nil {
		return failure(ActionExtract, format, err)
	}
	if err := ensureSourceUnchanged(req.ArchivePath, before); err != nil {
		return failure(ActionExtract, format, err)
	}
	publishStage := ExternalStage{Path: stage, Destination: dest}
	publishStage, _, _, _, err = publishStage.Validate(limits)
	if err != nil {
		return failure(ActionExtract, format, err)
	}
	if err := publishStage.Publish(); err != nil {
		return failure(ActionExtract, format, err)
	}
	warnings = append(warnings, archiveFormatNameWarnings(req.ArchivePath, format)...)
	return Result{OK: true, Action: ActionExtract, Format: format, InputPath: req.ArchivePath, OutputPath: dest, Files: c.files, Directories: c.dirs, WrittenBytes: c.written, Warnings: uniqueWarnings(warnings)}
}

func archiveFormatNameWarnings(archivePath string, format Format) []string {
	name := strings.ToLower(filepath.Base(archivePath))
	expected := map[Format][]string{
		FormatZIP:     {".zip", ".jar", ".apk", ".docx", ".xlsx", ".pptx"},
		FormatTAR:     {".tar"},
		FormatGZIP:    {".gz", ".gzip"},
		FormatTarGZIP: {".tar.gz", ".tgz"},
		FormatBZIP2:   {".bz2", ".bzip2"},
		FormatTarBZ2:  {".tar.bz2", ".tbz", ".tbz2"},
		FormatRAR:     {".rar"},
	}
	for _, suffix := range expected[format] {
		if strings.HasSuffix(name, suffix) {
			return nil
		}
	}
	if len(expected[format]) == 0 {
		return nil
	}
	return []string{"archive filename extension does not match detected format"}
}

func uniqueWarnings(warnings []string) []string {
	if len(warnings) < 2 {
		return warnings
	}
	seen := make(map[string]bool, len(warnings))
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning != "" && !seen[warning] {
			seen[warning] = true
			out = append(out, warning)
		}
	}
	return out
}

func embedded(f Format) bool {
	return f == FormatZIP || f == FormatTAR || f == FormatGZIP || f == FormatTarGZIP || f == FormatBZIP2 || f == FormatTarBZ2 || f == FormatRAR
}

func extractInto(path string, format Format, root string, c *counters) error {
	if format == FormatZIP {
		return extractZIP(path, root, c)
	}
	if format == FormatRAR {
		return extractRAR(path, root, c)
	}
	f, err := os.Open(path)
	if err != nil {
		return errorf(CodeIO, "open archive: %v", err)
	}
	defer f.Close()
	var reader io.Reader = f
	if format == FormatGZIP || format == FormatTarGZIP {
		gz, e := gzip.NewReader(f)
		if e != nil {
			return errorf(CodeCorruptArchive, "open gzip: %v", e)
		}
		defer gz.Close()
		reader = gz
	}
	if format == FormatBZIP2 || format == FormatTarBZ2 {
		reader = bzip2.NewReader(f)
	}
	if format == FormatTAR || format == FormatTarGZIP || format == FormatTarBZ2 {
		return extractTAR(tar.NewReader(reader), root, c)
	}
	name := filepath.Base(path)
	if format == FormatGZIP && len(name) > 3 {
		name = name[:len(name)-3]
	}
	if format == FormatBZIP2 && len(name) > 4 {
		name = name[:len(name)-4]
	}
	entry, err := canonicalEntry(name, c.limits)
	if err != nil {
		return err
	}
	include, err := c.include(Entry{Path: entry, OriginalPath: name})
	if err != nil {
		return err
	}
	if !include {
		return nil
	}
	dst, err := safeJoin(root, entry)
	if err != nil {
		return err
	}
	_, err = writeEntry(dst, reader, -1, c)
	return err
}

func extractZIP(path, root string, c *counters) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return errorf(CodeCorruptArchive, "open zip: %v", err)
	}
	defer zr.Close()
	return extractZIPFiles(zr.File, root, c)
}

func extractZIPFiles(files []*zip.File, root string, c *counters) error {
	seen := newEntrySet()
	for _, f := range files {
		entry, err := canonicalEntry(f.Name, c.limits)
		if err != nil {
			return err
		}
		if err := c.reserveEntry(); err != nil {
			return err
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "symlink entry is not supported: %s", entry)
		}
		if f.Flags&1 != 0 {
			return errorf(CodeEncryptedArchive, "encrypted ZIP archives are not supported")
		}
		isDir := f.FileInfo().IsDir()
		if isDir {
			if err := seen.add(entry, true); err != nil {
				return err
			}
			include, filterErr := c.include(Entry{Path: entry, OriginalPath: f.Name, Dir: true, Size: int64(f.UncompressedSize64)})
			if filterErr != nil {
				return filterErr
			}
			if !include {
				continue
			}
			if err := addDirectory(root, entry, c); err != nil {
				return err
			}
			continue
		}
		if !f.FileInfo().Mode().IsRegular() {
			return errorf(CodeUnsafeEntry, "special archive entry is not supported: %s", entry)
		}
		if err := seen.add(entry, false); err != nil {
			return err
		}
		include, filterErr := c.include(Entry{Path: entry, OriginalPath: f.Name, Size: int64(f.UncompressedSize64)})
		if filterErr != nil {
			return filterErr
		}
		if !include {
			continue
		}
		dst, err := safeJoin(root, entry)
		if err != nil {
			return err
		}
		in, err := f.Open()
		if err != nil {
			return errorf(CodeCorruptArchive, "open zip entry: %v", err)
		}
		_, copyErr := writeEntry(dst, in, int64(f.UncompressedSize64), c)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return errorf(CodeCorruptArchive, "close zip entry: %v", closeErr)
		}
	}
	return nil
}

func extractTAR(tr *tar.Reader, root string, c *counters) error {
	seen := newEntrySet()
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errorf(CodeCorruptArchive, "read tar entry: %v", err)
		}
		entry, err := canonicalEntry(h.Name, c.limits)
		if err != nil {
			return err
		}
		if err := c.reserveEntry(); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := seen.add(entry, true); err != nil {
				return err
			}
			include, filterErr := c.include(Entry{Path: entry, OriginalPath: h.Name, Dir: true, Size: h.Size})
			if filterErr != nil {
				return filterErr
			}
			if !include {
				continue
			}
			if err := addDirectory(root, entry, c); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := seen.add(entry, false); err != nil {
				return err
			}
			include, filterErr := c.include(Entry{Path: entry, OriginalPath: h.Name, Size: h.Size})
			if filterErr != nil {
				return filterErr
			}
			if !include {
				continue
			}
			dst, err := safeJoin(root, entry)
			if err != nil {
				return err
			}
			if _, err = writeEntry(dst, tr, h.Size, c); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := seen.add(entry, false); err != nil {
				return err
			}
			include, filterErr := c.include(Entry{Path: entry, OriginalPath: h.Name})
			if filterErr != nil {
				return filterErr
			}
			if !include {
				continue
			}
			if err := addSymlink(root, entry, h.Linkname, c); err != nil {
				return err
			}
		default:
			return errorf(CodeUnsafeEntry, "unsupported tar entry: %s", entry)
		}
	}
}

func extractRAR(path, root string, c *counters) error {
	rr, err := rardecode.OpenReader(path, rardecode.MaxDictionarySize(128<<20))
	if err != nil {
		if errors.Is(err, rardecode.ErrArchiveEncrypted) {
			return errorf(CodeEncryptedArchive, "encrypted RAR archives are not supported")
		}
		return errorf(CodeCorruptArchive, "open rar: %v", err)
	}
	defer rr.Close()
	seen := newEntrySet()
	for {
		header, err := rr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if errors.Is(err, rardecode.ErrArchiveEncrypted) || errors.Is(err, rardecode.ErrArchivedFileEncrypted) {
				return errorf(CodeEncryptedArchive, "encrypted RAR archives are not supported")
			}
			return errorf(CodeCorruptArchive, "read rar entry: %v", err)
		}
		entry, err := canonicalEntry(header.Name, c.limits)
		if err != nil {
			return err
		}
		if err := c.reserveEntry(); err != nil {
			return err
		}
		if header.Encrypted || header.HeaderEncrypted {
			return errorf(CodeEncryptedArchive, "encrypted RAR archives are not supported")
		}
		if header.Mode()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "symlink RAR entry is not supported: %s", entry)
		}
		if header.IsDir {
			size := int64(0)
			if !header.UnKnownSize {
				size = header.UnPackedSize
			}
			if err := seen.add(entry, true); err != nil {
				return err
			}
			include, filterErr := c.include(Entry{Path: entry, OriginalPath: header.Name, Dir: true, Size: size})
			if filterErr != nil {
				return filterErr
			}
			if !include {
				continue
			}
			if err := addDirectory(root, entry, c); err != nil {
				return err
			}
			continue
		}
		declared := int64(-1)
		if !header.UnKnownSize {
			declared = header.UnPackedSize
		}
		if err := seen.add(entry, false); err != nil {
			return err
		}
		include, filterErr := c.include(Entry{Path: entry, OriginalPath: header.Name, Size: declared})
		if filterErr != nil {
			return filterErr
		}
		if !include {
			continue
		}
		if !header.UnKnownSize && header.UnPackedSize > c.limits.MaxFileBytes {
			return errorf(CodeLimitExceeded, "RAR entry exceeds file size limit")
		}
		dst, err := safeJoin(root, entry)
		if err != nil {
			return err
		}
		if _, err := writeEntry(dst, rr, declared, c); err != nil {
			return err
		}
	}
}

func CreateZIP(req Request) Result {
	if len(req.SourcePaths) == 0 || req.OutputPath == "" || req.BasePath == "" {
		return failure(ActionCreateZIP, FormatZIP, errorf(CodeInvalidArgument, "source_paths, output_path and base_path are required"))
	}
	if err := validateConflictPolicy(req.ConflictPolicy); err != nil {
		return failure(ActionCreateZIP, FormatZIP, err)
	}
	if err := validateRootMode(req.RootMode); err != nil {
		return failure(ActionCreateZIP, FormatZIP, err)
	}
	if filepath.Ext(req.OutputPath) != ".zip" {
		return failure(ActionCreateZIP, FormatZIP, errorf(CodeInvalidArgument, "output_path must end in .zip"))
	}
	if _, err := os.Stat(req.OutputPath); err == nil {
		return failure(ActionCreateZIP, FormatZIP, errorf(CodeDestinationExists, "output already exists: %s", req.OutputPath))
	} else if !os.IsNotExist(err) {
		return failure(ActionCreateZIP, FormatZIP, errorf(CodeIO, "check output: %v", err))
	}
	if err := rejectSymlinkAncestors(filepath.Dir(req.OutputPath)); err != nil {
		return failure(ActionCreateZIP, FormatZIP, err)
	}
	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0o700); err != nil {
		return failure(ActionCreateZIP, FormatZIP, errorf(CodeIO, "create zip output parent: %v", err))
	}
	stage, err := os.CreateTemp(filepath.Dir(req.OutputPath), ".archive-create-*.zip")
	if err != nil {
		return failure(ActionCreateZIP, FormatZIP, errorf(CodeIO, "create zip staging: %v", err))
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath) }()
	zw := zip.NewWriter(stage)
	c := &counters{limits: req.Limits.normalized()}
	seen := newEntrySet()
	for _, source := range req.SourcePaths {
		if err := addZIPSource(zw, source, req.BasePath, stagePath, c, seen); err != nil {
			_ = zw.Close()
			_ = stage.Close()
			return failure(ActionCreateZIP, FormatZIP, err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = stage.Close()
		return failure(ActionCreateZIP, FormatZIP, errorf(CodeIO, "close zip: %v", err))
	}
	if err := stage.Close(); err != nil {
		return failure(ActionCreateZIP, FormatZIP, errorf(CodeIO, "close zip output: %v", err))
	}
	if err := publishNewFile(stagePath, req.OutputPath); err != nil {
		return failure(ActionCreateZIP, FormatZIP, err)
	}
	return Result{OK: true, Action: ActionCreateZIP, Format: FormatZIP, OutputPath: req.OutputPath, Files: c.files, Directories: c.dirs, WrittenBytes: c.written}
}

func validateConflictPolicy(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "fail" {
		return nil
	}
	return errorf(CodeInvalidArgument, "conflict_policy only supports fail in this version")
}

func validateRootMode(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "preserve" {
		return nil
	}
	return errorf(CodeInvalidArgument, "root_mode only supports preserve in this version")
}

func addZIPSource(zw *zip.Writer, source, base, excludePath string, c *counters, seen *entrySet) error {
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return errorf(CodeIO, "resolve base path: %v", err)
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return errorf(CodeIO, "resolve source path: %v", err)
	}
	rel, err := filepath.Rel(baseAbs, sourceAbs)
	if err != nil || rel == ".." || len(rel) > 3 && rel[:3] == ".."+string(os.PathSeparator) {
		return errorf(CodeInvalidArgument, "source is outside base_path: %s", source)
	}
	return filepath.WalkDir(sourceAbs, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errorf(CodeIO, "walk source: %v", walkErr)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "symlink source is not supported: %s", p)
		}
		if samePath(p, excludePath) {
			return nil
		}
		r, e := filepath.Rel(baseAbs, p)
		if e != nil {
			return e
		}
		// BasePath itself is a namespace root rather than a ZIP entry. This
		// permits callers to archive their whole working directory while still
		// keeping every emitted entry relative and non-absolute.
		if r == "." && d.IsDir() {
			return nil
		}
		entry, e := canonicalEntry(filepath.ToSlash(r), c.limits)
		if e != nil {
			return e
		}
		if err := seen.add(entry, d.IsDir()); err != nil {
			return errorf(CodeUnsafeEntry, "invalid zip source layout: %v", err)
		}
		if err := c.reserveEntry(); err != nil {
			return err
		}
		if d.IsDir() {
			h := &zip.FileHeader{Name: entry + "/", Method: zip.Store}
			h.SetMode(0o700)
			if _, e = zw.CreateHeader(h); e != nil {
				return e
			}
			c.dirs++
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errorf(CodeUnsafeEntry, "special source is not supported: %s", p)
		}
		before, e := captureSourceState(p)
		if e != nil {
			return e
		}
		if info.Size() > c.limits.MaxFileBytes || info.Size() > c.limits.MaxTotalBytes-c.written {
			return errorf(CodeLimitExceeded, "source exceeds zip size limit")
		}
		h := &zip.FileHeader{Name: entry, Method: zip.Deflate}
		h.SetMode(0o600)
		w, e := zw.CreateHeader(h)
		if e != nil {
			return e
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		n, e := io.Copy(w, io.LimitReader(in, c.limits.MaxFileBytes+1))
		closeErr := in.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if n != info.Size() {
			return errorf(CodeSourceChanged, "source changed while creating zip: %s", p)
		}
		if e := ensureSourceUnchanged(p, before); e != nil {
			return e
		}
		c.files++
		c.written += n
		return nil
	})
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && filepath.Clean(aa) == filepath.Clean(bb)
}
