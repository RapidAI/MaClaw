package tool

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// ErrNotRegularFile means the partial-write target exists and is not a file.
var ErrNotRegularFile = errors.New("partial write target is not a regular file")

// PartialWriteExisting is how a truncated body relates to bytes already on disk.
type PartialWriteExisting int

const (
	// PartialWriteUnrelated means the file is a different body. Leave it alone.
	PartialWriteUnrelated PartialWriteExisting = iota
	// PartialWriteExtendsExisting means the file is a prefix of the new body.
	PartialWriteExtendsExisting
	// PartialWriteAlreadyCovered means the new body is a prefix of the file.
	PartialWriteAlreadyCovered
)

// ClassifyPartialWriteExisting reports whether a truncated body is the same
// write as the file. A longer body may replace the file only when the file is
// its prefix. A shorter or equal body that the file already starts with must
// not shrink it. Any other bytes are a different file.
func ClassifyPartialWriteExisting(existing, incoming []byte) PartialWriteExisting {
	switch {
	case len(incoming) > len(existing) && bytes.HasPrefix(incoming, existing):
		return PartialWriteExtendsExisting
	case bytes.HasPrefix(existing, incoming):
		return PartialWriteAlreadyCovered
	default:
		return PartialWriteUnrelated
	}
}

// ReadExistingPrefix reads the bytes of path needed to compare it with an
// incoming body of incomingLen bytes. size is the file's full length. exists
// is false when the path is absent. A directory returns ErrNotRegularFile.
func ReadExistingPrefix(path string, incomingLen int) (data []byte, size int64, exists bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, false, nil
		}
		return nil, 0, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, info.Size(), true, ErrNotRegularFile
	}
	n := incomingLen
	if info.Size() < int64(n) {
		n = int(info.Size())
	}
	if n == 0 {
		return []byte{}, info.Size(), true, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, info.Size(), true, err
	}
	return buf, info.Size(), true, nil
}
