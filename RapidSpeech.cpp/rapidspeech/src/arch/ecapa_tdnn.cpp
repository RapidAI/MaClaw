#include "ecapa_tdnn.h"
#include "core/rs_context.h"
#include "ggml-backend.h"
#include "ggml-cpu.h"
#include "ggml.h"
#include "utils/rs_log.h"
#include <cmath>
#include <cstring>
#include <functional>
#include <numeric>

// Max graph nodes for ECAPA-TDNN (much smaller than SenseVoice)
#define ECAPA_MAX_NODES 2048

// =====================================================================
// ggml graph helpers
// =====================================================================

// Replacement for removed ggml_new_f32: create a 1-element f32 tensor
static struct ggml_tensor* ggml_scalar_f32(struct ggml_context* ctx, float val) {
  struct ggml_tensor* t = ggml_new_tensor_1d(ctx, GGML_TYPE_F32, 1);
  ggml_set_f32(t, val);
  return t;
}

// BatchNorm1d: y = (x - mean) / sqrt(var + eps) * w + b
// x shape: [C, T] (channels-first after conv1d)
static struct ggml_tensor* batch_norm_1d(struct ggml_context* ctx,
                                          struct ggml_tensor* x,
                                          struct ggml_tensor* w,
                                          struct ggml_tensor* b,
                                          struct ggml_tensor* mean,
                                          struct ggml_tensor* var,
                                          float eps) {
  // x: [C, T], mean/var/w/b: [C]
  // Subtract mean
  struct ggml_tensor* cur = ggml_sub(ctx, x, mean);
  // Divide by sqrt(var + eps) — we compute rsqrt = 1/sqrt(var+eps) then multiply
  struct ggml_tensor* var_eps = ggml_add1(ctx, var, ggml_scalar_f32(ctx, eps));
  struct ggml_tensor* inv_std = ggml_sqrt(ctx, var_eps);
  cur = ggml_div(ctx, cur, inv_std);
  // Scale and shift
  cur = ggml_mul(ctx, cur, w);
  cur = ggml_add(ctx, cur, b);
  return cur;
}

// TDNNBlock forward: Conv1d + BN + ReLU
// input: [in_ch, T], output: [out_ch, T']
static struct ggml_tensor* tdnn_block_forward(struct ggml_context* ctx,
                                               struct ggml_tensor* x,
                                               const TDNNBlock& block,
                                               int stride, int dilation,
                                               float eps) {
  // Conv1d: weight shape [out_ch, in_ch, kernel_size]
  int kernel_size = block.conv_w->ne[0];
  int padding = ((kernel_size - 1) * dilation) / 2;
  struct ggml_tensor* cur = ggml_conv_1d(ctx, block.conv_w, x, stride, padding, dilation);
  cur = ggml_add(ctx, cur, block.conv_b);
  // BatchNorm
  cur = batch_norm_1d(ctx, cur, block.bn_w, block.bn_b, block.bn_mean, block.bn_var, eps);
  // ReLU
  cur = ggml_relu(ctx, cur);
  return cur;
}

// SE-Res2Block forward
// input: [C, T], output: [C, T]
static struct ggml_tensor* se_res2_block_forward(struct ggml_context* ctx,
                                                  struct ggml_tensor* x,
                                                  const SERes2Block& block,
                                                  int res2_scale,
                                                  int kernel_size,
                                                  int dilation,
                                                  float eps) {
  int T = x->ne[1];

  // 1. First 1x1 conv (channel projection)
  struct ggml_tensor* cur = tdnn_block_forward(ctx, x, block.tdnn1, 1, 1, eps);

  // 2. Res2Net-style multi-scale processing
  // Split cur [C_out, T] into res2_scale sub-bands along channel dim
  int sub_ch = cur->ne[0] / res2_scale;

  std::vector<struct ggml_tensor*> sub_outputs;
  struct ggml_tensor* prev = nullptr;

  for (int s = 0; s < res2_scale; s++) {
    // Extract sub-band s: view [sub_ch, T] at offset s*sub_ch
    struct ggml_tensor* sub = ggml_view_2d(ctx, cur, sub_ch, T,
                                            cur->nb[1], s * sub_ch * sizeof(float));

    if (s == 0) {
      // First sub-band: identity
      sub_outputs.push_back(sub);
    } else {
      // Add previous sub-band output
      if (prev) {
        sub = ggml_add(ctx, sub, prev);
      }
      // Apply per-sub-band convolution
      int conv_idx = s - 1;  // res2_convs[0] corresponds to sub-band 1
      int padding = ((kernel_size - 1) * dilation) / 2;
      struct ggml_tensor* conv_out = ggml_conv_1d(ctx, block.res2_convs[conv_idx].conv_w,
                                                    sub, 1, padding, dilation);
      conv_out = ggml_add(ctx, conv_out, block.res2_convs[conv_idx].conv_b);
      conv_out = batch_norm_1d(ctx, conv_out,
                                block.res2_convs[conv_idx].bn_w,
                                block.res2_convs[conv_idx].bn_b,
                                block.res2_convs[conv_idx].bn_mean,
                                block.res2_convs[conv_idx].bn_var, eps);
      conv_out = ggml_relu(ctx, conv_out);
      sub_outputs.push_back(conv_out);
    }
    prev = sub_outputs.back();
  }

  // Concatenate sub-bands back: [C, T]
  struct ggml_tensor* cat = sub_outputs[0];
  for (int s = 1; s < res2_scale; s++) {
    cat = ggml_concat(ctx, cat, sub_outputs[s], 0);
  }

  // 3. Second 1x1 conv
  cur = tdnn_block_forward(ctx, cat, block.tdnn2, 1, 1, eps);

  // 4. SE (Squeeze-and-Excitation)
  // Global average pooling over time: [C, T] -> [C, 1]
  struct ggml_tensor* se_in = ggml_pool_1d(ctx, cur, GGML_OP_POOL_AVG, T, T, 0);
  // se_in shape: [C, 1]
  // FC1 + ReLU
  struct ggml_tensor* se = ggml_mul_mat(ctx, block.se.fc1_w, se_in);
  se = ggml_add(ctx, se, block.se.fc1_b);
  se = ggml_relu(ctx, se);
  // FC2 + Sigmoid
  se = ggml_mul_mat(ctx, block.se.fc2_w, se);
  se = ggml_add(ctx, se, block.se.fc2_b);
  se = ggml_sigmoid(ctx, se);
  // Scale: element-wise multiply (broadcast over T)
  cur = ggml_mul(ctx, cur, se);

  // 5. Residual connection
  struct ggml_tensor* residual = x;
  if (block.has_shortcut) {
    // Shortcut conv to match channels
    int sc_padding = 0;
    residual = ggml_conv_1d(ctx, block.shortcut.conv_w, x, 1, sc_padding, 1);
    residual = ggml_add(ctx, residual, block.shortcut.conv_b);
    residual = batch_norm_1d(ctx, residual, block.shortcut.bn_w, block.shortcut.bn_b,
                              block.shortcut.bn_mean, block.shortcut.bn_var, eps);
  }
  cur = ggml_add(ctx, cur, residual);

  return cur;
}

// =====================================================================
// EcapaTdnnModel implementation
// =====================================================================

EcapaTdnnModel::EcapaTdnnModel() {}
EcapaTdnnModel::~EcapaTdnnModel() {}

void EcapaTdnnModel::MapTDNNBlock(TDNNBlock& block,
                                   std::map<std::string, struct ggml_tensor*>& t,
                                   const std::string& prefix) {
  block.conv_w  = t.at(prefix + ".conv.weight");
  block.conv_b  = t.at(prefix + ".conv.bias");
  block.bn_w    = t.at(prefix + ".norm.weight");
  block.bn_b    = t.at(prefix + ".norm.bias");
  block.bn_mean = t.at(prefix + ".norm.running_mean");
  block.bn_var  = t.at(prefix + ".norm.running_var");
}

void EcapaTdnnModel::MapSERes2Block(SERes2Block& block,
                                     std::map<std::string, struct ggml_tensor*>& t,
                                     const std::string& prefix,
                                     bool has_shortcut) {
  // tdnn1: blocks.X.tdnn1
  MapTDNNBlock(block.tdnn1, t, prefix + ".tdnn1");
  // res2 sub-band convolutions: blocks.X.res2_convs.Y
  int n_res2 = hparams_.res2_scale - 1;
  block.res2_convs.resize(n_res2);
  for (int i = 0; i < n_res2; i++) {
    MapTDNNBlock(block.res2_convs[i], t, prefix + ".res2_convs." + std::to_string(i));
  }
  // tdnn2: blocks.X.tdnn2
  MapTDNNBlock(block.tdnn2, t, prefix + ".tdnn2");
  // SE block
  block.se.fc1_w = t.at(prefix + ".se.conv1.weight");
  block.se.fc1_b = t.at(prefix + ".se.conv1.bias");
  block.se.fc2_w = t.at(prefix + ".se.conv2.weight");
  block.se.fc2_b = t.at(prefix + ".se.conv2.bias");
  // Shortcut
  block.has_shortcut = has_shortcut;
  if (has_shortcut) {
    MapTDNNBlock(block.shortcut, t, prefix + ".shortcut");
  }
}

bool EcapaTdnnModel::MapTensors(std::map<std::string, struct ggml_tensor*>& tensors) {
  try {
    // layer0: initial TDNN
    MapTDNNBlock(weights_.layer0, tensors, "blocks.0");

    // 3 SE-Res2Blocks (blocks.1, blocks.2, blocks.3)
    weights_.se_res2_blocks.resize(hparams_.n_se_res2_blocks);
    for (int i = 0; i < hparams_.n_se_res2_blocks; i++) {
      std::string prefix = "blocks." + std::to_string(i + 1);
      // Detect shortcut from tensor presence (more robust than hardcoding)
      bool has_shortcut = tensors.count(prefix + ".shortcut.conv.weight") > 0;
      MapSERes2Block(weights_.se_res2_blocks[i], tensors, prefix, has_shortcut);
    }

    // MFA conv: blocks.4
    MapTDNNBlock(weights_.mfa_conv, tensors, "blocks.4");

    // ASP (Attentive Statistical Pooling): blocks.5
    weights_.asp.tdnn_w   = tensors.at("blocks.5.asp_conv.conv.weight");
    weights_.asp.tdnn_b   = tensors.at("blocks.5.asp_conv.conv.bias");
    weights_.asp.bn_w     = tensors.at("blocks.5.asp_conv.norm.weight");
    weights_.asp.bn_b     = tensors.at("blocks.5.asp_conv.norm.bias");
    weights_.asp.bn_mean  = tensors.at("blocks.5.asp_conv.norm.running_mean");
    weights_.asp.bn_var   = tensors.at("blocks.5.asp_conv.norm.running_var");
    weights_.asp.attn_w   = tensors.at("blocks.5.asp_attn.weight");
    weights_.asp.attn_b   = tensors.at("blocks.5.asp_attn.bias");

    // Final FC + BN: blocks.6
    weights_.fc_w       = tensors.at("blocks.6.fc.weight");
    weights_.fc_b       = tensors.at("blocks.6.fc.bias");
    weights_.fc_bn_w    = tensors.at("blocks.6.norm.weight");
    weights_.fc_bn_b    = tensors.at("blocks.6.norm.bias");
    weights_.fc_bn_mean = tensors.at("blocks.6.norm.running_mean");
    weights_.fc_bn_var  = tensors.at("blocks.6.norm.running_var");

    return true;
  } catch (const std::exception& e) {
    RS_LOG_ERR("ECAPA-TDNN tensor mapping failed: %s", e.what());
    return false;
  }
}

bool EcapaTdnnModel::Load(const std::unique_ptr<rs_context_t>& ctx, ggml_backend_t backend) {
  if (!ctx || !ctx->ctx_gguf || !ctx->gguf_data) {
    RS_LOG_ERR("Invalid context for ECAPA-TDNN Load");
    return false;
  }

  gguf_context* ctx_gguf = ctx->ctx_gguf;
  ggml_context* gguf_data = ctx->gguf_data;

  // Load hyperparameters from GGUF KV (with defaults)
  int64_t key;
  key = gguf_find_key(ctx_gguf, "ecapa.channels");
  if (key != -1) hparams_.channels = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "ecapa.emb_dim");
  if (key != -1) hparams_.emb_dim = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "ecapa.n_mels");
  if (key != -1) hparams_.n_mels = gguf_get_val_i32(ctx_gguf, key);
  key = gguf_find_key(ctx_gguf, "ecapa.res2_scale");
  if (key != -1) hparams_.res2_scale = gguf_get_val_i32(ctx_gguf, key);

  meta_.arch_name = "ecapa-tdnn";
  meta_.audio_sample_rate = 16000;
  meta_.n_mels = hparams_.n_mels;
  meta_.vocab_size = 0;  // no vocab for speaker embedding

  RS_LOG_INFO("ECAPA-TDNN: C=%d, emb=%d, mels=%d, scale=%d",
              hparams_.channels, hparams_.emb_dim, hparams_.n_mels, hparams_.res2_scale);

  // ECAPA-TDNN does NOT use CMVN or LFR — the processor was already
  // configured with use_lfr=false, use_cmvn=false via the arch-aware
  // RSProcessor constructor.

  // Map tensors
  std::map<std::string, struct ggml_tensor*> tensors;
  const int n_tensors = gguf_get_n_tensors(ctx_gguf);
  for (int i = 0; i < n_tensors; ++i) {
    const char* name = gguf_get_tensor_name(ctx_gguf, i);
    struct ggml_tensor* t = ggml_get_tensor(gguf_data, name);
    if (t) tensors[name] = t;
  }

  return MapTensors(tensors);
}

std::shared_ptr<RSState> EcapaTdnnModel::CreateState() {
  return std::make_shared<EcapaTdnnState>();
}

bool EcapaTdnnModel::Encode(const std::vector<float>& input_frames,
                             RSState& state, ggml_backend_sched_t sched) {
  auto& spk_state = static_cast<EcapaTdnnState&>(state);
  float eps = hparams_.eps;
  int C = hparams_.channels;
  int n_mels = hparams_.n_mels;
  int emb_dim = hparams_.emb_dim;

  // input_frames: [n_mels * T] flattened, row-major [n_mels, T]
  int T = input_frames.size() / n_mels;
  if (T <= 0) {
    RS_LOG_ERR("ECAPA-TDNN: empty input");
    return false;
  }

  struct ggml_context* ctx0 = nullptr;
  struct ggml_cgraph* gf = nullptr;
  if (!init_compute_ctx(&ctx0, &gf, ECAPA_MAX_NODES)) return false;

  // Input tensor: [n_mels, T]
  struct ggml_tensor* input = ggml_new_tensor_2d(ctx0, GGML_TYPE_F32, n_mels, T);
  ggml_set_name(input, "fbank_input");
  ggml_set_input(input);

  // --- Layer 0: TDNN (Conv1d(n_mels, C, 5) + BN + ReLU) ---
  struct ggml_tensor* cur = tdnn_block_forward(ctx0, input, weights_.layer0, 1, 1, eps);

  // --- SE-Res2Blocks ---
  // Collect outputs for MFA (multi-layer feature aggregation)
  std::vector<struct ggml_tensor*> block_outputs;
  for (int i = 0; i < hparams_.n_se_res2_blocks; i++) {
    cur = se_res2_block_forward(ctx0, cur, weights_.se_res2_blocks[i],
                                 hparams_.res2_scale,
                                 hparams_.kernel_sizes[i],
                                 hparams_.dilations[i], eps);
    block_outputs.push_back(cur);
  }

  // --- MFA: concatenate block outputs along channel dim ---
  struct ggml_tensor* mfa_in = block_outputs[0];
  for (int i = 1; i < (int)block_outputs.size(); i++) {
    mfa_in = ggml_concat(ctx0, mfa_in, block_outputs[i], 0);
  }
  // Conv1d(3*C, C*3, 1) + BN + ReLU  (the MFA conv)
  // Actually SpeechBrain uses Conv1d(3*C, C*3, 1) but let's follow the weights
  cur = tdnn_block_forward(ctx0, mfa_in, weights_.mfa_conv, 1, 1, eps);

  int T_out = cur->ne[1];  // time dimension after all convolutions

  // --- ASP (Attentive Statistical Pooling) ---
  // 1. Attention conv: Conv1d + BN + Tanh
  struct ggml_tensor* attn = ggml_conv_1d(ctx0, weights_.asp.tdnn_w, cur, 1, 0, 1);
  attn = ggml_add(ctx0, attn, weights_.asp.tdnn_b);
  attn = batch_norm_1d(ctx0, attn, weights_.asp.bn_w, weights_.asp.bn_b,
                        weights_.asp.bn_mean, weights_.asp.bn_var, eps);
  attn = ggml_tanh(ctx0, attn);

  // 2. Attention linear: project to 1 channel for attention weights
  // attn_w: [1, attn_ch], attn: [attn_ch, T]
  attn = ggml_mul_mat(ctx0, weights_.asp.attn_w, attn);
  attn = ggml_add(ctx0, attn, weights_.asp.attn_b);

  // 3. Softmax over time dimension
  // attn shape: [1, T] -> softmax along dim 1 (time)
  // ggml_soft_max operates on last dim (ne[0]), so we need to transpose
  attn = ggml_transpose(ctx0, attn);  // [T, 1]
  attn = ggml_soft_max(ctx0, attn);   // softmax over T
  attn = ggml_transpose(ctx0, attn);  // [1, T]

  // 4. Weighted mean: sum(cur * attn, dim=T) -> [C, 1]
  // Broadcast attn [1, T] over cur [C, T]
  struct ggml_tensor* weighted = ggml_mul(ctx0, cur, attn);
  struct ggml_tensor* mean = ggml_pool_1d(ctx0, weighted, GGML_OP_POOL_AVG, T_out, T_out, 0);
  // Scale: pool_avg divides by T, but we want sum, so multiply by T
  mean = ggml_scale(ctx0, mean, (float)T_out);

  // 5. Weighted std: sqrt(sum(cur^2 * attn, dim=T) - mean^2)
  struct ggml_tensor* cur_sq = ggml_mul(ctx0, cur, cur);
  struct ggml_tensor* weighted_sq = ggml_mul(ctx0, cur_sq, attn);
  struct ggml_tensor* var = ggml_pool_1d(ctx0, weighted_sq, GGML_OP_POOL_AVG, T_out, T_out, 0);
  var = ggml_scale(ctx0, var, (float)T_out);
  var = ggml_sub(ctx0, var, ggml_mul(ctx0, mean, mean));
  // Clamp variance to avoid sqrt of negative
  var = ggml_relu(ctx0, var);  // max(0, var)
  struct ggml_tensor* std_dev = ggml_sqrt(ctx0, ggml_add1(ctx0, var, ggml_scalar_f32(ctx0, 1e-10f)));

  // 6. Concatenate mean and std: [C*2, 1]
  struct ggml_tensor* pooled = ggml_concat(ctx0, mean, std_dev, 0);

  // --- Final FC + BN ---
  // FC: [emb_dim, C*2] x [C*2, 1] -> [emb_dim, 1]
  struct ggml_tensor* emb = ggml_mul_mat(ctx0, weights_.fc_w, pooled);
  emb = ggml_add(ctx0, emb, weights_.fc_b);
  // BN on embedding
  emb = batch_norm_1d(ctx0, emb, weights_.fc_bn_w, weights_.fc_bn_b,
                       weights_.fc_bn_mean, weights_.fc_bn_var, eps);

  // L2 normalization is done on CPU after graph compute (simpler and more reliable
  // than building it in the ggml graph, since ggml has no direct L2-norm op)

  ggml_set_name(emb, "embedding_out");
  ggml_set_output(emb);
  ggml_build_forward_expand(gf, emb);

  // Allocate and compute
  if (!ggml_backend_sched_alloc_graph(sched, gf)) {
    RS_LOG_ERR("ECAPA-TDNN: graph allocation failed");
    ggml_free(ctx0);
    return false;
  }

  // Set input data
  ggml_backend_tensor_set(input, input_frames.data(), 0,
                          input_frames.size() * sizeof(float));

  if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
    RS_LOG_ERR("ECAPA-TDNN: graph compute failed");
    ggml_free(ctx0);
    return false;
  }

  // Read embedding output and L2-normalize on CPU
  spk_state.embedding.resize(emb_dim);
  ggml_backend_tensor_get(emb, spk_state.embedding.data(), 0, emb_dim * sizeof(float));

  // L2 normalize on CPU (simple and reliable)
  float norm_sq = 0.0f;
  for (int i = 0; i < emb_dim; i++) {
    norm_sq += spk_state.embedding[i] * spk_state.embedding[i];
  }
  float inv_norm = 1.0f / (std::sqrt(norm_sq) + 1e-10f);
  for (int i = 0; i < emb_dim; i++) {
    spk_state.embedding[i] *= inv_norm;
  }

  ggml_free(ctx0);
  return true;
}

bool EcapaTdnnModel::Decode(RSState& state, ggml_backend_sched_t sched) {
  // No decode step for speaker embedding — everything happens in Encode
  (void)state; (void)sched;
  return true;
}

std::string EcapaTdnnModel::GetTranscription(RSState& state) {
  (void)state;
  return "";  // Speaker embedding model has no text output
}

int EcapaTdnnModel::GetEmbedding(RSState& state, float** out_data) {
  auto& spk_state = static_cast<EcapaTdnnState&>(state);
  if (spk_state.embedding.empty()) return 0;
  *out_data = spk_state.embedding.data();
  return static_cast<int>(spk_state.embedding.size());
}

// =====================================================================
// Static registration
// =====================================================================
extern void rs_register_model_arch(const std::string& arch,
                                    std::function<std::shared_ptr<ISpeechModel>()> creator);
namespace {
struct EcapaTdnnRegistrar {
  EcapaTdnnRegistrar() {
    rs_register_model_arch("ecapa-tdnn", []() {
      return std::make_shared<EcapaTdnnModel>();
    });
  }
} global_ecapa_tdnn_reg;
}  // namespace
