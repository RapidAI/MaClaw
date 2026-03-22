#include "utils/rs_log.h"
#include <cstdio>
#include <cstdarg>
#include <ctime>

// Default log level
static RSLogLevel g_current_log_level = RSLogLevel::RS_LOG_LEVEL_INFO;

/**
 * Maps log level enum to string representation.
 */
static const char* rs_log_level_to_str(RSLogLevel level) {
  switch (level) {
  case RSLogLevel::RS_LOG_LEVEL_DEBUG: return "DEBUG";
  case RSLogLevel::RS_LOG_LEVEL_INFO:  return "INFO ";
  case RSLogLevel::RS_LOG_LEVEL_WARN:  return "WARN ";
  case RSLogLevel::RS_LOG_LEVEL_ERR:   return "ERROR";
  default: return "UNKNOWN";
  }
}

void rs_log_set_level(RSLogLevel level) {
  g_current_log_level = level;
}

void rs_log(RSLogLevel level, const char* format, ...) {
  // Check if the current level allows this log
  if (level < g_current_log_level) {
    return;
  }

  // Get current time for timestamp
  time_t now = time(nullptr);
  struct tm* tm_info = localtime(&now);
  char time_buf[26];
  strftime(time_buf, 26, "%Y-%m-%d %H:%M:%S", tm_info);

  // Print metadata
  printf("[%s] [%s] ", time_buf, rs_log_level_to_str(level));

  // Print formatted message
  va_list args;
  va_start(args, format);
  vprintf(format, args);
  va_end(args);

  printf("\n");
  fflush(stdout);
}