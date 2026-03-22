#pragma once

#include "rapidspeech.h"
#include "core/rs_model.h"
#include "core/rs_processor.h"
#include "ggml-backend.h"
#include "gguf.h"
#include <memory>
#include <vector>
#include <string>
#include <functional>

/**
 * Internal context implementation.
 */
struct rs_context_t {
  rs_init_params_t params;
  std::vector<ggml_backend_buffer_t> weight_buffers;
  std::vector<ggml_backend_t> backends;
  ggml_backend_sched_t sched = nullptr;
  std::shared_ptr<ISpeechModel> model;
  std::unique_ptr<RSProcessor> processor;

  // GGUF related resources (required for Model::Load)
  gguf_context* ctx_gguf = nullptr;
  ggml_context* gguf_data = nullptr;

  rs_context_t();
  ~rs_context_t();

  bool init_backend();
};

// --- Internal Framework Registry API ---
// These declarations ensure consistent signatures across the library

using ModelCreator = std::function<std::shared_ptr<ISpeechModel>()>;

/**
 * Registers a model architecture to the global registry.
 */
void rs_register_model_arch(const std::string& arch, ModelCreator creator);

/**
 * Internal entry point for context initialization.
 */
rs_context_t* rs_context_init_internal(rs_init_params_t params);

bool init_compute_ctx(struct ggml_context ** ctx, struct ggml_cgraph ** gf, int n_nodes);