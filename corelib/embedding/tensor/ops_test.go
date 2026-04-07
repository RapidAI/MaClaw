package tensor

import "testing"

func TestElemMul_AllowsOutAliasA(t *testing.T) {
	a := []float32{2, 3, 4}
	b := []float32{10, 20, 30}
	ElemMul(a, a, b)
	want := []float32{20, 60, 120}
	for i, got := range a {
		if got != want[i] {
			t.Fatalf("a[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestElemMul_AllowsOutAliasB(t *testing.T) {
	a := []float32{10, 20, 30}
	b := []float32{2, 3, 4}
	ElemMul(b, a, b)
	want := []float32{20, 60, 120}
	for i, got := range b {
		if got != want[i] {
			t.Fatalf("b[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAdd_AllowsOutAliasA(t *testing.T) {
	a := []float32{2, 3, 4}
	b := []float32{10, 20, 30}
	Add(a, a, b)
	want := []float32{12, 23, 34}
	for i, got := range a {
		if got != want[i] {
			t.Fatalf("a[%d] = %v, want %v", i, got, want[i])
		}
	}
}

func TestAdd_AllowsOutAliasB(t *testing.T) {
	a := []float32{10, 20, 30}
	b := []float32{2, 3, 4}
	Add(b, a, b)
	want := []float32{12, 23, 34}
	for i, got := range b {
		if got != want[i] {
			t.Fatalf("b[%d] = %v, want %v", i, got, want[i])
		}
	}
}
