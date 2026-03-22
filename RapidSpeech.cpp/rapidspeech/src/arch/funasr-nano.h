#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include "sensevoice_encoder.h"
#include <map>
#include <string>
#include <unordered_map>
#include <vector>

struct SenseVoiceLayerEncoder;
struct SenseVoiceEncoder;
/**
 * FunASRNano hyperparameters structure
 */
struct FunASRNanoHParams {
  int32_t n_vocab = 0;
  int32_t n_encoder_hidden_state = 512;
  int32_t n_encoder_linear_units = 2048;
  int32_t n_encoder_attention_heads = 8;
  int32_t n_encoder_layers = 50;
  int32_t n_tp_encoder_layers = 10;
  int32_t n_mels = 80;
  int32_t feats_dim = 560;
  int32_t n_ctc_layers = 5;
  int32_t ctc_downsample_rate = 1;
  int32_t ctc_encoder_dim = 512;
  int32_t ctc_llm_dim = 512;
  int32_t ctc_ffn_dim = 2048;
  int32_t ctc_attention_heads = 8;

  int32_t fsmn_kernel_size = 11;
  float eps = 1e-5f;
  bool use_llm = true;
};

/**
 * FunASRNano vocabulary structure
 */
struct FunASRNanoVocab {
  std::unordered_map<int, std::string> id_to_token;
  int n_vocab = 0;
  int blank_id = 60514;
};

struct TransformerDecoder {
  struct ggml_tensor *self_attn_linear_q_weight, *self_attn_linear_q_bias;
  struct ggml_tensor *self_attn_linear_k_weight, *self_attn_linear_k_bias;
  struct ggml_tensor *self_attn_linear_v_weight, *self_attn_linear_v_bias;
  struct ggml_tensor *self_attn_linear_out_weight, *self_attn_linear_out_bias;
  struct ggml_tensor *feed_forward_w_1_weight, *feed_forward_w_1_bias;
  struct ggml_tensor *feed_forward_w_2_weight, *feed_forward_w_2_bias;
  struct ggml_tensor *norm1_weight, *norm1_bias;
  struct ggml_tensor *norm2_weight, *norm2_bias;
};

struct FunASRNanoTransformerDecoder {
  struct ggml_tensor *linear1_weight, *linear1_bias, *linear2_weight,
      *linear2_bias;
  struct ggml_tensor *ctc_out_linear_weight, *ctc_out_linear_bias;
  std::vector<TransformerDecoder> decoders_layer;
};

class FunASRNanoModel : public ISpeechModel {
public:
  FunASRNanoModel();
  virtual ~FunASRNanoModel();

  // Implement base class interface
  bool Load(const std::unique_ptr<rs_context_t> &ctx,
            ggml_backend_t backend) override;
  std::shared_ptr<RSState> CreateState() override;

  // Fixed: added ggml_backend_sched_t to match ISpeechModel interface
  bool Encode(const std::vector<float> &input_frames, RSState &state,
              ggml_backend_sched_t sched) override;
  bool Decode(RSState &state, ggml_backend_sched_t sched) override;
  std::string GetTranscription(RSState &state) override;

  const RSModelMeta &GetMeta() const override { return meta_; }

private:
  RSModelMeta meta_;
  FunASRNanoHParams hparams_;
  FunASRNanoVocab vocab_;
  std::unique_ptr<SenseVoiceEncoderModel> encoder_;
  std::unique_ptr<FunASRNanoTransformerDecoder> decoder_;
  struct ggml_context *ctx_weights_;
  std::vector<int32_t> raw_ids;
  std::vector<float> host_log_probs;
  int beam_size = 1;

  bool MapTensors(std::map<std::string, struct ggml_tensor *> &tensors);
  bool SetLayerWeights(std::vector<SenseVoiceLayerEncoder> &layers,
                       std::map<std::string, struct ggml_tensor *> &tensors,
                       int n_layers, const std::string &prefix);
  bool DecodeWithLLM(RSState &state, ggml_backend_sched_t sched);
  bool DecodeWithoutLLM(RSState &state, ggml_backend_sched_t sched);
};