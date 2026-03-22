#include "core/rs_context.h"
#include "utils/rs_log.h"
#include "ggml-alloc.h"
#include "ggml-cpu.h"
#include "ggml-backend.h"
#include "gguf.h"

// Include corresponding backend headers based on macro definitions
#ifdef RS_USE_METAL
#include "ggml-metal.h"
#endif
#ifdef RS_USE_CUDA
#include "ggml-cuda.h"
#endif
#ifdef RS_USE_VULKAN
#include "ggml-vulkan.h"
#endif
#ifdef RS_USE_CANN
#include "ggml-cann.h"
#endif

#include <iostream>
#include <stdexcept>
#include <functional>
#include <unordered_map>
#include <vector>

// Global registry for model architectures
static std::unordered_map<std::string, ModelCreator>& get_model_registry() {
    static std::unordered_map<std::string, ModelCreator> registry;
    return registry;
}

void rs_register_model_arch(const std::string& arch, ModelCreator creator) {
    get_model_registry()[arch] = creator;
}

rs_context_t::rs_context_t() : sched(nullptr), ctx_gguf(nullptr), gguf_data(nullptr) {}

rs_context_t::~rs_context_t() {
    // IMPORTANT: Clear processor and model first.
    // This ensures that any RSState or persistent backend buffers (especially Metal resources)
    // are deallocated while the backends and scheduler are still valid.
    processor.reset();
    model.reset();

    // Free weight buffers explicitly to satisfy Metal residency set assertions
    for (auto buf : weight_buffers) {
        ggml_backend_buffer_free(buf);
    }
    weight_buffers.clear();

    // Now it is safe to free the scheduler as all its managed tensors/buffers are gone
    if (sched) ggml_backend_sched_free(sched);

    // Free all backend instances
    for (auto b : backends) ggml_backend_free(b);

    // Cleanup GGUF related resources
    if (ctx_gguf) gguf_free(ctx_gguf);
    if (gguf_data) ggml_free(gguf_data);
}

/**
 * Initialize backends: try CUDA -> Metal -> Vulkan -> CANN -> CPU in order of priority
 */
bool rs_context_t::init_backend() {
    bool gpu_initialized = false;

    if (params.use_gpu) {
#ifdef RS_USE_CUDA
        if (!gpu_initialized) {
            ggml_backend_t b = ggml_backend_cuda_init(0); // Use device 0 by default
            if (b) {
                backends.push_back(b);
                RS_LOG_INFO("CUDA backend added to scheduler.");
                gpu_initialized = true;
            }
        }
#endif

#ifdef RS_USE_METAL
        if (!gpu_initialized) {
            ggml_backend_t b = ggml_backend_metal_init();
            if (b) {
                backends.push_back(b);
                RS_LOG_INFO("Metal backend added to scheduler.");
                gpu_initialized = true;
            }
        }
#endif

#ifdef RS_USE_VULKAN
        if (!gpu_initialized) {
            ggml_backend_t b = ggml_backend_vk_init(0);
            if (b) {
                backends.push_back(b);
                RS_LOG_INFO("Vulkan backend added to scheduler.");
                gpu_initialized = true;
            }
        }
#endif

#ifdef RS_USE_CANN
        if (!gpu_initialized) {
            ggml_backend_t b = ggml_backend_cann_init(0);
            if (b) {
                backends.push_back(b);
                RS_LOG_INFO("CANN backend added to scheduler.");
                gpu_initialized = true;
            }
        }
#endif

        if (!gpu_initialized) {
            RS_LOG_WARN("GPU requested but no supported GPU backend could be initialized. Falling back to CPU.");
        }
    }

    // Always add CPU backend as a fallback or for collaborative computing
    ggml_backend_t cpu = ggml_backend_cpu_init();
    if (cpu) {
        ggml_backend_cpu_set_n_threads(cpu, params.n_threads);
        backends.push_back(cpu);
    } else {
        RS_LOG_ERR("Failed to initialize CPU backend.");
        return false;
    }

    // Initialize the scheduler to distribute computation tasks across multiple backends.
    sched = ggml_backend_sched_new(backends.data(), nullptr, (int)backends.size(), 16384, false, false);
    return sched != nullptr;
}

rs_context_t* rs_context_init_internal(rs_init_params_t params) {
    auto ctx = std::make_unique<rs_context_t>();
    ctx->params = params;

    // 1. Hardware detection and backend initialization
    if (!ctx->init_backend()) {
        RS_LOG_ERR("Failed to initialize backend scheduler.");
        return nullptr;
    }

    // 2. Load GGUF handle and auto-populate ggml_context metadata
    struct gguf_init_params g_params = {
        /*.no_alloc =*/ true,
        /*.ctx      =*/ &ctx->gguf_data
    };

    ctx->ctx_gguf = gguf_init_from_file(params.model_path, g_params);
    if (!ctx->ctx_gguf) {
        RS_LOG_ERR("Failed to load GGUF file: %s", params.model_path);
        return nullptr;
    }

    // 3. Allocate physical memory buffers on the primary backend (resolves buffer.buft == NULL)
    // Capture the buffer handle so we can free it later
    ggml_backend_buffer_t weight_buffer = ggml_backend_alloc_ctx_tensors(ctx->gguf_data, ctx->backends[0]);
    if (weight_buffer) {
        ctx->weight_buffers.push_back(weight_buffer);
    }

    // 4. Load tensor data from the binary blob in the file
    FILE * f = fopen(params.model_path, "rb");
    if (!f) {
        RS_LOG_ERR("Failed to open model file for data loading: %s", params.model_path);
        return nullptr;
    }

    size_t data_offset = gguf_get_data_offset(ctx->ctx_gguf);
    int64_t n_tensors = gguf_get_n_tensors(ctx->ctx_gguf);
    std::vector<char> read_buf;

    for (int64_t i = 0; i < n_tensors; ++i) {
        const char * name = gguf_get_tensor_name(ctx->ctx_gguf, i);
        struct ggml_tensor * t = ggml_get_tensor(ctx->gguf_data, name);

        if (t) {
            size_t t_offset = gguf_get_tensor_offset(ctx->ctx_gguf, i);
            size_t t_size = ggml_nbytes(t);

            if (t_size > 0) {
                if (read_buf.size() < t_size) read_buf.resize(t_size);

                fseek(f, data_offset + t_offset, SEEK_SET);
                if (fread(read_buf.data(), 1, t_size, f) != t_size) {
                    RS_LOG_ERR("Failed to read data for tensor: %s", name);
                    fclose(f);
                    return nullptr;
                }

                ggml_backend_tensor_set(t, read_buf.data(), 0, t_size);
            }
        }
    }
    fclose(f);

    // 5. Identify and load model architecture
    int64_t arch_key = gguf_find_key(ctx->ctx_gguf, "general.architecture");
    if (arch_key == -1) {
        RS_LOG_ERR("GGUF file missing 'general.architecture' key.");
        return nullptr;
    }
    std::string arch = gguf_get_val_str(ctx->ctx_gguf, arch_key);
    RS_LOG_INFO("Architecture detected: %s", arch.c_str());

    auto it = get_model_registry().find(arch);
    if (it == get_model_registry().end()) {
        RS_LOG_ERR("Unsupported architecture: %s", arch.c_str());
        return nullptr;
    }

    ctx->model = it->second();

    // 6. Initialize processor and audio frontend
    ctx->processor = std::make_unique<RSProcessor>(ctx->model, ctx->sched);

    // 7. Execute model-specific Load (e.g., mapping pointers, loading CMVN)
    if (!ctx->model->Load(ctx, ctx->backends[0])) {
        RS_LOG_ERR("Model load failed.");
        return nullptr;
    }

    RS_LOG_INFO("RapidSpeech context core initialized successfully.");
    return ctx.release();
}

/**
 * Helper to safely initialize a ggml context and graph.
 * Prevents 0x0 crashes by checking allocation results.
 */
bool init_compute_ctx(struct ggml_context ** ctx, struct ggml_cgraph ** gf, int n_nodes) {
    // We add 1MB of buffer to the tensor overhead to be safe
    size_t mem_size = n_nodes * ggml_tensor_overhead() + (1024 * 1024);
    struct ggml_init_params params = { mem_size, nullptr, true };
    *ctx = ggml_init(params);
    if (!(*ctx)) {
        RS_LOG_ERR("ggml_init failed: out of memory for context.");
        return false;
    }
    *gf = ggml_new_graph_custom(*ctx, n_nodes, false);
    if (!(*gf)) {
        RS_LOG_ERR("ggml_new_graph_custom failed: too many nodes or out of memory.");
        return false;
    }
    return true;
}