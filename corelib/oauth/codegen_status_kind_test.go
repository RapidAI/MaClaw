package oauth

import "testing"

func TestCodeGenScanStatusMethodsUseNormalizedEnum(t *testing.T) {
	if !codeGenScanStatus(" SUCCESS ").IsSuccess() {
		t.Fatal("expected scan status with whitespace and uppercase to be success")
	}
	if !codeGenScanStatus(" Expired ").IsExpired() {
		t.Fatal("expected scan status with whitespace and mixed case to be expired")
	}
	if codeGenScanStatus("pending").IsSuccess() {
		t.Fatal("pending scan status should not be success")
	}
}

func TestCodeGenModelStatusIsUsableUsesNormalizedEnum(t *testing.T) {
	tests := []struct {
		name string
		in   codeGenModelStatus
		want bool
	}{
		{name: "blank", in: "", want: true},
		{name: "ready", in: " READY ", want: true},
		{name: "active", in: "active", want: true},
		{name: "disabled", in: " disabled ", want: false},
		{name: "no permission dash", in: "NO-PERMISSION", want: false},
		{name: "unknown external status", in: "rollout", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.IsUsable(); got != tt.want {
				t.Fatalf("IsUsable() = %v, want %v", got, tt.want)
			}
		})
	}
}
