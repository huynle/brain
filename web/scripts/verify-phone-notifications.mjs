// Exercise the production PWA and real subscription API against an isolated
// local server. Browser push-service registration is simulated; actual Android
// delivery must be checked on a opted-in phone.
import {chromium, expect} from '@playwright/test';
import {createECDH} from 'node:crypto';
import assert from 'node:assert/strict';
const origin=process.env.BRAIN_PUSH_TEST_URL || 'http://127.0.0.1:3346';
assert.ok(['localhost','127.0.0.1'].includes(new URL(origin).hostname));
const key=createECDH('prime256v1');key.generateKeys();
const device={endpoint:'https://fcm.googleapis.com/fcm/send/synthetic-ui-test',keys:{p256dh:key.getPublicKey().toString('base64url'),auth:Buffer.alloc(16).toString('base64url')}};
const browser=await chromium.launch();
try {
 const context=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true,permissions:['notifications']});
 await context.grantPermissions(['notifications'], {origin});
 await context.addInitScript(d=>{
   let active=localStorage.getItem('synthetic-push-enabled')==='yes';
   const sub={endpoint:d.endpoint,toJSON:()=>d,unsubscribe:async()=>{active=false;localStorage.removeItem('synthetic-push-enabled');return true;}};
   PushManager.prototype.getSubscription=async()=>active?sub:null;
   PushManager.prototype.subscribe=async()=>{active=true;localStorage.setItem('synthetic-push-enabled','yes');return sub;};
 },device);
 const page=await context.newPage();
 await page.goto(origin);
 await expect(page.getByRole('button',{name:'Assistant',exact:true})).toBeVisible({timeout:30000});
 await page.evaluate(()=>navigator.serviceWorker.ready.then(()=>true));
 const settings=async()=>{await page.keyboard.press('Control+k');await page.getByText('Open settings',{exact:true}).click();};
 await settings();
 await page.getByRole('button',{name:'Enable phone notifications',exact:true}).click();
 await expect(page.getByRole('button',{name:'Send test notification',exact:true})).toBeVisible();
 await page.getByLabel('Assistant job updates',{exact:true}).uncheck();
 await page.getByRole('button',{name:'Save notification preferences',exact:true}).click();
 await expect(page.getByText('Saved. Notifications can arrive with your screen off.',{exact:true})).toBeVisible();
 await page.screenshot({path:'/tmp/brain-phone-notifications.png',fullPage:true});
 // The real built worker is active. Headless Chrome cannot prove Android OS
 // notification display; the worker's push/click handlers have separate tests.
 assert.ok(await page.evaluate(async()=>Boolean((await navigator.serviceWorker.ready).active)));
 await page.reload();await expect(page.getByRole('button',{name:'Assistant',exact:true})).toBeVisible();await settings();
 await expect(page.getByLabel('Assistant job updates',{exact:true})).not.toBeChecked();
 await page.getByRole('button',{name:'Disable on this device',exact:true}).click();
 await expect(page.getByRole('button',{name:'Enable phone notifications',exact:true})).toBeVisible();
 await page.goto(origin+'/?notification=reminders');
 await expect(page.locator('.reminders-leaf')).toBeVisible({timeout:30000});
 console.log('PASS mobile enable, preference persistence, disable, real worker activation, and reminder deep link. Push-service registration simulated; OS delivery needs a phone.');
 await context.close();
} finally {await browser.close();}
