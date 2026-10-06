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
static unsigned log_count = 0;
static size_t log_bytes = 0;
static int terminal_sent = 0;
static unsigned unhandled_rejections = 0;

/* Pinned QuickJS calls false once on unhandled rejection and true once when a
 * handler is subsequently attached. Count without retaining/formatting secrets.
 * A caught rejection in this job turn is permitted; any outstanding rejection
 * prevents a successful terminal frame. */
static void track_rejection(JSContext *ctx, JSValueConst promise,
                            JSValueConst reason, JS_BOOL handled, void *opaque) {
    (void)ctx; (void)promise; (void)reason; (void)opaque;
    if (handled) {
        if (!unhandled_rejections) _exit(139);
        unhandled_rejections--;
    } else {
        if (unhandled_rejections == ~0U) _exit(139);
        unhandled_rejections++;
    }
}

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
/* Payload is already serialized and no JS runs while constructing this envelope.
 * Sequence is read only here, AFTER all reentrant calls have completed. */
static int send_json(const char *kind, const char *text, size_t size) {
    if (terminal_sent || !text || !size || (!strcmp(kind,"call") && sequence>OP_LIMIT)) return -1;
    char prefix[128];
    int n = snprintf(prefix,sizeof(prefix),"{\"version\":1,\"kind\":\"%s\",\"sequence\":%u,\"payload\":",kind,sequence);
    if (n<0 || (size_t)n>=sizeof(prefix) || size>FRAME_LIMIT-(size_t)n-1) return -1;
    size_t total=(size_t)n+size+1;
    unsigned char h[4]={total>>24,total>>16,total>>8,total};
    if (exact_write(h,4) || exact_write(prefix,n) || exact_write(text,size) || exact_write("}",1)) return -1;
    if (!strcmp(kind,"result")) terminal_sent=1;
    return 0;
}
static int send_value(JSContext *ctx, const char *kind, JSValueConst payload) {
    JSValue json = JS_JSONStringify(ctx, payload, JS_UNDEFINED, JS_UNDEFINED);
    size_t size = 0;
    const char *text = JS_IsString(json) ? JS_ToCStringLen(ctx, &size, json) : NULL;
    int rc = -1;
    if (text && size && size <= FRAME_LIMIT) {
        /* Serialization may enqueue async effects. Drain them before terminal,
         * never reserialize the value or execute jobs after terminal output. */
        int job=0; JSContext *jobctx;
        if (!strcmp(kind,"result"))
            while ((job=JS_ExecutePendingJob(JS_GetRuntime(ctx),&jobctx))>0) {}
        if (job>=0 && (strcmp(kind,"result") || !unhandled_rejections))
            rc=send_json(kind,text,size);
    }
    JS_FreeCString(ctx, text); JS_FreeValue(ctx, json);
    return rc;
}
/* Never inspect/stringify a submitted exception to manufacture an API error. */
static JSValue fixed_error(JSContext *ctx, const char *code) {
    JSValue error = JS_NewObjectProto(ctx, JS_NULL);
    JS_SetPropertyStr(ctx, error, "code", JS_NewString(ctx,code));
    JS_SetPropertyStr(ctx, error, "message", JS_NewString(ctx,code));
    return JS_Throw(ctx,error);
}
static JSValue entry_get(JSContext *ctx, JSValueConst self, int argc, JSValueConst *argv) {
    (void)self;
    if (terminal_sent || sequence > OP_LIMIT) _exit(130);
    if (argc<1 || argc>2 || !JS_IsString(argv[0]) ||
        (argc==2 && !JS_IsUndefined(argv[1]))) return fixed_error(ctx,"invalid_arguments");
    size_t id_size=0;const char *id=JS_ToCStringLen(ctx,&id_size,argv[0]);
    int valid=id && id_size>0;JS_FreeCString(ctx,id);
    if(!valid)return fixed_error(ctx,"invalid_arguments");
    JSValue call = JS_NewObjectProto(ctx,JS_NULL), args = JS_NewObjectProto(ctx,JS_NULL);
    JS_SetPropertyStr(ctx, args, "id", JS_DupValue(ctx, argv[0]));
    JS_SetPropertyStr(ctx, call, "operation", JS_NewString(ctx, "entries.get"));
    JS_SetPropertyStr(ctx, call, "arguments", args);
    int rc = send_value(ctx, "call", call);
    JS_FreeValue(ctx, call);
    if (rc) _exit(131);
    JSValue response = receive(ctx, "result", sequence++);
    if (JS_IsException(response) || JS_IsUndefined(response)) _exit(132);
    return response;
}
/* Complete public-name surface, NOT a service allowlist. All uncomposed methods
 * fail without touching arguments (including hostile getters/toJSON). The sole
 * transport-enabled method remains the existing entries.get fixture exchange.
 * Keeping absent support explicit avoids silently exposing writes/providers or
 * inventing preflight/authority. No generic operation or HTTP function is public. */
static JSValue unsupported_operation(JSContext *ctx, JSValueConst self, int argc, JSValueConst *argv) {
    (void)self; (void)argc; (void)argv;
    return fixed_error(ctx,"unsupported_operation");
}
/* Match SDK Promise-return semantics, including validation failures. Native
 * creation retains the engine intrinsic even if submitted code replaces Promise.
 * The prototype IPC remains serial/blocking; this does not claim concurrent RPC. */
static JSValue facade_method(JSContext *ctx, JSValueConst self, int argc, JSValueConst *argv, int fixture) {
    JSValue functions[2];
    JSValue promise=JS_NewPromiseCapability(ctx,functions);
    if(JS_IsException(promise))return promise;
    JSValue value=fixture?entry_get(ctx,self,argc,argv):unsupported_operation(ctx,self,argc,argv);
    int rejected=JS_IsException(value);
    if(rejected)value=JS_GetException(ctx);
    JSValue settled=JS_Call(ctx,functions[rejected],JS_UNDEFINED,1,&value);
    JS_FreeValue(ctx,value);JS_FreeValue(ctx,functions[0]);JS_FreeValue(ctx,functions[1]);
    if(JS_IsException(settled)){JS_FreeValue(ctx,promise);return settled;}
    JS_FreeValue(ctx,settled);return promise;
}
static int install_brain(JSContext *ctx, JSValueConst global) {
    const struct { const char *space; const char *methods; } surface[] = {
        {"", "health inject search"},
        {"entries", "updateMetadata iterate move bulkUpdate bulkDelete get list create update delete"},
        {"attachments", "list get delete extract text forEntry attach detach upload download"},
        {"tasks", "delivery verifyDelivery resume resumeWithContext assign clearAssignment trigger run dispatch logs status metadata claimStatus ready next waiting blocked list get"},
        {"projects", "delete getPlacement setPlacement run list"},
        {"features", "resume resumeWithContext assign clearAssignment checkout run cancel chains list ready get"},
        {"observability", "timeline stats stale"},
        {"events", "stream recent wait resourceHealth"},
        {"goals", "list create update delete progress audit run"},
        {"webhooks", "list get create update delete deliveries test"},
        {"automations", "run runs getRun"},
        {"reminders", "list get create update delete ack snooze fire"},
        {"attention", "list counts get create read unread snooze resolve dismiss"},
        {"sections", "list get"},
        {"graph", "orphans backlinks outlinks related"}
    };
    JSValue brain=JS_NewObjectProto(ctx,JS_NULL);
    for(size_t i=0;i<sizeof(surface)/sizeof(surface[0]);i++) {
        JSValue target=surface[i].space[0]?JS_NewObjectProto(ctx,JS_NULL):JS_DupValue(ctx,brain);
        const char *p=surface[i].methods;
        while(*p) {
            const char *end=strchr(p,' ');size_t n=end?(size_t)(end-p):strlen(p);
            char name[32];if(n>=sizeof(name))return -1;
            memcpy(name,p,n);name[n]=0;
            int fixture=!strcmp(surface[i].space,"entries")&&!strcmp(name,"get");
            JSValue fn=JS_NewCFunctionMagic(ctx,facade_method,name,fixture?1:0,JS_CFUNC_generic_magic,fixture);
            if(JS_SetPropertyStr(ctx,target,name,fn)<0)return -1;
            p=end?end+1:p+n;
        }
        if(surface[i].space[0]) {
            if(JS_SetPropertyStr(ctx,brain,surface[i].space,target)<0)return -1;
        } else JS_FreeValue(ctx,target);
    }
    if(JS_SetPropertyStr(ctx,global,"brain",brain)<0)return -1;
    /* Executed before source, while intrinsics are trusted. No private dispatch
     * closure or host token is reachable through the immutable public surface. */
    const char *freeze="for(const ns of Object.keys(brain)){if(typeof brain[ns]!==\"function\")Object.freeze(brain[ns]);}Object.freeze(brain);";
    JSValue done=JS_Eval(ctx,freeze,strlen(freeze),"facade",JS_EVAL_TYPE_GLOBAL);
    int failed=JS_IsException(done);JS_FreeValue(ctx,done);return failed?-1:0;
}
/* Console is protected IPC, never a host logger. Parent must independently bound
 * and quarantine these messages. This fixture has no authorized output release. */
static JSValue console_log(JSContext *ctx, JSValueConst self, int argc, JSValueConst *argv, int level) {
    (void)self;
    const char *levels[] = {"debug", "info", "warn", "error", "log"};
    if (terminal_sent || log_count >= 32 || sequence > OP_LIMIT) _exit(138);
    JSValue args = JS_NewObjectProto(ctx,JS_NULL), values = JS_NewArray(ctx);
    for (int i=0; i<argc; i++) JS_SetPropertyUint32(ctx, values, i, JS_DupValue(ctx, argv[i]));
    JS_SetPropertyStr(ctx, args, "level", JS_NewString(ctx, levels[level]));
    JS_SetPropertyStr(ctx, args, "values", values);
    /* Serialize once: callbacks may log recursively or perform brokered calls. */
    JSValue json = JS_JSONStringify(ctx, args, JS_UNDEFINED, JS_UNDEFINED);
    size_t size = 0;
    const char *text = JS_IsException(json) ? NULL : JS_ToCStringLen(ctx, &size, json);
    if (!text || log_count>=32 || size > 8192 || size > 16384-log_bytes) _exit(138);
    log_count++; log_bytes += size;
    const char *prefix="{\"operation\":\"console.log\",\"arguments\":";
    size_t total=strlen(prefix)+size+1;
    char *call=malloc(total+1);
    if (!call) _exit(138);
    memcpy(call,prefix,strlen(prefix));memcpy(call+strlen(prefix),text,size);
    call[total-1]='}';call[total]=0;
    int rc = send_json("call",call,total);
    free(call);JS_FreeCString(ctx,text);JS_FreeValue(ctx,json);JS_FreeValue(ctx,args);
    if (rc) _exit(131);
    JSValue ack = receive(ctx, "result", sequence++);
    if (!JS_IsNull(ack)) _exit(132);
    JS_FreeValue(ctx, ack);
    return JS_UNDEFINED;
}
static int worker_main(void) {
    setbuf(stdout, NULL);
    JSRuntime *rt = JS_NewRuntime();
    if (!rt) return 121;
    JS_SetHostPromiseRejectionTracker(rt,track_rejection,NULL);
    JS_SetMemoryLimit(rt, 16*1024*1024);
    JS_SetMaxStackSize(rt, 512*1024);
    JSContext *ctx = JS_NewContext(rt);
    /* Before reading/compiling submitted source: close all inherited non-IPC
     * descriptors and establish irreversible hard CPU plus address-space limits. */
    struct rlimit cpu = {1, 1};
    if (!ctx || syscall(SYS_close_range, 3U, ~0U, 0) || setrlimit(RLIMIT_CPU, &cpu) || seal()) return 125;
    /* Retain the intrinsic async adapter before submitted code can replace
     * globals. Final expressions and explicit returns use the same await rules. */
    const char *await_source = "(async value => await value)";
    JSValue await_value = JS_Eval(ctx, await_source, strlen(await_source),
        "completion-adapter", JS_EVAL_TYPE_GLOBAL);
    if (JS_IsException(await_value)) return 125;
    JSValue source = receive(ctx, "call", 1);
    size_t size = 0;
    const char *text = JS_IsString(source) ? JS_ToCStringLen(ctx, &size, source) : NULL;
    if (!text || !size || size > SOURCE_LIMIT) return 133;
    JSValue global = JS_GetGlobalObject(ctx);
    if(install_brain(ctx,global))return 125;
    JSValue console = JS_NewObject(ctx);
    const char *levels[] = {"debug", "info", "warn", "error", "log"};
    for (int i=0; i<5; i++)
        JS_SetPropertyStr(ctx, console, levels[i], JS_NewCFunctionMagic(ctx, console_log, levels[i], 0, JS_CFUNC_generic_magic, i));
    JS_SetPropertyStr(ctx, global, "console", console);
    JS_FreeValue(ctx, global);
    /* Ask the engine for JavaScript completion values, including top-level await.
     * COMPILE_ONLY is essential: fallback must never evaluate an effect twice.
     * Global eval rejects top-level return; a function-body parse supports that
     * alternate submission form. No regex/token guessing or runtime-error retry. */
    int completion_wrapped = 1;
    JSValue compiled = JS_Eval(ctx, text, size, "submitted",
        JS_EVAL_TYPE_GLOBAL|JS_EVAL_FLAG_ASYNC|JS_EVAL_FLAG_COMPILE_ONLY);
    if (JS_IsException(compiled)) {
        JS_FreeValue(ctx, JS_GetException(ctx));
        completion_wrapped = 0;
        const char *prefix = "(async()=>{\n", *suffix = "\n})()";
        size_t length = strlen(prefix)+size+strlen(suffix);
        char *program = malloc(length+1);
        if (!program) return 134;
        memcpy(program, prefix, strlen(prefix));
        memcpy(program+strlen(prefix), text, size);
        memcpy(program+strlen(prefix)+size, suffix, strlen(suffix)+1);
        compiled = JS_Eval(ctx, program, length, "submitted",
            JS_EVAL_TYPE_GLOBAL|JS_EVAL_FLAG_COMPILE_ONLY);
        free(program);
    }
    JS_FreeCString(ctx, text); JS_FreeValue(ctx, source);
    if (JS_IsException(compiled)) return 135;
    JSValue promise = JS_EvalFunction(ctx, compiled);
    if (JS_IsException(promise)) return 135;
    JSContext *jobctx; int job;
    while ((job=JS_ExecutePendingJob(rt, &jobctx))>0) {}
    if (job<0 || JS_PromiseState(ctx, promise)!=JS_PROMISE_FULFILLED) return 136;
    JSValue result = JS_PromiseResult(ctx, promise);
    if (completion_wrapped) {
        /* QuickJS's async global eval wraps completion as {value: completion}
         * to avoid assimilating a returned promise (pinned quickjs.c parser). */
        JSValue value = JS_GetPropertyStr(ctx, result, "value");
        JS_FreeValue(ctx, result);
        result = value;
    }
    if (JS_IsException(result)) return 136;
    JSValue settled = JS_Call(ctx, await_value, JS_UNDEFINED, 1, &result);
    JS_FreeValue(ctx, result); JS_FreeValue(ctx, await_value);
    if (JS_IsException(settled)) return 136;
    while ((job=JS_ExecutePendingJob(rt, &jobctx))>0) {}
    if (job<0 || JS_PromiseState(ctx, settled)!=JS_PROMISE_FULFILLED) return 136;
    result = JS_PromiseResult(ctx, settled);
    JS_FreeValue(ctx, settled);
    if (JS_IsUndefined(result) || send_value(ctx, "result", result)) return 137;
    JS_FreeValue(ctx, result); JS_FreeValue(ctx, promise);
    JS_FreeContext(ctx); JS_FreeRuntime(rt);
    return 0;
}

/* Trusted, test-only supervisor. It never reads an operation or contains any
 * service authority. Its hard wall timer covers blocked reads as well as JS.
 * Production coordinator/graph shutdown and cross-platform launch remain absent. */
static volatile sig_atomic_t supervised_pid = 0;
static volatile sig_atomic_t wall_expired = 0;
static volatile sig_atomic_t cancelled = 0;
static void wall_alarm(int signum) {
    (void)signum;
    wall_expired = 1;
    if (supervised_pid > 0) kill(supervised_pid, SIGKILL);
}
static void request_cancel(int signum) {
    (void)signum;
    cancelled = 1;
    if (supervised_pid > 0) kill(supervised_pid, SIGKILL);
}
int main(int argc, char **argv) {
    if (argc == 1) return worker_main();
    if (argc != 2 || strcmp(argv[1], "--supervise")) return 125;
    struct sigaction action = {0};
    action.sa_handler = wall_alarm;
    sigemptyset(&action.sa_mask);
    if (sigaction(SIGALRM, &action, NULL)) return 125;
    action.sa_handler = request_cancel;
    if (sigaction(SIGTERM, &action, NULL)) return 125;
    pid_t supervisor = getpid();
    pid_t pid = fork();
    if (pid < 0) return 125;
    if (pid == 0) {
        /* Install before untrusted input and check the fork/prctl race. The
         * later deny-default seal prohibits clearing the parent-death signal.
         * Reaping after supervisor death belongs to init/an external subreaper. */
        if (prctl(PR_SET_PDEATHSIG, SIGKILL, 0, 0, 0) || getppid() != supervisor) _exit(125);
        _exit(worker_main());
    }
    supervised_pid = pid;
    /* SIGTERM may arrive after installing the handler but before fork/pid
     * publication. Do not lose that cancellation request. */
    if (cancelled) kill(pid, SIGKILL);
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
    fprintf(stderr, "{\"timed_out\":%s,\"cancelled\":%s,\"reaped\":%s,\"signal\":%d}\n", wall_expired ? "true" : "false", cancelled ? "true" : "false", reaped ? "true" : "false", signal);
    if (!reaped) return 125;
    if (cancelled) return 143;
    if (wall_expired) return 124;
    return WIFEXITED(status) ? WEXITSTATUS(status) : 128+signal;
}
