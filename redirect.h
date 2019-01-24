#ifndef LOGGING_REDIRECT_H_
#define LOGGING_REDIRECT_H_

extern int old_stdout;
extern int old_stderr;

// Takes ownership of "file_name".
// Returns 0 for success.
int redirect_stdout_stderr_to(char* file_name);

// Writes log to stdout.
// Returns number of bytes written.
int write_to_log(const char* msg, int msg_len);

#endif  // LOGGING_REDIRECT_H_
