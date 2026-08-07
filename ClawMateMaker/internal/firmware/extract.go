package firmware

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExtractVerifiedImages copies only files named by a package that has already
// passed signature and manifest validation. Both ZIP entry and destination
// paths remain checked so the later write path never trusts archive metadata.
func ExtractVerifiedImages(archive, destination string, verified Verified) ([]FileSpec, error) {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if err := os.MkdirAll(destination, 0700); err != nil {
		return nil, err
	}
	entries := make(map[string]*zip.File, len(reader.File))
	for _, entry := range reader.File {
		entries[entry.Name] = entry
	}
	for _, spec := range verified.Manifest.Files {
		entry := entries[spec.Path]
		if entry == nil {
			return nil, fmt.Errorf("verified entry disappeared: %s", spec.Path)
		}
		clean, err := safePath(spec.Path)
		if err != nil {
			return nil, err
		}
		output := filepath.Join(destination, filepath.FromSlash(clean))
		relative, err := filepath.Rel(destination, output)
		if err != nil || relative == ".." || len(relative) >= 3 && relative[:3] == ".."+string(os.PathSeparator) {
			return nil, fmt.Errorf("unsafe output path: %s", clean)
		}
		if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
			return nil, err
		}
		input, err := entry.Open()
		if err != nil {
			return nil, err
		}
		outputFile, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			_ = input.Close()
			return nil, err
		}
		written, copyErr := io.Copy(outputFile, io.LimitReader(input, spec.Size+1))
		closeOutput := outputFile.Close()
		closeInput := input.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeOutput != nil {
			return nil, closeOutput
		}
		if closeInput != nil {
			return nil, closeInput
		}
		if written != spec.Size {
			return nil, fmt.Errorf("extracted size mismatch: %s", spec.Path)
		}
		if digest, err := fileHash(output); err != nil || !hashEqual("sha256:"+digest, spec.SHA256) {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("extracted digest mismatch: %s", spec.Path)
		}
	}
	return verified.Manifest.Files, nil
}
