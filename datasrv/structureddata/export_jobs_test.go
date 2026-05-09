package structureddata

import (
	"errors"
	"testing"
)

func TestNormalizeExportLimitRejectsOversizedExplicitLimit(t *testing.T) {
	if got, err := normalizeExportLimit(0, 5000); err != nil || got != 5000 {
		t.Fatalf("normalizeExportLimit default got=%d err=%v, want 5000 nil", got, err)
	}
	if got, err := normalizeExportLimit(250, 5000); err != nil || got != 250 {
		t.Fatalf("normalizeExportLimit explicit got=%d err=%v, want 250 nil", got, err)
	}
	if _, err := normalizeExportLimit(5001, 5000); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("normalizeExportLimit oversized err=%v, want ErrInvalidInput", err)
	}
}
