# MaClaw Pet Performance System Design

## Status

Pet Pack 3.0 is implemented by the desktop registry and Pet Store archive validator. A client advertises support with `pet-performance-v3`; agents must still generate a `schema_version: 2` / `native-skeleton` pack for targets that do not advertise that capability.

Runtime integration status (2026-08): all seven states are emitted by the desktop (AI flow emits `thinking`/`speaking`/`done`; voice flow emits `listening`/`thinking`/`speaking`; confirmation and sensitive-authorization entries emit `alert`). All eight events have real triggers: `click`/`hover`/`drag_start`/`drag_end` from the native window, `task_started`/`task_done`/`task_failed` from state transitions, and `long_idle` from a five-minute inactivity detector in the native window. The settings page shows the declared/effective renderer level and the degradation reason. macOS and Linux remain static-fallback only (sound, motion, and runtime-state bridges are stubs; the settings UI hides those options on non-Windows hosts). The desktop Pet Store client talks to HubCenter directly; there is no hub proxy.

## Product outcome

The pet is a compact desktop character, not a status icon. It should appear to notice activity, form an intention, respond to direct interaction, and settle naturally. The system must support animals, objects, people, and other figurative subjects. Complexity belongs in the AI-agent-authored package; users select a character and a motion level.

## Architecture

```text
runtime state + local interaction event
  -> performance director (mood, behavior choice, cooldown, interrupt policy)
  -> state transition and layer mixer
  -> body / expression / gaze / secondary-motion clips
  -> declarative rig renderer
  -> static native fallback
```

The director receives only semantic states and allowlisted local events. It never receives screen pixels, application content, raw microphone audio, or a network capability.

## Pack levels

| Level | Renderer | Contract |
| --- | --- | --- |
| Static | `native-raster` | Per-state raster frames |
| Motion | `native-skeleton` | 2.0 bone/slot JSON and raster textures |
| Performance | `native-character` | 3.0 performer JSON over the same restricted rig |

The compatibility chain is fixed: `native-character` → `native-skeleton` → `native/idle.png`. A failed pet must never block the desktop entry point.

## Pet Performance Pack 3.0

`pet-pack.yaml` adds `assets.character.definition`, referencing `character/performer.json`, and declares `capabilities.pet_performance_v3: true`. The package keeps the existing local `assets.rig.definition`, texture allowlist, and static `assets.native.idle` fallback. No pack code is executable.

`performer.json` version 1 contains:

- `moods`: `calm`, `curious`, `focused`, `pleased`, `concerned`, `tired`.
- `layers`: `body`, `expression`, `gaze`, `secondary`.
- `behaviors`: idle activity records with `enter`, `loop`, `exit`, weighted selection, duration range, cooldown, and optional moods.
- `states`: semantic runtime state mappings. A state picks a behavior pool or an `enter` / `loop` / `exit` sequence plus expression, gaze, and secondary-motion clips.
- `events`: short reactions to allowlisted local events, with cooldown and interrupt policy.
- `reactions`: at least 12 weighted visual variants bound to those same eight events. They add variety without adding any new host input or privilege.
- `rules`: `no_repeat_last`, `crossfade_ms`, and `max_interrupt_ms`.

Every clip reference must resolve to a declared clip in the restricted rig. The schema rejects unknown state, event, layer, gaze, or interrupt names. It also rejects invalid durations, non-positive weights, invalid min/max ranges, missing idle fallback, fewer than six expression clips or four gaze clips, and assets outside the manifest allowlist.

## Character quality floor

The official sample character is one concrete, friendly, recognizable desktop character. It is a quality reference, not a catalog: users create all other packs with AI agents.

Each performance pack must include:

- at least 10 idle behaviors with weighting, duration ranges, cooldowns, and repeat avoidance;
- `enter`, `loop`, and `exit` for primary active states;
- at least 12 event reactions;
- at least 6 expression clips and 4 gaze clips;
- parallel body, expression, gaze, and secondary-motion layers;
- transitions for task start, thinking, speaking, completion, failure, click, drag start/end, and long idle.

At 56px, 88px, and 120px, the character must remain identifiable. Reduced-motion mode uses static idle plus only non-continuous state changes.

## Natural motion contract

Rich does not mean rapid or constant movement. Every significant motion has five stages: intent, anticipation, execution, hold, and follow-through. The package author (normally an AI agent) must apply these rules:

- Significant actions begin with 100–300ms of opposing anticipation and end with 120–500ms of follow-through and settling.
- Body, expression, gaze, and secondary layers use offset timing; at least two parts must move differently during an action. The body leads, while head and gaze normally follow.
- Secondary elements (ears, tail, pendant, fabric, cable, or equivalent) follow the primary movement by 80–280ms and settle at a smaller amplitude.
- Loop clips have matching position and velocity at their seam, plus at least one non-uniform timing change within every long loop.
- Idle pools include breath, natural blink, observation, weight shift, brief distraction, stretch, pose adjustment, rest, return-to-attention, and environment response. The director excludes the last three chosen idle behaviors.
- Expression changes precede or overlap body movement; whole-character rotation and scale cannot substitute for expression.
- Pure pendulum translation, fully synchronized bone tracks, constant-frequency jitter, instant pose jumps, and immediate return-to-zero are invalid performance output.

Recommended timing is 80–220ms for micro-expression, 180–450ms for gaze shifts, 350–900ms for posture changes, 600–1800ms for reactions, and 2.5–10 seconds for idle behaviors. These are quality constraints for agent-generated packages, not user-facing authoring work.

## Runtime behavior

The runtime state vocabulary remains `idle`, `listening`, `thinking`, `speaking`, `done`, `alert`, and `quiet`. The event vocabulary is limited to `click`, `hover`, `drag_start`, `drag_end`, `task_started`, `task_done`, `task_failed`, and `long_idle`.

State changes are not hard cuts: the director plays the current action's authored `exit`, then blends over `crossfade_ms` into the target entry and loop. Events choose either `soft` (immediate, interruptible reaction) or `after_loop` (wait for the current loop boundary, then play the current exit). An `after_loop` event must wait no longer than `max_interrupt_ms` (80–2000 ms); if no natural boundary is observed in time, the director begins the authored exit at that deadline. For each allowlisted event, the director chooses a weighted `reactions` variant, respects its cooldown, and never expands the fixed event vocabulary. The idle director excludes cooled-down behaviors and the most recently played configured number of behaviors. The idle pool must contain more distinct behaviors than `no_repeat_last`, so repeat avoidance remains achievable even when cooldown is relaxed. Idle uses its declared mood, or `calm` when omitted; validation requires at least one compatible idle behavior so a package can always settle naturally.

## Security and resource limits

All packs are declarative local resources: PNG/WebP, YAML, JSON, TXT, MD, and SVG only. JavaScript, WASM, binaries, remote URLs, shaders, callbacks, and any code execution are forbidden.

- 24 bones, 32 slots, 32 textures, 240 keyframes per track, and 1,200 total keyframes.
- Images and static fallback frames: 512 KiB and 1024×1024 each; total texture pixels: 4,194,304.
- ZIP file: 3 MiB maximum; unpacked resources: 2 MiB maximum to prevent compression bombs; 64 files maximum.
- A single file is at most 512 KiB; path length is at most 180 characters; duplicate and unsafe paths are rejected.

## Implementation sequence

1. Define Go schema types and deterministic validation for performer JSON.
2. Add capability negotiation and retain 2.0 compatibility output.
3. Build the director, layer mixer, transition rules, reduced-motion path, and fallback path.
4. Create one official sample character that exercises the full quality floor.
5. Add desktop, Pet Store, and agent-side validation parity tests.
6. Keep the desktop and Pet Store validators in parity as the format evolves.
