#include "utils/rs_wav.h"
#include "gguf.h"
#include "rs_log.h"
#include <cstring>
#include <fstream>
#include <iostream>

bool load_wav_file(const char* filename, std::vector<float>& data, int* sample_rate) {
    std::ifstream file(filename, std::ios::binary);
    if (!file.is_open()) {
        return false;
    }

    // Read initial 12 bytes to verify RIFF/WAVE
    char riff_header[12];
    file.read(riff_header, 12);
    if (std::strncmp(riff_header, "RIFF", 4) != 0 || std::strncmp(riff_header + 8, "WAVE", 4) != 0) {
        std::cerr << "[rs_wav] Error: Invalid RIFF/WAVE file." << std::endl;
        return false;
    }

    uint16_t num_channels = 0;
    uint32_t samples_per_sec = 0;
    uint16_t bits_per_sample = 0;
    uint32_t data_size = 0;

    // Robustly search for 'fmt ' and 'data' chunks
    char chunk_id[4];
    uint32_t chunk_size;
    while (file.read(chunk_id, 4) && file.read(reinterpret_cast<char*>(&chunk_size), 4)) {
        if (std::strncmp(chunk_id, "fmt ", 4) == 0) {
            uint16_t audio_format;
            file.read(reinterpret_cast<char*>(&audio_format), 2);
            file.read(reinterpret_cast<char*>(&num_channels), 2);
            file.read(reinterpret_cast<char*>(&samples_per_sec), 4);
            file.seekg(6, std::ios::cur); // Skip byteRate and blockAlign
            file.read(reinterpret_cast<char*>(&bits_per_sample), 2);

            if (audio_format != 1 || bits_per_sample != 16) {
                std::cerr << "[rs_wav] Error: Only 16-bit PCM is supported." << std::endl;
                return false;
            }
            // Skip remaining fmt chunk if any (e.g. for non-PCM)
            if (chunk_size > 16) file.seekg(chunk_size - 16, std::ios::cur);
        } else if (std::strncmp(chunk_id, "data", 4) == 0) {
            data_size = chunk_size;
            break; // Found the data chunk
        } else {
            // Skip unknown chunks
            file.seekg(chunk_size, std::ios::cur);
        }
    }

    if (num_channels == 0 || data_size == 0) {
        std::cerr << "[rs_wav] Error: Could not find audio data." << std::endl;
        return false;
    }

    *sample_rate = static_cast<int>(samples_per_sec);

    // num_samples here is the number of audio frames (time steps)
    int bytes_per_sample = bits_per_sample / 8;
    int num_samples = data_size / (num_channels * bytes_per_sample);
    data.resize(num_samples);

    // Read samples
    for (int i = 0; i < num_samples; ++i) {
        int16_t sample = 0;
        file.read(reinterpret_cast<char*>(&sample), sizeof(int16_t));
        data[i] = static_cast<float>(sample);

        // Correctly skip the remaining channels for multi-channel files
        if (num_channels > 1) {
            file.seekg(bytes_per_sample * (num_channels - 1), std::ios::cur);
        }
    }

    return true;
}


/**
 * Helper to load CMVN from GGUF or fallback to defaults.
 * @param ctx_gguf The loaded GGUF context.
 * @param means Target vector for mean values.
 * @param vars Target vector for variance values.
 */
void load_cmvn_params(struct gguf_context * ctx_gguf, std::vector<float>& means, std::vector<float>& vars) {
  bool loaded = false;

  // 1. Try to load from GGUF KV
  int key_means = gguf_find_key(ctx_gguf, "model.cmvn_means");
  int key_vars  = gguf_find_key(ctx_gguf, "model.cmvn_vars");

  if (key_means != -1 && key_vars != -1) {
    const float * data_means = (const float *) gguf_get_arr_data(ctx_gguf, key_means);
    const float * data_vars  = (const float *) gguf_get_arr_data(ctx_gguf, key_vars);
    int n_means = gguf_get_arr_n(ctx_gguf, key_means);
    int n_vars  = gguf_get_arr_n(ctx_gguf, key_vars);

    if (n_means == 560 && n_vars == 560) {
      means.assign(data_means, data_means + 560);
      vars.assign(data_vars, data_vars + 560);
      RS_LOG_INFO("CMVN parameters loaded from GGUF KV metadata.");
      loaded = true;
    } else {
      RS_LOG_WARN("CMVN metadata size mismatch (expected 560, got %d). Falling back.", n_means);
    }
  }
}