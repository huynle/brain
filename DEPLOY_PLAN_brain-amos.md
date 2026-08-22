# Deployment Runbook — brain-api → brain.huynle.com on amos

---

## ✅ SHIPPED STATE — 2026-07-13 (supersedes the 2026-06-14 plan below)

**amos is the single authoritative brain-api.** The Mac (macpro) is a client only.

### What runs where
- **amos** (`ssh amos` → amos.local / 10.1.10.217): docker compose at `/docker/amos/brain-api/`,
  container `brain-api` (image built 2026-07-12), `restart: unless-stopped`, `user: 1000:1000`,
  data bind `/home/huy/docs/brain:/data/brain`, DB `/data/brain/.brain-data/brain.db`,
  `ENABLE_AUTH=true`, embeddings via openrouter (`ready`).
- **Routing**: `brain.huynle.com` → LAN split-horizon DNS → 10.1.10.200 (proxy box, fronts 443)
  → amos Traefik (file rule `/docker/amos/traefik2/rules/app-brain-api.yml`) → `http://brain-api:3333`.
  Public DNS managed by `cloudflare-ddns-huynle`. Health: `GET /api/v1/health` (unauthenticated, 200)
  — the June note that this route 404s is obsolete; current build serves it.
- **Mac client**: `~/.config/brain/config.yaml` → `runner.brain_api_url` and `mcp.api_url` =
  `https://brain.huynle.com` (bearer token in `runner.api_token`). Local `brain api` daemon STOPPED
  and must not be restarted against a local store.

### Syncthing / store topology (changed 2026-07-13)
- The Mac↔amos Syncthing pairing for the brain store is **removed on both sides** (folder `brain`
  deleted from Mac config; Mac device removed from amos's folder). amos's store syncs with **nobody**.
- Mac's old store retired to `/Users/huy/brain.retired-20260713` (moved out of `~/docs` so the
  parent-folder sync can't duplicate it to the NAS).
- truenas keeps a **frozen** copy of the store (last synced ~2026-07-12 07:59) via the `~/docs`
  parent folder; `/brain` added to `~/docs/.stignore` on the Mac so the local removal never
  propagates as a deletion.
- **No ongoing backup of the amos store** (decision 2026-07-13: none for now). The store contains
  its own `.git` on amos. Revisit.

### Ops caveats (learned 2026-07-12/13)
1. **Out-of-band writes (rsync/scp) into the store are NOT picked up** by the running server's
   watcher — `docker restart brain-api` forces a full reindex (~12s).
2. **PATCH /api/v1/entries/{id} embeds synchronously** in the request path — often >5s, so clients
   time out and the server logs 500 (`context canceled`) even though the write PERSISTED. Verify
   with a GET before retrying.
3. Store files must stay **owned by 1000:1000**; root-owned dirs (from the pre-`user:` container era)
   break writes. Fixed 2026-07-13 via `docker exec syncthing chown -R 1000:1000 /data/brain`.
4. Never overwrite `/docker/amos/brain-api/docker-compose.yml` or `.env` when deploying (rsync
   excludes — see Phase 3 below and the 2026-06-15 incident).
5. Container panics are survived by `restart: unless-stopped` (panicked 2026-07-13 04:31Z at
   `webui.go:82`, auto-recovered — the cause of that morning's 502).

### 2026-07-12/13 incident, condensed
Mac's local brain-api crashed unsupervised; its config had been reset to defaults; an emergency
local instance was stood up while amos panicked (502). The Mac runner crash-looped 10k+ times on
expired opencode credentials, wrote ~29GB of dead sessions, filled the disk past Syncthing's 10%
free-space floor, and froze all sync — stranding the newest task edits on the Mac. Resolution:
runner stopped, 36-file delta rsynced Mac→amos + reindex, Mac cut out of sync, Mac repointed to
https://brain.huynle.com, stuck tasks (13c4tiys, 8fk4jghb, 4u06obhu) released to `pending`,
junk test-project tasks archived.

---

## HISTORICAL: 2026-06-14 plan (kept for the runbook phases, which remain the sanctioned deploy method)

**Status:** PLAN ONLY (nothing executed). Generated from live read-only discovery on amos (LAN) + local repo.
**Date:** 2026-06-14
**Target:** `amos` (homelab R720), domain `brain.huynle.com`
**Local source:** `/Users/huy/projects/brain-api/.worktrees/dev` @ `5fe83c3` (branch `dev`, `v0.9.0-398-g5fe83c3-dirty`)

---

## TL;DR — This is an UPGRADE/REDEPLOY, not a fresh deploy

brain-api is **already live and healthy** at https://brain.huynle.com (HTTP 200). The redeploy =
rsync local `dev` source → `/docker/amos/brain-api` → `docker compose up -d --build` → verify.
Persistent data (583MB) at `/home/huy/docs/brain` is a bind mount and is preserved automatically.

---

## CONFIRMED FACTS (from live discovery)

### Connectivity
- ⚠️ **amos is OFFLINE on Tailscale** ("last seen 3d ago"). Reach it via **LAN only**: `10.1.10.217` (mDNS `amos.local`).
- Working SSH (LAN + correct host key):
  ```
  ssh -o HostName=10.1.10.217 -o HostKeyAlias=amos -o ForwardX11=no amos
  ```
- User `huy` is in `docker` group (no sudo needed for docker).

### Existing deployment
- Container: `brain-api`, image `brain-api-brain-api:latest` (30.5MB, built 2026-06-12), `restart: unless-stopped`, runs as `1000:1000`, exposes `3333/tcp` (not host-published).
- Compose project dir: `/docker/amos/brain-api/` (compose file `docker-compose.yml`).
- Networks: `brain-api_default` (172.19.0.2) AND `traefik-public` (172.18.0.14).
- Health: container `GET /` → 200; `/api/v1/tasks` → 401 (auth working); public `https://brain.huynle.com/` → 200.
- RestartCount=1; logs show a prior goroutine panic then a clean restart (worth watching).

### Data persistence (CRITICAL to preserve)
- Bind mount: **`/home/huy/docs/brain` → `/data/brain`** (583 MB).
- Active DB: `/data/brain/.brain-data/brain.db` (per startup logs).
- Also contains: `brain.db` (2.2MB), `global/`, `projects/` (65 projects), `attachments/`, `.backups/` (376K, last 2026-01-20), `.git` (markdown is version-controlled).
- ⚠️ Directory is **Syncthing-synced** (`.stfolder`, sync-conflict files present) AND contains `living-brain.db.corrupted` (past corruption). Treat backups as mandatory.

### Routing & TLS (file-provider, NOT container labels)
- Traefik rule: `/docker/amos/traefik2/rules/app-brain-api.yml`
  - `Host(\`brain.huynle.com\`)` on `https` entrypoint
  - TLS `certResolver: letsencrypt-do` (DigitalOcean DNS challenge) — cert already issued
  - middleware `noauth-chain@file` (brain has its own auth)
  - service → `http://brain-api:3333`
- Traefik static config `/docker/amos/traefik2/traefik.yml`: file provider watches `/rules` (`watch: true`), docker provider `network: traefik-public`, http→https redirect, trusts Cloudflare forwarded IPs.
- ✅ Compose container labels are COMMENTED OUT — routing is entirely via the file rule. **No label changes needed.**

### DNS
- `brain.huynle.com` → A `174.16.129.45` (direct A record, DNS-only / not CF-proxied).
- Managed by `cloudflare-ddns-huynle` container (Cloudflare DNS). **No DNS change needed** for redeploy.
- (Resolved here via Tailscale MagicDNS 100.100.100.100 — public value managed by DDNS.)

### Config / secrets (live `.env` at `/docker/amos/brain-api/.env`, mode 600)
- `PORT=3333`, `HOST=0.0.0.0`, `LOG_LEVEL=info`, `DEFAULT_PROJECT=default`
- `ENABLE_AUTH=true`, `CORS_ORIGIN=https://brain.huynle.com`, `XDG_CONFIG_HOME=/data/brain/.config`
- Secrets present: `OAUTH_PIN=<set>`, `OPENROUTER_API_KEY=<set>`
- ✅ `.env` already correct for production. **Do NOT overwrite it from local checkout.**

### Build
- Dockerfile: multi-stage (node web PWA → go:embed → Go binary → alpine runtime). `ENTRYPOINT ["brain","api"]`. Self-contained; builds on amos via compose `build: .`.
- Disk on amos: 97 GB free (46% used) — ample.
- Prior rollback image tag exists: `brain-api-brain-api:pre-deploy-20260305-113339` (established rollback convention).

### Deltas vs. deployed
- Local HEAD `5fe83c3` (2026-06-14) is **newer** than deployed image (built 2026-06-12).
- Worktree dirty: `bin/brain` modified (tracked binary; irrelevant — Dockerfile rebuilds, and `.dockerignore` excludes `.git/.env/.worktrees`).

---

## FLAGGED DISCREPANCIES / GUESSES

| Item | Note |
|------|------|
| `just deploy` recipe | PARTIALLY STALE: health-checks `/api/v1/health` (returns 404 here) and `deploy-token` calls `bun run src/cli/brain.ts` (TS-era). Use manual compose flow, not `just deploy`. |
| `DEPLOYMENT.md` | STALE (TS→Go cutover doc). Claims OAUTH_PIN "not supported", but live logs show `oauth_enabled=true` and `.env` has OAUTH_PIN. OAuth IS active now. |
| Health endpoint | `/health` and `/api/v1/health` = 404. Use **`GET /`** (200) as the health signal, or `/api/v1/tasks` (401 = up+auth). GUESS: there may be a better health route — confirm. |
| DNS public value | A=174.16.129.45 seen via Tailscale MagicDNS. Assumed = WAN IP via cloudflare-ddns. Not independently confirmed against Cloudflare dashboard. |
| Tailscale offline | amos is off Tailscale. If you expected to deploy remotely (off-LAN), that path is currently broken. |
| Prior panic | Container RestartCount=1 with a goroutine stack trace in logs before clean start. Cause unknown — GUESS it self-recovered. |

---

## RUNBOOK (ordered; commands are reference — confirm before executing state-changing steps)

> Set once per shell:
> ```bash
> AMOS="ssh -o ConnectTimeout=8 -o BatchMode=yes -o HostName=10.1.10.217 -o HostKeyAlias=amos -o ForwardX11=no amos"
> SVC=/docker/amos/brain-api
> ```

### Phase 0 — Pre-flight (read-only)
```bash
$AMOS "docker ps --filter name=brain-api --format '{{.Names}} {{.Status}}'"
$AMOS "df -h / | tail -1"
curl -s -o /dev/null -w 'public: %{http_code}\n' https://brain.huynle.com/
git -C /Users/huy/projects/brain-api/.worktrees/dev status --short
git -C /Users/huy/projects/brain-api/.worktrees/dev rev-parse --short HEAD
```

### Phase 1 — Local validation (STATE-CHANGING locally; safe)
```bash
cd /Users/huy/projects/brain-api/.worktrees/dev
just check        # vet + test + lint   (or: just test && just vet)
just docker       # optional: confirm image builds locally before touching amos
```

### Phase 2 — BACKUP brain data on amos (MANDATORY — requires confirmation)
```bash
# 2a. Tag current image for rollback
$AMOS "docker tag brain-api-brain-api:latest brain-api-brain-api:pre-deploy-$(date +%Y%m%d-%H%M%S)"
$AMOS "docker images | grep brain-api"

# 2b. Snapshot the SQLite DB cleanly (sqlite backup if available) + tar the data dir
$AMOS "sqlite3 /home/huy/docs/brain/.brain-data/brain.db \".backup /home/huy/docs/brain/.backups/brain-$(date +%Y%m%d-%H%M%S).db\"" 2>/dev/null || echo "sqlite3 not present — rely on tar"
$AMOS "tar -czf /home/huy/brain-backup-$(date +%Y%m%d-%H%M%S).tar.gz \
       --exclude='.stfolder' -C /home/huy/docs brain"
$AMOS "ls -lh /home/huy/brain-backup-*.tar.gz | tail -1"
```
> ⚠️ Optional but recommended: pause Syncthing for this folder during deploy to avoid sync-conflict
> races on the DB (`syncthing` container UI :8384), then resume after verify. CONFIRM with user.

### Phase 3 — Sync source to amos (requires confirmation)
> Preserve remote deployment-owned files: `docker-compose.yml`, `.env`, and the data dir is OUTSIDE
> the service dir so it is never touched. Export only committed HEAD; dry-run first.
```bash
tmpdir="$(mktemp -d)"
git -C /Users/huy/projects/brain-api/.worktrees/dev archive --format=tar HEAD | tar -xf - -C "$tmpdir"

# DRY RUN — review the change + delete list before doing it for real
rsync -azn --itemize-changes --delete \
  -e "ssh -o HostName=10.1.10.217 -o HostKeyAlias=amos -o ForwardX11=no" \
  --exclude='/docker-compose.yml' --exclude='/docker-compose.yaml' \
  --exclude='/compose.yml' --exclude='/compose.yaml' \
  --exclude='/.env' --exclude='/.env.*' \
  --exclude='/.git' --exclude='/node_modules/***' \
  "$tmpdir/" amos:/docker/amos/brain-api/
```
> Inspect dry-run output. If anything deployment-owned would be deleted, add an `--exclude` for it.
> Then re-run WITHOUT `-n`. (Never use `--delete-excluded`.)

### Phase 4 — Build & restart (requires confirmation)
```bash
$AMOS "cd $SVC && docker compose up -d --build"
$AMOS "cd $SVC && docker compose ps"
```

### Phase 5 — Verify
```bash
# Startup logs: expect 'indexing complete' + 'starting brain-api ... auth_enabled=true'
$AMOS "docker logs brain-api --tail 30"

# Container-level health (port not host-published; use container IP)
BIP=$($AMOS "docker inspect brain-api --format '{{(index .NetworkSettings.Networks \"traefik-public\").IPAddress}}'")
$AMOS "curl -s -m 5 -o /dev/null -w 'container / -> %{http_code}\n' http://$BIP:3333/"
$AMOS "curl -s -m 5 -o /dev/null -w 'container /api/v1/tasks -> %{http_code}\n' http://$BIP:3333/api/v1/tasks"  # expect 401

# Public end-to-end (from laptop)
curl -s -o /dev/null -w 'public https -> %{http_code}\n' https://brain.huynle.com/   # expect 200

# Confirm entry count survived (data integrity)
$AMOS "docker exec brain-api sh -c 'wc -l < /dev/null'"  # placeholder; prefer an authed API call or DB row count
$AMOS "sqlite3 /home/huy/docs/brain/.brain-data/brain.db 'select count(*) from notes;'" 2>/dev/null || echo "verify via API"
```

### Phase 6 — Rollback (if verify fails)
```bash
# Revert to the pre-deploy image tag from Phase 2a
$AMOS "cd $SVC && docker compose down"
# Edit compose to 'image: brain-api-brain-api:pre-deploy-<ts>' (remove build:) OR:
$AMOS "docker tag brain-api-brain-api:pre-deploy-<ts> brain-api-brain-api:latest && cd $SVC && docker compose up -d"
# If data corrupted: stop container, restore tarball:
#   $AMOS "cd /home/huy/docs && mv brain brain.bad && tar -xzf /home/huy/brain-backup-<ts>.tar.gz"
$AMOS "docker logs brain-api --tail 20"
curl -s -o /dev/null -w 'public: %{http_code}\n' https://brain.huynle.com/
```

---

## OPEN QUESTIONS (need user decision before execution)

1. **Tailscale**: amos is offline on TS. OK to proceed over LAN only? Or bring TS back first?
2. **Syncthing**: Pause sync on the brain folder during deploy to avoid DB sync-conflicts? (recommended)
3. **Scope**: Deploy committed `HEAD` (5fe83c3) only — OK? The dirty `bin/brain` is excluded (good).
4. **Health route**: Is there a canonical health endpoint other than `/` (which returns 200)? `/health` is 404.
5. **Prior panic**: Investigate the goroutine panic (RestartCount=1) before redeploy, or proceed?
6. **Build location**: Confirm build-on-amos (current convention) vs. build-locally-and-transfer-image.
