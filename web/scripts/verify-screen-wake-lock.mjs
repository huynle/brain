import {chromium, expect} from '@playwright/test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
const browser=await chromium.launch();
try {
 const context=await browser.newContext({viewport:{width:390,height:844},serviceWorkers:'block'});
 if(process.env.BRAIN_TEST_TOKEN_FILE) {
  const {token}=JSON.parse(fs.readFileSync(process.env.BRAIN_TEST_TOKEN_FILE,'utf8'));
  await context.addInitScript(token=>{localStorage.setItem('brain.access_token',token);localStorage.setItem('brain.auth_mode','manual');},token);
 }
 await context.addInitScript(()=>{
  // Hold microphone startup so lifecycle checks do not need a model or audio provider.
  navigator.mediaDevices.getUserMedia=()=>new Promise(()=>{});
  window.AudioContext=class {state='running';resume=async()=>{};close=async()=>{this.state='closed';};};
  window.mode='success';window.releases=0;
  const wakeLock={request:()=>{
   if(window.mode==='denied')return Promise.reject(new Error('denied'));
   const lock=new EventTarget();lock.released=false;
   lock.release=async()=>{if(!lock.released){lock.released=true;window.releases++;lock.dispatchEvent(new Event('release'));}};
   window.lock=lock;
   if(window.mode==='pending')return new Promise(resolve=>{window.resolveLock=()=>resolve(lock);});
   return Promise.resolve(lock);
  }};
  Object.defineProperty(navigator,'wakeLock',{configurable:true,get:()=>window.mode==='unsupported'?undefined:wakeLock});
 });
 const page=await context.newPage();
 await page.goto(process.env.BRAIN_PREVIEW_URL||'http://localhost:3340');
 await page.getByRole('button',{name:'Assistant',exact:true}).click();
 const start=()=>page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 const stop=()=>page.getByRole('button',{name:'End hands-free',exact:true}).click();
 await start();await expect(page.getByText('Screen kept awake',{exact:true})).toBeVisible();
 await page.evaluate(()=>window.lock.release());
 await expect(page.getByText('Screen may sleep — keep Brain visible.',{exact:true})).toBeVisible();
 await stop();assert.equal(await page.evaluate(()=>window.releases),1);
 for(const mode of ['denied','unsupported']) {
  await page.evaluate(mode=>window.mode=mode,mode);await start();
  await expect(page.getByText('Screen may sleep — keep Brain visible.',{exact:true})).toBeVisible();
  await stop();
 }
 await page.evaluate(()=>window.mode='pending');await start();
 await expect.poll(()=>page.evaluate(()=>typeof window.resolveLock)).toBe('function');
 await stop();await page.evaluate(()=>window.resolveLock());
 await expect.poll(()=>page.evaluate(()=>window.releases)).toBe(2);
 await expect(page.getByText('Screen kept awake',{exact:true})).toHaveCount(0);
 await page.evaluate(()=>window.mode='success');await start();
 await expect(page.getByText('Screen kept awake',{exact:true})).toBeVisible();
 await stop();await expect.poll(()=>page.evaluate(()=>window.releases)).toBe(3);
 console.log('PASS wake lock: acquired, system release visible, denied/unsupported nonfatal, pending acquisition released after stop, explicit stop releases');
} finally {await browser.close();}
