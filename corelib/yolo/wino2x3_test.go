package yolo

import (
	"math/rand"
	"testing"
)

// TestWinograd2x3_MatchesNormal validates the F(2,3) path against the
// generic im2col+GEMM conv on representative shapes.
func TestWinograd2x3_MatchesNormal(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	shapes := []struct{ outC, inC, H, W int }{
		{128, 128, 160, 160}, {256, 256, 80, 80}, {512, 512, 40, 40},
		{512, 512, 20, 20}, {64, 64, 16, 16}, {256, 128, 80, 80},
		{65, 128, 40, 40}, {48, 3, 640, 640},
	}
	for _, s := range shapes {
		c := &Conv2dBNSiLU{
			Weight:  NewTensor(s.outC, s.inC, 3, 3),
			Bias:    make([]float32, s.outC),
			OutC:    s.outC, InC: s.inC, KH: 3, KW: 3,
			Stride: 1, Padding: 1, Groups: 1, UseSiLU: true,
		}
		for i := range c.Weight.Data {
			c.Weight.Data[i] = rng.Float32()*2 - 1
		}
		for i := range c.Bias {
			c.Bias[i] = rng.Float32()*2 - 1
		}
		c.initWinograd2x3()
		if c.Wino2x3 == nil {
			t.Fatalf("%d→%d %dx%d: not eligible", s.inC, s.outC, s.H, s.W)
		}
		input := NewTensor(1, s.inC, s.H, s.W)
		for i := range input.Data {
			input.Data[i] = rng.Float32()*2 - 1
		}
		got := c.forwardWinograd2x3(input)
		if got == nil {
			t.Fatalf("%d→%d %dx%d: forwardWinograd2x3 refused", s.inC, s.outC, s.H, s.W)
		}
		want := c.forwardNormal(input)
		if i, worst := closeEnough(got.Data, want.Data, 2e-3); i >= 0 {
			t.Errorf("%d→%d %dx%d: worst rel err %g at %d (got %g want %g)",
				s.inC, s.outC, s.H, s.W, worst, i, got.Data[i], want.Data[i])
		}
	}
}

// TestWinograd2x3_NoActivation covers UseSiLU=false (detect head style).
func TestWinograd2x3_NoActivation(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	c := &Conv2dBNSiLU{
		Weight:  NewTensor(64, 128, 3, 3),
		Bias:    make([]float32, 64),
		OutC:    64, InC: 128, KH: 3, KW: 3,
		Stride: 1, Padding: 1, Groups: 1, UseSiLU: false,
	}
	for i := range c.Weight.Data {
		c.Weight.Data[i] = rng.Float32()*2 - 1
	}
	c.initWinograd2x3()
	input := NewTensor(1, 128, 40, 40)
	for i := range input.Data {
		input.Data[i] = rng.Float32()*2 - 1
	}
	got := c.forwardWinograd2x3(input)
	want := c.forwardNormal(input)
	if i, worst := closeEnough(got.Data, want.Data, 2e-3); i >= 0 {
		t.Errorf("worst rel err %g at %d (got %g want %g)", worst, i, got.Data[i], want.Data[i])
	}
}
