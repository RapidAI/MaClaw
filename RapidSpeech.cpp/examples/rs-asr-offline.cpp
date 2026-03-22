#include "rapidspeech.h"
#include "utils/rs_log.h"
#include "utils/rs_wav.h"

#include <cmath>
#include <cstring>
#include <iostream>
#include <vector>
#include <string>


static void print_usage(const char *prog) {
  std::cerr
      << "Usage:\n"
      << "  " << prog << " --model <model.gguf> [options]\n\n"
      << "Options:\n"
      << "  -m, --model <path>     Model file path (required)\n"
      << "  -w, --wav <path>       WAV file path (optional)\n"
      << "  -t, --threads <num>    Number of threads (default: 4)\n"
      << "      --gpu <true|false>\n"
      << std::endl;
}

static bool parse_bool(const std::string &v) {
  return (v == "1" || v == "true" || v == "TRUE");
}

// ---------------- main ----------------

int main(int argc, char *argv[]) {
  if (argc < 2) {
    print_usage(argv[0]);
    return 1;
  }

  // -------- defaults --------
  const char *model_path = nullptr;
  const char *wav_path   = nullptr;
  int n_threads          = 4;
  bool use_gpu           = true;

  // -------- parse argv --------
  for (int i = 1; i < argc; ++i) {
    std::string arg = argv[i];

    if ((arg == "-m" || arg == "--model") && i + 1 < argc) {
      model_path = argv[++i];
    } else if ((arg == "-w" || arg == "--wav") && i + 1 < argc) {
      wav_path = argv[++i];
    } else if ((arg == "-t" || arg == "--threads") && i + 1 < argc) {
      n_threads = std::stoi(argv[++i]);
    } else if (arg == "--gpu" && i + 1 < argc) {
      use_gpu = parse_bool(argv[++i]);
    } else {
      std::cerr << "Unknown argument: " << arg << std::endl;
      print_usage(argv[0]);
      return 1;
    }
  }

  if (!model_path) {
    std::cerr << "Error: --model is required\n";
    print_usage(argv[0]);
    return 1;
  }

  // -------- init params --------
  rs_init_params_t params = rs_default_params();
  params.model_path = model_path;
  params.n_threads  = n_threads;
  params.use_gpu   = use_gpu;

  std::cout << "[rs-cli] Model   : " << model_path << std::endl;
  std::cout << "[rs-cli] Threads : " << n_threads << std::endl;
  std::cout << "[rs-cli] GPU     : " << (use_gpu ? "ON" : "OFF") << std::endl;

  // -------- create context --------
  rs_context_t *ctx = rs_init_from_file(params);
  if (!ctx) {
    std::cerr << "[rs-cli] Failed to initialize context." << std::endl;
    return 1;
  }

  // -------- load audio --------
  std::vector<float> pcm;
  int sample_rate = 16000;

  if (wav_path) {
    RS_LOG_INFO("[rs-cli] Loading audio: %s", wav_path);
    if (!load_wav_file(wav_path, pcm, &sample_rate)) {
      RS_LOG_INFO("[rs-cli] Failed to load WAV file");
      rs_free(ctx);
      return 1;
    }
    RS_LOG_INFO("[rs-cli] Loaded %zu samples @ %d Hz", pcm.size(), sample_rate);
  } else {
    RS_LOG_INFO("[rs-cli] No WAV provided, generating 1s sine wave");
    pcm.resize(16000);
    for (int i = 0; i < 16000; ++i) {
      pcm[i] = sinf(2.0f * 3.14159f * 440.0f * i / 16000.0f);
    }
  }

  if (pcm.empty()) {
    std::cerr << "[rs-cli] No audio data." << std::endl;
    rs_free(ctx);
    return 1;
  }

  if (rs_push_audio(ctx, pcm.data(), static_cast<int>(pcm.size())) != 0) {
    std::cerr << "[rs-cli] Failed to push audio." << std::endl;
    rs_free(ctx);
    return 1;
  }

  // -------- inference --------
  std::cout << "[rs-cli] Starting inference..." << std::endl;

  while (true) {
    int status = rs_process(ctx);

    if (status < 0) {
      std::cerr << "[rs-cli] Inference error." << std::endl;
      break;
    } else if (status == 0) {
      break;
    } else {
      const char *text = rs_get_text_output(ctx);
      if (text && std::strlen(text) > 0) {
        std::cout << "\r[rs-cli] Result: " << text << std::flush;
      }
    }
  }

  std::cout << std::endl;
  std::cout << "[rs-cli] Finished." << std::endl;

  rs_free(ctx);
  return 0;
}