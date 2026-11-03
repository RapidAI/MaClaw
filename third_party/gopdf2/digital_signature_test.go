package gopdf

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"testing"
	"time"
)

// generateTestCert creates a self-signed certificate and key for testing.
func generateTestCert(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Test Signer",
			Organization: []string{"Test Org"},
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert, key
}

func TestSignPDF_Basic(t *testing.T) {
	ensureOutDir(t)
	cert, key := generateTestCert(t)

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont(fontFamily, resFontPath); err != nil {
		t.Skipf("font not available: %v", err)
	}
	if err := pdf.SetFont(fontFamily, "", 14); err != nil {
		t.Fatalf("SetFont: %v", err)
	}
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Digitally Signed Document")

	var buf bytes.Buffer
	err := pdf.SignPDF(SignatureConfig{
		Certificate: cert,
		PrivateKey:  key,
		Reason:      "Test Approval",
		Location:    "Test Lab",
		Name:        "Test Signer",
	}, &buf)
	if err != nil {
		t.Fatalf("SignPDF: %v", err)
	}

	// Verify the output is valid PDF
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatal("output does not start with %PDF-")
	}

	// Write to file for manual inspection
	outPath := resOutDir + "/signed_basic.pdf"
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	t.Logf("Signed PDF written to %s (%d bytes)", outPath, buf.Len())
}

func TestSignPDF_Visible(t *testing.T) {
	ensureOutDir(t)
	cert, key := generateTestCert(t)

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont(fontFamily, resFontPath); err != nil {
		t.Skipf("font not available: %v", err)
	}
	pdf.SetFont(fontFamily, "", 14)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Document with Visible Signature")

	var buf bytes.Buffer
	err := pdf.SignPDF(SignatureConfig{
		Certificate: cert,
		PrivateKey:  key,
		Reason:      "Approved",
		Location:    "Beijing",
		Visible:     true,
		X:           50,
		Y:           700,
		W:           200,
		H:           50,
		PageNo:      1,
	}, &buf)
	if err != nil {
		t.Fatalf("SignPDF visible: %v", err)
	}

	outPath := resOutDir + "/signed_visible.pdf"
	os.WriteFile(outPath, buf.Bytes(), 0644)
	t.Logf("Visible signed PDF written to %s", outPath)
}

func TestSignPDF_VerifyRoundTrip(t *testing.T) {
	cert, key := generateTestCert(t)

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont(fontFamily, resFontPath); err != nil {
		t.Skipf("font not available: %v", err)
	}
	pdf.SetFont(fontFamily, "", 14)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Verify round-trip test")

	var buf bytes.Buffer
	err := pdf.SignPDF(SignatureConfig{
		Certificate: cert,
		PrivateKey:  key,
		Reason:      "Round Trip",
	}, &buf)
	if err != nil {
		t.Fatalf("SignPDF: %v", err)
	}

	// Verify the signature
	results, err := VerifySignature(buf.Bytes())
	if err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no signatures found")
	}
	if !results[0].Valid {
		t.Fatalf("signature not valid: %v", results[0].Error)
	}
	if results[0].Reason != "Round Trip" {
		t.Errorf("reason = %q, want %q", results[0].Reason, "Round Trip")
	}
	t.Logf("Signature verified: signer=%s, valid=%v", results[0].SignerName, results[0].Valid)
}

func TestSignPDF_MissingCertificate(t *testing.T) {
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	var buf bytes.Buffer
	err := pdf.SignPDF(SignatureConfig{}, &buf)
	if err == nil {
		t.Fatal("expected error for missing certificate")
	}
}

func TestSignPDF_MissingKey(t *testing.T) {
	cert, _ := generateTestCert(t)

	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	pdf.AddPage()

	var buf bytes.Buffer
	err := pdf.SignPDF(SignatureConfig{
		Certificate: cert,
	}, &buf)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestAddSignatureField(t *testing.T) {
	ensureOutDir(t)
	pdf := &GoPdf{}
	pdf.Start(Config{PageSize: *PageSizeA4})
	if err := pdf.AddTTFFont(fontFamily, resFontPath); err != nil {
		t.Skipf("font not available: %v", err)
	}
	pdf.SetFont(fontFamily, "", 14)
	pdf.AddPage()
	pdf.SetXY(50, 50)
	pdf.Cell(nil, "Document with signature field placeholder")

	if err := pdf.AddSignatureField("sig1", 50, 700, 200, 50); err != nil {
		t.Fatalf("AddSignatureField: %v", err)
	}

	outPath := resOutDir + "/signature_field.pdf"
	if err := pdf.WritePdf(outPath); err != nil {
		t.Fatalf("WritePdf: %v", err)
	}
	t.Logf("PDF with signature field written to %s", outPath)
}

func TestParseCertificatePEM(t *testing.T) {
	cert, key := generateTestCert(t)

	// Encode cert to PEM
	pemData := encodeCertToPEM(cert)
	parsed, err := ParseCertificatePEM(pemData)
	if err != nil {
		t.Fatalf("ParseCertificatePEM: %v", err)
	}
	if parsed.Subject.CommonName != "Test Signer" {
		t.Errorf("CN = %q, want %q", parsed.Subject.CommonName, "Test Signer")
	}

	// Encode key to PEM
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := encodeToPEM("EC PRIVATE KEY", keyDER)
	parsedKey, err := ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		t.Fatalf("ParsePrivateKeyPEM: %v", err)
	}
	if _, ok := parsedKey.(*ecdsa.PrivateKey); !ok {
		t.Fatalf("expected *ecdsa.PrivateKey, got %T", parsedKey)
	}
}

func encodeCertToPEM(cert *x509.Certificate) []byte {
	return encodeToPEM("CERTIFICATE", cert.Raw)
}

func encodeToPEM(blockType string, data []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("-----BEGIN " + blockType + "-----\n")
	b64 := encodeBase64(data)
	for len(b64) > 64 {
		buf.WriteString(b64[:64] + "\n")
		b64 = b64[64:]
	}
	if len(b64) > 0 {
		buf.WriteString(b64 + "\n")
	}
	buf.WriteString("-----END " + blockType + "-----\n")
	return buf.Bytes()
}

func encodeBase64(data []byte) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var buf bytes.Buffer
	for i := 0; i < len(data); i += 3 {
		var b [3]byte
		n := copy(b[:], data[i:])
		buf.WriteByte(enc[b[0]>>2])
		buf.WriteByte(enc[((b[0]&0x03)<<4)|(b[1]>>4)])
		if n > 1 {
			buf.WriteByte(enc[((b[1]&0x0f)<<2)|(b[2]>>6)])
		} else {
			buf.WriteByte('=')
		}
		if n > 2 {
			buf.WriteByte(enc[b[2]&0x3f])
		} else {
			buf.WriteByte('=')
		}
	}
	return buf.String()
}
