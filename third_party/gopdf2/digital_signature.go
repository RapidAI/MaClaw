package gopdf

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"time"

	"go.mozilla.org/pkcs7"
)

// SignatureConfig holds the configuration for digitally signing a PDF.
type SignatureConfig struct {
	// Certificate is the X.509 signing certificate.
	Certificate *x509.Certificate
	// CertificateChain is the optional intermediate certificate chain.
	CertificateChain []*x509.Certificate
	// PrivateKey is the signer's private key (must be *rsa.PrivateKey or *ecdsa.PrivateKey).
	PrivateKey crypto.Signer

	// Reason is the reason for signing (e.g. "Approved").
	Reason string
	// Location is the signing location (e.g. "Beijing").
	Location string
	// ContactInfo is the signer's contact information.
	ContactInfo string
	// Name is the signer's name. If empty, the certificate's CN is used.
	Name string

	// SignTime is the signing time. Defaults to time.Now().
	SignTime time.Time

	// SignatureFieldName is the name of the signature form field.
	// If empty, defaults to "Signature1".
	SignatureFieldName string

	// Visible controls whether the signature has a visible appearance.
	Visible bool
	// X, Y, W, H define the visible signature rectangle (only used when Visible is true).
	X, Y, W, H float64
	// PageNo is the 1-based page number for the visible signature. Default: 1.
	PageNo int
}

func (cfg *SignatureConfig) defaults() {
	if cfg.SignTime.IsZero() {
		cfg.SignTime = time.Now()
	}
	if cfg.SignatureFieldName == "" {
		cfg.SignatureFieldName = "Signature1"
	}
	if cfg.Name == "" && cfg.Certificate != nil {
		cfg.Name = cfg.Certificate.Subject.CommonName
	}
	if cfg.PageNo <= 0 {
		cfg.PageNo = 1
	}
}

// LoadCertificateFromPEM loads an X.509 certificate from a PEM file.
func LoadCertificateFromPEM(certPath string) (*x509.Certificate, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read certificate file: %w", err)
	}
	return ParseCertificatePEM(data)
}

// ParseCertificatePEM parses an X.509 certificate from PEM-encoded data.
func ParseCertificatePEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in certificate data")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	return cert, nil
}

// LoadPrivateKeyFromPEM loads a private key (RSA or ECDSA) from a PEM file.
func LoadPrivateKeyFromPEM(keyPath string) (crypto.Signer, error) {
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	return ParsePrivateKeyPEM(data)
}

// ParsePrivateKeyPEM parses a private key from PEM-encoded data.
// Supports RSA, ECDSA, and PKCS#8 encoded keys.
func ParsePrivateKeyPEM(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in key data")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKCS8 key: %w", err)
		}
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
		return nil, fmt.Errorf("PKCS8 key does not implement crypto.Signer")
	default:
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
}

// LoadCertificateChainFromPEM loads a chain of certificates from a PEM file.
func LoadCertificateChainFromPEM(chainPath string) ([]*x509.Certificate, error) {
	data, err := os.ReadFile(chainPath)
	if err != nil {
		return nil, fmt.Errorf("read chain file: %w", err)
	}
	return ParseCertificateChainPEM(data)
}

// ParseCertificateChainPEM parses a chain of certificates from PEM-encoded data.
func ParseCertificateChainPEM(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate in chain: %w", err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates found in chain data")
	}
	return certs, nil
}

// signatureByteRangeSize is the reserved space for the ByteRange value.
// Format: [0 offset1 offset2 length] — we reserve enough digits.
const signatureByteRangeSize = 60

// signatureContentsSize is the reserved hex-encoded signature space (8KB).
// PKCS#7 signatures are typically 2-6KB; 8KB provides ample room.
const signatureContentsSize = 8192

// SignPDF digitally signs the PDF document and writes the signed output.
//
// This creates a PKCS#7 detached signature (adbe.pkcs7.detached) embedded
// in the PDF, compatible with Adobe Reader and other PDF viewers.
//
// The signing process:
//  1. Builds the PDF with a placeholder signature dictionary
//  2. Computes the byte ranges excluding the signature contents
//  3. Signs the byte ranges using PKCS#7
//  4. Patches the signature contents into the final output
//
// Example:
//
//	cert, _ := gopdf.LoadCertificateFromPEM("cert.pem")
//	key, _ := gopdf.LoadPrivateKeyFromPEM("key.pem")
//	pdf.SignPDF(gopdf.SignatureConfig{
//	    Certificate: cert,
//	    PrivateKey:  key,
//	    Reason:      "Document Approval",
//	    Location:    "Beijing",
//	}, w)
func (gp *GoPdf) SignPDF(cfg SignatureConfig, w io.Writer) error {
	cfg.defaults()

	if cfg.Certificate == nil {
		return fmt.Errorf("gopdf: SignatureConfig.Certificate is required")
	}
	if cfg.PrivateKey == nil {
		return fmt.Errorf("gopdf: SignatureConfig.PrivateKey is required")
	}

	// Validate key type
	switch cfg.PrivateKey.(type) {
	case *rsa.PrivateKey, *ecdsa.PrivateKey:
		// OK
	default:
		return fmt.Errorf("gopdf: unsupported private key type %T", cfg.PrivateKey)
	}

	// Create the signature value object (placeholder).
	sigValObj := &signatureValueObj{
		cfg:          &cfg,
		contentsSize: signatureContentsSize,
	}
	sigValIdx := gp.addObj(sigValObj)
	sigValObjID := sigValIdx + 1

	// Create the signature field widget.
	sigFieldObj := &signatureFieldObj{
		cfg:         &cfg,
		sigValueRef: sigValObjID,
		getRoot:     func() *GoPdf { return gp },
	}
	sigFieldIdx := gp.addObj(sigFieldObj)

	// Register the signature field in the form fields list so AcroForm picks it up.
	gp.formFields = append(gp.formFields, formFieldRef{
		field: FormField{
			Type: FormFieldSignature,
			Name: cfg.SignatureFieldName,
		},
		objIdx: sigFieldIdx,
	})

	// Add the widget annotation to the target page.
	if cfg.Visible {
		pageObj := gp.findPageObjByNumber(cfg.PageNo)
		if pageObj != nil {
			pageObj.LinkObjIds = append(pageObj.LinkObjIds, sigFieldIdx+1)
		}
	}

	// Render the PDF into a buffer.
	var buf bytes.Buffer
	if _, err := gp.compilePdf(&buf); err != nil {
		return fmt.Errorf("gopdf: compile PDF for signing: %w", err)
	}
	pdfBytes := buf.Bytes()

	// Locate the signature contents placeholder to compute byte ranges.
	contentsStart, contentsEnd, err := sigValObj.findContentsPlaceholder(pdfBytes)
	if err != nil {
		return fmt.Errorf("gopdf: %w", err)
	}

	// Byte ranges: [0, contentsStart, contentsEnd, totalLen-contentsEnd]
	totalLen := len(pdfBytes)
	byteRange := [4]int{0, contentsStart, contentsEnd, totalLen - contentsEnd}

	// Patch the ByteRange value in the PDF.
	byteRangeStr := fmt.Sprintf("[%d %d %d %d]", byteRange[0], byteRange[1], byteRange[2], byteRange[3])
	// Pad to fixed width
	for len(byteRangeStr) < signatureByteRangeSize {
		byteRangeStr += " "
	}
	brPlaceholder := sigValObj.byteRangePlaceholder()
	brOffset := bytes.Index(pdfBytes, []byte(brPlaceholder))
	if brOffset < 0 {
		return fmt.Errorf("gopdf: ByteRange placeholder not found in PDF output")
	}
	copy(pdfBytes[brOffset:brOffset+len(brPlaceholder)], []byte(byteRangeStr))

	// Collect the data to sign (everything except the Contents hex string).
	signedData := make([]byte, 0, byteRange[1]+byteRange[3])
	signedData = append(signedData, pdfBytes[byteRange[0]:byteRange[0]+byteRange[1]]...)
	signedData = append(signedData, pdfBytes[byteRange[2]:byteRange[2]+byteRange[3]]...)

	// Create PKCS#7 detached signature.
	pkcs7Sig, err := createPKCS7Signature(signedData, &cfg)
	if err != nil {
		return fmt.Errorf("gopdf: create PKCS#7 signature: %w", err)
	}

	// Hex-encode the signature and patch it into the Contents.
	hexSig := fmt.Sprintf("%X", pkcs7Sig)
	if len(hexSig) > signatureContentsSize*2 {
		return fmt.Errorf("gopdf: PKCS#7 signature too large (%d bytes, max %d)", len(pkcs7Sig), signatureContentsSize)
	}
	// Pad with zeros
	for len(hexSig) < signatureContentsSize*2 {
		hexSig += "0"
	}

	// The Contents value in the PDF is <placeholder...>
	// contentsStart points to '<', contentsEnd points after '>'
	// We need to replace the hex content between < and >
	copy(pdfBytes[contentsStart+1:contentsEnd-1], []byte(hexSig))

	// Write the final signed PDF.
	if _, err := w.Write(pdfBytes); err != nil {
		return fmt.Errorf("gopdf: write signed PDF: %w", err)
	}
	return nil
}

// SignPDFToFile signs the PDF and writes it to a file.
func (gp *GoPdf) SignPDFToFile(cfg SignatureConfig, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("gopdf: create file: %w", err)
	}
	defer f.Close()
	return gp.SignPDF(cfg, f)
}

// createPKCS7Signature creates a PKCS#7 detached signature over the given data.
func createPKCS7Signature(data []byte, cfg *SignatureConfig) ([]byte, error) {
	signedData, err := pkcs7.NewSignedData(data)
	if err != nil {
		return nil, fmt.Errorf("new signed data: %w", err)
	}

	signerConfig := pkcs7.SignerInfoConfig{}
	if err := signedData.AddSigner(cfg.Certificate, cfg.PrivateKey, signerConfig); err != nil {
		return nil, fmt.Errorf("add signer: %w", err)
	}

	// Add certificate chain
	for _, cert := range cfg.CertificateChain {
		signedData.AddCertificate(cert)
	}

	// Detach content — the signature does not embed the original data
	signedData.Detach()

	return signedData.Finish()
}

// findPageObjByNumber returns the PageObj for the given 1-based page number.
func (gp *GoPdf) findPageObjByNumber(pageNo int) *PageObj {
	count := 0
	for _, obj := range gp.pdfObjs {
		if p, ok := obj.(*PageObj); ok {
			count++
			if count == pageNo {
				return p
			}
		}
	}
	return nil
}

// AddSignatureField adds an empty (unsigned) signature field to the current page.
// This can be used to create a signature placeholder that can be signed later.
func (gp *GoPdf) AddSignatureField(name string, x, y, w, h float64) error {
	return gp.AddFormField(FormField{
		Type:        FormFieldSignature,
		Name:        name,
		X:           x,
		Y:           y,
		W:           w,
		H:           h,
		HasBorder:   true,
		BorderColor: [3]uint8{0, 0, 0},
	})
}

// VerifySignatureFromFile reads a signed PDF file and verifies its digital signatures.
func VerifySignatureFromFile(path string) ([]SignatureVerifyResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read PDF: %w", err)
	}
	return VerifySignature(data)
}

// SignatureVerifyResult holds the result of verifying a single PDF signature.
type SignatureVerifyResult struct {
	// SignerName is the common name from the signing certificate.
	SignerName string
	// Valid is true if the signature is cryptographically valid.
	Valid bool
	// Reason is the stated reason for signing.
	Reason string
	// Location is the stated signing location.
	Location string
	// SignTime is the stated signing time.
	SignTime time.Time
	// Error contains the verification error, if any.
	Error error
}

// VerifySignature verifies digital signatures in a PDF byte slice.
// It extracts PKCS#7 signatures and validates them against the signed byte ranges.
func VerifySignature(pdfData []byte) ([]SignatureVerifyResult, error) {
	sigs, err := extractSignatures(pdfData)
	if err != nil {
		return nil, err
	}
	if len(sigs) == 0 {
		return nil, fmt.Errorf("no digital signatures found in PDF")
	}

	var results []SignatureVerifyResult
	for _, sig := range sigs {
		result := SignatureVerifyResult{
			Reason:   sig.reason,
			Location: sig.location,
			SignTime: sig.signTime,
		}

		// Collect signed data from byte ranges
		signedData := make([]byte, 0)
		for i := 0; i+1 < len(sig.byteRange); i += 2 {
			offset := sig.byteRange[i]
			length := sig.byteRange[i+1]
			if offset+length > len(pdfData) {
				result.Error = fmt.Errorf("byte range exceeds PDF size")
				result.Valid = false
				results = append(results, result)
				continue
			}
			signedData = append(signedData, pdfData[offset:offset+length]...)
		}

		// Parse and verify PKCS#7
		p7, err := pkcs7.Parse(sig.contents)
		if err != nil {
			result.Error = fmt.Errorf("parse PKCS#7: %w", err)
			result.Valid = false
			results = append(results, result)
			continue
		}

		// For detached signatures, set the content to the signed data
		p7.Content = signedData

		if err := p7.Verify(); err != nil {
			result.Error = fmt.Errorf("verify signature: %w", err)
			result.Valid = false
		} else {
			result.Valid = true
		}

		// Extract signer name
		if len(p7.Signers) > 0 {
			for _, cert := range p7.Certificates {
				if cert.SerialNumber.Cmp(p7.Signers[0].IssuerAndSerialNumber.SerialNumber) == 0 {
					result.SignerName = cert.Subject.CommonName
					break
				}
			}
		}

		results = append(results, result)
	}
	return results, nil
}

// rawSignature holds extracted signature data from a PDF.
type rawSignature struct {
	contents  []byte
	byteRange []int
	reason    string
	location  string
	signTime  time.Time
}

// extractSignatures does a simple scan of the PDF for signature dictionaries.
// This is a lightweight parser — it finds /Type /Sig dictionaries and extracts
// the /Contents and /ByteRange values.
func extractSignatures(pdfData []byte) ([]rawSignature, error) {
	var sigs []rawSignature

	// Scan for /Type /Sig patterns
	searchFrom := 0
	for {
		idx := bytes.Index(pdfData[searchFrom:], []byte("/Type /Sig"))
		if idx < 0 {
			// Also try /Type/Sig (no space)
			idx = bytes.Index(pdfData[searchFrom:], []byte("/Type/Sig"))
			if idx < 0 {
				break
			}
		}
		pos := searchFrom + idx

		// Find the dictionary boundaries (scan backward for <<, forward for >>)
		dictStart := bytes.LastIndex(pdfData[:pos], []byte("<<"))
		if dictStart < 0 {
			searchFrom = pos + 10
			continue
		}

		// Find matching >> (simple approach: find next >>)
		dictEnd := bytes.Index(pdfData[pos:], []byte(">>"))
		if dictEnd < 0 {
			searchFrom = pos + 10
			continue
		}
		dictEnd = pos + dictEnd + 2
		dict := pdfData[dictStart:dictEnd]

		sig := rawSignature{}

		// Extract /ByteRange [...]
		if brIdx := bytes.Index(dict, []byte("/ByteRange")); brIdx >= 0 {
			brStart := bytes.Index(dict[brIdx:], []byte("["))
			brEnd := bytes.Index(dict[brIdx:], []byte("]"))
			if brStart >= 0 && brEnd > brStart {
				brStr := string(dict[brIdx+brStart+1 : brIdx+brEnd])
				sig.byteRange = parseIntArray(brStr)
			}
		}

		// Extract /Contents <hex>
		if cIdx := bytes.Index(dict, []byte("/Contents")); cIdx >= 0 {
			rest := dict[cIdx+9:]
			hexStart := bytes.IndexByte(rest, '<')
			hexEnd := bytes.IndexByte(rest, '>')
			if hexStart >= 0 && hexEnd > hexStart {
				hexStr := string(rest[hexStart+1 : hexEnd])
				sig.contents = hexDecode(hexStr)
			}
		}

		// Extract /Reason
		if rIdx := bytes.Index(dict, []byte("/Reason")); rIdx >= 0 {
			sig.reason = extractPDFString(dict[rIdx+7:])
		}

		// Extract /Location
		if lIdx := bytes.Index(dict, []byte("/Location")); lIdx >= 0 {
			sig.location = extractPDFString(dict[lIdx+9:])
		}

		if len(sig.byteRange) >= 4 && len(sig.contents) > 0 {
			sigs = append(sigs, sig)
		}

		searchFrom = dictEnd
	}

	return sigs, nil
}

// parseIntArray parses space-separated integers from a string.
func parseIntArray(s string) []int {
	var nums []int
	var current []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			current = append(current, c)
		} else if len(current) > 0 {
			n := 0
			for _, d := range current {
				n = n*10 + int(d-'0')
			}
			nums = append(nums, n)
			current = current[:0]
		}
	}
	if len(current) > 0 {
		n := 0
		for _, d := range current {
			n = n*10 + int(d-'0')
		}
		nums = append(nums, n)
	}
	return nums
}

// hexDecode decodes a hex string to bytes.
func hexDecode(s string) []byte {
	// Remove whitespace and trailing zeros
	s = trimTrailingZeros(s)
	if len(s)%2 != 0 {
		s += "0"
	}
	result := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		result[i/2] = hexByte(s[i])<<4 | hexByte(s[i+1])
	}
	return result
}

func hexByte(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return 0
	}
}

func trimTrailingZeros(s string) string {
	// Trim pairs of trailing "00"
	for len(s) >= 2 && s[len(s)-2:] == "00" {
		s = s[:len(s)-2]
	}
	return s
}

// extractPDFString extracts a parenthesized PDF string value.
func extractPDFString(data []byte) string {
	// Skip whitespace
	i := 0
	for i < len(data) && (data[i] == ' ' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	if i >= len(data) || data[i] != '(' {
		return ""
	}
	i++ // skip '('
	depth := 1
	var result []byte
	for i < len(data) && depth > 0 {
		switch data[i] {
		case '(':
			depth++
			result = append(result, data[i])
		case ')':
			depth--
			if depth > 0 {
				result = append(result, data[i])
			}
		case '\\':
			i++
			if i < len(data) {
				result = append(result, data[i])
			}
		default:
			result = append(result, data[i])
		}
		i++
	}
	return string(result)
}

// Ensure rand is used (for potential future use in nonce generation).
var _ = rand.Reader
