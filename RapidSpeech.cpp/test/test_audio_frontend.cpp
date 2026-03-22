#include "frontend/audio_processor.h"
#include <iostream>
#include <vector>
#include <cmath>
#include <cassert>
#include <iomanip>

// Helper: Generate 16kHz sine wave (A4 note)
std::vector<float> generate_sine_wave(float duration_sec, float freq_hz = 440.0f, int sample_rate = 16000) {
  int n_samples = static_cast<int>(duration_sec * sample_rate);
  std::vector<float> pcm(n_samples);
  for (int i = 0; i < n_samples; ++i) {
    pcm[i] = sinf(2.0f * 3.14159265f * freq_hz * i / sample_rate);
  }
  return pcm;
}

// Test 1: Standard Fbank (e.g., Whisper)
void test_fbank_standard() {
  std::cout << "\n[Test] Running Standard Fbank (Whisper-like)..." << std::endl;
    
  STFTConfig config;
  config.sample_rate = 16000;
  config.n_mels = 80;
  config.n_fft = 400;      // Whisper typically uses 400 (25ms)
  config.frame_size = 400;
  config.frame_step = 160; // 10ms stride
  config.use_lfr = false;
  config.use_cmvn = false;

  AudioProcessor processor(config);

  // 1 second audio -> 16000 samples
  auto pcm = generate_sine_wave(1.0f);
  std::vector<float> feats;
    
  // Run Compute
  processor.Compute(pcm, feats, false);

  // Expected Calculation:
  // Num Frames = (16000 - 400) / 160 + 1 = 97.5 -> 98 frames
  // Output Dim = 98 * 80 = 7840
  int expected_frames = (pcm.size() - config.frame_size) / config.frame_step + 1;
  int expected_size = expected_frames * config.n_mels;

  std::cout << "  Input samples: " << pcm.size() << std::endl;
  std::cout << "  Output feature size: " << feats.size() << std::endl;
  std::cout << "  Expected size: " << expected_size << " (Frames: " << expected_frames << ")" << std::endl;

  // Check first few values (should be non-zero for sine wave)
  std::cout << "  First 5 features of frame 0: ";
  for(int i=0; i<5 && i<feats.size(); ++i) std::cout << std::fixed << std::setprecision(4) << feats[i] << " ";
  std::cout << std::endl;

  if (abs((int)feats.size() - expected_size) <= config.n_mels) {
    std::cout << "  [PASS] Size checks out." << std::endl;
  } else {
    std::cerr << "  [FAIL] Size mismatch!" << std::endl;
  }
}

// Test 2: Fbank + LFR (e.g., SenseVoice / FunASR / Paraformer)
void test_fbank_lfr() {
  std::cout << "\n[Test] Running Fbank + LFR (SenseVoice-like)..." << std::endl;
    
  STFTConfig config;
  config.sample_rate = 16000;
  config.n_mels = 80;
  config.n_fft = 512;      // FunASR typically aligns to 512
  config.frame_size = 400; // 25ms
  config.frame_step = 160; // 10ms
  config.use_lfr = true;
  config.lfr_m = 7;        // Stack 7 frames
  config.lfr_n = 6;        // Skip 6 frames
  config.use_cmvn = false; // Skip CMVN for basic check

  AudioProcessor processor(config);

  // 1 second audio
  auto pcm = generate_sine_wave(1.0f);
  std::vector<float> feats;
    
  processor.Compute(pcm, feats, false);

  // Expected Calculation:
  // Basic Frames: 98
  // LFR Frames: ceil(98 / 6.0) = ceil(16.33) = 17 frames
  // Feature Dim: 17 * (7 * 80) = 17 * 560 = 9520
    
  int n_frames_basic = (pcm.size() - config.frame_size) / config.frame_step + 1;
  int n_frames_lfr = std::ceil(1.0 * n_frames_basic / config.lfr_n);
  int feature_dim_per_frame = config.lfr_m * config.n_mels;
  int expected_size = n_frames_lfr * feature_dim_per_frame;

  std::cout << "  Input samples: " << pcm.size() << std::endl;
  std::cout << "  Basic frames: " << n_frames_basic << std::endl;
  std::cout << "  LFR frames: " << n_frames_lfr << std::endl;
  std::cout << "  Output feature size: " << feats.size() << std::endl;
  std::cout << "  Expected size: " << expected_size << std::endl;

  if (feats.size() == expected_size) {
    std::cout << "  [PASS] Size checks out." << std::endl;
  } else {
    std::cerr << "  [FAIL] Size mismatch!" << std::endl;
  }
}

int main() {
  try {
    test_fbank_standard();
    test_fbank_lfr();
  } catch (const std::exception& e) {
    std::cerr << "Test failed with exception: " << e.what() << std::endl;
    return 1;
  }
  return 0;
}