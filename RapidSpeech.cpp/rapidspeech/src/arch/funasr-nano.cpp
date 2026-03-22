#include "funasr-nano.h"
#include "core/rs_context.h"
#include "ctc_decoder.h"
#include "ggml-backend.h"
#include "ggml.h"
#include "gguf.h"
#include "sensevoice.h"
#include "utils/debug_utils.h"
#include "utils/rs_log.h"
#include "utils/rs_wav.h"
#include <functional>

#include "ggml-cpu.h"

// Increased node limit to handle deep FunASRNano graphs (50+ layers)
#define FUNASR_NANO_ENCODER_MAX_NODES 6144
#define FUNASR_NANO_DECODER_MAX_NODES 1024

struct FunASRNanoState : public RSState {
  struct ggml_context *ctx_persistent = nullptr;
  ggml_backend_buffer_t buffer_persistent = nullptr;
  struct ggml_tensor *encoder_out = nullptr;

  std::vector<int32_t> ids;
  std::vector<std::string> tokens;
  int language_id = 0;

  FunASRNanoState() {
    // Increase persistent context to ensure enough room for tensor metadata
    struct ggml_init_params params = {512 * ggml_tensor_overhead(), nullptr,
                                      true};
    ctx_persistent = ggml_init(params);
  }

  ~FunASRNanoState() {
    if (buffer_persistent)
      ggml_backend_buffer_free(buffer_persistent);
    if (ctx_persistent)
      ggml_free(ctx_persistent);
  }
};

// --- FunASRNanoModel Implementation ---

FunASRNanoModel::FunASRNanoModel() : ctx_weights_(nullptr) {
  encoder_ = std::make_unique<SenseVoiceEncoderModel>();
  decoder_ = std::make_unique<FunASRNanoTransformerDecoder>();
}

FunASRNanoModel::~FunASRNanoModel() {
  if (ctx_weights_) {
    ggml_free(ctx_weights_);
    ctx_weights_ = nullptr;
  }
}

bool FunASRNanoModel::Load(const std::unique_ptr<rs_context_t> &ctx,
                           ggml_backend_t backend) {
  if (!ctx || !ctx->ctx_gguf || !ctx->gguf_data) {
    RS_LOG_ERR("Invalid context provided to FunASRNanoModel::Load");
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
  hparams_.n_ctc_layers =
      gguf_get_val_i32(ctx_gguf, gguf_find_key(ctx_gguf, "ctc.n_layer"));
  hparams_.ctc_downsample_rate =
      gguf_get_val_i32(ctx_gguf, gguf_find_key(ctx_gguf, "ctc.downsample_rate"));
  hparams_.ctc_encoder_dim =
      gguf_get_val_i32(ctx_gguf, gguf_find_key(ctx_gguf, "ctc.encoder_dim"));
  hparams_.ctc_llm_dim =
      gguf_get_val_i32(ctx_gguf, gguf_find_key(ctx_gguf, "ctc.llm_dim"));
  hparams_.ctc_ffn_dim =
      gguf_get_val_i32(ctx_gguf, gguf_find_key(ctx_gguf, "ctc.ffn_dim"));
  hparams_.n_mels =
      gguf_get_val_i32(ctx_gguf, gguf_find_key(ctx_gguf, "frontend.num_mels"));

  meta_.arch_name = "FunASRNano";
  meta_.audio_sample_rate = gguf_get_val_i32(
      ctx_gguf, gguf_find_key(ctx_gguf, "frontend.sample_rate"));
  meta_.n_mels = hparams_.n_mels;
  meta_.vocab_size = hparams_.n_vocab;

  // 2. Load Vocabulary
  const int token_idx = gguf_find_key(ctx_gguf, "tokenizer.ggml.tokens");
  if (token_idx != -1) {
    int n_vocab = gguf_get_arr_n(ctx_gguf, token_idx);
    for (int i = 0; i < n_vocab; i++) {
        vocab_.id_to_token[i] = gguf_get_arr_str(ctx_gguf, token_idx, i);
    }
  }

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

bool FunASRNanoModel::MapTensors(
    std::map<std::string, struct ggml_tensor *> &tensors) {
  try {
    encoder_->MapTensors(tensors);

    // ctc decoder
    decoder_->decoders_layer.resize(hparams_.n_ctc_layers);

    decoder_->linear1_weight = tensors.at("ctc_decoder.linear1.weight");
    decoder_->linear1_bias = tensors.at("ctc_decoder.linear1.bias");
    decoder_->linear2_weight = tensors.at("ctc_decoder.linear2.weight");
    decoder_->linear2_bias = tensors.at("ctc_decoder.linear2.bias");

    for (int i = 0; i < hparams_.n_ctc_layers; ++i) {
      // ctc_decoder.blocks.4.self_attn.linear_q.weight
      std::string p = "ctc_decoder.blocks." + std::to_string(i);
      decoder_->decoders_layer[i].self_attn_linear_q_weight =
          tensors.at(p + ".self_attn.linear_q.weight");
      decoder_->decoders_layer[i].self_attn_linear_q_bias =
          tensors.at(p + ".self_attn.linear_q.bias");
      decoder_->decoders_layer[i].self_attn_linear_k_weight =
          tensors.at(p + ".self_attn.linear_k.weight");
      decoder_->decoders_layer[i].self_attn_linear_k_bias =
          tensors.at(p + ".self_attn.linear_k.bias");
      decoder_->decoders_layer[i].self_attn_linear_v_weight =
          tensors.at(p + ".self_attn.linear_v.weight");
      decoder_->decoders_layer[i].self_attn_linear_v_bias =
          tensors.at(p + ".self_attn.linear_v.bias");
      decoder_->decoders_layer[i].self_attn_linear_out_weight =
          tensors.at(p + ".self_attn.linear_out.weight");
      decoder_->decoders_layer[i].self_attn_linear_out_bias =
          tensors.at(p + ".self_attn.linear_out.bias");
      decoder_->decoders_layer[i].feed_forward_w_1_weight =
          tensors.at(p + ".feed_forward.w_1.weight");
      decoder_->decoders_layer[i].feed_forward_w_1_bias =
          tensors.at(p + ".feed_forward.w_1.bias");
      decoder_->decoders_layer[i].feed_forward_w_2_weight =
          tensors.at(p + ".feed_forward.w_2.weight");
      decoder_->decoders_layer[i].feed_forward_w_2_bias =
          tensors.at(p + ".feed_forward.w_2.bias");
      decoder_->decoders_layer[i].norm1_weight =
          tensors.at(p + ".norm1.weight");
      decoder_->decoders_layer[i].norm1_bias = tensors.at(p + ".norm1.bias");
      decoder_->decoders_layer[i].norm2_weight =
          tensors.at(p + ".norm2.weight");
      decoder_->decoders_layer[i].norm2_bias = tensors.at(p + ".norm2.bias");
    }

    decoder_->ctc_out_linear_weight = tensors.at("ctc.ctc_lo.weight");
    decoder_->ctc_out_linear_bias = tensors.at("ctc.ctc_lo.bias");

    return true;
  } catch (...) {
    RS_LOG_ERR("Tensor mapping failed for FunASRNano.");
    return false;
  }
}

std::shared_ptr<RSState> FunASRNanoModel::CreateState() {
  return std::make_shared<FunASRNanoState>();
}

/**
 * Build and execute the Encoder computation graph.
 * 4 Prompt Tokens + Mel Features -> SANM Encoders -> Parallel Encoders ->
 * Hidden States.
 */
bool FunASRNanoModel::Encode(const std::vector<float> &input_frames,
                             RSState &state, ggml_backend_sched_t sched) {
  return encoder_->Encode(input_frames, state, sched);
}

static struct ggml_tensor *
decoder_forward(const FunASRNanoHParams &hparams, struct ggml_context *ctx,
                    struct ggml_tensor *cur,
                    FunASRNanoTransformerDecoder &layers){
    // --- 1. Downsampling & Initial Projection ---
    // PyTorch: x.view(batch, chunk_num, dim * k)
    const int k = hparams.ctc_downsample_rate;
    const int encoder_dim = cur->ne[0];
    const int seq_len = cur->ne[1];
    const int batch_size = cur->ne[2];
    const int chunk_num = (seq_len + k - 1) / k;

    // Reshape to flatten the downsample dimension into the feature dimension
    // Result shape: [encoder_dim * k, chunk_num, batch_size]
    cur = ggml_reshape_3d(ctx, ggml_cont(ctx, cur), encoder_dim * k, chunk_num, batch_size);

    // Linear 1 + ReLU
    cur = ggml_mul_mat(ctx, layers.linear1_weight, cur);
    cur = ggml_add(ctx, cur, layers.linear1_bias);
    cur = ggml_relu_inplace(ctx, cur);

    // Linear 2 -> Result shape: [llm_dim, chunk_num, batch_size]
    cur = ggml_mul_mat(ctx, layers.linear2_weight, cur);
    cur = ggml_add(ctx, cur, layers.linear2_bias);

    // --- 2. Transformer Encoder Blocks Loop ---
    const int llm_dim = cur->ne[0];
    const int n_head = hparams.ctc_attention_heads;
    const int d_k = llm_dim / n_head;
    const float scale = 1.0f / sqrtf((float)d_k);

    for (auto layer: layers.decoders_layer) {
        // --- Sub-layer 1: Multi-Head Attention ---
        struct ggml_tensor * block_input = cur;
        // Pre-Norm
        cur = ggml_norm(ctx, cur, hparams.eps);
        cur = ggml_add(ctx, ggml_mul(ctx, cur, layer.norm1_weight), layer.norm1_bias);

        // Q, K, V Projections
        struct ggml_tensor * q = ggml_add(ctx, ggml_mul_mat(ctx, layer.self_attn_linear_q_weight, cur), layer.self_attn_linear_q_bias);
        struct ggml_tensor * k_vec = ggml_add(ctx, ggml_mul_mat(ctx, layer.self_attn_linear_k_weight, cur), layer.self_attn_linear_k_bias);
        struct ggml_tensor * v = ggml_add(ctx, ggml_mul_mat(ctx, layer.self_attn_linear_v_weight, cur), layer.self_attn_linear_v_bias);

        // Reshape for MHA: [d_k, chunk_num, head, batch]
        q = ggml_permute(ctx, ggml_reshape_4d(ctx, q, d_k, n_head, chunk_num, batch_size), 0, 2, 1, 3);
        k_vec = ggml_permute(ctx, ggml_reshape_4d(ctx, k_vec, d_k, n_head, chunk_num, batch_size), 0, 2, 1, 3);
        v = ggml_permute(ctx, ggml_reshape_4d(ctx, v, d_k, n_head, chunk_num, batch_size), 0, 2, 1, 3);

        // Multi-head Attention Score: (Q * K^T) * scale
        // ggml_mul_mat(k, q) computes dot product over the first dimension (d_k)
        struct ggml_tensor * scores = ggml_mul_mat(ctx, k_vec, q);
        scores = ggml_scale_inplace(ctx, scores, scale);

        // Softmax
        struct ggml_tensor * probs = ggml_soft_max_inplace(ctx, scores);

        // 3. Calculate Context: [d_k, L, H, B] @ [L, L, H, B]
        // Problem: V's ne[0] is d_k, but Probs' ne[0] is L. They must match.
        // Solution: Transpose V so its ne[0] is L.
        struct ggml_tensor * v_T = ggml_cont(ctx, ggml_permute(ctx, v, 1, 0, 2, 3)); // [L, d_k, H, B]

        // Now multiply: [L, d_k, H, B] @ [L, L, H, B]
        // GGML reduces ne[0] (L).
        // Result ne[0] = Probs->ne[1] (L), ne[1] = v_T->ne[1] (d_k)
        struct ggml_tensor * context = ggml_mul_mat(ctx, v_T, probs); // [L, d_k, H, B]

        // 4. Final Transpose back to standard: [d_k, L, H, B]
        context = ggml_cont(ctx, ggml_permute(ctx, context, 0, 2, 1,3));
        context = ggml_reshape_3d(ctx, context, llm_dim, chunk_num, batch_size);

        // Output Projection & Residual Connection
        cur = ggml_mul_mat(ctx, layer.self_attn_linear_out_weight, context);
        cur = ggml_add(ctx, cur, layer.self_attn_linear_out_bias);
        cur = ggml_add(ctx, cur, block_input);

        // --- Sub-layer 2: Feed Forward Network (FFN) ---
        struct ggml_tensor * ffn_input = cur;

        // Pre-Norm
        cur = ggml_norm(ctx, cur, hparams.eps);
        cur = ggml_add(ctx, ggml_mul(ctx, cur, layer.norm2_weight), layer.norm2_bias);

        // FFN: Linear 1 -> ReLU -> Linear 2
        cur = ggml_mul_mat(ctx, layer.feed_forward_w_1_weight, cur);
        cur = ggml_add(ctx, cur, layer.feed_forward_w_1_bias);
        cur = ggml_relu_inplace(ctx, cur);

        cur = ggml_mul_mat(ctx, layer.feed_forward_w_2_weight, cur);
        cur = ggml_add(ctx, cur, layer.feed_forward_w_2_bias);

        // Residual Connection
        cur = ggml_add(ctx, cur, ffn_input);
    }

    return cur;


}

bool FunASRNanoModel::DecodeWithLLM(RSState &state,
                                    ggml_backend_sched_t sched) {
  return true;
};

bool FunASRNanoModel::DecodeWithoutLLM(RSState &state,
                                       ggml_backend_sched_t sched) {
  auto &sv_state = static_cast<SenseVoiceState &>(state);
  if (!sv_state.encoder_out)
    return false;

  int T = sv_state.encoder_out->ne[1];
  int V = hparams_.n_vocab;



  struct ggml_context *ctx0 =
      ggml_init({2 * 1024 * ggml_tensor_overhead(), nullptr, true});
  struct ggml_cgraph *gf =
      ggml_new_graph_custom(ctx0, FUNASR_NANO_DECODER_MAX_NODES, false);

  struct ggml_tensor *encoder_in = ggml_new_tensor_2d(
      ctx0, sv_state.encoder_out->type, sv_state.encoder_out->ne[0],
      sv_state.encoder_out->ne[1]);
  ggml_set_input(encoder_in);

  // transformer

    struct ggml_tensor *cur =  decoder_forward(hparams_, ctx0, encoder_in, *decoder_);

  // 1. Linear projection to vocab size
  cur = ggml_mul_mat(ctx0, decoder_->ctc_out_linear_weight, cur);
  cur = ggml_add(ctx0, cur, decoder_->ctc_out_linear_bias);

  // 2. Compute log-probabilities
  struct ggml_tensor *log_probs = ggml_log(ctx0, ggml_soft_max(ctx0, cur));

  struct ggml_tensor *output_node = nullptr;

  if (beam_size <= 1)
    // Greedy serach: Calculate argmax on backend
    output_node = ggml_argmax(ctx0, log_probs);

  ggml_set_name(output_node, "output");

  ggml_set_output(output_node);
  ggml_build_forward_expand(gf, output_node);

  if (!ggml_backend_sched_alloc_graph(sched, gf)) {
    ggml_free(ctx0);
    return false;
  }
  ggml_backend_tensor_copy(sv_state.encoder_out, encoder_in);
    // ggml_backend_sched_set_eval_callback(sched, ggml_debug, new callback_data());
  if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
    ggml_free(ctx0);
    return false;
  }
    // print_tensor(output_node);
  if (beam_size <= 1) {
    raw_ids.resize(T);
    ggml_backend_tensor_get(output_node, raw_ids.data(), 0,
                            T * sizeof(int32_t));
  } else {
    host_log_probs.resize(T * V);
    ggml_backend_tensor_get(output_node, host_log_probs.data(), 0,
                            T * V * sizeof(float));
  }
    sv_state.ids = CTCDecoder::GreedyDecode(raw_ids.data(), T);
  ggml_free(ctx0);
  return true;
};

/**
 * Enhanced Decode function supporting Greedy and Beam Search.
 */
bool FunASRNanoModel::Decode(RSState &state, ggml_backend_sched_t sched) {
    auto &sv_state = static_cast<SenseVoiceState &>(state);
    DecodeWithoutLLM(state, sched);
    for (auto id : sv_state.ids) {
        sv_state.tokens.push_back(this->vocab_.id_to_token[id]);
    }

  return true;
}

std::string FunASRNanoModel::GetTranscription(RSState &state) {
  auto &sv_state = static_cast<FunASRNanoState &>(state);
  std::string result;
  result.reserve(64); // ⭐ 关键：避免反复 realloc

  for (const auto &s : sv_state.tokens) {
    result += s;
  }
  sv_state.tokens.clear();
  return result;
}

// Registration logic
extern void
rs_register_model_arch(const std::string &arch,
                       std::function<std::shared_ptr<ISpeechModel>()> creator);
namespace {
struct FunASRNanoRegistrar {
  FunASRNanoRegistrar() {
    rs_register_model_arch(
        "FunASRNano", []() { return std::make_shared<FunASRNanoModel>(); });
  }
} global_FunASRNano_reg;
} // namespace