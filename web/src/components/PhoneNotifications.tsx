import { useEffect, useState } from "react";
import { api } from "../lib/api";

export function PhoneNotifications() {
  const supported = window.isSecureContext && "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
  const [enabled, setEnabled] = useState(false);
  const [reminders, setReminders] = useState(true);
  const [jobs, setJobs] = useState(true);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  async function registration() {
    const reg = await navigator.serviceWorker.getRegistration();
    if (!reg?.active) throw new Error("Reload Brain once to finish installing notification support.");
    return reg;
  }
  useEffect(() => {
    if (!supported) return;
    let live = true;
    void (async () => {
      const sub = await (await registration()).pushManager.getSubscription();
      if (!sub) return;
      const data = await api<{device: {reminders: boolean; jobs: boolean} | null}>("/api/v1/push/status", {method: "POST", body: {endpoint: sub.endpoint}});
      if (live && data.device) {setEnabled(true); setReminders(data.device.reminders); setJobs(data.device.jobs);}
    })().catch(e => {if(live) setMessage(e.message);});
    return () => {live = false;};
  }, [supported]);
  async function run(action: "enable" | "disable" | "test" | "save") {
    setBusy(true); setMessage("");
    try {
      // Permission must be requested directly from a tap, before network I/O.
      if (action === "enable" && await Notification.requestPermission() !== "granted") throw new Error("Notifications are blocked. Allow notifications for Brain in Chrome’s site settings, then retry.");
      const reg = await registration();
      let sub = await reg.pushManager.getSubscription();
      if (action === "enable" && !sub) {
        const {public_key} = await api<{public_key: string}>("/api/v1/push");
        const key = Uint8Array.from(atob(public_key.replace(/-/g,"+").replace(/_/g,"/")), c => c.charCodeAt(0));
        sub = await reg.pushManager.subscribe({userVisibleOnly: true, applicationServerKey: key});
      }
      if (!sub) throw new Error("Enable phone notifications first.");
      const path = action === "disable" ? "unsubscribe" : action === "test" ? "test" : "subscribe";
      await api(`/api/v1/push/${path}`, {method: "POST", body: {...sub.toJSON(), reminders, jobs}});
      if (action === "disable") {await sub.unsubscribe(); setEnabled(false);} else {setEnabled(true);}
      setMessage(action === "test" ? "Test sent. Check your phone’s notifications." : action === "disable" ? "Phone notifications disabled." : "Saved. Notifications can arrive with your screen off.");
    } catch(e) {setMessage(e instanceof Error ? e.message : String(e));}
    finally {setBusy(false);}
  }
  return <section style={{padding: 16, borderBottom: "1px solid var(--border)"}} aria-label="Phone notifications">
    <h3>Phone notifications</h3>
    <p>Get reminders and Assistant job updates even with your screen off. Reminder titles appear on your lock screen.</p>
    {!supported ? <p>Open Brain over HTTPS in a browser that supports push notifications.</p> : <>
      <label><input type="checkbox" checked={reminders} onChange={e => setReminders(e.target.checked)} /> Reminders</label>{" "}
      <label><input type="checkbox" checked={jobs} onChange={e => setJobs(e.target.checked)} /> Assistant job updates</label>
      <div style={{display:"flex",gap:8,flexWrap:"wrap",marginTop:12}}>
        <button disabled={busy} onClick={() => void run(enabled ? "save" : "enable")}>{enabled ? "Save notification preferences" : "Enable phone notifications"}</button>
        {enabled && <><button disabled={busy} onClick={() => void run("test")}>Send test notification</button><button disabled={busy} onClick={() => void run("disable")}>Disable on this device</button></>}
      </div>
    </>}
    <p role="status">{busy ? "Updating notifications…" : message}</p>
  </section>;
}
