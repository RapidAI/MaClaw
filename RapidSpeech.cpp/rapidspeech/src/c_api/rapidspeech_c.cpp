#include "rapidspeech.h"
#include "core/rs_context.h" // Use the actual internal header to avoid ABI mismatch
#include "utils/rs_log.h"
#include <string>
#include <memory>

// --- Public C-API Implementation ---

RS_API rs_init_params_t rs_default_params() {
  rs_init_params_t p;
  p.model_path = nullptr;
  p.n_threads = 4;
  p.use_gpu = true;
  p.task_type = RS_TASK_ASR_OFFLINE;
  return p;
}

RS_API rs_context_t* rs_init_from_file(rs_init_params_t params) {
  try {
    // Defined in rs_context.cpp
    extern rs_context_t* rs_context_init_internal(rs_init_params_t params);
    return rs_context_init_internal(params);
  } catch (const std::exception& e) {
    RS_LOG_ERR("C-API Init Error: %s", e.what());
    return nullptr;
  }
}

RS_API void rs_free(rs_context_t* ctx) {
  if (ctx) {
    delete ctx;
  }
}

RS_API int rs_push_audio(rs_context_t* ctx, const float* pcm, int n_samples) {
  if (!ctx || !ctx->processor) {
    RS_LOG_ERR("Invalid context or processor in rs_push_audio");
    return -1;
  }
  ctx->processor->PushAudio(pcm, n_samples);
  return 0;
}

RS_API int rs_process(rs_context_t* ctx) {
  if (!ctx || !ctx->processor) return -1;

  // TTS models use a different processing path
  if (ctx->processor->GetArchName() == "openvoice2") {
    return ctx->processor->ProcessTTS();
  }

  return ctx->processor->Process();
}

RS_API const char* rs_get_text_output(rs_context_t* ctx) {
  // Use thread_local to avoid data races when multiple contexts are used
  // from different threads (each thread gets its own buffer).
  thread_local std::string temp_res;
  if (!ctx || !ctx->processor) return "";
  temp_res = ctx->processor->GetTextResult();
  return temp_res.c_str();
}

RS_API int rs_push_text(rs_context_t* ctx, const char* text) {
  if (!ctx || !ctx->processor || !text) return -1;
  return ctx->processor->PushText(text);
}

RS_API int rs_get_audio_output(rs_context_t* ctx, float** out_pcm) {
  if (!ctx || !ctx->processor || !out_pcm) return 0;
  return ctx->processor->GetAudioOutput(out_pcm);
}

RS_API int rs_get_embedding_output(rs_context_t* ctx, float** out_embedding) {
  if (!ctx || !ctx->processor || !out_embedding) return 0;
  return ctx->processor->GetEmbeddingResult(out_embedding);
}

RS_API void rs_reset(rs_context_t* ctx) {
  if (ctx && ctx->processor) {
    ctx->processor->Reset();
  }
}

RS_API float rs_speaker_verify(rs_context_t* ctx,
                               const float* audio1, int n_samples1,
                               const float* audio2, int n_samples2,
                               int sample_rate) {
  (void)sample_rate;  // Currently assumes 16kHz internally
  if (!ctx || !ctx->processor) return -2.0f;

  // Extract embedding for audio1
  ctx->processor->Reset();
  ctx->processor->PushAudio(audio1, n_samples1);
  if (ctx->processor->Process() < 0) return -2.0f;
  float* emb1_ptr = nullptr;
  int dim1 = ctx->processor->GetEmbeddingResult(&emb1_ptr);
  if (dim1 <= 0 || !emb1_ptr) return -2.0f;
  // Copy embedding1 since Reset() will invalidate the pointer
  std::vector<float> emb1(emb1_ptr, emb1_ptr + dim1);

  // Extract embedding for audio2
  ctx->processor->Reset();
  ctx->processor->PushAudio(audio2, n_samples2);
  if (ctx->processor->Process() < 0) return -2.0f;
  float* emb2_ptr = nullptr;
  int dim2 = ctx->processor->GetEmbeddingResult(&emb2_ptr);
  if (dim2 <= 0 || !emb2_ptr || dim1 != dim2) return -2.0f;

  // Cosine similarity (embeddings are already L2-normalized by ECAPA-TDNN)
  float dot = 0.0f;
  for (int i = 0; i < dim1; i++) {
    dot += emb1[i] * emb2_ptr[i];
  }
  return dot;
}

RS_API int rs_push_reference_audio(rs_context_t* ctx, const float* samples,
                                   int n_samples, int sample_rate) {
  if (!ctx || !ctx->processor || !samples || n_samples <= 0) return -1;
  return ctx->processor->PushReferenceAudio(samples, n_samples, sample_rate);
}
