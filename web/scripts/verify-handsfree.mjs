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
 await expect(page.getByText('Listening… Your message sends after a pause.',{exact:true})).toBeVisible();
 await page.evaluate(()=>window.rec.onresult({results:[[{transcript:'First voice turn'}]]}));
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible({timeout:10000});
 assert.equal(turns.length,1);assert.equal(turns[0].message,'First voice turn');assert.equal(turns[0].voice,true);
 assert.equal(await page.evaluate(()=>window.started),1,'must not listen while assistant speaks');
 await page.evaluate(()=>window.audio.onended());
 await expect.poll(()=>page.evaluate(()=>window.started)).toBe(2);
 await page.evaluate(()=>window.rec.onresult({results:[[{transcript:'Second voice turn'}]]}));
 await expect.poll(()=>turns.length).toBe(2);
 assert.equal(turns[1].message,'Second voice turn');
 assert(turns[1].history.some(x=>x.role==='user'),'second turn retains history');
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toBeVisible();
 await page.evaluate(()=>{window.micLevel=.06;});
 await expect(page.getByRole('button',{name:'Stop audio',exact:true})).toHaveCount(0);
 await page.evaluate(()=>{window.micLevel=0;});
 await expect.poll(()=>page.evaluate(()=>window.started)).toBe(3);
 await page.getByRole('button',{name:'End hands-free',exact:true}).click();
 await expect(page.getByRole('button',{name:'Start hands-free',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 const stale=await page.evaluate(()=>{window.stale=window.rec;return window.started;});
 await page.getByRole('button',{name:'End hands-free',exact:true}).click();
 await page.evaluate(()=>{window.stale.onresult?.({results:[[{transcript:'Must never send'}]]});window.stale.onend?.();});
 assert.equal(turns.length,2);
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await page.evaluate(()=>window.rec.onerror({error:'not-allowed'}));
 await expect(page.getByText(/Microphone permission was denied/)).toBeVisible();
 await expect(page.getByRole('button',{name:'Start hands-free',exact:true})).toBeVisible();
 console.log('PASS hands-free: two automatic turns with history; quiet playback continues; sustained microphone input interrupts and rearms recognition; stop and permission denial prevent sending');
}finally{await browser.close();}
