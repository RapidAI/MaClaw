package yolo

import (
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

// convProfile records per-layer timing.
var convTimings []convTiming
var convTimingEnabled atomic.Bool

type convTiming struct {
	Name    string
	OutC    int
	InC     int
	KH      int
	Stride  int
	Groups  int
	InH     int
	InW     int
	Elapsed time.Duration
}

func init() {
	// Hook into Conv2dBNSiLU.Forward to record timings
	origForward := (*Conv2dBNSiLU).Forward
	_ = origForward // can't monkey-patch in Go, use a different approach
}

// TestProfileForward runs forward with per-layer timing instrumentation.
func TestProfileForward(t *testing.T) {
	weightsPath := "weights/omniparser-v2.yolow"
	if _, err := os.Stat(weightsPath); os.IsNotExist(err) {
		t.Skip("weights not found")
	}

	model, err := LoadModel(weightsPath)
	if err != nil {
		t.Fatalf("LoadModel: %v", err)
	}

	input := NewTensor(1, 3, 640, 640)
	for i := range input.Data {
		input.Data[i] = 0.5
	}

	// Profile each backbone layer individually
	type layerProfile struct {
		name    string
		elapsed time.Duration
	}
	var profiles []layerProfile

	profileLayer := func(name string, fn func(*Tensor) *Tensor, x *Tensor) *Tensor {
		start := time.Now()
		out := fn(x)
		profiles = append(profiles, layerProfile{name, time.Since(start)})
		return out
	}

	// Backbone
	x := profileLayer("B0:Conv(3→64,s2)", model.B0.Forward, input)
	x = profileLayer("B1:Conv(64→128,s2)", model.B1.Forward, x)
	x = profileLayer("B2:C3k2(128→256)", model.B2.Forward, x)
	x = profileLayer("B3:Conv(256→256,s2)", model.B3.Forward, x)
	p3 := profileLayer("B4:C3k2(256→512)", model.B4.Forward, x)
	x = profileLayer("B5:Conv(512→512,s2)", model.B5.Forward, p3)
	p4 := profileLayer("B6:C3k2(512→512)", model.B6.Forward, x)
	x = profileLayer("B7:Conv(512→512,s2)", model.B7.Forward, p4)
	x = profileLayer("B8:C3k2(512→512)", model.B8.Forward, x)
	x = profileLayer("B9:SPPF(512→512)", model.B9.Forward, x)
	p5 := profileLayer("B10:C2PSA(512→512)", model.B10.Forward, x)

	// Neck
	up1 := p5.Upsample2x()
	cat1 := ConcatChannel(up1, p4)
	n3 := profileLayer("N13:C3k2(1024→512)", model.N13.Forward, cat1)
	up2 := n3.Upsample2x()
	cat2 := ConcatChannel(up2, p3)
	n4 := profileLayer("N16:C3k2(1024→256)", model.N16.Forward, cat2)
	down1 := profileLayer("N17:Conv(256→256,s2)", model.N17.Forward, n4)
	cat3 := ConcatChannel(down1, n3)
	n5 := profileLayer("N19:C3k2(768→512)", model.N19.Forward, cat3)
	down2 := profileLayer("N20:Conv(512→512,s2)", model.N20.Forward, n5)
	cat4 := ConcatChannel(down2, p5)
	_ = profileLayer("N22:C3k2(1024→512)", model.N22.Forward, cat4)

	// Sort by time descending
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].elapsed > profiles[j].elapsed
	})

	total := time.Duration(0)
	for _, p := range profiles {
		total += p.elapsed
	}

	t.Logf("\n=== Per-layer profile (total: %v) ===", total)
	for _, p := range profiles {
		pct := float64(p.elapsed) / float64(total) * 100
		t.Logf("  %6.1f%% %8v  %s", pct, p.elapsed.Round(time.Millisecond), p.name)
	}
}

func init() {
	_ = fmt.Sprintf
}
