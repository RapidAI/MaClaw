package cloudworkspace

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const (
	masterKeyEnv  = "MACLAW_CWS_MASTER_KEY"
	masterKeyFile = "master.key"
	dekInfoPrefix = "maclaw-cws-v1"
)

func loadMasterKey(dir string) ([]byte, error) {
	key, err := defaultKeyProvider(dir).ActiveKey(context.Background())
	if err != nil {
		return nil, err
	}
	return key.Bytes, nil
}

var ciphertextEnvelopeMagic = [8]byte{'M', 'C', 'W', 'S', 'E', 'N', 'V', 2}

const ciphertextEnvelopeHeaderSize = len(ciphertextEnvelopeMagic) + sha256.Size

const ciphertextEnvelopePrefixSize = len(ciphertextEnvelopeMagic) - 1

func (s *BlobStore) keyProvider() KeyProvider {
	if s != nil && s.Keys != nil {
		return s.Keys
	}
	return defaultKeyProvider(s.keyDir())
}

func sealWorkspace(ctx context.Context, provider KeyProvider, tenantID, userID, workspaceID string, aad, plaintext []byte) ([]byte, string, error) {
	key, err := provider.ActiveKey(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(key.Bytes) != 32 || encryptionKeyID(key.Bytes) != key.ID {
		return nil, "", fmt.Errorf("invalid active cloud workspace encryption key")
	}
	dek := deriveDEK(key.Bytes, tenantID, userID, workspaceID)
	sealed, err := seal(dek, aad, plaintext)
	if err != nil {
		return nil, "", err
	}
	idRaw, err := hex.DecodeString(key.ID)
	if err != nil || len(idRaw) != sha256.Size {
		return nil, "", fmt.Errorf("invalid cloud workspace encryption key id")
	}
	envelope := make([]byte, 0, ciphertextEnvelopeHeaderSize+len(sealed))
	envelope = append(envelope, ciphertextEnvelopeMagic[:]...)
	envelope = append(envelope, idRaw...)
	envelope = append(envelope, sealed...)
	return envelope, encryptionVersion(key.ID), nil
}

func openWorkspace(ctx context.Context, provider KeyProvider, tenantID, userID, workspaceID string, aad, blob []byte) ([]byte, string, error) {
	// Once the envelope magic is present, never fall back to legacy probing.
	// A truncated/corrupted v2 header must fail closed instead of being
	// interpreted as a v1 blob (which could make corruption look like a valid
	// object if an old key happens to authenticate it).
	if hasCiphertextEnvelopePrefix(blob) {
		if len(blob) < len(ciphertextEnvelopeMagic) || !bytes.Equal(blob[:len(ciphertextEnvelopeMagic)], ciphertextEnvelopeMagic[:]) {
			return nil, "", ErrBlobCorrupt
		}
		if len(blob) < ciphertextEnvelopeHeaderSize {
			return nil, "", ErrBlobCorrupt
		}
		keyID := hex.EncodeToString(blob[len(ciphertextEnvelopeMagic):ciphertextEnvelopeHeaderSize])
		key, err := provider.Key(ctx, keyID)
		if err != nil {
			return nil, keyID, err
		}
		if len(key.Bytes) != 32 || encryptionKeyID(key.Bytes) != key.ID || key.ID != keyID {
			return nil, keyID, ErrBlobCorrupt
		}
		plain, err := open(deriveDEK(key.Bytes, tenantID, userID, workspaceID), aad, blob[ciphertextEnvelopeHeaderSize:])
		return plain, keyID, err
	}
	keys, err := provider.AllKeys(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, key := range keys {
		if len(key.Bytes) != 32 || encryptionKeyID(key.Bytes) != key.ID {
			continue
		}
		plain, openErr := open(deriveDEK(key.Bytes, tenantID, userID, workspaceID), aad, blob)
		if openErr == nil {
			return plain, key.ID, nil
		}
	}
	return nil, "", ErrBlobCorrupt
}

func ciphertextKeyID(blob []byte) (string, bool, error) {
	if !hasCiphertextEnvelopePrefix(blob) {
		return "", false, nil
	}
	if len(blob) < len(ciphertextEnvelopeMagic) || !bytes.Equal(blob[:len(ciphertextEnvelopeMagic)], ciphertextEnvelopeMagic[:]) || len(blob) < ciphertextEnvelopeHeaderSize {
		return "", true, ErrBlobCorrupt
	}
	return hex.EncodeToString(blob[len(ciphertextEnvelopeMagic):ciphertextEnvelopeHeaderSize]), true, nil
}

func hasCiphertextEnvelopePrefix(blob []byte) bool {
	return len(blob) >= ciphertextEnvelopePrefixSize && bytes.Equal(blob[:ciphertextEnvelopePrefixSize], ciphertextEnvelopeMagic[:ciphertextEnvelopePrefixSize])
}

func encryptionVersion(keyID string) string { return "aes-gcm-v2:" + keyID }

func deriveDEK(master []byte, tenantID, userID, workspaceID string) []byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(dekInfoPrefix + "\x00" + tenantID + "\x00" + userID + "\x00" + workspaceID))
	return mac.Sum(nil)
}

func objectAAD(tenantID, userID, workspaceID string) []byte {
	return []byte(tenantID + "|" + userID + "|" + workspaceID)
}

func seal(dek, aad, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

func open(dek, aad, blob []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(blob) < ns {
		return nil, ErrBlobCorrupt
	}
	plain, err := gcm.Open(nil, blob[:ns], blob[ns:], aad)
	if err != nil {
		return nil, ErrBlobCorrupt
	}
	return plain, nil
}

func plaintextSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
