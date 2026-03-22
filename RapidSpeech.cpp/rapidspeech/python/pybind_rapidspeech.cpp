#include <pybind11/pybind11.h>
#include <pybind11/numpy.h>
#include <pybind11/stl.h>

#include "rapidspeech.h"

namespace py = pybind11;

class RSAsrOffline {
public:
    RSAsrOffline(
        const std::string& model_path,
        int n_threads = 4,
        bool use_gpu = true
    ) {
        rs_init_params_t p = rs_default_params();
        p.model_path = model_path.c_str();
        p.n_threads  = n_threads;
        p.use_gpu    = use_gpu;

        ctx_ = rs_init_from_file(p);
        if (!ctx_) {
            throw std::runtime_error("Failed to initialize RapidSpeech context");
        }
    }

    ~RSAsrOffline() {
        if (ctx_) {
            rs_free(ctx_);
            ctx_ = nullptr;
        }
    }

    void push_audio(py::array_t<float, py::array::c_style | py::array::forcecast> pcm) {
        auto buf = pcm.request();
        if (buf.ndim != 1) {
            throw std::runtime_error("PCM must be 1-D float32 array");
        }

        float* data = static_cast<float*>(buf.ptr);
        int n = static_cast<int>(buf.shape[0]);

        if (rs_push_audio(ctx_, data, n) != 0) {
            throw std::runtime_error("rs_push_audio failed");
        }
    }

    int process() {
        return rs_process(ctx_);
    }

    std::string get_text() {
        const char* res = rs_get_text_output(ctx_);
        return res ? std::string(res) : std::string();
    }

private:
    rs_context_t* ctx_ = nullptr;
};

/* -------- Python Module -------- */

PYBIND11_MODULE(rapidspeech, m) {
    m.doc() = "RapidSpeech Python bindings";

    py::class_<RSAsrOffline>(m, "asr_offline")
        .def(py::init<
            const std::string&,
            int,
            bool
        >(),
        py::arg("model_path"),
        py::arg("n_threads") = 4,
        py::arg("use_gpu") = true)

        .def("push_audio", &RSAsrOffline::push_audio,
             py::arg("pcm"),
             py::call_guard<py::gil_scoped_release>())

        .def("process", &RSAsrOffline::process, py::call_guard<py::gil_scoped_release>())

        .def("get_text", &RSAsrOffline::get_text);
}