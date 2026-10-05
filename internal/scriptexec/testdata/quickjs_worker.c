/* Inactive experimental worker; no production linkage or authorization.
 * Build only in the opt-in fixture. The parent is a fixture, NOT Brain services.
 * Reuse the observed Linux boundary without claiming it is reviewed policy. */
#define main native_probe_main
#include "probe.c"
#undef main

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
int main(void) {
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
