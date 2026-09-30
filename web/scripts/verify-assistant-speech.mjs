import { chromium, expect } from '@playwright/test';
import assert from 'node:assert/strict';
const origin=process.env.BRAIN_MOBILE_TEST_URL??'http://localhost:3340';
assert.ok(['localhost','127.0.0.1'].includes(new URL(origin).hostname));
const browser=await chromium.launch();
try {
 const context=await browser.newContext({viewport:{width:390,height:844},serviceWorkers:'block'});
 await context.addInitScript(()=>{
  localStorage.setItem('panes-v2:assistant-chat:v1',JSON.stringify({version:1,state:{turns:[{role:'assistant',content:'Hello from Brain.',tools:[]}],history:[]}}));
  window.Audio=class {
   constructor(url){this.url=url;window.lastAudio=this;}
   async play(){window.audioPlayed=(window.audioPlayed||0)+1;}
   pause(){window.audioPaused=(window.audioPaused||0)+1;}
  };
 });
 const page=await context.newPage();
 let requests=0, fail=false, hold=false, release;
 await page.route('**/api/v1/assistant/speech',async route=>{
  requests++;
  if (hold) await new Promise(resolve=>{release=resolve;});
  if(fail) return route.fulfill({status:502,body:'Speech unavailable'});
  await route.fulfill({status:200,contentType:'audio/mpeg',body:'ID3fixture'});
 });
 await page.route('**/api/v1/assistant/chat/stream',route=>route.fulfill({contentType:'application/x-ndjson',body:'{"type":"delta","delta":"A new spoken reply."}\n{"type":"done","reply":"A new spoken reply."}\n'}));
 await page.goto(origin);
 await page.getByRole('button',{name:'Assistant',exact:true}).click({timeout:30000});
 await page.getByRole('button',{name:'Read aloud',exact:true}).click();
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Stop audio',exact:true}).click();
 await page.getByRole('button',{name:'Read aloud',exact:true}).click();
 assert.equal(requests,1,'replay should reuse audio without another billable request');
 await page.getByRole('button',{name:'Close assistant',exact:true}).click();
 assert(await page.evaluate(()=>window.audioPaused>0));
 await page.getByRole('button',{name:'Assistant',exact:true}).click();
 await page.getByRole('checkbox',{name:'Spoken replies'}).check();
 await page.getByRole('textbox',{name:'Message to Assistant'}).fill('Respond with a short sentence.');
 await page.getByRole('button',{name:/^Send/}).click();
 await expect(page.getByText('A new spoken reply.',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Stop audio',exact:true}).click();
 const playedBefore = await page.evaluate(()=>window.audioPlayed);
 hold=true;
 await page.getByRole('button',{name:'Read aloud',exact:true}).first().click();
 await expect(page.getByRole('button',{name:'Cancel audio',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Cancel audio',exact:true}).click();
 hold=false;
 release?.();
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toHaveCount(0);
 assert.equal(await page.evaluate(()=>window.audioPlayed),playedBefore);
 fail=true;
 await page.getByRole('button',{name:'Read aloud',exact:true}).first().click();
 await expect(page.getByText(/Speech is unavailable/)).toBeVisible();
 await expect(page.getByText('A new spoken reply.',{exact:true})).toBeVisible();
 console.log('PASS manual speech, cached replay, stop, close cleanup, automatic reply and provider failure retain chat');
} finally {await browser.close();}
