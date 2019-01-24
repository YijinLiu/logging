#include "logging.h"

#include <stdio.h>
#include <stdlib.h>

int verbose_log_level = 1;

void cLogText(const char* file, int line, int level, const char* text);
void Close();

static inline void logf_v(const char* file, int line, int level, const char* format, va_list ap) {
    char buf[4000];
    int len = vsnprintf(buf, sizeof(buf), format, ap);
    // Go log add a new line for every log, remove it if the C part already provides it.
    while (buf[len - 1] == '\n') buf[--len] = '\0';
    cLogText(file, line, level, buf);
}

void vlogf_v(const char* file, int line, int level, const char* format, va_list ap) {
    if (level > verbose_log_level) return;
    logf_v(file, line, level, format, ap);
}

void vlogf(const char* file, int line, int level, const char* format, ...) {
    va_list ap;
    va_start(ap, format);
    vlogf_v(file, line, level, format, ap);
    va_end(ap);
}

void our_fatalf_v(const char* file, int line, const char* format, va_list ap) {
    logf_v(file, line, -1, format, ap);
    Close();
    abort();
}

void our_fatalf(const char* file, int line, const char* format, ...) {
    va_list ap;
    va_start(ap, format);
    our_fatalf_v(file, line, format, ap);
    va_end(ap);
}

int safe_snprintf_v(char* buf, int buf_size, const char* format, va_list ap) {
    const int written = vsnprintf(buf, buf_size, format, ap);
    if (written >= buf_size) {
        our_fatalf(__FILE__, __LINE__, "Buffer is too small! At least %d is required.", written + 1);
    }
    return written;
}

int safe_snprintf(char* buf, int buf_size, const char* format, ...) {
    va_list ap;
    va_start(ap, format);
    const int written = safe_snprintf_v(buf, buf_size, format, ap);
    va_end(ap);
    return written;
}
