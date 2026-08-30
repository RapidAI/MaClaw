#include "fangtang_ml307_transport.h"

#include <algorithm>
#include <atomic>
#include <charconv>
#include <chrono>
#include <cctype>
#include <condition_variable>
#include <cstring>
#include <memory>
#include <mutex>
#include <limits>
#include <string>
#include <vector>

#include "at_modem.h"
#include "at_uart.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

static const char *TAG = "maclaw_ml307";
static std::unique_ptr<AtModem> s_modem;

// Modem discovery/network registration is a lifecycle operation. HTTP traffic
// only borrows the shared AtUart and must not hold this lock during a request.
static std::mutex s_lifecycle_mutex;
static std::atomic<bool> s_admission_open{true};
static std::atomic<bool> s_start_in_progress{false};
static std::atomic<bool> s_start_stop_requested{false};

/* Admission and active-borrower accounting form one lifecycle boundary.  A
 * request is counted before it observes the shared modem/UART, so quiesce can
 * close admission and then wait for every pre-existing borrower to finish its
 * MHTTP cleanup.  Keep this separate from s_lifecycle_mutex: HTTP traffic
 * must not serialize for its full lifetime just because start/probe needs the
 * modem object. */
static std::mutex s_http_borrower_mutex;
static std::condition_variable s_http_borrower_cv;
static unsigned s_active_http_borrowers;

// +MHTTPCREATE does not identify the caller. Serialize only allocation so the
// one request whose awaiting_create_ flag is set can claim the returned ID.
// Once allocated, every MHTTP command and URC carries that ID and requests may
// proceed independently in the modem's four hardware HTTP slots.
static std::mutex s_http_create_mutex;

class Ml307Request;
static std::mutex s_foreground_mutex;
static Ml307Request *s_foreground_request;
/* Requests are registered by a logical worker owner, rather than by a board
 * or modem role.  A meeting upload can therefore be cancelled during a
 * lifecycle stop without borrowing the foreground slot used by commands. */
static std::mutex s_owner_requests_mutex;
static std::vector<Ml307Request *> s_owner_requests;

// The modem exposes exactly four HTTP IDs (0..3). Waiting here avoids turning
// a brief burst (poll + ACK + foreground + asset) into MHTTPCREATE failures.
static std::mutex s_slot_mutex;
static std::condition_variable s_slot_cv;
static unsigned s_slots_in_use;
static constexpr unsigned kHttpSlotCount = 4;
// MHTTPCONTENT accepts at most 4 KiB per raw UART transfer. Large voice or
// meeting bodies therefore have to be appended in bounded pieces instead of
// being submitted as one modem command. The vendor Ml307Http::Write path uses
// append mode (1) for every streamed chunk, including the first. Mode 0 is the
// SetContent one-shot path and replaces the request body; using mode 0 before
// streamed append chunks makes large WAV PUTs firmware-dependent and can leave
// the upload incomplete even though the small JSON requests still work.
static constexpr size_t kHttpContentChunkSize = 4096;
static constexpr int kHttpContentWriteTimeoutMs = 10000;
// Some ML307 firmware revisions emit CEREG only when registration changes and
// do not emit a PDP-loss URC. Poll both registration and PDP state so the
// application recovery task can observe a detached SIM or dropped context
// instead of trusting the last successful boot state indefinitely.
static constexpr int kNetworkProbeIntervalMs = 5000;

/* Parse the modem's decoded response-header blob without relying on substring
 * matches.  This keeps malformed framing from turning into a successful EOF
 * and makes the cellular path agree with the generic EC801E client. */
static bool parse_http_header_framing(const std::string& decoded,
                                      bool *out_chunked,
                                      bool *out_content_length_zero) {
    if (!out_chunked || !out_content_length_zero) return false;
    *out_chunked = false;
    *out_content_length_zero = false;
    bool transfer_seen = false;
    bool content_length_seen = false;
    size_t content_length_value = 0;
    bool status_line_seen = false;
    size_t cursor = 0;
    while (cursor <= decoded.size()) {
        const size_t line_end = decoded.find('\n', cursor);
        const size_t end = line_end == std::string::npos ? decoded.size() : line_end;
        size_t last = end;
        if (last > cursor && decoded[last - 1] == '\r') --last;
        if (cursor == last) {
            if (line_end == std::string::npos) break;
            cursor = line_end + 1;
            continue;
        }
        const std::string line = decoded.substr(cursor, last - cursor);
        if (line.rfind("HTTP/", 0) == 0) {
            const size_t version_end = line.find(' ', 5u);
            if (version_end == std::string::npos || version_end < 8u ||
                version_end > line.size() - 4u ||
                !std::isdigit(static_cast<unsigned char>(line[5])) ||
                line[6] != '.' ||
                !std::isdigit(static_cast<unsigned char>(line[7])) ||
                !std::isdigit(static_cast<unsigned char>(line[version_end + 1])) ||
                !std::isdigit(static_cast<unsigned char>(line[version_end + 2])) ||
                !std::isdigit(static_cast<unsigned char>(line[version_end + 3])) ||
                (version_end + 4u < line.size() && line[version_end + 4u] != ' ')) {
                return false;
            }
            status_line_seen = true;
            if (line_end == std::string::npos) break;
            cursor = line_end + 1;
            continue;
        }
        const size_t colon = line.find(':');
        if (colon == std::string::npos || colon == 0u) return false;
        std::string name = line.substr(0, colon);
        std::string value = line.substr(colon + 1);
        while (!name.empty() && (name.back() == ' ' || name.back() == '\t')) name.pop_back();
        size_t first = 0;
        while (first < value.size() && (value[first] == ' ' || value[first] == '\t')) ++first;
        value.erase(0, first);
        while (!value.empty() && (value.back() == ' ' || value.back() == '\t')) value.pop_back();
        for (unsigned char c : name) {
            if (c < 0x21u || c > 0x7eu || c == ':' || c == '"') return false;
        }
        for (unsigned char c : value) {
            if (c == '\r' || c == '\n' || c == '"' || c == 0x7fu ||
                (c < 0x20u && c != '\t')) return false;
        }
        std::transform(name.begin(), name.end(), name.begin(), [](unsigned char c) {
            return static_cast<char>(std::tolower(c));
        });
        if (name == "transfer-encoding") {
            if (transfer_seen) return false;
            transfer_seen = true;
            size_t token_begin = 0;
            size_t token_count = 0;
            std::string token;
            while (token_begin <= value.size()) {
                const size_t comma = value.find(',', token_begin);
                const size_t token_end = comma == std::string::npos ? value.size() : comma;
                size_t token_first = token_begin;
                size_t token_last = token_end;
                while (token_first < token_last &&
                       (value[token_first] == ' ' || value[token_first] == '\t')) ++token_first;
                while (token_last > token_first &&
                       (value[token_last - 1] == ' ' || value[token_last - 1] == '\t')) --token_last;
                if (token_first == token_last) return false;
                token = value.substr(token_first, token_last - token_first);
                for (unsigned char c : token) {
                    if (!std::isalnum(c) && c != '-' && c != '_') return false;
                }
                ++token_count;
                if (comma == std::string::npos) break;
                token_begin = comma + 1;
            }
            std::transform(token.begin(), token.end(), token.begin(), [](unsigned char c) {
                return static_cast<char>(std::tolower(c));
            });
            if (token_count != 1u || token != "chunked") return false;
            *out_chunked = true;
        } else if (name == "content-length") {
            size_t length = 0;
            const auto result = std::from_chars(value.data(), value.data() + value.size(), length, 10);
            if (result.ec != std::errc() || result.ptr != value.data() + value.size()) return false;
            if (content_length_seen && length != content_length_value) return false;
            content_length_seen = true;
            content_length_value = length;
            *out_content_length_zero = length == 0u;
        }
        if (line_end == std::string::npos) break;
        cursor = line_end + 1;
    }
    return status_line_seen;
}

static TaskHandle_t s_network_probe_task;
static SemaphoreHandle_t s_network_probe_stopped;
static std::atomic<bool> s_network_probe_stop_requested{false};
static std::mutex s_network_probe_mutex;
/* A successful System Sleep PREPARE stops the retained probe task.  If ABORT
 * races the task's final exit, remember the old generation's intent so that
 * the exiting task recreates it after releasing its own task handle. */
static std::atomic<bool> s_network_probe_restart_after_stop{false};
static std::mutex s_system_sleep_mutex;
static bool s_system_sleep_preparing;
static bool s_system_sleep_was_admitted;
static bool s_system_sleep_probe_was_running;

static void ensure_network_probe_task();

static void network_probe_task(void *) {
    while (!s_network_probe_stop_requested.load()) {
        /* The probe owns no persistent transport state.  Use its direct task
         * notification as the stop token rather than making quiesce wait for
         * a five-second periodic sleep. */
        (void)ulTaskNotifyTake(pdTRUE, pdMS_TO_TICKS(kNetworkProbeIntervalMs));
        if (s_network_probe_stop_requested.load()) break;
        std::shared_ptr<AtUart> uart;
        {
            std::lock_guard<std::mutex> lock(s_lifecycle_mutex);
            if (s_modem) uart = s_modem->GetAtUart();
        }
        if (!uart) continue;
        // CEREG refreshes AtModem::network_ready_; MIPCALL refreshes the PDP
        // context on ML307 and repairs a registered-but-addressless state.
        (void)uart->SendCommand("AT+CEREG?", 1000);
        (void)uart->SendCommand("AT+MIPCALL?", 1000);
    }
    {
        std::lock_guard<std::mutex> lock(s_network_probe_mutex);
        if (s_network_probe_task == xTaskGetCurrentTaskHandle()) {
            s_network_probe_task = nullptr;
        }
    }
    if (s_network_probe_stopped) xSemaphoreGive(s_network_probe_stopped);
    /* Do not create the replacement until this instance has stopped touching
     * the UART and published its completion. `ensure_network_probe_task()`
     * drains that completion token before publishing its new generation, so a
     * subsequent PREPARE can never mistake this old task's exit for the new
     * task's acknowledgement. Permanent quiesce never sets this marker. */
    if (s_network_probe_restart_after_stop.exchange(false) &&
        s_admission_open.load()) {
        ensure_network_probe_task();
    }
    vTaskDelete(nullptr);
}

static void ensure_network_probe_task() {
    std::lock_guard<std::mutex> lock(s_network_probe_mutex);
    if (s_network_probe_task || !s_admission_open.load()) return;
    if (!s_network_probe_stopped) {
        s_network_probe_stopped = xSemaphoreCreateBinary();
        if (!s_network_probe_stopped) {
            ESP_LOGW(TAG, "cannot allocate ML307 network probe completion semaphore");
            return;
        }
    }
    while (xSemaphoreTake(s_network_probe_stopped, 0) == pdTRUE) {}
    s_network_probe_stop_requested.store(false);
    if (xTaskCreate(network_probe_task, "ml307_network_probe", 3072, nullptr, 2,
                    &s_network_probe_task) != pdPASS) {
        s_network_probe_task = nullptr;
        ESP_LOGW(TAG, "cannot start ML307 network probe");
    }
}

static esp_err_t stop_network_probe_task(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    TaskHandle_t task = nullptr;
    {
        std::lock_guard<std::mutex> lock(s_network_probe_mutex);
        task = s_network_probe_task;
    }
    if (!task) return ESP_OK;
    if (xTaskGetCurrentTaskHandle() == task) return ESP_ERR_INVALID_STATE;
    s_network_probe_stop_requested.store(true);
    xTaskNotifyGive(task);
    if (!s_network_probe_stopped ||
        xSemaphoreTake(s_network_probe_stopped, pdMS_TO_TICKS(timeout_ms)) != pdTRUE) {
        return ESP_ERR_TIMEOUT;
    }
    ESP_LOGI(TAG, "ML307 network probe stopped");
    return ESP_OK;
}

static bool network_probe_task_is_running(void) {
    std::lock_guard<std::mutex> lock(s_network_probe_mutex);
    return s_network_probe_task != nullptr;
}

/* Caller holds s_system_sleep_mutex.  This is deliberately the common close
 * path for permanent quiesce and reversible PREPARE: admission closes before
 * start/probe/HTTP work is observed, so no new borrower can slip past the
 * subsequent drain. */
static esp_err_t close_transport_and_drain(uint32_t timeout_ms) {
    {
        std::lock_guard<std::mutex> lock(s_http_borrower_mutex);
        s_admission_open.store(false);
    }
    s_slot_cv.notify_all();
    s_start_stop_requested.store(true);
    const TickType_t started = xTaskGetTickCount();
    const TickType_t budget = pdMS_TO_TICKS(timeout_ms);
    while (s_start_in_progress.load()) {
        if (xTaskGetTickCount() - started >= budget) {
            ESP_LOGW(TAG, "ML307 transport start did not stop before quiesce deadline");
            return ESP_ERR_TIMEOUT;
        }
        vTaskDelay(pdMS_TO_TICKS(25));
    }
    const TickType_t elapsed = xTaskGetTickCount() - started;
    uint32_t remaining_ms = 1u;
    if (elapsed < budget) {
        const TickType_t remaining_ticks = budget - elapsed;
        remaining_ms = (uint32_t)(remaining_ticks * portTICK_PERIOD_MS);
        if (remaining_ms == 0) remaining_ms = 1u;
    }
    esp_err_t probe_err = stop_network_probe_task(remaining_ms);
    if (probe_err != ESP_OK) {
        ESP_LOGW(TAG, "ML307 network probe did not stop during quiesce: %s",
                 esp_err_to_name(probe_err));
        return probe_err;
    }

    const TickType_t after_probe = xTaskGetTickCount() - started;
    if (after_probe >= budget) {
        ESP_LOGW(TAG, "ML307 HTTP borrowers did not drain before quiesce deadline");
        return ESP_ERR_TIMEOUT;
    }
    const uint32_t drain_ms = std::max<uint32_t>(
        1u, (uint32_t)((budget - after_probe) * portTICK_PERIOD_MS));
    {
        std::unique_lock<std::mutex> lock(s_http_borrower_mutex);
        if (!s_http_borrower_cv.wait_for(
                lock, std::chrono::milliseconds(drain_ms),
                [] { return s_active_http_borrowers == 0; })) {
            ESP_LOGW(TAG, "ML307 quiesce timed out with %u active HTTP borrower(s)",
                     s_active_http_borrowers);
            return ESP_ERR_TIMEOUT;
        }
    }
    return ESP_OK;
}

static bool acquire_http_slot(std::unique_lock<std::mutex>& lock,
                              int timeout_ms,
                              const std::atomic<bool>& cancelled) {
    return s_slot_cv.wait_for(
        lock, std::chrono::milliseconds(timeout_ms),
        [&cancelled] {
            return s_slots_in_use < kHttpSlotCount || cancelled.load() ||
                   !s_admission_open.load();
        });
}

static bool argument_int(const std::vector<AtArgumentValue>& arguments,
                         size_t index, int *value) {
    if (!value || index >= arguments.size() ||
        arguments[index].type != AtArgumentValue::Type::Int) {
        return false;
    }
    *value = arguments[index].int_value;
    return true;
}

static bool argument_string(const std::vector<AtArgumentValue>& arguments,
                            size_t index, const std::string **value) {
    if (!value || index >= arguments.size() ||
        arguments[index].type != AtArgumentValue::Type::String) {
        return false;
    }
    *value = &arguments[index].string_value;
    return true;
}

static int method_number(const char *method) {
    if (!method || !method[0]) return 0;
    if (!strcmp(method, "GET")) return 1;
    if (!strcmp(method, "POST")) return 2;
    if (!strcmp(method, "PUT")) return 3;
    if (!strcmp(method, "DELETE")) return 4;
    if (!strcmp(method, "HEAD")) return 5;
    return 0;
}

/* Lifetime must enclose the complete Ml307Request object.  In particular,
 * Ml307Request's destructor sends MHTTPDEL and removes its URC callback before
 * this borrower decrements the counter; a successful quiesce therefore proves
 * neither operation is still touching the modem/UART. */
class Ml307HttpBorrower {
public:
    Ml307HttpBorrower() = default;
    ~Ml307HttpBorrower() { Release(); }

    Ml307HttpBorrower(const Ml307HttpBorrower&) = delete;
    Ml307HttpBorrower& operator=(const Ml307HttpBorrower&) = delete;

    bool Acquire() {
        std::unique_lock<std::mutex> borrower_lock(s_http_borrower_mutex);
        if (!s_admission_open.load()) return false;

        /* Lock order is borrower -> lifecycle everywhere.  Quiesce only holds
         * the borrower lock long enough to close admission, then releases it
         * before joining workers, so it cannot deadlock behind a UART call. */
        std::lock_guard<std::mutex> lifecycle_lock(s_lifecycle_mutex);
        if (!s_admission_open.load() || !s_modem || !s_modem->network_ready()) {
            return false;
        }
        uart_ = s_modem->GetAtUart();
        if (!uart_) return false;
        ++s_active_http_borrowers;
        active_ = true;
        return true;
    }

    const std::shared_ptr<AtUart>& uart() const { return uart_; }

private:
    void Release() {
        if (!active_) return;
        std::lock_guard<std::mutex> lock(s_http_borrower_mutex);
        active_ = false;
        uart_.reset();
        if (s_active_http_borrowers > 0) --s_active_http_borrowers;
        s_http_borrower_cv.notify_all();
    }

    std::shared_ptr<AtUart> uart_;
    bool active_ = false;
};

class Ml307Request {
public:
    Ml307Request(std::shared_ptr<AtUart> uart, int timeout_ms,
                 const void *cancellation_owner, bool foreground)
        : uart_(std::move(uart)), timeout_ms_(timeout_ms > 0 ? timeout_ms : 30000),
          cancellation_owner_(cancellation_owner), foreground_(foreground) {
        callback_ = uart_->RegisterUrcCallback(
            [this](const std::string& command,
                   const std::vector<AtArgumentValue>& arguments) {
                HandleUrc(command, arguments);
            });
        if (foreground_) {
            std::lock_guard<std::mutex> lock(s_foreground_mutex);
            s_foreground_request = this;
        }
        if (cancellation_owner_) {
            std::lock_guard<std::mutex> lock(s_owner_requests_mutex);
            s_owner_requests.push_back(this);
        }
    }

    ~Ml307Request() {
        if (cancellation_owner_) {
            std::lock_guard<std::mutex> lock(s_owner_requests_mutex);
            auto request = std::find(s_owner_requests.begin(), s_owner_requests.end(), this);
            if (request != s_owner_requests.end()) s_owner_requests.erase(request);
        }
        if (foreground_) {
            std::lock_guard<std::mutex> lock(s_foreground_mutex);
            if (s_foreground_request == this) s_foreground_request = nullptr;
        }
        Close();
        uart_->UnregisterUrcCallback(callback_);
        if (slot_acquired_) {
            std::lock_guard<std::mutex> lock(s_slot_mutex);
            --s_slots_in_use;
            slot_acquired_ = false;
            s_slot_cv.notify_one();
        }
    }

    Ml307Request(const Ml307Request&) = delete;
    Ml307Request& operator=(const Ml307Request&) = delete;

    const void *cancellation_owner() const { return cancellation_owner_; }

    bool Open(const char *method, const char *url, const char *content_type,
              const char *authorization, const char *extra_header_name,
              const char *extra_header_value, const void *body,
              size_t body_len,
              ml307_transport_body_reader_t body_reader = nullptr,
              void *body_reader_context = nullptr,
              void *stream_buffer = nullptr,
              size_t stream_buffer_size = 0) {
        int method_id = method_number(method);
        if (!method_id || !ParseUrl(url)) return false;
        if (body_len > 0 && !body && !body_reader) return false;
        if (body_reader && (!stream_buffer || stream_buffer_size == 0)) return false;
        auto header_value_safe = [](const char *value) {
            if (!value) return true;
            for (const unsigned char *p = reinterpret_cast<const unsigned char *>(value);
                 *p; ++p) {
                if (*p == '\r' || *p == '\n' || *p == '"') return false;
            }
            return true;
        };
        auto header_name_safe = [](const char *value) {
            if (!value) return true;
            for (const unsigned char *p = reinterpret_cast<const unsigned char *>(value);
                 *p; ++p) {
                if (*p == '\r' || *p == '\n' || *p == '"' || *p == ':' ||
                    *p == ' ' || *p == '\t') return false;
            }
            return true;
        };
        if (!header_value_safe(content_type) || !header_value_safe(authorization) ||
            !header_name_safe(extra_header_name) ||
            !header_value_safe(extra_header_value)) {
            return false;
        }
        method_ = method;

        {
            std::lock_guard<std::mutex> lock(state_mutex_);
            body_.clear();
            body_offset_ = 0;
            error_ = false;
            eof_ = false;
            headers_received_ = false;
            response_chunked_ = false;
            body_forbidden_ = false;
        }

        {
            std::unique_lock<std::mutex> slot_lock(s_slot_mutex);
            bool available = acquire_http_slot(slot_lock, timeout_ms_, cancelled_);
            if (!available || cancelled_.load() || !s_admission_open.load()) return false;
            ++s_slots_in_use;
            slot_acquired_ = true;
        }

        // Every live request receives every UART URC. Only the request inside
        // this allocation critical section advertises awaiting_create_, so an
        // existing long poll never has its ID overwritten by a later request.
        {
            std::lock_guard<std::mutex> create_lock(s_http_create_mutex);
            {
                std::lock_guard<std::mutex> lock(state_mutex_);
                awaiting_create_ = true;
            }
            std::string command = "AT+MHTTPCREATE=\"" + origin_ + "\"";
            if (!uart_->SendCommand(command)) {
                std::lock_guard<std::mutex> lock(state_mutex_);
                awaiting_create_ = false;
                error_ = true;
                return false;
            }
            std::unique_lock<std::mutex> lock(state_mutex_);
            const int create_timeout_ms = std::min(timeout_ms_, 3000);
            bool created = state_cv_.wait_for(
                lock, std::chrono::milliseconds(create_timeout_ms),
                [this] { return http_id_.load() >= 0 || error_; });
            awaiting_create_ = false;
            if (!created || http_id_.load() < 0 || error_ || cancelled_) {
                return false;
            }
            ESP_LOGI(TAG, "ML307 HTTP connection created, ID: %d", http_id_.load());
        }

        const int id = http_id_.load();
        if (cancelled_.load()) return false;
        if (secure_ && !SendConfig("ssl", "1,0")) return false;
        if (!SendConfig("encoding", "0,0")) return false;

        std::vector<std::pair<std::string, std::string>> headers;
        headers.emplace_back("Accept", "application/json");
        if (content_type && content_type[0]) {
            headers.emplace_back("Content-Type", content_type);
        }
        if (authorization && authorization[0]) {
            headers.emplace_back("Authorization", authorization);
        }
        if (extra_header_name && extra_header_name[0] && extra_header_value) {
            headers.emplace_back(extra_header_name, extra_header_value);
        }
        for (size_t i = 0; i < headers.size(); ++i) {
            if (cancelled_.load()) return false;
            const std::string line = headers[i].first + ": " + headers[i].second;
            std::string command = "AT+MHTTPHEADER=" + std::to_string(id) + "," +
                                  (i + 1 == headers.size() ? "0," : "1,") +
                                  std::to_string(line.size()) + ",\"" + line + "\"";
            if (!uart_->SendCommand(command)) return false;
        }

        if ((body || body_reader) && body_len) {
            const char *bytes = static_cast<const char *>(body);
            size_t offset = 0;
            while (offset < body_len) {
                if (cancelled_.load()) return false;
                size_t count = std::min(body_len - offset, kHttpContentChunkSize);
                const char *content = bytes ? bytes + offset
                                            : static_cast<const char *>(stream_buffer);
                if (body_reader) {
                    if (!stream_buffer || stream_buffer_size < count) {
                        ESP_LOGE(TAG, "ML307 HTTP stream buffer too small: need=%u have=%u",
                                 (unsigned)count, (unsigned)stream_buffer_size);
                        return false;
                    }
                    size_t read_bytes = 0;
                    esp_err_t read_err = body_reader(body_reader_context, stream_buffer,
                                                     count, &read_bytes);
                    if (read_err != ESP_OK || read_bytes != count) {
                        ESP_LOGE(TAG,
                                 "ML307 HTTP body read failed: offset=%u wanted=%u got=%u err=%s",
                                 (unsigned)offset, (unsigned)count,
                                 (unsigned)read_bytes, esp_err_to_name(read_err));
                        return false;
                    }
                }
                // Match the proven vendor streaming implementation exactly:
                // append mode for every 4 KiB write, including offset zero.
                constexpr int append = 1;
                std::string command = "AT+MHTTPCONTENT=" + std::to_string(id) +
                                      "," + std::to_string(append) + "," +
                                      std::to_string(count);
                if (!uart_->SendCommandWithData(
                        command, kHttpContentWriteTimeoutMs, true,
                        content, count)) {
                    ESP_LOGE(TAG,
                             "ML307 HTTP content upload failed: id=%d offset=%u chunk=%u total=%u",
                             id, (unsigned)offset, (unsigned)count, (unsigned)body_len);
                    return false;
                }
                offset += count;
                if ((offset % (32u * 1024u)) == 0 || offset == body_len) {
                    ESP_LOGI(TAG, "ML307 HTTP body staged: id=%d %u/%u bytes",
                             id, (unsigned)offset, (unsigned)body_len);
                }
                // Give the modem parser and UART RX task a scheduling point
                // during long PCM uploads. Small Bread/Wi-Fi requests never
                // enter this ML307-only path.
                taskYIELD();
            }
        }
        if (cancelled_.load() || !SendConfig("encoding", "1,1")) return false;

        std::string command = "AT+MHTTPREQUEST=" + std::to_string(id) + "," +
                              std::to_string(method_id) + ",0," +
                              uart_->EncodeHex(path_);
        return !cancelled_.load() && uart_->SendCommand(command);
    }

    bool WaitForHeaders(int *status_code) {
        std::unique_lock<std::mutex> lock(state_mutex_);
        bool ready = state_cv_.wait_for(
            lock, std::chrono::milliseconds(timeout_ms_),
            [this] { return headers_received_ || error_ || cancelled_; });
        if (!ready || error_ || cancelled_ || !headers_received_) return false;
        *status_code = status_code_;
        return true;
    }

    // Returns bytes copied, zero at EOF, and -1 on timeout/error/cancel.
    int Read(char *buffer, size_t capacity) {
        if (capacity > 0 && !buffer) return -1;
        std::unique_lock<std::mutex> lock(state_mutex_);
        bool ready = state_cv_.wait_for(
            lock, std::chrono::milliseconds(timeout_ms_),
            [this] { return !body_.empty() || eof_ || error_ || cancelled_; });
        if (!ready || error_ || cancelled_) return -1;
        if (body_.empty() && eof_) return 0;
        size_t count = std::min(capacity, body_.size());
        memcpy(buffer, body_.data(), count);
        body_.erase(0, count);
        return static_cast<int>(count);
    }

    void RequestCancel() {
        cancelled_.store(true);
        {
            std::lock_guard<std::mutex> lock(state_mutex_);
            state_cv_.notify_all();
        }
        s_slot_cv.notify_all();
    }

    void Cancel() {
        RequestCancel();
        // If MHTTPCREATE is in progress, wait until Open has claimed the
        // returned ID before deleting it. Otherwise a cancellation between the
        // command's OK and its allocation URC can leak one of four modem slots.
        // Never wait indefinitely here: a wedged UART/create command used to
        // block the high-priority cancel worker, leaving the interaction task
        // and cancellation UI stuck even though Cancel() had already woken all
        // request waiters. Open() observes cancelled_ and its bounded 3-second
        // create wait will close/release any late slot itself.
        std::unique_lock<std::mutex> create_lock(s_http_create_mutex,
                                                 std::defer_lock);
        const auto lock_deadline = std::chrono::steady_clock::now() +
                                   std::chrono::milliseconds(3500);
        while (!create_lock.try_lock() &&
               std::chrono::steady_clock::now() < lock_deadline) {
            vTaskDelay(pdMS_TO_TICKS(10));
        }
        if (!create_lock.owns_lock()) {
            ESP_LOGW(TAG, "ML307 foreground cancel deferred while HTTP create is busy");
            return;
        }
        Close();
    }

    void Close() {
        if (!instance_active_.exchange(false)) return;
        int id = http_id_.load();
        http_id_.store(-1);
        if (id >= 0) {
            (void)uart_->SendCommand("AT+MHTTPDEL=" + std::to_string(id));
            ESP_LOGI(TAG, "ML307 HTTP connection closed, ID: %d", id);
        }
    }

private:
    bool ParseUrl(const char *url) {
        std::string value(url ? url : "");
        size_t scheme_end = value.find("://");
        if (scheme_end == std::string::npos) return false;
        std::string scheme = value.substr(0, scheme_end);
        if (scheme != "http" && scheme != "https") return false;
        secure_ = scheme == "https";
        size_t host_start = scheme_end + 3;
        size_t path_start = value.find_first_of("/?", host_start);
        std::string authority = path_start == std::string::npos
                                    ? value.substr(host_start)
                                    : value.substr(host_start, path_start - host_start);
        if (authority.empty() ||
            value.find('#', host_start) != std::string::npos ||
            authority.find_first_of("@\\") != std::string::npos) return false;
        for (size_t i = host_start; i < value.size(); ++i) {
            const unsigned char c = static_cast<unsigned char>(value[i]);
            if (c < 0x20u || c == 0x7fu || c == '"') return false;
        }
        origin_ = scheme + "://" + authority;
        if (path_start == std::string::npos) {
            path_ = "/";
        } else if (value[path_start] == '?') {
            path_ = "/" + value.substr(path_start);
        } else {
            path_ = value.substr(path_start);
        }
        return true;
    }

    bool SendConfig(const char *name, const char *values) {
        if (cancelled_.load()) return false;
        std::string command = "AT+MHTTPCFG=\"" + std::string(name) + "\"," +
                              std::to_string(http_id_.load()) + "," + values;
        return uart_->SendCommand(command);
    }

    void HandleUrc(const std::string& command,
                   const std::vector<AtArgumentValue>& arguments) {
        if (command == "MHTTPCREATE") {
            int id = -1;
            if (arguments.size() != 1u || !argument_int(arguments, 0, &id) ||
                id < 0 || id >= static_cast<int>(kHttpSlotCount)) {
                std::lock_guard<std::mutex> lock(state_mutex_);
                if (awaiting_create_) {
                    error_ = true;
                    awaiting_create_ = false;
                    state_cv_.notify_all();
                }
                return;
            }
            std::lock_guard<std::mutex> lock(state_mutex_);
            if (!awaiting_create_ || instance_active_.load()) return;
            http_id_.store(id);
            instance_active_.store(true);
            state_cv_.notify_all();
            return;
        }
        if (command == "FIFO_OVERFLOW") {
            std::lock_guard<std::mutex> lock(state_mutex_);
            error_ = true;
            state_cv_.notify_all();
            return;
        }
        if (command != "MHTTPURC") return;

        int id = -1;
        const std::string *type = nullptr;
        if (arguments.size() < 2u || arguments.size() > 6u ||
            !argument_string(arguments, 0, &type) ||
            !argument_int(arguments, 1, &id) || id != http_id_.load()) {
            return;
        }

        std::lock_guard<std::mutex> lock(state_mutex_);
        if (*type == "header") {
            const std::string *encoded_headers = nullptr;
            if (arguments.size() != 5u ||
                !argument_int(arguments, 2, &status_code_) ||
                status_code_ < 100 || status_code_ > 599 ||
                arguments[3].type != AtArgumentValue::Type::Int ||
                !argument_string(arguments, 4, &encoded_headers)) {
                error_ = true;
            } else {
                /* 1xx responses are interim (except 101, which switches
                 * protocols and is outside this request/response API). Keep
                 * waiting for the final response instead of exposing an
                 * incomplete header to callers. */
                if (status_code_ >= 100 && status_code_ < 200) {
                    if (status_code_ == 101) {
                        error_ = true;
                    } else {
                        response_chunked_ = false;
                        body_forbidden_ = false;
                    }
                    state_cv_.notify_all();
                    return;
                }
                headers_received_ = true;
                // Responses without a body are not guaranteed to emit a
                // content URC; let the reader complete immediately.
                body_forbidden_ = method_ == "HEAD" || status_code_ == 204 || status_code_ == 304;
                if (body_forbidden_) {
                    eof_ = true;
                } else {
                    std::string decoded;
                    if (!uart_->DecodeHexAppend(decoded, encoded_headers->data(), encoded_headers->size())) {
                        error_ = true;
                    } else {
                        bool chunked = false;
                        bool content_length_zero = false;
                        if (!parse_http_header_framing(decoded, &chunked, &content_length_zero)) {
                            error_ = true;
                        } else {
                            response_chunked_ = chunked;
                            if (content_length_zero) eof_ = true;
                        }
                    }
                }
            }
        } else if (*type == "content") {
            int content_length = 0;
            int received_length = 0;
            int current_length = 0;
            if (!headers_received_ || arguments.size() < 5u || arguments.size() > 6u ||
                !argument_int(arguments, 2, &content_length) ||
                !argument_int(arguments, 3, &received_length) ||
                !argument_int(arguments, 4, &current_length)) {
                error_ = true;
            } else {
                if (body_forbidden_ && (received_length != 0 || current_length != 0)) {
                    error_ = true;
                } else if (content_length < 0 || received_length < 0 || current_length < 0 ||
                    current_length > received_length ||
                    (!response_chunked_ && received_length > content_length) ||
                    static_cast<size_t>(current_length) >
                        std::numeric_limits<size_t>::max() - body_offset_ ||
                    static_cast<size_t>(received_length) != body_offset_ +
                        static_cast<size_t>(current_length)) {
                    error_ = true;
                } else if (current_length > 0 && arguments.size() == 6u) {
                    const std::string *encoded = nullptr;
                    if (argument_string(arguments, 5, &encoded)) {
                        std::string decoded;
                        if (!uart_->DecodeHexAppend(decoded, encoded->data(), encoded->size()) ||
                            decoded.size() != static_cast<size_t>(current_length) ||
                            decoded.size() > std::numeric_limits<size_t>::max() - body_.size() ||
                            decoded.size() != static_cast<size_t>(current_length)) {
                            ESP_LOGE(TAG, "ML307 cellular HTTP body contains malformed hex");
                            body_.clear();
                            error_ = true;
                            eof_ = true;
                            state_cv_.notify_all();
                            return;
                        }
                        body_.append(decoded);
                        body_offset_ = static_cast<size_t>(received_length);
                    } else {
                        error_ = true;
                    }
                } else if (current_length > 0) {
                    error_ = true;
                } else if (arguments.size() != 5u) {
                    // A zero-length terminator carries no payload.  Accepting
                    // an extra sixth argument would make malformed URCs look
                    // like a valid EOF and could desynchronise the next URC.
                    error_ = true;
                } else {
                    /* A zero-length terminator must still carry the exact
                     * cumulative offset; retain the independent offset even
                     * though readers may have drained body_. */
                    body_offset_ = static_cast<size_t>(received_length);
                }
                // Non-chunked firmware reports total/accumulated bytes;
                // chunked firmware terminates with a zero-length content URC.
                if (response_chunked_) {
                    if (current_length == 0) eof_ = true;
                } else if (received_length >= content_length) {
                    eof_ = true;
                }
            }
        } else if (*type == "err") {
            int modem_error = 0;
            if (arguments.size() != 3u || !argument_int(arguments, 2, &modem_error) ||
                modem_error < 0) {
                error_ = true;
                state_cv_.notify_all();
                return;
            }
            ESP_LOGE(TAG, "ML307 HTTP request error: id=%d modem_error=%d", id,
                     modem_error);
            error_ = true;
        }
        state_cv_.notify_all();
    }

    std::shared_ptr<AtUart> uart_;
    int timeout_ms_;
    const void *cancellation_owner_ = nullptr;
    bool foreground_;
    std::list<UrcCallback>::iterator callback_;
    std::mutex state_mutex_;
    std::condition_variable state_cv_;
    std::atomic<int> http_id_{-1};
    std::atomic<bool> instance_active_{false};
    std::atomic<bool> cancelled_{false};
    bool awaiting_create_ = false;
    bool headers_received_ = false;
    bool error_ = false;
    bool eof_ = false;
    bool body_forbidden_ = false;
    bool secure_ = false;
    bool response_chunked_ = false;
    bool slot_acquired_ = false;
    int status_code_ = 0;
    std::string method_;
    std::string origin_;
    std::string path_;
    std::string body_;
    size_t body_offset_ = 0;
};

extern "C" bool ml307_transport_is_ready(void) {
    std::lock_guard<std::mutex> lock(s_lifecycle_mutex);
    return s_modem && s_modem->network_ready();
}

extern "C" esp_err_t ml307_transport_start(int tx_gpio, int rx_gpio,
                                            int baud_rate, int timeout_ms,
                                            const char *apn) {
    if (timeout_ms <= 0) return ESP_ERR_INVALID_ARG;
    if (!s_admission_open.load()) return ESP_ERR_INVALID_STATE;
    bool expected_not_starting = false;
    if (!s_start_in_progress.compare_exchange_strong(expected_not_starting, true)) {
        return ESP_ERR_INVALID_STATE;
    }
    s_start_stop_requested.store(false);
    const auto complete_start = []() { s_start_in_progress.store(false); };
    std::lock_guard<std::mutex> lock(s_lifecycle_mutex);
    if (!s_admission_open.load() || s_start_stop_requested.load()) {
        complete_start();
        return ESP_ERR_INVALID_STATE;
    }
    if (s_modem && s_modem->network_ready()) {
        complete_start();
        return ESP_OK;
    }

    if (!s_modem) {
        ESP_LOGI(TAG, "detecting ML307 on GPIO%d/GPIO%d at %d baud",
                 tx_gpio, rx_gpio, baud_rate);
        s_modem = AtModem::Detect((gpio_num_t)tx_gpio, (gpio_num_t)rx_gpio,
                                  GPIO_NUM_NC, baud_rate, timeout_ms,
                                  [] { return s_start_stop_requested.load() ||
                                               !s_admission_open.load(); });
        if (!s_modem) {
            ESP_LOGE(TAG, "ML307 detection failed");
            complete_start();
            return ESP_ERR_NOT_FOUND;
        }
    }

    // Most SIMs use the module's provisioned PDP profile. When a deployment
    // supplies an APN explicitly, apply it before registration/IP activation.
    // Restrict accepted characters so the Kconfig value cannot escape the AT
    // command's quoted parameter.
    if (apn && apn[0]) {
        size_t apn_len = strlen(apn);
        bool safe = apn_len <= 63;
        for (size_t i = 0; safe && i < apn_len; ++i) {
            unsigned char c = static_cast<unsigned char>(apn[i]);
            safe = std::isalnum(c) || c == '.' || c == '-' || c == '_';
        }
        if (!safe) {
            ESP_LOGE(TAG, "invalid ML307 APN");
            complete_start();
            return ESP_ERR_INVALID_ARG;
        }
        auto uart = s_modem->GetAtUart();
        std::string command = "AT+CGDCONT=1,\"IP\",\"" + std::string(apn) + "\"";
        if (!uart->SendCommand(command)) {
            ESP_LOGE(TAG, "cannot configure ML307 APN");
            complete_start();
            return ESP_FAIL;
        }
        ESP_LOGI(TAG, "ML307 custom APN configured");
    }

    NetworkStatus status = s_modem->WaitForNetworkReady(
        timeout_ms, [] { return s_start_stop_requested.load() ||
                                     !s_admission_open.load(); });
    if (status != NetworkStatus::Ready || !s_modem->network_ready()) {
        ESP_LOGE(TAG, "ML307 network registration failed: %d", (int)status);
        complete_start();
        return status == NetworkStatus::ErrorTimeout ? ESP_ERR_TIMEOUT : ESP_FAIL;
    }
    ESP_LOGI(TAG, "ML307 network ready: revision=%s carrier=%s signal=%d",
             s_modem->GetModuleRevision().c_str(),
             s_modem->GetCarrierName().c_str(), s_modem->GetCsq());
    ensure_network_probe_task();
    auto uart = s_modem->GetAtUart();
    if (!uart->SendCommand("AT+MSSLCFG=\"auth\",0,0")) {
        ESP_LOGW(TAG, "cannot select ML307 TLS auth mode");
    }
    complete_start();
    return ESP_OK;
}

extern "C" esp_err_t ml307_transport_quiesce(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(s_system_sleep_mutex);
    if (s_system_sleep_preparing) return ESP_ERR_INVALID_STATE;
    s_network_probe_restart_after_stop.store(false);
    esp_err_t err = close_transport_and_drain(timeout_ms);
    if (err != ESP_OK) return err;
    /* Do not destroy s_modem or its UART here. A successful drain proves all
     * admitted requests completed MHTTPDEL and unregistered their callbacks;
     * the adapter may safely regard the transport as quiescent, but this slice
     * deliberately does not claim a full modem deinit/restart contract. */
    ESP_LOGI(TAG, "ML307 transport admission closed; probe and HTTP borrowers quiesced");
    return ESP_OK;
}

extern "C" esp_err_t ml307_transport_deinit(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(s_system_sleep_mutex);
    if (s_system_sleep_preparing) return ESP_ERR_INVALID_STATE;
    s_network_probe_restart_after_stop.store(false);
    esp_err_t err = close_transport_and_drain(timeout_ms);
    if (err != ESP_OK) {
        /* Admission remains closed after a partial drain.  The caller must
         * retry or enter a terminal fault state; destroying the modem here
         * would race a borrower that has not completed MHTTPDEL/callback
         * retirement. */
        return err;
    }
    {
        std::lock_guard<std::mutex> lifecycle_lock(s_lifecycle_mutex);
        /* AtModem::Shutdown unregisters its URC callback before stopping the
         * shared AtUart receive/event tasks.  Resetting the unique_ptr only
         * after borrower drain makes UART/DMA destruction deterministic. */
        if (s_modem && !s_modem->Shutdown(timeout_ms)) {
            ESP_LOGW(TAG, "ML307 modem/UART shutdown did not meet deadline");
            return ESP_ERR_TIMEOUT;
        }
        s_modem.reset();
    }
    s_start_stop_requested.store(false);
    ESP_LOGI(TAG, "ML307 transport generation deinitialized");
    return ESP_OK;
}

extern "C" esp_err_t ml307_transport_reinitialize(int tx_gpio, int rx_gpio,
                                                    int baud_rate, int timeout_ms,
                                                    const char *apn) {
    if (timeout_ms <= 0 || tx_gpio < 0 || rx_gpio < 0 || baud_rate <= 0) {
        return ESP_ERR_INVALID_ARG;
    }
    {
        std::lock_guard<std::mutex> lifecycle_lock(s_lifecycle_mutex);
        if (s_modem || s_start_in_progress.load()) return ESP_ERR_INVALID_STATE;
    }
    /* A successful terminal deinit leaves admission closed. Reopen it only
     * for this explicit fresh-generation start; no stale worker can reuse the
     * old modem pointer because s_modem was reset after borrower drain. */
    s_admission_open.store(true);
    return ml307_transport_start(tx_gpio, rx_gpio, baud_rate, timeout_ms, apn);
}

extern "C" esp_err_t ml307_transport_prepare_system_sleep(uint32_t timeout_ms) {
    if (timeout_ms == 0) return ESP_ERR_INVALID_ARG;
    std::lock_guard<std::mutex> lock(s_system_sleep_mutex);
    if (s_system_sleep_preparing) return ESP_ERR_INVALID_STATE;
    s_system_sleep_preparing = true;
    s_system_sleep_was_admitted = s_admission_open.load();
    s_system_sleep_probe_was_running = network_probe_task_is_running();
    s_network_probe_restart_after_stop.store(false);
    esp_err_t err = close_transport_and_drain(timeout_ms);
    if (err != ESP_OK) {
        /* Keep the closed boundary until the parent transaction invokes ABORT;
         * a timeout must never reopen transport while a late borrower/probe
         * can still be leaving its critical section. */
        ESP_LOGW(TAG, "ML307 System Sleep PREPARE did not reach a safe point: %s",
                 esp_err_to_name(err));
        return err;
    }
    ESP_LOGI(TAG, "ML307 System Sleep PREPARE parked probe and HTTP borrowers");
    return ESP_OK;
}

extern "C" void ml307_transport_abort_system_sleep_prepare(void) {
    std::lock_guard<std::mutex> lock(s_system_sleep_mutex);
    if (!s_system_sleep_preparing) return;
    const bool reopen = s_system_sleep_was_admitted;
    const bool restart_probe = s_system_sleep_probe_was_running;
    s_system_sleep_was_admitted = false;
    s_system_sleep_probe_was_running = false;
    s_system_sleep_preparing = false;
    if (!reopen) return;

    s_start_stop_requested.store(false);
    s_admission_open.store(true);
    if (restart_probe) {
        /* If the old task is still releasing its completion token, defer
         * recreation to its exit path.  Otherwise create the original probe
         * generation now. */
        if (network_probe_task_is_running()) {
            s_network_probe_restart_after_stop.store(true);
        } else {
            ensure_network_probe_task();
        }
    }
    ESP_LOGI(TAG, "ML307 System Sleep ABORT restored transport admission");
}

extern "C" bool ml307_transport_cancel_foreground(void) {
    /* Foreground cancellation has the same non-blocking lifecycle semantics
     * as owner cancellation.  The request releases its modem HTTP ID while
     * its synchronous caller unwinds, rather than letting an input/cancel
     * worker wait on a possibly wedged UART/create transaction. */
    std::lock_guard<std::mutex> lock(s_foreground_mutex);
    if (!s_foreground_request) return false;
    s_foreground_request->RequestCancel();
    return true;
}

extern "C" bool ml307_transport_cancel_requests_for_owner(const void *owner) {
    if (!owner) return false;
    /* Request destructors take the same mutex before their object storage can
     * disappear.  Cancel only its atomic token and condition variables while
     * protected; do not issue MHTTPDEL here, because that can wait behind the
     * create critical section and would deadlock a returning request trying to
     * unregister itself. The synchronous owner performs Close() as it unwinds. */
    std::lock_guard<std::mutex> lock(s_owner_requests_mutex);
    bool cancelled = false;
    for (Ml307Request *request : s_owner_requests) {
        if (request && request->cancellation_owner() == owner) {
            request->RequestCancel();
            cancelled = true;
        }
    }
    return cancelled;
}

extern "C" esp_err_t ml307_transport_http_request(
    const char *method, const char *url, const char *content_type,
    const char *authorization, const char *extra_header_name,
    const char *extra_header_value, const void *body, size_t body_len,
    char *response, size_t response_capacity, size_t *response_len,
    int *status_code, bool *truncated, int timeout_ms,
    const void *cancellation_owner, bool foreground) {
    if (!method || !url || !response || response_capacity < 2 ||
        !response_len || !status_code || !truncated) {
        return ESP_ERR_INVALID_ARG;
    }
    *response_len = 0;
    *status_code = 0;
    *truncated = false;
    response[0] = '\0';

    Ml307HttpBorrower borrower;
    if (!borrower.Acquire()) return ESP_ERR_INVALID_STATE;

    Ml307Request request(borrower.uart(), timeout_ms, cancellation_owner, foreground);
    if (!request.Open(method, url, content_type, authorization,
                      extra_header_name, extra_header_value, body, body_len)) {
        ESP_LOGE(TAG, "ML307 HTTP open failed: %s %s", method, url);
        return ESP_FAIL;
    }
    if (!request.WaitForHeaders(status_code)) {
        ESP_LOGE(TAG, "ML307 HTTP response header timeout/error: %s %s",
                 method, url);
        return ESP_ERR_TIMEOUT;
    }

    while (true) {
        char chunk[1024];
        int count = request.Read(chunk, sizeof(chunk));
        if (count < 0) {
            ESP_LOGE(TAG, "ML307 HTTP response body timeout/error: %s %s",
                     method, url);
            return ESP_ERR_TIMEOUT;
        }
        if (count == 0) break;
        size_t available = response_capacity - *response_len - 1;
        size_t copy = std::min(available, static_cast<size_t>(count));
        if (copy) {
            memcpy(response + *response_len, chunk, copy);
            *response_len += copy;
            response[*response_len] = '\0';
        }
        if (copy != static_cast<size_t>(count)) *truncated = true;
    }
    return *truncated ? ESP_ERR_INVALID_SIZE : ESP_OK;
}

extern "C" esp_err_t ml307_transport_http_request_stream(
    const char *method, const char *url, const char *content_type,
    const char *authorization, const char *extra_header_name,
    const char *extra_header_value, size_t body_len,
    ml307_transport_body_reader_t body_reader, void *body_reader_context,
    void *stream_buffer, size_t stream_buffer_size,
    char *response, size_t response_capacity, size_t *response_len,
    int *status_code, bool *truncated, int timeout_ms,
    const void *cancellation_owner, bool foreground) {
    if (!method || !url || !body_reader || !stream_buffer ||
        stream_buffer_size < kHttpContentChunkSize || !response ||
        response_capacity < 2 || !response_len || !status_code || !truncated) {
        return ESP_ERR_INVALID_ARG;
    }
    *response_len = 0;
    *status_code = 0;
    *truncated = false;
    response[0] = '\0';

    Ml307HttpBorrower borrower;
    if (!borrower.Acquire()) return ESP_ERR_INVALID_STATE;

    Ml307Request request(borrower.uart(), timeout_ms, cancellation_owner, foreground);
    if (!request.Open(method, url, content_type, authorization,
                      extra_header_name, extra_header_value, nullptr, body_len,
                      body_reader, body_reader_context,
                      stream_buffer, stream_buffer_size)) {
        ESP_LOGE(TAG, "ML307 HTTP stream open failed: %s %s", method, url);
        return ESP_FAIL;
    }
    if (!request.WaitForHeaders(status_code)) {
        ESP_LOGE(TAG, "ML307 HTTP stream response header timeout/error: %s %s",
                 method, url);
        return ESP_ERR_TIMEOUT;
    }

    while (true) {
        char chunk[1024];
        int count = request.Read(chunk, sizeof(chunk));
        if (count < 0) {
            ESP_LOGE(TAG, "ML307 HTTP stream response body timeout/error: %s %s",
                     method, url);
            return ESP_ERR_TIMEOUT;
        }
        if (count == 0) break;
        size_t available = response_capacity - *response_len - 1;
        size_t copy = std::min(available, static_cast<size_t>(count));
        if (copy) {
            memcpy(response + *response_len, chunk, copy);
            *response_len += copy;
            response[*response_len] = '\0';
        }
        if (copy != static_cast<size_t>(count)) *truncated = true;
    }
    return *truncated ? ESP_ERR_INVALID_SIZE : ESP_OK;
}
