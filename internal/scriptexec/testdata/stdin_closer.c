/* Test-only launcher fixture. It passes the launcher's attestation exactly like
 * the real worker (fds {0,1,2}, CPU/AS limits, NoNewPrivs, its own seccomp
 * filter from the shipped seal.h) but first replaces fd 0 with an eventfd, so
 * the launcher's stdin pipe has NO reader left: every source write fails with
 * EPIPE, deterministically. It then blocks forever on its own eventfd. */
#include "seal.h"
#include <sys/eventfd.h>

int main(void) {
    struct rlimit cpu = {1, 1};
    int e = eventfd(0, 0);
    if (e < 0 || dup2(e, 0) < 0) return 125; /* closes the launcher pipe's read end */
    if (e != 0) close(e);
    if (syscall(SYS_close_range, 3U, ~0U, 0) || setrlimit(RLIMIT_CPU, &cpu) || seal()) return 125;
    unsigned long long value;
    (void)read(0, &value, sizeof(value)); /* never written: blocks until killed */
    return 0;
}
