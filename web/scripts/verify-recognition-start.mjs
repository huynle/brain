import { chromium, expect } from '@playwright/test';
import assert from 'node:assert/strict';
const browser=await chromium.launch();
try {
 const context=await browser.newContext({viewport:{width:390,height:844},serviceWorkers:'block'});
 await context.addInitScript(()=>{
  window.liveAudioContexts=0;window.started=0;window.aborted=0;window.micLevel=0;
  navigator.mediaDevices.getUserMedia=async()=>{window.track={muted:false,readyState:'live',stop(){this.readyState='ended';},getSettings:()=>({echoCancellation:true})};return {getTracks:()=>[window.track],getAudioTracks:()=>[window.track]};};
  window.AudioContext=class {
    constructor(){window.liveAudioContexts++;} state='running'; async resume(){} async close(){if(this.state!=='closed')window.liveAudioContexts--;this.state='closed';}
    createMediaStreamSource(){return {connect(){},disconnect(){}};}
    createAnalyser(){return {fftSize:1024,getFloatTimeDomainData(buffer){buffer.fill(window.micLevel);}};}
  };
  window.SpeechRecognition=class {
   start(){window.rec=this;window.started++;this.onaudiostart?.();}
   stop(){this.onend?.();}
   abort(){window.aborted++;this.onend?.();}
  };
  window.Audio=class {constructor(){window.audio=this;} async play(){} pause(){} };
 });
 const page=await context.newPage();let turns=[];const diagnostics=[];
 page.on('request',request=>{if(request.url().endsWith('/voice-diagnostics')) diagnostics.push(request.postDataJSON());});
 await page.route('**/api/v1/assistant/chat/stream',route=>{
  turns.push(route.request().postDataJSON());
  return route.fulfill({contentType:'application/x-ndjson',body:'{"type":"delta","delta":"Hello back."}\n{"type":"done","reply":"Hello back."}\n'});
 });
 await page.route('**/api/v1/assistant/speech',route=>route.fulfill({contentType:'audio/mpeg',body:'ID3fixture'}));

 await page.clock.install();
 await page.goto('http://localhost:3340');
 await page.getByRole('button',{name:'Assistant',exact:true}).click({timeout:30000});
 await page.evaluate(()=>{window.SpeechRecognition.prototype.start=function(){window.rec=this;window.started++;};});
 await page.getByRole('button',{name:'Start hands-free',exact:true}).click();
 await expect(page.getByText('Starting speech recognition…',{exact:true})).toBeVisible();
 assert.equal(await page.evaluate(()=>window.liveAudioContexts),0);
 await page.clock.fastForward(60000);
 await expect(page.getByRole('button',{name:'End hands-free',exact:true})).toBeVisible();
 assert.equal(turns.length,0);
 assert.equal(await page.evaluate(()=>window.aborted),0);
 for(let i=0;i<4;i++) {
   await page.evaluate(()=>{window.rec.onerror({error:'no-speech'});window.rec.onend();});
   await page.clock.fastForward(1100);
   await expect.poll(()=>page.evaluate(()=>window.started)).toBe(i+2);
 }
 await page.evaluate(()=>{window.rec.onaudiostart();window.rec.onresult({results:[[{transcript:'Hello after a long pause'}]]});});
 await page.clock.fastForward(1200);
 await expect.poll(()=>turns.length).toBe(1);
 assert.equal(turns[0].message,'Hello after a long pause');
 assert(diagnostics.some(e=>e.event==='start_requested'));
 assert(diagnostics.some(e=>e.event==='waiting'));
 assert(diagnostics.some(e=>e.event==='result'));
 assert(diagnostics.every(e=>!('transcript' in e) && !('audio' in e) && !JSON.stringify(e).includes('Hello after')));
 console.log('PASS long silence keeps hands-free enabled; repeated no-speech restarts; next spoken turn still sends');
}finally{await browser.close();}
