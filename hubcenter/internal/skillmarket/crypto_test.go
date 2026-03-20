package skillmarket

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

func generateTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// ── Task 5.3: 加密解密 round-trip 属性测试 ──────────────────────────────

func TestCrypto_RoundTrip(t *testing.T) {
	privKey := generateTestKey(t)

	testCases := []struct {
		name   string
		data   []byte
		userID string
	}{
		{"small", []byte("hello world"), "user-123"},
		{"empty", []byte{}, "user-456"},
		{"binary", make([]byte, 1024), "user-789"},
		{"large", make([]byte, 64*1024), "user-abc"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 填充随机数据
			if len(tc.data) > 11 {
				rand.Read(tc.data)
			}

			pkg, err := EncryptForDownload(tc.data, tc.userID, privKey)
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}

			decrypted, err := DecryptDownload(pkg, tc.userID, privKey)
			if err != nil {
				t.Fatalf("decrypt: %v", err)
			}

			if !bytes.Equal(decrypted, tc.data) {
				t.Errorf("round-trip failed: decrypted data doesn't match original")
			}
		})
	}
}

// ── Task 5.4: 加密安全性测试 ────────────────────────────────────────────

func TestCrypto_WrongUserID(t *testing.T) {
	privKey := generateTestKey(t)
	data := []byte("secret skill content")

	pkg, err := EncryptForDownload(data, "correct-user", privKey)
	if err != nil {
		t.Fatal(err)
	}

	// 用错误的 userID 解密应失败
	_, err = DecryptDownload(pkg, "wrong-user", privKey)
	if err == nil {
		t.Error("expected error when decrypting with wrong userID")
	}
}

func TestCrypto_WrongKey(t *testing.T) {
	privKey1 := generateTestKey(t)
	privKey2 := generateTestKey(t)
	data := []byte("secret skill content")

	pkg, err := EncryptForDownload(data, "user-1", privKey1)
	if err != nil {
		t.Fatal(err)
	}

	// 用错误的私钥解密 salt 应失败
	_, err = DecryptDownload(pkg, "user-1", privKey2)
	if err == nil {
		t.Error("expected error when decrypting with wrong RSA key")
	}
}

func TestCrypto_TamperedData(t *testing.T) {
	privKey := generateTestKey(t)
	data := []byte("secret skill content")

	pkg, err := EncryptForDownload(data, "user-1", privKey)
	if err != nil {
		t.Fatal(err)
	}

	// 篡改密文
	if len(pkg.EncryptedZip) > 20 {
		pkg.EncryptedZip[15] ^= 0xFF
	}

	_, err = DecryptDownload(pkg, "user-1", privKey)
	if err == nil {
		t.Error("expected error when decrypting tampered data")
	}
}
