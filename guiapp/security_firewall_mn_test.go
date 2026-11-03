package guiapp

import "testing"

func TestMNLocalSideEffectNeedsConfirmReadOnlyExempt(t *testing.T) {
	// No MN local accept recorded → never forces confirmation.
	if mnLocalSideEffectNeedsConfirm("write_file", "Энэ файлд хадгална уу") {
		t.Fatal("remote-decided MN turn must not force confirmation")
	}
	if mnLocalSideEffectNeedsConfirm("write_file", "save this file") {
		t.Fatal("non-Cyrillic must not force confirmation")
	}
}
