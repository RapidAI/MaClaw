# MaClaw Desktop Pet System Design

## 1. Goal

MaClaw comes from `Ma + Claw`, so the floating AI entry can grow into a desktop companion that catches problems, grabs context, and pulls out the important parts for the user. The pet system should keep the current floating button's low-friction behavior while adding a visible character, motion states, and lightweight voice/text interaction.

## 2. Product Positioning

MaClaw Pet is a desktop AI companion with four jobs:

- Entry: click, drag, right-click, file drop, and voice entry for the AI assistant.
- Feedback: show listening, thinking, speaking, done, and alert states through motion and small bubbles.
- Conversation: use existing ASR, LLM/Agent, and TTS capabilities for simple turn-based or continuous dialogue.
- Personalization: support skin, size, motion intensity, readback behavior, and quiet mode.

## 3. Character Direction

Default character: `ClawMate`.

ClawMate keeps the round MaClaw assistant recognition but adds a small claw-like body and tool-companion feeling. It should feel professional, useful, and slightly alive, without becoming childish or noisy.

Initial skins:

- `ClawMate`: default official mechanical claw companion.
- `Mini Claw`: compact skin close to the current floating button.
- `Dev Claw`: developer-focused style with coding cues.
- `Focus Claw`: low-distraction form for quiet work.

## 4. Voice Conversation Layer

MaClaw already has ASR and TTS, so the pet should be designed as a conversation layer over existing capabilities instead of a separate voice stack.

Runtime chain:

```text
idle
  -> user click / hold / shortcut
  -> ASR listening
  -> ASR transcript ready
  -> LLM or Agent thinking
  -> TTS readback, if enabled
  -> done or return to listening
```

Conversation modes:

- `text-first`: default. The pet opens text chat and uses voice only when the user explicitly starts voice input.
- `voice-turn`: one spoken user turn produces one spoken/text answer.
- `continuous`: after TTS finishes, the pet returns to listening until timeout or user stops it.

Readback modes:

- `off`: never speak automatically.
- `summary`: speak a short summary, keep full answer in the panel.
- `full`: speak the full answer.
- `done-only`: only announce task completion and ask the user to view the panel.

No-hear behavior:

- If ASR confidence is low or no transcript arrives, the pet can show a short prompt and optionally ask again.
- Continuous mode should always have a visible timeout and a one-click stop.

## 5. Motion States

| State | Trigger | Motion |
| --- | --- | --- |
| idle | no active task | subtle breathing and blink |
| listen | ASR recording | eyes or antenna light up, claw moves lightly |
| transcribe | ASR processing | scanning pulse |
| thinking | LLM/Agent active | claw catches small light dots |
| speaking | TTS playing | mouth/body follows voice pulse |
| done | task complete | claw hands over a bubble/card |
| alert | confirmation or risk | serious expression and warning color |
| quiet | do-not-disturb | edge-hugging compact state |

## 6. Settings Tab

Add a `Pet` tab in MaClaw settings. MVP settings:

- Enable pet entry.
- Skin: ClawMate, Mini Claw, Dev Claw, Focus Claw.
- Size: 56-120px.
- Motion animation.
- Text chat.
- Voice input.
- Voice readback.
- File drop.
- Interaction style: quiet, balanced, active.
- Conversation mode: text-first, voice-turn, continuous.
- Readback mode: off, summary, full, done-only.
- Continuous conversation timeout.
- Ask again when speech is unclear.
- Do-not-disturb mode.

## 7. Technical Plan

Config fields are stored in `corelib.AppConfig` and persisted through existing `LoadConfig` / `SaveConfig`:

- `pet_enabled`
- `pet_skin`
- `pet_size`
- `pet_motion_enabled`
- `pet_text_interaction_enabled`
- `pet_voice_input_enabled`
- `pet_voice_readback_enabled`
- `pet_file_drop_enabled`
- `pet_interaction_mode`
- `pet_conversation_mode`
- `pet_readback_mode`
- `pet_auto_retry_on_no_hear`
- `pet_continuous_timeout_sec`
- `pet_quiet_mode`

Existing voice-related fields remain the source of truth for actual voice capability availability:

- `asr_enabled`
- `tts_enabled`
- `tts_voice_id`
- `audio_input_device_id`
- `audio_output_device_id`

## 8. Implementation Phases

Phase 1, done in this slice:

- Product/technical design document.
- ClawMate concept SVG asset.
- Pet settings tab.
- Pet config fields.
- Voice conversation settings surfaced in the pet tab.

Phase 2:

- Make `FloatingButton.tsx` read pet config.
- Render ClawMate instead of the round logo when `pet_enabled` is true.
- Resize the floating window using `pet_size`.
- Bind AI/ASR/TTS events to pet state.

Phase 3:

- File drop into pet.
- Continuous voice loop with timeout and stop affordance.
- Skin package directory and install/update mechanism.
