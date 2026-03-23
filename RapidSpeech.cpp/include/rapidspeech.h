#ifndef RAPIDSPEECH_H
#define RAPIDSPEECH_H

#include <stdint.h>
#include <stdbool.h>

// --- API Export Macro ---
#if defined(_WIN32)
#if defined(RAPIDSPEECH_BUILD)
#define RS_API __declspec(dllexport)
#else
#define RS_API __declspec(dllimport)
#endif
#else
#define RS_API __attribute__((visibility("default")))
#endif

#ifdef __cplusplus
extern "C" {
#endif

// --- Core Type Definitions ---

// Context Handle (Opaque Pointer)
typedef struct rs_context_t rs_context_t;

// Task Types
typedef enum {
  RS_TASK_ASR_OFFLINE = 0,
  RS_TASK_ASR_ONLINE,
  RS_TASK_TTS_OFFLINE,
  RS_TASK_TTS_ONLINE,
  RS_TASK_E2E_SPEECH_LLM, // End-to-End Speech LLM
  RS_TASK_SPEAKER_EMBED    // Speaker embedding extraction
} rs_task_type_t;

// Initialization Parameters
typedef struct {
  const char* model_path;   // GGUF model path
  int n_threads;           // Number of CPU threads
  bool use_gpu;            // Whether to use GPU/NPU
  rs_task_type_t task_type;
} rs_init_params_t;

// Default parameter generator
RS_API rs_init_params_t rs_default_params();

// --- Lifecycle Management ---

// Initialize context from file
RS_API rs_context_t* rs_init_from_file(rs_init_params_t params);

// Free context
RS_API void rs_free(rs_context_t* ctx);

// --- Data Processing Interface (Unified Streaming Abstraction) ---

// Push audio data (ASR/E2E mode)
// pcm: 32-bit float audio data, n_samples: number of samples
RS_API int rs_push_audio(rs_context_t* ctx, const float* pcm, int n_samples);

// Push text data (TTS/LLM mode)
RS_API int rs_push_text(rs_context_t* ctx, const char* text);

// Execute single inference step
// Returns: 0=No output, 1=Has output, -1=Error
RS_API int rs_process(rs_context_t* ctx);

// --- Result Retrieval Interface ---

// Get generated audio (TTS mode)
// out_pcm: Pointer to internal buffer, returns number of samples
RS_API int rs_get_audio_output(rs_context_t* ctx, float** out_pcm);

// Get generated text (ASR mode)
// Returns string, no need to free
RS_API const char* rs_get_text_output(rs_context_t* ctx);

// Get speaker embedding (SPEAKER_EMBED mode)
// out_embedding: Pointer to internal buffer, returns embedding dimension
// The embedding is L2-normalized
RS_API int rs_get_embedding_output(rs_context_t* ctx, float** out_embedding);

// Reset context state for a new request (clears audio buffer, text, state)
RS_API void rs_reset(rs_context_t* ctx);

// --- Speaker Verification ---

// Compare two audio segments and return cosine similarity score.
// Both audio inputs should be 32-bit float PCM at the given sample_rate.
// Returns similarity score in [-1, 1], or -2.0f on error.
RS_API float rs_speaker_verify(rs_context_t* ctx,
                               const float* audio1, int n_samples1,
                               const float* audio2, int n_samples2,
                               int sample_rate);

// --- TTS Voice Cloning ---

// Push reference audio for voice cloning (tone color transfer).
// Must be called before rs_push_text + rs_process for the cloned voice to take effect.
// Returns 0 on success, -1 on error.
RS_API int rs_push_reference_audio(rs_context_t* ctx, const float* samples,
                                   int n_samples, int sample_rate);

#ifdef __cplusplus
}
#endif

#endif // RAPIDSPEECH_H