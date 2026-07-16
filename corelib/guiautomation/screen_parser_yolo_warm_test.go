package guiautomation

import (
	"path/filepath"
	"testing"
)

func TestYOLOScreenParserWarmMissingWeights(t *testing.T) {
	p := NewYOLOScreenParser(filepath.Join(t.TempDir(), "missing.yolow"), 0.3, 0.5)
	if err := p.Warm(); err == nil {
		t.Fatal("expected error for missing weights")
	}
	if p.Loaded() {
		t.Fatal("should not be loaded after failed warm")
	}
}

func TestYOLOScreenParserWarmNil(t *testing.T) {
	var p *YOLOScreenParser
	if err := p.Warm(); err == nil {
		t.Fatal("expected error for nil parser")
	}
}
