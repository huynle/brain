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
 const page=await context.newPage();let turns=[];
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
 await page.clock.fastForward(16000);
 await expect(page.getByText('No transcription received from the browser. Try Speak again, or use keyboard dictation.',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Start hands-free',exact:true})).toBeVisible();
 assert.equal(turns.length,0);
 assert.equal(await page.evaluate(()=>window.aborted),1);
 console.log('PASS stalled recognizer reports startup accurately, aborts after timeout, and sends no message');
}finally{await browser.close();}
