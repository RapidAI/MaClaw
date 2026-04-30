package tts

import (
	"math"
	"testing"
)

func TestLeakyReLU(t *testing.T) {
	x := []float32{-2, -1, 0, 1, 2}
	LeakyReLU(x, 0.1)
	want := []float32{-0.2, -0.1, 0, 1, 2}
	for i := range x {
		if math.Abs(float64(x[i]-want[i])) > 1e-6 {
			t.Errorf("LeakyReLU[%d] = %f, want %f", i, x[i], want[i])
		}
	}
}

func TestReLU(t *testing.T) {
	x := []float32{-2, -1, 0, 1, 2}
	ReLU(x)
	want := []float32{0, 0, 0, 1, 2}
	for i := range x {
		if x[i] != want[i] {
			t.Errorf("ReLU[%d] = %f, want %f", i, x[i], want[i])
		}
	}
}

func TestExp(t *testing.T) {
	x := []float32{0, 1, -1}
	Exp(x)
	if math.Abs(float64(x[0]-1.0)) > 1e-6 {
		t.Errorf("Exp(0) = %f", x[0])
	}
	if math.Abs(float64(x[1])-math.E) > 1e-5 {
		t.Errorf("Exp(1) = %f", x[1])
	}
	if math.Abs(float64(x[2])-1.0/math.E) > 1e-5 {
		t.Errorf("Exp(-1) = %f", x[2])
	}
}

func TestCeil(t *testing.T) {
	x := []float32{1.1, 2.0, -0.5, 3.9}
	Ceil(x)
	want := []float32{2, 2, 0, 4}
	for i := range x {
		if x[i] != want[i] {
			t.Errorf("Ceil[%d] = %f, want %f", i, x[i], want[i])
		}
	}
}

func TestFlipChannels(t *testing.T) {
	// [3, 2] tensor: channels 0=[1,2], 1=[3,4], 2=[5,6]
	data := []float32{1, 2, 3, 4, 5, 6}
	FlipChannels(data, 3, 2)
	want := []float32{5, 6, 3, 4, 1, 2}
	for i := range data {
		if data[i] != want[i] {
			t.Errorf("FlipChannels[%d] = %f, want %f", i, data[i], want[i])
		}
	}
}

func TestConv1D_NoPadding(t *testing.T) {
	// input: [1, 5] = {1, 2, 3, 4, 5}
	// kernel: [1, 1, 3] = {1, 0, -1}
	// stride=1, padding=0
	// expected output: [1, 3] = {1*1+2*0+3*(-1), 2*1+3*0+4*(-1), 3*1+4*0+5*(-1)} = {-2, -2, -2}
	input := []float32{1, 2, 3, 4, 5}
	kernel := []float32{1, 0, -1}
	out := Conv1D(input, 1, 5, kernel, 3, 1, 1, 0, nil)
	if len(out) != 3 {
		t.Fatalf("Conv1D output len = %d, want 3", len(out))
	}
	for i, v := range out {
		if math.Abs(float64(v-(-2))) > 1e-6 {
			t.Errorf("Conv1D[%d] = %f, want -2", i, v)
		}
	}
}

func TestConv1D_WithPadding(t *testing.T) {
	// input: [1, 3] = {1, 2, 3}
	// kernel: [1, 1, 3] = {1, 1, 1}
	// stride=1, padding=1
	// padded: {0, 1, 2, 3, 0}
	// expected: {0+1+2, 1+2+3, 2+3+0} = {3, 6, 5}
	input := []float32{1, 2, 3}
	kernel := []float32{1, 1, 1}
	out := Conv1D(input, 1, 3, kernel, 3, 1, 1, 1, nil)
	if len(out) != 3 {
		t.Fatalf("Conv1D output len = %d, want 3", len(out))
	}
	want := []float32{3, 6, 5}
	for i := range out {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("Conv1D[%d] = %f, want %f", i, out[i], want[i])
		}
	}
}

func TestConv1D_WithBias(t *testing.T) {
	input := []float32{1, 2, 3}
	kernel := []float32{1, 1, 1}
	bias := []float32{10}
	out := Conv1D(input, 1, 3, kernel, 3, 1, 1, 1, bias)
	want := []float32{13, 16, 15}
	for i := range out {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("Conv1D_bias[%d] = %f, want %f", i, out[i], want[i])
		}
	}
}

func TestConvTranspose1D_Basic(t *testing.T) {
	// input: [1, 3] = {1, 2, 3}
	// kernel: [1, 1, 3] = {1, 1, 1}
	// stride=1, padding=0
	// outLen = (3-1)*1 - 0 + 3 = 5
	// Position 0: in[0]*k[0] = 1
	// Position 1: in[0]*k[1] + in[1]*k[0] = 1+2 = 3
	// Position 2: in[0]*k[2] + in[1]*k[1] + in[2]*k[0] = 1+2+3 = 6
	// Position 3: in[1]*k[2] + in[2]*k[1] = 2+3 = 5
	// Position 4: in[2]*k[2] = 3
	input := []float32{1, 2, 3}
	kernel := []float32{1, 1, 1}
	out := ConvTranspose1D(input, 1, 3, kernel, 3, 1, 1, 0, nil)
	if len(out) != 5 {
		t.Fatalf("ConvTranspose1D output len = %d, want 5", len(out))
	}
	want := []float32{1, 3, 6, 5, 3}
	for i := range out {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("ConvTranspose1D[%d] = %f, want %f", i, out[i], want[i])
		}
	}
}

func TestConvTranspose1D_Stride2(t *testing.T) {
	// input: [1, 2] = {1, 2}
	// kernel: [1, 1, 4] = {1, 2, 3, 4}
	// stride=2, padding=1
	// outLen = (2-1)*2 - 2 + 4 = 4
	// Scatter from in[0]=1 at positions {0-1, 1-1, 2-1, 3-1} = {-1, 0, 1, 2}
	//   pos 0: 1*k[1]=2, pos 1: 1*k[2]=3, pos 2: 1*k[3]=4
	// Scatter from in[1]=2 at positions {2-1, 3-1, 4-1, 5-1} = {1, 2, 3, 4}
	//   pos 1: 2*k[0]=2, pos 2: 2*k[1]=4, pos 3: 2*k[2]=6
	// Result: {2, 3+2, 4+4, 6} = {2, 5, 8, 6}
	input := []float32{1, 2}
	kernel := []float32{1, 2, 3, 4}
	out := ConvTranspose1D(input, 1, 2, kernel, 4, 1, 2, 1, nil)
	if len(out) != 4 {
		t.Fatalf("ConvTranspose1D stride2 output len = %d, want 4", len(out))
	}
	want := []float32{2, 5, 8, 6}
	for i := range out {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("ConvTranspose1D_s2[%d] = %f, want %f", i, out[i], want[i])
		}
	}
}

func TestConvTranspose1D_MultiChannel(t *testing.T) {
	// input: [2, 1] inCh=2, inLen=1, values {3, 5}
	// kernel: [2, 1, 2] = {1, 2, 3, 4} (ic=0,oc=0: {1,2}; ic=1,oc=0: {3,4})
	// stride=1, padding=0, outCh=1
	// outLen = (1-1)*1 + 2 = 2
	// ic=0, i=0, val=3: out[0] += 3*1=3, out[1] += 3*2=6
	// ic=1, i=0, val=5: out[0] += 5*3=15, out[1] += 5*4=20
	// Result: {18, 26}
	input := []float32{3, 5}
	kernel := []float32{1, 2, 3, 4}
	out := ConvTranspose1D(input, 2, 1, kernel, 2, 1, 1, 0, nil)
	if len(out) != 2 {
		t.Fatalf("ConvTranspose1D multi-ch output len = %d, want 2", len(out))
	}
	want := []float32{18, 26}
	for i := range out {
		if math.Abs(float64(out[i]-want[i])) > 1e-6 {
			t.Errorf("ConvTranspose1D_mc[%d] = %f, want %f", i, out[i], want[i])
		}
	}
}

func TestGeneratePath(t *testing.T) {
	durations := []int{2, 3, 1}
	path, tMel := GeneratePath(durations)
	if tMel != 6 {
		t.Fatalf("tMel = %d, want 6", tMel)
	}
	// path is [6, 3]:
	// mel 0 → text 0: path[0*3+0]=1
	// mel 1 → text 0: path[1*3+0]=1
	// mel 2 → text 1: path[2*3+1]=1
	// mel 3 → text 1: path[3*3+1]=1
	// mel 4 → text 1: path[4*3+1]=1
	// mel 5 → text 2: path[5*3+2]=1
	expected := []struct{ mel, text int }{
		{0, 0}, {1, 0}, {2, 1}, {3, 1}, {4, 1}, {5, 2},
	}
	for _, e := range expected {
		v := path[e.mel*3+e.text]
		if v != 1.0 {
			t.Errorf("path[%d,%d] = %f, want 1", e.mel, e.text, v)
		}
	}
	// Check total ones = tMel
	var sum float32
	for _, v := range path {
		sum += v
	}
	if int(sum) != tMel {
		t.Errorf("total ones = %f, want %d", sum, tMel)
	}
}

func TestSequenceMask(t *testing.T) {
	mask := SequenceMask(3, 5)
	want := []float32{1, 1, 1, 0, 0}
	for i := range mask {
		if mask[i] != want[i] {
			t.Errorf("mask[%d] = %f, want %f", i, mask[i], want[i])
		}
	}
}
