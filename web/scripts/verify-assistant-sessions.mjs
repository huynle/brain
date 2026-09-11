import { chromium, expect } from '@playwright/test';
import assert from 'node:assert/strict';
const browser=await chromium.launch();
try {
 const context=await browser.newContext({viewport:{width:390,height:844},serviceWorkers:'block'});
 await context.addInitScript(()=>{
  window.started=0;window.aborted=0;window.micLevel=0;
  navigator.mediaDevices.getUserMedia=async()=>({getTracks:()=>[{stop(){}}],getAudioTracks:()=>[{getSettings:()=>({echoCancellation:true})}]});
  window.AudioContext=class {
    state='running'; async resume(){} async close(){this.state='closed';}
    createMediaStreamSource(){return {connect(){}};}
    createAnalyser(){return {fftSize:1024,getFloatTimeDomainData(buffer){buffer.fill(window.micLevel);}};}
  };
  window.SpeechRecognition=class {
   start(){window.rec=this;window.started++;}
   stop(){this.onend?.();}
   abort(){window.aborted++;this.onend?.();}
  };
  window.Audio=class {constructor(){window.audio=this;} async play(){} pause(){} };
 });
 const page=await context.newPage();let turns=[];
 await page.route('**/api/v1/assistant/chat/stream',route=>{
  turns.push(route.request().postDataJSON());
  return route.fulfill({contentType:'application/x-ndjson',body:'{"type":"delta","delta":"Hello back."}\n{"type":"done","reply":"Hello back."}\n'});
 });
 await page.route('**/api/v1/assistant/speech',route=>route.fulfill({contentType:'audio/mpeg',body:'ID3fixture'}));

 await page.goto('http://localhost:3340');
 await page.getByRole('button',{name:'Assistant',exact:true}).click({timeout:30000});
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>window.started)).toBe(1);
 await page.evaluate(()=>window.rec.onresult({results:[[{transcript:'First saved topic'}]]}));
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible();
 const first=await page.getByRole('combobox',{name:'Conversation',exact:true}).inputValue();
 await page.getByRole('button',{name:'New chat',exact:true}).click();
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Start hands-free',exact:true})).toBeVisible();
 await expect(page.locator('.assistant-msg.user')).toHaveCount(0);
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>window.started)).toBe(2);
 await page.evaluate(()=>window.rec.onresult({results:[[{transcript:'Second saved topic'}]]}));
 await expect.poll(()=>turns.length).toBe(2);
 assert.equal(turns[1].history.length,0);
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible();
 const second=await page.getByRole('combobox',{name:'Conversation',exact:true}).inputValue();
 await page.getByRole('combobox',{name:'Conversation',exact:true}).selectOption(first);
 await expect(page.locator('.assistant-msg.user')).toContainText('First saved topic');
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toHaveCount(0);
 await page.reload();
 await page.getByRole('button',{name:'Assistant',exact:true}).click({timeout:30000});
 await expect(page.locator('.assistant-msg.user')).toContainText('First saved topic');
 await page.getByRole('combobox',{name:'Conversation',exact:true}).selectOption(second);
 await expect(page.locator('.assistant-msg.user')).toContainText('Second saved topic');
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect.poll(()=>page.evaluate(()=>window.started)).toBe(1);
 await page.getByRole('combobox',{name:'Conversation',exact:true}).selectOption(first);
 assert((await page.evaluate(()=>window.aborted))>=1,'session switch aborts recognition');
 let held;
 await page.route('**/api/v1/assistant/chat/stream',route=>{held=route;});
 await page.locator('.assistant-chat textarea').fill('Slow request');
 await page.locator('.assistant-chat textarea').press('Enter');
 await expect.poll(()=>Boolean(held)).toBe(true);
 await page.getByRole('combobox',{name:'Conversation',exact:true}).selectOption(second);
 await held.fulfill({contentType:'application/x-ndjson',body:'{"type":"done","reply":"Stale reply must not appear"}\n'}).catch(()=>{});
 await expect(page.locator('.assistant-msg.user')).toContainText('Second saved topic');
 await expect(page.getByText('Stale reply must not appear',{exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Stop',exact:true})).toHaveCount(0);
 await page.screenshot({path:'/tmp/brain-sessions-mobile.png'});
 console.log('PASS saved sessions: isolated replay, history reload, switch during playback and listening');
} finally {await browser.close();}
