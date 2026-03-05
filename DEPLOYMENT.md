# Deployment Guide

How to deploy the Brain API from local development to a publicly accessible service.

## Quick Start (Local)

No authentication needed for local development. The defaults work out of the box:

```bash
cp .env.example .env
bun run dev
```

The API is available at `http://localhost:3333`.

## Production Deployment

### Step 1: Enable Authentication

Set `ENABLE_AUTH=true` in your `.env` file. This protects all API routes (except `/health`) with token-based authentication.

```bash
ENABLE_AUTH=true
```

When auth is enabled:
- The `/api/v1/health` endpoint remains public
- All other endpoints require a valid Bearer token or `?token=` query parameter
- MCP OAuth clients use the consent flow with `OAUTH_PIN`

### Step 2: Create an API Token

Use the `brain token` CLI to create tokens:

```bash
brain token create --name cli-primary
```

This outputs a token that can only be viewed once. Save it securely.

```
API Token created:

  Name:  cli-primary
  Token: brn_xxxxxxxxxxxxxxxxxxxx

Save this token - it cannot be displayed again.
```

Manage tokens:

```bash
brain token list          # List all tokens (names and metadata only)
brain token revoke <name> # Revoke a token
```

### Step 3: Configure Clients

Clients authenticate using the API token via the `BRAIN_API_TOKEN` environment variable or the `Authorization` header.

**Environment variable (recommended):**

```bash
export BRAIN_API_TOKEN=brn_xxxxxxxxxxxxxxxxxxxx
```

**HTTP header:**

```bash
curl -H "Authorization: Bearer brn_xxxxxxxxxxxxxxxxxxxx" \
  https://brain.huynle.com/api/v1/health
```

**Query parameter (for SSE and browser use):**

```
https://brain.huynle.com/api/v1/tasks/stream?token=brn_xxxxxxxxxxxxxxxxxxxx
```

### Step 4: Enable TLS

Never expose the API over plain HTTP on a public network. Choose one of:

#### Option A: Reverse Proxy (Recommended)

Use a reverse proxy like Caddy or nginx to handle TLS termination. This is the simplest approach and provides automatic certificate management.

**Caddy** (automatic HTTPS with Let's Encrypt):

```
brain.huynle.com {
    reverse_proxy localhost:3333
}
```

**nginx:**

```nginx
server {
    listen 443 ssl;
    server_name brain.huynle.com;

    ssl_certificate /etc/letsencrypt/live/brain.huynle.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/brain.huynle.com/privkey.pem;

    location / {
        proxy_pass http://localhost:3333;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE support (required for task streaming)
        proxy_set_header Connection '';
        proxy_http_version 1.1;
        chunked_transfer_encoding off;
        proxy_buffering off;
        proxy_cache off;
    }
}
```

#### Option B: Built-in TLS

The Brain API has built-in TLS support for cases where a reverse proxy is not available.

```bash
ENABLE_TLS=true
TLS_KEY=/path/to/privkey.pem
TLS_CERT=/path/to/fullchain.pem
```

When built-in TLS is enabled, the server automatically adds `Strict-Transport-Security` headers.

### Step 5: Set CORS Origin

Restrict CORS to your domain in production. The default (`*`) allows all origins, which is only appropriate for local development.

```bash
CORS_ORIGIN=https://brain.huynle.com
```

### Step 6: Set OAuth PIN

If using MCP clients (e.g., Claude Code, OpenCode) that connect via OAuth, set a PIN for the consent page:

```bash
OAUTH_PIN=your-secure-pin
```

The PIN is required to authorize new MCP client connections.

## Runner Configuration

The task runner needs an API token when connecting to an authenticated Brain API.

**Via environment variable:**

```bash
export BRAIN_API_TOKEN=brn_xxxxxxxxxxxxxxxxxxxx
bun run src/runner/index.ts start my-project --tui
```

**Via runner YAML config** (`~/.config/brain/runner.yaml`):

```yaml
api:
  url: https://brain.huynle.com
  token: brn_xxxxxxxxxxxxxxxxxxxx
```

**Connecting to a remote Brain API:**

```bash
BRAIN_API_URL=https://brain.huynle.com \
BRAIN_API_TOKEN=brn_xxxxxxxxxxxxxxxxxxxx \
bun run src/runner/index.ts start my-project --tui
```

## Security Checklist

Before exposing the Brain API publicly, verify:

- [ ] `ENABLE_AUTH=true` is set
- [ ] API token created via `brain token create` and saved securely
- [ ] TLS enabled (reverse proxy or built-in)
- [ ] `CORS_ORIGIN` set to your specific domain
- [ ] `OAUTH_PIN` set for MCP client authorization
- [ ] Firewall configured (only port 443 exposed publicly)
- [ ] API token is not committed to version control
- [ ] `.env` is in `.gitignore`

## Environment Variable Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3333` | Server listen port |
| `HOST` | `0.0.0.0` | Server bind address |
| `LOG_LEVEL` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `BRAIN_DIR` | `~/.brain` | Brain data storage directory |
| `DEFAULT_PROJECT` | `default` | Default project ID |
| `ENABLE_AUTH` | `false` | Enable API authentication |
| `API_KEY` | — | Legacy API key (prefer `brain token create`) |
| `OAUTH_PIN` | — | PIN for MCP OAuth consent page |
| `CORS_ORIGIN` | `*` | Allowed CORS origin |
| `ENABLE_TLS` | `false` | Enable built-in TLS |
| `TLS_KEY` | — | Path to TLS private key |
| `TLS_CERT` | — | Path to TLS certificate |
| `ENABLE_TENANTS` | `false` | Enable multi-tenancy |
| `BRAIN_API_TOKEN` | — | API token for client authentication |
