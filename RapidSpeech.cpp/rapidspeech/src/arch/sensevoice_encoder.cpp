#include "sensevoice_encoder.h"
#include "core/rs_context.h"
#include "ggml-backend.h"
#include "ggml.h"
#include "utils/rs_log.h"
#include <functional>
#include <cmath>
#include "gguf.h"
#include "utils/debug_utils.h"
#include "utils/rs_wav.h"

// Increased node limit to handle deep SenseVoice graphs (50+ layers)
#define SENSE_VOICE_model_MAX_NODES 6144

/**
 * Matrix multiplication with padding optimization for better performance on
 * Apple Metal.
 */
static struct ggml_tensor *ggml_mul_mat_pad(struct ggml_context *ctx,
                                            struct ggml_tensor *x,
                                            struct ggml_tensor *y,
                                            int pad = 32) {
  const int n_pad_req = 8;
  if (x->ne[0] % pad == 0 || x->ne[0] / pad < n_pad_req) {
    return ggml_mul_mat(ctx, x, y);
  }

  struct ggml_tensor *x_0 =
      ggml_view_3d(ctx, x, (x->ne[0] / pad) * pad, x->ne[1], x->ne[2], x->nb[1],
                   x->nb[2], 0);
  struct ggml_tensor *x_1 =
      ggml_view_3d(ctx, x, x->ne[0] % pad, x->ne[1], x->ne[2], x->nb[1],
                   x->nb[2], x_0->ne[0] * x_0->nb[0]);

  struct ggml_tensor *y_0 =
      ggml_view_3d(ctx, y, (y->ne[0] / pad) * pad, y->ne[1], y->ne[2], y->nb[1],
                   y->nb[2], 0);
  struct ggml_tensor *y_1 =
      ggml_view_3d(ctx, y, y->ne[0] % pad, y->ne[1], y->ne[2], y->nb[1],
                   y->nb[2], y_0->ne[0] * y_0->nb[0]);

  return ggml_add(ctx, ggml_mul_mat(ctx, x_0, y_0),
                  ggml_mul_mat(ctx, x_1, y_1));
}

/**
 * Forward pass for a single SANM (Self-Attention Network with Memory) layer.
 * Implements LayerNorm -> SelfAttention -> FSMN -> Residual -> LayerNorm -> MLP
 * -> Residual.
 */
static struct ggml_tensor *
model_layer_sanm_forward(const SenseVoiceHParams &hparams,
                         struct ggml_context *ctx, struct ggml_tensor *cur,
                         SenseVoiceLayerEncoder &layer) {

  const int n_state = hparams.n_encoder_hidden_state;
  const int n_head = hparams.n_encoder_attention_heads;
  const int n_ctx = cur->ne[1];
  const int n_batch = cur->ne[2];

  struct ggml_tensor *residual = cur;
  if (layer.e_norm_w1->ne[0] == layer.e_norm_w2->ne[0]) {
    residual = ggml_cpy(
        ctx, cur,
        ggml_new_tensor_3d(ctx, cur->type, cur->ne[0], cur->ne[1], cur->ne[2]));
  }
  // 1. Layer Norm 1
  cur = ggml_norm(ctx, cur, hparams.eps);
  cur = ggml_add(ctx, ggml_mul(ctx, cur, layer.e_norm_w1), layer.e_norm_b1);

  // 2. Self Attention (Linear Projections)
  struct ggml_tensor *Q = ggml_add(
      ctx, ggml_mul_mat_pad(ctx, ggml_cont(ctx, layer.e_attn_ln_q_w), cur),
      layer.e_attn_ln_q_b);
  struct ggml_tensor *K = ggml_add(
      ctx, ggml_mul_mat_pad(ctx, ggml_cont(ctx, layer.e_attn_ln_k_w), cur),
      layer.e_attn_ln_k_b);
  struct ggml_tensor *V =
      ggml_add(ctx, ggml_mul_mat_pad(ctx, layer.e_attn_ln_v_w, cur),
               layer.e_attn_ln_v_b);

  // Reshape and Permute for multi-head attention
  struct ggml_tensor *Q_h = ggml_permute(
      ctx, ggml_reshape_4d(ctx, Q, n_state / n_head, n_head, n_ctx, n_batch), 0,
      2, 1, 3);
  struct ggml_tensor *K_h = ggml_permute(
      ctx, ggml_reshape_4d(ctx, K, n_state / n_head, n_head, n_ctx, n_batch), 0,
      2, 1, 3);
  struct ggml_tensor *V_h = ggml_permute(
      ctx, ggml_reshape_4d(ctx, V, n_state / n_head, n_head, n_ctx, n_batch), 0,
      2, 1, 3);
  ggml_set_name(K_h, "attention_K");
  ggml_set_name(V_h, "attention_V");
  ggml_set_name(Q_h, "attention_Q");

  // Scaled Dot-Product Attention
  float scale = 1.0f / sqrtf(float(n_state) / n_head);
  struct ggml_tensor *KQ = ggml_mul_mat(ctx, K_h, Q_h);
  struct ggml_tensor *KQ_soft_max =
      ggml_soft_max_ext(ctx, KQ, nullptr, scale, 0.0f);
  struct ggml_tensor *KQV =
      ggml_mul_mat(ctx, ggml_cont(ctx, ggml_transpose(ctx, V_h)), KQ_soft_max);

  // Merge heads
  struct ggml_tensor *attn_out =
      ggml_reshape_3d(ctx, ggml_cont(ctx, ggml_permute(ctx, KQV, 0, 2, 1, 3)),
                      n_state, n_ctx, n_batch);
  attn_out = ggml_add(ctx, ggml_mul_mat(ctx, layer.e_attn_ln_out_w, attn_out),
                      layer.e_attn_ln_out_b);
  ggml_set_name(attn_out, "attn_out");

  // 3. FSMN Block (Memory Block) using depth-wise convolution logic
  int padding = (hparams.fsmn_kernel_size - 1) / 2; // Kernel size 31
  struct ggml_tensor *fsmn_in = ggml_cont(ctx, ggml_transpose(ctx, V));
  struct ggml_tensor *im2col = ggml_im2col(
      ctx, layer.e_attn_fsmn_w,
      ggml_reshape_4d(ctx, fsmn_in, fsmn_in->ne[0], 1, fsmn_in->ne[1],
                      fsmn_in->ne[2] * fsmn_in->ne[3]),
      1, 0, padding, 0, 1, 0, false, GGML_TYPE_F32);
  struct ggml_tensor *fsmn_out = ggml_mul_mat(ctx, layer.e_attn_fsmn_w, im2col);
  fsmn_out = ggml_reshape_3d(ctx, fsmn_out, im2col->ne[1], im2col->ne[2],
                             im2col->ne[3]);
  fsmn_out = ggml_cont(ctx, ggml_transpose(ctx, fsmn_out));
  fsmn_out = ggml_add(ctx, fsmn_out, V);
  ggml_set_name(fsmn_out, "fsmn_memory");

  attn_out = ggml_add(ctx, fsmn_out, attn_out);

  if (layer.e_norm_w1->ne[0] == layer.e_norm_w2->ne[0]) {
    attn_out = ggml_add(ctx, attn_out, residual);
  }

  // 4. Layer Norm 2 + MLP
  cur = ggml_norm(ctx, attn_out, hparams.eps);
  cur = ggml_add(ctx, ggml_mul(ctx, cur, layer.e_norm_w2), layer.e_norm_b2);

  cur = ggml_add(ctx, ggml_mul_mat(ctx, layer.e_mlp_w1, cur), layer.e_mlp_b1);
  cur = ggml_relu(ctx, cur);
  cur = ggml_add(ctx, ggml_mul_mat(ctx, layer.e_mlp_w2, cur), layer.e_mlp_b2);

  return ggml_add(ctx, cur, attn_out);
}

SenseVoiceEncoderModel::SenseVoiceEncoderModel() : ctx_weights_(nullptr) {
  model_ = std::make_unique<SenseVoiceEncoder>();
}

SenseVoiceEncoderModel::~SenseVoiceEncoderModel() {
  if (ctx_weights_) {
    ggml_free(ctx_weights_);
    ctx_weights_ = nullptr;
  }
}

bool SenseVoiceEncoderModel::Load(const std::unique_ptr<rs_context_t> &ctx,
                                  ggml_backend_t backend) {
  if (!ctx || !ctx->ctx_gguf || !ctx->gguf_data) {
    RS_LOG_ERR("Invalid context provided to SenseVoiceModel::Load");
    return false;
  }

  gguf_context *ctx_gguf = ctx->ctx_gguf;
  ggml_context *gguf_data = ctx->gguf_data;

  // 1. Load Hyperparameters from GGUF KV
  hparams_.n_vocab = gguf_get_val_i32(
      ctx_gguf, gguf_find_key(ctx_gguf, "tokenizer.vocab_size"));
  hparams_.n_encoder_hidden_state = gguf_get_val_i32(
      ctx_gguf, gguf_find_key(ctx_gguf, "encoder.output_size"));
  hparams_.n_encoder_linear_units = gguf_get_val_i32(
      ctx_gguf, gguf_find_key(ctx_gguf, "encoder.linear_units"));
  hparams_.n_encoder_attention_heads = gguf_get_val_i32(
      ctx_gguf, gguf_find_key(ctx_gguf, "encoder.attention_heads"));
  hparams_.n_encoder_layers =
      gguf_get_val_i32(ctx_gguf, gguf_find_key(ctx_gguf, "encoder.num_blocks"));
  hparams_.n_tp_encoder_layers =
      gguf_get_val_i32(ctx_gguf, gguf_find_key(ctx_gguf, "encoder.tp_blocks"));
  hparams_.n_mels = 80;

  // 3. Extract CMVN from GGUF metadata
  std::vector<float> cmvn_means, cmvn_vars;
  load_cmvn_params(ctx_gguf, cmvn_means, cmvn_vars);
  if (ctx->processor) {
    ctx->processor->SetCMVN(cmvn_means, cmvn_vars);
  }

  // 4. Map Tensors from ggml_data
  std::map<std::string, struct ggml_tensor *> tensors;
  const int n_tensors = gguf_get_n_tensors(ctx_gguf);
  for (int i = 0; i < n_tensors; ++i) {
    const char *name = gguf_get_tensor_name(ctx_gguf, i);
    struct ggml_tensor *t = ggml_get_tensor(gguf_data, name);
    if (t)
      tensors[name] = t;
  }

  return MapTensors(tensors);
}

bool SenseVoiceEncoderModel::MapTensors(
    std::map<std::string, struct ggml_tensor *> &tensors) {
  try {

    model_->embedding =
        tensors["embed.weight"] ? tensors["embed.weight"] : nullptr;

    std::vector<SenseVoiceLayerEncoder> tmp_encoder0(1);
    SetLayerWeights(tmp_encoder0, tensors, 1, "encoders0");
    model_->encoder0 = tmp_encoder0[0];

    SetLayerWeights(model_->encoders_layer, tensors,
                    hparams_.n_encoder_layers - 1, "encoders");
    SetLayerWeights(model_->tp_encoders_layer, tensors,
                    hparams_.n_tp_encoder_layers, "tp_encoders");

    model_->e_after_norm_w = tensors.at("encoder.after_norm.weight");
    model_->e_after_norm_b = tensors.at("encoder.after_norm.bias");
    model_->e_tp_norm_w = tensors.at("encoder.tp_norm.weight");
    model_->e_tp_norm_b = tensors.at("encoder.tp_norm.bias");

    return true;
  } catch (...) {
    RS_LOG_ERR("Tensor mapping failed for SenseVoice encoder.");
    return false;
  }
}

bool SenseVoiceEncoderModel::SetLayerWeights(
    std::vector<SenseVoiceLayerEncoder> &layers,
    std::map<std::string, struct ggml_tensor *> &tensors, int n_layers,
    const std::string &prefix) {
  layers.resize(n_layers);
  for (int i = 0; i < n_layers; ++i) {
    std::string p = "encoder." + prefix + "." + std::to_string(i);
    layers[i].e_attn_ln_out_w = tensors.at(p + ".self_attn.linear_out.weight");
    layers[i].e_attn_ln_out_b = tensors.at(p + ".self_attn.linear_out.bias");
    layers[i].e_attn_ln_q_w = tensors.at(p + ".self_attn.linear_q.weight");
    layers[i].e_attn_ln_q_b = tensors.at(p + ".self_attn.linear_q.bias");
    layers[i].e_attn_ln_k_w = tensors.at(p + ".self_attn.linear_k.weight");
    layers[i].e_attn_ln_k_b = tensors.at(p + ".self_attn.linear_k.bias");
    layers[i].e_attn_ln_v_w = tensors.at(p + ".self_attn.linear_v.weight");
    layers[i].e_attn_ln_v_b = tensors.at(p + ".self_attn.linear_v.bias");
    layers[i].e_attn_fsmn_w = tensors.at(p + ".self_attn.fsmn_block.weight");
    layers[i].e_mlp_w1 = tensors.at(p + ".feed_forward.w_1.weight");
    layers[i].e_mlp_b1 = tensors.at(p + ".feed_forward.w_1.bias");
    layers[i].e_mlp_w2 = tensors.at(p + ".feed_forward.w_2.weight");
    layers[i].e_mlp_b2 = tensors.at(p + ".feed_forward.w_2.bias");
    layers[i].e_norm_w1 = tensors.at(p + ".norm1.weight");
    layers[i].e_norm_b1 = tensors.at(p + ".norm1.bias");
    layers[i].e_norm_w2 = tensors.at(p + ".norm2.weight");
    layers[i].e_norm_b2 = tensors.at(p + ".norm2.bias");
  }
  return true;
}

void SenseVoiceEncoderModel::ensure_pos_encoding_size(int required_len, int dim) {
    // If the cache is already large enough, do nothing
    if (required_len <= max_pos_len_ && !cached_pos_encoding_.empty()) {
        return;
    }

    // Allocate slightly more than needed to prevent frequent resizing (e.g., +1024 frames)
    int target_len = std::max(required_len, max_pos_len_ + 1024);

    // Resize the cache vector
    // We store a single batch. If inference uses batch > 1, we reuse this data.
    cached_pos_encoding_.resize(target_len * dim);

    // Pre-compute sinusoidal positional encodings
    // This logic replaces the O(N*D) math operations in the hot path
    for (int k = 1; k <= target_len; k++) {
        for (int i = 0; i < dim / 2; i++) {
            float freq = pow(10000.0f, -2.0f * i / dim);

            // Store interleaved sin/cos
            cached_pos_encoding_[(k - 1) * dim + i] = sinf(k * freq);
            cached_pos_encoding_[(k - 1) * dim + i + dim / 2] = cosf(k * freq);
        }
    }

    max_pos_len_ = target_len;
}

/**
 * Build and execute the Encoder computation graph.
 * 4 Prompt Tokens + Mel Features -> SANM Encoders -> Parallel Encoders ->
 * Hidden States.
 */
bool SenseVoiceEncoderModel::Encode(const std::vector<float> &input_frames,
                                    RSState &state,
                                    ggml_backend_sched_t sched) {
  auto &sv_state = static_cast<SenseVoiceState &>(state);
  int n_len = input_frames.size() / hparams_.feats_dim;
  int hidden = hparams_.n_encoder_hidden_state;

  // 1. Optimization: Pre-check and expand positional encoding cache
  // If embedding is used, the sequence length is effectively n_len + 4
  int required_pos_len = model_->embedding ? n_len + 4 : n_len;
  ensure_pos_encoding_size(required_pos_len, hparams_.feats_dim);

  struct ggml_context *ctx0 = nullptr;
  struct ggml_cgraph *gf = nullptr;

  // Initialize context. Ensure SENSE_VOICE_model_MAX_NODES is large enough.
  if (!init_compute_ctx(&ctx0, &gf, SENSE_VOICE_model_MAX_NODES)) {
    return false;
  }

  // --- Define Input Tensors ---
  struct ggml_tensor *feature =
      ggml_new_tensor_2d(ctx0, GGML_TYPE_F32, hparams_.feats_dim, n_len);

  struct ggml_tensor *prompt_ids =
      ggml_new_tensor_2d(ctx0, GGML_TYPE_I32, 4, 1);

  // Note: 'required_pos_len' is used here for the dimension
  struct ggml_tensor *position =
      ggml_new_tensor_3d(ctx0, GGML_TYPE_F32, hparams_.feats_dim, required_pos_len, 1);

  ggml_set_name(feature, "feats");
  ggml_set_name(prompt_ids, "prompt_ids");
  ggml_set_name(position, "position");

  ggml_set_input(feature);
  ggml_set_input(prompt_ids);
  ggml_set_input(position);

  // --- Graph Construction ---
  struct ggml_tensor *cur = feature;
  if (model_->embedding) {
    struct ggml_tensor *emb =
        ggml_get_rows(ctx0, model_->embedding, prompt_ids);
    ggml_set_name(emb, "embedding");

    // Repeat the embedding to match dimensions
    emb = ggml_repeat(
        ctx0, emb,
        ggml_new_tensor_3d(ctx0, GGML_TYPE_I32, emb->ne[0], emb->ne[1], 1));

    cur = ggml_concat(ctx0, emb, feature, 1);
  }

  cur = ggml_scale(ctx0, cur, sqrtf(hidden));
  cur = ggml_add(ctx0, position, cur);

  // Forward pass: Main Encoder Layers
  cur = model_layer_sanm_forward(hparams_, ctx0, cur, model_->encoder0);
  for (int i = 0; i < hparams_.n_encoder_layers - 1; ++i) {
    cur = model_layer_sanm_forward(hparams_, ctx0, cur,
                                   model_->encoders_layer[i]);
  }

  // Normalization and projection
  cur = ggml_norm(ctx0, cur, hparams_.eps);
  cur = ggml_add(ctx0, ggml_mul(ctx0, cur, model_->e_after_norm_w),
                 model_->e_after_norm_b);

  // Forward pass: TP Encoder Layers
  for (int i = 0; i < hparams_.n_tp_encoder_layers; ++i) {
    cur = model_layer_sanm_forward(hparams_, ctx0, cur,
                                   model_->tp_encoders_layer[i]);
  }

  // Final Normalization
  cur = ggml_norm(ctx0, cur, hparams_.eps);
  cur = ggml_add(ctx0, ggml_mul(ctx0, cur, model_->e_tp_norm_w),
                 model_->e_tp_norm_b);

  ggml_set_name(cur, "encoder_out");
  ggml_set_output(cur);
  ggml_build_forward_expand(gf, cur);

  // --- Allocation ---
  if (!ggml_backend_sched_alloc_graph(sched, gf)) {
    // Allocation failed (likely OOM or node limit exceeded)
    ggml_free(ctx0);
    return false;
  }

  // --- Set Tensor Data (Optimized) ---

  // 1. Set Feature Data
  ggml_backend_tensor_set(feature, input_frames.data(), 0,
                          input_frames.size() * sizeof(float));

  // 2. Set Prompt IDs
  if (model_->embedding) {
    // Use a stack array instead of dynamic allocation for better performance
    int p_tokens[4] = {sv_state.language_id, 1, 2, sv_state.use_itn ? 14 : 15};
    ggml_backend_tensor_set(prompt_ids, p_tokens, 0, sizeof(p_tokens));
  }

  // 3. Set Positional Encoding (Key Optimization)
  // Instead of recalculating sin/cos in a triple loop, copy from pre-computed cache.
  int p_dim = hparams_.feats_dim;
  size_t bytes_per_batch = (size_t)required_pos_len * p_dim * sizeof(float);

  // Handle batching (though typically batch=1 for inference)
  int n_batch = position->ne[2];

  if (n_batch == 1) {
      // Single batch copy
      ggml_backend_tensor_set(position, cached_pos_encoding_.data(), 0, bytes_per_batch);
  } else {
      // If batch > 1, broadcast the same positional encoding to all batches
      for (int b = 0; b < n_batch; ++b) {
          ggml_backend_tensor_set(position, cached_pos_encoding_.data(),
                                  b * bytes_per_batch, bytes_per_batch);
      }
  }

  // --- Compute ---
  // ggml_backend_sched_set_eval_callback(sched, ggml_debug, new callback_data());
  if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
    ggml_free(ctx0);
    return false;
  }

  // --- State Persistence Logic ---
  // Ensure the output tensor in the state matches the current output dimensions
  if (sv_state.encoder_out == nullptr ||
      sv_state.encoder_out->ne[0] != cur->ne[0] ||
      sv_state.encoder_out->ne[1] != cur->ne[1]) {

    if (sv_state.buffer_persistent) {
      ggml_backend_buffer_free(sv_state.buffer_persistent);
    }

    // Allocate persistent tensor
    sv_state.encoder_out = ggml_new_tensor_2d(
        sv_state.ctx_persistent, cur->type, cur->ne[0], cur->ne[1]);

    ggml_backend_t primary_backend = ggml_backend_sched_get_backend(sched, 0);
    sv_state.buffer_persistent = ggml_backend_alloc_buffer(
        primary_backend, ggml_nbytes(sv_state.encoder_out));

    void *base_addr = ggml_backend_buffer_get_base(sv_state.buffer_persistent);
    ggml_backend_tensor_alloc(sv_state.buffer_persistent, sv_state.encoder_out,
                              base_addr);
  }

  // Copy result to persistent state
  ggml_backend_tensor_copy(cur, sv_state.encoder_out);

  ggml_free(ctx0);
  return true;
}