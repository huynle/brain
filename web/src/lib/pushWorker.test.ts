import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

function worker() {
  const listeners: Record<string, (event: any) => void> = {};
  const notifications: any[] = [];
  const opened: string[] = [];
  const messages: any[] = [];
  const windows: any[] = [];
  runInNewContext(readFileSync(new URL("../../public/push-sw.js", import.meta.url), "utf8"), {
    URL, self: {location: {origin: "https://brain.test"},
      addEventListener: (name: string, fn: (event: any) => void) => {listeners[name] = fn;},
      registration: {showNotification: async (title: string, options: any) => {notifications.push({title,...options});}},
      clients: {matchAll: async () => windows, openWindow: async (url: string) => {opened.push(url);}},
    },
  });
  return {listeners, notifications, opened, messages, windows};
}
test("push displays without an open tab and click opens reminders", async () => {
  const w=worker(); let work: Promise<void> = Promise.resolve();
  w.listeners.push({data: {json: () => ({title: "Reminder", body:"Call home",url:"/?notification=reminders",tag:"reminder:1"})},waitUntil:(p:Promise<void>)=>{work=p;}});
  await work; assert.equal(w.notifications[0].body,"Call home");
  w.listeners.notificationclick({notification:{...w.notifications[0],close(){}},waitUntil:(p:Promise<void>)=>{work=p;}});
  await work; assert.equal(w.opened[0],"https://brain.test/?notification=reminders");
});
test("push handles malformed payloads and refuses external navigation", async () => {
  const w=worker(); let work:Promise<void>=Promise.resolve();
  w.listeners.push({data:{json(){throw new Error("bad JSON");}},waitUntil:(p:Promise<void>)=>{work=p;}});
  await work; assert.equal(w.notifications[0].title,"Brain");
  w.listeners.notificationclick({notification:{data:{url:"https://evil.test"},close(){}},waitUntil:(p:Promise<void>)=>{work=p;}});
  await work; assert.equal(w.opened.length,0);
});
test("notification focuses an existing tab without reloading its chat", async () => {
  const w=worker();let focused=false;let work:Promise<void>=Promise.resolve();
  w.windows.push({url:"https://brain.test/",focus:async()=>{focused=true;},postMessage:(m:any)=>{w.messages.push(m);}});
  w.listeners.notificationclick({notification:{data:{url:"https://brain.test/?notification=assistant"},close(){}},waitUntil:(p:Promise<void>)=>{work=p;}});
  await work;assert.ok(focused);assert.equal(w.opened.length,0);assert.equal(w.messages[0].target,"assistant");
});
test("a reader tab does not swallow the notification navigation", async () => {
  const w=worker();let work:Promise<void>=Promise.resolve();
  w.windows.push({url:"https://brain.test/read.html?entry=demo",focus:async()=>{throw new Error("reader should remain untouched");}});
  w.listeners.notificationclick({notification:{data:{url:"https://brain.test/?notification=reminders"},close(){}},waitUntil:(p:Promise<void>)=>{work=p;}});
  await work;assert.equal(w.opened[0],"https://brain.test/?notification=reminders");
});
