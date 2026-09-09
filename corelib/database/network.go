package database

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// lookupIP is the DNS resolver used by the host allowlist. Tests replace it.
var lookupIP = net.LookupIP

func authorizeProfileEndpoint(p Profile) error {
	switch p.Type {
	case SourceMySQL, SourcePostgres, SourceSQLServer:
	default:
		return nil
	}
	if profileUsesSSHTunnel(p) {
		if strings.TrimSpace(p.Host) == "" {
			return fmt.Errorf("syntax: host is required")
		}
		return nil
	}
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return fmt.Errorf("syntax: host is required")
	}
	if i := strings.IndexAny(host, `\:`); i >= 0 {
		// SQL Server instance ("host\\instance") or host:port accidentally
		// stored in Host. Strip the suffix before DNS.
		if strings.ContainsRune(host, '\\') {
			host = strings.Split(host, `\`)[0]
		} else if _, _, err := net.SplitHostPort(host); err == nil {
			host, _, _ = net.SplitHostPort(host)
		}
	}
	ips, err := resolveProfileIPs(host)
	if err != nil {
		return fmt.Errorf("connection: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("connection: host %s did not resolve", host)
	}
	hasPublic, hasPrivate := false, false
	for _, ip := range ips {
		if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("permission: host resolved to a non-unicast address")
		}
		if isPrivateOrLoopback(ip) {
			hasPrivate = true
			continue
		}
		hasPublic = true
		if !p.AllowExternalHost {
			return fmt.Errorf("permission: public address requires allow_external_host")
		}
	}
	if hasPublic && hasPrivate {
		return fmt.Errorf("permission: DNS rebinding mixed public and private addresses")
	}
	return nil
}

func resolveProfileIPs(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	return lookupIP(host)
}

func isPrivateOrLoopback(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	return false
}

func sqlDialAddr(p Profile) (string, string, error) {
	if profileUsesSSHTunnel(p) {
		return tunnelDialAddr(p)
	}
	return pinnedDialAddr(p.Host, p.Port)
}

func tunnelDialAddr(p Profile) (string, string, error) {
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return "", "", fmt.Errorf("host is required")
	}
	if i := strings.IndexRune(host, '\\'); i >= 0 {
		host = host[:i]
	}
	if h, portStr, err := net.SplitHostPort(host); err == nil {
		host = h
		if p.Port <= 0 {
			if port, convErr := strconv.Atoi(portStr); convErr == nil {
				p.Port = port
			}
		}
	}
	port := p.Port
	if port <= 0 {
		switch p.Type {
		case SourceMySQL:
			port = 3306
		case SourcePostgres:
			port = 5432
		case SourceSQLServer:
			port = 1433
		default:
			return "", "", fmt.Errorf("port is required")
		}
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), host, nil
}

func pinnedDialAddr(host string, port int) (string, string, error) {
	name := strings.TrimSpace(host)
	if i := strings.IndexRune(name, '\\'); i >= 0 {
		name = name[:i]
	}
	ips, err := resolveProfileIPs(name)
	if err != nil {
		return "", "", err
	}
	if len(ips) == 0 {
		return "", "", fmt.Errorf("host %s did not resolve", name)
	}
	ip := ips[0].String()
	if port > 0 {
		return net.JoinHostPort(ip, fmt.Sprintf("%d", port)), name, nil
	}
	return ip, name, nil
}
