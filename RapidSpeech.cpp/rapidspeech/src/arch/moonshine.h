#pragma once

#include "core/rs_context.h"
#include "core/rs_model.h"
#include <vector>
#include <string>
#include <unordered_map>
#include <random>

/**
 * Moonshine model hyperparameters (from GGUF metadata).
 * Supports both non-streaming (tiny/base) and streaming variants.
 */
struct MoonshineHParams {
    // Encoder
    int encoder_dim = 288;       // tiny=288, base=416
    int encoder_depth = 6;       // tiny=6, base=8
    int encoder_heads = 8;
    int encoder_head_dim = 36;   // encoder_dim / encoder_heads

    // Decoder
    int decoder_dim = 288;
    int decoder_depth = 6;
    int decoder_heads = 8;
    int decoder_head_dim = 36;

    // Vocabulary
    int vocab_size = 32768;
    int bos_id = 1;
    int eos_id = 2;
    int max_seq_len = 448;

    // Audio frontend
    int sample_rate = 16000;

    // Streaming-specific
    bool is_streaming = false;
    int frame_len = 80;          // samples per frame
    int total_lookahead = 16;    // encoder lookahead frames
    int d_model_frontend = 288;
    int c1 = 576;                // conv1 output channels
    int c2 = 288;                // conv2 output channels

    // RoPE
    float rope_theta = 10000.0f;
    float partial_rotary_factor = 0.9f;
};

/**
 * Whisper-style generation config for advanced decoding strategies.
 * Controls suppress tokens, no-repeat n-gram blocking, and
 * temperature fallback with quality checks.
 */
struct MoonshineGenerationConfig {
    // Tokens to suppress (set logit to -inf). Default: {0} (pad/blank).
    std::vector<int> suppress_tokens = {0};

    // No-repeat n-gram: if > 0, any n-gram of this size that already
    // appeared in the generated sequence is forbidden.
    int no_repeat_ngram_size = 3;

    // Temperature fallback schedule.
    // Decode is attempted with each temperature in order.
    // temperature=0 means greedy argmax; >0 means softmax sampling.
    std::vector<float> temperature_fallback = {0.0f, 0.2f, 0.4f, 0.6f, 0.8f, 1.0f};

    // Quality thresholds for accepting a decode attempt.
    // If compression_ratio > threshold OR avg_logprob < threshold,
    // the attempt is rejected and the next temperature is tried.
    float compression_ratio_threshold = 2.4f;
    float logprob_threshold = -1.0f;

    // Max tokens to generate (overrides hparams.max_seq_len if > 0).
    int max_new_tokens = 0;
};

// ============================================================
// Pre-allocated KV cache for decoder graph reuse.
// All tensors are pre-allocated to max_seq_len and live in a
// persistent ggml_context + backend buffer.  The decoder graph
// uses ggml_view to read/write at the current position.
// ============================================================

/**
 * Persistent decoder KV cache.
 * Allocated once per Decode() call, reused across all steps.
 */
struct MoonshineDecoderCache {
    // Persistent ggml context that owns the cache tensors.
    ggml_context* ctx = nullptr;
    ggml_backend_buffer_t buf = nullptr;

    // Per-layer self-attention KV: [head_dim, n_heads, max_seq_len]
    std::vector<ggml_tensor*> self_k;  // one per decoder layer
    std::vector<ggml_tensor*> self_v;

    // Per-layer cross-attention KV: [head_dim, n_heads, max_enc_frames]
    // Allocated to a fixed max size; actual enc_frames may be smaller.
    std::vector<ggml_tensor*> cross_k;
    std::vector<ggml_tensor*> cross_v;

    int self_seq_len = 0;       // current number of cached self-attn tokens
    bool cross_valid = false;   // true after first step computes cross KV
    int max_enc_frames = 0;     // allocated cross KV capacity

    void Free() {
        if (buf) { ggml_backend_buffer_free(buf); buf = nullptr; }
        if (ctx) { ggml_free(ctx); ctx = nullptr; }
        self_k.clear(); self_v.clear();
        cross_k.clear(); cross_v.clear();
        self_seq_len = 0; cross_valid = false;
    }
    ~MoonshineDecoderCache() { Free(); }
};

/**
 * Runtime state for Moonshine inference.
 */
struct MoonshineState : public RSState {
    // Encoder hidden states output
    std::vector<float> encoder_out;
    int encoder_frames = 0;

    // Persistent decoder KV cache (allocated in Decode, freed on reset)
    MoonshineDecoderCache dec_cache;

    // Generated token sequence
    std::vector<int> tokens;

    // Per-step log probabilities (for quality checking)
    std::vector<float> token_logprobs;
    float sum_logprob = 0.0f;

    // Streaming encoder state
    std::vector<float> sample_buffer;       // raw PCM accumulator
    std::vector<float> conv1_state;         // conv1 overlap buffer
    std::vector<float> conv2_state;         // conv2 overlap buffer
    int streaming_enc_frames = 0;           // total encoder frames emitted so far

    // Transcription result
    std::string text_result;
};

/**
 * Moonshine encoder layer weights.
 */
struct MoonshineEncoderLayer {
    // Fused QKV (legacy, may be nullptr if model uses separate Q/K/V)
    struct ggml_tensor* attn_qkv_weight = nullptr;
    struct ggml_tensor* attn_qkv_bias = nullptr;
    // Separate Q/K/V (used by Moonshine HuggingFace models)
    struct ggml_tensor* attn_q_weight = nullptr;
    struct ggml_tensor* attn_k_weight = nullptr;
    struct ggml_tensor* attn_v_weight = nullptr;
    struct ggml_tensor* attn_out_weight = nullptr;
    struct ggml_tensor* attn_out_bias = nullptr;
    struct ggml_tensor* attn_norm_weight = nullptr;
    struct ggml_tensor* attn_norm_bias = nullptr;
    struct ggml_tensor* ff_up_weight = nullptr;
    struct ggml_tensor* ff_up_bias = nullptr;
    struct ggml_tensor* ff_down_weight = nullptr;
    struct ggml_tensor* ff_down_bias = nullptr;
    struct ggml_tensor* ff_norm_weight = nullptr;
    struct ggml_tensor* ff_norm_bias = nullptr;
};

/**
 * Moonshine decoder layer weights.
 */
struct MoonshineDecoderLayer {
    // Self-attention
    struct ggml_tensor* self_attn_q_weight = nullptr;
    struct ggml_tensor* self_attn_k_weight = nullptr;
    struct ggml_tensor* self_attn_v_weight = nullptr;
    struct ggml_tensor* self_attn_out_weight = nullptr;
    struct ggml_tensor* self_attn_norm_weight = nullptr;
    struct ggml_tensor* self_attn_norm_bias = nullptr;
    // Cross-attention
    struct ggml_tensor* cross_attn_q_weight = nullptr;
    struct ggml_tensor* cross_attn_k_weight = nullptr;
    struct ggml_tensor* cross_attn_v_weight = nullptr;
    struct ggml_tensor* cross_attn_out_weight = nullptr;
    struct ggml_tensor* cross_attn_norm_weight = nullptr;
    struct ggml_tensor* cross_attn_norm_bias = nullptr;
    // Feed-forward
    struct ggml_tensor* ff_up_weight = nullptr;
    struct ggml_tensor* ff_up_bias = nullptr;
    struct ggml_tensor* ff_down_weight = nullptr;
    struct ggml_tensor* ff_down_bias = nullptr;
    struct ggml_tensor* ff_norm_weight = nullptr;
    struct ggml_tensor* ff_norm_bias = nullptr;
};

/**
 * All Moonshine model weights.
 */
struct MoonshineWeights {
    // Audio frontend: 3 x 1D conv + groupnorm
    struct ggml_tensor* frontend_conv1_weight = nullptr;
    struct ggml_tensor* frontend_conv1_bias = nullptr;   // may be null
    struct ggml_tensor* frontend_conv2_weight = nullptr;
    struct ggml_tensor* frontend_conv2_bias = nullptr;
    struct ggml_tensor* frontend_conv3_weight = nullptr;  // was "linear"
    struct ggml_tensor* frontend_conv3_bias = nullptr;
    struct ggml_tensor* frontend_groupnorm_weight = nullptr;
    struct ggml_tensor* frontend_groupnorm_bias = nullptr;
    std::vector<MoonshineEncoderLayer> encoder_layers;
    struct ggml_tensor* encoder_final_norm_weight = nullptr;
    struct ggml_tensor* encoder_final_norm_bias = nullptr;
    std::vector<MoonshineDecoderLayer> decoder_layers;
    struct ggml_tensor* decoder_final_norm_weight = nullptr;
    struct ggml_tensor* decoder_final_norm_bias = nullptr;
    struct ggml_tensor* token_embedding = nullptr;
    struct ggml_tensor* lm_head_weight = nullptr;
    struct ggml_tensor* lm_head_bias = nullptr;
};

/**
 * Moonshine ASR model — ggml native implementation.
 * Encoder-decoder transformer with RoPE.
 * Features:
 *   - Pre-allocated KV cache with graph reuse across decoder steps
 *   - Causal streaming encoder with conv frontend state
 *   - Cross K/V computed once and cached
 */
class MoonshineModel : public ISpeechModel {
public:
    MoonshineModel();
    ~MoonshineModel() override;

    bool Load(const std::unique_ptr<rs_context_t>& ctx,
              ggml_backend_t backend) override;
    std::shared_ptr<RSState> CreateState() override;
    bool Encode(const std::vector<float>& input_frames, RSState& state,
                ggml_backend_sched_t sched) override;
    bool Decode(RSState& state, ggml_backend_sched_t sched) override;
    std::string GetTranscription(RSState& state) override;
    const RSModelMeta& GetMeta() const override { return meta_; }

    // Generation config (public so callers can customize before Decode).
    MoonshineGenerationConfig gen_config;

    // Streaming: push audio chunk, returns number of new encoder frames.
    // Uses causal conv frontend + causal encoder attention.
    int PushStreamingAudio(RSState& state, const float* audio, int n_samples,
                           ggml_backend_sched_t sched);

private:
    RSModelMeta meta_;
    MoonshineHParams hparams_;
    MoonshineWeights weights_;
    std::unordered_map<int, std::string> vocab_;

    bool MapTensors(ggml_context* gguf_data);
    bool LoadVocab(gguf_context* ctx_gguf);

    // Allocate persistent KV cache tensors in state.dec_cache.
    bool AllocDecoderCache(MoonshineState& ms, int max_enc_frames,
                           ggml_backend_t backend);

    // Compute cross K/V projections and write into dec_cache.
    // Called once at the start of Decode.
    bool ComputeCrossKV(MoonshineState& ms, ggml_backend_sched_t sched);

    // Build and run a single decoder step.  Uses pre-allocated KV cache
    // via ggml_view; the graph structure is identical for every step
    // (only input data changes), enabling future graph caching.
    // Writes raw logits into logit_out buffer. Returns false on error.
    bool RunDecoderStep(MoonshineState& ms, int step, int cur_token,
                        ggml_backend_sched_t sched,
                        std::vector<float>& logit_out);

    // --- Logit processing & sampling ---

    // Apply suppress_tokens: set logits[id] = -inf for each suppressed id.
    void ApplySuppressTokens(std::vector<float>& logits,
                             const std::vector<int>& suppress_ids);

    // Apply no-repeat n-gram blocking on logits given generated tokens so far.
    void ApplyNoRepeatNgram(std::vector<float>& logits,
                            const std::vector<int>& tokens,
                            int ngram_size);

    // Sample from logits with given temperature.
    // temperature=0 -> greedy argmax. >0 -> softmax + multinomial.
    // Returns (token_id, log_probability).
    std::pair<int, float> SampleToken(const std::vector<float>& logits,
                                      float temperature,
                                      std::mt19937& rng);

    // Compute compression ratio of token sequence (unique bigrams heuristic).
    float ComputeCompressionRatio(const std::vector<int>& tokens);

    // Run a single decode attempt with given temperature.
    // Returns: 1 = success (quality ok), 0 = quality rejected, -1 = compute error.
    int DecodeWithTemperature(MoonshineState& ms, ggml_backend_sched_t sched,
                              float temperature, ggml_backend_t backend);

    // Build encoder computation graph
    // out_positions: if non-null, receives the position tensor that caller must fill
    ggml_tensor* BuildEncoder(ggml_context* ctx0, ggml_tensor* audio_features,
                              int n_frames, bool causal,
                              ggml_tensor** out_positions = nullptr);

    // Build encoder with causal mask for streaming
    ggml_tensor* BuildCausalEncoder(ggml_context* ctx0,
                                     ggml_tensor* audio_features,
                                     int n_new_frames, int n_prev_frames);

    // RoPE helper (encoder only; decoder uses inline rope with position tensor)
    ggml_tensor* ApplyRoPE(ggml_context* ctx0, ggml_tensor* x,
                           int head_dim, int offset);
};
