# MaClaw Pet Engine — Multi-part Join v1

**Status:** implemented in `gui/petpack` (desktop rig renderer)  
**Goal:** reduce neck “plug”, hard seams, and multi-face glitches for `body + head_*` paper-doll packs without requiring every pack to be re-authored.

## Background

Performance packs (`native-character`) composite:

1. `rig/body.png` (should be headless / collar-flush)
2. `rig/head_*.png` expression heads on a shared `head` bone

Common failure modes:

| Failure | Cause |
|--------|--------|
| Exposed neck plug | Head has a long flesh stump; head draws above body |
| Double face | Body still contains a face, or all expression heads draw at alpha=1 |
| State-only seam | Different heads have different chin lengths / pivots |

## Engine behavior (v1)

### 1. Expression alpha on **bone tracks** (bugfix)

Authoring convention puts expression alpha on bone names (`h_idle`, `h_speak`, …), while slots are named `slot_h_idle`.

The renderer now multiplies **both** slot-name and bone-name track alphas when drawing a slot. Fully hidden heads (`alpha < 0.02`) are skipped.

### 2. Optional `join` block on the rig JSON

```json
{
  "version": 1,
  "join": {
    "auto": true,
    "collar_cover": true,
    "collar_cover_frac": 0.18,
    "collar_overlay": "rig/collar_overlay.png",
    "head_neck_fade_px": 10,
    "head_neck_fade_center": 0.42,
    "chin_inset_px": 12,
    "state_head_offset": {
      "listening": { "y": 2 },
      "alert": { "y": 1 }
    }
  },
  "bones": [ ... ],
  "slots": [ ... ],
  "clips": { ... }
}
```

| Field | Meaning | Default (multi-head packs) |
|-------|---------|----------------------------|
| `auto` | Master switch | `true` if ≥2 expression-head slots |
| `collar_cover` | After body+heads, re-draw upper body (or overlay) on top | `true` |
| `collar_cover_frac` | Fraction of **body content height** re-covered from the top | `0.18` (range 0.05–0.40) |
| `collar_overlay` | Optional allowlisted texture drawn last at body bone | none |
| `head_neck_fade_px` | Soft-clear center-bottom of each `head_*` texture | `10` (0–48) |
| `head_neck_fade_center` | Horizontal fraction treated as “neck center” | `0.42` |
| `chin_inset_px` | Documented authored tuck (tools / future use) | `0` |
| `state_head_offset` | Per runtime state head-bone Δx/Δy (design px, ±32) | none |

**Non multi-head packs** (e.g. ClawMate shell/claws/eyes) leave join **off** unless `join.auto: true` is forced.

### 3. Multi-head detection

A pack is multi-head when:

- ≥2 slots look like expression heads (`head_*.png`, `h_*` / `slot_h_*`), or
- hair + face layout: a `hair` slot plus ≥2 `face_*` / `f_*` / `slot_f_*` slots

Body-only clips (no expression alpha tracks) draw **idle** face/head only so
empty `trackAlpha` defaults do not stack every expression layer.

### 4. Collar cover algorithm

1. Composite slots as usual (body then heads by z).
2. If collar cover is on:
   - Prefer `collar_overlay` texture at the **body** world transform, else
   - Build a same-size copy of `body.png` with only the **top content strip** opaque (soft feather at strip bottom), draw it again at the body transform.

Clothing therefore paints over residual neck plugs without cutting side hair on the head layer.

### 5. Head neck fade

At load time, each expression-head texture is copied and the **center-bottom** band is soft-faded (sides preserved for long hair). This shrinks “neck plug” geometry before any frame is drawn.

## Compatibility

- Existing packs without `join` keep working.
- Multi-head packs get free collar cover + mild neck fade.
- ClawMate-style packs are unchanged (not multi-head).
- Budgets / security model unchanged (no scripts, allowlisted textures only).
- If `collar_overlay` is set, it **must** appear in `assets.rig.textures`.

## Pack authoring tips (engine-aware)

1. Keep body **collar-flush** (no face on body).
2. Keep head chin short; rely on collar cover for remaining seam.
3. Prefer `collar_overlay` for difficult clothing (collars, turtlenecks).
4. Use `state_head_offset` when one expression’s chin is longer (e.g. open mouth).
5. Always put expression alpha tracks on the **bone** name (`h_idle`) **or** the slot name (`slot_h_idle`); both are now honored.

## Tests

```bash
cd gui/petpack
go test -count=1 .
```

Key cases:

- `TestRigJoinValidation`
- `TestMultiHeadCollarCoverHidesNeckPlug`
- `TestClawmateStylePackDoesNotForceJoin`

## hair + face layout (supported)

Multi-part detection also treats packs with:

- `rig/hair.png` (slot `hair` / `slot_hair`), and
- ≥2 `rig/face_*.png` (bones `f_idle`… / slots `slot_f_*`)

as join-enabled. Recommended z-order:

```text
body (z=1) → hair (z=2, fixed hole) → face_* (z=3, expression only)
```

Expression alpha tracks live on `f_idle`… (same bone-track alpha fix as `h_*`).

Example pack (assets + zip): `maclaw-pet/demo-hair-face/`  
Rebuild: `python maclaw-pet/_refs/build_demo_hair_face.py`

## Settings preview

`Registry.LoadStateFrameBytes` (settings “实时预览”) prefers:

1. Live `CharacterRenderer` / `RigRenderer` frame (Join v1 applied)
2. Declared `native/<state>.png` raster fallback

So collar cover and head neck fade are visible in settings, not only on the floating pet.

## Follow-ups

- Runtime `collar_mask` stencil (destination-in)
- Production validator: chin-Y equality + body-face residual scan
- Optional `hair_front` / `hair_back` split for bangs over face
- Cache live preview renders per pack/state when settings panel polls rapidly
