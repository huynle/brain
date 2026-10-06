/* Shared deny-default seal for the sealed QuickJS worker and its confinement
 * probe. Install AFTER engine initialization and descriptor closure, BEFORE any
 * untrusted input. Irreversible: setrlimit/prctl/seccomp are not allowlisted.
 * Not a reviewed production policy until independent acceptance. */
#ifndef BRAIN_SCRIPT_WORKER_SEAL_H
#define BRAIN_SCRIPT_WORKER_SEAL_H
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/uio.h>
#include <sys/wait.h>
#include <unistd.h>
#include <stddef.h>
#include <linux/audit.h>
#include <linux/filter.h>
#include <linux/seccomp.h>
#include <sys/prctl.h>
#include <sys/syscall.h>
#include <sys/resource.h>

#if defined(__aarch64__)
#define PROBE_ARCH AUDIT_ARCH_AARCH64
#elif defined(__x86_64__)
#define PROBE_ARCH AUDIT_ARCH_X86_64
#else
#error Unsupported probe architecture
#endif
#define LOAD_ARG(n) BPF_STMT(BPF_LD|BPF_W|BPF_ABS,offsetof(struct seccomp_data,args[n]))
#define ALLOW BPF_STMT(BPF_RET|BPF_K,SECCOMP_RET_ALLOW)
#define DENY BPF_STMT(BPF_RET|BPF_K,SECCOMP_RET_ERRNO|EPERM)
#define ALLOW_NR(n) BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,SYS_##n,0,1), ALLOW
static int seal(void) {
    struct rlimit address_space={64*1024*1024,64*1024*1024};
    if(setrlimit(RLIMIT_AS,&address_space))return -1;
    struct sock_filter filter[]={
        BPF_STMT(BPF_LD|BPF_W|BPF_ABS,offsetof(struct seccomp_data,arch)),
        BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,PROBE_ARCH,1,0),
        BPF_STMT(BPF_RET|BPF_K,SECCOMP_RET_KILL_PROCESS),
        BPF_STMT(BPF_LD|BPF_W|BPF_ABS,offsetof(struct seccomp_data,nr)),
        ALLOW_NR(exit), ALLOW_NR(exit_group), ALLOW_NR(rt_sigreturn),
        ALLOW_NR(brk), ALLOW_NR(munmap), ALLOW_NR(close), ALLOW_NR(clock_gettime),
        /* Only parent IPC stdin may be read; only stdout/stderr written. */
        BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,SYS_read,0,4),
        LOAD_ARG(0), BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,0,0,1), ALLOW, DENY,
        BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,SYS_write,0,5),
        LOAD_ARG(0), BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,1,1,0),
        BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,2,0,1), ALLOW, DENY,
        /* mmap must be private anonymous non-executable memory, never an FD. */
        BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,SYS_mmap,0,9),
        LOAD_ARG(2), BPF_JUMP(BPF_JMP|BPF_JSET|BPF_K,~(PROT_READ|PROT_WRITE),6,0),
        LOAD_ARG(3), BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,MAP_PRIVATE|MAP_ANONYMOUS,0,4),
        LOAD_ARG(4), BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,0xffffffffU,0,2),
        ALLOW, DENY, DENY,
        DENY
    };
    struct sock_fprog program={sizeof(filter)/sizeof(filter[0]),filter};
    if(prctl(PR_SET_NO_NEW_PRIVS,1,0,0,0))return -1;
    /* A positive failing-thread ID is also failure, never successful sealing. */
    return syscall(SYS_seccomp,SECCOMP_SET_MODE_FILTER,SECCOMP_FILTER_FLAG_TSYNC,&program)!=0;
}
#endif
