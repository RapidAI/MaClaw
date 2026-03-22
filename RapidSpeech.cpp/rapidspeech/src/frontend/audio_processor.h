#pragma once

#include <vector>
#include <cstdint>
#include <string>

// Configuration for SenseVoice/FunASR frontend pipeline
struct STFTConfig {
  int sample_rate = 16000;
  int frame_size = 400; // 25ms @ 16k
  int frame_step = 160; // 10ms @ 16k
  int n_fft = 512;      // Power of 2 padding
  int n_mels = 80;
  float f_min = 31.748642f;
  float f_max = 8000.0f;

  // --- SenseVoice Specific (LFR & CMVN) ---
  bool use_lfr = true;
  int lfr_m = 7;        // Stack 7 frames
  int lfr_n = 6;        // Stride 6 frames

  bool use_cmvn = true;
  int fbank_num_threads = 2;
  // Default CMVN values are usually provided via weights/file
};

struct CMVNData {
  std::vector<float> means;
  std::vector<float> vars;
};

class AudioProcessor {
public:
  AudioProcessor(const STFTConfig& config);
  ~AudioProcessor();

  // Set CMVN parameters (extracted from model weights or external file)
  void SetCMVN(const std::vector<float>& means, const std::vector<float>& vars);

  // Main pipeline: PCM -> Fbank -> LFR -> CMVN
  void Compute(const std::vector<float>& input_pcm,
               std::vector<float>& output_feats);

private:
  STFTConfig config_;
  std::vector<double> hamming_window_;
  std::vector<float> mel_filters_; // [n_mels, n_fft/2 + 1]
  CMVNData cmvn_;

  // Internal FFT workspace
  std::vector<int> fft_ip_;
  std::vector<double> fft_w_;

  void InitTables();
  void InitMelFilters();

  // Core processing steps
  void ComputeFbank(const std::vector<float>& samples, std::vector<float>& output_mel);
  void ApplyLFR(const std::vector<float>& input_mel, int n_frames, std::vector<float>& output_lfr);
  void ApplyCMVN(std::vector<float>& feats);
  void ProcessFrame(int i, const std::vector<float>& samples,
                                        std::vector<double>& window,
                                        std::vector<double>& power_spec,
                                        std::vector<float>& output_mel);

  // Mathematical utilities
  int RoundToPowerOfTwo(int n);
  void RealFFT(std::vector<double>& window);
};