/*
 * attention-sw.js — hand-written Web Push handler, imported into the
 * Workbox-generated service worker via workbox.importScripts in
 * vite.config.ts. Dependency-free and minimal: it only adds a "push" and a
 * "notificationclick" listener; Workbox owns caching/precaching/routing.
 *
 * Push payload shape (JSON, best-effort — all fields optional):
 *   { id, title, body, severity, kind, project, task_id, feature_id, session_id }
 */
/* eslint-disable no-undef */

self.addEventListener("push", (event) => {
  let payload = {};
  if (event.data) {
    try {
      payload = event.data.json();
    } catch (_e) {
      // Fall back to plain text so a malformed payload still surfaces.
      payload = { title: "Brain", body: event.data.text() };
    }
  }

  const title = payload.title || "Brain notification";
  const bodyParts = [];
  if (payload.body) bodyParts.push(payload.body);
  const context = [payload.project, payload.kind].filter(Boolean).join(" · ");
  if (context) bodyParts.push(context);

  const options = {
    body: bodyParts.join("\n"),
    tag: payload.id || undefined,
    // Critical items should not be auto-dismissed by the OS.
    requireInteraction: payload.severity === "critical",
    data: {
      id: payload.id || "",
      task_id: payload.task_id || "",
      feature_id: payload.feature_id || "",
      session_id: payload.session_id || "",
      project: payload.project || "",
    },
  };

  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();

  const data = event.notification.data || {};
  const id = data.id || "";
  // Deep-link into the attention inbox; the app reads ?attention=<id> and
  // opens the inbox view on load (see pages/Dashboard.tsx).
  const target = id
    ? "/?attention=" + encodeURIComponent(id)
    : "/";

  event.waitUntil(
    self.clients
      .matchAll({ type: "window", includeUncontrolled: true })
      .then((clientList) => {
        // Focus an existing tab if one is open, navigating it to the target.
        for (const client of clientList) {
          if ("focus" in client) {
            if ("navigate" in client) {
              return client.navigate(target).then((c) => (c || client).focus());
            }
            return client.focus();
          }
        }
        if (self.clients.openWindow) {
          return self.clients.openWindow(target);
        }
        return undefined;
      }),
  );
});
