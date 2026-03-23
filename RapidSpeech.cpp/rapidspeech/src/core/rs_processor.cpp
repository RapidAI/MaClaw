#include "core/rs_processor.h"
#include "arch/openvoice2.h"
#include "utils/rs_log.h"
#include "ggml-backend.h"
#include <iostream>
#include <chrono>

RSProcessor::RSProcessor(std::shared_ptr<ISpeechModel> model, ggml_backend_sched_t sched)
    : model_(model), sched_(sched) {

  STFTConfig config;
  if (model_) {
    const auto& meta = model_->GetMeta();
    config.sample_rate = meta.audio_sample_rate;
    config.n_mels = meta.n_mels;

    // Architecture-specific frontend config
    const std::string& arch = meta.arch_name;
    if (arch == "ecapa-tdnn") {
      // ECAPA-TDNN: standard 80-mel fbank, no LFR, no CMVN
      config.use_lfr = false;
      config.use_cmvn = false;
    } else {
      // SenseVoice and other ASR models: LFR + CMVN
      config.use_lfr = true;
      config.lfr_m = 7;
      config.lfr_n = 6;
      config.use_cmvn = true;
    }
  }

  audio_proc_ = std::make_unique<AudioProcessor>(config);
  if (model_) {
    state_ = model_->CreateState();
  }
}

void RSProcessor::PushAudio(const float* data, size_t size) {
  audio_buffer_.Push(data, size);
}

void RSProcessor::SetCMVN(const std::vector<float>& means, const std::vector<float>& vars) {
  if (audio_proc_) {
    audio_proc_->SetCMVN(means, vars);
  }
}

int RSProcessor::Process() {
  if (!model_ || !state_ || !sched_) {
    RS_LOG_ERR("Processor error: Missing model, state, or scheduler.");
    return -1;
  }

  if (audio_buffer_.Size() < static_cast<size_t>(chunk_size_samples_)) {
    return 0;
  }

  std::vector<float> pcm_chunk = audio_buffer_.Pop(audio_buffer_.Size());
  float pcm_duration = pcm_chunk.size() / model_->GetMeta().audio_sample_rate;
  std::chrono::steady_clock::time_point start = std::chrono::steady_clock::now();
  std::chrono::steady_clock::time_point fist_start = std::chrono::steady_clock::now();
  std::chrono::steady_clock::time_point end = std::chrono::steady_clock::now();
  std::vector<float> features;
  audio_proc_->Compute(pcm_chunk, features);
  end = std::chrono::steady_clock::now();
  RS_LOG_INFO("compute features takes: %f", std::chrono::duration_cast<std::chrono::microseconds>(end - start).count() / 1e6 );
  if (features.empty()) return 0;

  // --- CRITICAL FIX FOR Encode Graph ---
  // Reset the scheduler to clear memory assignments before building/allocating the Encode graph.
  ggml_backend_sched_reset(sched_);

  // 3. Model Encoding
  start = std::chrono::steady_clock::now();
  if (!model_->Encode(features, *state_, sched_)) {
    RS_LOG_ERR("Model encoding failed.");
    return -1;
  }
  end = std::chrono::steady_clock::now();
  RS_LOG_INFO("encoder takes: %f", std::chrono::duration_cast<std::chrono::microseconds>(end - start).count() / 1e6 );

  // --- CRITICAL FIX FOR Decode Graph ---
  // Since Decode builds a NEW ggml_cgraph with its own context, we MUST reset the scheduler
  // again to prevent "GGML_ASSERT(!sched->is_alloc)" failure.
  // The previous graph (Encode) has already finished execution, so it's safe to clear.
  ggml_backend_sched_reset(sched_);

  // 4. Model Decoding
  start = std::chrono::steady_clock::now();
  if (!model_->Decode(*state_, sched_)) {
    RS_LOG_ERR("Model decoding failed.");
    return -1;
  }
  end = std::chrono::steady_clock::now();
  RS_LOG_INFO("decoder takes: %f", std::chrono::duration_cast<std::chrono::microseconds>(end - start).count() / 1e6 );

  RS_LOG_INFO("RTF is: %f", std::chrono::duration_cast<std::chrono::microseconds>(end - fist_start).count() / 1e6 / pcm_duration);
  return 1;
}

std::string RSProcessor::GetTextResult() {
  text_accumulator_ = model_->GetTranscription(*state_);
  return text_accumulator_;
}

int RSProcessor::GetEmbeddingResult(float** out_data) {
  if (!model_ || !state_ || !out_data) return 0;
  float* ptr = nullptr;
  int dim = model_->GetEmbedding(*state_, &ptr);
  if (dim > 0 && ptr) {
    embedding_buffer_.assign(ptr, ptr + dim);
    *out_data = embedding_buffer_.data();
    return dim;
  }
  *out_data = nullptr;
  return 0;
}

void RSProcessor::Reset() {
  // Drain any residual audio
  while (audio_buffer_.Size() > 0) {
    audio_buffer_.Pop(audio_buffer_.Size());
  }
  text_accumulator_.clear();
  embedding_buffer_.clear();
  last_token_id_ = -1;
  // Reset TTS state
  tts_encoded_ = false;
  tts_audio_buf_.clear();
  tts_audio_read_pos_ = 0;
  // Recreate model state for a clean slate
  if (model_) {
    state_ = model_->CreateState();
  }
}

// =====================================================================
// TTS methods
// =====================================================================

int RSProcessor::PushText(const char* text, const char* language) {
  if (!model_ || !state_) return -1;

  // Check if model is OpenVoice2 by arch name
  const auto& meta = model_->GetMeta();
  if (meta.arch_name != "openvoice2") {
    RS_LOG_ERR("PushText: model is not a TTS model (arch=%s)", meta.arch_name.c_str());
    return -1;
  }

  // Dynamic cast to OpenVoice2Model to call PushText
  // We use a simple approach: store text in state, let Process handle it
  auto* ov2 = dynamic_cast<OpenVoice2Model*>(model_.get());
  if (!ov2) return -1;

  if (!ov2->PushText(*state_, text, language)) return -1;
  tts_encoded_ = false;
  tts_audio_buf_.clear();
  tts_audio_read_pos_ = 0;
  return 0;
}

int RSProcessor::PushReferenceAudio(const float* samples, int n_samples, int sample_rate) {
  if (!model_ || !state_ || !sched_) return -1;

  auto* ov2 = dynamic_cast<OpenVoice2Model*>(model_.get());
  if (!ov2) return -1;

  return ov2->PushReferenceAudio(*state_, samples, n_samples, sample_rate, sched_) ? 0 : -1;
}

int RSProcessor::ProcessTTS() {
  if (!model_ || !state_ || !sched_) return -1;

  auto* ov2 = dynamic_cast<OpenVoice2Model*>(model_.get());
  if (!ov2) return -1;

  // First call: run text encoder + duration predictor + flow decoder
  if (!tts_encoded_) {
    ggml_backend_sched_reset(sched_);
    // Encode runs TextEncoder + DurationPredictor + FlowDecoder
    if (!ov2->Encode({}, *state_, sched_)) return -1;
    tts_encoded_ = true;
  }

  // Each subsequent call: run vocoder on next mel chunk
  ggml_backend_sched_reset(sched_);
  if (!ov2->Decode(*state_, sched_)) {
    return 0;  // No more chunks
  }

  // Get audio from this chunk
  float* chunk_data = nullptr;
  int chunk_n = ov2->GetAudioOutput(*state_, &chunk_data);
  if (chunk_n > 0 && chunk_data) {
    tts_audio_buf_.assign(chunk_data, chunk_data + chunk_n);
    tts_audio_read_pos_ = 0;
  }

  return 1;  // Audio available
}

int RSProcessor::GetAudioOutput(float** out_data) {
  if (tts_audio_buf_.empty() || tts_audio_read_pos_ >= static_cast<int>(tts_audio_buf_.size())) {
    *out_data = nullptr;
    return 0;
  }
  *out_data = tts_audio_buf_.data() + tts_audio_read_pos_;
  int n = static_cast<int>(tts_audio_buf_.size()) - tts_audio_read_pos_;
  tts_audio_read_pos_ = static_cast<int>(tts_audio_buf_.size());
  return n;
}

void CircularBuffer::Push(const float* data, size_t size) {
  if (data && size > 0) {
    buffer_.insert(buffer_.end(), data, data + size);
  }
}

std::vector<float> CircularBuffer::Pop(size_t size) {
  if (buffer_.size() < size) return {};
  std::vector<float> result(size);
  for (size_t i = 0; i < size; ++i) {
    result[i] = buffer_.front();
    buffer_.pop_front();
  }
  return result;
}

size_t CircularBuffer::Size() const { return buffer_.size(); }