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
	"fmt"
	"io"

	"golang.org/x/crypto/pbkdf2"
)

// Client-side Token Bank key envelope.
//
// The plaintext provider key must never reach HubCenter in the clear: only the
// server holds the RSA private key, and the GUI only ever has the public key
// (GET /api/v1/crypto/pubkey). This file is the *encrypting* half of
// `skillmarket.EncryptForDownload` / `DecryptDownload`; the server decrypts with
// `DecryptDownload` and a fixed salt owner, so the four parameters that must
// match exactly are:
//
//	PBKDF2-SHA256(password=owner, salt, iter=100_000, keyLen=32)
//	nonce is PREPENDED to the GCM ciphertext (Go's `gcm.Seal(nonce, nonce, ...)`)
//	RSA-OAEP with SHA-256 over the raw salt
//	both byte fields are standard base64 when marshalled (Go's []byte JSON form)
//
// Any drift in one of those silently produces an envelope the server cannot
// decrypt — and "decrypt_failed" is all the user would see. Keeping this next to
// the encrypt call, rather than in a generic crypto helper, is deliberate.

const (
	// tokenBankEnvelopeSaltOwner mirrors tokenBankEnvelopeSaltOwner on the
	// server. It is a domain separator, not a per-user secret: the
	// confidentiality comes from the RSA layer.
	tokenBankEnvelopeSaltOwner = "token-bank-share"
	tokenBankEnvelopeIter      = 100_000
	tokenBankEnvelopeKeyLen    = 32
	tokenBankEnvelopeSaltLen   = 32
)

// tokenBankEncryptedPackage is the wire shape the server unmarshals into
// `skillmarket.EncryptedPackage`. Field names match it exactly.
type tokenBankEncryptedPackage struct {
	EncryptedSalt []byte `json:"encrypted_salt"`
	EncryptedZip  []byte `json:"encrypted_zip"`
}

// tokenBankKeyPayload is the JSON that gets encrypted. The server requires this
// exact shape: a bare key string is rejected on purpose, because a payload
// without `api_url` would not bind the key to the endpoint it was tested
// against.
type tokenBankKeyPayload struct {
	APIKey   string `json:"api_key"`
	APIURL   string `json:"api_url"`
	Protocol string `json:"protocol"`
}

// tokenBankEncryptKey wraps the provider key into the envelope the server can
// decrypt. `publicKeyPEM` is the raw body of GET /api/v1/crypto/pubkey.
func tokenBankEncryptKey(publicKeyPEM []byte, payload tokenBankKeyPayload) (string, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return "", fmt.Errorf("HubCenter public key is not valid PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("HubCenter public key could not be parsed: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("HubCenter public key is not an RSA key")
	}

	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	salt := make([]byte, tokenBankEnvelopeSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	aesKey := pbkdf2.Key([]byte(tokenBankEnvelopeSaltOwner), salt, tokenBankEnvelopeIter, tokenBankEnvelopeKeyLen, sha256.New)
	blockCipher, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	// nonce || ciphertext — the server slices the first NonceSize bytes back off.
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)

	encryptedSalt, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, rsaPub, salt, nil)
	if err != nil {
		return "", fmt.Errorf("rsa encrypt salt: %w", err)
	}

	envelope, err := json.Marshal(tokenBankEncryptedPackage{
		EncryptedSalt: encryptedSalt,
		EncryptedZip:  sealed,
	})
	if err != nil {
		return "", err
	}
	return string(envelope), nil
}
