#pragma once

#include <vector>
#include <string>
#include <memory>
#include "ggml.h"
#include "gguf.h"
#include "ggml-backend.h"

// Internal State Base Class (Used for KV Cache or RNN States)
struct RSState {
  virtual ~RSState() = default;
};

struct rs_context_t;

// Model Metadata
struct RSModelMeta {
  std::string arch_name;
  int audio_sample_rate = 16000;
  int n_mels = 80;
  int vocab_size = 0;
};

// --- Core Model Interface ---
class ISpeechModel {
public:
  virtual ~ISpeechModel() = default;

  // 1. Loading Phase: Parse GGUF, load weights to ggml_backend
  virtual bool Load(const std::unique_ptr<rs_context_t>& ctx, ggml_backend_t backend) = 0;

  // 2. State Creation: Create an independent state for each request
  virtual std::shared_ptr<RSState> CreateState() = 0;

  // 3. Encode/Preprocess: e.g., Audio -> Encoder Hidden States
  // Updated: Pass the backend scheduler to handle memory allocation and compute
  virtual bool Encode(const std::vector<float>& input_frames, RSState& state, ggml_backend_sched_t sched) = 0;

  // 4. Decode/Generate: e.g., Hidden States -> Text Tokens
  virtual bool Decode(RSState& state, ggml_backend_sched_t sched) = 0;

  virtual std::string GetTranscription(RSState& state) = 0;

  // 5. Get speaker embedding (SPEAKER_EMBED mode only)
  // Returns embedding dimension, fills out_data. Default returns 0 (not supported).
  virtual int GetEmbedding(RSState& state, float** out_data) { (void)state; (void)out_data; return 0; }

  // Get metadata
  virtual const RSModelMeta& GetMeta() const = 0;
};