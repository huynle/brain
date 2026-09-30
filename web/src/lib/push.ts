/**
 * Web Push helpers.
 *
 * `urlBase64ToUint8Array` is pure and unit-tested; the subscribe/unsubscribe
 * functions wrap the browser's PushManager and this app's API client. Kept
 * dependency-free so the pure decoder can run under `node --test`.
 */
import {
  deletePushSubscription,
  getVapidPublicKey,
  savePushSubscription,
} from "./api";
import type { PushSubscription } from "./types";

/**
 * Decode a URL-safe base64 VAPID key into the Uint8Array the PushManager
 * expects for `applicationServerKey`. URL-safe base64 replaces `+`/`/` with
 * `-`/`_` and drops `=` padding, so we restore both before decoding.
 */
export function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const normalized = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(normalized);
  // Back the array with a concrete ArrayBuffer (not ArrayBufferLike) so it
  // satisfies BufferSource where PushManager expects applicationServerKey.
  const buffer = new ArrayBuffer(raw.length);
  const out = new Uint8Array(buffer);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

/** Base64-encode an ArrayBuffer (subscription keys arrive as ArrayBuffers). */
function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary);
}

/** Extract the {endpoint, p256dh, auth} the server stores from a browser sub. */
export function extractSubscription(
  sub: globalThis.PushSubscription,
): PushSubscription {
  const p256dh = sub.getKey("p256dh");
  const auth = sub.getKey("auth");
  return {
    endpoint: sub.endpoint,
    p256dh: p256dh ? arrayBufferToBase64(p256dh) : "",
    auth: auth ? arrayBufferToBase64(auth) : "",
  };
}

export type PushEnableResult =
  | { ok: true }
  | { ok: false; reason: "unsupported" | "not-configured" | "denied" | "error"; message?: string };

/**
 * Enable browser notifications end-to-end: fetch the VAPID key, request
 * permission, subscribe via the active service worker, and register the
 * subscription with the server. Returns a discriminated result so the caller
 * can render the right message rather than throwing on the expected paths.
 */
export async function enablePushNotifications(): Promise<PushEnableResult> {
  if (
    typeof navigator === "undefined" ||
    !("serviceWorker" in navigator) ||
    typeof window === "undefined" ||
    !("PushManager" in window) ||
    typeof Notification === "undefined"
  ) {
    return { ok: false, reason: "unsupported" };
  }

  let vapid: string;
  try {
    vapid = await getVapidPublicKey();
  } catch (err) {
    return {
      ok: false,
      reason: "error",
      message: err instanceof Error ? err.message : String(err),
    };
  }
  if (!vapid) return { ok: false, reason: "not-configured" };

  const permission = await Notification.requestPermission();
  if (permission !== "granted") return { ok: false, reason: "denied" };

  try {
    const registration = await navigator.serviceWorker.ready;
    const existing = await registration.pushManager.getSubscription();
    const subscription =
      existing ??
      (await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(vapid),
      }));
    await savePushSubscription(extractSubscription(subscription));
    return { ok: true };
  } catch (err) {
    return {
      ok: false,
      reason: "error",
      message: err instanceof Error ? err.message : String(err),
    };
  }
}

/**
 * Unsubscribe locally and tell the server to drop the endpoint. Best-effort:
 * a missing subscription is treated as already-off.
 */
export async function disablePushNotifications(): Promise<PushEnableResult> {
  if (
    typeof navigator === "undefined" ||
    !("serviceWorker" in navigator)
  ) {
    return { ok: false, reason: "unsupported" };
  }
  try {
    const registration = await navigator.serviceWorker.ready;
    const subscription = await registration.pushManager.getSubscription();
    if (subscription) {
      const endpoint = subscription.endpoint;
      await subscription.unsubscribe();
      await deletePushSubscription(endpoint);
    }
    return { ok: true };
  } catch (err) {
    return {
      ok: false,
      reason: "error",
      message: err instanceof Error ? err.message : String(err),
    };
  }
}

/** Whether Web Push is even possible in this browser. */
export function pushSupported(): boolean {
  return (
    typeof navigator !== "undefined" &&
    "serviceWorker" in navigator &&
    typeof window !== "undefined" &&
    "PushManager" in window &&
    typeof Notification !== "undefined"
  );
}
