/* Negative experiment: RLIMIT_AS alone is NOT a macOS resident-memory bound.
 * Bounded to 128MiB requested small allocations; never linked into Brain. */
#include <mach/mach.h>
#include <sys/resource.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static struct mach_task_basic_info measure(void) {
    struct mach_task_basic_info info = {0};
    mach_msg_type_number_t count = MACH_TASK_BASIC_INFO_COUNT;
    if (task_info(mach_task_self(), MACH_TASK_BASIC_INFO, (task_info_t)&info, &count) != KERN_SUCCESS) exit(2);
    return info;
}
int main(void) {
    size_t capacity = 1024*1024;
    void **p = calloc(capacity, sizeof(void*));
    if (!p) return 3;
    struct mach_task_basic_info before = measure();
    struct rlimit cpu = {5,5};
    struct rlimit as = {before.virtual_size+64*1024*1024, before.virtual_size+64*1024*1024};
    if (setrlimit(RLIMIT_CPU, &cpu) || setrlimit(RLIMIT_AS, &as)) return 4;
    size_t n;
    for (n=0;n<capacity;n++) {
        p[n] = malloc(128);
        if (!p[n]) break;
        memset(p[n], 0xa5, 128);
    }
    struct mach_task_basic_info after = measure();
    printf("{\"requested\":%zu,\"vm_before\":%llu,\"vm_after\":%llu,\"rss_before\":%llu,\"rss_after\":%llu,\"rss_delta\":%llu}\n",n*128,before.virtual_size,after.virtual_size,before.resident_size,after.resident_size,after.resident_size-before.resident_size);
    for (size_t i=0;i<n;i++) free(p[i]);
    free(p);
    return 0;
}
