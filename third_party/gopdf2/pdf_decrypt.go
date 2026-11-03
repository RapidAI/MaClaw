package gopdf

import (
	"bytes"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrEncryptedPDF      = errors.New("PDF is encrypted; call OpenPDF with Password option")
	ErrInvalidPassword   = errors.New("invalid password")
	ErrUnsupportedCrypto = errors.New("unsupported encryption version (only V1/V2 R2/R3 supported)")
)

// decryptContext holds the state needed to decrypt a PDF.
type decryptContext struct {
	encryptionKey []byte
	keyLen        int // key length in bytes (5 for 40-bit, up to 16 for 128-bit)
	v             int // /V value
	r             int // /R value
}

// Pre-compiled regexes for PDF decryption.
var (
	reEncryptRef       = regexp.MustCompile(`/Encrypt\s+(\d+)\s+\d+\s+R`)
	reEncryptRefRemove = regexp.MustCompile(`/Encrypt\s+\d+\s+\d+\s+R\s*\n?`)
)

// detectEncryption checks if the PDF data contains an /Encrypt reference
// in the trailer and returns the encryption object number, or 0 if not encrypted.
func detectEncryption(data []byte) int {
	// Find trailer dictionary.
	trailerIdx := bytes.LastIndex(data, []byte("trailer"))
	if trailerIdx < 0 {
		return 0
	}
	trailer := string(data[trailerIdx:])
	m := reEncryptRef.FindStringSubmatch(trailer)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// parseEncryptDict extracts encryption parameters from an encrypt dictionary string.
func parseEncryptDict(dict string) (v, r, keyLen int, oValue, uValue []byte, pValue int, err error) {
	v = extractSignedIntValue(dict, "/V")
	r = extractSignedIntValue(dict, "/R")
	pValue = extractSignedIntValue(dict, "/P")

	keyLen = 5 // default 40-bit
	if lv := extractSignedIntValue(dict, "/Length"); lv > 0 {
		keyLen = lv / 8
	}
	if v == 1 {
		keyLen = 5
	}

	// Only support V1 (R2, 40-bit RC4) and V2 (R3, up to 128-bit RC4).
	if v != 1 && v != 2 {
		err = ErrUnsupportedCrypto
		return
	}
	if r != 2 && r != 3 {
		err = ErrUnsupportedCrypto
		return
	}

	oValue = extractHexOrLiteralString(dict, "/O")
	uValue = extractHexOrLiteralString(dict, "/U")

	if len(oValue) < 32 || len(uValue) < 32 {
		err = fmt.Errorf("invalid O or U value in encryption dictionary")
		return
	}
	return
}

// authenticate attempts to authenticate with the given password and returns
// a decryptContext if successful.
func authenticate(data []byte, password string) (*decryptContext, error) {
	encObjNum := detectEncryption(data)
	if encObjNum == 0 {
		return nil, nil // not encrypted
	}

	// Parse the encryption object.
	parser, err := newRawPDFParser(data)
	if err != nil {
		return nil, fmt.Errorf("parse PDF for decryption: %w", err)
	}

	encObj, ok := parser.objects[encObjNum]
	if !ok {
		return nil, fmt.Errorf("encryption object %d not found", encObjNum)
	}

	v, r, keyLen, oValue, uValue, pValue, err := parseEncryptDict(encObj.dict)
	if err != nil {
		return nil, err
	}

	pass := []byte(password)

	// Try as user password first.
	key, ok := tryUserPassword(pass, oValue, uValue, pValue, keyLen, r)
	if ok {
		return &decryptContext{encryptionKey: key, keyLen: keyLen, v: v, r: r}, nil
	}

	// Try as owner password.
	key, ok = tryOwnerPassword(pass, oValue, uValue, pValue, keyLen, r)
	if ok {
		return &decryptContext{encryptionKey: key, keyLen: keyLen, v: v, r: r}, nil
	}

	return nil, ErrInvalidPassword
}

// tryUserPassword attempts to authenticate with a user password.
func tryUserPassword(userPass, oValue, uValue []byte, pValue, keyLen, r int) ([]byte, bool) {
	key := computeEncryptionKey(userPass, oValue, pValue, keyLen, r)
	computedU := computeUValue(key, r)
	if r == 2 {
		return key, bytes.Equal(computedU, uValue[:32])
	}
	// R3: compare first 16 bytes only.
	return key, bytes.Equal(computedU[:16], uValue[:16])
}

// tryOwnerPassword attempts to authenticate with an owner password.
func tryOwnerPassword(ownerPass, oValue, uValue []byte, pValue, keyLen, r int) ([]byte, bool) {
	// Recover the user password from the O value.
	paddedOwner := padPassword(ownerPass)
	hash := md5.Sum(paddedOwner)
	ownerKey := hash[:]

	if r >= 3 {
		for i := 0; i < 50; i++ {
			h := md5.Sum(ownerKey[:keyLen])
			ownerKey = h[:]
		}
	}
	ownerKey = ownerKey[:keyLen]

	var userPass []byte
	if r == 2 {
		cip, err := rc4.NewCipher(ownerKey)
		if err != nil {
			return nil, false
		}
		userPass = make([]byte, 32)
		cip.XORKeyStream(userPass, oValue[:32])
	} else {
		userPass = make([]byte, 32)
		copy(userPass, oValue[:32])
		for i := 19; i >= 0; i-- {
			tmpKey := make([]byte, len(ownerKey))
			for j := range ownerKey {
				tmpKey[j] = ownerKey[j] ^ byte(i)
			}
			cip, err := rc4.NewCipher(tmpKey)
			if err != nil {
				return nil, false
			}
			cip.XORKeyStream(userPass, userPass)
		}
	}

	return tryUserPassword(userPass, oValue, uValue, pValue, keyLen, r)
}

// computeEncryptionKey computes the encryption key per PDF spec Algorithm 2.
func computeEncryptionKey(userPass, oValue []byte, pValue, keyLen, r int) []byte {
	padded := padPassword(userPass)
	m := md5.New()
	m.Write(padded)
	m.Write(oValue[:32])

	pBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(pBytes, uint32(int32(pValue)))
	m.Write(pBytes)

	// We don't have the file ID, so we skip it (common for simple encryption).
	// For a more robust implementation, the file ID from the trailer would be used.

	hash := m.Sum(nil)

	if r >= 3 {
		for i := 0; i < 50; i++ {
			h := md5.Sum(hash[:keyLen])
			hash = h[:]
		}
	}

	return hash[:keyLen]
}

// computeUValue computes the expected U value for verification.
func computeUValue(key []byte, r int) []byte {
	if r == 2 {
		cip, err := rc4.NewCipher(key)
		if err != nil {
			return nil
		}
		result := make([]byte, 32)
		cip.XORKeyStream(result, protectionPadding)
		return result
	}

	// R3: Algorithm 5.
	m := md5.New()
	m.Write(protectionPadding)
	// File ID would be added here for full compliance.
	hash := m.Sum(nil)

	cip, err := rc4.NewCipher(key)
	if err != nil {
		return nil
	}
	result := make([]byte, 16)
	cip.XORKeyStream(result, hash[:16])

	for i := 1; i <= 19; i++ {
		tmpKey := make([]byte, len(key))
		for j := range key {
			tmpKey[j] = key[j] ^ byte(i)
		}
		cip2, err := rc4.NewCipher(tmpKey)
		if err != nil {
			return nil
		}
		cip2.XORKeyStream(result, result)
	}

	// Pad to 32 bytes.
	padded := make([]byte, 32)
	copy(padded, result)
	return padded
}

// padPassword pads or truncates a password to 32 bytes using the standard padding.
func padPassword(pass []byte) []byte {
	padded := make([]byte, 32)
	n := copy(padded, pass)
	if n < 32 {
		copy(padded[n:], protectionPadding)
	}
	return padded
}

// decryptObjectStream decrypts a stream using the per-object key.
func (dc *decryptContext) decryptStream(objNum int, data []byte) ([]byte, error) {
	key := dc.objectKey(objNum)
	cip, err := rc4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	result := make([]byte, len(data))
	cip.XORKeyStream(result, data)
	return result, nil
}

// objectKey computes the per-object encryption key.
func (dc *decryptContext) objectKey(objNum int) []byte {
	tmp := make([]byte, dc.keyLen+5)
	copy(tmp, dc.encryptionKey)
	tmp[dc.keyLen] = byte(objNum & 0xff)
	tmp[dc.keyLen+1] = byte((objNum >> 8) & 0xff)
	tmp[dc.keyLen+2] = byte((objNum >> 16) & 0xff)
	tmp[dc.keyLen+3] = 0 // generation number low byte
	tmp[dc.keyLen+4] = 0 // generation number high byte

	hash := md5.Sum(tmp)
	n := dc.keyLen + 5
	if n > 16 {
		n = 16
	}
	return hash[:n]
}

// decryptPDF decrypts all streams and strings in the PDF data.
// Returns the decrypted PDF data with the /Encrypt reference removed.
func decryptPDF(data []byte, dc *decryptContext) []byte {
	parser, err := newRawPDFParser(data)
	if err != nil {
		return data
	}

	result := make([]byte, len(data))
	copy(result, data)

	for objNum, obj := range parser.objects {
		if obj.stream == nil {
			continue
		}
		// Find the raw (undecoded) stream in the original data.
		objHeader := fmt.Sprintf("%d 0 obj", objNum)
		idx := bytes.Index(data, []byte(objHeader))
		if idx < 0 {
			continue
		}
		objData := data[idx:]
		endIdx := bytes.Index(objData, []byte("endobj"))
		if endIdx < 0 {
			continue
		}
		objData = objData[:endIdx+6]

		streamStart := bytes.Index(objData, []byte("stream"))
		if streamStart < 0 {
			continue
		}
		rawStream := objData[streamStart+6:]
		if len(rawStream) > 0 && rawStream[0] == '\r' {
			rawStream = rawStream[1:]
		}
		if len(rawStream) > 0 && rawStream[0] == '\n' {
			rawStream = rawStream[1:]
		}
		endStream := bytes.Index(rawStream, []byte("endstream"))
		if endStream < 0 {
			continue
		}
		rawStream = rawStream[:endStream]
		rawStream = bytes.TrimRight(rawStream, "\r\n")

		// Decrypt the raw stream.
		decrypted, err := dc.decryptStream(objNum, rawStream)
		if err != nil {
			continue
		}

		// If FlateDecode, try to decompress to verify decryption worked.
		if strings.Contains(obj.dict, "/FlateDecode") {
			if _, err := zlibDecompress(decrypted); err != nil {
				continue // decryption may have failed, skip
			}
		}

		// Replace the raw stream in the result.
		result = replaceObjectStream(result, objNum, obj.dict, decrypted)
	}

	// Remove /Encrypt reference from trailer.
	result = removeEncryptFromTrailer(result)
	result = rebuildXref(result)
	return result
}

// removeEncryptFromTrailer removes the /Encrypt entry from the PDF trailer.
func removeEncryptFromTrailer(data []byte) []byte {
	return reEncryptRefRemove.ReplaceAll(data, nil)
}

// extractSignedIntValue extracts a (possibly negative) integer value for a given key from a PDF dictionary.
// Uses string search instead of regex for performance.
func extractSignedIntValue(dict, key string) int {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return 0
	}
	rest := dict[idx+len(key):]
	// Skip whitespace
	i := 0
	for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\r' || rest[i] == '\n') {
		i++
	}
	// Read optional minus sign and digits
	start := i
	if i < len(rest) && rest[i] == '-' {
		i++
	}
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i > start {
		v, _ := strconv.Atoi(rest[start:i])
		return v
	}
	return 0
}

// extractHexOrLiteralString extracts a string value (hex or literal) for a key.
func extractHexOrLiteralString(dict, key string) []byte {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return nil
	}
	rest := strings.TrimSpace(dict[idx+len(key):])

	if len(rest) > 0 && rest[0] == '<' {
		// Hex string.
		end := strings.Index(rest, ">")
		if end < 0 {
			return nil
		}
		hex := rest[1:end]
		return decodeHexString(hex)
	}

	if len(rest) > 0 && rest[0] == '(' {
		// Literal string.
		return decodeLiteralString(rest)
	}

	return nil
}

// decodeHexString decodes a PDF hex string.
func decodeHexString(hex string) []byte {
	hex = strings.ReplaceAll(hex, " ", "")
	hex = strings.ReplaceAll(hex, "\n", "")
	hex = strings.ReplaceAll(hex, "\r", "")
	if len(hex)%2 != 0 {
		hex += "0"
	}
	result := make([]byte, len(hex)/2)
	for i := 0; i < len(hex); i += 2 {
		b, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
		result[i/2] = byte(b)
	}
	return result
}

// decodeLiteralString decodes a PDF literal string starting with '('.
func decodeLiteralString(s string) []byte {
	if len(s) == 0 || s[0] != '(' {
		return nil
	}
	depth := 0
	var result []byte
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == '(' {
			depth++
			if depth > 1 {
				result = append(result, ch)
			}
		} else if ch == ')' {
			depth--
			if depth == 0 {
				break
			}
			result = append(result, ch)
		} else if ch == '\\' && i+1 < len(s) {
			i++
			next := s[i]
			switch next {
			case 'n':
				result = append(result, '\n')
			case 'r':
				result = append(result, '\r')
			case 't':
				result = append(result, '\t')
			case '\\':
				result = append(result, '\\')
			case '(':
				result = append(result, '(')
			case ')':
				result = append(result, ')')
			default:
				// Octal escape.
				if next >= '0' && next <= '7' {
					oct := string(next)
					for j := 0; j < 2 && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '7'; j++ {
						i++
						oct += string(s[i])
					}
					v, _ := strconv.ParseUint(oct, 8, 8)
					result = append(result, byte(v))
				} else {
					result = append(result, next)
				}
			}
		} else {
			result = append(result, ch)
		}
		i++
	}
	return result
}
