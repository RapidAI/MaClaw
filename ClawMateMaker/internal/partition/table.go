// Package partition parses ESP-IDF partition tables and derives the canonical
// layout fingerprint used by profiles and firmware manifests.
package partition

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

const EntrySize = 32
const Magic uint16 = 0x50AA

type Entry struct {
	Type    uint8  `json:"type"`
	Subtype uint8  `json:"subtype"`
	Offset  uint32 `json:"offset"`
	Size    uint32 `json:"size"`
	Label   string `json:"label"`
	Flags   uint32 `json:"flags"`
}
type Table struct {
	Entries     []Entry `json:"entries"`
	Fingerprint string  `json:"fingerprint"`
	MD5Present  bool    `json:"md5Present"`
}

func Parse(raw []byte, flashBytes uint64) (Table, error) {
	if len(raw) < EntrySize {
		return Table{}, errors.New("partition table is too short")
	}
	var entries []Entry
	md5Present := false
	foundEnd := false
	for pos := 0; pos+EntrySize <= len(raw); pos += EntrySize {
		r := raw[pos : pos+EntrySize]
		magic := binary.LittleEndian.Uint16(r[:2])
		if magic == 0xFFFF && bytes.Equal(r, bytes.Repeat([]byte{0xff}, EntrySize)) {
			foundEnd = true
			break
		}
		if magic == 0xEBEB {
			if md5Present {
				return Table{}, errors.New("duplicate partition MD5 record")
			}
			md5Present = true
			continue
		}
		if magic != Magic {
			return Table{}, fmt.Errorf("invalid partition magic at %#x", pos)
		}
		labelBytes := r[12:28]
		if i := bytes.IndexByte(labelBytes, 0); i >= 0 {
			labelBytes = labelBytes[:i]
		}
		if bytes.IndexByte(labelBytes, 0) >= 0 {
			return Table{}, errors.New("invalid label")
		}
		e := Entry{Type: r[2], Subtype: r[3], Offset: binary.LittleEndian.Uint32(r[4:8]), Size: binary.LittleEndian.Uint32(r[8:12]), Label: string(labelBytes), Flags: binary.LittleEndian.Uint32(r[28:32])}
		if e.Size == 0 || uint64(e.Offset)+uint64(e.Size) > flashBytes {
			return Table{}, fmt.Errorf("partition %s exceeds flash", e.Label)
		}
		if e.Offset%0x1000 != 0 || e.Size%0x1000 != 0 {
			return Table{}, fmt.Errorf("partition %s is not 4 KiB aligned", e.Label)
		}
		entries = append(entries, e)
	}
	if !foundEnd {
		return Table{}, errors.New("partition table has no terminator")
	}
	if len(entries) == 0 {
		return Table{}, errors.New("partition table is empty")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Offset < entries[j].Offset })
	for i := 1; i < len(entries); i++ {
		if uint64(entries[i-1].Offset)+uint64(entries[i-1].Size) > uint64(entries[i].Offset) {
			return Table{}, fmt.Errorf("partitions %s and %s overlap", entries[i-1].Label, entries[i].Label)
		}
	}
	h := sha256.New()
	for _, e := range entries {
		h.Write([]byte{e.Type, e.Subtype})
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], e.Offset)
		h.Write(b[:])
		binary.LittleEndian.PutUint32(b[:], e.Size)
		h.Write(b[:])
		h.Write([]byte(e.Label))
		h.Write([]byte{0})
		binary.LittleEndian.PutUint32(b[:], e.Flags)
		h.Write(b[:])
	}
	return Table{Entries: entries, Fingerprint: "sha256:" + hex.EncodeToString(h.Sum(nil)), MD5Present: md5Present}, nil
}

func Find(entries []Entry, label string) (Entry, bool) {
	for _, e := range entries {
		if e.Label == label {
			return e, true
		}
	}
	return Entry{}, false
}

// Encode creates the 4 KiB ESP-IDF partition-table binary consumed by the ROM
// flasher and parsed above. It is used by deterministic CI package tests;
// production packages use ESP-IDF's generated table.
func Encode(entries []Entry) ([]byte, error) {
	if len(entries) == 0 || len(entries) > 95 {
		return nil, errors.New("invalid partition entry count")
	}
	raw := bytes.Repeat([]byte{0xff}, 4096)
	for i, e := range entries {
		if e.Label == "" || len(e.Label) > 15 || e.Size == 0 {
			return nil, fmt.Errorf("invalid entry %d", i)
		}
		off := i * EntrySize
		binary.LittleEndian.PutUint16(raw[off:], Magic)
		raw[off+2], raw[off+3] = e.Type, e.Subtype
		binary.LittleEndian.PutUint32(raw[off+4:], e.Offset)
		binary.LittleEndian.PutUint32(raw[off+8:], e.Size)
		// ESP-IDF labels are NUL-terminated within a fixed-width field. The
		// surrounding table starts out as 0xFF, so clear this field explicitly
		// before copying the label; otherwise Parse would treat padding as part
		// of the label and app-only package generation could not find factory.
		for position := off + 12; position < off+28; position++ {
			raw[position] = 0
		}
		copy(raw[off+12:off+28], []byte(e.Label))
		binary.LittleEndian.PutUint32(raw[off+28:], e.Flags)
	}
	return raw, nil
}
