#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include <map>
#include <string>
#include <vector>

// --- ECAPA-TDNN Hyperparameters ---
struct EcapaTdnnHParams {
  int32_t n_mels        = 80;
  int32_t channels      = 1024;   // C=1024 for ECAPA-TDNN large
  int32_t emb_dim       = 192;    // output embedding dimension
  int32_t n_se_res2_blocks = 3;   // number of SE-Res2Block layers
  // Res2Block internal scale (number of sub-bands)
  int32_t res2_scale    = 8;
  // Kernel sizes for the 3 SE-Res2Blocks
  int32_t kernel_sizes[3] = {3, 3, 3};
  // Dilation sizes for the 3 SE-Res2Blocks
  int32_t dilations[3]    = {2, 3, 4};
  float   eps           = 1e-5f;
};

// --- ECAPA-TDNN State ---
struct EcapaTdnnState : public RSState {
  // Output embedding buffer (192-dim)
  std::vector<float> embedding;

  EcapaTdnnState() {}
  ~EcapaTdnnState() override = default;
};

// --- Weight structures ---

// A single 1D convolution block: Conv1d + BatchNorm + ReLU
struct TDNNBlock {
  struct ggml_tensor* conv_w;   // [out_ch, in_ch, kernel]
  struct ggml_tensor* conv_b;
  struct ggml_tensor* bn_w;     // [out_ch]
  struct ggml_tensor* bn_b;
  struct ggml_tensor* bn_mean;
  struct ggml_tensor* bn_var;
};

// SE (Squeeze-and-Excitation) block weights
struct SEBlock {
  struct ggml_tensor* fc1_w;    // [channels/se_r, channels]
  struct ggml_tensor* fc1_b;
  struct ggml_tensor* fc2_w;    // [channels, channels/se_r]
  struct ggml_tensor* fc2_b;
};

// SE-Res2Block: the core building block of ECAPA-TDNN
// Conv1d(1x1) -> split into sub-bands -> Conv1d per sub-band -> concat -> Conv1d(1x1) -> SE -> residual
struct SERes2Block {
  // Input 1x1 conv (channel projection)
  TDNNBlock tdnn1;
  // Per-sub-band 1D convolutions (res2_scale - 1 sub-convolutions)
  // sub-band 0 is identity, sub-bands 1..scale-1 have convolutions
  std::vector<TDNNBlock> res2_convs;  // size = res2_scale - 1
  // Output 1x1 conv
  TDNNBlock tdnn2;
  // SE block
  SEBlock se;
  // Shortcut conv (if input channels != output channels)
  TDNNBlock shortcut;
  bool has_shortcut = false;
};

// Full ECAPA-TDNN model weights
struct EcapaTdnnWeights {
  // Initial TDNN layer: Conv1d(n_mels, C, 5) + BN + ReLU
  TDNNBlock layer0;
  // 3 SE-Res2Blocks
  std::vector<SERes2Block> se_res2_blocks;  // size = 3
  // MFA (Multi-layer Feature Aggregation) conv: Conv1d(3*C, C*3, 1)
  // Actually: concatenate outputs of 3 blocks -> Conv1d -> BN -> ReLU
  TDNNBlock mfa_conv;
  // Attentive Statistical Pooling
  struct {
    struct ggml_tensor* tdnn_w;   // Conv1d weight for attention
    struct ggml_tensor* tdnn_b;
    struct ggml_tensor* bn_w;
    struct ggml_tensor* bn_b;
    struct ggml_tensor* bn_mean;
    struct ggml_tensor* bn_var;
    struct ggml_tensor* attn_w;   // final attention linear weight
    struct ggml_tensor* attn_b;
  } asp;
  // Final FC layer: Linear(C*2, emb_dim)
  struct ggml_tensor* fc_w;
  struct ggml_tensor* fc_b;
  // Final BatchNorm on embedding
  struct ggml_tensor* fc_bn_w;
  struct ggml_tensor* fc_bn_b;
  struct ggml_tensor* fc_bn_mean;
  struct ggml_tensor* fc_bn_var;
};

// --- ECAPA-TDNN Model Class ---
class EcapaTdnnModel : public ISpeechModel {
public:
  EcapaTdnnModel();
  ~EcapaTdnnModel() override;

  bool Load(const std::unique_ptr<rs_context_t>& ctx, ggml_backend_t backend) override;
  std::shared_ptr<RSState> CreateState() override;
  bool Encode(const std::vector<float>& input_frames, RSState& state, ggml_backend_sched_t sched) override;
  bool Decode(RSState& state, ggml_backend_sched_t sched) override;
  std::string GetTranscription(RSState& state) override;
  int GetEmbedding(RSState& state, float** out_data) override;
  const RSModelMeta& GetMeta() const override { return meta_; }

private:
  RSModelMeta meta_;
  EcapaTdnnHParams hparams_;
  EcapaTdnnWeights weights_;

  bool MapTensors(std::map<std::string, struct ggml_tensor*>& tensors);
  void MapTDNNBlock(TDNNBlock& block, std::map<std::string, struct ggml_tensor*>& t,
                    const std::string& prefix);
  void MapSERes2Block(SERes2Block& block, std::map<std::string, struct ggml_tensor*>& t,
                      const std::string& prefix, bool has_shortcut);
};
