/**
 * Cross-runtime Bun compatibility shim.
 *
 * Under Bun runtime, re-exports the real Bun global unchanged.
 * Under Node runtime, provides Node-backed implementations of the Bun
 * APIs used across this repo:
 *
 *   Bun.file(path)                -> BunFile with .text(), .arrayBuffer(),
 *                                     .stream(), .exists(), .size, .name
 *   Bun.write(path, data)         -> writes string / Buffer / ArrayBuffer
 *   Bun.spawn(argv, opts)         -> Subprocess with .stdout/.stderr streams,
 *                                     .exited, .kill(), .pid
 *   Bun.which(cmd)                -> resolves to absolute path or null
 *   Bun.env                       -> process.env
 *   Bun.stripANSI(s)              -> strips ANSI escape sequences
 *   Bun.inspect(v)                -> util.inspect
 *   Bun.CryptoHasher              -> minimal wrapper over node:crypto Hash
 *   Bun.sleepSync(ms)             -> blocking sleep via Atomics.wait
 *   Bun.hash(v)                   -> Wyhash-ish 64-bit surrogate via SHA-256 slice
 *   Bun.serve(opts)               -> minimal HTTP server (fetch handler) over node:http
 *
 * Usage:
 *   import { Bun } from "<relative>/lib/bun-shim.ts"
 *   const contents = await Bun.file("./x.txt").text()
 */

// If running under real Bun, just re-export the global as-is.
declare const globalThis: {
  Bun?: unknown
  process: NodeJS.Process
} & Record<string, unknown>

const RealBun: unknown =
  typeof (globalThis as { Bun?: unknown }).Bun !== "undefined"
    ? (globalThis as { Bun: unknown }).Bun
    : undefined

// ============================================================================
// Node implementations
// ============================================================================
import { spawn as nodeSpawn } from "node:child_process"
import type { ChildProcessByStdio, SpawnOptions } from "node:child_process"
import { readFile, writeFile, stat, access } from "node:fs/promises"
import { createReadStream, constants as fsConstants } from "node:fs"
import { basename, resolve as pathResolve } from "node:path"
import { createServer, type IncomingMessage, type ServerResponse } from "node:http"
import { createHash, type Hash } from "node:crypto"
import { inspect as utilInspect } from "node:util"
import { Readable } from "node:stream"

// Bun.file
class NodeBunFile {
  readonly path: string
  readonly name: string
  constructor(path: string) {
    this.path = pathResolve(path)
    this.name = basename(this.path)
  }
  async text(): Promise<string> {
    return await readFile(this.path, "utf8")
  }
  async arrayBuffer(): Promise<ArrayBuffer> {
    const buf = await readFile(this.path)
    const ab = new ArrayBuffer(buf.byteLength)
    new Uint8Array(ab).set(buf)
    return ab
  }
  async bytes(): Promise<Uint8Array> {
    return new Uint8Array(await readFile(this.path))
  }
  async json(): Promise<unknown> {
    return JSON.parse(await this.text())
  }
  stream(): ReadableStream<Uint8Array> {
    const nodeStream = createReadStream(this.path)
    return Readable.toWeb(nodeStream) as unknown as ReadableStream<Uint8Array>
  }
  async exists(): Promise<boolean> {
    try {
      await access(this.path, fsConstants.F_OK)
      return true
    } catch {
      return false
    }
  }
  get size(): Promise<number> {
    return stat(this.path).then((s) => s.size)
  }
}

function nodeBunFile(path: string): NodeBunFile {
  return new NodeBunFile(path)
}

// Bun.write
async function nodeBunWrite(
  dest: string | NodeBunFile,
  data: string | ArrayBuffer | ArrayBufferView | Blob | NodeBunFile,
): Promise<number> {
  const destPath = typeof dest === "string" ? dest : dest.path
  let payload: string | Uint8Array
  if (typeof data === "string") payload = data
  else if (data instanceof NodeBunFile) payload = new Uint8Array(await data.arrayBuffer())
  else if (data instanceof ArrayBuffer) payload = new Uint8Array(data)
  else if (ArrayBuffer.isView(data))
    payload = new Uint8Array(data.buffer, data.byteOffset, data.byteLength)
  else if (data instanceof Blob) payload = new Uint8Array(await data.arrayBuffer())
  else throw new TypeError("Unsupported data type for Bun.write")
  await writeFile(destPath, payload)
  return typeof payload === "string" ? Buffer.byteLength(payload, "utf8") : payload.byteLength
}

// Bun.spawn — the trickiest to keep API-compatible.
// Callers use: Bun.spawn(argv, { stdout: "pipe" | "inherit" | "ignore", stderr, cwd, env })
// and access .stdout (ReadableStream), .stderr, .stdin, .exited (Promise<number>),
// .kill(sig?), .pid
export interface SpawnBunOptions {
  cwd?: string
  env?: Record<string, string | undefined>
  stdout?: "pipe" | "inherit" | "ignore"
  stderr?: "pipe" | "inherit" | "ignore"
  stdin?: "pipe" | "inherit" | "ignore"
  onExit?: (subprocess: NodeBunSubprocess, exitCode: number | null, signalCode: string | null, err: Error | null) => void
}

class NodeBunSubprocess {
  readonly pid: number
  readonly stdout: ReadableStream<Uint8Array> | null
  readonly stderr: ReadableStream<Uint8Array> | null
  readonly stdin: WritableStream<Uint8Array> | null
  readonly exited: Promise<number>
  private child: ChildProcessByStdio<unknown, unknown, unknown>
  private _exitCode: number | null = null
  private _signalCode: string | null = null

  constructor(argv: string[], opts: SpawnBunOptions = {}) {
    const stdio: SpawnOptions["stdio"] = [
      mapStdio(opts.stdin ?? "ignore"),
      mapStdio(opts.stdout ?? "pipe"),
      mapStdio(opts.stderr ?? "pipe"),
    ]
    const child = nodeSpawn(argv[0], argv.slice(1), {
      cwd: opts.cwd,
      env: opts.env as NodeJS.ProcessEnv | undefined,
      stdio,
    }) as ChildProcessByStdio<unknown, unknown, unknown>

    this.child = child
    this.pid = child.pid ?? -1

    this.stdout = child.stdout
      ? (Readable.toWeb(child.stdout as unknown as Readable) as unknown as ReadableStream<Uint8Array>)
      : null
    this.stderr = child.stderr
      ? (Readable.toWeb(child.stderr as unknown as Readable) as unknown as ReadableStream<Uint8Array>)
      : null
    this.stdin = null // Callers in this repo don't write to stdin; expose if needed.

    this.exited = new Promise<number>((resolve, reject) => {
      child.once("error", (err) => {
        opts.onExit?.(this, null, null, err)
        reject(err)
      })
      child.once("close", (code, signal) => {
        this._exitCode = code
        this._signalCode = signal
        opts.onExit?.(this, code, signal, null)
        resolve(code ?? 0)
      })
    })
  }

  get exitCode(): number | null {
    return this._exitCode
  }
  get signalCode(): string | null {
    return this._signalCode
  }
  get killed(): boolean {
    return this.child.killed
  }

  kill(signal?: number | NodeJS.Signals): void {
    this.child.kill(signal ?? "SIGTERM")
  }

  ref(): void {
    this.child.ref?.()
  }
  unref(): void {
    this.child.unref?.()
  }
}

function mapStdio(s: "pipe" | "inherit" | "ignore"): "pipe" | "inherit" | "ignore" {
  return s === "pipe" ? "pipe" : s === "inherit" ? "inherit" : "ignore"
}

function nodeBunSpawn(argv: string[], opts?: SpawnBunOptions): NodeBunSubprocess {
  return new NodeBunSubprocess(argv, opts)
}

// Bun.which
async function nodeBunWhich(cmd: string): Promise<string | null> {
  const PATH = process.env.PATH ?? ""
  for (const dir of PATH.split(":")) {
    if (!dir) continue
    const candidate = `${dir}/${cmd}`
    try {
      await access(candidate, fsConstants.X_OK)
      return candidate
    } catch {
      /* not there */
    }
  }
  return null
}

// Bun.stripANSI
const ANSI_REGEX = /[\u001B\u009B][[\]()#;?]*(?:(?:(?:(?:;[-a-zA-Z\d\/#&.:=?%@~_]+)*|[a-zA-Z\d]+(?:;[-a-zA-Z\d\/#&.:=?%@~_]*)*)?\u0007)|(?:(?:\d{1,4}(?:;\d{0,4})*)?[\dA-PR-TZcf-nq-uy=><~]))/g
function nodeStripANSI(s: string): string {
  return s.replace(ANSI_REGEX, "")
}

// Bun.CryptoHasher (subset)
class NodeCryptoHasher {
  private hash: Hash
  constructor(algorithm: string) {
    this.hash = createHash(algorithm)
  }
  update(input: string | ArrayBuffer | ArrayBufferView): this {
    if (typeof input === "string") this.hash.update(input)
    else if (input instanceof ArrayBuffer) this.hash.update(Buffer.from(input))
    else this.hash.update(Buffer.from(input.buffer, input.byteOffset, input.byteLength))
    return this
  }
  digest(encoding: "hex" | "base64" | "base64url"): string
  digest(): Buffer
  digest(encoding?: string): string | Buffer {
    return encoding ? this.hash.digest(encoding as BufferEncoding) : this.hash.digest()
  }
}

// Bun.sleepSync
function nodeSleepSync(ms: number): void {
  const sab = new SharedArrayBuffer(4)
  const view = new Int32Array(sab)
  Atomics.wait(view, 0, 0, ms)
}

// Bun.hash (64-bit surrogate — good enough for cache keys, NOT crypto-secure)
function nodeHash(input: string | ArrayBuffer | ArrayBufferView): bigint {
  const h = createHash("sha256")
  if (typeof input === "string") h.update(input)
  else if (input instanceof ArrayBuffer) h.update(Buffer.from(input))
  else h.update(Buffer.from(input.buffer, input.byteOffset, input.byteLength))
  const digest = h.digest()
  return digest.readBigUInt64BE(0)
}

// Bun.serve (minimal — supports { port, hostname, fetch, error })
interface BunServeOptions {
  port?: number
  hostname?: string
  fetch: (req: Request) => Promise<Response> | Response
  error?: (err: Error) => Response | Promise<Response>
}

class NodeBunServer {
  readonly port: number
  readonly hostname: string
  private server: ReturnType<typeof createServer>
  constructor(opts: BunServeOptions) {
    this.hostname = opts.hostname ?? "127.0.0.1"
    this.server = createServer(async (req, res) => {
      try {
        const url = `http://${req.headers.host ?? this.hostname}${req.url ?? "/"}`
        const headers = new Headers()
        for (const [k, v] of Object.entries(req.headers)) {
          if (typeof v === "string") headers.set(k, v)
          else if (Array.isArray(v)) v.forEach((val) => headers.append(k, val))
        }
        let body: BodyInit | undefined
        if (req.method !== "GET" && req.method !== "HEAD") {
          body = await new Promise<Buffer>((res2, rej2) => {
            const chunks: Buffer[] = []
            req.on("data", (c: Buffer) => chunks.push(c))
            req.on("end", () => res2(Buffer.concat(chunks)))
            req.on("error", rej2)
          }) as unknown as BodyInit
        }
        const request = new Request(url, { method: req.method, headers, body })
        const response = await opts.fetch(request)
        res.writeHead(response.status, Object.fromEntries(response.headers.entries()))
        if (response.body) {
          const reader = response.body.getReader()
          for (;;) {
            const { done, value } = await reader.read()
            if (done) break
            res.write(value)
          }
        }
        res.end()
      } catch (err) {
        try {
          const errRes = opts.error
            ? await opts.error(err instanceof Error ? err : new Error(String(err)))
            : new Response("Internal Server Error", { status: 500 })
          res.writeHead(errRes.status)
          res.end(await errRes.text())
        } catch {
          if (!res.headersSent) res.writeHead(500)
          res.end()
        }
      }
    })
    this.server.listen(opts.port ?? 0, this.hostname)
    const addr = this.server.address()
    this.port = typeof addr === "object" && addr ? addr.port : opts.port ?? 0
  }
  stop(): void {
    this.server.close()
  }
  get url(): URL {
    return new URL(`http://${this.hostname}:${this.port}`)
  }
}

function nodeBunServe(opts: BunServeOptions): NodeBunServer {
  return new NodeBunServer(opts)
}

// ============================================================================
// Node-backed Bun object (when RealBun is undefined)
// ============================================================================
const NodeBun = {
  file: nodeBunFile,
  write: nodeBunWrite,
  spawn: nodeBunSpawn,
  which: nodeBunWhich,
  env: process.env,
  stripANSI: nodeStripANSI,
  inspect: utilInspect,
  CryptoHasher: NodeCryptoHasher,
  sleepSync: nodeSleepSync,
  hash: nodeHash,
  serve: nodeBunServe,
  // Bun.$ template — delegate to our shell shim in ../lib/shell.ts by re-import
  // (kept out here to avoid a circular reliance; users of Bun.$ should
  //  import { $ } from "../lib/shell.ts" directly instead)
}

// Export the resolved Bun-like object.
// Under real Bun, this is the native global; under Node, our shim.
export const Bun: typeof NodeBun & { $?: unknown } =
  (RealBun as typeof NodeBun & { $?: unknown } | undefined) ?? NodeBun
