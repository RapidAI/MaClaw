package main

import _ "embed"

// defaultHardwareWelcomeWAV is the built-in ESP32 boot greeting. Keeping it
// inside the executable makes a fresh install immediately playable without a
// TTS model download or a generated file from a previous installation.
//
//go:embed hello-maclaw.wav
var defaultHardwareWelcomeWAV []byte
