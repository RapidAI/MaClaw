#include "sensevoice.h"
#include "core/rs_context.h"
#include "ctc_decoder.h"
#include "ggml-backend.h"
#include "ggml-cpu.h"
#include "ggml.h"
#include "utils/rs_log.h"
#include <cmath>
#include <cstring>
#include <functional>

#include "gguf.h"
#include "utils/debug_utils.h"
#include "utils/rs_wav.h"

// Increased node limit to handle deep SenseVoice graphs (50+ layers)
#define SENSE_VOICE_ENCODER_MAX_NODES 6144
#define SENSE_VOICE_DECODER_MAX_NODES 128

// --- Internal Mathematical Helpers ---

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

// --- SenseVoiceModel Implementation ---

SenseVoiceModel::SenseVoiceModel() : ctx_weights_(nullptr) {
  encoder_ = std::make_unique<SenseVoiceEncoderModel>();
  decoder_ = std::make_unique<SenseVoiceDecoder>();
}

SenseVoiceModel::~SenseVoiceModel() {
  if (ctx_weights_) {
    ggml_free(ctx_weights_);
    ctx_weights_ = nullptr;
  }
}

bool SenseVoiceModel::Load(const std::unique_ptr<rs_context_t> &ctx,
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

  meta_.arch_name = "SenseVoiceSmall";
  meta_.audio_sample_rate = 16000;
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

bool SenseVoiceModel::MapTensors(
    std::map<std::string, struct ggml_tensor *> &tensors) {
  try {
    if (!encoder_->MapTensors(tensors)) {
      RS_LOG_ERR("Tensor mapping failed for SenseVoice encoder");
    };
    decoder_->ctc_out_linear_weight = tensors.at("ctc.ctc_lo.weight");
    decoder_->ctc_out_linear_bias = tensors.at("ctc.ctc_lo.bias");
    return true;
  } catch (...) {
    RS_LOG_ERR("Tensor mapping failed for SenseVoice.");
    return false;
  }
}

std::shared_ptr<RSState> SenseVoiceModel::CreateState() {
  return std::make_shared<SenseVoiceState>();
}

bool SenseVoiceModel::Encode(const std::vector<float> &input_frames,
                             RSState &state, ggml_backend_sched_t sched) {
  return encoder_->Encode(input_frames, state, sched);
}


/**
 * Enhanced Decode function supporting Greedy and Beam Search.
 */
bool
SenseVoiceModel::Decode(RSState &state, ggml_backend_sched_t sched) {
  auto &sv_state = static_cast<SenseVoiceState &>(state);
  if (!sv_state.encoder_out)
    return false;

  int T = sv_state.encoder_out->ne[1];
  int V = hparams_.n_vocab;
  int beam_size = 1; // You can pull this from params later

  struct ggml_context *ctx0 =
      ggml_init({256 * ggml_tensor_overhead(), nullptr, true});
  struct ggml_cgraph *gf =
      ggml_new_graph_custom(ctx0, SENSE_VOICE_DECODER_MAX_NODES, false);

  struct ggml_tensor *encoder_in = ggml_new_tensor_2d(
      ctx0, sv_state.encoder_out->type, sv_state.encoder_out->ne[0],
      sv_state.encoder_out->ne[1]);
  ggml_set_input(encoder_in);

  // 1. Linear projection to vocab size
  struct ggml_tensor *cur =
      ggml_mul_mat(ctx0, decoder_->ctc_out_linear_weight, encoder_in);
  cur = ggml_add(ctx0, cur, decoder_->ctc_out_linear_bias);

  // 2. Compute log-probabilities
  struct ggml_tensor *log_probs = ggml_log(ctx0, ggml_soft_max(ctx0, cur));

  struct ggml_tensor *output_node = nullptr;

  if (beam_size <= 1) {
    // Greedy serach: Calculate argmax on backend
    output_node = ggml_argmax(ctx0, log_probs);
  } else {
    // Beam Search Mode: We need the full log-probs on host
    output_node = log_probs;
  }
  ggml_set_name(output_node, "output");

  ggml_set_output(output_node);
  ggml_build_forward_expand(gf, output_node);

  if (!ggml_backend_sched_alloc_graph(sched, gf)) {
    ggml_free(ctx0);
    return false;
  }

  ggml_backend_tensor_copy(sv_state.encoder_out, encoder_in);

  if (ggml_backend_sched_graph_compute(sched, gf) != GGML_STATUS_SUCCESS) {
    ggml_free(ctx0);
    return false;
  }

  // 3. Post-Processing on Host
  if (beam_size <= 1) {
    // --- Greedy Decoding ---
    std::vector<int32_t> raw_ids(T);
    ggml_backend_tensor_get(output_node, raw_ids.data(), 0,
                            T * sizeof(int32_t));

    // Use CTCDecoder to collapse repeats and remove blanks
    sv_state.ids = CTCDecoder::GreedyDecode(raw_ids.data(), T);
  } else {
    // --- Beam Search Decoding ---
    std::vector<float> host_log_probs(T * V);
    ggml_backend_tensor_get(output_node, host_log_probs.data(), 0,
                            T * V * sizeof(float));

    sv_state.ids =
        CTCDecoder::BeamSearchDecode(host_log_probs.data(), T, V, beam_size);
  }
  for (auto id : sv_state.ids) {
    sv_state.tokens.push_back(this->vocab_.id_to_token[id]);
  }

  ggml_free(ctx0);
  return true;
}

std::string SenseVoiceModel::GetTranscription(RSState &state) {
  auto &sv_state = static_cast<SenseVoiceState &>(state);
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
struct SenseVoiceRegistrar {
  SenseVoiceRegistrar() {
    rs_register_model_arch("SenseVoiceSmall", []() {
      return std::make_shared<SenseVoiceModel>();
    });
  }
} global_sensevoice_reg;
} // namespace