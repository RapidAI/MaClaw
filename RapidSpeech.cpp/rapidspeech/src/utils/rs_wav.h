#pragma once

#include <vector>
#include <cstdint>

/**
 * WAV header， 16bit PCM
 */
struct WaveHeader {
  char chunkId[4];
  uint32_t chunkSize;
  char format[4];
  char subchunk1Id[4];
  uint32_t subchunk1Size;
  uint16_t audioFormat;
  uint16_t numChannels;
  uint32_t sampleRate;
  uint32_t byteRate;
  uint16_t blockAlign;
  uint16_t bitsPerSample;
  char subchunk2Id[4];
  uint32_t subchunk2Size;

};

/**
 * load 16bit PCM WAV convert into float PCM
 * @param filename .wav file path
 * @param data  float
 * @param sample_rate sample rate
 * @return true if success
 */
bool load_wav_file(const char* filename, std::vector<float>& data, int* sample_rate);

void load_cmvn_params(struct gguf_context * ctx_gguf, std::vector<float>& means, std::vector<float>& vars);