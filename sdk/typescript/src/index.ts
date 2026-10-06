export type { components, paths, operations } from "./schema.gen.js";
import type { components, operations } from "./schema.gen.js";
type Schema = components["schemas"];

export const contractVersion = "1.0.0";
export type CapabilityManifest = Schema["CapabilityManifest"];

function decodeCapabilities(body: Uint8Array): CapabilityManifest {
  const invalid = (): never => { throw new BrainError("invalid_capability_manifest"); };
  const exact = (value: unknown, names: string[]): value is Record<string, unknown> => value !== null && typeof value === "object" && !Array.isArray(value) && Object.keys(value).length === names.length && names.every(k=>Object.hasOwn(value,k));
  if (!body.byteLength || body.byteLength>65536) invalid();
  let value: unknown;
  try {
    const text = new TextDecoder("utf-8",{fatal:true,ignoreBOM:true}).decode(body);
    value = JSON.parse(text);
    // JSON.parse validates grammar but discards duplicate decoded object keys.
    const tokens = [...text.matchAll(/"(?:\\.|[^"\\])*"|[{}\[\]:,]|true|false|null|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g)].map(m=>m[0]);
    const stack: (Set<string>|null)[] = [];
    for (let i=0;i<tokens.length;i++) {
      const token = tokens[i]!;
      if (token==='{' || token==='[') {stack.push(token==='{'?new Set():null);if(stack.length>4)invalid();}
      else if(token==='}'||token===']')stack.pop();
      else if(token.startsWith('"')&&tokens[i+1]===':') {
        const key=JSON.parse(token) as string, seen=stack[stack.length-1];
        if(!seen||seen.has(key))invalid();seen!.add(key);
      }
    }
  } catch { invalid(); }
  if (!exact(value,['contract_version','operations','scripts'])) return invalid();
  if(typeof value.contract_version!=='string'||!value.contract_version||new TextEncoder().encode(value.contract_version).length>64)invalid();
  if(!Array.isArray(value.operations)||value.operations.length>10000)return invalid();
  const seen=new Set<string>();
  for(const op of value.operations){if(typeof op!=='string'||!/^[a-z][a-zA-Z0-9]{0,63}\.[a-z][a-zA-Z0-9]{0,63}$/.test(op)||seen.has(op))invalid();seen.add(op);}
  const s=value.scripts;
  const fields=['compiled','configured','deployment_available','caller_authorized','available'];
  if(!exact(s,fields)||fields.some(k=>typeof s[k]!=='boolean')||s.available!==(s.compiled&&s.configured&&s.deployment_available&&s.caller_authorized))invalid();
  if(value.contract_version!==contractVersion)throw new BrainError('incompatible_contract_version');
  return value as unknown as CapabilityManifest;
}

export interface ClientConfig {
  baseUrl: string;
  token?: string;
  tenant?: string;
  authGeneration?: string;
  timeoutMs?: number;
  maxResponseBytes?: number;
  /** Trusted transport seam. Must honor AbortSignal and redirect: manual. */
  fetch?: typeof globalThis.fetch;
}
export interface RequestOptions {
  signal?: AbortSignal;
  requestId?: string;
  idempotencyKey?: string;
}
export interface StreamOptions extends RequestOptions { lastEventId?: string }
export type EntriesListParams = NonNullable<operations["entries.list"]["parameters"]["query"]>;
export interface FieldViolation {field: string; message: string}

const machineCode = /^[a-z][a-z0-9_]{0,63}$/;
// Parity with Go Error(): stable machine code and HTTP status only. Server
// message, request ID and details stay in explicit, non-enumerable fields.
function formatBrainError(code: string, status: number): string {
  const stable = typeof code === "string" && machineCode.test(code) ? code : "";
  if (stable && status) return `brain: ${stable} (HTTP ${status})`;
  if (stable) return `brain: ${stable}`;
  return status ? `brain: request failed (HTTP ${status})` : "brain: request failed";
}

export class BrainError extends Error {
  constructor(
    readonly code: string,
    readonly status = 0,
    readonly requestId = "",
    readonly serverMessage = "",
    readonly retryable = false,
    readonly details: readonly FieldViolation[] = [],
  ) {
    super(formatBrainError(code, status));
    this.name = "BrainError";
    // Wire-derived fields require explicit access, never ordinary logging.
    for (const key of ["code", "requestId", "serverMessage", "details"]) {
      Object.defineProperty(this, key, {enumerable: false});
    }
  }
  [Symbol.for("nodejs.util.inspect.custom")](): string { return this.toString(); }
}

export class BrainClient {
  readonly #config: Readonly<ClientConfig & {timeoutMs: number; maxResponseBytes: number}>;
  readonly #lifetime = new AbortController();

  constructor(config: ClientConfig) {
    let url: URL;
    try { url = new URL(config.baseUrl); }
    catch { throw new BrainError("invalid_configuration"); }
    const timeoutMs = config.timeoutMs ?? 30_000;
    const maxResponseBytes = config.maxResponseBytes ?? 8 * 1024 * 1024;
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash ||
      !Number.isSafeInteger(timeoutMs) || timeoutMs <= 0 || timeoutMs > 2_147_483_647 ||
      !Number.isSafeInteger(maxResponseBytes) || maxResponseBytes <= 0 || maxResponseBytes > 64 * 1024 * 1024 ||
      [config.token, config.tenant, config.authGeneration].some(s => s !== undefined && /[\r\n\0]/.test(s))) {
      throw new BrainError("invalid_configuration");
    }
    this.#config = Object.freeze({...config, baseUrl: url.href.replace(/\/+$/, ""), timeoutMs, maxResponseBytes});
  }

  close(): void { this.#lifetime.abort(); }
  rebind(config: ClientConfig): BrainClient {
    const next = new BrainClient(config);
    this.close();
    return next;
  }

  async #request<T>(method: string, path: string, body?: unknown, query?: object, options: StreamOptions = {}, expectBody = true, binary = false, stream?: (response: Response,signal: AbortSignal)=>Promise<void>, responseLimit = this.#config.maxResponseBytes, expectedStatus = 0): Promise<T> {
    // Reject before WHATWG URL normalization, including encoded path segments.
    for (let decoded = path;;) {
      if (decoded.split(/[/\\]/).some(segment => segment === "." || segment === "..")) throw new BrainError("invalid_request");
      let next: string;
      try { next = decodeURIComponent(decoded); } catch { break; }
      if (next === decoded) break;
      decoded = next;
    }
    const signals = [this.#lifetime.signal, AbortSignal.timeout(this.#config.timeoutMs)];
    if (options.signal) signals.push(options.signal);
    const signal = AbortSignal.any(signals);
    signal.throwIfAborted();
    const q = new URLSearchParams();
    for (const [key, value] of Object.entries(query ?? {})) {
      if (value === undefined) continue;
      if (Array.isArray(value)) for (const item of value) q.append(key, String(item));
      else q.set(key, String(value));
    }
    const suffix = q.size ? `?${q}` : "";
    const headers = new Headers({Accept: "application/json"});
    if(stream){
      headers.set("Accept","text/event-stream");
      if(options.lastEventId!==undefined){
        if(/[\r\n\0]/.test(options.lastEventId))throw new BrainError("invalid_request");
        headers.set("Last-Event-ID",options.lastEventId);
      }
    }
    if (body !== undefined && !(body instanceof FormData)) headers.set("Content-Type", "application/json");
    if (this.#config.token) headers.set("Authorization", `Bearer ${this.#config.token}`);
    if (this.#config.tenant) headers.set("X-Brain-Tenant", this.#config.tenant);
    if (options.requestId) headers.set("X-Request-ID", options.requestId);
    if (options.idempotencyKey) headers.set("Idempotency-Key", options.idempotencyKey);
    let response: Response;
    try {
      response = await (this.#config.fetch ?? globalThis.fetch)(`${this.#config.baseUrl}/api/v1${path}${suffix}`, {
        method, headers, body: body instanceof FormData ? body : body === undefined ? undefined : JSON.stringify(body), signal, redirect: "manual",
      });
    } catch {
      signal.throwIfAborted();
      throw new BrainError("transport_error");
    }
    const requestId = response.headers.get("X-Request-ID") ?? "";
    if (response.status >= 300 && response.status < 400) {
      await response.body?.cancel();
      throw new BrainError("redirect_refused", response.status, requestId);
    }
    if(stream && response.ok){await stream(response,signal);return undefined as T;}
    const reader = response.body?.getReader();
    const chunks: Uint8Array[] = [];
    let total = 0;
    try {
      if (reader) while (true) {
        const {done, value} = await reader.read();
        signal.throwIfAborted();
        if (done) break;
        total += value.byteLength;
        if (total > Math.min(responseLimit,this.#config.maxResponseBytes)) {
          await reader.cancel();
          throw new BrainError("response_too_large", response.status, requestId);
        }
        chunks.push(value);
      }
    } catch (error) {
      signal.throwIfAborted();
      if (error instanceof BrainError) throw error;
      throw new BrainError("response_read_failed", response.status, requestId);
    } finally { reader?.releaseLock(); }
    signal.throwIfAborted();
    const data = new Uint8Array(total);
    let offset = 0;
    for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.byteLength; }
    const text = new TextDecoder().decode(data);
    if (!response.ok) {
      let wire: {code?: unknown; message?: unknown; error?: unknown; request_id?: unknown; details?: unknown} = {};
      try { const parsed: unknown = JSON.parse(text); if (parsed && typeof parsed === "object") wire = parsed; } catch { /* Legacy non-JSON errors retain their HTTP code. */ }
      const codes: Record<number, string> = {400:"invalid_request",401:"unauthorized",403:"forbidden",404:"not_found",409:"conflict",429:"rate_limited",501:"unsupported_operation",503:"unavailable"};
      const details = Array.isArray(wire.details) ? wire.details.filter((v): v is FieldViolation => !!v && typeof v === "object" && typeof v.field === "string" && typeof v.message === "string").map(v=>({field:v.field,message:v.message})) : [];
      throw new BrainError(typeof wire.code === "string" && /^[a-z][a-z0-9_]{0,63}$/.test(wire.code) ? wire.code : codes[response.status] ?? "http_error", response.status, requestId || (typeof wire.request_id === "string" ? wire.request_id : ""),
        typeof wire.message === "string" && wire.message !== "" ? wire.message : typeof wire.error === "string" ? wire.error : "", [429,503].includes(response.status), details);
    }
    if (expectedStatus && response.status!==expectedStatus) throw new BrainError("unexpected_status",response.status,requestId);
    if (!expectBody) return undefined as T;
    if (binary) return data as T;
    try { return JSON.parse(text) as T; }
    catch { throw new BrainError("invalid_response", response.status, requestId); }
  }

  async #readEvents(response: Response, signal: AbortSignal, onEvent: (event: Schema["Event"]) => void | Promise<void>): Promise<void> {
    const failure = (code: string) => new BrainError(code, response.status, response.headers.get("X-Request-ID") ?? "");
    if (response.headers.get("Content-Type")?.split(";")[0]?.trim().toLowerCase() !== "text/event-stream" || !response.body) {
      await response.body?.cancel();
      throw failure("invalid_response");
    }
    const reader = response.body.getReader();
    const abort = () => { void reader.cancel().catch(() => {}); };
    signal.addEventListener("abort", abort, {once: true});
    const decoder = new TextDecoder("utf-8", {fatal: true});
    let line: number[] = [], data = "", size = 0;
    try {
      signal.throwIfAborted();
      while (true) {
        let chunk: ReadableStreamReadResult<Uint8Array>;
        try { chunk = await reader.read(); }
        catch { signal.throwIfAborted(); throw failure("response_read_failed"); }
        signal.throwIfAborted();
        if (chunk.done) return; // Incomplete frames are not delivered at EOF.
        for (const byte of chunk.value) {
          if (++size > this.#config.maxResponseBytes) throw failure("response_too_large");
          if (byte !== 10) { line.push(byte); continue; }
          if (line.at(-1) === 13) line.pop();
          let text: string;
          try { text = decoder.decode(new Uint8Array(line)); }
          catch { throw failure("invalid_response"); }
          line = [];
          if (text === "") {
            if (data !== "") {
              let event: Schema["Event"];
              try {
                event = JSON.parse(data);
                if (!event || typeof event.id !== "string" || !event.id || typeof event.type !== "string" || !event.type || typeof event.source !== "string" || !event.source || typeof event.timestamp !== "string" || !Number.isFinite(Date.parse(event.timestamp))) throw new Error();
              } catch { throw failure("invalid_response"); }
              signal.throwIfAborted();
              await onEvent(event);
              signal.throwIfAborted();
            }
            data = ""; size = 0;
          } else if (text.startsWith("data:")) {
            data += text.slice(5).replace(/^ /, "") + "\n";
          }
        }
      }
    } finally {
      signal.removeEventListener("abort", abort);
      await reader.cancel().catch(() => {});
      reader.releaseLock();
    }
  }

  health(options?: RequestOptions): Promise<Schema["HealthResponse"]> { return this.#request("GET", "/health", undefined, undefined, options); }
  /** Contract negotiation is not a grant. No cache, anonymous retry or fallback. */
  async capabilities(options?: RequestOptions): Promise<CapabilityManifest> {
    let body: Uint8Array;
    try { body = await this.#request<Uint8Array>("GET","/capabilities",undefined,undefined,options,true,true,undefined,65536,200); }
    catch(e) {
      if(!(e instanceof BrainError))throw e;
      const code = [404,501].includes(e.status)?'unsupported_server':[401,403].includes(e.status)?'capability_auth_required':e.code==='response_too_large'?'invalid_capability_manifest':'capability_discovery_unavailable';
      throw new BrainError(code,e.status,e.requestId);
    }
    return decodeCapabilities(body);
  }
  inject(request: Schema["InjectRequest"], options?: RequestOptions): Promise<Schema["InjectResponse"]> { return this.#request("POST", "/inject", request, undefined, options); }
  search(request: Schema["SearchRequest"], options?: RequestOptions): Promise<Schema["SearchResponse"]> { return this.#request("POST", "/search", request, undefined, options); }

  readonly entries = Object.freeze({
    updateMetadata: (id: string,request: Schema["MetadataUpdateRequest"],options?: RequestOptions): Promise<Schema["BrainEntry"]> => this.#request("PATCH",`/entries/${encodeURIComponent(id)}/metadata`,request,undefined,options),
    iterate: (query: EntriesListParams = {}, options: RequestOptions = {}): AsyncGenerator<Schema["BrainEntry"]> => this.#iterateEntries({...query}, {...options}),
    move: (id: string, request: Schema["MoveEntryRequest"], options?: RequestOptions): Promise<Schema["MoveResult"]> => this.#request("POST", `/entries/${encodeURIComponent(id)}/move`, request, undefined, options),
    bulkUpdate: (request: Schema["BulkUpdateRequest"], options?: RequestOptions): Promise<Schema["BulkUpdateResponse"]> => this.#request("POST", "/entries/bulk-update", request, undefined, options),
    bulkDelete: (request: Schema["BulkDeleteRequest"], options?: RequestOptions): Promise<Schema["BulkDeleteResponse"]> => this.#request("POST", "/entries/bulk-delete", request, undefined, options),
    get: (id: string, options?: RequestOptions): Promise<Schema["BrainEntry"]> => this.#request("GET", `/entries/${encodeURIComponent(id)}`, undefined, undefined, options),
    list: (query?: EntriesListParams, options?: RequestOptions): Promise<Schema["ListEntriesResponse"]> => this.#request("GET", "/entries", undefined, query, options),
    create: (request: Schema["CreateEntryRequest"], options?: RequestOptions): Promise<Schema["CreateEntryResponse"]> => this.#request("POST", "/entries", request, undefined, options),
    update: (id: string, request: Schema["UpdateEntryRequest"], options?: RequestOptions): Promise<Schema["BrainEntry"]> => this.#request("PATCH", `/entries/${encodeURIComponent(id)}`, request, undefined, options),
    delete: (id: string, force = false, options?: RequestOptions): Promise<void> => this.#request("DELETE", `/entries/${encodeURIComponent(id)}`, undefined, {confirm:true,...(force ? {force:true} : {})}, options, false),
  });
  readonly attachments = Object.freeze({
    list: (project: string, options?: RequestOptions): Promise<Schema["ListAttachmentsResponse"]> => this.#request("GET","/attachments",undefined,{project_id:project},options),
    get: (project: string,id: string, options?: RequestOptions): Promise<Schema["Attachment"]> => this.#request("GET",`/attachments/${encodeURIComponent(id)}`,undefined,{project_id:project},options),
    delete: (project: string,id: string, options?: RequestOptions): Promise<Schema["AttachmentDeletionResponse"]> => this.#request("DELETE",`/attachments/${encodeURIComponent(id)}`,undefined,{project_id:project},options),
    extract: (project: string,id: string, request: Schema["AttachmentExtractionRequest"], options?: RequestOptions): Promise<Schema["AttachmentExtractionResult"]> => this.#request("POST",`/attachments/${encodeURIComponent(id)}/extract`,request,{project_id:project},options),
    text: async (project: string,id: string, options?: RequestOptions): Promise<string> => new TextDecoder().decode(await this.#request<Uint8Array>("GET",`/attachments/${encodeURIComponent(id)}/text`,undefined,{project_id:project},options,true,true)),
    forEntry: (project: string,id: string, options?: RequestOptions): Promise<Schema["AttachEntryAttachmentResponse"]> => this.#request("GET",`/entries/${encodeURIComponent(id)}/attachments`,undefined,{project_id:project},options),
    attach: (project: string,id: string,request: Schema["AttachEntryAttachmentRequest"], options?: RequestOptions): Promise<Schema["AttachEntryAttachmentResponse"]> => this.#request("POST",`/entries/${encodeURIComponent(id)}/attachments`,request,{project_id:project},options),
    detach: (project: string,id: string,attachmentID: string,role: string, options?: RequestOptions): Promise<Schema["AttachEntryAttachmentResponse"]> => this.#request("DELETE",`/entries/${encodeURIComponent(id)}/attachments/${encodeURIComponent(attachmentID)}`,undefined,{project_id:project,role},options),
    upload: async (project: string, request: {filename: string; content: Uint8Array; contentType?: string; metadata?: Record<string,string>}, options?: RequestOptions): Promise<Schema["CreateAttachmentResponse"]> => {
      if (!request.filename || [".",".."].includes(request.filename) || /[/\\\r\n\0]/.test(request.filename) || /[\r\n\0]/.test(request.contentType ?? "")) throw new BrainError("invalid_request");
      if (request.content.byteLength > this.#config.maxResponseBytes) throw new BrainError("request_too_large");
      const form = new FormData();form.set("project_id",project);
      if (request.metadata) form.set("metadata",JSON.stringify(request.metadata));
      form.set("file",new Blob([new Uint8Array(request.content)],{type:request.contentType ?? "application/octet-stream"}),request.filename);
      // Materialize once to enforce the encoded multipart bound, including metadata.
      const encoded = new Request("http://localhost",{method:"POST",body:form});
      if ((await encoded.arrayBuffer()).byteLength > this.#config.maxResponseBytes) throw new BrainError("request_too_large");
      return this.#request("POST","/attachments",form,undefined,options);
    },
    download: (project: string, id: string, options?: RequestOptions): Promise<Uint8Array> => this.#request("GET",`/attachments/${encodeURIComponent(id)}/content`,undefined,{project_id:project},options,true,true),
  });
  readonly tasks = Object.freeze({
    delivery: (project: string,id: string,options?: RequestOptions): Promise<Schema["TaskDeliveryResponse"]> => this.#request("GET",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/delivery`,undefined,undefined,options),
    verifyDelivery: (project: string,id: string,request: Schema["DeliveryCommand"],options?: RequestOptions): Promise<Schema["DeliveryUpdateResponse"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/delivery`,request,undefined,options),
    resume: (project: string,id: string,request: Schema["ResumeTaskOptions"] = {},options?: RequestOptions): Promise<Schema["ResumeTaskResult"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/resume`,request,undefined,options),
    resumeWithContext: (project: string,id: string,request: Schema["ResumeWithContextOptions"],options?: RequestOptions): Promise<Schema["ResumeWithContextResult"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/resume-with-context`,request,undefined,options),
    assign: (project: string,id: string,request: Schema["TaskAssignmentRequest"],options?: RequestOptions): Promise<Schema["TaskAssignmentResponse"]> => this.#request("PUT",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/assignment`,request,undefined,options),
    clearAssignment: (project: string,id: string,request: Schema["ClearFeatureAssignmentRequest"],options?: RequestOptions): Promise<Schema["TaskAssignmentResponse"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/assignment/clear`,request,undefined,options),
    trigger: (project: string,id: string,options?: RequestOptions): Promise<Schema["TriggerResponse"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/trigger`,undefined,undefined,options),
    run: (project: string,id: string,request: Schema["RunTaskRequest"] = {},options?: RequestOptions): Promise<Schema["RunTaskResponse"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/run`,request,undefined,options),
    dispatch: (project: string,id: string,request: Schema["DispatchRequest"],options?: RequestOptions): Promise<Schema["SDKDispatchResponse"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/dispatch`,request,undefined,options),
    logs: (project: string,id: string,query?: NonNullable<operations["tasks.logs"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["LogQueryResponse"]> => this.#request("GET",`/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/logs`,undefined,query,options),
    status: (project: string, request: Schema["MultiTaskStatusRequest"], options?: RequestOptions): Promise<Schema["MultiTaskStatusResponse"]> => this.#request("POST", `/tasks/${encodeURIComponent(project)}/status`, request, undefined, options),
    metadata: (project: string, id: string, options?: RequestOptions): Promise<Schema["TaskMetadataResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/metadata`, undefined, undefined, options),
    claimStatus: (project: string, id: string, options?: RequestOptions): Promise<Schema["ClaimStatusResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}/claim-status`, undefined, undefined, options),
    ready: (project: string, query?: NonNullable<operations["tasks.ready"]["parameters"]["query"]>, options?: RequestOptions): Promise<Schema["TaskSelectionResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/ready`, undefined, query, options),
    next: (project: string, query?: NonNullable<operations["tasks.next"]["parameters"]["query"]>, options?: RequestOptions): Promise<Schema["ResolvedTask"] | null> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/next`, undefined, query, options),
    waiting: (project: string, options?: RequestOptions): Promise<Schema["TaskSelectionResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/waiting`, undefined, undefined, options),
    blocked: (project: string, options?: RequestOptions): Promise<Schema["TaskSelectionResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/blocked`, undefined, undefined, options),
    list: (project: string, options?: RequestOptions): Promise<Schema["TaskListResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}`, undefined, undefined, options),
    get: (project: string, id: string, options?: RequestOptions): Promise<Schema["ResolvedTask"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}`, undefined, undefined, options),
  });
  readonly projects = Object.freeze({
    delete: (project: string,confirm: string,force = false,options?: RequestOptions): Promise<Schema["DeleteProjectResponse"]> => this.#request("DELETE",`/tasks/${encodeURIComponent(project)}`,undefined,{confirm,force},options),
    getPlacement: (project: string,options?: RequestOptions): Promise<Schema["ProjectPlacement"] | null> => this.#request("GET",`/projects/${encodeURIComponent(project)}/placement`,undefined,undefined,options),
    setPlacement: (project: string,request: Schema["ProjectPlacement"],options?: RequestOptions): Promise<Schema["ProjectPlacement"]> => this.#request("PUT",`/projects/${encodeURIComponent(project)}/placement`,request,undefined,options),
    run: (project: string,request: Schema["RunProjectRequest"] = {},options?: RequestOptions): Promise<Schema["RunProjectResponse"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/run`,request,undefined,options),
    list: (options?: RequestOptions): Promise<Schema["ProjectListResponse"]> => this.#request("GET","/tasks",undefined,undefined,options),
  });
  readonly features = Object.freeze({
    resume: (project: string,id: string,request: Schema["ResumeTaskOptions"] = {},options?: RequestOptions): Promise<Schema["ResumeFeatureResult"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/features/${encodeURIComponent(id)}/resume`,request,undefined,options),
    resumeWithContext: (project: string,id: string,request: Schema["ResumeWithContextOptions"],options?: RequestOptions): Promise<Schema["ResumeWithContextFeatureResult"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/features/${encodeURIComponent(id)}/resume-with-context`,request,undefined,options),
    assign: (project: string,id: string,request: Schema["FeatureAssignmentRequest"],options?: RequestOptions): Promise<Schema["FeatureAssignmentResponse"]> => this.#request("PUT",`/tasks/${encodeURIComponent(project)}/features/${encodeURIComponent(id)}/assignment`,request,undefined,options),
    clearAssignment: (project: string,id: string,request: Schema["ClearFeatureAssignmentRequest"],options?: RequestOptions): Promise<Schema["FeatureAssignmentResponse"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/features/${encodeURIComponent(id)}/assignment/clear`,request,undefined,options),
    checkout: (project: string,id: string,request: Schema["FeatureCheckoutOptions"] = {},options?: RequestOptions): Promise<Schema["CheckoutFeatureResult"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/features/${encodeURIComponent(id)}/checkout`,request,undefined,options),
    run: (project: string,id: string,request: Schema["RunFeatureRequest"] = {},options?: RequestOptions): Promise<Schema["RunFeatureResponse"]> => this.#request("POST",`/tasks/${encodeURIComponent(project)}/features/${encodeURIComponent(id)}/run`,request,undefined,options),
    cancel: (project: string,id: string,options?: RequestOptions): Promise<Schema["CancelChainResponse"]> => this.#request("DELETE",`/tasks/${encodeURIComponent(project)}/features/${encodeURIComponent(id)}/run`,undefined,undefined,options),
    chains: (project: string,options?: RequestOptions): Promise<Schema["DependentChainsResponse"]> => this.#request("GET",`/tasks/${encodeURIComponent(project)}/chains`,undefined,undefined,options),
    list: (project: string, options?: RequestOptions): Promise<Schema["FeatureListResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/features`, undefined, undefined, options),
    ready: (project: string, options?: RequestOptions): Promise<Schema["FeatureListResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/features/ready`, undefined, undefined, options),
    get: (project: string, id: string, options?: RequestOptions): Promise<Schema["FeatureResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/features/${encodeURIComponent(id)}`, undefined, undefined, options),
  });
  readonly observability = Object.freeze({
    timeline: (query?: NonNullable<operations["observability.timeline"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["TimelineResponse"]> => this.#request("GET","/timeline",undefined,query,options),
    stats: (query?: NonNullable<operations["observability.stats"]["parameters"]["query"]>, options?: RequestOptions): Promise<Schema["StatsResponse"]> => this.#request("GET","/stats",undefined,query,options),
    stale: (query?: NonNullable<operations["observability.stale"]["parameters"]["query"]>, options?: RequestOptions): Promise<Schema["BrainEntry"][] | null> => this.#request("GET","/stale",undefined,query,options),
  });
  readonly events = Object.freeze({
    stream: (query: {project_id?: string;feature_id?: string;type?: string;source?: string},onEvent: (event: Schema["Event"])=>void|Promise<void>,options?: StreamOptions): Promise<void> => this.#request("GET","/events/stream",undefined,query,options,true,false,(response,signal)=>this.#readEvents(response,signal,onEvent)),
    recent: (query?: NonNullable<operations["events.recent"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["RecentEventsResponse"]> => this.#request("GET","/events/recent",undefined,query,options),
    wait: (query: NonNullable<operations["events.wait"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["EventWaitResponse"]> => this.#request("GET","/events/wait",undefined,query,options),
    resourceHealth: (query: NonNullable<operations["events.resourceHealth"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["ResourceHealthResponse"]> => this.#request("GET","/events/resource-health",undefined,query,options),
  });
  readonly goals = Object.freeze({
    list: (query?: NonNullable<operations["goals.list"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["ListGoalsResponse"]> => this.#request("GET","/goals",undefined,query,options),
    create: (request: Schema["CreateGoalRequest"],options?: RequestOptions): Promise<Schema["GoalSummary"]> => this.#request("POST","/goals",request,undefined,options),
    update: (id: string,request: Schema["UpdateGoalRequest"],options?: RequestOptions): Promise<Schema["GoalSummary"]> => this.#request("PATCH",`/goals/${encodeURIComponent(id)}`,request,undefined,options),
    delete: (id: string,options?: RequestOptions): Promise<Schema["DeleteGoalResponse"]> => this.#request("DELETE",`/goals/${encodeURIComponent(id)}`,undefined,undefined,options),
    progress: (id: string,options?: RequestOptions): Promise<Schema["GoalProgressResponse"]> => this.#request("GET",`/goals/${encodeURIComponent(id)}/progress`,undefined,undefined,options),
    audit: (id: string,limit = 50,options?: RequestOptions): Promise<Schema["GoalAuditResponse"]> => this.#request("GET",`/goals/${encodeURIComponent(id)}/audit`,undefined,{limit},options),
    run: (id: string,options?: RequestOptions): Promise<Schema["GoalReconcileAudit"]> => this.#request("POST",`/goals/${encodeURIComponent(id)}/run`,undefined,undefined,options),
  });
  readonly webhooks = Object.freeze({
    list: (enabled = false,options?: RequestOptions): Promise<Schema["ListWebhooksResponse"]> => this.#request("GET","/webhooks",undefined,{enabled},options),
    get: (id: string,options?: RequestOptions): Promise<Schema["WebhookResponse"]> => this.#request("GET",`/webhooks/${encodeURIComponent(id)}`,undefined,undefined,options),
    create: (request: Schema["CreateWebhookRequest"],options?: RequestOptions): Promise<Schema["WebhookResponse"]> => this.#request("POST","/webhooks",request,undefined,options),
    update: (id: string,request: Schema["UpdateWebhookRequest"],options?: RequestOptions): Promise<Schema["WebhookResponse"]> => this.#request("PATCH",`/webhooks/${encodeURIComponent(id)}`,request,undefined,options),
    delete: (id: string,options?: RequestOptions): Promise<Schema["SuccessResponse"]> => this.#request("DELETE",`/webhooks/${encodeURIComponent(id)}`,undefined,undefined,options),
    deliveries: (id: string,limit = 50,options?: RequestOptions): Promise<Schema["ListWebhookDeliveriesResponse"]> => this.#request("GET",`/webhooks/${encodeURIComponent(id)}/deliveries`,undefined,{limit},options),
    test: (id: string,options?: RequestOptions): Promise<Schema["WebhookDeliveryResponse"]> => this.#request("POST",`/webhooks/${encodeURIComponent(id)}/test`,undefined,undefined,options),
  });
  readonly automations = Object.freeze({
    run: (request: Schema["RunAutomationRequest"],options?: RequestOptions): Promise<Schema["RunAutomationResponse"]> => this.#request("POST","/automations/run",request,undefined,options),
    runs: (query?: NonNullable<operations["automations.runs"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["ListEntriesResponse"]> => this.#request("GET","/automation-runs",undefined,query,options),
    getRun: (id: string,options?: RequestOptions): Promise<Schema["BrainEntry"]> => this.#request("GET",`/automation-runs/${encodeURIComponent(id)}`,undefined,undefined,options),
  });
  readonly reminders = Object.freeze({
    list: (query?: NonNullable<operations["reminders.list"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["ReminderListResponse"]> => this.#request("GET","/reminders",undefined,query,options),
    get: (id: string,options?: RequestOptions): Promise<Schema["ReminderSummary"]> => this.#request("GET",`/reminders/${encodeURIComponent(id)}`,undefined,undefined,options),
    create: (request: Schema["CreateReminderRequest"],options?: RequestOptions): Promise<Schema["ReminderSummary"]> => this.#request("POST","/reminders",request,undefined,options),
    update: (id: string,request: Schema["UpdateReminderRequest"],options?: RequestOptions): Promise<Schema["ReminderSummary"]> => this.#request("PATCH",`/reminders/${encodeURIComponent(id)}`,request,undefined,options),
    delete: (id: string,options?: RequestOptions): Promise<Schema["DeletionResponse"]> => this.#request("DELETE",`/reminders/${encodeURIComponent(id)}`,undefined,undefined,options),
    ack: (id: string,options?: RequestOptions): Promise<Schema["ReminderSummary"]> => this.#request("POST",`/reminders/${encodeURIComponent(id)}/ack`,undefined,undefined,options),
    snooze: (id: string,request: Schema["SnoozeReminderRequest"],options?: RequestOptions): Promise<Schema["ReminderSummary"]> => this.#request("POST",`/reminders/${encodeURIComponent(id)}/snooze`,request,undefined,options),
    fire: (id: string,options?: RequestOptions): Promise<Schema["ReminderSummary"]> => this.#request("POST",`/reminders/${encodeURIComponent(id)}/fire`,undefined,undefined,options),
  });
  readonly attention = Object.freeze({
    list: (query?: NonNullable<operations["attention.list"]["parameters"]["query"]>,options?: RequestOptions): Promise<Schema["AttentionListResponse"]> => this.#request("GET","/attention",undefined,query,options),
    counts: (options?: RequestOptions): Promise<Schema["AttentionCounts"]> => this.#request("GET","/attention/counts",undefined,undefined,options),
    get: (id: string,options?: RequestOptions): Promise<Schema["Attention"]> => this.#request("GET",`/attention/${encodeURIComponent(id)}`,undefined,undefined,options),
    create: (request: Schema["CreateAttentionRequest"],options?: RequestOptions): Promise<Schema["Attention"]> => this.#request("POST","/attention",request,undefined,options),
    read: (id: string,options?: RequestOptions): Promise<Schema["Attention"]> => this.#request("POST",`/attention/${encodeURIComponent(id)}/read`,undefined,undefined,options),
    unread: (id: string,options?: RequestOptions): Promise<Schema["Attention"]> => this.#request("POST",`/attention/${encodeURIComponent(id)}/unread`,undefined,undefined,options),
    snooze: (id: string,request: Schema["SnoozeAttentionRequest"],options?: RequestOptions): Promise<Schema["Attention"]> => this.#request("POST",`/attention/${encodeURIComponent(id)}/snooze`,request,undefined,options),
    resolve: (id: string,options?: RequestOptions): Promise<Schema["Attention"]> => this.#request("POST",`/attention/${encodeURIComponent(id)}/resolve`,undefined,undefined,options),
    dismiss: (id: string,options?: RequestOptions): Promise<Schema["Attention"]> => this.#request("POST",`/attention/${encodeURIComponent(id)}/dismiss`,undefined,undefined,options),
  });
  readonly sections = Object.freeze({
    list: (id: string, options?: RequestOptions): Promise<Schema["SectionsResponse"]> => this.#request("GET", `/entries/${encodeURIComponent(id)}/sections`, undefined, undefined, options),
    get: (id: string, title: string, includeSubsections = false, options?: RequestOptions): Promise<Schema["SectionContentResponse"]> => this.#request("GET", `/entries/${encodeURIComponent(id)}/sections/${encodeURIComponent(title)}`, undefined, {includeSubsections}, options),
  });
  readonly graph = Object.freeze({
    orphans: (query?: NonNullable<operations["graph.orphans"]["parameters"]["query"]>, options?: RequestOptions): Promise<Schema["BrainEntry"][] | null> => this.#request("GET","/orphans",undefined,query,options),
    backlinks: (id: string, options?: RequestOptions): Promise<Schema["BrainEntry"][]> => this.#request("GET", `/entries/${encodeURIComponent(id)}/backlinks`, undefined, undefined, options),
    outlinks: (id: string, options?: RequestOptions): Promise<Schema["BrainEntry"][]> => this.#request("GET", `/entries/${encodeURIComponent(id)}/outlinks`, undefined, undefined, options),
    related: (id: string, limit = 10, options?: RequestOptions): Promise<Schema["BrainEntry"][]> => this.#request("GET", `/entries/${encodeURIComponent(id)}/related`, undefined, {limit}, options),
  });

  async *#iterateEntries(query: EntriesListParams, options: RequestOptions): AsyncGenerator<Schema["BrainEntry"]> {
    const limit = query.limit === 0 ? 100 : (query.limit ?? 100);
    let offset = query.offset ?? 0;
    if (!Number.isSafeInteger(limit) || limit < 1 || !Number.isSafeInteger(offset) || offset < 0) throw new BrainError("invalid_pagination");
    for (let page = 0; page < 10000; page++) {
      const response = await this.entries.list({...query,limit,offset}, options);
      if (response.truncated) throw new BrainError("pagination_incomplete");
      if (response.offset !== offset || response.limit !== limit || (response.entries !== null && !Array.isArray(response.entries))) throw new BrainError("invalid_pagination");
      if (!response.entries?.length) return;
      if (response.entries.length > limit) throw new BrainError("invalid_pagination");
      for (const entry of response.entries) {
        this.#lifetime.signal.throwIfAborted(); options.signal?.throwIfAborted();
        yield entry;
      }
      offset += limit;
      if (!Number.isSafeInteger(offset)) throw new BrainError("invalid_pagination");
    }
    throw new BrainError("pagination_limit");
  }
}
