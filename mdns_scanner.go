package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// MDNSServiceEntry holds information about a discovered mDNS service.
type MDNSServiceEntry struct {
	Name        string
	Host        string
	Port        int
	EndpointURL string
	TxtRecords  map[string]string
}

// MDNSScanner discovers MCP Servers on the local network via mDNS/DNS-SD.
type MDNSScanner struct {
	registry *MCPRegistry
	stopCh   chan struct{}
	mu       sync.Mutex
	running  bool
}

// NewMDNSScanner creates a new mDNS scanner bound to the given registry.
func NewMDNSScanner(registry *MCPRegistry) *MDNSScanner {
	return &MDNSScanner{
		registry: registry,
	}
}

const (
	mdnsAddr       = "224.0.0.251:5353"
	mdnsPort       = 5353
	mcpServiceName = "_mcp._tcp.local."
	scanInterval   = 30 * time.Second
	listenTimeout  = 5 * time.Second
)

// Start begins periodic mDNS scanning in a background goroutine.
func (s *MDNSScanner) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("mDNS scanner already running")
	}

	s.stopCh = make(chan struct{})
	s.running = true

	go s.loop()
	log.Println("[MDNSScanner] started")
	return nil
}

// Stop terminates the mDNS scanning goroutine.
func (s *MDNSScanner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}
	close(s.stopCh)
	s.running = false
	log.Println("[MDNSScanner] stopped")
}

// loop runs the periodic scan cycle.
func (s *MDNSScanner) loop() {
	// Run an initial scan immediately on start.
	s.scan()

	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.scan()
		}
	}
}

// scan sends an mDNS query for _mcp._tcp.local. and processes responses.
func (s *MDNSScanner) scan() {
	entries, err := s.queryMDNS()
	if err != nil {
		log.Printf("[MDNSScanner] scan error: %v", err)
		return
	}

	for _, entry := range entries {
		serverEntry := MCPServerEntry{
			ID:          fmt.Sprintf("mdns-%s-%d", entry.Host, entry.Port),
			Name:        entry.Name,
			EndpointURL: entry.EndpointURL,
			AuthType:    "none",
		}
		if err := s.registry.RegisterAutoDiscovered(serverEntry, MCPSourceMDNS); err != nil {
			log.Printf("[MDNSScanner] failed to register %s: %v", entry.Name, err)
		}
	}
}

// queryMDNS sends an mDNS PTR query for _mcp._tcp.local. over multicast UDP
// and collects responses within the listen timeout window.
func (s *MDNSScanner) queryMDNS() ([]MDNSServiceEntry, error) {
	addr, err := net.ResolveUDPAddr("udp4", mdnsAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve mDNS addr: %w", err)
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("listen UDP: %w", err)
	}
	defer conn.Close()

	query := buildMDNSQuery(mcpServiceName)
	if _, err := conn.WriteToUDP(query, addr); err != nil {
		return nil, fmt.Errorf("send mDNS query: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(listenTimeout))

	var entries []MDNSServiceEntry
	seen := make(map[string]bool)
	buf := make([]byte, 4096)

	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Timeout or other read error — done collecting.
			break
		}
		if n < 12 {
			continue
		}

		parsed := parseMDNSResponse(buf[:n])
		for _, entry := range parsed {
			key := fmt.Sprintf("%s:%d", entry.Host, entry.Port)
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, entry)
		}
	}

	if len(entries) > 0 {
		log.Printf("[MDNSScanner] discovered %d MCP service(s)", len(entries))
	}
	return entries, nil
}

// ---------------------------------------------------------------------------
// Minimal DNS wire-format helpers (standard library only)
// ---------------------------------------------------------------------------

// buildMDNSQuery constructs a minimal DNS query packet for a PTR record.
func buildMDNSQuery(service string) []byte {
	// DNS header: ID=0, Flags=0 (standard query), QDCOUNT=1
	header := []byte{
		0x00, 0x00, // ID
		0x00, 0x00, // Flags
		0x00, 0x01, // QDCOUNT
		0x00, 0x00, // ANCOUNT
		0x00, 0x00, // NSCOUNT
		0x00, 0x00, // ARCOUNT
	}

	// Encode the QNAME.
	qname := encodeDNSName(service)

	// QTYPE=PTR (12), QCLASS=IN (1) with unicast-response bit clear.
	suffix := []byte{0x00, 0x0C, 0x00, 0x01}

	pkt := make([]byte, 0, len(header)+len(qname)+len(suffix))
	pkt = append(pkt, header...)
	pkt = append(pkt, qname...)
	pkt = append(pkt, suffix...)
	return pkt
}

// encodeDNSName encodes a dotted DNS name into wire format.
// e.g. "_mcp._tcp.local." → \x04_mcp\x04_tcp\x05local\x00
func encodeDNSName(name string) []byte {
	name = strings.TrimSuffix(name, ".")
	parts := strings.Split(name, ".")
	var buf []byte
	for _, p := range parts {
		buf = append(buf, byte(len(p)))
		buf = append(buf, []byte(p)...)
	}
	buf = append(buf, 0x00)
	return buf
}

// parseMDNSResponse extracts MCP service entries from an mDNS response packet.
// It looks for SRV, A, and TXT records to build service entries.
func parseMDNSResponse(pkt []byte) []MDNSServiceEntry {
	if len(pkt) < 12 {
		return nil
	}

	// Parse header counts.
	qdcount := int(binary.BigEndian.Uint16(pkt[4:6]))
	ancount := int(binary.BigEndian.Uint16(pkt[6:8]))
	nscount := int(binary.BigEndian.Uint16(pkt[8:10]))
	arcount := int(binary.BigEndian.Uint16(pkt[10:12]))

	offset := 12

	// Skip question section.
	for i := 0; i < qdcount && offset < len(pkt); i++ {
		_, newOff := decodeDNSName(pkt, offset)
		offset = newOff + 4 // QTYPE + QCLASS
		if offset > len(pkt) {
			return nil
		}
	}

	totalRR := ancount + nscount + arcount

	type srvInfo struct {
		instanceName string
		host         string
		port         int
	}

	var srvRecords []srvInfo
	aRecords := make(map[string]string)    // hostname → IP
	txtRecords := make(map[string]map[string]string) // instanceName → kv

	for i := 0; i < totalRR && offset < len(pkt); i++ {
		rrName, newOff := decodeDNSName(pkt, offset)
		offset = newOff
		if offset+10 > len(pkt) {
			break
		}

		rrType := binary.BigEndian.Uint16(pkt[offset : offset+2])
		// rrClass at offset+2
		// ttl at offset+4
		rdLength := int(binary.BigEndian.Uint16(pkt[offset+8 : offset+10]))
		offset += 10

		if offset+rdLength > len(pkt) {
			break
		}

		rdataStart := offset

		switch rrType {
		case 33: // SRV
			if rdLength >= 6 {
				// Priority(2) + Weight(2) + Port(2) + Target
				port := int(binary.BigEndian.Uint16(pkt[rdataStart+4 : rdataStart+6]))
				target, _ := decodeDNSName(pkt, rdataStart+6)
				srvRecords = append(srvRecords, srvInfo{
					instanceName: rrName,
					host:         target,
					port:         port,
				})
			}
		case 1: // A
			if rdLength == 4 {
				ip := net.IPv4(pkt[rdataStart], pkt[rdataStart+1], pkt[rdataStart+2], pkt[rdataStart+3])
				aRecords[rrName] = ip.String()
			}
		case 16: // TXT
			kv := parseTXTRdata(pkt[rdataStart : rdataStart+rdLength])
			if len(kv) > 0 {
				txtRecords[rrName] = kv
			}
		}

		offset = rdataStart + rdLength
	}

	// Build service entries from SRV records.
	var entries []MDNSServiceEntry
	for _, srv := range srvRecords {
		ip := aRecords[srv.host]
		if ip == "" {
			// Try without trailing dot.
			ip = aRecords[strings.TrimSuffix(srv.host, ".")]
		}
		if ip == "" {
			ip = strings.TrimSuffix(srv.host, ".")
		}

		name := srv.instanceName
		// Strip the service suffix to get a friendly name.
		if idx := strings.Index(name, "._mcp._tcp"); idx > 0 {
			name = name[:idx]
		}

		endpointURL := fmt.Sprintf("http://%s:%d", ip, srv.port)

		// Check TXT records for an explicit endpoint URL.
		txt := txtRecords[srv.instanceName]
		if txt == nil {
			txt = make(map[string]string)
		}
		if u, ok := txt["endpoint"]; ok && u != "" {
			endpointURL = u
		}

		entries = append(entries, MDNSServiceEntry{
			Name:        name,
			Host:        ip,
			Port:        srv.port,
			EndpointURL: endpointURL,
			TxtRecords:  txt,
		})
	}

	return entries
}

// decodeDNSName decodes a DNS name from wire format, handling compression pointers.
// Returns the decoded name and the offset immediately after the name in the packet.
func decodeDNSName(pkt []byte, offset int) (string, int) {
	var parts []string
	visited := make(map[int]bool) // guard against pointer loops
	jumped := false
	returnOffset := offset

	for offset < len(pkt) {
		if visited[offset] {
			break // pointer loop
		}
		visited[offset] = true

		length := int(pkt[offset])
		if length == 0 {
			if !jumped {
				returnOffset = offset + 1
			}
			break
		}

		// Check for DNS compression pointer (top 2 bits set).
		if length&0xC0 == 0xC0 {
			if offset+1 >= len(pkt) {
				break
			}
			ptr := int(binary.BigEndian.Uint16(pkt[offset:offset+2])) & 0x3FFF
			if !jumped {
				returnOffset = offset + 2
			}
			jumped = true
			offset = ptr
			continue
		}

		offset++
		if offset+length > len(pkt) {
			break
		}
		parts = append(parts, string(pkt[offset:offset+length]))
		offset += length
	}

	return strings.Join(parts, "."), returnOffset
}

// parseTXTRdata parses DNS TXT record RDATA into key=value pairs.
func parseTXTRdata(data []byte) map[string]string {
	kv := make(map[string]string)
	offset := 0
	for offset < len(data) {
		length := int(data[offset])
		offset++
		if offset+length > len(data) {
			break
		}
		txt := string(data[offset : offset+length])
		offset += length

		if idx := strings.IndexByte(txt, '='); idx >= 0 {
			kv[txt[:idx]] = txt[idx+1:]
		}
	}
	return kv
}
