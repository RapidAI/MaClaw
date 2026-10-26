package vk

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// TestDeviceEnumerate reports what the Vulkan loader exposes on this machine.
// It skips (not fails) when no Vulkan device exists, so the suite still passes
// on CI boxes without a GPU driver.
func TestDeviceEnumerate(t *testing.T) {
	d, err := NewDevice()
	if err != nil {
		t.Skipf("no Vulkan device: %v", err)
	}
	defer d.Close()
	t.Logf("device=%q type=%d api=%d.%d.%d cpu=%v memSize=%dMB",
		d.Info.Name, d.Info.Type,
		d.Info.APIVersion>>22, (d.Info.APIVersion>>12)&0x3ff, d.Info.APIVersion&0xfff,
		d.Info.IsCPU, d.memSize>>20)
	if d.Info.Name == "" {
		t.Fatal("device name is empty")
	}
}

// TestBufferRoundTrip allocates a host-visible buffer and checks the mapped
// memory is readable and writable (the shared-memory model the forward relies on).
func TestBufferRoundTrip(t *testing.T) {
	d, err := NewDevice()
	if err != nil {
		t.Skipf("no Vulkan device: %v", err)
	}
	defer d.Close()
	b, err := d.NewBuffer(4096)
	if err != nil {
		t.Fatalf("NewBuffer: %v", err)
	}
	defer b.Release()
	if len(b.Bytes()) != 4096 {
		t.Fatalf("Bytes len = %d, want 4096", len(b.Bytes()))
	}
	f := b.Float32s()
	for i := range f {
		f[i] = float32(i) * 0.5
	}
	for i := range f {
		if want := float32(i) * 0.5; f[i] != want {
			t.Fatalf("f[%d] = %v, want %v", i, f[i], want)
		}
	}
	i32 := b.Int32s()
	if len(i32) != 1024 {
		t.Fatalf("Int32s len = %d, want 1024", len(i32))
	}
}

// tinySPIRV is a compute shader (compiled offline with glslang) that does
// out[i] = in[i] * 2.0 for 256-wide groups. It validates the whole pipeline
// path: module, layout, descriptor set, dispatch, barrier, submit, fence.
var tinySPIRV = mustHex(`
07230203 00010000 0008000b 00000026 00000000 00020011 00000001 0006000b
00000001 4c534c47 6474732e 3035342e 00000000 0003000e 00000000 00000001
0006000f 00000005 00000004 6e69616d 00000000 0000000b 00060010 00000004
00000011 00000100 00000001 00000001 00030003 00000002 000001c2 00040005
00000004 6e69616d 00000000 00030005 00000008 00000069 00080005 0000000b
475f6c67 61626f6c 766e496c 7461636f 496e6f69 00000044 00030005 00000012
0074754f 00040006 00000012 00000000 00000062 00030005 00000014 00000000
00030005 00000019 00006e49 00040006 00000019 00000000 00000061 00030005
0000001b 00000000 00040047 0000000b 0000000b 0000001c 00040047 00000011
00000006 00000004 00030047 00000012 00000003 00040048 00000012 00000000
00000019 00050048 00000012 00000000 00000023 00000000 00030047 00000014
00000019 00040047 00000014 00000021 00000001 00040047 00000014 00000022
00000000 00040047 00000018 00000006 00000004 00030047 00000019 00000003
00040048 00000019 00000000 00000018 00050048 00000019 00000000 00000023
00000000 00030047 0000001b 00000018 00040047 0000001b 00000021 00000000
00040047 0000001b 00000022 00000000 00040047 00000025 0000000b 00000019
00020013 00000002 00030021 00000003 00000002 00040015 00000006 00000020
00000000 00040020 00000007 00000007 00000006 00040017 00000009 00000006
00000003 00040020 0000000a 00000001 00000009 0004003b 0000000a 0000000b
00000001 0004002b 00000006 0000000c 00000000 00040020 0000000d 00000001
00000006 00030016 00000010 00000020 0003001d 00000011 00000010 0003001e
00000012 00000011 00040020 00000013 00000002 00000012 0004003b 00000013
00000014 00000002 00040015 00000015 00000020 00000001 0004002b 00000015
00000016 00000000 0003001d 00000018 00000010 0003001e 00000019 00000018
00040020 0000001a 00000002 00000019 0004003b 0000001a 0000001b 00000002
00040020 0000001d 00000002 00000010 0004002b 00000010 00000020 40000000
0004002b 00000006 00000023 00000100 0004002b 00000006 00000024 00000001
0006002c 00000009 00000025 00000023 00000024 00000024 00050036 00000002
00000004 00000000 00000003 000200f8 00000005 0004003b 00000007 00000008
00000007 00050041 0000000d 0000000e 0000000b 0000000c 0004003d 00000006
0000000f 0000000e 0003003e 00000008 0000000f 0004003d 00000006 00000017
00000008 0004003d 00000006 0000001c 00000008 00060041 0000001d 0000001e
0000001b 00000016 0000001c 0004003d 00000010 0000001f 0000001e 00050085
00000010 00000021 0000001f 00000020 00060041 0000001d 00000022 00000014
00000016 00000017 0003003e 00000022 00000021 000100fd 00010038
`)

// mustHex parses whitespace-separated 32-bit hex words into SPIR-V bytes
// (little-endian, matching the SPIR-V binary format).
func mustHex(s string) []byte {
	fields := splitFields(s)
	out := make([]byte, 0, len(fields)*4)
	for _, f := range fields {
		var v uint32
		if _, err := fmt.Sscanf(f, "%x", &v); err != nil {
			panic(fmt.Sprintf("mustHex: bad word %q: %v", f, err))
		}
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		out = append(out, b[:]...)
	}
	return out
}

func splitFields(s string) []string {
	var f []string
	cur := ""
	for _, c := range s {
		if c == ' ' || c == '\n' || c == '\t' || c == '\r' {
			if cur != "" {
				f = append(f, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		f = append(f, cur)
	}
	return f
}

// TestComputeDispatch runs a real shader end-to-end: upload, dispatch,
// barrier, read back. This is the test that proves the binding actually
// computes rather than merely loading.
func TestComputeDispatch(t *testing.T) {
	d, err := NewDevice()
	if err != nil {
		t.Skipf("no Vulkan device: %v", err)
	}
	defer d.Close()
	p, err := d.Compile(tinySPIRV, 2, 0)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer p.Release()

	const n = 1024
	in, err := d.NewBuffer(n * 4)
	if err != nil {
		t.Fatalf("NewBuffer(in): %v", err)
	}
	defer in.Release()
	out, err := d.NewBuffer(n * 4)
	if err != nil {
		t.Fatalf("NewBuffer(out): %v", err)
	}
	defer out.Release()

	src := in.Float32s()
	for i := range src {
		src[i] = float32(i)
	}
	// Zero the destination so a missing dispatch is detectable.
	for i := range out.Float32s() {
		out.Float32s()[i] = -1
	}

	p.Bind(in, out)
	e := d.Begin()
	e.SetPipeline(p)
	e.Dispatch(n/256, 1, 1, 256, 1, 1)
	e.Barrier()
	if err := e.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	got := out.Float32s()
	for i := 0; i < n; i++ {
		if want := float32(i) * 2; got[i] != want {
			t.Fatalf("out[%d] = %v, want %v (dispatch did not run correctly)", i, got[i], want)
		}
	}
	t.Logf("dispatch OK: %d elements doubled on %s", n, d.Info.Name)
}

// TestDeviceCloseIdempotent guards the shared-instance refcount: closing one
// device twice must not destroy the instance while another device is alive.
func TestDeviceCloseIdempotent(t *testing.T) {
	d1, err := NewDevice()
	if err != nil {
		t.Skipf("no Vulkan device: %v", err)
	}
	d2, err := NewDevice()
	if err != nil {
		d1.Close()
		t.Skipf("second device unavailable: %v", err)
	}
	// Closing d1 twice must not drop d2's instance.
	d1.Close()
	d1.Close()
	b, err := d2.NewBuffer(1024)
	if err != nil {
		t.Fatalf("buffer on the surviving device failed: %v", err)
	}
	b.Release()
	d2.Close()
	d2.Close()

	// The instance must be usable again after everything was closed.
	d3, err := NewDevice()
	if err != nil {
		t.Fatalf("re-open after full teardown: %v", err)
	}
	b3, err := d3.NewBuffer(256)
	if err != nil {
		t.Fatalf("buffer on re-opened device: %v", err)
	}
	b3.Release()
	d3.Close()
}

// TestDeviceMinHeapFilter verifies the heap-size filter rejects an impossible
// request with a diagnostic instead of failing later at allocation time.
func TestDeviceMinHeapFilter(t *testing.T) {
	// A petabyte cannot exist, so every device must be skipped.
	d, err := NewDeviceWithMinHeap(1 << 50)
	if err == nil {
		d.Close()
		t.Fatal("NewDeviceWithMinHeap(1<<50) succeeded, want a heap-size rejection")
	}
	t.Logf("rejected as expected: %v", err)

	// A tiny request must still work (this is the normal path).
	d2, err := NewDeviceWithMinHeap(1 << 20)
	if err != nil {
		t.Skipf("no Vulkan device: %v", err)
	}
	defer d2.Close()
	if d2.Info.Name == "" {
		t.Fatal("device name is empty")
	}
}
