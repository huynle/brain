# Browser session recovery

API and stream requests associate unauthorized responses with the access token
that was actually sent. A late response cannot invalidate a subsequent login.
Password and OAuth refreshes share one exchange per access token; Web Locks
serialize refresh-token rotation across tabs. Logout and new login also prevent
an in-flight refresh or startup probe from overwriting the new session state.
Reminder polling waits for an authenticated or auth-disabled session.

## Regression verification

`web/src/lib/auth.test.ts` covers concurrent 401s, stale anonymous requests,
startup/refresh coordination, a login racing its initial probe, and logout during
refresh. Invalid current credentials still require sign-in.

For browser verification, build with `just web-build && just build`, then run an
isolated development server with `ENABLE_AUTH=true`, username `admin`, and the
password `mobile-login-fixture` (hash with `brain auth hash`). Use a separate data
and configuration directory. Never configure these fixture credentials in production.
From `web/`, run:

```sh
BRAIN_LOGIN_TEST_URL=http://localhost:3337 npm run test:login
BRAIN_LOGIN_TEST_URL=http://localhost:3337 BRAIN_LOGIN_BROWSER=webkit npm run test:login
```

The script rejects non-loopback URLs. It signs in using the real password
endpoint, loads the dashboard, then rejects requests bearing the old access
token to simulate expiry. Two tabs reload together and must use exactly one real
rotating refresh exchange, remain signed in, and successfully call the protected
identity endpoint. No passwords or tokens are printed. The pre-fix build returned
to the sign-in choices after duplicate refreshes were rejected; the fixed build
passes this same sequence in Chromium and WebKit.
