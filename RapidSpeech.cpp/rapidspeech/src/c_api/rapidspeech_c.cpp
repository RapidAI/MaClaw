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
  (void)ctx; (void)text;
  return 0;
}

RS_API int rs_get_audio_output(rs_context_t* ctx, float** out_pcm) {
  (void)ctx; (void)out_pcm;
  return 0;
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