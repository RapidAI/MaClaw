#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include <map>
#include <string>
#include <vector>

struct SenseVoiceHParams {
  int32_t n_vocab = 0;
  int32_t n_encoder_hidden_state = 512;
  int32_t n_encoder_linear_units = 2048;
  int32_t n_encoder_attention_heads = 4;
  int32_t n_encoder_layers = 50;
  int32_t n_tp_encoder_layers = 20;
  int32_t n_mels = 80;
  int32_t feats_dim = 560;
  int32_t fsmn_kernel_size = 11;
  float eps = 1e-5f;
};

struct SenseVoiceState : public RSState {
  struct ggml_context *ctx_persistent = nullptr;
  ggml_backend_buffer_t buffer_persistent = nullptr;
  struct ggml_tensor *encoder_out = nullptr;

  std::vector<int32_t> ids;
  std::vector<std::string> tokens;
  int language_id = 0;
  bool use_itn = true;

  SenseVoiceState() {
    // Increase persistent context to ensure enough room for tensor metadata
    struct ggml_init_params params = {512 * ggml_tensor_overhead(), nullptr,
                                      true};
    ctx_persistent = ggml_init(params);
  }

  ~SenseVoiceState() {
    if (buffer_persistent)
      ggml_backend_buffer_free(buffer_persistent);
    if (ctx_persistent)
      ggml_free(ctx_persistent);
  }
};

/**
 * SANM encoder layer structure
 */
struct SenseVoiceLayerEncoder {
  struct ggml_tensor *e_attn_ln_q_w, *e_attn_ln_q_b;
  struct ggml_tensor *e_attn_ln_k_w, *e_attn_ln_k_b;
  struct ggml_tensor *e_attn_ln_v_w, *e_attn_ln_v_b;
  struct ggml_tensor *e_attn_fsmn_w;
  struct ggml_tensor *e_attn_ln_out_w, *e_attn_ln_out_b;
  struct ggml_tensor *e_mlp_w1, *e_mlp_b1;
  struct ggml_tensor *e_mlp_w2, *e_mlp_b2;
  struct ggml_tensor *e_norm_w1, *e_norm_b1;
  struct ggml_tensor *e_norm_w2, *e_norm_b2;
};

/**
 * Collection of SenseVoice model tensors
 */
struct SenseVoiceEncoder {
  struct ggml_tensor *embedding;
  SenseVoiceLayerEncoder encoder0;
  std::vector<SenseVoiceLayerEncoder> encoders_layer;
  std::vector<SenseVoiceLayerEncoder> tp_encoders_layer;
  struct ggml_tensor *e_after_norm_w, *e_after_norm_b;
  struct ggml_tensor *e_tp_norm_w, *e_tp_norm_b;
};

class SenseVoiceEncoderModel {
public:
  SenseVoiceEncoderModel();
  virtual ~SenseVoiceEncoderModel();

  SenseVoiceHParams hparams_;
  std::unique_ptr<SenseVoiceEncoder> model_;
  // Implement base class interface
  bool Load(const std::unique_ptr<rs_context_t> &ctx, ggml_backend_t backend);

  bool Encode(const std::vector<float> &input_frames, RSState &state,
              ggml_backend_sched_t sched);
  virtual bool MapTensors(std::map<std::string, struct ggml_tensor *> &tensors);

private:
  struct ggml_context *ctx_weights_;
  // Optimization: Cache for positional encodings to avoid re-computing sin/cos every inference
  std::vector<float> cached_pos_encoding_;
  int max_pos_len_ = 0; // Tracks the current maximum length allocated

  // Helper to resize and pre-compute the table
  void ensure_pos_encoding_size(int required_len, int dim);
  bool SetLayerWeights(std::vector<SenseVoiceLayerEncoder> &layers,
                       std::map<std::string, struct ggml_tensor *> &tensors,
                       int n_layers, const std::string &prefix);
};