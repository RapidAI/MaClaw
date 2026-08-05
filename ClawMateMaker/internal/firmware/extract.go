package firmware

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExtractVerifiedImages extracts only files declared by a verified archive into
// an application-owned temporary directory. It never trusts archive paths.
func ExtractVerifiedImages(archive, destination string, verified Verified) ([]FileSpec, error) {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if err := os.MkdirAll(destination, 0700); err != nil {
		return nil, err
	}
	entries := make(map[string]*zip.File, len(r.File))
	for _, f := range r.File {
		entries[f.Name] = f
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
		rel, err := filepath.Rel(destination, output)
		if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(os.PathSeparator) {
			return nil, fmt.Errorf("unsafe output path: %s", clean)
		}
		if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
			return nil, err
		}
		in, err := entry.Open()
		if err != nil {
			return nil, err
		}
		out, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			in.Close()
			return nil, err
		}
		n, copyErr := io.Copy(out, io.LimitReader(in, spec.Size+1))
		closeOut := out.Close()
		closeIn := in.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeOut != nil {
			return nil, closeOut
		}
		if closeIn != nil {
			return nil, closeIn
		}
		if n != spec.Size {
			return nil, fmt.Errorf("extracted size mismatch: %s", spec.Path)
		}
		if sum, err := fileHash(output); err != nil || !hashEqual("sha256:"+sum, spec.SHA256) {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("extracted digest mismatch: %s", spec.Path)
		}
	}
	return verified.Manifest.Files, nil
}
