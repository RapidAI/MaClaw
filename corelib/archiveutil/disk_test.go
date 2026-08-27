package archiveutil

import (
	"testing"
)

func TestAvailableBytes(t *testing.T) {
	n, err := AvailableBytes(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if n <= 0 {
		t.Fatalf("available=%d", n)
	}
}
