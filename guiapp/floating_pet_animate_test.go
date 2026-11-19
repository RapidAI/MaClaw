package guiapp

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestPetDesktopAnimatorPlaysRagdollAndWalks(t *testing.T) {
	prev := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(prev) })
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	var anim petDesktopAnimator
	anim.Configure(petAnimConfig{
		Skin:          petpack.DefaultPackID,
		Variant:       petpack.VariantDefault,
		State:         string(petpack.StateIdle),
		PixelSize:     96,
		WindowSize:    256,
		MotionEnabled: true,
	})
	start := time.UnixMilli(1_700_000_000_000)
	first := anim.Render(start)
	later := anim.Render(start.Add(1200 * time.Millisecond))
	if first == nil || later == nil {
		t.Fatal("ragdoll frame missing")
	}
	if bytes.Equal(first.Pix, later.Pix) {
		t.Fatal("ragdoll idle pose did not change")
	}
	anim.SetPos(20, 30)
	cursor := start
	x, y := 20, 30
	roamed := false
	// The seeded idle sequence can spend a while on non-travel behaviors
	// before a walking clip comes up; keep roaming until one does.
	for i := 0; i < 300; i++ {
		cursor = cursor.Add(200 * time.Millisecond)
		if anim.Render(cursor) == nil {
			t.Fatal("ragdoll frame dropped during roam")
		}
		var moved bool
		x, y, moved = anim.Advance(cursor, false, 2000, 2000)
		if moved {
			roamed = true
			break
		}
	}
	if !roamed {
		t.Fatal("ragdoll never left its start position")
	}
	anim.Configure(petAnimConfig{
		Skin:          petpack.DefaultPackID,
		Variant:       petpack.VariantDefault,
		State:         string(petpack.StateIdle),
		PixelSize:     96,
		WindowSize:    256,
		MotionEnabled: true,
		Quiet:         true,
	})
	qx, qy, qMoved := anim.Advance(start.Add(time.Second), false, 800, 600)
	if qMoved || qx != x || qy != y {
		t.Fatalf("quiet mode moved to (%d,%d) from (%d,%d)", qx, qy, x, y)
	}
	level, reason := anim.RuntimeLevel(petpack.RendererCharacter)
	if level != petpack.RendererNative || reason == "" {
		t.Fatalf("quiet level=%q reason=%q", level, reason)
	}
	anim.Configure(petAnimConfig{
		Skin:          petpack.DefaultPackID,
		Variant:       petpack.VariantDefault,
		State:         string(petpack.StateThinking),
		PixelSize:     96,
		WindowSize:    256,
		MotionEnabled: true,
	})
	thinkAt := cursor
	for i := 0; i < 20; i++ {
		thinkAt = thinkAt.Add(200 * time.Millisecond)
		if anim.Render(thinkAt) == nil {
			t.Fatal("thinking frame dropped")
		}
		tx, ty, moved := anim.Advance(thinkAt, false, 2000, 2000)
		if moved || tx != x || ty != y {
			t.Fatalf("thinking moved to (%d,%d) from (%d,%d)", tx, ty, x, y)
		}
	}
}

func TestPetDesktopAnimatorReusesStaticFrame(t *testing.T) {
	prev := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(prev) })
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	var anim petDesktopAnimator
	anim.Configure(petAnimConfig{
		Skin:          petpack.DefaultPackID,
		Variant:       petpack.VariantDefault,
		State:         string(petpack.StateIdle),
		PixelSize:     96,
		WindowSize:    256,
		MotionEnabled: true,
		Quiet:         true,
	})
	now := time.UnixMilli(1_700_000_000_000)
	first := anim.Render(now)
	second := anim.Render(now.Add(time.Second))
	if first == nil || first != second {
		t.Fatal("quiet pose was rebuilt")
	}
}

func TestDarwinRenderScaleCapsRetina(t *testing.T) {
	if darwinRenderScale(0) != 1 || darwinRenderScale(1) != 1 || darwinRenderScale(2) != 2 || darwinRenderScale(3) != 2 || darwinRenderScale(4) != 2 {
		t.Fatalf("scale 0=%d 1=%d 2=%d 3=%d 4=%d", darwinRenderScale(0), darwinRenderScale(1), darwinRenderScale(2), darwinRenderScale(3), darwinRenderScale(4))
	}
}

func TestPetFrameSigDetectsPoseChanges(t *testing.T) {
	if petFrameSig(nil) != 0 || petFrameSig([]byte{}) != 0 {
		t.Fatal("empty signature")
	}
	base := bytes.Repeat([]byte{1, 2, 3, 4}, 80)
	same := append([]byte(nil), base...)
	if petFrameSig(base) != petFrameSig(same) {
		t.Fatal("signature changed for an identical buffer")
	}
	same[0] ^= 0xff
	if petFrameSig(base) == petFrameSig(same) {
		t.Fatal("first byte was ignored")
	}
	mid := append([]byte(nil), base...)
	mid[1] ^= 0xff
	if petFrameSig(base) == petFrameSig(mid) {
		t.Fatal("off-grid byte was ignored")
	}
	stride := append([]byte(nil), base...)
	stride[64] ^= 0xff
	if petFrameSig(base) == petFrameSig(stride) {
		t.Fatal("later byte was ignored")
	}
	longer := append(append([]byte(nil), base...), 9)
	if petFrameSig(base) == petFrameSig(longer) {
		t.Fatal("length was ignored")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	pix, w, h, strideBytes, ok := nrgbaBytes(img)
	if !ok || w != 2 || h != 2 || strideBytes < 8 || len(pix) < 8 {
		t.Fatalf("nrgba view = %d bytes %dx%d stride %d ok %v", len(pix), w, h, strideBytes, ok)
	}
	if _, _, _, _, ok := nrgbaBytes(nil); ok {
		t.Fatal("nil frame reported bytes")
	}
}

func TestPetDesktopAnimatorKeepsChosenPositionWhileDragging(t *testing.T) {
	var anim petDesktopAnimator
	anim.Configure(petAnimConfig{Skin: "ragdoll", MotionEnabled: true, WindowSize: 256, PixelSize: 64})
	anim.SetPos(40, 50)
	now := time.UnixMilli(1_700_000_000_000)
	anim.Advance(now, true, 800, 600)
	x, y, moved := anim.Advance(now.Add(500*time.Millisecond), true, 800, 600)
	if moved || x != 40 || y != 50 {
		t.Fatalf("drag position = (%d,%d) moved=%v", x, y, moved)
	}
}

func TestPetScreenTravelVelocityDropsWhenLocked(t *testing.T) {
	vx, vy := petScreenTravelVelocity(1.5, -0.4, true)
	if vx != 0 || vy != 0 {
		t.Fatalf("locked velocity = (%v,%v)", vx, vy)
	}
	vx, vy = petScreenTravelVelocity(1.5, -0.4, false)
	if vx != 1.5 || vy != -0.4 {
		t.Fatalf("unlocked velocity = (%v,%v)", vx, vy)
	}
}

func TestPetDesktopAnimatorStaysPutWhilePositionLocked(t *testing.T) {
	prev := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(prev) })
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	var anim petDesktopAnimator
	anim.Configure(petAnimConfig{
		Skin:           petpack.DefaultPackID,
		Variant:        petpack.VariantDefault,
		State:          string(petpack.StateIdle),
		PixelSize:      96,
		WindowSize:     256,
		MotionEnabled:  true,
		PositionLocked: true,
	})
	start := time.UnixMilli(1_700_000_000_000)
	first := anim.Render(start)
	later := anim.Render(start.Add(1200 * time.Millisecond))
	if first == nil || later == nil {
		t.Fatal("pinned ragdoll frame missing")
	}
	if bytes.Equal(first.Pix, later.Pix) {
		t.Fatal("pinned ragdoll idle pose did not change")
	}
	anim.SetPos(20, 30)
	cursor := start
	for i := 0; i < 40; i++ {
		cursor = cursor.Add(200 * time.Millisecond)
		x, y, moved := anim.Advance(cursor, false, 2000, 2000)
		if moved || x != 20 || y != 30 {
			t.Fatalf("pinned pet moved to (%d,%d) moved=%v", x, y, moved)
		}
	}
	anim.Configure(petAnimConfig{
		Skin:          petpack.DefaultPackID,
		Variant:       petpack.VariantDefault,
		State:         string(petpack.StateIdle),
		PixelSize:     96,
		WindowSize:    256,
		MotionEnabled: true,
	})
	roamed := false
	for i := 0; i < 300; i++ {
		cursor = cursor.Add(200 * time.Millisecond)
		if anim.Render(cursor) == nil {
			t.Fatal("unpinned ragdoll frame dropped")
		}
		if _, _, moved := anim.Advance(cursor, false, 2000, 2000); moved {
			roamed = true
			break
		}
	}
	if !roamed {
		t.Fatal("unlock did not restore roaming")
	}
}
