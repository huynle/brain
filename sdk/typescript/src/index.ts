export type { components, paths, operations } from "./schema.gen.js";
import type { components, operations } from "./schema.gen.js";
type Schema = components["schemas"];

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
export type EntriesListParams = NonNullable<operations["entries.list"]["parameters"]["query"]>;

export class BrainError extends Error {
  constructor(
    readonly code: string,
    readonly status = 0,
    readonly requestId = "",
    readonly serverMessage = "",
    readonly retryable = false,
  ) {
    super(`brain: ${code} (HTTP ${status})`);
    this.name = "BrainError";
  }
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

  async #request<T>(method: string, path: string, body?: unknown, query?: object, options: RequestOptions = {}, expectBody = true, binary = false): Promise<T> {
    const signals = [this.#lifetime.signal, AbortSignal.timeout(this.#config.timeoutMs)];
    if (options.signal) signals.push(options.signal);
    const signal = AbortSignal.any(signals);
    signal.throwIfAborted();
    const q = new URLSearchParams();
    for (const [key, value] of Object.entries(query ?? {})) if (value !== undefined) q.set(key, String(value));
    const suffix = q.size ? `?${q}` : "";
    const headers = new Headers({Accept: "application/json"});
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
    const reader = response.body?.getReader();
    const chunks: Uint8Array[] = [];
    let total = 0;
    try {
      if (reader) while (true) {
        const {done, value} = await reader.read();
        signal.throwIfAborted();
        if (done) break;
        total += value.byteLength;
        if (total > this.#config.maxResponseBytes) {
          await reader.cancel();
          throw new BrainError("response_too_large", response.status, requestId);
        }
        chunks.push(value);
      }
    } finally { reader?.releaseLock(); }
    signal.throwIfAborted();
    const data = new Uint8Array(total);
    let offset = 0;
    for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.byteLength; }
    const text = new TextDecoder().decode(data);
    if (!response.ok) {
      let wire: {code?: unknown; message?: unknown} = {};
      try { const parsed: unknown = JSON.parse(text); if (parsed && typeof parsed === "object") wire = parsed; } catch { /* Legacy non-JSON errors retain their HTTP code. */ }
      const codes: Record<number, string> = {400:"invalid_request",401:"unauthorized",403:"forbidden",404:"not_found",409:"conflict",429:"rate_limited",501:"unsupported_operation",503:"unavailable"};
      throw new BrainError(typeof wire.code === "string" ? wire.code : codes[response.status] ?? "http_error", response.status, requestId,
        typeof wire.message === "string" ? wire.message : "", [429,503].includes(response.status));
    }
    if (!expectBody) return undefined as T;
    if (binary) return data as T;
    try { return JSON.parse(text) as T; }
    catch { throw new BrainError("invalid_response", response.status, requestId); }
  }

  health(options?: RequestOptions): Promise<Schema["HealthResponse"]> { return this.#request("GET", "/health", undefined, undefined, options); }
  search(request: Schema["SearchRequest"], options?: RequestOptions): Promise<Schema["SearchResponse"]> { return this.#request("POST", "/search", request, undefined, options); }

  readonly entries = Object.freeze({
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
    list: (project: string, options?: RequestOptions): Promise<Schema["TaskListResponse"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}`, undefined, undefined, options),
    get: (project: string, id: string, options?: RequestOptions): Promise<Schema["ResolvedTask"]> => this.#request("GET", `/tasks/${encodeURIComponent(project)}/${encodeURIComponent(id)}`, undefined, undefined, options),
  });
  readonly sections = Object.freeze({
    list: (id: string, options?: RequestOptions): Promise<Schema["SectionsResponse"]> => this.#request("GET", `/entries/${encodeURIComponent(id)}/sections`, undefined, undefined, options),
    get: (id: string, title: string, includeSubsections = false, options?: RequestOptions): Promise<Schema["SectionContentResponse"]> => this.#request("GET", `/entries/${encodeURIComponent(id)}/sections/${encodeURIComponent(title)}`, undefined, {includeSubsections}, options),
  });
  readonly graph = Object.freeze({
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
