# CAM++ meeting diarization (Go-only CPU runtime)

The local diarization pipeline is:

```text
16 kHz mono PCM -> Silero VAD -> 1.5 s / 0.75 s CAM++ windows
-> 192-dimensional embeddings -> cosine agglomerative clustering
-> speaker time segments -> per-segment SenseVoice ASR
```

It identifies **when each local speaker is active**, so an application can
transcribe each speaker's spans separately. It is not blind source separation:
overlapping speakers are not split into independent clean audio tracks.

## Provision the model

The Go runtime does not load PyTorch pickle files. Convert the official
Apache-2.0 checkpoint once and place the generated file in the app model
directory as `campplus-cn-common.cmpg`:

```powershell
hf download funasr/campplus campplus_cn_common.bin --local-dir .\campplus
python .\RapidSpeech.cpp\scripts\convert_campplus.py `
  --checkpoint .\campplus\campplus_cn_common.bin `
  --output .\models\campplus-cn-common.cmpg
```

The converter needs Python and PyTorch only at build/provision time. End-user
inference is pure Go and needs neither Python, CUDA nor ONNX Runtime.

## Use from Go

```go
pcm, err := asr.WAVToFloat32(wavBytes) // 16 kHz mono float32
if err != nil { /* handle */ }

model, err := diarization.LoadCAMPlus("models/campplus-cn-common.cmpg")
if err != nil { /* handle */ }

segments, err := diarization.Diarize(pcm, model, diarization.Config{
    KnownSpeakers: 4, // optional; improves a meeting with known attendee count
})
// Segment{Start, End, Speaker}; call ASR on pcm[Start:End] per segment.
```

`Speaker` is local to one recording. If named, cross-meeting identities are
needed, enrol each person with a reference embedding and map the resulting
cluster centroids after diarization.

## Validation

Use the official model and a WAV fixture for the optional live test:

```powershell
$env:CAMPLUS_TEST_MODEL = "$PWD\models\campplus-cn-common.cmpg"
$env:CAMPLUS_TEST_WAV = "$PWD\meeting.wav"
go test ./corelib/diarization -run TestLoadCAMPlusAndEmbedOptionalModel
```
