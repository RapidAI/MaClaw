#pragma once

// Single source of truth for the audio pipeline sample rate. board_port.c
// (capture/playback) and main.c (meeting WAV headers, Hub capability
// declaration) previously defined their own copies of this constant, which
// could silently diverge.
#define AUDIO_RATE 16000

// Alias kept for the meeting-recording code paths in main.c.
#define MEETING_SAMPLE_RATE AUDIO_RATE
