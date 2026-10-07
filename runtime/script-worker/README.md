# Sealed QuickJS script worker (release source, inactive)

This directory holds the release source for the Linux script worker. It is not a
Go package and nothing in Brain builds, installs or starts it. Script execution
stays disabled: `internal/scriptexec` has no route, configuration key or caller,
and its launcher refuses the zero configuration and every non-Linux OS.

| File | Purpose |
|---|---|
| `worker.c` | Worker: framed stdin/stdout IPC, closed `brain` facade, bounded console, fixed error codes |
| `seal.h` | Shared deny-default seccomp + `RLIMIT_AS` seal; the confinement probe in `internal/scriptexec/testdata` compiles the same header |
| `build.sh` | Reproducible compiler invocation (fixed locale/TZ/`SOURCE_DATE_EPOCH`, PIE, full RELRO/BIND_NOW, non-exec stack, stack protector, FORTIFY_SOURCE=3, path-prefix map) |
| `release.json` | Pinned inputs (source, compiler image index plus per-platform manifest) and the expected output SHA-256 for each platform (`linux/arm64`, `linux/amd64`, both observed) |

## Reproducible build

1. Download `source_url` and verify its SHA-256 equals `source_sha256`
   (`b376e839…0ad2a`, QuickJS 2026-06-04). Never build from an unverified archive.
2. Use the already installed compiler image `compiler_image`. Run it offline
   (`--network=none`), with a read-only root, `--cap-drop=ALL` and a tmpfs work dir.
3. Copy the archive, `worker.c`, `seal.h` and `build.sh` into the work dir
   (`<root>`), extract with `tar --no-same-owner -xf`, then run
   `/bin/sh build.sh <root> worker.c <root>/brain-script-worker`.
4. `sha256sum` the output. It must equal `outputs["linux/<arch>"]`. If it
   doesn't, the toolchain or inputs differ: stop, don't install.

`TestQuickJSLauncherLinux` performs an independent relocated rebuild and fails
unless the result equals the committed `release.json` digest. Only platforms
with an observed build are recorded. Never add a digest that wasn't produced
by this recipe.

## Install path (documentation only; no deployment is performed)

```
install -o root -g root -m 0555 brain-script-worker /usr/libexec/brain/brain-script-worker
```

- **Owner and mode:** root-owned, mode 0555. The launcher refuses set-ID files,
  group/world-writable files, symlinked final components, and owners other than
  root or the service user.
- **Pin:** set the launcher's `WorkerSHA256` to the recorded digest. The
  launcher re-hashes the opened descriptor on every launch and execs that exact
  inode.
- **Host requirements:** Linux ≥5.9 (`Seccomp_filters` in `/proc`), with
  brain-api under an init that reaps (systemd, or `docker run --init`/tini). See
  `internal/scriptexec/README.md`, "Linux-first production launcher".

Producing this artifact is not production runtime approval. Runtime selection,
the seal policy and the launcher still need independent review, plus C–F
integration, before any activation.
