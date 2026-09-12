// Loaded by Workbox. Push wakes this worker even with no Brain tab open.
self.addEventListener("push", (event) => {
  let payload = {};
  try { payload = event.data?.json() || {}; } catch { /* show a safe fallback */ }
  let target;
  try { target = new URL(payload.url || "/", self.location.origin); }
  catch { target = new URL("/", self.location.origin); }
  const url = target.origin === self.location.origin ? target.href : self.location.origin;
  event.waitUntil(self.registration.showNotification(payload.title || "Brain", {
    body: payload.body || "You have a new Brain notification.",
    icon: "/icons/pwa-192x192.png",
    badge: "/icons/pwa-192x192.png",
    tag: payload.tag || "brain",
    data: { url },
  }));
});
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil((async () => {
    let target;
    try { target = new URL(event.notification.data?.url || "/", self.location.origin); }
    catch { return; }
    if (target.origin !== self.location.origin) return;
    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const client of windows) {
      const current = new URL(client.url);
      if (current.origin !== target.origin || current.pathname !== "/") continue;
      await client.focus();
      // Preserve an open chat, including any unsent draft.
      client.postMessage({ type: "brain-notification", target: target.searchParams.get("notification") });
      return;
    }
    await self.clients.openWindow(target.href);
  })());
});
