/* Inactive native test harness, not an installed worker. No QuickJS libc module. */
#include "quickjs.h"
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

/* Deliberately local to this experiment, NOT a reviewed production policy. */
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

int main(void) {
    setbuf(stdout, NULL);
    int fd = open("/etc/passwd", O_RDONLY);
    if (fd < 0) return 120;
    JSRuntime *rt = JS_NewRuntime();
    if (!rt) return 121;
    JS_SetMemoryLimit(rt, 16 * 1024 * 1024);
    JS_SetMaxStackSize(rt, 512 * 1024);
    JSContext *ctx = JS_NewContext(rt);
    if (!ctx || seal()) return 125;
    errno=0; int opened=open("/etc/passwd",O_RDONLY); int eopen=errno; if(opened>=0)close(opened);
    errno=0; int written=open("/tmp/native-write",O_CREAT|O_WRONLY,0600); int ewrite=errno; if(written>=0)close(written);
    errno=0; int sock=socket(AF_INET,SOCK_STREAM,0); int esocket=errno; if(sock>=0)close(sock);
    errno=0; pid_t pid=fork(); int efork=errno; if(pid==0)_exit(0); if(pid>0)waitpid(pid,NULL,0);
    char buf[8]; errno=0; (void)read(fd,buf,sizeof(buf)); int eread=errno;
    errno=0; (void)pread(fd,buf,sizeof(buf),0); int epread=errno;
    struct iovec v={buf,sizeof(buf)}; errno=0; (void)readv(fd,&v,1); int ereadv=errno;
    errno=0; int dupped=dup(fd); int edup=errno; if(dupped>=0)close(dupped);
    errno=0; void *mapped=mmap(NULL,4096,PROT_READ,MAP_PRIVATE,fd,0); int emmap=errno; if(mapped!=MAP_FAILED)munmap(mapped,4096);
    errno=0; mapped=mmap(NULL,4096,PROT_READ|PROT_EXEC,MAP_PRIVATE|MAP_ANONYMOUS,-1,0); int eexec=errno; if(mapped!=MAP_FAILED)munmap(mapped,4096);
    const char *src="(async () => { return await Promise.resolve(42); })()";
    JSValue promise=JS_Eval(ctx,src,strlen(src),"probe",JS_EVAL_TYPE_GLOBAL);
    if(JS_IsException(promise))return 122;
    JSContext *jobctx; int job;
    while((job=JS_ExecutePendingJob(rt,&jobctx))>0){}
    if(job<0 || JS_PromiseState(ctx,promise)!=JS_PROMISE_FULFILLED)return 123;
    JSValue result=JS_PromiseResult(ctx,promise); int value=0;
    if(JS_ToInt32(ctx,&value,result))return 124;
    printf("{\"async\":%d,\"open\":%d,\"write\":%d,\"socket\":%d,\"fork\":%d,\"read_fd\":%d,\"pread_fd\":%d,\"readv_fd\":%d,\"dup_fd\":%d,\"mmap_fd\":%d,\"mmap_exec\":%d}\n",value,eopen,ewrite,esocket,efork,eread,epread,ereadv,edup,emmap,eexec);
    JS_FreeValue(ctx,result); JS_FreeValue(ctx,promise);
    JS_FreeContext(ctx); JS_FreeRuntime(rt);
    return 0;
}
