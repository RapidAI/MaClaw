package partition

import (
	"encoding/binary"
	"testing"
)

func testTable() []byte {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = 0xff
	}
	put := func(at int, typ, sub byte, offset, size uint32, label string) {
		binary.LittleEndian.PutUint16(b[at:], Magic)
		b[at+2] = typ
		b[at+3] = sub
		binary.LittleEndian.PutUint32(b[at+4:], offset)
		binary.LittleEndian.PutUint32(b[at+8:], size)
		copy(b[at+12:], label)
	}
	put(0, 1, 2, 0x9000, 0x6000, "nvs")
	put(32, 0, 0, 0x10000, 0x3a0000, "factory")
	return b
}
func TestParseTableAndFingerprint(t *testing.T) {
	table, err := Parse(testTable(), 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Entries) != 2 || table.Fingerprint == "" {
		t.Fatalf("bad table: %#v", table)
	}
}
func TestParseRejectsOverlap(t *testing.T) {
	b := testTable()
	binary.LittleEndian.PutUint32(b[36:40], 0x9000)
	if _, err := Parse(b, 16*1024*1024); err == nil {
		t.Fatal("expected overlap")
	}
}

func TestEncodeRoundTripPreservesLabels(t *testing.T) {
	raw, err := Encode([]Entry{{Label: "factory", Type: 0, Subtype: 0, Offset: 0x10000, Size: 0x3a0000}})
	if err != nil {
		t.Fatal(err)
	}
	table, err := Parse(raw, 16*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	entry, found := Find(table.Entries, "factory")
	if !found || entry.Offset != 0x10000 || entry.Size != 0x3a0000 {
		t.Fatalf("round-trip entry = %#v, found=%v", entry, found)
	}
}
