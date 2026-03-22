#include "frontend/audio_processor.h"
#include <cmath>
#include <algorithm>
#include <iostream>
#include <cstring>
#include <cassert>
#include <vector>
#include <thread>
#include <future>

// Check if OpenMP is available
#if defined(_OPENMP)
#include <omp.h>
#endif

#ifndef M_PI
#define M_PI 3.14159265358979323846
#endif

#define PREEMPH_COEFF 0.97f

// External reference to rdft
extern "C" {
    void rdft(int n, int isgn, double *a, int *ip, double *w);
}

static inline double mel_scale(double hz) {
    return 1127.0 * std::log(1.0 + hz / 700.0);
}

AudioProcessor::AudioProcessor(const STFTConfig& config) : config_(config) {
    config_.n_fft = RoundToPowerOfTwo(config_.frame_size);
    InitTables();
    InitMelFilters();
}

AudioProcessor::~AudioProcessor() {}

int AudioProcessor::RoundToPowerOfTwo(int n) {
    n--;
    n |= n >> 1; n |= n >> 2; n |= n >> 4; n |= n >> 8; n |= n >> 16;
    return n + 1;
}

void AudioProcessor::InitTables() {
    // 1. Hamming Window
    hamming_window_.resize(config_.frame_size);
    for (int i = 0; i < config_.frame_size; i++) {
        hamming_window_[i] = 0.54 - 0.46 * cos((2.0 * M_PI * i) / (config_.frame_size));
    }

    // 2. FFT Tables
    // rdft requires 'ip' size to be at least 2 + sqrt(n/2)
    fft_ip_.assign(2 + static_cast<int>(sqrt(config_.n_fft / 2)) + 1, 0);
    fft_w_.assign(config_.n_fft / 2, 0.0);

    // Initialize ip[0] to 0 to trigger internal initialization on first rdft call
    fft_ip_[0] = 0;

    // Dry run to generate internal tables.
    // This ensures 'fft_ip_' and 'fft_w_' are fully initialized, making them
    // read-only and thread-safe for subsequent concurrent calls.
    std::vector<double> dummy(config_.n_fft, 0.0);
    rdft(config_.n_fft, 1, dummy.data(), fft_ip_.data(), fft_w_.data());
}

void AudioProcessor::InitMelFilters() {
    const int n_mel = config_.n_mels;
    const int fft_bins = config_.n_fft / 2;
    const int num_bins = fft_bins;

    mel_filters_.assign(n_mel * num_bins, 0.0f);

    const double mel_low_freq = 31.748642;
    const double mel_freq_delta = 34.6702385;
    const double fft_bin_width = (double)config_.sample_rate / config_.n_fft;

    for (int i = 0; i < n_mel; i++) {
        double left_mel   = mel_low_freq + i * mel_freq_delta;
        double center_mel = mel_low_freq + (i + 1.0) * mel_freq_delta;
        double right_mel  = mel_low_freq + (i + 2.0) * mel_freq_delta;

        for (int j = 0; j < num_bins; j++) {
            double freq_hz = fft_bin_width * j;
            double mel_num = mel_scale(freq_hz);

            double up_slope   = (mel_num - left_mel) / (center_mel - left_mel);
            double down_slope = (right_mel - mel_num) / (right_mel - center_mel);

            double filter_val = std::max(0.0, std::min(up_slope, down_slope));
            mel_filters_[i * num_bins + j] = static_cast<float>(filter_val);
        }
    }
}

// Core processing unit: Process a single frame.
// Note: window_buf and power_spec_buf are provided by the caller to avoid repeated allocation.
void AudioProcessor::ProcessFrame(int frame_idx,
                                  const std::vector<float>& samples,
                                  std::vector<double>& window_buf,
                                  std::vector<double>& power_spec_buf,
                                  std::vector<float>& output_mel) {

    int n_samples = samples.size();
    int offset = frame_idx * config_.frame_step;

    // 1. Copy and Pad
    // Use std::fill for zeroing, often faster/cleaner than loop assignment
    std::fill(window_buf.begin(), window_buf.end(), 0.0);

    int copy_len = std::min(config_.frame_size, n_samples - offset);
    if (copy_len > 0) {
        for (int j = 0; j < copy_len; j++) {
            window_buf[j] = static_cast<double>(samples[offset + j]);
        }
    }

    // 2. Remove DC
    double sum = 0.0;
    for (int j = 0; j < config_.frame_size; j++) sum += window_buf[j];
    double mean = sum / config_.frame_size;
    for (int j = 0; j < config_.frame_size; j++) window_buf[j] -= mean;

    // 3. Pre-emphasis
    for (int j = config_.frame_size - 1; j > 0; j--) {
        window_buf[j] -= PREEMPH_COEFF * window_buf[j - 1];
    }
    window_buf[0] -= PREEMPH_COEFF * window_buf[0];

    // 4. Hamming window
    for (int j = 0; j < config_.frame_size; j++) {
        window_buf[j] *= hamming_window_[j];
    }

    // 5. FFT
    // Note: Since ip/w arrays are pre-initialized, they are read-only and thread-safe.
    rdft(config_.n_fft, 1, window_buf.data(), const_cast<int*>(fft_ip_.data()), const_cast<double*>(fft_w_.data()));

    // 6. Power Spectrum (Corrected logic)
    // Ooura rdft layout:
    // a[0] = Re(0) (DC)
    // a[1] = Re(N/2) (Nyquist) - This is a separate real number, not the imaginary part of DC
    // a[2k] = Re(k), a[2k+1] = Im(k)

    power_spec_buf[0] = window_buf[0] * window_buf[0];           // DC
    power_spec_buf[config_.n_fft / 2] = window_buf[1] * window_buf[1]; // Nyquist

    for (int j = 1; j < config_.n_fft / 2; j++) {
        power_spec_buf[j] = window_buf[2 * j] * window_buf[2 * j] +
                            window_buf[2 * j + 1] * window_buf[2 * j + 1];
    }

    // 7. Mel Filtering
    // Leverage cache locality
    int num_bins = config_.n_fft / 2;
    for (int j = 0; j < config_.n_mels; j++) {
        double mel_energy = 0.0;
        const float* filter_row = &mel_filters_[j * num_bins];

        // Core dot product loop, usually auto-vectorized by compiler (SIMD)
        for (int k = 0; k < num_bins; k++) {
             mel_energy += power_spec_buf[k] * filter_row[k];
        }

        output_mel[frame_idx * config_.n_mels + j] = static_cast<float>(log(std::max(mel_energy, 1.19e-7)));
    }
}

void AudioProcessor::ComputeFbank(const std::vector<float>& samples, std::vector<float>& output_mel) {
    int n_samples = samples.size();
    if (n_samples < config_.frame_size) return;

    int n_frames = (n_samples - config_.frame_size) / config_.frame_step + 1;
    output_mel.resize(n_frames * config_.n_mels);

    // ==========================================
    // Strategy 1: OpenMP Parallelization
    // ==========================================
#if defined(_OPENMP)
    #pragma omp parallel
    {
        // Thread-private buffers to avoid repeated allocation
        std::vector<double> window_buf(config_.n_fft);
        std::vector<double> power_spec_buf(config_.n_fft / 2 + 1);

        #pragma omp for
        for (int i = 0; i < n_frames; i++) {
            ProcessFrame(i, samples, window_buf, power_spec_buf, output_mel);
        }
    }

    // ==========================================
    // Strategy 2: std::thread Parallelization (Fallback)
    // ==========================================
#else
    unsigned int num_threads = config_.fbank_num_threads;
    if (num_threads == 0) num_threads = 1; // Safety fallback

    // If there are too few frames, threading overhead outweighs benefits; use single thread.
    if (n_frames < num_threads * 2) num_threads = 1;

    std::vector<std::thread> threads;
    int chunk_size = n_frames / num_threads;

    for (unsigned int t = 0; t < num_threads; t++) {
        int start = t * chunk_size;
        int end = (t == num_threads - 1) ? n_frames : (t + 1) * chunk_size;

        threads.emplace_back([this, start, end, &samples, &output_mel]() {
            // Thread-local buffer
            std::vector<double> window_buf(config_.n_fft);
            std::vector<double> power_spec_buf(config_.n_fft / 2 + 1);

            for (int i = start; i < end; i++) {
                ProcessFrame(i, samples, window_buf, power_spec_buf, output_mel);
            }
        });
    }

    for (auto& t : threads) {
        if (t.joinable()) t.join();
    }
#endif
}

void AudioProcessor::ApplyLFR(const std::vector<float>& input_mel, int n_frames, std::vector<float>& output_lfr) {
    int m = config_.lfr_m;
    int n = config_.lfr_n;
    int n_mels = config_.n_mels;

    int T_lfr = static_cast<int>(ceil(1.0 * n_frames / n));
    output_lfr.resize(T_lfr * m * n_mels);

    int left_pad = (m - 1) / 2;

    // LFR can also be parallelized; however, as it's memory-bound (copying),
    // the acceleration depends heavily on memory bandwidth.
    #if defined(_OPENMP)
    #pragma omp parallel for
    #endif
    for (int i = 0; i < T_lfr; i++) {
        for (int j = 0; j < m; j++) {
            int source_frame_idx = i * n - left_pad + j;
            if (source_frame_idx < 0) source_frame_idx = 0;
            if (source_frame_idx >= n_frames) source_frame_idx = n_frames - 1;

            std::memcpy(output_lfr.data() + (i * m * n_mels) + (j * n_mels),
                        input_mel.data() + (source_frame_idx * n_mels),
                        n_mels * sizeof(float));
        }
    }
}

void AudioProcessor::SetCMVN(const std::vector<float>& means, const std::vector<float>& vars) {
    cmvn_.means = means;
    cmvn_.vars = vars;
}

void AudioProcessor::ApplyCMVN(std::vector<float>& feats) {
    if (cmvn_.means.empty() || cmvn_.vars.empty()) return;

    int feat_dim = config_.lfr_m * config_.n_mels;
    int n_frames = feats.size() / feat_dim;

    #if defined(_OPENMP)
    #pragma omp parallel for
    #endif
    for (int i = 0; i < n_frames; i++) {
        for (int j = 0; j < feat_dim; j++) {
            int idx = i * feat_dim + j;
            feats[idx] = (feats[idx] + cmvn_.means[j]) * cmvn_.vars[j];
        }
    }
}

void AudioProcessor::Compute(const std::vector<float>& input_pcm, std::vector<float>& output_feats) {
    if (input_pcm.empty()) return;

    std::vector<float> mel_feats;
    ComputeFbank(input_pcm, mel_feats);
    int n_frames = mel_feats.size() / config_.n_mels;

    if (config_.use_lfr) {
        std::vector<float> lfr_feats;
        ApplyLFR(mel_feats, n_frames, lfr_feats);
        output_feats = std::move(lfr_feats);
    } else {
        output_feats = std::move(mel_feats);
    }

    if (config_.use_cmvn) {
        ApplyCMVN(output_feats);
    }
}