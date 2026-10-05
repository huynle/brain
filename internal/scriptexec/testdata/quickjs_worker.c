/* Inactive experimental worker; no production linkage or authorization.
 * Build only in the opt-in fixture. The parent is a fixture, NOT Brain services.
 * Reuse the observed Linux boundary without claiming it is reviewed policy. */
#define main native_probe_main
#include "probe.c"
#undef main
#include <signal.h>

#define FRAME_LIMIT 65536
#define SOURCE_LIMIT 32768
#define OP_LIMIT 100
static unsigned sequence = 1;

static int exact_read(void *buf, size_t size) {
    char *p = buf;
    while (size) {
        ssize_t n = read(0, p, size);
        if (n <= 0) return -1;
        p += n; size -= n;
    }
    return 0;
}
static int exact_write(const void *buf, size_t size) {
    const char *p = buf;
    while (size) {
        ssize_t n = write(1, p, size);
        if (n <= 0) return -1;
        p += n; size -= n;
    }
    return 0;
}
static JSValue receive(JSContext *ctx, const char *kind, unsigned expected) {
    unsigned char h[4];
    if (exact_read(h, 4)) return JS_EXCEPTION;
    unsigned size = ((unsigned)h[0]<<24)|((unsigned)h[1]<<16)|((unsigned)h[2]<<8)|h[3];
    if (!size || size > FRAME_LIMIT) return JS_EXCEPTION;
    char *buf = malloc(size+1);
    if (!buf) return JS_EXCEPTION;
    if (exact_read(buf, size)) { free(buf); return JS_EXCEPTION; }
    buf[size] = 0;
    JSValue frame = JS_ParseJSON(ctx, buf, size, "parent-frame");
    free(buf);
    JSValue ver = JS_GetPropertyStr(ctx, frame, "version");
    JSValue seq = JS_GetPropertyStr(ctx, frame, "sequence");
    JSValue k = JS_GetPropertyStr(ctx, frame, "kind");
    double version = 0, index = 0;
    const char *name = JS_ToCString(ctx, k);
    int invalid = JS_IsException(frame) || !JS_IsNumber(ver) || !JS_IsNumber(seq) ||
        JS_ToFloat64(ctx, &version, ver) || JS_ToFloat64(ctx, &index, seq) ||
        version != 1 || index != expected || !name || strcmp(name, kind);
    JS_FreeCString(ctx, name); JS_FreeValue(ctx, ver); JS_FreeValue(ctx, seq); JS_FreeValue(ctx, k);
    JSValue payload = invalid ? JS_EXCEPTION : JS_GetPropertyStr(ctx, frame, "payload");
    JS_FreeValue(ctx, frame);
    return payload;
}
static int send_value(JSContext *ctx, const char *kind, unsigned index, JSValueConst payload) {
    JSValue frame = JS_NewObject(ctx);
    JS_SetPropertyStr(ctx, frame, "version", JS_NewInt32(ctx, 1));
    JS_SetPropertyStr(ctx, frame, "kind", JS_NewString(ctx, kind));
    JS_SetPropertyStr(ctx, frame, "sequence", JS_NewInt32(ctx, index));
    JS_SetPropertyStr(ctx, frame, "payload", JS_DupValue(ctx, payload));
    JSValue json = JS_JSONStringify(ctx, frame, JS_UNDEFINED, JS_UNDEFINED);
    JS_FreeValue(ctx, frame);
    size_t size = 0;
    const char *text = JS_IsException(json) ? NULL : JS_ToCStringLen(ctx, &size, json);
    int rc = -1;
    if (text && size && size <= FRAME_LIMIT) {
        unsigned char h[4] = {size>>24, size>>16, size>>8, size};
        rc = exact_write(h, 4) || exact_write(text, size);
    }
    JS_FreeCString(ctx, text); JS_FreeValue(ctx, json);
    return rc;
}
static JSValue entry_get(JSContext *ctx, JSValueConst self, int argc, JSValueConst *argv) {
    (void)self;
    if (sequence > OP_LIMIT || argc != 1 || !JS_IsString(argv[0])) _exit(130);
    JSValue call = JS_NewObject(ctx), args = JS_NewObject(ctx);
    JS_SetPropertyStr(ctx, args, "id", JS_DupValue(ctx, argv[0]));
    JS_SetPropertyStr(ctx, call, "operation", JS_NewString(ctx, "entries.get"));
    JS_SetPropertyStr(ctx, call, "arguments", args);
    int rc = send_value(ctx, "call", sequence, call);
    JS_FreeValue(ctx, call);
    if (rc) _exit(131);
    JSValue response = receive(ctx, "result", sequence++);
    if (JS_IsException(response) || JS_IsUndefined(response)) _exit(132);
    return response;
}
static int worker_main(void) {
    setbuf(stdout, NULL);
    JSRuntime *rt = JS_NewRuntime();
    if (!rt) return 121;
    JS_SetMemoryLimit(rt, 16*1024*1024);
    JS_SetMaxStackSize(rt, 512*1024);
    JSContext *ctx = JS_NewContext(rt);
    /* Before reading/compiling submitted source: close all inherited non-IPC
     * descriptors and establish irreversible hard CPU plus address-space limits. */
    struct rlimit cpu = {1, 1};
    if (!ctx || syscall(SYS_close_range, 3U, ~0U, 0) || setrlimit(RLIMIT_CPU, &cpu) || seal()) return 125;
    JSValue source = receive(ctx, "call", 1);
    size_t size = 0;
    const char *text = JS_IsString(source) ? JS_ToCStringLen(ctx, &size, source) : NULL;
    if (!text || !size || size > SOURCE_LIMIT) return 133;
    JSValue global = JS_GetGlobalObject(ctx), brain = JS_NewObject(ctx), entries = JS_NewObject(ctx);
    JS_SetPropertyStr(ctx, entries, "get", JS_NewCFunction(ctx, entry_get, "get", 1));
    JS_SetPropertyStr(ctx, brain, "entries", entries);
    JS_SetPropertyStr(ctx, global, "brain", brain);
    JS_FreeValue(ctx, global);
    const char *prefix = "(async()=>{\n", *suffix = "\n})()";
    size_t length = strlen(prefix)+size+strlen(suffix);
    char *program = malloc(length+1);
    if (!program) return 134;
    memcpy(program, prefix, strlen(prefix));
    memcpy(program+strlen(prefix), text, size);
    memcpy(program+strlen(prefix)+size, suffix, strlen(suffix)+1);
    JS_FreeCString(ctx, text); JS_FreeValue(ctx, source);
    JSValue promise = JS_Eval(ctx, program, length, "submitted", JS_EVAL_TYPE_GLOBAL);
    free(program);
    if (JS_IsException(promise)) return 135;
    JSContext *jobctx; int job;
    while ((job=JS_ExecutePendingJob(rt, &jobctx))>0) {}
    if (job<0 || JS_PromiseState(ctx, promise)!=JS_PROMISE_FULFILLED) return 136;
    JSValue result = JS_PromiseResult(ctx, promise);
    if (JS_IsUndefined(result) || send_value(ctx, "result", sequence, result)) return 137;
    JS_FreeValue(ctx, result); JS_FreeValue(ctx, promise);
    JS_FreeContext(ctx); JS_FreeRuntime(rt);
    return 0;
}

/* Trusted, test-only supervisor. It never reads an operation or contains any
 * service authority. Its hard wall timer covers blocked reads as well as JS.
 * Production coordinator/graph shutdown and cross-platform launch remain absent. */
static volatile sig_atomic_t supervised_pid = 0;
static volatile sig_atomic_t wall_expired = 0;
static void wall_alarm(int signum) {
    (void)signum;
    wall_expired = 1;
    if (supervised_pid > 0) kill(supervised_pid, SIGKILL);
}
int main(int argc, char **argv) {
    if (argc == 1) return worker_main();
    if (argc != 2 || strcmp(argv[1], "--supervise")) return 125;
    struct sigaction action = {0};
    action.sa_handler = wall_alarm;
    sigemptyset(&action.sa_mask);
    if (sigaction(SIGALRM, &action, NULL)) return 125;
    pid_t pid = fork();
    if (pid < 0) return 125;
    if (pid == 0) _exit(worker_main());
    supervised_pid = pid;
    alarm(2);
    int status = 0;
    /* Observe exit without reaping first: the PID cannot be reused while the
     * alarm handler still names it. Disarm/clear the target before waitpid. */
    siginfo_t observed;
    int observation;
    do { observation = waitid(P_PID, pid, &observed, WEXITED|WNOWAIT); }
    while (observation < 0 && errno == EINTR);
    if (observation < 0) kill(pid, SIGKILL);
    alarm(0);
    supervised_pid = 0;
    pid_t waited;
    do { waited = waitpid(pid, &status, 0); } while (waited < 0 && errno == EINTR);
    int reaped = waited == pid;
    int signal = reaped && WIFSIGNALED(status) ? WTERMSIG(status) : 0;
    fprintf(stderr, "{\"timed_out\":%s,\"reaped\":%s,\"signal\":%d}\n", wall_expired ? "true" : "false", reaped ? "true" : "false", signal);
    if (!reaped) return 125;
    if (wall_expired) return 124;
    return WIFEXITED(status) ? WEXITSTATUS(status) : 128+signal;
}
