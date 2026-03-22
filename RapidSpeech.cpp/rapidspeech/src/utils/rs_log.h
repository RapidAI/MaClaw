#pragma once

#include <vector>

/**
 * Log levels for the RapidSpeech framework.
 */
enum class RSLogLevel {
  RS_LOG_LEVEL_DEBUG = 0,
  RS_LOG_LEVEL_INFO  = 1,
  RS_LOG_LEVEL_WARN  = 2,
  RS_LOG_LEVEL_ERR   = 3
};

/**
 * Set the global logging level.
 */
void rs_log_set_level(RSLogLevel level);

/**
 * Core logging function. Supports printf-style formatting.
 */
void rs_log(RSLogLevel level, const char* format, ...);

/**
 * Convenience macros for internal usage.
 */
#define RS_LOG_DEBUG(...) rs_log(RSLogLevel::RS_LOG_LEVEL_DEBUG, __VA_ARGS__)
#define RS_LOG_INFO(...)  rs_log(RSLogLevel::RS_LOG_LEVEL_INFO,  __VA_ARGS__)
#define RS_LOG_WARN(...)  rs_log(RSLogLevel::RS_LOG_LEVEL_WARN,  __VA_ARGS__)
#define RS_LOG_ERR(...)   rs_log(RSLogLevel::RS_LOG_LEVEL_ERR,   __VA_ARGS__)