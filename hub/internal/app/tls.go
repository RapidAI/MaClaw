package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// certRenewThreshold is how far before expiry we regenerate the certificate.
const certRenewThreshold = 30 * 24 * time.Hour // 30 days

// EnsureSelfSignedCert checks if cert/key files exist and the certificate is
// still valid (not expired, not expiring within 30 days). If missing or
// invalid, generates a new self-signed ECDSA certificate valid for 10 years.
// The certificate includes SANs for localhost, 127.0.0.1, and all
// non-loopback IPs on the machine.
func EnsureSelfSignedCert(certFile, keyFile string) error {
	if fileExists(certFile) && fileExists(keyFile) {
		if needsRegen, reason := certNeedsRegeneration(certFile); !needsRegen {
			log.Printf("[tls] using existing cert=%s key=%s", certFile, keyFile)
			return nil
		} else {
			log.Printf("[tls] regenerating certificate: %s", reason)
		}
	}

	log.Printf("[tls] generating self-signed certificate...")

	if err := os.MkdirAll(filepath.Dir(certFile), 0700); err != nil {
		return fmt.Errorf("create tls dir: %w", err)
	}
	if dir := filepath.Dir(keyFile); dir != filepath.Dir(certFile) {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create tls key dir: %w", err)
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "MaClaw Hub"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	// Add all non-loopback IPs so clients can connect via LAN IP.
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ipNet.IP)
			}
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	// Write cert
	certOut, err := os.Create(certFile)
	if err != nil {
		return fmt.Errorf("create cert file: %w", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		certOut.Close()
		return err
	}
	certOut.Close()

	// Write key
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create key file: %w", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		keyOut.Close()
		return err
	}
	keyOut.Close()

	log.Printf("[tls] self-signed certificate generated: %s", certFile)
	for _, ip := range tmpl.IPAddresses {
		log.Printf("[tls]   SAN IP: %s", ip)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// certNeedsRegeneration parses the PEM certificate and checks validity.
// Returns (true, reason) if the cert should be regenerated.
func certNeedsRegeneration(certFile string) (bool, string) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return true, fmt.Sprintf("cannot read cert: %v", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return true, "invalid PEM format"
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true, fmt.Sprintf("cannot parse certificate: %v", err)
	}

	now := time.Now()
	if now.After(cert.NotAfter) {
		return true, fmt.Sprintf("expired on %s", cert.NotAfter.Format("2006-01-02"))
	}
	if cert.NotAfter.Sub(now) < certRenewThreshold {
		return true, fmt.Sprintf("expires soon on %s (< 30 days)", cert.NotAfter.Format("2006-01-02"))
	}

	log.Printf("[tls] certificate valid until %s", cert.NotAfter.Format("2006-01-02"))
	return false, ""
}
