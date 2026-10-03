package guiapp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

// The Token Bank key envelope is the one place where the GUI and HubCenter must
// agree byte for byte on four separate parameters (PBKDF2 owner/iterations/key
// length, nonce placement, OAEP hash, base64 framing). Get any one wrong and the
// failure surfaces only as the server's `decrypt_failed` — no field name, no
// hint.
//
// guiapp cannot import hubcenter/internal/skillmarket (Go's internal rule), so
// these tests decrypt with an *independent* reimplementation of the server's
// documented algorithm rather than by calling the server. That is the weaker of
// the two options, so the parameter values themselves are additionally pinned
// against the server source by a static check in
// TestTokenBankEnvelopeParamsMatchServerSource: if someone changes the server
// constants, that test fails and points at this file.

func tokenBankTestPublicKeyPEM(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// tokenBankServerDecrypt mirrors skillmarket.DecryptDownload exactly. It exists
// only in the test so the assertion does not simply re-run our own encryptor.
func tokenBankServerDecrypt(t *testing.T, raw string, owner string, priv *rsa.PrivateKey) ([]byte, error) {
	t.Helper()
	var pkg struct {
		EncryptedSalt []byte `json:"encrypted_salt"`
		EncryptedZip  []byte `json:"encrypted_zip"`
	}
	if err := json.Unmarshal([]byte(raw), &pkg); err != nil {
		t.Fatalf("the server could not unmarshal our envelope: %v", err)
	}
	salt, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, pkg.EncryptedSalt, nil)
	if err != nil {
		return nil, err
	}
	aesKey := pbkdf2.Key([]byte(owner), salt, 100_000, 32, sha256.New)
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(pkg.EncryptedZip) < nonceSize {
		return nil, io.ErrUnexpectedEOF
	}
	return gcm.Open(nil, pkg.EncryptedZip[:nonceSize], pkg.EncryptedZip[nonceSize:], nil)
}

func TestTokenBankEnvelopeRoundTripsThroughServerAlgorithm(t *testing.T) {
	priv, pubPEM := tokenBankTestPublicKeyPEM(t)

	const apiKey = "sk-live-abcdef0123456789"
	const apiURL = "https://api.example.com/v1"
	const protocol = "openai"

	envelope, err := tokenBankEncryptKey(pubPEM, tokenBankKeyPayload{
		APIKey:   apiKey,
		APIURL:   apiURL,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	plaintext, err := tokenBankServerDecrypt(t, envelope, tokenBankEnvelopeSaltOwner, priv)
	if err != nil {
		t.Fatalf("the server algorithm could not decrypt our envelope: %v", err)
	}

	var decoded struct {
		APIKey   string `json:"api_key"`
		APIURL   string `json:"api_url"`
		Protocol string `json:"protocol"`
	}
	if err := json.Unmarshal(plaintext, &decoded); err != nil {
		t.Fatalf("the decrypted payload is not the expected JSON shape: %v", err)
	}
	if decoded.APIKey != apiKey {
		t.Errorf("api_key round-trip: got %q want %q", decoded.APIKey, apiKey)
	}
	if decoded.APIURL != apiURL {
		t.Errorf("api_url round-trip: got %q want %q", decoded.APIURL, apiURL)
	}
	if decoded.Protocol != protocol {
		t.Errorf("protocol round-trip: got %q want %q", decoded.Protocol, protocol)
	}
}

// Changing any matched parameter must break decryption. Without this, a wrong
// value that is symmetric across both halves would still round-trip.
func TestTokenBankEnvelopeRejectsWrongSaltOwner(t *testing.T) {
	priv, pubPEM := tokenBankTestPublicKeyPEM(t)

	envelope, err := tokenBankEncryptKey(pubPEM, tokenBankKeyPayload{APIKey: "k", APIURL: "u"})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := tokenBankServerDecrypt(t, envelope, "some-other-owner", priv); err == nil {
		t.Fatal("decryption succeeded with the wrong salt owner; the owner is not actually bound into the key")
	}
}

func TestTokenBankEnvelopeRejectsWrongPrivateKey(t *testing.T) {
	_, pubPEM := tokenBankTestPublicKeyPEM(t)
	otherPriv, _ := tokenBankTestPublicKeyPEM(t)

	envelope, err := tokenBankEncryptKey(pubPEM, tokenBankKeyPayload{APIKey: "k", APIURL: "u"})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := tokenBankServerDecrypt(t, envelope, tokenBankEnvelopeSaltOwner, otherPriv); err == nil {
		t.Fatal("decryption succeeded with an unrelated private key; the RSA layer is not doing anything")
	}
}

// The fields must be base64 strings, which is what Go's []byte decoder expects.
// A number array would be a silent mismatch.
func TestTokenBankEnvelopeEmitsBase64ByteFields(t *testing.T) {
	_, pubPEM := tokenBankTestPublicKeyPEM(t)

	envelope, err := tokenBankEncryptKey(pubPEM, tokenBankKeyPayload{APIKey: "k", APIURL: "u"})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(envelope), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"encrypted_salt", "encrypted_zip"} {
		value, ok := raw[field]
		if !ok {
			t.Fatalf("envelope is missing %q", field)
		}
		if len(value) == 0 || value[0] != '"' {
			t.Errorf("%s is not a base64 string: %s", field, value)
		}
	}
}

func TestTokenBankEnvelopeRejectsNonPEMPublicKey(t *testing.T) {
	if _, err := tokenBankEncryptKey([]byte("not a pem"), tokenBankKeyPayload{APIKey: "k"}); err == nil {
		t.Fatal("expected an error for a non-PEM public key")
	}
}

// The nonce must be prepended (Go's `gcm.Seal(nonce, nonce, ...)` convention),
// not appended. An appending implementation would decrypt fine against itself
// but never against the server.
func TestTokenBankEnvelopePrependsNonce(t *testing.T) {
	priv, pubPEM := tokenBankTestPublicKeyPEM(t)

	envelope, err := tokenBankEncryptKey(pubPEM, tokenBankKeyPayload{APIKey: "k", APIURL: "u"})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var pkg struct {
		EncryptedSalt []byte `json:"encrypted_salt"`
		EncryptedZip  []byte `json:"encrypted_zip"`
	}
	if err := json.Unmarshal([]byte(envelope), &pkg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	salt, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, pkg.EncryptedSalt, nil)
	if err != nil {
		t.Fatalf("decrypt salt: %v", err)
	}
	block, _ := aes.NewCipher(pbkdf2.Key([]byte(tokenBankEnvelopeSaltOwner), salt, 100_000, 32, sha256.New))
	gcm, _ := cipher.NewGCM(block)
	nonceSize := gcm.NonceSize()
	if len(pkg.EncryptedZip) <= nonceSize {
		t.Fatalf("ciphertext too short to contain a prepended nonce: %d bytes", len(pkg.EncryptedZip))
	}
	nonce := pkg.EncryptedZip[:nonceSize]
	body := pkg.EncryptedZip[nonceSize:]
	if _, err := gcm.Open(nil, nonce, body, nil); err != nil {
		t.Fatalf("nonce is not prepended as the server expects: %v", err)
	}
}
