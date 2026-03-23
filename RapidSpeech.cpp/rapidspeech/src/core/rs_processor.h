#pragma once

#include "rs_model.h"
#include "frontend/audio_processor.h"
#include "ggml-backend.h"
#include <deque>
#include <string>
#include <vector>
#include <memory>
#define SENSE_VOICE_MAX_GRAPH_SIZE 8196
/**
 * Simple thread-safe-ish circular buffer for raw PCM samples.
 */
class CircularBuffer {
public:
  void Push(const float* data, size_t size);
  std::vector<float> Pop(size_t size);
  size_t Size() const;
private:
  std::deque<float> buffer_;
};

/**
 * RSProcessor orchestrates the entire speech processing pipeline:
 * Audio Buffer -> Feature Extraction -> Model Encoding -> Model Decoding -> Text/Audio Output
 */
class RSProcessor {
public:
  /**
     * Constructor
     * @param model Shared pointer to the architecture-specific model
     * @param sched Backend scheduler used for inference
   */
  RSProcessor(std::shared_ptr<ISpeechModel> model, ggml_backend_sched_t sched);

  /**
     * Pushes raw PCM audio samples into the internal buffer.
   */
  void PushAudio(const float* data, size_t size);

  /**
     * Updates the CMVN (Mean and Variance) parameters for the audio frontend.
   */
  void SetCMVN(const std::vector<float>& means, const std::vector<float>& vars);

  /**
     * Executes one iteration of the processing pipeline.
     * @return 0: No new results, 1: New text/audio output available, -1: Error
   */
  int Process();

  /**
     * Returns the accumulated text result.
   */
  std::string GetTextResult();

  /**
     * Returns the speaker embedding vector.
     * Only valid after Process() returns 1 in SPEAKER_EMBED mode.
     * @return embedding dimension, fills out_data with pointer to internal buffer
   */
  int GetEmbeddingResult(float** out_data);

  /**
     * Resets the processor state for a new request.
     * Clears audio buffer, text accumulator, and recreates model state.
   */
  void Reset();

  // --- TTS-specific methods ---

  /// Push text for TTS synthesis. Returns 0 on success, -1 on error.
  int PushText(const char* text, const char* language = "zh");

  /// Push reference audio for voice cloning. Returns 0 on success, -1 on error.
  int PushReferenceAudio(const float* samples, int n_samples, int sample_rate);

  /// Process TTS: runs encoder on first call, then vocoder chunks on subsequent calls.
  /// Returns 1 if audio chunk available, 0 if done, -1 on error.
  int ProcessTTS();

  /// Get audio output from TTS. Returns number of samples.
  int GetAudioOutput(float** out_data);

  /// Get model architecture name.
  std::string GetArchName() const {
    return model_ ? model_->GetMeta().arch_name : "";
  }

private:
  std::shared_ptr<ISpeechModel> model_;
  std::shared_ptr<RSState> state_;
  std::unique_ptr<AudioProcessor> audio_proc_;

  // Reference to the global backend scheduler managed by rs_context_t
  ggml_backend_sched_t sched_;

  CircularBuffer audio_buffer_;
  std::string text_accumulator_;
  std::vector<float> embedding_buffer_;

  // Last processed token for CTC deduplication
  int last_token_id_ = -1;

  // Config: SenseVoice usually processes in chunks.
  // 1600 samples = 100ms at 16kHz. Adjust based on model requirements.
  int chunk_size_samples_ = 16000; // 1 second chunks for offline-like testing

  // TTS state
  bool tts_encoded_ = false;  // true after text encoder + flow decoder done
  std::vector<float> tts_audio_buf_;  // accumulated audio output
  int tts_audio_read_pos_ = 0;
};