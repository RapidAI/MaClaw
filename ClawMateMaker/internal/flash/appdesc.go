package flash

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ESPAppDescription is the small immutable ESP-IDF application descriptor
// stored at the start of an app image. It is read from the currently installed
// factory partition during preflight; it is not a claim made by the firmware
// currently running on the serial console.
type ESPAppDescription struct {
	ProjectName string `json:"projectName"`
	Version     string `json:"version"`
	ELFSHA256   string `json:"elfSha256"`
}

const (
	// ESP_APP_DESC_MAGIC_WORD from ESP-IDF's esp_app_desc.h.
	espAppDescriptionMagic uint32 = 0xABCD5432
	appDescriptionBytes           = 4 + 4 + 8 + 32 + 32 + 16 + 16 + 32 + 32
)

// ParseESPAppDescription finds and validates the first ESP-IDF app descriptor
// in a bounded application-header read. The descriptor normally follows the
// image and first-segment headers at 0x20, but scanning is intentional: it
// keeps the parser compatible with a valid image whose segment arrangement
// differs while never examining more than the caller-provided bounded block.
func ParseESPAppDescription(raw []byte) (ESPAppDescription, error) {
	if len(raw) < appDescriptionBytes {
		return ESPAppDescription{}, errors.New("application header is too short")
	}
	magic := []byte{0x32, 0x54, 0xCD, 0xAB}
	for start := 0; start+appDescriptionBytes <= len(raw); start++ {
		if !bytes.Equal(raw[start:start+4], magic) || binary.LittleEndian.Uint32(raw[start:start+4]) != espAppDescriptionMagic {
			continue
		}
		version, ok := appDescriptionString(raw[start+16 : start+48])
		if !ok {
			continue
		}
		project, ok := appDescriptionString(raw[start+48 : start+80])
		if !ok {
			continue
		}
		// ESP-IDF stores app_elf_sha256 as 32 binary bytes, not a C string.
		// Preserve it as hex for diagnostics without requiring a printable
		// encoding from a perfectly valid image descriptor.
		elf := hex.EncodeToString(raw[start+144 : start+176])
		return ESPAppDescription{ProjectName: project, Version: version, ELFSHA256: elf}, nil
	}
	return ESPAppDescription{}, errors.New("ESP-IDF application descriptor is unavailable")
}

func appDescriptionString(raw []byte) (string, bool) {
	if end := bytes.IndexByte(raw, 0); end >= 0 {
		raw = raw[:end]
	}
	if len(raw) == 0 || !utf8Printable(raw) {
		return "", false
	}
	return string(raw), true
}

func utf8Printable(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	for _, r := range string(raw) {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ValidateCurrentAppDescription returns only stable, support-useful metadata.
// A complete replacement can recover an invalid old application; an app-only
// update cannot, because it would otherwise preserve arbitrary data under an
// unknown layout/project contract.
func ValidateCurrentAppDescription(desc ESPAppDescription, expectedProject string) error {
	if strings.TrimSpace(desc.ProjectName) == "" || strings.TrimSpace(expectedProject) == "" {
		return errors.New("application project identity is unavailable")
	}
	if desc.ProjectName != expectedProject {
		return fmt.Errorf("installed project %q does not match firmware project %q", desc.ProjectName, expectedProject)
	}
	return nil
}
