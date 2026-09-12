# Phone notifications

In Brain Settings, select **Enable phone notifications**, grant the browser's
permission, then select **Send test notification**. Reminders and Assistant job
updates can be enabled separately. These preferences apply to this browser/device.
Android Chrome can receive Web Push while Brain is closed or the screen is off;
Chrome and Android must allow site notifications. Battery restrictions, lack of
network access, or force-stopping Chrome can delay or prevent delivery.

Reminder titles appear on the lock screen. Assistant alerts contain only the job
state, not its private result. Tapping opens the reminder centre or Assistant;
an existing tab is focused without reloading its draft. Signing out unsubscribes
the device. Signing back in requires enabling notifications again.

## Server behavior

This initial implementation is for the configured local tenant and full-access
(`admin:*`) accounts, since reminder notifications cover the whole installation.
Assistant job notifications are additionally matched to the job's owning identity.
It does not broadcast one account's job results to other accounts.

Subscriptions, VAPID keys, and the retry queue live in
`.brain-data/push/notifications.db`, separate from the content index. The private
directory/file use 0700/0600 permissions. Preserve this database in backups; losing
the keys requires devices to unsubscribe and subscribe again. No external sender
account or key setup is required. Subscription endpoints are restricted to known
browser push services and HTTP redirects are refused.

Every ten seconds the API reads reminder firing state and recent terminal job
summaries (completed, failed, paused, cancelled). No note bodies or job credentials
are loaded by the job scan. Devices receive events fired after they subscribed,
within the last 24 hours. The persisted queue deduplicates by occurrence/revision
and device, retries temporary failures with backoff, and retires expired endpoints.
Events survive API restarts through their source records and the queue. Delivery
is at least once: a crash after the push provider accepts a request but before the
receipt commits can retry it; notification tags collapse matching visible alerts.
Terminal delivery records are retained for two days. Completed jobs still have
their full details in Brain's conversation inbox.

Routes (authenticated, admin scope):

- `GET /api/v1/push`: public VAPID key.
- `POST /api/v1/push/subscribe`: browser subscription plus `reminders` and `jobs`.
- `POST /api/v1/push/status`: lookup this owner's subscription by `endpoint`.
- `POST /api/v1/push/unsubscribe`: remove this owner's device and queued alerts.
- `POST /api/v1/push/test`: send a test to this owner's subscribed `endpoint`.

## Verification

Go tests cover encrypted requests, retry/backoff, expiry, durable deduplication,
owner isolation, preference filtering, and a seeded reminder/job collection cycle.
Worker tests cover push display, malformed payloads, safe navigation, and preserving
existing tabs. `web/scripts/verify-phone-notifications.mjs` runs against an isolated
local preview with seeded project/task data; it verifies the mobile controls,
preferences after reload, unsubscribe, worker activation, and reminder navigation.
That browser test simulates push-service registration. It does not prove Android
lock-screen delivery: verify that on a real opted-in phone with a dated reminder.
