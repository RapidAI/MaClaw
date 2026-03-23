// RapidSpeech REST API Server
// Provides /v1/asr, /v1/tts, /v1/speaker-embed, /v1/speaker-verify, /v1/health endpoints
// Depends on cpp-httplib (header-only): server/third_party/httplib.h

#include "httplib.h"
#include "rapidspeech.h"
#include "utils/rs_log.h"
#include "utils/rs_wav.h"

#include <csignal>
#include <cstring>
#include <iostream>
#include <mutex>
#include <string>
#include <vector>

// --------------- Simple JSON helpers (no dependency) ---------------

static std::string json_escape(const std::string& s) {
    std::string out;
    out.reserve(s.size() + 8);
    for (char c : s) {
        switch (c) {
            case '"':  out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\n': out += "\\n";  break;
            case '\r': out += "\\r";  break;
            case '\t': out += "\\t";  break;
            default:   out += c;
        }
    }
    return out;
}

static std::string json_ok(const std::string& key, const std::string& value) {
    return "{\"" + key + "\":\"" + json_escape(value) + "\"}";
}

static std::string json_error(const std::string& msg) {
    return "{\"error\":\"" + json_escape(msg) + "\"}";
}

// --------------- Global state ---------------

struct ServerState {
    rs_context_t* asr_ctx = nullptr;
    rs_context_t* tts_ctx = nullptr;
    rs_context_t* spk_ctx = nullptr;
    std::mutex    asr_mtx;
    std::mutex    tts_mtx;
    std::mutex    spk_mtx;
};

// --------------- WAV parsing from memory buffer ---------------

// Minimal WAV header parser for 16-bit PCM or 32-bit float
static bool parse_wav_buffer(const char* data, size_t len,
                             std::vector<float>& pcm, int* sample_rate) {
    if (len < 44) return false;
    // Check RIFF header
    if (std::memcmp(data, "RIFF", 4) != 0) return false;
    if (std::memcmp(data + 8, "WAVE", 4) != 0) return false;

    // Find fmt chunk
    size_t pos = 12;
    uint16_t audio_fmt = 0, channels = 0, bits_per_sample = 0;
    uint32_t sr = 0;

    while (pos + 8 <= len) {
        uint32_t chunk_size = 0;
        std::memcpy(&chunk_size, data + pos + 4, 4);

        if (std::memcmp(data + pos, "fmt ", 4) == 0) {
            // fmt chunk needs at least 16 bytes; bits_per_sample is at offset 14
            if (pos + 8 + 16 > len) return false;
            std::memcpy(&audio_fmt, data + pos + 8, 2);
            std::memcpy(&channels, data + pos + 10, 2);
            std::memcpy(&sr, data + pos + 12, 4);
            std::memcpy(&bits_per_sample, data + pos + 22, 2);
            if (channels == 0) return false;
            *sample_rate = static_cast<int>(sr);
        } else if (std::memcmp(data + pos, "data", 4) == 0) {
            const char* audio_data = data + pos + 8;
            size_t audio_len = chunk_size;
            if (pos + 8 + audio_len > len) audio_len = len - pos - 8;

            if (audio_fmt == 1 && bits_per_sample == 16) {
                // PCM 16-bit
                size_t n = audio_len / (2 * channels);
                pcm.resize(n);
                const int16_t* src = reinterpret_cast<const int16_t*>(audio_data);
                for (size_t i = 0; i < n; i++) {
                    pcm[i] = static_cast<float>(src[i * channels]) / 32768.0f;
                }
            } else if (audio_fmt == 3 && bits_per_sample == 32) {
                // IEEE float
                size_t n = audio_len / (4 * channels);
                pcm.resize(n);
                const float* src = reinterpret_cast<const float*>(audio_data);
                for (size_t i = 0; i < n; i++) {
                    pcm[i] = src[i * channels];
                }
            } else {
                return false;
            }
            return true;
        }
        pos += 8 + chunk_size;
        if (chunk_size % 2) pos++; // padding
    }
    return false;
}

// --------------- WAV encoding to memory buffer ---------------

static std::string encode_wav(const float* pcm, int n_samples, int sample_rate) {
    // Output 16-bit PCM WAV
    std::vector<int16_t> buf16(n_samples);
    for (int i = 0; i < n_samples; i++) {
        float v = pcm[i] * 32767.0f;
        if (v > 32767.0f) v = 32767.0f;
        if (v < -32768.0f) v = -32768.0f;
        buf16[i] = static_cast<int16_t>(v);
    }

    uint32_t data_size = static_cast<uint32_t>(n_samples) * 2;
    uint32_t file_size = 36 + data_size;
    uint32_t fmt_size = 16;
    uint16_t audio_fmt = 1;
    uint16_t channels = 1;
    uint32_t sr = static_cast<uint32_t>(sample_rate);
    uint32_t byte_rate = sr * 2;
    uint16_t block_align = 2;
    uint16_t bits = 16;

    std::string wav;
    wav.reserve(44 + data_size);
    wav.append("RIFF", 4);
    wav.append(reinterpret_cast<const char*>(&file_size), 4);
    wav.append("WAVE", 4);
    wav.append("fmt ", 4);
    wav.append(reinterpret_cast<const char*>(&fmt_size), 4);
    wav.append(reinterpret_cast<const char*>(&audio_fmt), 2);
    wav.append(reinterpret_cast<const char*>(&channels), 2);
    wav.append(reinterpret_cast<const char*>(&sr), 4);
    wav.append(reinterpret_cast<const char*>(&byte_rate), 4);
    wav.append(reinterpret_cast<const char*>(&block_align), 2);
    wav.append(reinterpret_cast<const char*>(&bits), 2);
    wav.append("data", 4);
    wav.append(reinterpret_cast<const char*>(&data_size), 4);
    wav.append(reinterpret_cast<const char*>(buf16.data()), data_size);

    return wav;
}

// --------------- Route handlers ---------------

static void handle_health(const httplib::Request&, httplib::Response& res) {
    res.set_content("{\"status\":\"ok\"}", "application/json");
}

static void handle_asr(ServerState& state,
                       const httplib::Request& req, httplib::Response& res) {
    // Expect multipart form: file=<wav audio>
    if (!req.has_file("file")) {
        res.status = 400;
        res.set_content(json_error("missing 'file' field (WAV audio)"), "application/json");
        return;
    }

    auto file = req.get_file_value("file");
    std::vector<float> pcm;
    int sr = 16000;

    if (!parse_wav_buffer(file.content.data(), file.content.size(), pcm, &sr)) {
        res.status = 400;
        res.set_content(json_error("invalid WAV format (need 16-bit PCM or 32-bit float)"),
                        "application/json");
        return;
    }

    if (pcm.empty()) {
        res.status = 400;
        res.set_content(json_error("empty audio data"), "application/json");
        return;
    }

    std::lock_guard<std::mutex> lock(state.asr_mtx);

    if (!state.asr_ctx) {
        res.status = 503;
        res.set_content(json_error("ASR model not loaded"), "application/json");
        return;
    }

    // Reset state to avoid residual data from previous requests
    rs_reset(state.asr_ctx);

    if (rs_push_audio(state.asr_ctx, pcm.data(), static_cast<int>(pcm.size())) != 0) {
        res.status = 500;
        res.set_content(json_error("failed to push audio"), "application/json");
        return;
    }

    std::string result;
    while (true) {
        int status = rs_process(state.asr_ctx);
        if (status < 0) {
            res.status = 500;
            res.set_content(json_error("inference error"), "application/json");
            return;
        }
        if (status == 0) break;
        const char* text = rs_get_text_output(state.asr_ctx);
        if (text && std::strlen(text) > 0) {
            result = text;
        }
    }

    res.set_content(json_ok("text", result), "application/json");
}

static void handle_tts(ServerState& state,
                       const httplib::Request& req, httplib::Response& res) {
    // Expect multipart form: text=<string>, optional reference_audio=<wav> for voice cloning
    std::string text;

    if (req.has_file("text")) {
        text = req.get_file_value("text").content;
    } else if (req.has_param("text")) {
        text = req.get_param_value("text");
    }

    if (text.empty()) {
        res.status = 400;
        res.set_content(json_error("missing 'text' field"), "application/json");
        return;
    }

    // Optional: reference audio for voice cloning (OpenVoice2 etc.)
    std::vector<float> ref_pcm;
    int ref_sr = 16000;
    if (req.has_file("reference_audio")) {
        auto ref_file = req.get_file_value("reference_audio");
        if (!parse_wav_buffer(ref_file.content.data(), ref_file.content.size(),
                              ref_pcm, &ref_sr)) {
            res.status = 400;
            res.set_content(json_error("invalid reference_audio WAV format"),
                            "application/json");
            return;
        }
        // TODO: pass ref_pcm to voice cloning pipeline when model supports it
        rs_push_reference_audio(state.tts_ctx, ref_pcm.data(),
                                static_cast<int>(ref_pcm.size()), ref_sr);
    }

    std::lock_guard<std::mutex> lock(state.tts_mtx);

    if (!state.tts_ctx) {
        res.status = 503;
        res.set_content(json_error("TTS model not loaded"), "application/json");
        return;
    }

    // Reset state for new request
    rs_reset(state.tts_ctx);

    if (rs_push_text(state.tts_ctx, text.c_str()) != 0) {
        res.status = 500;
        res.set_content(json_error("failed to push text"), "application/json");
        return;
    }

    std::vector<float> audio_out;
    while (true) {
        int status = rs_process(state.tts_ctx);
        if (status < 0) {
            res.status = 500;
            res.set_content(json_error("TTS inference error"), "application/json");
            return;
        }
        if (status == 0) break;
        float* out = nullptr;
        int n = rs_get_audio_output(state.tts_ctx, &out);
        if (n > 0 && out) {
            audio_out.insert(audio_out.end(), out, out + n);
        }
    }

    if (audio_out.empty()) {
        res.status = 500;
        res.set_content(json_error("TTS produced no audio"), "application/json");
        return;
    }

    std::string wav = encode_wav(audio_out.data(),
                                 static_cast<int>(audio_out.size()), 16000);
    res.set_content(wav, "audio/wav");
}

static void handle_speaker_embed(ServerState& state,
                                  const httplib::Request& req, httplib::Response& res) {
    if (!req.has_file("file")) {
        res.status = 400;
        res.set_content(json_error("missing 'file' field (WAV audio)"), "application/json");
        return;
    }

    auto file = req.get_file_value("file");
    std::vector<float> pcm;
    int sr = 16000;

    if (!parse_wav_buffer(file.content.data(), file.content.size(), pcm, &sr)) {
        res.status = 400;
        res.set_content(json_error("invalid WAV format"), "application/json");
        return;
    }

    if (pcm.empty()) {
        res.status = 400;
        res.set_content(json_error("empty audio data"), "application/json");
        return;
    }

    std::lock_guard<std::mutex> lock(state.spk_mtx);

    if (!state.spk_ctx) {
        res.status = 503;
        res.set_content(json_error("speaker model not loaded"), "application/json");
        return;
    }

    // Reset state to avoid residual data from previous requests
    rs_reset(state.spk_ctx);

    if (rs_push_audio(state.spk_ctx, pcm.data(), static_cast<int>(pcm.size())) != 0) {
        res.status = 500;
        res.set_content(json_error("failed to push audio"), "application/json");
        return;
    }

    int status = rs_process(state.spk_ctx);
    if (status < 0) {
        res.status = 500;
        res.set_content(json_error("speaker embedding inference error"), "application/json");
        return;
    }

    float* emb_ptr = nullptr;
    int emb_dim = rs_get_embedding_output(state.spk_ctx, &emb_ptr);
    if (emb_dim <= 0 || !emb_ptr) {
        res.status = 500;
        res.set_content(json_error("failed to extract embedding"), "application/json");
        return;
    }

    // Build JSON array for embedding
    std::string json = "{\"dim\":" + std::to_string(emb_dim) + ",\"embedding\":[";
    for (int i = 0; i < emb_dim; i++) {
        if (i > 0) json += ",";
        char buf[32];
        snprintf(buf, sizeof(buf), "%.8f", emb_ptr[i]);
        json += buf;
    }
    json += "]}";

    res.set_content(json, "application/json");
}

// --------------- Cosine similarity helper ---------------

static float cosine_similarity(const float* a, const float* b, int dim) {
    float dot = 0.0f, norm_a = 0.0f, norm_b = 0.0f;
    for (int i = 0; i < dim; i++) {
        dot    += a[i] * b[i];
        norm_a += a[i] * a[i];
        norm_b += b[i] * b[i];
    }
    float denom = std::sqrt(norm_a) * std::sqrt(norm_b);
    return denom > 1e-10f ? dot / denom : 0.0f;
}

// --------------- Speaker Verify handler ---------------

// Extract embedding from a WAV buffer using the speaker context.
// Caller must hold state.spk_mtx.
static bool extract_embedding(rs_context_t* spk_ctx,
                               const std::vector<float>& pcm,
                               std::vector<float>& embedding, int& emb_dim) {
    rs_reset(spk_ctx);
    if (rs_push_audio(spk_ctx, pcm.data(), static_cast<int>(pcm.size())) != 0) return false;
    int status = rs_process(spk_ctx);
    if (status < 0) return false;
    float* emb_ptr = nullptr;
    emb_dim = rs_get_embedding_output(spk_ctx, &emb_ptr);
    if (emb_dim <= 0 || !emb_ptr) return false;
    embedding.assign(emb_ptr, emb_ptr + emb_dim);
    return true;
}

static void handle_speaker_verify(ServerState& state,
                                   const httplib::Request& req, httplib::Response& res) {
    // Expect multipart form: audio1=<wav>, audio2=<wav>
    // Optional query param: threshold (default 0.5)
    if (!req.has_file("audio1") || !req.has_file("audio2")) {
        res.status = 400;
        res.set_content(json_error("missing 'audio1' and/or 'audio2' fields (WAV audio)"),
                        "application/json");
        return;
    }

    auto file1 = req.get_file_value("audio1");
    auto file2 = req.get_file_value("audio2");

    std::vector<float> pcm1, pcm2;
    int sr1 = 16000, sr2 = 16000;

    if (!parse_wav_buffer(file1.content.data(), file1.content.size(), pcm1, &sr1)) {
        res.status = 400;
        res.set_content(json_error("invalid WAV format for audio1"), "application/json");
        return;
    }
    if (!parse_wav_buffer(file2.content.data(), file2.content.size(), pcm2, &sr2)) {
        res.status = 400;
        res.set_content(json_error("invalid WAV format for audio2"), "application/json");
        return;
    }
    if (pcm1.empty() || pcm2.empty()) {
        res.status = 400;
        res.set_content(json_error("empty audio data"), "application/json");
        return;
    }

    // Parse threshold
    float threshold = 0.5f;
    if (req.has_param("threshold")) {
        try {
            threshold = std::stof(req.get_param_value("threshold"));
        } catch (...) {
            res.status = 400;
            res.set_content(json_error("invalid threshold value"), "application/json");
            return;
        }
    }

    std::lock_guard<std::mutex> lock(state.spk_mtx);

    if (!state.spk_ctx) {
        res.status = 503;
        res.set_content(json_error("speaker model not loaded"), "application/json");
        return;
    }

    // Extract embeddings for both audio files
    std::vector<float> emb1, emb2;
    int dim1 = 0, dim2 = 0;

    if (!extract_embedding(state.spk_ctx, pcm1, emb1, dim1)) {
        res.status = 500;
        res.set_content(json_error("failed to extract embedding for audio1"), "application/json");
        return;
    }
    if (!extract_embedding(state.spk_ctx, pcm2, emb2, dim2)) {
        res.status = 500;
        res.set_content(json_error("failed to extract embedding for audio2"), "application/json");
        return;
    }

    if (dim1 != dim2) {
        res.status = 500;
        res.set_content(json_error("embedding dimension mismatch"), "application/json");
        return;
    }

    float score = cosine_similarity(emb1.data(), emb2.data(), dim1);
    bool same_speaker = score >= threshold;

    // Build JSON response
    char score_buf[32], thresh_buf[32];
    snprintf(score_buf, sizeof(score_buf), "%.6f", score);
    snprintf(thresh_buf, sizeof(thresh_buf), "%.6f", threshold);
    std::string json = "{\"score\":" + std::string(score_buf)
                     + ",\"same_speaker\":" + (same_speaker ? "true" : "false")
                     + ",\"threshold\":" + std::string(thresh_buf) + "}";

    res.set_content(json, "application/json");
}

// --------------- Signal handling for graceful shutdown ---------------

static httplib::Server* g_svr = nullptr;

static void signal_handler(int) {
    if (g_svr) g_svr->stop();
}

// --------------- CLI & main ---------------

static void print_usage(const char* prog) {
    std::cerr
        << "Usage:\n"
        << "  " << prog << " [options]\n\n"
        << "Options:\n"
        << "  --asr-model <path>   ASR model file (GGUF)\n"
        << "  --tts-model <path>   TTS model file (GGUF)\n"
        << "  --spk-model <path>   Speaker embedding model file (GGUF)\n"
        << "  --host <addr>        Listen address (default: 0.0.0.0)\n"
        << "  --port <num>         Listen port (default: 8080)\n"
        << "  --threads <num>      Inference threads (default: 4)\n"
        << "  --gpu <true|false>   Use GPU (default: true)\n"
        << std::endl;
}

int main(int argc, char* argv[]) {
    const char* asr_model = nullptr;
    const char* tts_model = nullptr;
    const char* spk_model = nullptr;
    std::string host = "0.0.0.0";
    int port = 8080;
    int n_threads = 4;
    bool use_gpu = true;

    for (int i = 1; i < argc; i++) {
        std::string arg = argv[i];
        if (arg == "--asr-model" && i + 1 < argc) {
            asr_model = argv[++i];
        } else if (arg == "--tts-model" && i + 1 < argc) {
            tts_model = argv[++i];
        } else if (arg == "--spk-model" && i + 1 < argc) {
            spk_model = argv[++i];
        } else if (arg == "--host" && i + 1 < argc) {
            host = argv[++i];
        } else if (arg == "--port" && i + 1 < argc) {
            port = std::stoi(argv[++i]);
        } else if (arg == "--threads" && i + 1 < argc) {
            n_threads = std::stoi(argv[++i]);
        } else if (arg == "--gpu" && i + 1 < argc) {
            std::string v = argv[++i];
            use_gpu = (v == "1" || v == "true" || v == "TRUE");
        } else if (arg == "--help" || arg == "-h") {
            print_usage(argv[0]);
            return 0;
        } else {
            std::cerr << "Unknown argument: " << arg << std::endl;
            print_usage(argv[0]);
            return 1;
        }
    }

    if (!asr_model && !tts_model && !spk_model) {
        std::cerr << "Error: at least one of --asr-model, --tts-model, or --spk-model is required.\n";
        print_usage(argv[0]);
        return 1;
    }

    ServerState state;

    // Load ASR model
    if (asr_model) {
        RS_LOG_INFO("[rs-server] Loading ASR model: %s", asr_model);
        rs_init_params_t p = rs_default_params();
        p.model_path = asr_model;
        p.n_threads = n_threads;
        p.use_gpu = use_gpu;
        p.task_type = RS_TASK_ASR_OFFLINE;
        state.asr_ctx = rs_init_from_file(p);
        if (!state.asr_ctx) {
            std::cerr << "[rs-server] Failed to load ASR model.\n";
            return 1;
        }
        RS_LOG_INFO("[rs-server] ASR model loaded.");
    }

    // Load TTS model
    if (tts_model) {
        RS_LOG_INFO("[rs-server] Loading TTS model: %s", tts_model);
        rs_init_params_t p = rs_default_params();
        p.model_path = tts_model;
        p.n_threads = n_threads;
        p.use_gpu = use_gpu;
        p.task_type = RS_TASK_TTS_OFFLINE;
        state.tts_ctx = rs_init_from_file(p);
        if (!state.tts_ctx) {
            std::cerr << "[rs-server] Failed to load TTS model.\n";
            return 1;
        }
        RS_LOG_INFO("[rs-server] TTS model loaded.");
    }

    // Load Speaker Embedding model
    if (spk_model) {
        RS_LOG_INFO("[rs-server] Loading speaker model: %s", spk_model);
        rs_init_params_t p = rs_default_params();
        p.model_path = spk_model;
        p.n_threads = n_threads;
        p.use_gpu = use_gpu;
        p.task_type = RS_TASK_SPEAKER_EMBED;
        state.spk_ctx = rs_init_from_file(p);
        if (!state.spk_ctx) {
            std::cerr << "[rs-server] Failed to load speaker model.\n";
            return 1;
        }
        RS_LOG_INFO("[rs-server] Speaker model loaded.");
    }

    // Setup HTTP server
    httplib::Server svr;
    g_svr = &svr;
    std::signal(SIGINT, signal_handler);
    std::signal(SIGTERM, signal_handler);

    svr.Get("/v1/health", handle_health);

    svr.Post("/v1/asr", [&state](const httplib::Request& req, httplib::Response& res) {
        handle_asr(state, req, res);
    });

    svr.Post("/v1/tts", [&state](const httplib::Request& req, httplib::Response& res) {
        handle_tts(state, req, res);
    });

    svr.Post("/v1/speaker-embed", [&state](const httplib::Request& req, httplib::Response& res) {
        handle_speaker_embed(state, req, res);
    });

    svr.Post("/v1/speaker-verify", [&state](const httplib::Request& req, httplib::Response& res) {
        handle_speaker_verify(state, req, res);
    });

    std::cout << "[rs-server] Listening on " << host << ":" << port << std::endl;
    if (state.asr_ctx) std::cout << "[rs-server] ASR endpoint: POST /v1/asr" << std::endl;
    if (state.tts_ctx) std::cout << "[rs-server] TTS endpoint: POST /v1/tts" << std::endl;
    if (state.spk_ctx) std::cout << "[rs-server] Speaker embed endpoint: POST /v1/speaker-embed" << std::endl;
    if (state.spk_ctx) std::cout << "[rs-server] Speaker verify endpoint: POST /v1/speaker-verify" << std::endl;
    std::cout << "[rs-server] Health check: GET /v1/health" << std::endl;

    if (!svr.listen(host, port)) {
        std::cerr << "[rs-server] Failed to start server on " << host << ":" << port << std::endl;
        return 1;
    }

    // Cleanup
    if (state.asr_ctx) rs_free(state.asr_ctx);
    if (state.tts_ctx) rs_free(state.tts_ctx);
    if (state.spk_ctx) rs_free(state.spk_ctx);

    return 0;
}
