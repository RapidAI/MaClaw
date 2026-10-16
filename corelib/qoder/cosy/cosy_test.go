package cosy

import (
	"strings"
	"testing"
)

// TestWASMModuleChecksum pins the embedded module so a stale or truncated
// copy cannot ship silently.
func TestWASMModuleChecksum(t *testing.T) {
	const want = "0061736d01"
	if len(qoderAuthWASM) != 298606 {
		t.Fatalf("embedded wasm size %d, want 298606 (official 1.1.64 module)", len(qoderAuthWASM))
	}
	if s := hexPrefix(qoderAuthWASM[:5]); s != want {
		t.Fatalf("wasm magic prefix %s, want %s", s, want)
	}
}

func hexPrefix(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0x0f])
	}
	return string(out)
}

func TestGenerateAuthFieldsShape(t *testing.T) {
	material, err := GenerateAuthFields(UserInfo{UID: "01a11487-c7fa-7aab-b1b4-0ec70fcd5743"})
	if err != nil {
		t.Fatalf("GenerateAuthFields: %v", err)
	}
	if material.UID != "01a11487-c7fa-7aab-b1b4-0ec70fcd5743" {
		t.Fatalf("material uid = %q", material.UID)
	}
	if !strings.Contains(material.EncryptUserInfo, "+") && !strings.Contains(material.EncryptUserInfo, "=") && len(material.EncryptUserInfo) < 100 {
		t.Fatalf("encrypt_user_info too short/shapeless: %q", material.EncryptUserInfo)
	}
	if len(material.Key) < 100 {
		t.Fatalf("key too short: %q", material.Key)
	}
}

func TestPrepareCatalogRequest(t *testing.T) {
	ctx, err := New("aca8dfc3-8c5a-43f5-8d81-4043fbf64335", "1.1.64", "01a11487-c7fa-7aab-b1b4-0ec70fcd5743")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ctx.Close()
	if err := ctx.RefreshAuthFields("dt-test-token"); err != nil {
		t.Fatalf("RefreshAuthFields: %v", err)
	}
	res, err := ctx.PrepareRequest("https://gateway.qoder.com.cn", "/algo/api/v2/model/list", "GET", "auth", "", "")
	if err != nil {
		t.Fatalf("PrepareRequest: %v", err)
	}
	if res.URL != "https://gateway.qoder.com.cn/algo/api/v2/model/list" {
		t.Fatalf("url = %q", res.URL)
	}
	auth, ok := res.Headers["Authorization"]
	if !ok || !strings.HasPrefix(auth, "Bearer COSY.") {
		t.Fatalf("Authorization missing COSY bearer: %v", res.Headers)
	}
	for _, key := range []string{"Cosy-Business-Product", "Cosy-Business-Type", "Cosy-ClientType", "Cosy-Date", "Cosy-Key", "Cosy-MachineId", "Cosy-Data-Policy"} {
		if res.Headers[key] == "" {
			t.Fatalf("missing header %s in %v", key, res.Headers)
		}
	}
}
