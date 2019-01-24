#include "redirect.h"

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

int old_stdout = -1;
int old_stderr = -1;

void announce_error(int fd1, int fd2, char *prefix) {
    char buf[512], msg[1024];
    strerror_r(errno, buf, sizeof(buf));
    snprintf(msg, sizeof(msg), "%s: %s\n", prefix, buf);
    int fd = (fd1 >= 0 ? fd1 : fd2) ;
    int ignored_rc = write(fd, msg, strlen(msg));
}

int redirect_stdout_stderr_to(char* file_name) {
    if (old_stdout < 0) old_stdout = dup(fileno(stdout));
    if (old_stderr < 0) old_stderr = dup(fileno(stderr));
    int rc = 0;
    if (freopen(file_name, "w", stdout) == NULL) {
        char msg[1024];
        snprintf(msg, sizeof(msg), "Failed to open '%s'", file_name);
        announce_error(old_stderr, fileno(stderr), msg);
        rc = -errno;
    } else if (dup2(fileno(stdout), fileno(stderr)) == -1) {
        announce_error(old_stderr, fileno(stderr), "Failed to dup stderr to stdout");
        rc = -errno;
    }
    free(file_name);
    return rc;
}

int write_to_log(const char* msg, int msg_len) {
    int n = fwrite(msg, 1, msg_len, stdout);
    if (n < msg_len) {
        return n;
    }
    if (fflush(stdout)) {
        announce_error(old_stderr, fileno(stderr), "Error flushing log");
        return -errno;
    }
    return n;
}
