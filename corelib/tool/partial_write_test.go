package tool

import "testing"

func TestClassifyPartialWriteExisting(t *testing.T) {
	existing := []byte("hello")
	if got := ClassifyPartialWriteExisting(existing, []byte("hello world")); got != PartialWriteExtendsExisting {
		t.Fatalf("longer same body = %v", got)
	}
	if got := ClassifyPartialWriteExisting([]byte("hello world"), existing); got != PartialWriteAlreadyCovered {
		t.Fatalf("shorter same body = %v", got)
	}
	if got := ClassifyPartialWriteExisting(existing, existing); got != PartialWriteAlreadyCovered {
		t.Fatalf("equal body = %v", got)
	}
	if got := ClassifyPartialWriteExisting([]byte("references"), []byte("a much longer different document")); got != PartialWriteUnrelated {
		t.Fatalf("different longer body = %v", got)
	}
	if got := ClassifyPartialWriteExisting(nil, []byte("new")); got != PartialWriteExtendsExisting {
		t.Fatalf("empty file = %v", got)
	}
}
