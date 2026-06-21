package imgconv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConvertSVGToPNG_RealFile(t *testing.T) {
	svgDir := `D:\专利申请测试1`
	if _, err := os.Stat(svgDir); err != nil {
		t.Skip("test SVG directory not available")
	}

	svgPath := filepath.Join(svgDir, "附图_图1_整体方法流程图.svg")
	if _, err := os.Stat(svgPath); err != nil {
		t.Skip("test SVG file not available")
	}

	pngPath := filepath.Join(os.TempDir(), "imgconv_test_output.png")
	defer os.Remove(pngPath)

	err := ConvertSVGToPNG(svgPath, pngPath, 900)
	if err != nil {
		t.Fatalf("ConvertSVGToPNG failed: %v", err)
	}

	info, err := os.Stat(pngPath)
	if err != nil {
		t.Fatalf("PNG not created: %v", err)
	}
	t.Logf("PNG size: %d bytes", info.Size())
	if info.Size() < 1000 {
		t.Error("PNG suspiciously small, might be blank")
	}
}
