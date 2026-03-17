package main

import (
	"encoding/binary"
	"testing"
)

func TestEncodeDNSName(t *testing.T) {
	got := encodeDNSName("_mcp._tcp.local.")
	// Expected: \x04_mcp\x04_tcp\x05local\x00
	expected := []byte{4, '_', 'm', 'c', 'p', 4, '_', 't', 'c', 'p', 5, 'l', 'o', 'c', 'a', 'l', 0}
	if len(got) != len(expected) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(expected))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("byte %d: got 0x%02x, want 0x%02x", i, got[i], expected[i])
		}
	}
}

func TestBuildMDNSQuery(t *testing.T) {
	pkt := buildMDNSQuery("_mcp._tcp.local.")
	if len(pkt) < 12 {
		t.Fatal("packet too short")
	}
	// QDCOUNT should be 1.
	qdcount := binary.BigEndian.Uint16(pkt[4:6])
	if qdcount != 1 {
		t.Fatalf("QDCOUNT: got %d, want 1", qdcount)
	}
	// QTYPE at end should be PTR (12).
	qtypeOff := len(pkt) - 4
	qtype := binary.BigEndian.Uint16(pkt[qtypeOff : qtypeOff+2])
	if qtype != 12 {
		t.Fatalf("QTYPE: got %d, want 12 (PTR)", qtype)
	}
}

func TestDecodeDNSName(t *testing.T) {
	// Encode then decode round-trip.
	wire := encodeDNSName("_mcp._tcp.local.")
	name, off := decodeDNSName(wire, 0)
	if name != "_mcp._tcp.local" {
		t.Fatalf("got %q, want %q", name, "_mcp._tcp.local")
	}
	if off != len(wire) {
		t.Fatalf("offset: got %d, want %d", off, len(wire))
	}
}

func TestDecodeDNSNameCompression(t *testing.T) {
	// Build a packet with a name at offset 0, then a compression pointer.
	name := encodeDNSName("test.local.")
	// Append a pointer to offset 0.
	ptr := []byte{0xC0, 0x00}
	pkt := append(name, ptr...)

	decoded, off := decodeDNSName(pkt, len(name))
	if decoded != "test.local" {
		t.Fatalf("got %q, want %q", decoded, "test.local")
	}
	if off != len(name)+2 {
		t.Fatalf("offset: got %d, want %d", off, len(name)+2)
	}
}

func TestParseTXTRdata(t *testing.T) {
	// Build TXT rdata: two strings "endpoint=http://10.0.0.1:8080" and "version=1"
	s1 := "endpoint=http://10.0.0.1:8080"
	s2 := "version=1"
	var data []byte
	data = append(data, byte(len(s1)))
	data = append(data, []byte(s1)...)
	data = append(data, byte(len(s2)))
	data = append(data, []byte(s2)...)

	kv := parseTXTRdata(data)
	if kv["endpoint"] != "http://10.0.0.1:8080" {
		t.Fatalf("endpoint: got %q", kv["endpoint"])
	}
	if kv["version"] != "1" {
		t.Fatalf("version: got %q", kv["version"])
	}
}

func TestParseMDNSResponse_SRVAndA(t *testing.T) {
	// Build a synthetic mDNS response with an SRV and A record.
	pkt := buildTestMDNSResponse()
	entries := parseMDNSResponse(pkt)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Name != "my-mcp-server" {
		t.Fatalf("name: got %q, want %q", e.Name, "my-mcp-server")
	}
	if e.Port != 9090 {
		t.Fatalf("port: got %d, want 9090", e.Port)
	}
	if e.Host != "192.168.1.100" {
		t.Fatalf("host: got %q, want %q", e.Host, "192.168.1.100")
	}
	if e.EndpointURL != "http://192.168.1.100:9090" {
		t.Fatalf("endpoint: got %q", e.EndpointURL)
	}
}

// buildTestMDNSResponse constructs a minimal mDNS response packet containing
// one SRV record and one A record for testing.
func buildTestMDNSResponse() []byte {
	var pkt []byte

	// --- Header ---
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], 0)    // ID
	binary.BigEndian.PutUint16(header[2:4], 0x8400) // Flags: response, authoritative
	binary.BigEndian.PutUint16(header[4:6], 0)     // QDCOUNT
	binary.BigEndian.PutUint16(header[6:8], 2)     // ANCOUNT (SRV + A)
	binary.BigEndian.PutUint16(header[8:10], 0)    // NSCOUNT
	binary.BigEndian.PutUint16(header[10:12], 0)   // ARCOUNT
	pkt = append(pkt, header...)

	// --- SRV Record ---
	// Name: my-mcp-server._mcp._tcp.local
	srvName := encodeDNSName("my-mcp-server._mcp._tcp.local.")
	pkt = append(pkt, srvName...)
	// TYPE=SRV(33), CLASS=IN(1), TTL=120, RDLENGTH=placeholder
	srvMeta := make([]byte, 10)
	binary.BigEndian.PutUint16(srvMeta[0:2], 33)  // TYPE SRV
	binary.BigEndian.PutUint16(srvMeta[2:4], 1)   // CLASS IN
	binary.BigEndian.PutUint32(srvMeta[4:8], 120)  // TTL
	// RDLENGTH will be filled after building RDATA.
	targetName := encodeDNSName("mcp-host.local.")
	rdLength := 6 + len(targetName) // priority(2) + weight(2) + port(2) + target
	binary.BigEndian.PutUint16(srvMeta[8:10], uint16(rdLength))
	pkt = append(pkt, srvMeta...)
	// RDATA: priority=0, weight=0, port=9090, target
	srvRdata := make([]byte, 6)
	binary.BigEndian.PutUint16(srvRdata[0:2], 0)    // priority
	binary.BigEndian.PutUint16(srvRdata[2:4], 0)    // weight
	binary.BigEndian.PutUint16(srvRdata[4:6], 9090)  // port
	pkt = append(pkt, srvRdata...)
	pkt = append(pkt, targetName...)

	// --- A Record ---
	// Name: mcp-host.local
	aName := encodeDNSName("mcp-host.local.")
	pkt = append(pkt, aName...)
	aMeta := make([]byte, 10)
	binary.BigEndian.PutUint16(aMeta[0:2], 1)   // TYPE A
	binary.BigEndian.PutUint16(aMeta[2:4], 1)   // CLASS IN
	binary.BigEndian.PutUint32(aMeta[4:8], 120)  // TTL
	binary.BigEndian.PutUint16(aMeta[8:10], 4)   // RDLENGTH
	pkt = append(pkt, aMeta...)
	pkt = append(pkt, 192, 168, 1, 100) // 192.168.1.100

	return pkt
}

func TestNewMDNSScanner(t *testing.T) {
	scanner := NewMDNSScanner(nil)
	if scanner == nil {
		t.Fatal("expected non-nil scanner")
	}
	if scanner.running {
		t.Fatal("scanner should not be running initially")
	}
}

func TestMDNSScanner_StartStop(t *testing.T) {
	scanner := NewMDNSScanner(nil)

	// Start should succeed.
	if err := scanner.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Double start should fail.
	if err := scanner.Start(); err == nil {
		t.Fatal("expected error on double Start")
	}

	// Stop should succeed.
	scanner.Stop()

	// Double stop should be a no-op.
	scanner.Stop()
}
