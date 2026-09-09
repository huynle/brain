# Initial Token Bootstrap

`POST /api/v1/tokens/bootstrap` creates the first `admin:*` API token only for
an unclaimed installation. Send a JSON body such as `{"name":"initial-admin"}`.
The token is returned on success (HTTP 201); keep it secure.

Bootstrap requires a loopback connection by default (IPv4 or IPv6). The server
checks the actual connection's `RemoteAddr`, not `Forwarded`, `X-Forwarded-For`,
or `X-Real-IP`. Nonlocal and malformed peer addresses receive HTTP 403.

## Containers and Proxies

If container networking prevents a loopback connection, explicitly set
`BRAIN_ALLOW_REMOTE_BOOTSTRAP=true` in the server environment for initial setup.
Only the exact value `true` enables this exception. Restrict network access
while bootstrapping, then remove the setting. It bypasses only the address
restriction, never the installation-claimed check.

A reverse proxy connecting over loopback looks local to the server. Block
`/api/v1/tokens/bootstrap` at that proxy rather than exposing first-run setup
publicly. Forwarded headers do not change this policy.

## Permanent Closure

Bootstrap returns HTTP 403 when any of these conditions applies:

- The installation has already been claimed.
- There is a non-revoked API token, regardless of its scope.
- There is an unexpired OAuth access token, including password-login sessions.
- `BRAIN_AUTH_PASSWORD_HASH` is configured with a nonempty, trimmed value.

Closure is persisted when credentials are created, when existing credentials
are found at storage startup, or when a configured password is found at server
startup. Bootstrap also checks credentials atomically when claiming an install.
Revoking or deleting tokens, letting sessions expire, removing the password
configuration, or restarting the server does not reopen a claimed installation.
Concurrent bootstrap requests can create only one initial administrator token.

The presence-only `brain:system/install_claimed` row in the existing `entry_meta`
table stores this state. It is not a markdown entry or an identity-schema table;
it is excluded from tracked-entry statistics. Preserve it across database
maintenance and migrations. Do not remove it for credential recovery: use
authenticated token management or the local token-management CLI instead.
