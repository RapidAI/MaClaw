package flash

import (
	"encoding/binary"
	"testing"
)

func TestParseESPAppDescription(t *testing.T) {
	raw := make([]byte, 4096)
	start := 0x20
	binary.LittleEndian.PutUint32(raw[start:start+4], espAppDescriptionMagic)
	copy(raw[start+16:start+48], "V7.0.0")
	copy(raw[start+48:start+80], "maclaw_esp32s3_client")
	for i := start + 144; i < start+176; i++ {
		raw[i] = byte(i - (start + 144))
	}
	desc, err := ParseESPAppDescription(raw)
	if err != nil {
		t.Fatal(err)
	}
	if desc.ProjectName != "maclaw_esp32s3_client" || desc.Version != "V7.0.0" || desc.ELFSHA256 != "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f" {
		t.Fatalf("description=%+v", desc)
	}
}

func TestParseESPAppDescriptionRejectsInvalidFields(t *testing.T) {
	raw := make([]byte, 4096)
	binary.LittleEndian.PutUint32(raw[0:4], espAppDescriptionMagic)
	raw[16] = 1
	if _, err := ParseESPAppDescription(raw); err == nil {
		t.Fatal("invalid descriptor was accepted")
	}
}

func TestParseESPAppDescriptionAcceptsUTF8ProjectName(t *testing.T) {
	raw := make([]byte, 4096)
	start := 0x20
	binary.LittleEndian.PutUint32(raw[start:start+4], espAppDescriptionMagic)
	copy(raw[start+16:start+48], "V7.0.0")
	copy(raw[start+48:start+80], "固件客户端")
	desc, err := ParseESPAppDescription(raw)
	if err != nil || desc.ProjectName != "固件客户端" {
		t.Fatalf("description=%+v err=%v", desc, err)
	}
}

func TestValidateCurrentAppDescription(t *testing.T) {
	desc := ESPAppDescription{ProjectName: "client"}
	if err := ValidateCurrentAppDescription(desc, "client"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCurrentAppDescription(desc, "other"); err == nil {
		t.Fatal("cross-project update was accepted")
	}
}
