# shine-mp3
This is a pure Go implementation of the [shine mp3 encoding library](https://github.com/toots/shine).

## Local patches (MaClaw fork)

This vendored copy diverges from upstream `braheezy/shine-mp3` v0.1.0:

- `pkg/mp3/layer3.go` `Write` chunks input as `samplesPerPass × channels`
  interleaved samples and zero-pads the tail frame. Upstream advanced the
  offset by `samplesPerPass × 2` regardless of channel count, which dropped
  half of mono input and could fault on stereo.
- `pkg/mp3/layer3.go` `NewEncoder` picks the highest table bitrate ≤ 128 kbps
  whose per-channel granule budget fits the 12-bit `part2_3_length` field
  (max 4095 bits). Upstream hardcoded 128 kbps; at e.g. 16 kHz mono the
  4504-bit budget was silently clamped to 4095, so every frame was ~409 bits
  short and the whole stream desynced (unplayable files).
- Slot-per-frame math uses integer arithmetic. The upstream float form rounds
  e.g. 112 kbps @ 16 kHz to 503.999…, leaving `FracSlotsPerFrame ≈ 1` and
  making every frame claim a phantom padding slot.
- `Encoder.Flush` was added and `Write` no longer goes through
  `encoding/binary`. `putBits` only emits full 32-bit words, so upstream
  streams end mid-frame (up to 3 bytes of the final frame missing; ffmpeg
  reports "invalid new backstep"). `Flush` drains the pending whole bytes so
  the stream ends exactly on the last frame's boundary; callers must call it
  once after the final `Write`.
- `pkg/mp3/layer3_test.go` was added upstream-standalone, including a frame
  walk that asserts the encoded stream is exactly tiled by valid MPEG frames
  for every supported sample rate and channel count.


> shine is a blazing fast mp3 encoding library implemented in fixed-point arithmetic. The library can thus be used to perform super fast mp3 encoding on architectures without a FPU, such as armel, etc.. It is also super fast on architectures with a FPU!

AFAIK, this is the only pure Go MP3 ***encoding*** library. It produces byte-identical binaries to the original Shine C library.

The code was originally developed in [this project](https://github.com/braheezy/goqoa).

## Usage
The `main.go` file has simple example of reading WAV file and encoding it to MP3. It all comes down to this:
```go
// Create the encoder with the sample rate and number of audio channels
mp3Encoder := NewEncoder(44100, 2)
// Assuming all your audio data is in []int16 slice called decodedData, write it to a file referenced by out
mp3Encoder.Write(out, decodedData)
```

## A Quick Comment on MP3 Encoders
There is essentially one actively maintained MP3 encoding library: [LAME MP3](https://lame.sourceforge.io/). If you want to encode audio files to MP3 using a programming language, you use a library that provides bindings to LAME, which means users of your software must have the LAME MP3 C library installed. You might be able to work around this producing a 100% statically compiled binary and include the LAME files. However, that might prove challenging on all platforms, like Windows.

I found about the Shine MP3 encoder from this [list of alternative encoders on the LAME website](https://lame.sourceforge.io/links.php#Alternatives).
