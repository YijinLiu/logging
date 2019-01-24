#ifndef LOGGING_LOGGING_H_
#define LOGGING_LOGGING_H_

#include <stdarg.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

extern int verbose_log_level;

void vlogf_v(const char* file, int line, int level, const char* format, va_list ap);
void vlogf(const char* file, int line, int level, const char* format, ...);

// gcc has its own fatalf. Use our_fatalf to avoid symbol conflict.
void our_fatalf_v(const char* file, int line, const char* format, va_list ap);
void our_fatalf(const char* file, int line, const char* format, ...);

int safe_snprintf_v(char* buf, int buf_size, const char* format, va_list ap);
int safe_snprintf(char* buf, int buf_size, const char* format, ...);

#define VLOGF(...) vlogf(__FILE__, __LINE__, __VA_ARGS__)
#define FATALF(...) our_fatalf(__FILE__, __LINE__, __VA_ARGS__)
#define VLOGF_EVERY_N(N, ...) \
    { static int cnt = 0; if (cnt++ % N == 0) vlogf(__FILE__, __LINE__, __VA_ARGS__); }
#define SNPRINTF(buf, format, args...) safe_snprintf(buf, sizeof(buf), format, args)
#define LOG_HEX_DATA(level, data, len) log_hex_data(level, data, len)

#ifdef __cplusplus
}  // extern "C"
#endif

#endif  // LOGGING_LOGGING_H_
