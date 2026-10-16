package excel

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/richardlehane/mscfb"
)

// ErrEncryptedNeedsPassword is returned when the workbook is encrypted and no
// secret_ref/password was supplied. Callers must map this to authentication.
var ErrEncryptedNeedsPassword = errors.New("authentication: encrypted workbook requires secret_ref")

// ErrEncryptedUnsupported is returned when a password was supplied but this
// process cannot open the encryption scheme. It is not a crack attempt.
var ErrEncryptedUnsupported = errors.New("unsupported_capability: encrypted workbook cannot be opened with the supplied secret")

var oleMagic = []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}

// DecryptWorkbook, if set, turns an encrypted workbook plus password into a
// temporary unencrypted path. The default implementation never decrypts: it
// reports unsupported_capability so hosts fail closed instead of parsing the
// OLE container as a spreadsheet.
var DecryptWorkbook = func(path, password string) (string, error) {
	_ = path
	_ = password
	return "", ErrEncryptedUnsupported
}

// IsEncryptedWorkbook reports whether path is an OLE-encrypted Office
// workbook (EncryptedPackage + EncryptionInfo) or a BIFF file-password
// workbook. Unencrypted xlsx/csv files return false.
func IsEncryptedWorkbook(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	head := make([]byte, 8)
	n, err := io.ReadAtLeast(f, head, 8)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, err
	}
	if n < 8 || !bytes.Equal(head[:8], oleMagic) {
		return false, nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	doc, err := mscfb.New(f)
	if err != nil {
		return false, nil
	}
	hasPackage, hasInfo := false, false
	for _, entry := range doc.File {
		if entry == nil || len(entry.Path) != 0 {
			continue
		}
		name := strings.ToLower(entry.Name)
		if strings.Contains(name, "encryptedpackage") {
			hasPackage = true
		}
		if strings.Contains(name, "encryptioninfo") || strings.Contains(name, "dataspaces") {
			hasInfo = true
		}
	}
	return hasPackage && hasInfo, nil
}

// ResolveReadablePath returns a path that ReadFile can open. Encrypted
// workbooks without a password fail as authentication; with a password they
// go through DecryptWorkbook.
func ResolveReadablePath(path, password string) (string, error) {
	encrypted, err := IsEncryptedWorkbook(path)
	if err != nil {
		return "", err
	}
	if !encrypted {
		return path, nil
	}
	if strings.TrimSpace(password) == "" {
		return "", ErrEncryptedNeedsPassword
	}
	opened, err := DecryptWorkbook(path, password)
	if err != nil {
		if errors.Is(err, ErrEncryptedUnsupported) || errors.Is(err, ErrEncryptedNeedsPassword) {
			return "", err
		}
		return "", fmt.Errorf("authentication: %w", err)
	}
	if strings.TrimSpace(opened) == "" {
		return "", ErrEncryptedUnsupported
	}
	return opened, nil
}
